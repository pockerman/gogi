package postgres

import (
	"context"
	"errors"
	"gogi/gogi/utils"

	"github.com/jackc/pgx/v5"
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
			job_type,
			error_message
		)
		VALUES ($1,$2,$3,$4,$5)
		`,
		job.ID,
		job.DocumentID,
		job.Status,
		job.JobType,
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

// Cancel marks a job CANCELLED unless it has finished; the ingestion worker stops a cancelled job
func (r *JobsRepository) Cancel(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE jobs SET status = $1
		WHERE id = $2 AND status IN ($3, $4)
	`, utils.JobCancelled, id, utils.JobPending, utils.JobRunning)
	return err
}

// Fail marks a job FAILED with the error, unless it has finished or been cancelled
func (r *JobsRepository) Fail(ctx context.Context, id string, message string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE jobs SET status = $1, error_message = $2, completed_at = NOW()
		WHERE id = $3 AND status IN ($4, $5)
	`, utils.JobFailed, message, id, utils.JobPending, utils.JobRunning)
	return err
}

func (r *JobsRepository) GetJob(id string) (Job, error) {

	var job Job

	err := r.pool.QueryRow(
		context.Background(),
		`
		SELECT
			id,
			document_id,
			job_type,
			status,
			error_message,
			created_at,
			started_at,
			completed_at
		FROM jobs
		WHERE id = $1
		`,
		id,
	).Scan(
		&job.ID,
		&job.DocumentID,
		&job.JobType,
		&job.Status,
		&job.ErrorMessage,
		&job.CreatedAt,
		&job.StartedAt,
		&job.CompletedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, utils.ErrJobNotFound
	}

	if err != nil {
		return Job{}, err
	}

	return job, nil
}
