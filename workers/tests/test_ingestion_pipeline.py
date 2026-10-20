"""
Run from the repository root: PYTHONPATH=. pytest workers/tests
(needs only the packages in workers/tests/requirements.txt)
"""
from types import SimpleNamespace

import pytest

from workers.ingest_document import ingestion_pipeline
from workers.ingest_document.ingestion_pipeline import IngestionCancelled, IngestionPipeline
from workers.ingest_document.ingestion_request import DocumentIngestionRequest, EmbeddingModelConfig


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
    """a chunk for every 100 words a 500 word document has to be cut into"""

    def __init__(self, words=500):
        self.words = words
        self.sizes = []

    def chunk(self, document, chunking_strategy, chunk_size, chunk_overlap):
        self.sizes.append(chunk_size)
        count = -(-self.words // chunk_size)
        return [SimpleNamespace(text=f"chunk {i} of {chunk_size} words") for i in range(count)]


class Embeddings:
    def __init__(self, on_batch=lambda: None):
        self.batches = []
        self.on_batch = on_batch

    def embed(self, application, model, texts):
        self.batches.append((application, model, len(texts)))
        self.on_batch()
        return [[0.1, 0.2] for _ in texts]


class VectorStorage:
    """the chunks of each collection, with the job that stored them"""

    def __init__(self, on_insert=lambda: None):
        self.collections = {}
        self.on_insert = on_insert

    def insert(self, index_name, document_id, chunks, embeddings, metadata):
        self.collections.setdefault(index_name, []).extend((chunk, metadata["job_id"]) for chunk in chunks)
        self.metadata = metadata
        self.on_insert()

    def delete_by_document(self, index_name, document_id, job_id=None):
        kept = [(chunk, job) for chunk, job in self.collections.get(index_name, []) if job_id and job != job_id]
        removed = len(self.collections.get(index_name, [])) - len(kept)
        self.collections[index_name] = kept
        return removed

    def delete_other_jobs(self, index_name, document_id, job_id):
        kept = [(chunk, job) for chunk, job in self.collections.get(index_name, []) if job == job_id]
        removed = len(self.collections.get(index_name, [])) - len(kept)
        self.collections[index_name] = kept
        return removed

    def jobs(self, collection):
        return {job for _, job in self.collections.get(collection, [])}


class State:
    """the document is wanted until it's deleted"""

    def __init__(self):
        self.deleted = False
        self.sections = None
        self.replaced = False
        self.embedded = {}

    def is_wanted(self, job_id, index_name, document_id):
        return not self.deleted

    def replace_sections(self, job_id, index_name, document_id, sections):
        if self.replaced:
            return False
        self.sections = sections
        return True

    def record_embedding(self, job_id, index_name, document_id, model, chunk_count):
        if self.deleted:
            return False
        self.embedded[model] = chunk_count
        return True


BGE = EmbeddingModelConfig(model="bge-small-en-v1.5", collection="maria--aeronautics.bge-small-en-v1.5",
                           chunk_size=300, chunk_overlap=30)
MINILM = EmbeddingModelConfig(model="all-MiniLM-L6-v2", collection="maria--aeronautics.all-MiniLM-L6-v2",
                              chunk_size=150, chunk_overlap=15)


def request(*models, job_id="job-1"):
    return DocumentIngestionRequest(job_id=job_id, index_name="maria--aeronautics", filename="fe-basics.txt",
                                    document_id="fe-basics", application="qubit-notes",
                                    embedding_models=list(models or [BGE]), chunk_strategy="fixed", metadata={},
                                    bucket="gogi-documents", object_key="maria--aeronautics/fe-basics")


def pipeline(state, chunker=None, embeddings=None, storage=None, objects=None):
    return IngestionPipeline(objects=objects or Objects(), parse_router=Parser(), chunk_router=chunker or Chunker(),
                             embeddings=embeddings or Embeddings(), vector_storage=storage or VectorStorage(),
                             state=state)


@pytest.fixture(autouse=True)
def small_batches(monkeypatch):
    monkeypatch.setattr(ingestion_pipeline, "EMBEDDING_BATCH_SIZE", 2)


def test_a_document_is_chunked_and_embedded_for_each_model_into_its_collection():
    chunker, embeddings, storage, state = Chunker(words=500), Embeddings(), VectorStorage(), State()

    _, chunks = pipeline(state, chunker=chunker, embeddings=embeddings, storage=storage).run(request(BGE, MINILM))

    # MiniLM reads less, so it gets more, shorter chunks
    assert chunker.sizes == [300, 150]
    assert len(chunks["bge-small-en-v1.5"]) == 2 and len(chunks["all-MiniLM-L6-v2"]) == 4
    assert len(storage.collections[BGE.collection]) == 2 and len(storage.collections[MINILM.collection]) == 4
    assert state.embedded == {"bge-small-en-v1.5": 2, "all-MiniLM-L6-v2": 4}


def test_chunks_are_embedded_in_batches_by_the_applications_model():
    embeddings = Embeddings()

    pipeline(State(), chunker=Chunker(words=1500), embeddings=embeddings).run(request(BGE))

    assert embeddings.batches == [("qubit-notes", "bge-small-en-v1.5", 2), ("qubit-notes", "bge-small-en-v1.5", 2),
                                  ("qubit-notes", "bge-small-en-v1.5", 1)]


def test_ingesting_again_with_a_model_replaces_what_it_embedded_and_keeps_the_other_models():
    storage, state = VectorStorage(), State()
    pipeline(state, storage=storage).run(request(BGE, MINILM, job_id="job-1"))

    pipeline(state, storage=storage).run(request(BGE, job_id="job-2"))

    assert storage.jobs(BGE.collection) == {"job-2"}
    assert storage.jobs(MINILM.collection) == {"job-1"}


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
    embeddings = Embeddings(on_batch=lambda: setattr(state, "deleted", True))

    with pytest.raises(IngestionCancelled):
        pipeline(state, chunker=Chunker(words=1800), embeddings=embeddings, storage=storage).run(request())

    assert len(embeddings.batches) == 1
    assert storage.collections == {}


def test_chunks_stored_after_the_delete_removed_the_document_are_removed():
    state = State()
    # the delete runs while the chunks are being stored, so it misses them
    storage = VectorStorage(on_insert=lambda: setattr(state, "deleted", True))

    with pytest.raises(IngestionCancelled):
        pipeline(state, storage=storage).run(request())

    assert storage.collections[BGE.collection] == []


def test_chunks_record_the_job_and_a_stopped_job_removes_only_its_own():
    state, storage = State(), VectorStorage()
    pipeline(state, storage=storage).run(request(job_id="job-1"))
    storage.on_insert = lambda: setattr(state, "deleted", True)

    with pytest.raises(IngestionCancelled):
        pipeline(state, storage=storage).run(request(job_id="job-2"))

    assert storage.metadata["job_id"] == "job-2"
    assert storage.jobs(BGE.collection) == {"job-1"}


def test_a_job_whose_document_was_replaced_stores_nothing():
    state, storage = State(), VectorStorage()
    state.replaced = True

    with pytest.raises(IngestionCancelled):
        pipeline(state, storage=storage).run(request())

    assert state.sections is None and storage.collections == {}
