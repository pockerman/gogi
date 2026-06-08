import os
from temporalio.client import Client
from temporalio.worker import Worker

import asyncio

from workers.ingest_document.ingestion_pipeline_job import ingest_document




async def main():

    TEMPORAL_HOST = os.getenv("TEMPORAL_HOST")
    TASK_QUEUE = os.getenv("TASK_QUEUE")

    client = await Client.connect(TEMPORAL_HOST)

    worker = Worker(
        client,
        task_queue=TASK_QUEUE,
        activities=[ingest_document],
    )

    print("Starting Python temporal worker...")
    await worker.run()


if __name__ == "__main__":
    asyncio.run(main())