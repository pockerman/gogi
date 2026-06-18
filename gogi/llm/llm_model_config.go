package llm

type LLMModelConfig struct {
	ModelName   string
	MaxTokens   int
	Temperature float32
	TopP        float32
}
