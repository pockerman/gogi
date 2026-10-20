"""
Run from the repository root: PYTHONPATH=. pytest workers/tests
"""
import json
from types import SimpleNamespace

import pytest

from workers.ingest_document import aws_lambda, ingestion_handler
from workers.ingest_document.ingestion_handler import Ingestion, Retry
from workers.worker_utils.embeddings.model_service_embeddings import EmbeddingRefused


class Database:
    """the jobs' statuses, and the document's state as the pipeline sees it"""

    def __init__(self):
        self.statuses = {}
        self.stats = None
        self.wanted = True

    def mark_running(self, job_id):
        self.statuses.setdefault(job_id, "RUNNING")

    def update_job_status(self, job_id, status, progress, error_message=None):
        self.statuses[job_id] = (status, error_message)

    def update_document_stats(self, index_name, document_id, page_count, word_count, job_id):
        self.stats = (page_count, word_count)

    def is_wanted(self, job_id, index_name, document_id):
        return self.wanted

    def replace_sections(self, job_id, index_name, document_id, sections):
        return True

    def record_embedding(self, job_id, index_name, document_id, model, chunk_count):
        return self.wanted


class Embeddings:
    def __init__(self, error=None):
        self.error = error

    def embed(self, application, model, texts):
        if self.error:
            raise self.error
        return [[0.1, 0.2] for _ in texts]


class Storage:
    def __init__(self):
        self.inserted = 0

    def insert(self, index_name, document_id, chunks, embeddings, metadata):
        self.inserted += len(chunks)

    def delete_by_document(self, index_name, document_id, job_id=None):
        return 0

    def delete_other_jobs(self, index_name, document_id, job_id):
        return 0


def services(embeddings=None, storage=None):
    document = SimpleNamespace(sections=[SimpleNamespace(content="shape functions of linear elements")],
                               page_count=2)
    return {
        "objects": SimpleNamespace(read=lambda bucket, object_key: b"text"),
        "parse_router": SimpleNamespace(parse=lambda file_bytes, filename: document),
        "chunk_router": SimpleNamespace(chunk=lambda *a, **k: [SimpleNamespace(text="shape functions")]),
        "embeddings": embeddings or Embeddings(),
        "vector_storage": storage or Storage(),
    }


MESSAGE = {
    "job_id": "job-1", "index_name": "maria--aeronautics", "filename": "fem-notes.md", "document_id": "fem-notes",
    "application": "qubit-notes", "bucket": "gogi-documents", "object_key": "maria--aeronautics/fem-notes",
    "chunk_strategy": "fixed", "batch_size": 0, "metadata": {},
    "embedding_models": [{"model": "bge-small-en-v1.5", "collection": "maria--aeronautics.bge-small-en-v1.5",
                          "chunk_size": 307, "chunk_overlap": 30}],
}


def test_an_ingestion_completes_its_job():
    db, storage = Database(), Storage()

    Ingestion(services(storage=storage), db).handle(MESSAGE)

    assert db.statuses["job-1"] == ("COMPLETED", None)
    assert db.stats == (2, 5)
    assert storage.inserted == 1


def test_a_failed_ingestion_is_tried_again_until_its_last_attempt_fails_its_job():
    db = Database()
    ingestion = Ingestion(services(embeddings=Embeddings(error=ConnectionError("llms is down"))), db, max_attempts=3)

    for attempt in (1, 2):
        with pytest.raises(Retry):
            ingestion.handle(MESSAGE, attempt=attempt)
    assert db.statuses["job-1"] == "RUNNING"

    ingestion.handle(MESSAGE, attempt=3)

    assert db.statuses["job-1"] == ("FAILED", "llms is down")


def test_a_model_that_refuses_fails_the_job_at_once():
    db = Database()
    ingestion = Ingestion(services(embeddings=Embeddings(error=EmbeddingRefused("no key"))), db)

    ingestion.handle(MESSAGE, attempt=1)

    status, error = db.statuses["job-1"]
    assert status == "FAILED" and "no key" in error


def test_a_cancelled_ingestion_is_done_with():
    db = Database()
    db.wanted = False

    Ingestion(services(), db).handle(MESSAGE)

    assert db.statuses["job-1"] == "RUNNING"


def test_a_message_that_is_not_an_ingestion_is_dropped():
    db = Database()

    Ingestion(services(), db).handle({"job_id": "job-1"})

    assert db.statuses == {}


def test_the_lambda_reports_the_messages_to_deliver_again(monkeypatch):
    handled = []

    class Handler:
        def handle(self, message, attempt):
            handled.append((message["job_id"], attempt))
            if message["job_id"] == "job-2":
                raise Retry("llms is down")

    monkeypatch.setattr(aws_lambda, "ingestion_from_env", lambda: Handler())
    event = {"Records": [
        {"messageId": "m-1", "body": json.dumps({"job_id": "job-1"}), "attributes": {"ApproximateReceiveCount": "1"}},
        {"messageId": "m-2", "body": json.dumps({"job_id": "job-2"}), "attributes": {"ApproximateReceiveCount": "2"}},
        {"messageId": "m-3", "body": "not json", "attributes": {}},
    ]}

    response = aws_lambda.handler(event, None)

    assert response == {"batchItemFailures": [{"itemIdentifier": "m-2"}]}
    assert handled == [("job-1", 1), ("job-2", 2)]


def test_the_handler_is_set_up_once_per_process(monkeypatch):
    for name in ("DB_HOST", "VECTOR_STORAGE_TYPE", "VECTOR_STORAGE_HOST", "VECTOR_STORAGE_PORT"):
        monkeypatch.setenv(name, "localhost" if name != "VECTOR_STORAGE_PORT" else "8000")
    monkeypatch.setenv("VECTOR_STORAGE_TYPE", "chromadb")
    ingestion_handler.ingestion_from_env.cache_clear()

    assert ingestion_handler.ingestion_from_env() is ingestion_handler.ingestion_from_env()
