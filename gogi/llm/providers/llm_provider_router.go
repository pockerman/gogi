package providers

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	gogiv1 "gogi/gogi/gogi/v1"
	LLM "gogi/gogi/llm"
	"gogi/gogi/utils"

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
	resilience ResilienceConfig
	// breaker stops calling models that keep failing; nil if none is set
	breaker *utils.CircuitBreaker

	// sleep waits between retries and jitter spreads the waits; replaced in tests
	sleep  func(ctx context.Context, d time.Duration) error
	jitter func(d time.Duration) time.Duration
}

func NewLLMProviderRouter(providers map[string]ModelProvider) *LLMProviderRouter {
	return &LLMProviderRouter{
		providers:  providers,
		registered: make(map[string]map[string]ModelProvider),
		resilience: DefaultResilienceConfig(),
		sleep:      sleep,
		jitter:     equalJitter,
	}
}

// SetCircuitBreaker sets the circuit breaker that stops calling models, by
// provider/model, that keep failing; a request then falls back straight away
func (p *LLMProviderRouter) SetCircuitBreaker(breaker *utils.CircuitBreaker) {
	p.breaker = breaker
}

// SetResilienceConfig sets how failed calls are retried and which models they fall back to
func (p *LLMProviderRouter) SetResilienceConfig(config ResilienceConfig) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.resilience = config
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

// requestInput converts and checks the messages and the configuration of a request
func requestInput(req *gogiv1.LLMRunRequest) ([]LLM.LLMMessage, LLM.LLMModelConfig, error) {

	messages, err := convertLLMMessages(req.GetMessages())
	if err != nil {
		return nil, LLM.LLMModelConfig{}, err
	}

	tools, err := convertTools(req.GetTools())
	if err != nil {
		return nil, LLM.LLMModelConfig{}, err
	}

	responseFormat, err := convertResponseFormat(req.GetResponseFormat())
	if err != nil {
		return nil, LLM.LLMModelConfig{}, err
	}

	config := LLM.LLMModelConfig{ModelName: req.GetConfig().GetModel(), MaxTokens: int(req.GetConfig().GetMaxTokens()),
		Temperature: req.GetConfig().GetTemperature(), TopP: req.GetConfig().GetTopP(), Tools: tools,
		ResponseFormat: responseFormat}
	return messages, config, nil
}

// findProvider returns the provider that serves model, resolving models registered
// by another replica, or before a restart, the first time they are asked for
func (p *LLMProviderRouter) findProvider(providerName, model string) (ModelProvider, error) {

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

// Run returns the response of the requested model. Failed calls are retried and, if
// the model keeps failing, the models to fall back to are tried in order; the
// response's provider and model say which model answered
func (p *LLMProviderRouter) Run(ctx context.Context, req *gogiv1.LLMRunRequest) (LLM.LLModelResponse, error) {

	log.Infof("Using provider %s", req.GetConfig().GetProvider())

	messages, config, err := requestInput(req)
	if err != nil {
		return LLM.LLModelResponse{}, err
	}

	var response LLM.LLModelResponse
	err = p.callWithFallback(ctx, req, func(ctx context.Context, provider ModelProvider, model FallbackModel) error {
		modelConfig := config
		modelConfig.ModelName = model.Model

		var err error
		response, err = provider.Run(ctx, messages, modelConfig)
		return err
	}, nil)

	if err != nil {
		log.Infof("An error occurred %s", err)
		return LLM.LLModelResponse{}, err
	}
	return response, nil
}

// RunStream streams the response of the requested model, retrying and falling back
// as Run does, but only until the first chunk is sent: from then on the caller has
// part of the response and an error ends the stream. Every chunk names its provider
func (p *LLMProviderRouter) RunStream(req *gogiv1.LLMRunRequest,
	stream grpc.ServerStreamingServer[gogiv1.LLMStreamChunkResponse]) error {

	messages, config, err := requestInput(req)
	if err != nil {
		return err
	}

	providerStream := &providerChunkStream{ServerStreamingServer: stream}
	return p.callWithFallback(stream.Context(), req, func(ctx context.Context, provider ModelProvider,
		model FallbackModel) error {
		modelConfig := config
		modelConfig.ModelName = model.Model

		providerStream.ctx = ctx
		providerStream.provider = model.Provider
		return provider.RunStream(messages, modelConfig, providerStream)
	}, func() bool { return providerStream.sent })
}

// providerChunkStream sets the provider of the chunks it sends and remembers
// whether it sent any. Its context is the one of the current call to the provider
type providerChunkStream struct {
	grpc.ServerStreamingServer[gogiv1.LLMStreamChunkResponse]
	ctx      context.Context
	provider string
	sent     bool
}

func (s *providerChunkStream) Context() context.Context {
	return s.ctx
}

func (s *providerChunkStream) Send(chunk *gogiv1.LLMStreamChunkResponse) error {
	s.sent = true
	if chunk.Provider == "" {
		chunk.Provider = s.provider
	}
	return s.ServerStreamingServer.Send(chunk)
}
