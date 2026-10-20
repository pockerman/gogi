"""
Ingests the documents the documents service hands to the worker through the ingestion queue. A message names the
document, where its content is in the object store, and the embedding models the application named; the pipeline
parses the document once, then for each model chunks it, has the model service embed the chunks and stores them in
the index's collection for the model.

The handler is the same wherever the worker runs; only what delivers the messages differs:

- local_worker.py polls gogi's queue table in Postgres, e.g. in development (GOGI_INGESTION_QUEUE=postgres)
- aws_lambda.py is a Lambda that SQS triggers (GOGI_INGESTION_QUEUE=sqs)
- workers/azure_function is a Function that an Azure Storage queue triggers (GOGI_INGESTION_QUEUE=azure)

A message the handler raises Retry for is delivered again, after a while. It doesn't for the last attempt, nor when
asking again can't help: the job fails instead, and the message is done with.
"""
import os
from functools import lru_cache
from typing import Callable

from loguru import logger
from pydantic import ValidationError

from workers.worker_utils.documents.parsers.parser_router import ParseRouter
from workers.worker_utils.documents.parsers.document_parser_base import ParserType
from workers.worker_utils.documents.parsers.text_parser import TextParser
from workers.worker_utils.documents.parsers.pdf_parser import PdfParser
from workers.worker_utils.documents.parsers.markdown_parser import MarkdownParser
from workers.worker_utils.chunking.chunking_router import ChunkingRouter
from workers.worker_utils.chunking.chunking_strategy_base import ChunkingType
from workers.worker_utils.chunking.fixed_size_chunking import FixedSizeChunking
from workers.worker_utils.storage.vector_storage_router import VectorStorageRouter
from workers.worker_utils.storage.postgres.db import PostgresDB
from workers.worker_utils.storage.objects.object_store import object_store_from_env
from workers.worker_utils.embeddings.model_service_embeddings import ModelServiceEmbeddings, EmbeddingRefused
from workers.worker_utils.job_status import JobStatus

from workers.ingest_document.ingestion_request import DocumentIngestionRequest
from workers.ingest_document.ingestion_pipeline import IngestionPipeline, IngestionCancelled

# how many times a message is delivered at most; the queue's own limit (SQS' maxReceiveCount, the Function's
# maxDequeueCount) must be no lower, or the job is left RUNNING when the queue gives up on its message
MAX_ATTEMPTS = int(os.getenv("GOGI_INGESTION_MAX_ATTEMPTS", "3"))


class Retry(Exception):
    """The ingestion failed, but may succeed if its message is delivered again."""


class Ingestion:
    """Ingests the documents of the messages, and reports on their jobs."""

    def __init__(self, services: dict, db, max_attempts: int = MAX_ATTEMPTS):
        # what the pipeline needs, but its heartbeat: objects, parse_router, chunk_router, embeddings, vector_storage
        self._services = services
        self._db = db
        self._max_attempts = max_attempts

    def handle(self, message: dict, attempt: int = 1, heartbeat: Callable[[], None] = lambda: None) -> None:
        """Ingest the document of the message: its attempt-th delivery. Returns when the message is done with,
        however the ingestion ended; raises Retry if it's to be delivered again."""
        try:
            request = DocumentIngestionRequest(**message)
        except ValidationError as e:
            # no delivery of it will be any better
            logger.error(f"Dropped a message that is not an ingestion: {e}")
            return

        logger.debug(f"Starting job {request.job_id} (attempt {attempt})")
        self._db.mark_running(job_id=request.job_id)
        pipeline = IngestionPipeline(**self._services, state=self._db, heartbeat=heartbeat)
        try:
            extracted_document, _ = pipeline.run(request)

            words = sum(len(section.content.split()) for section in extracted_document.sections)
            self._db.update_document_stats(index_name=request.index_name, document_id=request.document_id,
                                           page_count=extracted_document.page_count, word_count=words,
                                           job_id=request.job_id)
        except IngestionCancelled as e:
            # the job is CANCELLED already, or its document went with its index; asking again can't help
            logger.info(f"Job {request.job_id} stopped: {e}")
            return
        except EmbeddingRefused as e:
            # e.g. a model the application can't use any more, or a key its provider refused: asking again won't
            # help, so the job fails now instead of using up its attempts
            self._fail(request.job_id, f"The embedding model refused: {e}")
            return
        except Exception as e:
            if attempt >= self._max_attempts:
                self._fail(request.job_id, str(e) or type(e).__name__, attempt)
                return
            logger.warning(f"Job {request.job_id} failed on attempt {attempt} of {self._max_attempts}: {e}")
            raise Retry(str(e)) from e

        self._db.update_job_status(job_id=request.job_id, status=JobStatus.JobCompleted, progress=100.0)
        logger.debug(f"Finished job {request.job_id}")

    def _fail(self, job_id: str, error_message: str, attempts: int | None = None):
        tried = f" after {attempts} attempt(s)" if attempts else ""
        logger.error(f"Job {job_id} failed{tried}: {error_message}")
        self._db.update_job_status(job_id=job_id, status=JobStatus.JobFailed, progress=0.0,
                                   error_message=error_message)


def database_from_env() -> PostgresDB:
    return PostgresDB(host=os.getenv("DB_HOST"), port=os.getenv("DB_PORT"), database=os.getenv("DB_NAME"),
                      user=os.getenv("DB_USER"), password=os.getenv("DB_PASSWORD"))


@lru_cache(maxsize=1)
def ingestion_from_env() -> Ingestion:
    """The handler, set up from the environment once per process: a function's process serves many messages."""
    services = {
        # the uploads: MinIO in development, S3 or Blob Storage in the cloud
        "objects": object_store_from_env(),
        "parse_router": ParseRouter(parsers={
            ParserType.TEXT: TextParser(),
            ParserType.MARKDOWN: MarkdownParser(),
            ParserType.PDF: PdfParser(),
        }),
        "chunk_router": ChunkingRouter(chunking_strategies={ChunkingType.FIXED: FixedSizeChunking()}),
        # the worker runs no embedding model: the model service calls them, with the application's keys
        "embeddings": ModelServiceEmbeddings(address=os.getenv("GOGI_LLM_SERVICE_ADDR", "llms:50057")),
        "vector_storage": VectorStorageRouter(name=os.getenv("VECTOR_STORAGE_TYPE"),
                                              host=os.getenv("VECTOR_STORAGE_HOST"),
                                              port=os.getenv("VECTOR_STORAGE_PORT")),
    }
    return Ingestion(services=services, db=database_from_env())
