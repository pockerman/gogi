package utils

type JobStatus string

const (
	JobPending   JobStatus = "PENDING"
	JobRunning   JobStatus = "RUNNING"
	JobCompleted JobStatus = "COMPLETED"
	JobFailed    JobStatus = "FAILED"
)
