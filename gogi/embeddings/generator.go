package embeddings

import (
	"errors"

	"gogi/gogi/chunks"
)

// Equivalent to:
// Callable[[List[str], str], List[List[float]]]
type EmbedFn func(texts []string, model string) [][]float32

// The EmbeddingsGenerator for generating embeddings from chunks of text.
type EmbeddingGenerator struct {
	embedFn EmbedFn
}

// Constructor
func NewEmbeddingGenerator(
	embedFn EmbedFn,
	modelClient EmbeddingsModelClient,
) (*EmbeddingGenerator, error) {

	switch {
	case embedFn != nil:
		return &EmbeddingGenerator{
			embedFn: embedFn,
		}, nil

	case modelClient != nil:
		return &EmbeddingGenerator{
			embedFn: modelClient.Embed,
		}, nil

	default:
		return nil, errors.New(
			"provide either embedFn or modelClient",
		)
	}
}

// Embed document chunks
func (e *EmbeddingGenerator) EmbedChunks(
	chunks []chunks.Chunk,
	model string,
	batchSize int,
) [][]float32 {

	if batchSize <= 0 {
		batchSize = 100
	}

	texts := make([]string, 0, len(chunks))

	for _, chunk := range chunks {
		texts = append(texts, chunk.Text)
	}

	var allEmbeddings [][]float32

	for i := 0; i < len(texts); i += batchSize {

		end := i + batchSize

		if end > len(texts) {
			end = len(texts)
		}

		batch := texts[i:end]

		embeddings := e.embedFn(batch, model)

		allEmbeddings = append(allEmbeddings, embeddings...)
	}

	return allEmbeddings
}

// Embed a single query
func (e *EmbeddingGenerator) EmbedQuery(
	query string,
	model string,
) []float32 {

	results := e.embedFn([]string{query}, model)

	if len(results) == 0 {
		return nil
	}

	return results[0]
}
