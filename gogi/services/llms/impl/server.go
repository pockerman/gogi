package impl

import (
	"context"
	"errors"
	"fmt"
	"gogi/gogi/credentials"
	gogiv1 "gogi/gogi/gogi/v1"
	LLM "gogi/gogi/llm"
	"gogi/gogi/llm/providers"
	"gogi/gogi/utils"
	"net/http"
	"net/url"
	"sort"
	"time"

	"gogi/gogi/storage/postgres"
	"gogi/gogi/storage/vector_storage"

	"github.com/jackc/pgx/v5/pgxpool"
	Log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// LLMServer implements the LLMModelService server.
// The server provides the following endpoints
//  Core inference
//  Run(LLMRunRequest) returns (LLMRunResponse);
//  RunStream(LLMRunRequest) returns (stream LLMStreamChunkResponse);

//  // Discovery
//  ListLLMs(ListLLMsRequest) returns (ListLLMsResponse);
//  GetLLMCapabilities(GetLLMCapabilitiesRequest) returns (LLMCapabilitiesResponse);
//  GetLLMProviders(GetLLMProvidersRequest) returns (LLMProvidersResponse);

//  // Custom model registry
//  RegisterLLM(RegisterLLMRequest) returns (RegisterLLMResponse);

//	// Get all the user registered models
//	ListRegisteredLLMs(ListRegisteredLLMsRequest) returns (ListRegisteredLLMsResponse);
//	GetLLMStatus(GetLLMStatusRequest) returns (LLMStatusResponse);
//

const (
	// LLMStatusProvisioning is the status of a registered model whose health has not been checked yet
	LLMStatusProvisioning = "provisioning"
	// LLMStatusHealthy is the status of a registered model whose last health check succeeded
	LLMStatusHealthy = "healthy"
	// LLMStatusUnhealthy is the status of a registered model whose last health check failed
	LLMStatusUnhealthy = "unhealthy"

	// DefaultRegisteredLLMProvider is the provider of a registered model that does not name one
	DefaultRegisteredLLMProvider = "custom"

	healthCheckTimeout = 5 * time.Second
)

// RegisteredLLMStore stores the models registered with the platform
type RegisteredLLMStore interface {
	UpsertRegisteredLLM(ctx context.Context, model *postgres.GogiRegisteredLLM) (*postgres.GogiRegisteredLLM, error)
	GetRegisteredLLMByName(ctx context.Context, name string) (*postgres.GogiRegisteredLLM, error)
	ListRegisteredLLMs(ctx context.Context) ([]*postgres.GogiRegisteredLLM, error)
	UpdateRegisteredLLMStatus(ctx context.Context, name, status string, checkedAt time.Time) error
}

func toGRPCLLMRunResponse(llmResponse LLM.LLModelResponse) *gogiv1.LLMRunResponse {

	toolCalls := make([]*gogiv1.ToolCall, 0, len(llmResponse.ToolCalls))

	for _, tc := range llmResponse.ToolCalls {
		toolCalls = append(toolCalls, &gogiv1.ToolCall{
			Id:   tc.Id,
			Type: tc.ToolType,
			Function: &gogiv1.ToolCallFunction{
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			},
		})
	}

	return &gogiv1.LLMRunResponse{
		Content:      llmResponse.Content,
		Model:        llmResponse.Model,
		Provider:     llmResponse.Provider,
		FinishReason: llmResponse.FinishReason,
		Usage: &gogiv1.TokenUsage{
			PromptTokens:     int32(llmResponse.TokenUsage.PromptTokens),
			CompletionTokens: int32(llmResponse.TokenUsage.CompletionTokens),
			TotalTokens:      int32(llmResponse.TokenUsage.TotalTokens),
		},
		ToolCalls: toolCalls,
	}
}

func toModelInfo(model *postgres.GogiRegisteredLLM) *gogiv1.ModelInfo {
	return &gogiv1.ModelInfo{
		Name:     model.Name,
		Provider: model.Provider,
		Capabilities: &gogiv1.LLMCapabilities{
			ContextWindow:     model.ContextWindow,
			SupportsVision:    model.SupportsVision,
			SupportsTools:     model.SupportsTools,
			SupportsStreaming: model.SupportsStreaming,
			SupportsJsonMode:  model.SupportsJSONMode,
		},
	}
}

// runError maps a provider router error to a gRPC status error
func runError(err error) error {
	if errors.Is(err, providers.ErrUnknownProvider) || errors.Is(err, providers.ErrUnknownModel) {
		return status.Errorf(codes.InvalidArgument, "%v", err)
	}
	if _, ok := status.FromError(err); ok {
		return err
	}
	return status.Errorf(codes.Internal, "%v", err)
}

type LLMModelServer struct {
	gogiv1.UnimplementedLLMModelServerServer
	chromaDBClient *vector_storage.ChromaDBClient
	gogiIndexRepo  postgres.GogiIndexRepository
	providerRouter *providers.LLMProviderRouter
	registeredLLMs RegisteredLLMStore
	// credentialStore holds the credentials of registered models; nil if none is configured
	credentialStore credentials.CredentialStore
	healthClient    *http.Client
}

func NewLLMModelServer(chromaDBClient *vector_storage.ChromaDBClient, dbClient *pgxpool.Pool,
	providerRouter *providers.LLMProviderRouter, credentialStore credentials.CredentialStore) *LLMModelServer {
	return newLLMModelServer(providerRouter, postgres.NewGogiRegisteredLLMsRepository(dbClient), credentialStore,
		chromaDBClient, *postgres.NewGogiIndexesRepository(dbClient))
}

func newLLMModelServer(providerRouter *providers.LLMProviderRouter, registeredLLMs RegisteredLLMStore,
	credentialStore credentials.CredentialStore, chromaDBClient *vector_storage.ChromaDBClient,
	gogiIndexRepo postgres.GogiIndexRepository) *LLMModelServer {

	server := &LLMModelServer{
		chromaDBClient:  chromaDBClient,
		gogiIndexRepo:   gogiIndexRepo,
		providerRouter:  providerRouter,
		registeredLLMs:  registeredLLMs,
		credentialStore: credentialStore,
		healthClient:    &http.Client{Timeout: healthCheckTimeout},
	}

	// models registered by another replica, or before a restart, are
	// looked up in the store the first time a request names them
	providerRouter.SetResolver(server.resolveRegisteredModel)
	return server
}

// resolveRegisteredModel returns the provider of a registered model
func (s *LLMModelServer) resolveRegisteredModel(provider, model string) (providers.ModelProvider, error) {

	registered, err := s.registeredLLMs.GetRegisteredLLMByName(context.Background(), model)
	if errors.Is(err, utils.ErrRegisteredLLMNotFound) || (err == nil && registered.Provider != provider) {
		return nil, fmt.Errorf("%w %q for provider %q", providers.ErrUnknownModel, model, provider)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to look up registered model %q: %w", model, err)
	}

	return providers.NewProviderForAdapter(registered.AdapterType, registered.Provider, registered.Endpoint,
		s.apiKeySource(registered.CredentialRef))
}

// apiKeySource returns the API key source of a model registered with the given
// credential reference, or nil if the model needs no credentials. The credential is
// read from the store for every request, so the secret is never kept with the
// registration and a rotated secret is used without re-registering the model
func (s *LLMModelServer) apiKeySource(credentialRef string) providers.APIKeySource {
	if credentialRef == "" {
		return nil
	}

	return func() (string, error) {
		if s.credentialStore == nil {
			return "", fmt.Errorf("credential %q is needed but no credential store is configured", credentialRef)
		}
		credential, err := s.credentialStore.Retrieve(context.Background(), credentialRef)
		if err != nil {
			return "", err
		}
		return credential.Value, nil
	}
}

// checkCredential checks that the credential store holds the credential
func (s *LLMModelServer) checkCredential(ctx context.Context, credentialRef string) error {
	if credentialRef == "" {
		return nil
	}
	if s.credentialStore == nil {
		return status.Errorf(codes.FailedPrecondition,
			"credential %q is referenced but no credential store is configured", credentialRef)
	}

	_, err := s.credentialStore.Retrieve(ctx, credentialRef)
	if errors.Is(err, credentials.ErrCredentialNotFound) {
		return status.Errorf(codes.InvalidArgument, "credential %q not found in the credential store", credentialRef)
	}
	if err != nil {
		return status.Errorf(codes.Unavailable, "failed to check credential %q: %v", credentialRef, err)
	}
	return nil
}

func (s *LLMModelServer) Run(ctx context.Context, req *gogiv1.LLMRunRequest) (*gogiv1.LLMRunResponse, error) {

	Log.Infof("Using provider %s", req.GetConfig().GetProvider())

	// the request should specify the model and the provider
	response, err := s.providerRouter.Run(req)
	if err != nil {
		return nil, runError(err)
	}
	return toGRPCLLMRunResponse(response), nil

}

func (s *LLMModelServer) RunStream(req *gogiv1.LLMRunRequest,
	stream grpc.ServerStreamingServer[gogiv1.LLMStreamChunkResponse]) error {

	if err := s.providerRouter.RunStream(req, stream); err != nil {
		return runError(err)
	}
	return nil
}

// ListLLMs returns the models of the built-in providers and the registered models
func (s *LLMModelServer) ListLLMs(ctx context.Context, req *gogiv1.ListLLMsRequest) (*gogiv1.ListLLMsResponse, error) {

	models := make([]*gogiv1.ModelInfo, 0)
	for _, provider := range s.providerRouter.BuiltinProviders() {
		models = append(models, builtinModels[provider]...)
	}

	registered, err := s.registeredLLMs.ListRegisteredLLMs(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list registered models: %v", err)
	}
	for _, model := range registered {
		models = append(models, toModelInfo(model))
	}

	return &gogiv1.ListLLMsResponse{Models: models}, nil
}

func (s *LLMModelServer) GetLLMCapabilities(ctx context.Context, req *gogiv1.GetLLMCapabilitiesRequest) (*gogiv1.LLMCapabilitiesResponse, error) {

	if model := findBuiltinModel(req.GetModel()); model != nil {
		return &gogiv1.LLMCapabilitiesResponse{Capabilities: model.Capabilities}, nil
	}

	registered, err := s.registeredLLMs.GetRegisteredLLMByName(ctx, req.GetModel())
	if errors.Is(err, utils.ErrRegisteredLLMNotFound) {
		return nil, status.Errorf(codes.NotFound, "model %q not found", req.GetModel())
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get model %q: %v", req.GetModel(), err)
	}

	return &gogiv1.LLMCapabilitiesResponse{Capabilities: toModelInfo(registered).Capabilities}, nil
}

// GetLLMProviders returns the built-in providers and the providers of the registered models
func (s *LLMModelServer) GetLLMProviders(ctx context.Context, req *gogiv1.GetLLMProvidersRequest) (*gogiv1.LLMProvidersResponse, error) {

	providerModels := make(map[string][]string)
	for _, provider := range s.providerRouter.BuiltinProviders() {
		providerModels[provider] = make([]string, 0, len(builtinModels[provider]))
		for _, model := range builtinModels[provider] {
			providerModels[provider] = append(providerModels[provider], model.Name)
		}
	}

	registered, err := s.registeredLLMs.ListRegisteredLLMs(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list registered models: %v", err)
	}
	for _, model := range registered {
		providerModels[model.Provider] = append(providerModels[model.Provider], model.Name)
	}

	names := make([]string, 0, len(providerModels))
	for name := range providerModels {
		names = append(names, name)
	}
	sort.Strings(names)

	providerList := make([]*gogiv1.Provider, 0, len(names))
	for _, name := range names {
		p := &gogiv1.Provider{Name: name}
		if req.GetFetchModels() {
			p.Models = providerModels[name]
		}
		providerList = append(providerList, p)
	}

	return &gogiv1.LLMProvidersResponse{
		Providers: providerList,
	}, nil
}

// registration is a validated registration request, with its defaults applied
type registration struct {
	provider    string
	adapterType string
}

// validateRegistration checks a registration request and applies its defaults
func (s *LLMModelServer) validateRegistration(req *gogiv1.RegisterLLMRequest) (registration, error) {

	info := req.GetInfo()
	if info.GetName() == "" {
		return registration{}, fmt.Errorf("the model name is required")
	}

	result := registration{provider: info.GetProvider(), adapterType: req.GetAdapterType()}
	if result.provider == "" {
		result.provider = DefaultRegisteredLLMProvider
	}
	if result.adapterType == "" {
		result.adapterType = providers.DefaultAdapterType
	}

	if s.providerRouter.IsBuiltinProvider(result.provider) {
		return registration{}, fmt.Errorf(
			"provider %q is a built-in provider; register the model under another provider name", result.provider)
	}
	if findBuiltinModel(info.GetName()) != nil {
		return registration{}, fmt.Errorf("model %q is a built-in model", info.GetName())
	}
	if err := validateURL(req.GetEndpoint()); err != nil {
		return registration{}, fmt.Errorf("invalid endpoint: %w", err)
	}
	if _, err := healthCheckURL(req.GetEndpoint(), req.GetHealthCheck()); err != nil {
		return registration{}, fmt.Errorf("invalid health check: %w", err)
	}

	return result, nil
}

func validateURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return fmt.Errorf("%q is not an http(s) URL", raw)
	}
	return nil
}

// healthCheckURL resolves the health check against the endpoint, so a path such as
// "/health" is called on the endpoint's server, while a full URL is used as is.
// It returns "" if the model has no health check
func healthCheckURL(endpoint, healthCheck string) (string, error) {
	if healthCheck == "" {
		return "", nil
	}

	base, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	reference, err := url.Parse(healthCheck)
	if err != nil {
		return "", err
	}

	resolved := base.ResolveReference(reference).String()
	if err := validateURL(resolved); err != nil {
		return "", err
	}
	return resolved, nil
}

// RegisterLLM registers a model served by a self-hosted inference server with an
// OpenAI-compatible API, e.g. vLLM, TGI or Ollama. Registering a model with the name
// of an already registered model updates its registration
func (s *LLMModelServer) RegisterLLM(ctx context.Context, req *gogiv1.RegisterLLMRequest) (*gogiv1.RegisterLLMResponse, error) {

	validated, err := s.validateRegistration(req)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}

	if err := s.checkCredential(ctx, req.GetCredentialRef()); err != nil {
		return nil, err
	}

	info := req.GetInfo()
	provider, err := providers.NewProviderForAdapter(validated.adapterType, validated.provider, req.GetEndpoint(),
		s.apiKeySource(req.GetCredentialRef()))
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}

	capabilities := info.GetCapabilities()
	registered, err := s.registeredLLMs.UpsertRegisteredLLM(ctx, &postgres.GogiRegisteredLLM{
		Name:              info.GetName(),
		Provider:          validated.provider,
		ContextWindow:     capabilities.GetContextWindow(),
		SupportsVision:    capabilities.GetSupportsVision(),
		SupportsTools:     capabilities.GetSupportsTools(),
		SupportsStreaming: capabilities.GetSupportsStreaming(),
		SupportsJSONMode:  capabilities.GetSupportsJsonMode(),
		Endpoint:          req.GetEndpoint(),
		HealthCheck:       req.GetHealthCheck(),
		AdapterType:       validated.adapterType,
		CredentialRef:     req.GetCredentialRef(),
		Status:            LLMStatusProvisioning,
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to register model %q: %v", info.GetName(), err)
	}

	s.providerRouter.RegisterModel(registered.Provider, registered.Name, provider)

	response := &gogiv1.RegisterLLMResponse{Name: registered.Name, Status: registered.Status,
		RegisteredAt: registered.CreatedAt.Format(time.RFC3339)}

	Log.Infof("Registered model %s of provider %s served at %s", registered.Name, registered.Provider,
		registered.Endpoint)
	return response, nil
}

func (s *LLMModelServer) ListRegisteredLLMs(ctx context.Context, req *gogiv1.ListRegisteredLLMsRequest) (*gogiv1.ListRegisteredLLMsResponse, error) {

	registered, err := s.registeredLLMs.ListRegisteredLLMs(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list registered models: %v", err)
	}

	models := make([]*gogiv1.RegisteredLLM, 0, len(registered))
	for _, model := range registered {
		models = append(models, &gogiv1.RegisteredLLM{
			Info:          toModelInfo(model),
			Endpoint:      model.Endpoint,
			HealthCheck:   model.HealthCheck,
			Status:        model.Status,
			AdapterType:   model.AdapterType,
			CredentialRef: model.CredentialRef,
			RegisteredAt:  model.CreatedAt.Format(time.RFC3339),
		})
	}

	return &gogiv1.ListRegisteredLLMsResponse{
		Models: models,
	}, nil
}

// GetLLMStatus checks the health of a registered model, records the result and returns it.
// A model without a health check keeps its current status
func (s *LLMModelServer) GetLLMStatus(ctx context.Context, req *gogiv1.GetLLMStatusRequest) (*gogiv1.LLMStatusResponse, error) {

	registered, err := s.registeredLLMs.GetRegisteredLLMByName(ctx, req.GetName())
	if errors.Is(err, utils.ErrRegisteredLLMNotFound) {
		return nil, status.Errorf(codes.NotFound, "registered model %q not found", req.GetName())
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get model %q: %v", req.GetName(), err)
	}

	response := &gogiv1.LLMStatusResponse{
		Name:     registered.Name,
		Status:   registered.Status,
		Endpoint: registered.Endpoint,
	}
	if registered.LastCheckedAt != nil {
		response.LastChecked = registered.LastCheckedAt.Format(time.RFC3339)
	}

	// the URL was validated at registration
	healthCheck, _ := healthCheckURL(registered.Endpoint, registered.HealthCheck)
	if healthCheck == "" {
		return response, nil
	}

	checkedAt := time.Now()
	response.Status = s.checkHealth(ctx, healthCheck)
	response.LastChecked = checkedAt.Format(time.RFC3339)

	if err := s.registeredLLMs.UpdateRegisteredLLMStatus(ctx, registered.Name, response.Status, checkedAt); err != nil {
		Log.Warnf("Failed to record the status of model %s: %v", registered.Name, err)
	}
	return response, nil
}

// checkHealth calls the health check URL; any 2xx response means the model is healthy
func (s *LLMModelServer) checkHealth(ctx context.Context, healthCheck string) string {

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, healthCheck, nil)
	if err != nil {
		return LLMStatusUnhealthy
	}

	resp, err := s.healthClient.Do(request)
	if err != nil {
		Log.Warnf("Health check %s failed: %v", healthCheck, err)
		return LLMStatusUnhealthy
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		Log.Warnf("Health check %s returned status %d", healthCheck, resp.StatusCode)
		return LLMStatusUnhealthy
	}
	return LLMStatusHealthy
}
