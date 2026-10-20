package impl

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"gogi/gogi/applications"
	"gogi/gogi/credentials"
	"gogi/gogi/embeddings"
	gogiv1 "gogi/gogi/gogi/v1"
	"gogi/gogi/storage/postgres"
	"gogi/gogi/utils"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// memoryEmbeddingModelStore is an in-memory EmbeddingModelStore
type memoryEmbeddingModelStore struct {
	mu     sync.Mutex
	models map[string]postgres.EmbeddingModel
}

func (s *memoryEmbeddingModelStore) Upsert(_ context.Context, model postgres.EmbeddingModel) (postgres.EmbeddingModel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.models[model.Name] = model
	return model, nil
}

func (s *memoryEmbeddingModelStore) Get(_ context.Context, name string) (postgres.EmbeddingModel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	model, ok := s.models[name]
	if !ok {
		return postgres.EmbeddingModel{}, utils.ErrEmbeddingModelNotFound
	}
	return model, nil
}

func (s *memoryEmbeddingModelStore) List(_ context.Context, prefix string) ([]postgres.EmbeddingModel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var listed []postgres.EmbeddingModel
	for name, model := range s.models {
		if strings.HasPrefix(name, prefix) {
			listed = append(listed, model)
		}
	}
	return listed, nil
}

// fakeEmbeddingModel serves an OpenAI-compatible embeddings endpoint with vectors of the dimensions,
// recording the keys it was called with
type fakeEmbeddingModel struct {
	*httptest.Server
	mu   sync.Mutex
	keys []string
}

func newFakeEmbeddingModel(t *testing.T, dimensions int) *fakeEmbeddingModel {
	model := &fakeEmbeddingModel{}
	model.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		model.mu.Lock()
		model.keys = append(model.keys, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		model.mu.Unlock()
		var request struct {
			Input []string `json:"input"`
		}
		json.NewDecoder(r.Body).Decode(&request)
		data := make([]map[string]any, len(request.Input))
		for i := range request.Input {
			data[i] = map[string]any{"index": i, "embedding": make([]float32, dimensions)}
		}
		json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(model.Close)
	return model
}

func (m *fakeEmbeddingModel) calledWith() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.keys...)
}

func newEmbeddingServer(credentialStore credentials.CredentialStore, policy embeddings.URLPolicy) *LLMModelServer {
	server := newTestServerWithCredentials(newMemoryRegisteredLLMStore(), credentialStore)
	server.SetEmbeddingModels(&memoryEmbeddingModelStore{models: map[string]postgres.EmbeddingModel{}}, policy, "platform-key")
	return server
}

func forApp(application string) context.Context {
	return applications.WithApplication(context.Background(), application)
}

func TestEmbedWithAModelGogiRuns(t *testing.T) {
	model := newFakeEmbeddingModel(t, 384)
	t.Setenv("GOGI_EMBEDDINGS_BGE_SMALL_URL", model.URL+"/v1")
	server := newEmbeddingServer(credentials.NewMemoryCredentialStore(), embeddings.URLPolicy{})

	response, err := server.Embed(forApp("qubit-notes"), &gogiv1.EmbedRequest{Model: "bge-small-en-v1.5", Inputs: []string{"wing flutter", "shape functions"}})

	if err != nil {
		t.Fatal(err)
	}
	if len(response.GetEmbeddings()) != 2 || len(response.GetEmbeddings()[0].GetValues()) != 384 {
		t.Errorf("embeddings = %d of %d", len(response.GetEmbeddings()), len(response.GetEmbeddings()[0].GetValues()))
	}
	if keys := model.calledWith(); len(keys) != 1 || keys[0] != "" {
		t.Errorf("called with keys %q, want none", keys)
	}
}

func TestEmbedWithAModelGogiNeitherRunsNorHasRegistered(t *testing.T) {
	server := newEmbeddingServer(credentials.NewMemoryCredentialStore(), embeddings.URLPolicy{})

	for _, name := range []string{"clip", "clip-ViT-L-14", ""} {
		_, err := server.Embed(forApp("qubit-notes"), &gogiv1.EmbedRequest{Model: name, Inputs: []string{"wing flutter"}})
		if status.Code(err) != codes.NotFound || !strings.Contains(status.Convert(err).Message(), "there is no embedding model") {
			t.Errorf("%q: err = %v, want NotFound", name, err)
		}
	}
}

func TestOpenAIsModelIsCalledWithTheApplicationsKey(t *testing.T) {
	openAI := newFakeEmbeddingModel(t, 1536)
	t.Setenv("GOGI_EMBEDDINGS_OPENAI_URL", openAI.URL+"/v1")
	store := credentials.NewMemoryCredentialStore()
	store.Store(context.Background(), "qubit-notes.openai", "qubit-notes-openai-key")
	server := newEmbeddingServer(store, embeddings.URLPolicy{})

	if _, err := server.Embed(forApp("qubit-notes"), &gogiv1.EmbedRequest{Model: "text-embedding-3-small", Inputs: []string{"wing flutter"}}); err != nil {
		t.Fatal(err)
	}
	_, err := server.Embed(forApp("xact-marine"), &gogiv1.EmbedRequest{Model: "text-embedding-3-small", Inputs: []string{"hull"}})
	if status.Code(err) != codes.FailedPrecondition || !strings.Contains(status.Convert(err).Message(), `credential "openai"`) {
		t.Errorf("an application without an OpenAI key: err = %v", err)
	}
	// without keys, as in development, the platform's
	if _, err := server.Embed(context.Background(), &gogiv1.EmbedRequest{Model: "text-embedding-3-small", Inputs: []string{"x"}}); err != nil {
		t.Fatal(err)
	}

	if keys := openAI.calledWith(); len(keys) != 2 || keys[0] != "qubit-notes-openai-key" || keys[1] != "platform-key" {
		t.Errorf("OpenAI was called with %q", keys)
	}

	listed, err := server.ListEmbeddingModels(forApp("xact-marine"), &gogiv1.ListEmbeddingModelsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, model := range listed.GetModels() {
		if model.GetName() == "text-embedding-3-small" && model.GetAvailable() {
			t.Error("OpenAI's model is available to an application without an OpenAI key")
		}
		if model.GetName() == "bge-small-en-v1.5" && (!model.GetAvailable() || model.GetUrl() != "") {
			t.Errorf("bge = %+v", model)
		}
	}
}

func TestAnApplicationRegistersAModelItHosts(t *testing.T) {
	hosted := newFakeEmbeddingModel(t, 768)
	store := credentials.NewMemoryCredentialStore()
	store.Store(context.Background(), "qubit-notes.models-key", "qubit-notes-models-key")
	server := newEmbeddingServer(store, embeddings.URLPolicy{AllowPrivate: true})

	registered, err := server.RegisterEmbeddingModel(forApp("qubit-notes"), &gogiv1.RegisterEmbeddingModelRequest{
		Name: "nomic", Url: hosted.URL + "/v1", ServedName: "nomic-embed-text", CredentialRef: "models-key"})
	if err != nil {
		t.Fatal(err)
	}
	if registered.GetName() != "nomic" || registered.GetDimensions() != 768 || registered.GetMaxInputTokens() != 512 ||
		registered.GetCredentialRef() != "models-key" || !registered.GetAvailable() {
		t.Errorf("registered = %+v", registered)
	}

	response, err := server.Embed(forApp("qubit-notes"), &gogiv1.EmbedRequest{Model: "nomic", Inputs: []string{"wing flutter"}})
	if err != nil || len(response.GetEmbeddings()[0].GetValues()) != 768 {
		t.Fatalf("embed: %v", err)
	}
	if keys := hosted.calledWith(); len(keys) != 2 || keys[1] != "qubit-notes-models-key" {
		t.Errorf("the model was called with %q", keys)
	}

	listed, _ := server.ListEmbeddingModels(forApp("qubit-notes"), &gogiv1.ListEmbeddingModelsRequest{})
	if names := embeddingModelNames(listed); !strings.Contains(names, "nomic") {
		t.Errorf("qubit-notes lists %s", names)
	}
	// another application's model is not there for it
	listed, _ = server.ListEmbeddingModels(forApp("xact-marine"), &gogiv1.ListEmbeddingModelsRequest{})
	if names := embeddingModelNames(listed); strings.Contains(names, "nomic") {
		t.Errorf("xact-marine lists %s", names)
	}
	_, err = server.Embed(forApp("xact-marine"), &gogiv1.EmbedRequest{Model: "nomic", Inputs: []string{"hull"}})
	if status.Code(err) != codes.NotFound {
		t.Errorf("xact-marine embeds with qubit-notes' model: err = %v", err)
	}
}

func TestRegisteringAnEmbeddingModelRefusals(t *testing.T) {
	hosted := newFakeEmbeddingModel(t, 768)
	tests := []struct {
		name    string
		request *gogiv1.RegisterEmbeddingModelRequest
		policy  embeddings.URLPolicy
		code    codes.Code
		message string
	}{
		{"a name with a dot", &gogiv1.RegisterEmbeddingModelRequest{Name: "nomic.v2", Url: hosted.URL + "/v1"},
			embeddings.URLPolicy{AllowPrivate: true}, codes.InvalidArgument, "lowercase letters"},
		{"a model gogi provides", &gogiv1.RegisterEmbeddingModelRequest{Name: "all-MiniLM-L6-v2", Url: hosted.URL + "/v1"},
			embeddings.URLPolicy{AllowPrivate: true}, codes.InvalidArgument, "lowercase letters"},
		{"the metadata endpoint", &gogiv1.RegisterEmbeddingModelRequest{Name: "nomic", Url: "http://169.254.169.254/v1"},
			embeddings.URLPolicy{AllowPrivate: true}, codes.InvalidArgument, "not allowed"},
		{"a private address", &gogiv1.RegisterEmbeddingModelRequest{Name: "nomic", Url: hosted.URL + "/v1"},
			embeddings.URLPolicy{}, codes.InvalidArgument, "not allowed"},
		{"nothing answering", &gogiv1.RegisterEmbeddingModelRequest{Name: "nomic", Url: "http://127.0.0.1:1/v1"},
			embeddings.URLPolicy{AllowPrivate: true}, codes.Unavailable, "could not be reached"},
		{"a credential it has not stored", &gogiv1.RegisterEmbeddingModelRequest{Name: "nomic", Url: hosted.URL + "/v1", CredentialRef: "models-key"},
			embeddings.URLPolicy{AllowPrivate: true}, codes.InvalidArgument, `there is no credential "models-key"`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newEmbeddingServer(credentials.NewMemoryCredentialStore(), test.policy)

			_, err := server.RegisterEmbeddingModel(forApp("qubit-notes"), test.request)

			if status.Code(err) != test.code || !strings.Contains(status.Convert(err).Message(), test.message) {
				t.Errorf("err = %v, want %s containing %q", err, test.code, test.message)
			}
		})
	}
}

func embeddingModelNames(response *gogiv1.ListEmbeddingModelsResponse) string {
	names := make([]string, 0, len(response.GetModels()))
	for _, model := range response.GetModels() {
		names = append(names, model.GetName())
	}
	return strings.Join(names, ",")
}
