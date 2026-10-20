// Package ingestion hands documents to the ingestion worker. Each ingestion is a message on a queue: the
// worker reads the document from the object store, parses it, chunks it for each embedding model, has the
// model service embed the chunks and stores them in the index's collection for the model, and reports on
// the ingestion's job in Postgres. The worker is a function the queue triggers in the cloud (an AWS Lambda
// fed by SQS, an Azure Function fed by a Storage Queue), and a process polling a queue table in Postgres
// elsewhere, e.g. in development. A message the worker fails on is delivered again, a few times at most.
package ingestion

import (
	"context"
	"fmt"

	"gogi/gogi/utils"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Message asks the worker to ingest a document; its JSON is what the worker reads
type Message struct {
	JobID      string `json:"job_id"`
	IndexName  string `json:"index_name"`
	Filename   string `json:"filename"`
	DocumentID string `json:"document_id"`
	// the application whose document it is: the models are its own, and called with its keys
	Application string `json:"application"`
	// the document's content is in the object store; queue messages are limited to a few hundred KB
	Bucket        string `json:"bucket"`
	ObjectKey     string `json:"object_key"`
	ChunkStrategy string `json:"chunk_strategy"`
	// the models that embed the document, each into a collection of its own
	EmbeddingModels []EmbeddingModel  `json:"embedding_models"`
	BatchSize       int               `json:"batch_size"`
	Metadata        map[string]string `json:"metadata"`
}

// EmbeddingModel is how a document is chunked for a model, and where its chunks are kept
type EmbeddingModel struct {
	// as the application named it
	Model      string `json:"model"`
	Collection string `json:"collection"`
	// in words, so a chunk is no longer than the model reads
	ChunkSize    int `json:"chunk_size"`
	ChunkOverlap int `json:"chunk_overlap"`
}

// Queue hands ingestions to the worker
type Queue interface {
	Enqueue(ctx context.Context, message Message) error
}

// FromEnv is the queue GOGI_INGESTION_QUEUE names: "postgres" (the default), "sqs" or "azure"
func FromEnv(ctx context.Context, pool *pgxpool.Pool) (Queue, error) {
	switch kind := utils.GetEnv("GOGI_INGESTION_QUEUE", "postgres"); kind {
	case "postgres":
		return NewPostgresQueue(pool), nil
	case "sqs":
		return NewSQSQueue(ctx, utils.GetEnv("GOGI_SQS_QUEUE_URL", ""))
	case "azure":
		return NewAzureQueue(utils.GetEnv("GOGI_AZURE_QUEUE_URL", ""))
	default:
		return nil, fmt.Errorf("GOGI_INGESTION_QUEUE is postgres, sqs or azure, not %q", kind)
	}
}
