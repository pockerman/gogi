package ingestion

import "gogi/data/documents"

type IngestionService interface {
	IngestDocument(
		indexName string,
		fileBytes []byte,
		metadata map[string]string,
	) (documents.DocumentMetadata, error)

	IngestStatus(jobId string) (IngestJob, error)
}
