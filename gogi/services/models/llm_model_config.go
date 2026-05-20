package model_service

type LLMModelConfig struct {
	ModelName   string
	MaxTokens   int
	Temperature float32
	TopP        float32
}
