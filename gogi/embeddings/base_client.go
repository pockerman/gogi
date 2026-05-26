package embeddings

// Optional interface if using a model client
type EmbeddingsModelClient interface {
	Embed(texts []string, model string) [][]float32
}
