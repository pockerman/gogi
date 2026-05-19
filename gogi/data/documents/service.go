package documents

type DocumentService interface {
	ListDocuments(indexName string) ([]DocumentMetadata, error)

	GetDocument(
		indexName string,
		documentID string,
	) (DocumentMetadata, error)

	DeleteDocument(
		indexName string,
		documentID string,
	) error
}
