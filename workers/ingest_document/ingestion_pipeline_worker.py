import os
from concurrent.futures import ThreadPoolExecutor
from temporalio.client import Client
from temporalio.worker import Worker

import asyncio

from workers.ingest_document.ingestion_pipeline_job import ingest_document, search_document, delete_document

ACTIVITIES = {
    "ingest_document": ingest_document,
    "search_document": search_document,
    "delete_document": delete_document,
}


async def main():

    TEMPORAL_HOST = os.getenv("TEMPORAL_HOST")
    TASK_QUEUE = os.getenv("TASK_QUEUE")
    # The same image runs as two workers: one ingests documents, on the ingestion queue; the other
    # searches and deletes them, on the documents queue, so long ingestions don't hold up either
    names = [name.strip() for name in os.getenv("ACTIVITIES", ",".join(ACTIVITIES)).split(",") if name.strip()]
    unknown = set(names) - set(ACTIVITIES)
    if unknown:
        raise SystemExit(f"unknown activities in ACTIVITIES: {sorted(unknown)}")

    client = await Client.connect(TEMPORAL_HOST)

    # The activities are synchronous: parsing and embedding block. Each runs in a thread of its
    # own, so one activity doesn't hold up the others.
    max_concurrent = int(os.getenv("MAX_CONCURRENT_ACTIVITIES", "8"))
    worker = Worker(
        client,
        task_queue=TASK_QUEUE,
        activities=[ACTIVITIES[name] for name in names],
        activity_executor=ThreadPoolExecutor(max_workers=max_concurrent),
        max_concurrent_activities=max_concurrent,
    )

    print(f"Starting Python temporal worker on {TASK_QUEUE} for {names}...")
    await worker.run()


if __name__ == "__main__":
    asyncio.run(main())
