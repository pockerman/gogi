package postgres

import (
	"gogi/gogi/utils"
	"time"
)

type Job struct {
	ID         string
	DocumentID string
	Status     utils.JobStatus
	//WorkerID     string
	JobType      string
	ErrorMessage string

	CreatedAt   time.Time
	StartedAt   *time.Time
	CompletedAt *time.Time
}
