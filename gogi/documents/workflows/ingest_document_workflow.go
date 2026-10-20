package workflows

import (
	"time"

	"gogi/gogi/utils"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const IngestDocumentWorkflowName = "IngestDocumentWorkflow"

type IngestDocumentWorkflowConfig struct {
	JobId      string `json:"job_id"`
	IndexName  string `json:"index_name"`
	Filename   string `json:"filename"`
	DocumentID string `json:"document_id"`
	// the application whose document it is: the models are its own, and called with its keys
	Application string `json:"application"`
	// the document's content is in the object store; workflow inputs are limited to a few MB
	Bucket        string `json:"bucket"`
	ObjectKey     string `json:"object_key"`
	ChunkStrategy string `json:"chunk_strategy"`
	// the models that embed the document, each into a collection of its own
	EmbeddingModels []EmbeddingModelConfig `json:"embedding_models"`
	BatchSize       int                    `json:"batch_size"`
	Metadata        map[string]string      `json:"metadata"`
}

// EmbeddingModelConfig is how a document is chunked for a model, and where its chunks are kept
type EmbeddingModelConfig struct {
	// as the application named it
	Model      string `json:"model"`
	Collection string `json:"collection"`
	// in words, so a chunk is no longer than the model reads
	ChunkSize    int `json:"chunk_size"`
	ChunkOverlap int `json:"chunk_overlap"`
}

// IngestDocumentWorkflow is the workflow definition.
// It runs inside the Temporal Worker, NOT the gRPC server.
func IngestDocumentWorkflow(ctx workflow.Context, config IngestDocumentWorkflowConfig) error {

	taskQueueName := utils.GetIngestionDocumentQueueName()

	// Configure options to route to the Python worker's queue
	opts := workflow.ActivityOptions{
		TaskQueue:           taskQueueName,    // Matches Python worker
		StartToCloseTimeout: 10 * time.Minute, // Adjust based on document size
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3, // bounded so a persistently failing job doesn't retry forever
		},
	}
	ctx = workflow.WithActivityOptions(ctx, opts)

	// Execute the activity defined in your Python worker
	// The name "ingest_document" must match the @activity.defn(name="ingest_document") in Python
	err := workflow.ExecuteActivity(ctx, "ingest_document", config).Get(ctx, nil)
	return err
}
