"""
Ingest a document pipeline worker. The pipeline has the following steps

1. extract the document from the bytes
2. Create the chunks.
3. Generate embeddings for the chunks
4. Store the chunks and embeddings in the vector database
"""
import os
from openai import AuthenticationError
from temporalio import activity
from temporalio.exceptions import ApplicationError
from loguru import logger


from workers.worker_utils.documents.parsers.parser_router import ParseRouter
from workers.worker_utils.documents.parsers.document_parser_base import ParserType
from workers.worker_utils.documents.parsers.text_parser import TextParser
from workers.worker_utils.documents.parsers.pdf_parser import PdfParser
from workers.worker_utils.chunking.chunking_router import ChunkingRouter
from workers.worker_utils.chunking.chunking_strategy_base import ChunkingType
from workers.worker_utils.chunking.fixed_size_chunking import FixedSizeChunking
from workers.worker_utils.storage.vector_storage_router import VectorStorageRouter
from workers.worker_utils.storage.postgres.db import PostgresDB
from workers.worker_utils.embeddings.embbeddings_router import EmbeddingsRouter
from workers.worker_utils.embeddings.embeder_base import EmbedderClientType, EmbedderModelType
from workers.worker_utils.embeddings.sentence_transformer_embeddings import SentenceTransformerEmbeddings
from workers.worker_utils.embeddings.openai_embeddings import OpenAIEmbeddings
from workers.worker_utils.job_status import JobStatus

from workers.ingest_document.ingestion_request import DocumentIngestionRequest


PARSERS = {

   ParserType.TEXT: TextParser(),
   ParserType.PDF: PdfParser(),
}

parse_router = ParseRouter(parsers=PARSERS)


CHUNKING_STRATEGIES = {
    ChunkingType.FIXED: FixedSizeChunking()
}

chunk_router = ChunkingRouter(chunking_strategies=CHUNKING_STRATEGIES)

EMBEDDERS = {

   (EmbedderModelType.CLIP, EmbedderClientType.SENTENCE_TRANSFORMER): SentenceTransformerEmbeddings(),
   (EmbedderModelType.TEXT_EMBEDDING_3_SMALL, EmbedderClientType.OPENAI): OpenAIEmbeddings(),
}

embeddings_router = EmbeddingsRouter(embedders=EMBEDDERS)

vector_storage_router = VectorStorageRouter(name=os.getenv("VECTOR_STORAGE_TYPE"),
                                            host=os.getenv("VECTOR_STORAGE_HOST"),
                                            port=os.getenv("VECTOR_STORAGE_PORT"))

db = PostgresDB(host=os.getenv("DB_HOST"), port=os.getenv("DB_PORT"), 
                database=os.getenv("DB_NAME"), user=os.getenv("DB_USER"),
                password=os.getenv("DB_PASSWORD"))

@activity.defn(name="ingest_document")
async def ingest_document(req):

    request = DocumentIngestionRequest(**req)
    logger.debug(f"Starting job {request.job_id}")

    print(f"ingest_document received request: {req}")

    try:
        # 1. Parse the document
        extracted_document = parse_router.parse(file_bytes=request.content, filename=request.filename)

        # 2. Create chunks
        chunking_strategy = request.chunk_strategy
        chunks = chunk_router.chunk(extracted_document, chunking_strategy=chunking_strategy,
                                    chunk_size=request.chunk_size, chunk_overlap=request.chunk_overlap)

        # 3. Create the embeddings for the chunks
        embeddings = embeddings_router.embed_chunks(chunks=chunks,
                                                    model=request.embeddings_model,
                                                    client=request.embeddings_client)
        embeddings_vals = [ embed.embeddings for embed in embeddings]


        # 4. Store vectors
        vector_storage_router.insert(index_name=request.index_name, document_id=request.document_id,
                                     chunks=chunks, embeddings=embeddings_vals, metadata=request.metadata)
    except AuthenticationError as e:
        # A bad/missing API key will never succeed on retry: fail the job now instead of
        # burning through the activity's retry budget.
        error_message = f"Embeddings provider rejected the API key: {e}"
        logger.error(f"Job {request.job_id} failed (non-retryable): {error_message}")
        db.update_job_status(job_id=request.job_id, status=JobStatus.JobFailed, progress=0.0,
                             error_message=error_message)
        raise ApplicationError(error_message, type="AuthenticationError", non_retryable=True) from e
    except Exception as e:
        info = activity.info()
        max_attempts = info.retry_policy.maximum_attempts if info.retry_policy else 0
        is_last_attempt = max_attempts > 0 and info.attempt >= max_attempts
        if is_last_attempt:
            error_message = str(e)
            logger.error(f"Job {request.job_id} failed after {info.attempt} attempt(s): {error_message}")
            db.update_job_status(job_id=request.job_id, status=JobStatus.JobFailed, progress=0.0,
                                 error_message=error_message)
        raise

    # 5. Update the job status
    db.update_job_status(job_id=request.job_id, status=JobStatus.JobCompleted, progress=100.0)

    logger.debug(f"Finished job {request.job_id}")
    return "done"



