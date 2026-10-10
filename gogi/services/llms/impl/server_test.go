package impl

import (
	"context"
	"fmt"
	"gogi/gogi/credentials"
	gogiv1 "gogi/gogi/gogi/v1"
	"gogi/gogi/llm/providers"
	"gogi/gogi/storage/postgres"
	"gogi/gogi/utils"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// memoryRegisteredLLMStore is an in-memory RegisteredLLMStore
type memoryRegisteredLLMStore struct {
	mu     sync.Mutex
	models map[string]*postgres.GogiRegisteredLLM
}

func newMemoryRegisteredLLMStore() *memoryRegisteredLLMStore {
	return &memoryRegisteredLLMStore{models: make(map[string]*postgres.GogiRegisteredLLM)}
}

func (s *memoryRegisteredLLMStore) UpsertRegisteredLLM(ctx context.Context,
	model *postgres.GogiRegisteredLLM) (*postgres.GogiRegisteredLLM, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	model.UpdatedAt, model.LastCheckedAt = now, nil
	if existing, ok := s.models[model.Name]; ok {
		model.ID, model.CreatedAt = existing.ID, existing.CreatedAt
	} else {
		model.ID, model.CreatedAt = utils.NewUUIDString(), now
	}

	stored := *model
	s.models[model.Name] = &stored
	return model, nil
}

func (s *memoryRegisteredLLMStore) GetRegisteredLLMByName(ctx context.Context,
	name string) (*postgres.GogiRegisteredLLM, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	model, ok := s.models[name]
	if !ok {
		return nil, utils.ErrRegisteredLLMNotFound
	}
	stored := *model
	return &stored, nil
}

func (s *memoryRegisteredLLMStore) UpdateRegisteredLLMStatus(ctx context.Context,
	name, status string, checkedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	model, ok := s.models[name]
	if !ok {
		return utils.ErrRegisteredLLMNotFound
	}
	model.Status, model.LastCheckedAt = status, &checkedAt
	return nil
}

func (s *memoryRegisteredLLMStore) ListRegisteredLLMs(ctx context.Context) ([]*postgres.GogiRegisteredLLM, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	models := make([]*postgres.GogiRegisteredLLM, 0, len(s.models))
	for _, model := range s.models {
		stored := *model
		models = append(models, &stored)
	}
	return models, nil
}

// newFakeOllama serves Ollama's version endpoint and its OpenAI-compatible chat endpoint
func newFakeOllama(t *testing.T) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/version", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"version":"0.12.0"}`)
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("no credentials expected for a self-hosted model")
		}
		fmt.Fprint(w, `{"model":"llama3.2","choices":[{"message":{"role":"assistant","content":"Hi from Ollama"},
			"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":3,"total_tokens":7}}`)
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func newTestServer(store RegisteredLLMStore) *LLMModelServer {
	router := providers.NewLLMProviderRouter(map[string]providers.ModelProvider{
		"anthropic": providers.NewAnthropicLLMModelProvider("test-key"),
		"openai":    providers.NewOpenAILLMModelProvider("test-key"),
	})
	return newLLMModelServer(router, store, nil, nil, postgres.GogiIndexRepository{})
}

// ollamaRegistration registers llama3.2 served by the Ollama server at serverURL
func ollamaRegistration(serverURL string) *gogiv1.RegisterLLMRequest {
	return &gogiv1.RegisterLLMRequest{
		Info: &gogiv1.ModelInfo{
			Name:     "llama3.2",
			Provider: "ollama",
			Capabilities: &gogiv1.LLMCapabilities{
				ContextWindow:     128_000,
				SupportsTools:     true,
				SupportsStreaming: true,
				SupportsJsonMode:  true,
			},
		},
		Endpoint:    serverURL + "/v1",
		HealthCheck: "/api/version",
		AdapterType: "ollama",
	}
}

func ollamaRunRequest() *gogiv1.LLMRunRequest {
	return &gogiv1.LLMRunRequest{
		Config:   &gogiv1.LLMRunRequestConfig{Provider: "ollama", Model: "llama3.2"},
		Messages: []*gogiv1.LLMMessage{{Role: "user", Content: proto.String("Hello")}},
	}
}

func requireCode(t *testing.T, err error, code codes.Code) {
	t.Helper()
	if status.Code(err) != code {
		t.Errorf("expected code %s, got %v", code, err)
	}
}

func TestRegisterAndRunOllamaModel(t *testing.T) {
	ollama := newFakeOllama(t)
	server := newTestServer(newMemoryRegisteredLLMStore())
	ctx := context.Background()

	// before the registration the model cannot be used
	_, err := server.Run(ctx, ollamaRunRequest())
	requireCode(t, err, codes.InvalidArgument)

	registration, err := server.RegisterLLM(ctx, ollamaRegistration(ollama.URL))
	if err != nil {
		t.Fatal(err)
	}
	if registration.Name != "llama3.2" || registration.Status != LLMStatusProvisioning {
		t.Errorf("wrong registration response %+v", registration)
	}

	response, err := server.Run(ctx, ollamaRunRequest())
	if err != nil {
		t.Fatal(err)
	}
	if response.Provider != "ollama" || response.Content != "Hi from Ollama" || response.Usage.TotalTokens != 7 {
		t.Errorf("wrong run response %+v", response)
	}
}

func TestRegisteredModelIsResolvedByAnotherReplica(t *testing.T) {
	ollama := newFakeOllama(t)
	store := newMemoryRegisteredLLMStore()
	ctx := context.Background()

	if _, err := newTestServer(store).RegisterLLM(ctx, ollamaRegistration(ollama.URL)); err != nil {
		t.Fatal(err)
	}

	// a server sharing the store, e.g. another replica or the same one after a restart
	response, err := newTestServer(store).Run(ctx, ollamaRunRequest())
	if err != nil {
		t.Fatal(err)
	}
	if response.Content != "Hi from Ollama" {
		t.Errorf("wrong run response %+v", response)
	}

	// the registered model is routed only under its own provider
	request := ollamaRunRequest()
	request.Config.Provider = "other"
	_, err = newTestServer(store).Run(ctx, request)
	requireCode(t, err, codes.InvalidArgument)
}

func TestRegisterLLMValidation(t *testing.T) {
	server := newTestServer(newMemoryRegisteredLLMStore())
	ctx := context.Background()

	tests := map[string]func(req *gogiv1.RegisterLLMRequest){
		"missing name":         func(req *gogiv1.RegisterLLMRequest) { req.Info.Name = "" },
		"built-in provider":    func(req *gogiv1.RegisterLLMRequest) { req.Info.Provider = "openai" },
		"built-in model":       func(req *gogiv1.RegisterLLMRequest) { req.Info.Name = "gpt-4o" },
		"invalid endpoint":     func(req *gogiv1.RegisterLLMRequest) { req.Endpoint = "localhost:11434" },
		"invalid health check": func(req *gogiv1.RegisterLLMRequest) { req.HealthCheck = "ftp://localhost/health" },
		"unsupported adapter":  func(req *gogiv1.RegisterLLMRequest) { req.AdapterType = "bedrock" },
	}

	for name, modify := range tests {
		t.Run(name, func(t *testing.T) {
			req := ollamaRegistration("http://localhost:11434")
			modify(req)
			_, err := server.RegisterLLM(ctx, req)
			requireCode(t, err, codes.InvalidArgument)
		})
	}
}

func TestRegisterLLMDefaults(t *testing.T) {
	ollama := newFakeOllama(t)
	store := newMemoryRegisteredLLMStore()
	server := newTestServer(store)
	ctx := context.Background()

	// without a provider and an adapter type the model is a "custom" model
	// served through the OpenAI-compatible adapter
	req := ollamaRegistration(ollama.URL)
	req.Info.Provider, req.AdapterType = "", ""
	if _, err := server.RegisterLLM(ctx, req); err != nil {
		t.Fatal(err)
	}

	registered, _ := store.GetRegisteredLLMByName(ctx, "llama3.2")
	if registered.Provider != DefaultRegisteredLLMProvider || registered.AdapterType != providers.DefaultAdapterType {
		t.Errorf("wrong defaults %+v", registered)
	}

	request := ollamaRunRequest()
	request.Config.Provider = DefaultRegisteredLLMProvider
	response, err := server.Run(ctx, request)
	if err != nil || response.Provider != DefaultRegisteredLLMProvider {
		t.Errorf("wrong run response %+v, %v", response, err)
	}
}

func TestHealthCheckURL(t *testing.T) {
	tests := []struct{ endpoint, healthCheck, expected string }{
		// a path is called on the endpoint's server
		{"http://ml-inference.internal:8000/v1", "/health", "http://ml-inference.internal:8000/health"},
		{"http://localhost:11434/v1", "/api/version", "http://localhost:11434/api/version"},
		// a full URL is used as is
		{"http://localhost:11434/v1", "http://monitor:9000/ollama", "http://monitor:9000/ollama"},
		// no health check
		{"http://localhost:11434/v1", "", ""},
	}

	for _, test := range tests {
		resolved, err := healthCheckURL(test.endpoint, test.healthCheck)
		if err != nil || resolved != test.expected {
			t.Errorf("healthCheckURL(%q, %q) = %q, %v; expected %q",
				test.endpoint, test.healthCheck, resolved, err, test.expected)
		}
	}
}

func TestGetLLMStatus(t *testing.T) {
	ollama := newFakeOllama(t)
	server := newTestServer(newMemoryRegisteredLLMStore())
	ctx := context.Background()

	_, err := server.GetLLMStatus(ctx, &gogiv1.GetLLMStatusRequest{Name: "llama3.2"})
	requireCode(t, err, codes.NotFound)

	if _, err := server.RegisterLLM(ctx, ollamaRegistration(ollama.URL)); err != nil {
		t.Fatal(err)
	}
	requireRegisteredStatus(t, server, LLMStatusProvisioning)

	response, err := server.GetLLMStatus(ctx, &gogiv1.GetLLMStatusRequest{Name: "llama3.2"})
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != LLMStatusHealthy || response.Endpoint != ollama.URL+"/v1" || response.LastChecked == "" {
		t.Errorf("wrong status %+v", response)
	}
	requireRegisteredStatus(t, server, LLMStatusHealthy)

	// the model server goes down
	ollama.Close()
	response, err = server.GetLLMStatus(ctx, &gogiv1.GetLLMStatusRequest{Name: "llama3.2"})
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != LLMStatusUnhealthy {
		t.Errorf("expected an unhealthy model, got %+v", response)
	}
	requireRegisteredStatus(t, server, LLMStatusUnhealthy)
}

func TestGetLLMStatusWithoutHealthCheck(t *testing.T) {
	server := newTestServer(newMemoryRegisteredLLMStore())
	ctx := context.Background()

	req := ollamaRegistration("http://localhost:11434")
	req.HealthCheck = ""
	if _, err := server.RegisterLLM(ctx, req); err != nil {
		t.Fatal(err)
	}

	response, err := server.GetLLMStatus(ctx, &gogiv1.GetLLMStatusRequest{Name: "llama3.2"})
	if err != nil || response.Status != LLMStatusProvisioning || response.LastChecked != "" {
		t.Errorf("a model without a health check keeps its status, got %+v, %v", response, err)
	}
}

// requireRegisteredStatus checks the status ListRegisteredLLMs reports for llama3.2
func requireRegisteredStatus(t *testing.T, server *LLMModelServer, expected string) {
	t.Helper()
	registered, err := server.ListRegisteredLLMs(context.Background(), &gogiv1.ListRegisteredLLMsRequest{})
	if err != nil || len(registered.Models) != 1 || registered.Models[0].Status != expected {
		t.Errorf("expected status %s, got %+v, %v", expected, registered, err)
	}
}

func TestDiscoveryIncludesRegisteredModels(t *testing.T) {
	server := newTestServer(newMemoryRegisteredLLMStore())
	ctx := context.Background()

	if _, err := server.RegisterLLM(ctx, ollamaRegistration("http://localhost:11434")); err != nil {
		t.Fatal(err)
	}

	providersResponse, err := server.GetLLMProviders(ctx, &gogiv1.GetLLMProvidersRequest{FetchModels: true})
	if err != nil {
		t.Fatal(err)
	}
	models := make(map[string][]string)
	for _, provider := range providersResponse.Providers {
		models[provider.Name] = provider.Models
	}
	if len(models) != 3 || len(models["anthropic"]) == 0 || len(models["openai"]) == 0 ||
		len(models["ollama"]) != 1 || models["ollama"][0] != "llama3.2" {
		t.Errorf("wrong providers %v", models)
	}

	capabilities, err := server.GetLLMCapabilities(ctx, &gogiv1.GetLLMCapabilitiesRequest{Model: "llama3.2"})
	if err != nil || capabilities.Capabilities.ContextWindow != 128_000 {
		t.Errorf("wrong registered model capabilities %+v, %v", capabilities, err)
	}
	capabilities, err = server.GetLLMCapabilities(ctx, &gogiv1.GetLLMCapabilitiesRequest{Model: "claude-opus-5-5"})
	if err != nil || capabilities.Capabilities.ContextWindow != 1_000_000 {
		t.Errorf("wrong built-in model capabilities %+v, %v", capabilities, err)
	}
	_, err = server.GetLLMCapabilities(ctx, &gogiv1.GetLLMCapabilitiesRequest{Model: "unknown"})
	requireCode(t, err, codes.NotFound)

	listed, err := server.ListLLMs(ctx, &gogiv1.ListLLMsRequest{})
	if err != nil || len(listed.Models) != len(builtinModels["anthropic"])+len(builtinModels["openai"])+1 {
		t.Errorf("wrong models %+v, %v", listed, err)
	}

	registered, err := server.ListRegisteredLLMs(ctx, &gogiv1.ListRegisteredLLMsRequest{})
	if err != nil || len(registered.Models) != 1 || registered.Models[0].AdapterType != "ollama" {
		t.Errorf("wrong registered models %+v, %v", registered, err)
	}
}

// newFakeAuthenticatedServer serves an OpenAI-compatible chat endpoint and records
// the Authorization header of every request
func newFakeAuthenticatedServer(t *testing.T) (*httptest.Server, *[]string) {
	var mu sync.Mutex
	authorizations := make([]string, 0)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		authorizations = append(authorizations, r.Header.Get("Authorization"))
		mu.Unlock()
		fmt.Fprint(w, `{"model":"llama3.2","choices":[{"message":{"content":"Hi"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	}))
	t.Cleanup(server.Close)
	return server, &authorizations
}

func newTestServerWithCredentials(store RegisteredLLMStore, credentialStore credentials.CredentialStore) *LLMModelServer {
	server := newTestServer(store)
	server.credentialStore = credentialStore
	return server
}

func TestRegisterLLMWithCredential(t *testing.T) {
	inference, authorizations := newFakeAuthenticatedServer(t)
	ctx := context.Background()

	credentialStore := credentials.NewMemoryCredentialStore()
	if err := credentialStore.Store(ctx, "ml-inference-prod", "secret-1"); err != nil {
		t.Fatal(err)
	}

	store := newMemoryRegisteredLLMStore()
	server := newTestServerWithCredentials(store, credentialStore)

	req := ollamaRegistration(inference.URL)
	req.CredentialRef = "ml-inference-prod"
	if _, err := server.RegisterLLM(ctx, req); err != nil {
		t.Fatal(err)
	}

	// the registration keeps the reference, never the secret
	registered, err := server.ListRegisteredLLMs(ctx, &gogiv1.ListRegisteredLLMsRequest{})
	if err != nil || registered.Models[0].CredentialRef != "ml-inference-prod" {
		t.Fatalf("wrong registered models %+v, %v", registered, err)
	}

	if _, err := server.Run(ctx, ollamaRunRequest()); err != nil {
		t.Fatal(err)
	}

	// a rotated credential is used by the next request, also by another replica
	if err := credentialStore.Rotate(ctx, "ml-inference-prod", "secret-2"); err != nil {
		t.Fatal(err)
	}
	if _, err := server.Run(ctx, ollamaRunRequest()); err != nil {
		t.Fatal(err)
	}
	if _, err := newTestServerWithCredentials(store, credentialStore).Run(ctx, ollamaRunRequest()); err != nil {
		t.Fatal(err)
	}

	expected := []string{"Bearer secret-1", "Bearer secret-2", "Bearer secret-2"}
	if fmt.Sprint(*authorizations) != fmt.Sprint(expected) {
		t.Errorf("expected Authorization headers %v, got %v", expected, *authorizations)
	}
}

func TestRegisterLLMWithMissingCredential(t *testing.T) {
	ctx := context.Background()

	req := ollamaRegistration("http://localhost:11434")
	req.CredentialRef = "ml-inference-prod"

	// no credential store configured
	_, err := newTestServer(newMemoryRegisteredLLMStore()).RegisterLLM(ctx, req)
	requireCode(t, err, codes.FailedPrecondition)

	// the credential store does not hold the credential
	server := newTestServerWithCredentials(newMemoryRegisteredLLMStore(), credentials.NewMemoryCredentialStore())
	_, err = server.RegisterLLM(ctx, req)
	requireCode(t, err, codes.InvalidArgument)
}

func TestRunWithDeletedCredential(t *testing.T) {
	inference, _ := newFakeAuthenticatedServer(t)
	ctx := context.Background()

	credentialStore := credentials.NewMemoryCredentialStore()
	if err := credentialStore.Store(ctx, "ml-inference-prod", "secret"); err != nil {
		t.Fatal(err)
	}
	store := newMemoryRegisteredLLMStore()

	req := ollamaRegistration(inference.URL)
	req.CredentialRef = "ml-inference-prod"
	if _, err := newTestServerWithCredentials(store, credentialStore).RegisterLLM(ctx, req); err != nil {
		t.Fatal(err)
	}

	// another replica without the credential: the request fails instead of being sent unauthenticated
	_, err := newTestServerWithCredentials(store, credentials.NewMemoryCredentialStore()).Run(ctx, ollamaRunRequest())
	requireCode(t, err, codes.Internal)
	if err == nil || !strings.Contains(err.Error(), "credential not found") {
		t.Errorf("expected a credential error, got %v", err)
	}
}
