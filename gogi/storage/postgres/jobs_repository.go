package postgres

import (
	"context"
	"gogi/gogi/utils"

	"github.com/jackc/pgx/v5/pgxpool"
)

type JobsRepository struct {
	pool *pgxpool.Pool
}

func NewJobsRepository(
	pool *pgxpool.Pool,
) *JobsRepository {

	return &JobsRepository{
		pool: pool,
	}
}

func (r *JobsRepository) Create(
	ctx context.Context,
	job Job,
) error {

	_, err := r.pool.Exec(
		ctx,
		`
		INSERT INTO jobs(
			id,
			document_id,
			status,
			worker_id,
			error_message
		)
		VALUES ($1,$2,$3,$4,$5)
		`,
		job.ID,
		job.DocumentID,
		job.Status,
		job.WorkerID,
		job.ErrorMessage,
	)

	return err
}

func (r *JobsRepository) UpdateStatus(
	ctx context.Context,
	id string,
	status utils.JobStatus,
) error {

	_, err := r.pool.Exec(
		ctx,
		`
		UPDATE jobs
		SET status = $1
		WHERE id = $2
		`,
		status,
		id,
	)

	return err
}
