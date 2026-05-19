package model_providers

import "gogenai/model_service"

// ModelProvider defines an abstraction for interacting with a language model.
// Implementations may call external APIs or local models.
type ModelProvider interface {

	// Run processes a list of LLMMessage inputs and returns a complete response.
	// This method is synchronous and blocks until the full response is available.
	Run(messages []model_service.LLMMessage, config model_service.LLMModelConfig) model_service.LLModelResponse

	// RunStream processes a list of LLMMessage inputs and returns a streamed response.
	// The response may be delivered incrementally depending on the implementation.
	RunStream(messages []model_service.LLMMessage, config model_service.LLMModelConfig) model_service.LLModelResponse

	// EstimateCost estimate the cost of generating  a response
	// for the given tokens using the given model
	EstimateCost(tokens []string, model string) float64
}
