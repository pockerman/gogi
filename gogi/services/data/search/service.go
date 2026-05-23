package search

type RetrievalService interface {
	Search(
		indexName string,
		query string,
		topK int,
	) ([]SearchResult, error)
}
