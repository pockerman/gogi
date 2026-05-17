package utils

import "gogenai/gogenai/model_service"

type TracedServiceInterface interface {

	// Run processes a list of LLMMessage inputs and returns a complete response.
	// This method is synchronous and blocks until the full response is available.
	TraceOperation() model_service.LLModelResponse

	// RunStream processes a list of LLMMessage inputs and returns a streamed response.
	// The response may be delivered incrementally depending on the implementation.
	TraceGeneration() model_service.LLModelResponse

	// EstimateCost estimate the cost of generating  a response
	// for the given tokens using the given model
	EstimateCost(tokens []string, model string) float64
}
