package models

import "time"

type DocumentMetadata struct {
	DocumentID     string            `json:"document_id"`
	IndexName      string            `json:"index_name"`
	Filename       string            `json:"filename"`
	IngestedAt     time.Time         `json:"ingested_at"`
	ChunkCount     int               `json:"chunk_count"`
	PageCount      int               `json:"page_count,omitempty"`
	WordCount      int               `json:"word_count,omitempty"`
	CustomMetadata map[string]string `json:"custom_metadata,omitempty"`
}
