"""
Run from the repository root: PYTHONPATH=. pytest workers/tests
(needs only the packages in workers/tests/requirements.txt)
"""
from types import SimpleNamespace

import pytest

from workers.ingest_document import ingestion_pipeline
from workers.ingest_document.ingestion_pipeline import IngestionCancelled, IngestionPipeline
from workers.ingest_document.ingestion_request import DocumentIngestionRequest


class Objects:
    def __init__(self):
        self.reads = 0

    def read(self, bucket, object_key):
        self.reads += 1
        return b"text"


class Parser:
    def parse(self, file_bytes, filename):
        return SimpleNamespace(sections=["a section"])


class Chunker:
    def __init__(self, count):
        self.count = count

    def chunk(self, document, chunking_strategy, chunk_size, chunk_overlap):
        return [f"chunk {i}" for i in range(self.count)]


class Embedder:
    def __init__(self, on_batch=lambda: None):
        self.batches = []
        self.on_batch = on_batch

    def embed_chunks(self, chunks, model, client):
        self.batches.append(len(chunks))
        self.on_batch()
        return [SimpleNamespace(embeddings=[0.1, 0.2]) for _ in chunks]


class VectorStorage:
    def __init__(self, on_insert=lambda: None):
        self.chunks = []
        self.on_insert = on_insert

    def insert(self, index_name, document_id, chunks, embeddings, metadata):
        self.chunks.extend(chunks)
        self.metadata = metadata
        self.on_insert()

    def delete_by_document(self, index_name, document_id, job_id=None):
        self.deleted_job = job_id
        removed = len(self.chunks)
        self.chunks = []
        return removed


class State:
    """the document is wanted until it's deleted"""

    def __init__(self):
        self.deleted = False
        self.sections = None
        self.replaced = False

    def is_wanted(self, job_id, index_name, document_id):
        return not self.deleted

    def replace_sections(self, job_id, index_name, document_id, sections):
        if self.replaced:
            return False
        self.sections = sections
        return True


def request():
    return DocumentIngestionRequest(job_id="job-1", index_name="maria--aeronautics", filename="fe-basics.txt",
                                    document_id="fe-basics", batch_size=10, embeddings_model="clip",
                                    embeddings_client="sentence-transformer", chunk_strategy="fixed",
                                    metadata={}, bucket="gogi-documents", object_key="maria--aeronautics/fe-basics")


def pipeline(state, chunks=3, embedder=None, storage=None, objects=None):
    return IngestionPipeline(objects=objects or Objects(), parse_router=Parser(), chunk_router=Chunker(chunks),
                             embeddings_router=embedder or Embedder(), vector_storage=storage or VectorStorage(),
                             state=state)


@pytest.fixture(autouse=True)
def small_batches(monkeypatch):
    monkeypatch.setattr(ingestion_pipeline, "EMBEDDING_BATCH_SIZE", 2)


def test_a_document_is_embedded_in_batches_and_stored():
    embedder, storage = Embedder(), VectorStorage()

    _, chunks = pipeline(State(), chunks=5, embedder=embedder, storage=storage).run(request())

    assert embedder.batches == [2, 2, 1]
    assert storage.chunks == chunks and len(chunks) == 5


def test_the_sections_are_stored():
    state = State()

    pipeline(state).run(request())

    assert state.sections == ["a section"]


def test_a_document_deleted_before_its_ingestion_starts_is_not_read():
    state, objects = State(), Objects()
    state.deleted = True

    with pytest.raises(IngestionCancelled):
        pipeline(state, objects=objects).run(request())

    assert objects.reads == 0


def test_a_document_deleted_while_it_is_embedded_stops_and_stores_nothing():
    state, storage = State(), VectorStorage()
    embedder = Embedder(on_batch=lambda: setattr(state, "deleted", True))

    with pytest.raises(IngestionCancelled):
        pipeline(state, chunks=6, embedder=embedder, storage=storage).run(request())

    assert embedder.batches == [2]
    assert storage.chunks == []


def test_chunks_stored_after_the_delete_removed_the_document_are_removed():
    state = State()
    # the delete runs while the chunks are being stored, so it misses them
    storage = VectorStorage(on_insert=lambda: setattr(state, "deleted", True))

    with pytest.raises(IngestionCancelled):
        pipeline(state, storage=storage).run(request())

    assert storage.chunks == []


def test_chunks_record_the_job_and_a_stopped_job_removes_only_its_own():
    state = State()
    storage = VectorStorage(on_insert=lambda: setattr(state, "deleted", True))

    with pytest.raises(IngestionCancelled):
        pipeline(state, storage=storage).run(request())

    assert storage.metadata["job_id"] == "job-1"
    assert storage.deleted_job == "job-1"


def test_a_job_whose_document_was_replaced_stores_nothing():
    state, storage = State(), VectorStorage()
    state.replaced = True

    with pytest.raises(IngestionCancelled):
        pipeline(state, storage=storage).run(request())

    assert state.sections is None and storage.chunks == []
