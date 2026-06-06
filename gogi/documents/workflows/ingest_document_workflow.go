package workflows

import (
	"time"

	"gogi/gogi/utils"

	"go.temporal.io/sdk/workflow"
)

const IngestDocumentWorkflowName = "IngestDocumentWorkflow"

type IngestDocumentWorkflowConfig struct {
	JobId            string            `json:"job_id"`
	IndexName        string            `json:"index_name"`
	Filename         string            `json:"filename"`
	DocumentID       string            `json:"document_id"`
	Content          []byte            `json:"content"` // Base64 encoded automatically
	ChunkStrategy    string            `json:"chunk_strategy"`
	EmbeddingsModel  string            `json:"embeddings_model"`
	EmbeddingsClient string            `json:"embeddings_client"`
	BatchSize        int               `json:"batch_size"`
	Metadata         map[string]string `json:"metadata"`
}

// IngestDocumentWorkflow is the workflow definition.
// It runs inside the Temporal Worker, NOT the gRPC server.
func IngestDocumentWorkflow(ctx workflow.Context, config IngestDocumentWorkflowConfig) error {

	taskQueueName := utils.GetIngestionDocumentQueueName()

	// Configure options to route to the Python worker's queue
	opts := workflow.ActivityOptions{
		TaskQueue:           taskQueueName,    // Matches Python worker
		StartToCloseTimeout: 10 * time.Minute, // Adjust based on document size
		// RetryPolicy can be added here for resilience
	}
	ctx = workflow.WithActivityOptions(ctx, opts)

	// Execute the activity defined in your Python worker
	// The name "ingest_document" must match the @activity.defn(name="ingest_document") in Python
	err := workflow.ExecuteActivity(ctx, "ingest_document", config).Get(ctx, nil)
	return err
}
