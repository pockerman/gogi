package impl

import (
	"gogi/gogi/embeddings"
	"gogi/gogi/storage/vector"
)

// The IngestionPipeline struct will handle the end-to-end process of ingesting a document,
// including chunking, embedding, and storing in the vector database.

type IngetionPipeline struct {
	vectorStore        *vector.VectorStore
	embeddingGenerator *embeddings.EmbeddingGenerator
}

func NewIngestionPipeline(vectorStore *vector.VectorStore,
	embeddingGenerator *embeddings.EmbeddingGenerator) *IngetionPipeline {
	return &IngetionPipeline{
		vectorStore:        vectorStore,
		embeddingGenerator: embeddingGenerator,
	}
}

func (p *IngetionPipeline) IngestDocument(indexName string, documentID string,
	documentText string, metadata map[string]string) error {

	// 1. Chunk the document text
	chunks := chunkDocument(documentText)

	// 2. Generate embeddings for the chunks
	embeddings := p.embeddingGenerator.EmbedChunks(chunks, "text-embedding-3-small", 100)

	// 3. Store the chunks and embeddings in the vector database
	_, err := (*p.vectorStore).Insert(indexName, documentID, chunks, embeddings, metadata)
	return err
}
