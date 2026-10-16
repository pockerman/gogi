import os
from concurrent.futures import ThreadPoolExecutor
from temporalio.client import Client
from temporalio.worker import Worker

import asyncio

from workers.ingest_document.ingestion_pipeline_job import ingest_document, search_document, delete_document




async def main():

    TEMPORAL_HOST = os.getenv("TEMPORAL_HOST")
    TASK_QUEUE = os.getenv("TASK_QUEUE")

    client = await Client.connect(TEMPORAL_HOST)

    # The activities are synchronous: parsing and embedding block. Each runs in a thread of its
    # own, so a long ingestion doesn't hold up searches and deletes.
    max_concurrent = int(os.getenv("MAX_CONCURRENT_ACTIVITIES", "8"))
    worker = Worker(
        client,
        task_queue=TASK_QUEUE,
        activities=[ingest_document, search_document, delete_document],
        activity_executor=ThreadPoolExecutor(max_workers=max_concurrent),
        max_concurrent_activities=max_concurrent,
    )

    print("Starting Python temporal worker...")
    await worker.run()


if __name__ == "__main__":
    asyncio.run(main())