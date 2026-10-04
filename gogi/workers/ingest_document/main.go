package main

import (
	"gogi/gogi/documents/workflows"

	"os"

	log "github.com/sirupsen/logrus"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

func main() {

	TEMPORAL_HOST := os.Getenv("TEMPORAL_HOST")
	WORK_QUEUE := os.Getenv("WORK_QUEUE")

	// Connect to Temporal
	c, err := client.Dial(client.Options{
		HostPort: TEMPORAL_HOST, //os.Getenv("TEMPORAL_HOST"),
	})
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer c.Close()

	// Create Worker on the GO queue. This worker only hosts the workflow definition; the
	// actual "ingest_document" activity runs on the Python worker polling the same queue.
	// LocalActivityWorkerOnly stops this worker from also polling for (and erroneously
	// claiming, since it has none registered) remote activity tasks meant for Python.
	w := worker.New(c, WORK_QUEUE, worker.Options{
		LocalActivityWorkerOnly: true,
	})

	// Register the workflows here
	w.RegisterWorkflow(workflows.IngestDocumentWorkflow)
	w.RegisterWorkflow(workflows.SearchDocumentsWorkflow)

	log.Infof("Starting Temporal Worker on '%s'...", WORK_QUEUE)
	if err := w.Run(worker.InterruptCh()); err != nil {
		log.Fatalf("Worker failed: %v", err)
	}
}
