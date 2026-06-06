package models

type JobPayload struct {
	Filename            string            `json:"filename"`
	Content             []byte            `json:"content"`
	CallerMetadata      map[string]string `json:"caller_metadata,omitempty"`
	RequestedDocumentID *string           `json:"requested_document_id,omitempty"`
}
