package providers

import (
	"context"
	gogiv1 "gogi/gogi/gogi/v1"
	"gogi/gogi/llm"

	"google.golang.org/grpc"
)

// ModelProvider defines an abstraction for interacting with a language model.
// Implementations may call external APIs or local models.
type ModelProvider interface {

	// Run processes a list of LLMMessage inputs and returns a complete response.
	// This method is synchronous and blocks until the full response is available,
	// or ctx is done.
	Run(ctx context.Context, messages []llm.LLMMessage, config llm.LLMModelConfig) (llm.LLModelResponse, error)

	// RunStream processes a list of LLMMessage inputs and returns a streamed response.
	// The response may be delivered incrementally depending on the implementation.
	// The call ends when the stream's context is done.
	RunStream(
		messages []llm.LLMMessage,
		config llm.LLMModelConfig,
		stream grpc.ServerStreamingServer[gogiv1.LLMStreamChunkResponse],
	) error

	// EstimateCost estimate the cost of generating  a response
	// for the given tokens using the given model
	EstimateCost(tokens []string, model string) (float64, error)
}
