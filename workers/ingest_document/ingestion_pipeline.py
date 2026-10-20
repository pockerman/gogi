"""
The steps of ingesting a document, given the services they use, so they can be tested without them.

A document is parsed once, then chunked and embedded for each model the application named: each model gets chunks it
reads whole, stored in the index's collection for that model. Ingesting a document with a model again replaces what
the model embedded of it before, once the new chunks are stored; what other models embedded is kept.

A document can be deleted while it's being ingested. Deleting it marks its job CANCELLED and then deletes its chunks
from the collections of the models that embed it, so the pipeline records a model before storing its chunks, checks
before each step that the document is still wanted, and checks again after storing a model's chunks: chunks stored
after the delete removed the document's chunks are removed here.
"""
from typing import Callable, Dict, List, Protocol

from loguru import logger

from workers.ingest_document.ingestion_request import DocumentIngestionRequest

# chunks embedded between checks that the document is still wanted
EMBEDDING_BATCH_SIZE = 64


class IngestionCancelled(Exception):
    """The document was deleted, or its index was, while it was being ingested."""


class IngestionState(Protocol):
    def is_wanted(self, job_id: str, index_name: str, document_id: str) -> bool:
        """whether the job isn't cancelled and its document is still registered"""

    def replace_sections(self, job_id: str, index_name: str, document_id: str, sections) -> bool:
        """store the document's sections, so it can be read section by section, if the job is still the
        document's; returns whether they were stored"""

    def record_embedding(self, job_id: str, index_name: str, document_id: str, model: str, chunk_count: int) -> bool:
        """record that the model embeds the document, if the job is still the document's; returns whether it was"""


class Embeddings(Protocol):
    def embed(self, application: str, model: str, texts: List[str]) -> List[List[float]]:
        """the texts' vectors, in their order"""


class IngestionPipeline:

    def __init__(self, objects, parse_router, chunk_router, embeddings: Embeddings, vector_storage,
                 state: IngestionState, heartbeat: Callable[[], None] = lambda: None):
        self._objects = objects
        self._parse_router = parse_router
        self._chunk_router = chunk_router
        self._embeddings = embeddings
        self._vector_storage = vector_storage
        self._state = state
        self._heartbeat = heartbeat

    def run(self, request: DocumentIngestionRequest):
        """Ingest the document and return it with its chunks for each model. Raises IngestionCancelled if it's
        deleted meanwhile."""

        def check():
            self._heartbeat()
            if not self._state.is_wanted(request.job_id, request.index_name, request.document_id):
                raise IngestionCancelled(f"document {request.document_id} of {request.index_name} was deleted")

        # 1. read the document from the object store and parse it
        check()
        content = self._objects.read(bucket=request.bucket, object_key=request.object_key)
        extracted_document = self._parse_router.parse(file_bytes=content, filename=request.filename)
        check()
        if not self._state.replace_sections(request.job_id, request.index_name, request.document_id,
                                            extracted_document.sections):
            raise IngestionCancelled(f"document {request.document_id} of {request.index_name} was replaced")

        chunks_by_model: Dict[str, list] = {}
        for model in request.embedding_models:
            chunks_by_model[model.model] = self._embed(request, model, extracted_document, check)
        return extracted_document, chunks_by_model

    def _embed(self, request: DocumentIngestionRequest, model, extracted_document, check):
        # 2. chunk the document for the model
        chunks = self._chunk_router.chunk(extracted_document, chunking_strategy=request.chunk_strategy,
                                          chunk_size=model.chunk_size, chunk_overlap=model.chunk_overlap)

        # 3. embed the chunks, in batches, stopping if the document is deleted
        embeddings = []
        for start in range(0, len(chunks), EMBEDDING_BATCH_SIZE):
            check()
            batch = chunks[start:start + EMBEDDING_BATCH_SIZE]
            embeddings.extend(self._embeddings.embed(request.application, model.model, [chunk.text for chunk in batch]))

        # 4. store the vectors, unless the document was deleted meanwhile. The model is recorded first, so a delete
        # that starts while they're stored removes them from its collection
        check()
        if not self._state.record_embedding(request.job_id, request.index_name, request.document_id, model.model,
                                            len(chunks)):
            raise IngestionCancelled(f"document {request.document_id} of {request.index_name} was deleted")
        # each chunk records the job that stored it, so the job removes only its own chunks if it's stopped
        metadata = {**(request.metadata or {}), "job_id": request.job_id}
        self._vector_storage.insert(index_name=model.collection, document_id=request.document_id,
                                    chunks=chunks, embeddings=embeddings, metadata=metadata)
        try:
            check()
        except IngestionCancelled:
            # deleted while the chunks were stored: the delete may have run before they were
            removed = self._vector_storage.delete_by_document(index_name=model.collection,
                                                              document_id=request.document_id,
                                                              job_id=request.job_id)
            logger.info(f"Removed {removed} chunks of {request.document_id}, deleted during its ingestion")
            raise

        # 5. what the model embedded of the document before is replaced
        replaced = self._vector_storage.delete_other_jobs(index_name=model.collection, document_id=request.document_id,
                                                          job_id=request.job_id)
        if replaced:
            logger.debug(f"Replaced {replaced} chunks of {request.document_id} embedded by {model.model} before")
        return chunks
