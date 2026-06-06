package models

// A single result from vector or hybrid search
type VectorSearchResult struct {
	ChunkID    string
	DocumentID string
	Text       string
	Score      float64
	Metadata   map[string]string
}
