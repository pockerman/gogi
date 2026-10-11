package providers

import (
	"fmt"
	"sort"
)

// DefaultAdapterType is the adapter type of a registered model that does not name one
const DefaultAdapterType = "openai"

// ModelProviderAdapter creates a provider that talks to the model server at endpoint
// and reports itself as providerName. apiKey is nil if the endpoint needs no credentials
type ModelProviderAdapter func(providerName, endpoint string, apiKey APIKeySource) ModelProvider

func openAICompatibleAdapter(providerName, endpoint string, apiKey APIKeySource) ModelProvider {
	provider := NewOpenAICompatibleLLMModelProvider(providerName, "", endpoint)
	if apiKey != nil {
		provider.SetAPIKeySource(apiKey)
	}
	return provider
}

// adapters are the adapter types a registered model can use. Self-hosted inference
// servers such as vLLM, TGI and Ollama expose an OpenAI-compatible API, so they
// all use the OpenAI adapter; their names are accepted for readability
var adapters = map[string]ModelProviderAdapter{
	"openai": openAICompatibleAdapter,
	"vllm":   openAICompatibleAdapter,
	"tgi":    openAICompatibleAdapter,
	"ollama": openAICompatibleAdapter,
}

// SupportedAdapterTypes returns the adapter types a registered model can use
func SupportedAdapterTypes() []string {
	types := make([]string, 0, len(adapters))
	for adapterType := range adapters {
		types = append(types, adapterType)
	}
	sort.Strings(types)
	return types
}

// NewProviderForAdapter creates the provider for a model registered with the given
// adapter type, served at endpoint. apiKey is nil if the endpoint needs no credentials
func NewProviderForAdapter(adapterType, providerName, endpoint string, apiKey APIKeySource) (ModelProvider, error) {
	adapter, ok := adapters[adapterType]
	if !ok {
		return nil, fmt.Errorf("unsupported adapter type %q, supported adapter types are %v",
			adapterType, SupportedAdapterTypes())
	}
	return adapter(providerName, endpoint, apiKey), nil
}
