from contextlib import contextmanager
from typing import NamedTuple

from psycopg import connect


class QueuedIngestion(NamedTuple):
    job_id: str
    message: dict
    # this delivery's number: 1 the first time it's taken
    attempt: int


class PostgresIngestionQueue:
    """gogi's ingestion queue table, where the documents service queues ingestions when the worker runs as a process
    (GOGI_INGESTION_QUEUE=postgres). A worker takes a message by hiding it for a while, and deletes it once done with
    it: a message a worker fails on, or dies holding, is taken again once it shows."""

    def __init__(self, host: str, port: int, database: str, user: str, password: str,
                 visibility_seconds: int = 900):
        self._conninfo = {"host": host, "port": port, "dbname": database, "user": user, "password": password}
        self._visibility = visibility_seconds

    @contextmanager
    def _cursor(self):
        with connect(**self._conninfo, autocommit=True) as connection, connection.cursor() as cur:
            yield cur

    def take(self) -> QueuedIngestion | None:
        """the oldest message showing, hidden from the other workers while this one ingests it; None if none shows"""
        with self._cursor() as cur:
            cur.execute(
                """
                UPDATE gogi_ingestion_queue
                SET attempts = attempts + 1, visible_at = NOW() + make_interval(secs => %s)
                WHERE job_id = (
                    SELECT job_id FROM gogi_ingestion_queue WHERE visible_at <= NOW()
                    ORDER BY enqueued_at FOR UPDATE SKIP LOCKED LIMIT 1
                )
                RETURNING job_id::text, message, attempts
                """,
                (self._visibility,),
            )
            row = cur.fetchone()
        return QueuedIngestion(*row) if row else None

    def extend(self, job_id: str):
        """keep the message hidden while it's being ingested"""
        with self._cursor() as cur:
            cur.execute("UPDATE gogi_ingestion_queue SET visible_at = NOW() + make_interval(secs => %s) "
                        "WHERE job_id = %s", (self._visibility, job_id))

    def retry_later(self, job_id: str, delay_seconds: int):
        """show the message again after the delay"""
        with self._cursor() as cur:
            cur.execute("UPDATE gogi_ingestion_queue SET visible_at = NOW() + make_interval(secs => %s) "
                        "WHERE job_id = %s", (delay_seconds, job_id))

    def delete(self, job_id: str):
        with self._cursor() as cur:
            cur.execute("DELETE FROM gogi_ingestion_queue WHERE job_id = %s", (job_id,))
