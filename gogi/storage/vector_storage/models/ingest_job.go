package models

type IngestJob struct {
	JobId        string
	Status       string
	DocumentId   string
	Progress     float32
	ErrorMessage string
}
