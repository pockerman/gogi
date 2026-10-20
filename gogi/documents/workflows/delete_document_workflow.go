package workflows

import (
	"time"

	"gogi/gogi/utils"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

type DeleteDocumentWorkflowConfig struct {
	IndexName  string `json:"index_name"`
	DocumentID string `json:"document_id"`
	// the index's collections the document has chunks in, one for each model that embedded it
	Collections []string `json:"collections"`
}

type DeleteDocumentWorkflowResult struct {
	ChunksDeleted int `json:"chunks_deleted"`
}

// DeleteDocumentWorkflow deletes a document's chunks from the vector store, in each of its collections. Like
// SearchDocumentsWorkflow, the gRPC handler blocks on its result.
func DeleteDocumentWorkflow(ctx workflow.Context, config DeleteDocumentWorkflowConfig) (DeleteDocumentWorkflowResult, error) {

	opts := workflow.ActivityOptions{
		// a delete doesn't wait behind ingestions or searches: it runs on the delete worker
		TaskQueue:           utils.GetDeleteDocumentsQueueName(),
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, opts)

	var result DeleteDocumentWorkflowResult
	err := workflow.ExecuteActivity(ctx, "delete_document", config).Get(ctx, &result)
	return result, err
}
