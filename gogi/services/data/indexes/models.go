package indexes

import "time"

// IndexConfig defines the configuration used to create and manage
// a vector index for document embeddings and retrieval pipelines.
//
// The configuration controls:
//   - The index identity
//   - Which embedding model is used
//   - Embedding vector dimensions
//   - Document chunking behavior
//   - Optional metadata schema attached to indexed documents
//
// This type is useful in RAG (Retrieval-Augmented Generation)
// systems where documents are split into chunks, embedded into vectors,
// and stored in a vector database for semantic search.
type IndexConfig struct {

	// Name is the unique identifier of the index.
	//
	// Example:
	//   "customer-support-index"
	Name string

	// EmbeddingModel specifies the embedding model used
	// to generate vector representations for text chunks.
	//
	// Example:
	//   "text-embedding-3-small"
	EmbeddingModel string

	// EmbeddingDimensions defines the dimensionality of
	// the embedding vectors produced by the embedding model.
	//
	// This value must match the output dimensions of the
	// configured embedding model.
	//
	// Example:
	//   1536
	EmbeddingDimensions int32

	// chunkingStrategy specifies how documents are split
	// into chunks before embedding generation.
	//
	// Common strategies include:
	//   - "fixed"
	//   - "sentence"
	//   - "recursive"
	//
	// This field is unexported and accessible only within
	// the package.
	ChunkingStrategy string

	// chunkSize defines the maximum size of each generated
	// document chunk.
	//
	// Depending on the chunking strategy, this may represent
	// characters, tokens, or words.
	//
	// Example:
	//   512
	ChunkSize int32

	// chunkOverlap defines how much overlap exists between
	// consecutive chunks.
	//
	// Overlapping chunks help preserve semantic continuity
	// across chunk boundaries.
	//
	// Example:
	//   50
	ChunkOverlap int32

	// metadataSchema defines the metadata fields associated
	// with indexed documents.
	//
	// The schema is represented as a flexible key-value map,
	// allowing arbitrary metadata definitions.
	//
	// Example:
	//   map[string]any{
	//       "author": "string",
	//       "created_at": "datetime",
	//   }
	//
	// This field is unexported and accessible only within
	// the package.
	MetadataSchema map[string]any
}

type Index struct {
	Name           string
	Config         IndexConfig
	Owner          string
	DocumentCount  int32
	TotalChunks    int32
	CreatedAt      time.Time
	LastIngestedAt time.Time
}
