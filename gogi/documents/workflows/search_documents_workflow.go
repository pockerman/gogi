package workflows

import (
	"time"

	"gogi/gogi/utils"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const SearchDocumentsWorkflowName = "SearchDocumentsWorkflow"

type SearchDocumentsWorkflowConfig struct {
	IndexName string `json:"index_name"`
	Query     string `json:"query"`
	TopK      int    `json:"top_k"`
	// the application whose index it is, and the model, of its, that embeds the query
	Application    string `json:"application"`
	EmbeddingModel string `json:"embedding_model"`
	// the index's collection for the model
	Collection     string            `json:"collection"`
	DocumentIds    []string          `json:"document_ids"`
	MetadataFilter map[string]string `json:"metadata_filter"`
}

type DocumentChunkResult struct {
	ChunkID    string            `json:"chunk_id"`
	DocumentID string            `json:"document_id"`
	IndexName  string            `json:"index_name"`
	Content    string            `json:"content"`
	Score      float64           `json:"score"`
	Metadata   map[string]string `json:"metadata"`
}

type SearchDocumentsWorkflowResult struct {
	Chunks []DocumentChunkResult `json:"chunks"`
}

// SearchDocumentsWorkflow retrieves the chunks most similar to a query. Unlike
// IngestDocumentWorkflow it is driven synchronously: the gRPC handler starts it and
// blocks on its result, since a search has to return an answer to the caller.
func SearchDocumentsWorkflow(ctx workflow.Context, config SearchDocumentsWorkflowConfig) (SearchDocumentsWorkflowResult, error) {

	// a search doesn't wait behind ingestions or deletes: it runs on the search worker
	taskQueueName := utils.GetSearchDocumentsQueueName()

	opts := workflow.ActivityOptions{
		TaskQueue:           taskQueueName,
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, opts)

	var result SearchDocumentsWorkflowResult
	err := workflow.ExecuteActivity(ctx, "search_document", config).Get(ctx, &result)
	return result, err
}
