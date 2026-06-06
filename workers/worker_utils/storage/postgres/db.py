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
    progress: float, ):
        with self._conn.cursor() as cur:
            cur.execute(
                """
                UPDATE jobs
                SET
                    status = %s
                WHERE id = %s
                """,
                (status, job_id),
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