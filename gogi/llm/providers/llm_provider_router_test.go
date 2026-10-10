package providers

import (
	"errors"
	"fmt"
	gogiv1 "gogi/gogi/gogi/v1"
	"gogi/gogi/llm"
	"testing"

	"google.golang.org/grpc"
)

// fakeProvider answers every request with its name
type fakeProvider struct {
	name string
}

func (p *fakeProvider) Run(messages []llm.LLMMessage, config llm.LLMModelConfig) (llm.LLModelResponse, error) {
	return llm.LLModelResponse{Provider: p.name, Model: config.ModelName}, nil
}

func (p *fakeProvider) RunStream(messages []llm.LLMMessage, config llm.LLMModelConfig,
	stream grpc.ServerStreamingServer[gogiv1.LLMStreamChunkResponse]) error {
	return stream.Send(&gogiv1.LLMStreamChunkResponse{Token: p.name})
}

func (p *fakeProvider) EstimateCost(tokens []string, model string) (float64, error) {
	return 0, nil
}

func runRequest(provider, model string) *gogiv1.LLMRunRequest {
	return &gogiv1.LLMRunRequest{Config: &gogiv1.LLMRunRequestConfig{Provider: provider, Model: model}}
}

func TestRouterBuiltinProvider(t *testing.T) {
	router := NewLLMProviderRouter(map[string]ModelProvider{"openai": &fakeProvider{name: "openai"}})

	response, err := router.Run(runRequest("openai", "any-model"))
	if err != nil || response.Provider != "openai" {
		t.Errorf("wrong response %+v, %v", response, err)
	}
	if !router.IsBuiltinProvider("openai") || router.IsBuiltinProvider("ollama") {
		t.Errorf("wrong built-in providers %v", router.BuiltinProviders())
	}
}

func TestRouterUnknownProvider(t *testing.T) {
	router := NewLLMProviderRouter(map[string]ModelProvider{})

	_, err := router.Run(runRequest("ollama", "llama3.2"))
	if !errors.Is(err, ErrUnknownProvider) {
		t.Errorf("expected ErrUnknownProvider, got %v", err)
	}
}

func TestRouterRegisteredModel(t *testing.T) {
	router := NewLLMProviderRouter(map[string]ModelProvider{})
	router.RegisterModel("ollama", "llama3.2", &fakeProvider{name: "ollama"})

	response, err := router.Run(runRequest("ollama", "llama3.2"))
	if err != nil || response.Provider != "ollama" {
		t.Errorf("wrong response %+v, %v", response, err)
	}

	stream := &fakeChunkStream{}
	if err := router.RunStream(runRequest("ollama", "llama3.2"), stream); err != nil || len(stream.chunks) != 1 {
		t.Errorf("wrong stream %+v, %v", stream.chunks, err)
	}

	// registered models are routed per (provider, model)
	if _, err := router.Run(runRequest("ollama", "mistral")); !errors.Is(err, ErrUnknownProvider) {
		t.Errorf("expected ErrUnknownProvider for an unregistered model, got %v", err)
	}
}

func TestRouterResolver(t *testing.T) {
	router := NewLLMProviderRouter(map[string]ModelProvider{})

	calls := 0
	router.SetResolver(func(provider, model string) (ModelProvider, error) {
		calls++
		if model != "llama3.2" {
			return nil, fmt.Errorf("%w %q", ErrUnknownModel, model)
		}
		return &fakeProvider{name: provider}, nil
	})

	for i := 0; i < 2; i++ {
		response, err := router.Run(runRequest("ollama", "llama3.2"))
		if err != nil || response.Provider != "ollama" {
			t.Errorf("wrong response %+v, %v", response, err)
		}
	}
	if calls != 1 {
		t.Errorf("the resolved provider should be cached, resolver called %d times", calls)
	}

	if _, err := router.Run(runRequest("ollama", "mistral")); !errors.Is(err, ErrUnknownModel) {
		t.Errorf("expected ErrUnknownModel, got %v", err)
	}
}

func TestNewProviderForAdapter(t *testing.T) {
	for _, adapterType := range []string{"openai", "vllm", "tgi", "ollama"} {
		provider, err := NewProviderForAdapter(adapterType, "my-ollama", "http://localhost:11434/v1", nil)
		if err != nil {
			t.Fatal(err)
		}
		compatible, ok := provider.(*OpenAILLMModelProvider)
		if !ok || compatible.Name() != "my-ollama" || compatible.baseURL != "http://localhost:11434/v1" {
			t.Errorf("wrong provider for adapter type %s: %+v", adapterType, provider)
		}
	}

	if _, err := NewProviderForAdapter("bedrock", "bedrock", "http://localhost:8000", nil); err == nil {
		t.Errorf("expected an error for an unsupported adapter type")
	}
}
