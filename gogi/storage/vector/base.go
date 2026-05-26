package vector

import (
	"gogi/gogi/chunks"
	"gogi/gogi/storage/vector/models"
)

// Interface for vector storage.
// This can be implemented using various backends like in-memory, disk-based, or cloud-based storage.
type VectorStore interface {

	// Insert chunks with embeddings. Returns count inserted.
	Insert(index_name string, document_id string,
		chunks []chunks.Chunk, embeddings [][]float64,
		metadata map[string]string) (int, error)

	// Delete chunks and document metadata for a document. Returns chunks deleted..
	DeleteByDocumentID(index_name string, document_id string) (int, error)

	// Cascade-delete chunks, documents, and index metadata. Returns chunks deleted
	DeleteIndex(index_name string) (int, error)

	// Find chunks most similar to the query embedding.
	Search(query []float64, top_k int, index_name string,
		metadata_filters map[string]string, score_threshold float64) ([]models.VectorSearchResult, error)

	KeywordSearch(index_name string, query string, top_k int,
		metadata_filters map[string]string) ([]models.VectorSearchResult, error)
}
