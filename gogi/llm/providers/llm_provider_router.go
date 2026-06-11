package providers

import (
	gogiv1 "gogi/gogi/gogi/v1"
	LLM "gogi/gogi/llm"

	"google.golang.org/grpc"
)

type LLMProviderRouter struct {
	providers map[string]ModelProvider
}

func fetchLLMModelConfigFromRequest(req *gogiv1.LLMRunRequest) LLM.LLMModelConfig {
	return LLM.LLMModelConfig{ModelName: req.Config.Model, MaxTokens: int(req.Config.MaxTokens)}
}

func convertLLMMessages(messages []*gogiv1.LLMMessage) []LLM.LLMMessage {
	converted := make([]LLM.LLMMessage, len(messages))
	for i, msg := range messages {
		converted[i] = LLM.LLMMessage{
			Role:    msg.Role,
			Content: msg.Content,
		}
	}
	return converted
}

func (p *LLMProviderRouter) Run(req *gogiv1.LLMRunRequest) (LLM.LLModelResponse, error) {
	return p.providers[req.Config.Provider].Run(convertLLMMessages(req.Messages), fetchLLMModelConfigFromRequest(req))
}

func (p *LLMProviderRouter) RunStream(req *gogiv1.LLMRunRequest,
	stream grpc.ServerStreamingServer[gogiv1.LLMStreamChunkResponse]) error {
	return p.providers[req.Config.Provider].RunStream(convertLLMMessages(req.Messages), fetchLLMModelConfigFromRequest(req)), nil
}
