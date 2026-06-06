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

	// Create Worker on the GO queue
	w := worker.New(c, WORK_QUEUE, worker.Options{})

	// Register the workflow here
	w.RegisterWorkflow(workflows.IngestDocumentWorkflow)

	log.Infof("Starting Temporal Worker on '%s'...", WORK_QUEUE)
	if err := w.Run(worker.InterruptCh()); err != nil {
		log.Fatalf("Worker failed: %v", err)
	}
}
