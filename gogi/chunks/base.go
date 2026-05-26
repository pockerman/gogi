package chunks

import (
	"gogi/gogi/documents"
)

// The ChunkStrategy interface defines a method for generating chunks from an extracted document.
// This allows for different chunking strategies to be implemented and used interchangeably.
type ChunkStrategy interface {
	GenerateChunks(
		document documents.ExtractedDocument,
	) []Chunk
}
