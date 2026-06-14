package providers

import (
	gogiv1 "gogi/gogi/gogi/v1"
	LLM "gogi/gogi/llm"

	"fmt"

	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
)

type LLMProviderRouter struct {
	providers map[string]ModelProvider
}

func NewLLMProviderRouter(providers map[string]ModelProvider) *LLMProviderRouter {
	return &LLMProviderRouter{
		providers: providers,
	}
}

func fetchLLMModelConfigFromRequest(req *gogiv1.LLMRunRequest) LLM.LLMModelConfig {
	return LLM.LLMModelConfig{ModelName: req.Config.Model, MaxTokens: int(req.Config.MaxTokens)}
}

func (p *LLMProviderRouter) chechProvider(req *gogiv1.LLMRunRequest) (ModelProvider, error) {

	provider, ok := p.providers[req.Config.Provider]
	if !ok {
		return nil, fmt.Errorf("unknown provider %q", req.Config.Provider)
	}

	return provider, nil
}

func convertLLMMessages(messages []*gogiv1.LLMMessage) []LLM.LLMMessage {
	converted := make([]LLM.LLMMessage, len(messages))
	for i, msg := range messages {
		converted[i] = LLM.LLMMessage{
			Role:    msg.GetRole(),
			Content: msg.GetContent(),
		}
	}
	return converted
}

func (p *LLMProviderRouter) Run(req *gogiv1.LLMRunRequest) (LLM.LLModelResponse, error) {

	provider, err := p.chechProvider(req)

	log.Infof("Using provider %s", req.Config.Provider)

	if err != nil {
		log.Infof("An error occurred %s", err)
		return LLM.LLModelResponse{}, err
	}

	return provider.Run(convertLLMMessages(req.Messages), fetchLLMModelConfigFromRequest(req))
}

func (p *LLMProviderRouter) RunStream(req *gogiv1.LLMRunRequest,
	stream grpc.ServerStreamingServer[gogiv1.LLMStreamChunkResponse]) error {

	provider, err := p.chechProvider(req)

	if err != nil {
		return err
	}

	provider.RunStream(convertLLMMessages(req.Messages), fetchLLMModelConfigFromRequest(req), stream)
	return nil
}
