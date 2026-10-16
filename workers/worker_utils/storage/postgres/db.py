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
                WHERE id = %s
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

    def get_job(self, job_id: str):
        with self._conn.cursor() as cur:
            cur.execute(
                """
                SELECT *
                FROM jobs
                WHERE id = %s
                """,
                (job_id,),
            )

        return cur.fetchone()