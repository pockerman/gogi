from contextlib import contextmanager

from psycopg import connect


class PostgresDB:
    """The worker's database. Ingestions can run in threads of their own, so every operation has a connection of its
    own: threads sharing one would interleave their transactions."""

    def __init__(
        self,
        host: str,
        port: int,
        database: str,
        user: str,
        password: str,
    ):
        self._conninfo = {"host": host, "port": port, "dbname": database, "user": user, "password": password}

    @contextmanager
    def _connection(self):
        with connect(**self._conninfo, autocommit=True) as connection:
            yield connection

    def close(self):
        pass

    def mark_running(self, job_id: str):
        """the job is RUNNING, unless it's no longer PENDING: cancelled, or delivered again"""
        with self._connection() as connection, connection.cursor() as cur:
            cur.execute(
                """
                UPDATE jobs SET status = 'RUNNING', started_at = COALESCE(started_at, NOW())
                WHERE id = %s AND status = 'PENDING'
                """,
                (job_id,),
            )

    def update_job_status(
    self,
    job_id: str,
    status: str,
    progress: float,
    error_message: str | None = None, ):
        with self._connection() as connection, connection.cursor() as cur:
            cur.execute(
                """
                UPDATE jobs
                SET
                    status = %s,
                    error_message = %s,
                    completed_at = NOW()
                WHERE id = %s AND status <> 'CANCELLED'
                """,
                # the Go side scans this column into a plain (non-nullable) string, so
                # NULL must never be written here; use "" to mean "no error" instead.
                (status, error_message or "", job_id),
            )

    def update_document_stats(self, index_name: str, document_id: str, page_count: int, word_count: int,
                              job_id: str):
        """the document's stats, unless its content was replaced since the job started; its chunks are those of
        every model that embeds it"""
        with self._connection() as connection, connection.cursor() as cur:
            cur.execute(
                """
                UPDATE gogi_documents d
                SET page_count = %s, word_count = %s,
                    chunk_count = (SELECT COALESCE(SUM(e.chunk_count), 0) FROM gogi_document_embeddings e
                                   WHERE e.index_id = d.index_id AND e.document_id = d.id)
                WHERE d.id = %s AND d.index_id = (SELECT id FROM gogi_indexes WHERE name = %s) AND d.job_id = %s::uuid
                """,
                (page_count, word_count, document_id, index_name, job_id),
            )

    def record_embedding(self, job_id: str, index_name: str, document_id: str, model: str, chunk_count: int) -> bool:
        """record that the model embeds the document, replacing what it embedded before, if the job is still the
        document's and isn't cancelled. Returns whether it was recorded"""
        with self._connection() as connection, connection.cursor() as cur:
            cur.execute(
                """
                INSERT INTO gogi_document_embeddings (index_id, document_id, model, job_id, chunk_count)
                SELECT d.index_id, d.id, %s, %s::uuid, %s
                FROM gogi_documents d JOIN gogi_indexes i ON i.id = d.index_id
                JOIN jobs j ON j.id = d.job_id
                WHERE i.name = %s AND d.id = %s AND d.job_id = %s::uuid AND j.status <> 'CANCELLED'
                ON CONFLICT (index_id, document_id, model) DO UPDATE
                SET job_id = EXCLUDED.job_id, chunk_count = EXCLUDED.chunk_count, embedded_at = NOW()
                """,
                (model, job_id, chunk_count, index_name, document_id, job_id),
            )
            return cur.rowcount == 1

    def replace_sections(self, job_id: str, index_name: str, document_id: str, sections) -> bool:
        """store the document's sections, replacing any stored before, if the job is still the document's: its
        content may have been replaced since the job started. Returns whether they were stored"""
        with self._connection() as connection, connection.transaction(), connection.cursor() as cur:
            # the document's row is locked, so its content can't be replaced while its sections are stored
            cur.execute(
                """
                SELECT d.job_id::text FROM gogi_documents d JOIN gogi_indexes i ON i.id = d.index_id
                WHERE i.name = %s AND d.id = %s FOR UPDATE OF d
                """,
                (index_name, document_id),
            )
            row = cur.fetchone()
            if row is None or row[0] != job_id:
                return False
            cur.execute(
                """
                DELETE FROM gogi_document_sections
                WHERE document_id = %s AND index_id = (SELECT id FROM gogi_indexes WHERE name = %s)
                """,
                (document_id, index_name),
            )
            cur.executemany(
                """
                INSERT INTO gogi_document_sections
                    (index_id, document_id, position, title, level, page_start, page_end, content)
                SELECT id, %s, %s, %s, %s, %s, %s, %s FROM gogi_indexes WHERE name = %s
                """,
                [(document_id, position, section.heading or "", section.level, section.page_number or 0,
                  section.page_end or 0, section.content, index_name)
                 for position, section in enumerate(sections, start=1)],
            )
        return True

    def is_wanted(self, job_id: str, index_name: str, document_id: str) -> bool:
        """whether the ingestion job isn't cancelled and is still its document's: deleting a document or
        replacing its content cancels its job, and deleting an index deletes its documents"""
        with self._connection() as connection, connection.cursor() as cur:
            cur.execute(
                """
                SELECT
                    COALESCE((SELECT status <> 'CANCELLED' FROM jobs WHERE id = %s), TRUE)
                    AND EXISTS (
                        SELECT 1 FROM gogi_documents d JOIN gogi_indexes i ON i.id = d.index_id
                        WHERE i.name = %s AND d.id = %s AND d.job_id = %s::uuid
                    )
                """,
                (job_id, index_name, document_id, job_id),
            )
            return cur.fetchone()[0]
