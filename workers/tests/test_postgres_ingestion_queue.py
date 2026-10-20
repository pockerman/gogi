"""
Run from the repository root, against a migrated database, e.g.
GOGI_TEST_POSTGRES_DSN=postgres://gogi:gogi@localhost:5432/gogi_test PYTHONPATH=. pytest workers/tests
"""
import json
import os
import uuid
from urllib.parse import urlparse

import pytest

psycopg = pytest.importorskip("psycopg")

from workers.worker_utils.storage.postgres.ingestion_queue import PostgresIngestionQueue

DSN = os.getenv("GOGI_TEST_POSTGRES_DSN")
pytestmark = pytest.mark.skipif(not DSN, reason="GOGI_TEST_POSTGRES_DSN is not set")


@pytest.fixture
def queue():
    url = urlparse(DSN)
    queue = PostgresIngestionQueue(host=url.hostname, port=url.port or 5432, database=url.path.lstrip("/"),
                                   user=url.username, password=url.password, visibility_seconds=60)
    with psycopg.connect(DSN, autocommit=True) as connection:
        connection.execute("DELETE FROM gogi_ingestion_queue")
    yield queue
    with psycopg.connect(DSN, autocommit=True) as connection:
        connection.execute("DELETE FROM gogi_ingestion_queue")


def enqueue(job_id):
    with psycopg.connect(DSN, autocommit=True) as connection:
        connection.execute("INSERT INTO jobs (id, status, job_type) VALUES (%s, 'PENDING', 'DOCUMENT_INGESTION')",
                           (job_id,))
        connection.execute("INSERT INTO gogi_ingestion_queue (job_id, message) VALUES (%s, %s)",
                           (job_id, json.dumps({"job_id": job_id})))


def test_messages_are_taken_oldest_first_and_hidden_while_ingested(queue):
    first, second = str(uuid.uuid4()), str(uuid.uuid4())
    enqueue(first)
    enqueue(second)

    taken = queue.take()
    assert (taken.job_id, taken.message, taken.attempt) == (first, {"job_id": first}, 1)
    assert queue.take().job_id == second
    assert queue.take() is None

    queue.retry_later(first, 0)
    again = queue.take()
    assert (again.job_id, again.attempt) == (first, 2)

    queue.delete(first)
    queue.retry_later(second, 0)
    assert queue.take().job_id == second
    assert queue.take() is None
