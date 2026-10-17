from psycopg import connect


class PostgresDB:

    def __init__(
        self,
        host: str,
        port: int,
        database: str,
        user: str,
        password: str,
    ):
        self._conn = connect(
            host=host,
            port=port,
            dbname=database,
            user=user,
            password=password,
            autocommit=True,
        )

    def close(self):
        self._conn.close()

    def update_job_status(
    self,
    job_id: str,
    status: str,
    progress: float,
    error_message: str | None = None, ):
        with self._conn.cursor() as cur:
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

    def update_document_stats(self, index_name: str, document_id: str, chunk_count: int, page_count: int,
                              word_count: int):
        with self._conn.cursor() as cur:
            cur.execute(
                """
                UPDATE gogi_documents
                SET chunk_count = %s, page_count = %s, word_count = %s
                WHERE id = %s AND index_id = (SELECT id FROM gogi_indexes WHERE name = %s)
                """,
                (chunk_count, page_count, word_count, document_id, index_name),
            )

    def replace_sections(self, index_name: str, document_id: str, sections) -> None:
        """store the document's sections, replacing any an earlier attempt stored"""
        with self._conn.transaction(), self._conn.cursor() as cur:
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

    def is_wanted(self, job_id: str, index_name: str, document_id: str) -> bool:
        """whether the ingestion job isn't cancelled and its document is still registered; deleting a
        document cancels its job, and deleting an index deletes its documents"""
        with self._conn.cursor() as cur:
            cur.execute(
                """
                SELECT
                    COALESCE((SELECT status <> 'CANCELLED' FROM jobs WHERE id = %s), TRUE)
                    AND EXISTS (
                        SELECT 1 FROM gogi_documents d JOIN gogi_indexes i ON i.id = d.index_id
                        WHERE i.name = %s AND d.id = %s
                    )
                """,
                (job_id, index_name, document_id),
            )
            return cur.fetchone()[0]
