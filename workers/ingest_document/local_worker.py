"""
Runs the ingestion worker as a process that polls gogi's ingestion queue table in Postgres: in development, or where
gogi runs without cloud functions (GOGI_INGESTION_QUEUE=postgres). Each ingestion runs in a thread of its own, so a
long one doesn't hold up the others.
"""
import os
import threading
import time
from concurrent.futures import ThreadPoolExecutor

from loguru import logger

from workers.ingest_document.ingestion_handler import Retry, ingestion_from_env
from workers.worker_utils.storage.postgres.ingestion_queue import PostgresIngestionQueue, QueuedIngestion

# how long the worker waits when no ingestion is queued
POLL_SECONDS = float(os.getenv("GOGI_INGESTION_POLL_SECONDS", "1"))
# how long a failed ingestion waits before it's tried again: longer each time
RETRY_SECONDS = int(os.getenv("GOGI_INGESTION_RETRY_SECONDS", "10"))


def process(queue: PostgresIngestionQueue, queued: QueuedIngestion, slots: threading.Semaphore):
    try:
        ingestion_from_env().handle(queued.message, attempt=queued.attempt,
                                    heartbeat=lambda: queue.extend(queued.job_id))
        queue.delete(queued.job_id)
    except Retry:
        queue.retry_later(queued.job_id, RETRY_SECONDS * 2 ** (queued.attempt - 1))
    except Exception:
        # e.g. the database went away: the message shows again when its time is up
        logger.exception(f"Job {queued.job_id} could not be handled")
    finally:
        slots.release()


def main():
    queue = PostgresIngestionQueue(host=os.getenv("DB_HOST"), port=os.getenv("DB_PORT"),
                                   database=os.getenv("DB_NAME"), user=os.getenv("DB_USER"),
                                   password=os.getenv("DB_PASSWORD"))
    concurrency = int(os.getenv("MAX_CONCURRENT_INGESTIONS", "4"))
    slots = threading.Semaphore(concurrency)
    logger.info(f"Ingesting documents from the Postgres queue, {concurrency} at a time")
    with ThreadPoolExecutor(max_workers=concurrency) as executor:
        while True:
            slots.acquire()
            try:
                queued = queue.take()
            except Exception:
                logger.exception("Could not read the ingestion queue")
                queued = None
            if queued is None:
                slots.release()
                time.sleep(POLL_SECONDS)
                continue
            executor.submit(process, queue, queued, slots)


if __name__ == "__main__":
    main()
