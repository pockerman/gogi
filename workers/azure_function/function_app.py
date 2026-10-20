"""
The ingestion worker as an Azure Function, which an Azure Storage queue triggers with the ingestions the documents
service queues (GOGI_INGESTION_QUEUE=azure). A message the function raises for is delivered again; after host.json's
maxDequeueCount deliveries it moves to the poison queue.

Build the app with build.sh: it puts the worker's code and gogi's generated client beside this file.
"""
import azure.functions as func

from workers.ingest_document.ingestion_handler import ingestion_from_env

app = func.FunctionApp()


# the queue is the app setting GOGI_INGESTION_QUEUE_NAME's; the connection, GOGI_INGESTION_QUEUE__queueServiceUri
# and the managed identity's GOGI_INGESTION_QUEUE__credential and __clientId
@app.queue_trigger(arg_name="message", queue_name="%GOGI_INGESTION_QUEUE_NAME%", connection="GOGI_INGESTION_QUEUE")
def ingest_document(message: func.QueueMessage) -> None:
    # Retry propagates: the queue delivers the message again
    ingestion_from_env().handle(message.get_json(), attempt=message.dequeue_count)
