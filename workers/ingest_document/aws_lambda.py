"""
The ingestion worker as an AWS Lambda, which SQS triggers with the ingestions the documents service queues
(GOGI_INGESTION_QUEUE=sqs). The event source mapping reports batch item failures, so only the messages that failed
are delivered again, after the queue's visibility timeout; after maxReceiveCount deliveries SQS moves a message to
the dead-letter queue.
"""
import json

from loguru import logger

from workers.ingest_document.ingestion_handler import Retry, ingestion_from_env


def handler(event, context):
    failures = []
    for record in event.get("Records", []):
        try:
            message = json.loads(record["body"])
        except json.JSONDecodeError:
            logger.error(f"Dropped message {record.get('messageId')}: its body is not JSON")
            continue
        try:
            attempt = int(record.get("attributes", {}).get("ApproximateReceiveCount", "1"))
            ingestion_from_env().handle(message, attempt=attempt)
        except Retry:
            failures.append({"itemIdentifier": record["messageId"]})
    return {"batchItemFailures": failures}
