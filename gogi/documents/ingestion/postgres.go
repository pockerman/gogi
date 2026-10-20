package ingestion

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresQueue keeps the messages in the table gogi_ingestion_queue, which the worker polls: it takes
// a message by hiding it for a while, and deletes it once it's done with it, so a message a worker
// failed on, or died holding, is taken again
type PostgresQueue struct {
	pool *pgxpool.Pool
}

func NewPostgresQueue(pool *pgxpool.Pool) *PostgresQueue {
	return &PostgresQueue{pool: pool}
}

func (q *PostgresQueue) Enqueue(ctx context.Context, message Message) error {
	encoded, err := json.Marshal(message)
	if err != nil {
		return err
	}
	_, err = q.pool.Exec(ctx, `INSERT INTO gogi_ingestion_queue (job_id, message) VALUES ($1, $2)`,
		message.JobID, encoded)
	return err
}
