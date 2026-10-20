"""
Ingest a document pipeline worker. The pipeline has the following steps

1. extract the document from the bytes
2. for each embedding model the application named: create the chunks, have the model service embed them, and store
   them in the index's collection for the model
"""
import os
from temporalio import activity
from temporalio.exceptions import ApplicationError
from loguru import logger


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
from workers.ingest_document.search_request import DocumentSearchRequest
from workers.ingest_document.delete_request import DocumentDeleteRequest


PARSERS = {

   ParserType.TEXT: TextParser(),
   ParserType.MARKDOWN: MarkdownParser(),
   ParserType.PDF: PdfParser(),
}

parse_router = ParseRouter(parsers=PARSERS)


CHUNKING_STRATEGIES = {
    ChunkingType.FIXED: FixedSizeChunking()
}

chunk_router = ChunkingRouter(chunking_strategies=CHUNKING_STRATEGIES)

# the workers run no embedding model: the model service calls them, with the application's keys
embeddings = ModelServiceEmbeddings(address=os.getenv("GOGI_LLM_SERVICE_ADDR", "llms:50057"))

vector_storage_router = VectorStorageRouter(name=os.getenv("VECTOR_STORAGE_TYPE"),
                                            host=os.getenv("VECTOR_STORAGE_HOST"),
                                            port=os.getenv("VECTOR_STORAGE_PORT"))

db = PostgresDB(host=os.getenv("DB_HOST"), port=os.getenv("DB_PORT"), 
                database=os.getenv("DB_NAME"), user=os.getenv("DB_USER"),
                password=os.getenv("DB_PASSWORD"))

# the uploads: MinIO in development, S3 or Blob Storage in the cloud
object_store = object_store_from_env()

pipeline = IngestionPipeline(objects=object_store, parse_router=parse_router, chunk_router=chunk_router,
                             embeddings=embeddings, vector_storage=vector_storage_router, state=db,
                             heartbeat=activity.heartbeat)

@activity.defn(name="ingest_document")
def ingest_document(req):

    request = DocumentIngestionRequest(**req)
    logger.debug(f"Starting job {request.job_id}")

    try:
        extracted_document, _ = pipeline.run(request)

        words = sum(len(section.content.split()) for section in extracted_document.sections)
        db.update_document_stats(index_name=request.index_name, document_id=request.document_id,
                                 page_count=extracted_document.page_count, word_count=words, job_id=request.job_id)
    except IngestionCancelled as e:
        # the job is CANCELLED already, or its document went with its index; retrying can't help
        logger.info(f"Job {request.job_id} stopped: {e}")
        raise ApplicationError(str(e), type="IngestionCancelled", non_retryable=True) from e
    except EmbeddingRefused as e:
        # e.g. a model the application can't use any more, or a key its provider refused: asking again won't
        # help, so the job fails now instead of burning through the activity's retry budget
        error_message = f"The embedding model refused: {e}"
        logger.error(f"Job {request.job_id} failed (non-retryable): {error_message}")
        db.update_job_status(job_id=request.job_id, status=JobStatus.JobFailed, progress=0.0,
                             error_message=error_message)
        raise ApplicationError(error_message, type="EmbeddingRefused", non_retryable=True) from e
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


def _build_where_clause(metadata_filter, document_ids):
    conditions = [{key: value} for key, value in (metadata_filter or {}).items()]
    if document_ids:
        conditions.append({"document_id": {"$in": document_ids}})

    if not conditions:
        return None
    if len(conditions) == 1:
        return conditions[0]
    return {"$and": conditions}


@activity.defn(name="search_document")
def search_document(req):

    request = DocumentSearchRequest(**req)
    logger.debug(f"Searching index {request.index_name} for: {request.query!r}")

    try:
        query_embedding = embeddings.embed(request.application, request.embedding_model, [request.query])[0]
        # only the documents the model embedded are in its collection
        results = vector_storage_router.search(
            index_name=request.collection,
            query_embedding=query_embedding,
            top_k=request.top_k,
            metadata_filters=_build_where_clause(request.metadata_filter, request.document_ids),
        )
    except EmbeddingRefused as e:
        error_message = f"The embedding model refused: {e}"
        logger.error(f"Search of {request.index_name} failed (non-retryable): {error_message}")
        raise ApplicationError(error_message, type="EmbeddingRefused", non_retryable=True) from e

    return {
        "chunks": [
            {
                "chunk_id": r.chunk_id,
                "document_id": r.document_id,
                "index_name": request.index_name,
                "content": r.text,
                "score": r.score,
                "metadata": r.metadata,
            }
            for r in results
        ]
    }


@activity.defn(name="delete_document")
def delete_document(req):

    request = DocumentDeleteRequest(**req)
    logger.debug(f"Deleting document {request.document_id} from index {request.index_name}")

    chunks_deleted = sum(vector_storage_router.delete_by_document(index_name=collection,
                                                                  document_id=request.document_id)
                         for collection in request.collections)
    return {"chunks_deleted": chunks_deleted}
