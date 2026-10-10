package providers

import (
	"errors"
	"sort"
	"sync"

	gogiv1 "gogi/gogi/gogi/v1"
	LLM "gogi/gogi/llm"

	"fmt"

	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
)

var (
	// ErrUnknownProvider is returned when a request names a provider the router cannot route to
	ErrUnknownProvider = errors.New("unknown provider")
	// ErrUnknownModel is returned when a request names a model that is not registered
	ErrUnknownModel = errors.New("unknown model")
)

// ProviderResolver returns the provider for a registered (provider, model) pair the
// router has not seen yet, e.g. a model registered by another replica or before a
// restart. It returns an error wrapping ErrUnknownModel if the model is not registered
type ProviderResolver func(provider, model string) (ModelProvider, error)

// LLMProviderRouter routes requests to the built-in providers, which serve any of
// their models, and to registered models, which are routed per (provider, model)
type LLMProviderRouter struct {
	providers map[string]ModelProvider

	mu         sync.RWMutex
	registered map[string]map[string]ModelProvider
	resolver   ProviderResolver
}

func NewLLMProviderRouter(providers map[string]ModelProvider) *LLMProviderRouter {
	return &LLMProviderRouter{
		providers:  providers,
		registered: make(map[string]map[string]ModelProvider),
	}
}

// SetResolver sets the resolver used for registered models the router has not seen yet
func (p *LLMProviderRouter) SetResolver(resolver ProviderResolver) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.resolver = resolver
}

// IsBuiltinProvider returns true if name is one of the built-in providers
func (p *LLMProviderRouter) IsBuiltinProvider(name string) bool {
	_, ok := p.providers[name]
	return ok
}

// BuiltinProviders returns the names of the built-in providers, sorted
func (p *LLMProviderRouter) BuiltinProviders() []string {
	names := make([]string, 0, len(p.providers))
	for name := range p.providers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// RegisterModel routes requests for the given provider and model to modelProvider,
// replacing any previous registration of the pair
func (p *LLMProviderRouter) RegisterModel(provider, model string, modelProvider ModelProvider) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, ok := p.registered[provider]; !ok {
		p.registered[provider] = make(map[string]ModelProvider)
	}
	p.registered[provider][model] = modelProvider
}

func fetchLLMModelConfigFromRequest(req *gogiv1.LLMRunRequest) LLM.LLMModelConfig {
	return LLM.LLMModelConfig{ModelName: req.Config.Model, MaxTokens: int(req.Config.MaxTokens),
		Temperature: req.Config.Temperature, TopP: req.Config.TopP}
}

func (p *LLMProviderRouter) findProvider(req *gogiv1.LLMRunRequest) (ModelProvider, error) {

	providerName := req.GetConfig().GetProvider()
	model := req.GetConfig().GetModel()

	if provider, ok := p.providers[providerName]; ok {
		return provider, nil
	}

	p.mu.RLock()
	provider, ok := p.registered[providerName][model]
	resolver := p.resolver
	p.mu.RUnlock()

	if ok {
		return provider, nil
	}

	if resolver == nil {
		return nil, fmt.Errorf("%w %q", ErrUnknownProvider, providerName)
	}

	provider, err := resolver(providerName, model)
	if err != nil {
		return nil, err
	}

	p.RegisterModel(providerName, model, provider)
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

	provider, err := p.findProvider(req)

	log.Infof("Using provider %s", req.GetConfig().GetProvider())

	if err != nil {
		log.Infof("An error occurred %s", err)
		return LLM.LLModelResponse{}, err
	}

	return provider.Run(convertLLMMessages(req.Messages), fetchLLMModelConfigFromRequest(req))
}

func (p *LLMProviderRouter) RunStream(req *gogiv1.LLMRunRequest,
	stream grpc.ServerStreamingServer[gogiv1.LLMStreamChunkResponse]) error {

	provider, err := p.findProvider(req)

	if err != nil {
		return err
	}

	return provider.RunStream(convertLLMMessages(req.Messages), fetchLLMModelConfigFromRequest(req), stream)
}
