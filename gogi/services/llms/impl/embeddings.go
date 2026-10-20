package impl

import (
	"context"
	"errors"
	"net/http"
	"time"

	"gogi/gogi/applications"
	"gogi/gogi/credentials"
	"gogi/gogi/embeddings"
	gogiv1 "gogi/gogi/gogi/v1"
	"gogi/gogi/storage/postgres"
	"gogi/gogi/utils"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// how long a call to an embedding model may take: a batch of chunks on a small CPU server
const embeddingTimeout = 2 * time.Minute

// EmbeddingModelStore stores the embedding models applications register
type EmbeddingModelStore interface {
	Upsert(ctx context.Context, model postgres.EmbeddingModel) (postgres.EmbeddingModel, error)
	Get(ctx context.Context, name string) (postgres.EmbeddingModel, error)
	List(ctx context.Context, prefix string) ([]postgres.EmbeddingModel, error)
}

// embeddingModels are how the server embeds text: the models applications register, where they
// may be, and the clients that call the models gogi provides and those applications host
type embeddingModels struct {
	store  EmbeddingModelStore
	policy embeddings.URLPolicy
	// calls the models gogi provides
	provided *embeddings.Client
	// calls the models applications host, only where the policy allows
	hosted *embeddings.Client
	// the platform's OpenAI key, for calls made without keys, as in development
	platformOpenAIKey string
}

// SetEmbeddingModels lets the server embed text
func (s *LLMModelServer) SetEmbeddingModels(store EmbeddingModelStore, policy embeddings.URLPolicy, platformOpenAIKey string) {
	s.embeddings = &embeddingModels{
		store:             store,
		policy:            policy,
		provided:          embeddings.NewClient(&http.Client{Timeout: embeddingTimeout}),
		hosted:            embeddings.NewClient(policy.HTTPClient(embeddingTimeout)),
		platformOpenAIKey: platformOpenAIKey,
	}
}

func (s *LLMModelServer) RegisterEmbeddingModel(ctx context.Context, req *gogiv1.RegisterEmbeddingModelRequest) (*gogiv1.EmbeddingModel, error) {
	if s.embeddings == nil {
		return nil, status.Error(codes.Unimplemented, "embedding models are not configured")
	}
	if err := embeddings.CheckName(req.GetName()); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if req.GetMaxInputTokens() < 0 {
		return nil, status.Error(codes.InvalidArgument, "max_input_tokens must not be negative")
	}
	if err := s.embeddings.policy.Check(ctx, req.GetUrl()); err != nil {
		return nil, embeddingURLError(req.GetUrl(), err)
	}

	model := embeddings.Model{
		Name:           req.GetName(),
		URL:            req.GetUrl(),
		ServedName:     req.GetServedName(),
		CredentialRef:  req.GetCredentialRef(),
		MaxInputTokens: int(req.GetMaxInputTokens()),
	}
	if model.ServedName == "" {
		model.ServedName = model.Name
	}
	if model.MaxInputTokens == 0 {
		model.MaxInputTokens = embeddings.DefaultMaxInputTokens
	}

	// the model is called once, to know it answers and how long its vectors are
	application := applications.FromContext(ctx)
	key, err := s.embeddingKey(ctx, application, model)
	if status.Code(err) == codes.FailedPrecondition {
		// as for a model registered to run, a credential the application hasn't stored is a wrong argument
		return nil, status.Error(codes.InvalidArgument, status.Convert(err).Message())
	}
	if err != nil {
		return nil, err
	}
	vectors, err := s.embeddings.hosted.Embed(ctx, model.URL, model.ServedName, key, []string{"gogi"})
	if err != nil {
		return nil, embeddingURLError(model.URL, err)
	}
	model.Dimensions = len(vectors[0])
	if model.Dimensions == 0 {
		return nil, status.Errorf(codes.FailedPrecondition, "the model at %s gave an empty vector", model.URL)
	}

	stored, err := s.embeddings.store.Upsert(ctx, postgres.EmbeddingModel{
		Name:           applications.Qualify(application, model.Name),
		URL:            model.URL,
		ServedName:     model.ServedName,
		CredentialRef:  applications.Qualify(application, model.CredentialRef),
		Dimensions:     model.Dimensions,
		MaxInputTokens: model.MaxInputTokens,
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to register the embedding model %q: %v", model.Name, err)
	}
	return toEmbeddingModel(registeredModel(application, stored), true), nil
}

func (s *LLMModelServer) ListEmbeddingModels(ctx context.Context, _ *gogiv1.ListEmbeddingModelsRequest) (*gogiv1.ListEmbeddingModelsResponse, error) {
	if s.embeddings == nil {
		return nil, status.Error(codes.Unimplemented, "embedding models are not configured")
	}
	application := applications.FromContext(ctx)

	response := &gogiv1.ListEmbeddingModelsResponse{}
	for _, model := range embeddings.BuiltIn() {
		available := true
		if model.CredentialRef != "" {
			_, err := s.embeddingKey(ctx, application, model)
			available = err == nil
		}
		response.Models = append(response.Models, toEmbeddingModel(model, available))
	}

	// the Default application's names are kept as they are given: it has everything
	prefix := ""
	if application != applications.Default {
		prefix = application + "."
	}
	registered, err := s.embeddings.store.List(ctx, prefix)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list the embedding models: %v", err)
	}
	for _, stored := range registered {
		response.Models = append(response.Models, toEmbeddingModel(registeredModel(application, stored), true))
	}
	return response, nil
}

func (s *LLMModelServer) Embed(ctx context.Context, req *gogiv1.EmbedRequest) (*gogiv1.EmbedResponse, error) {
	if s.embeddings == nil {
		return nil, status.Error(codes.Unimplemented, "embedding models are not configured")
	}
	if len(req.GetInputs()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "there is nothing to embed")
	}
	application := applications.FromContext(ctx)
	model, err := s.embeddingModel(ctx, application, req.GetModel())
	if err != nil {
		return nil, err
	}
	key, err := s.embeddingKey(ctx, application, model)
	if err != nil {
		return nil, err
	}

	client := s.embeddings.hosted
	if model.BuiltIn {
		client = s.embeddings.provided
	}
	vectors, err := client.Embed(ctx, model.URL, model.ServedName, key, req.GetInputs())
	if err != nil {
		return nil, embeddingCallError(model.Name, err)
	}

	response := &gogiv1.EmbedResponse{Model: model.Name, Embeddings: make([]*gogiv1.Embedding, len(vectors))}
	for i, vector := range vectors {
		response.Embeddings[i] = &gogiv1.Embedding{Values: vector}
	}
	return response, nil
}

// embeddingModel is the model of the name: one gogi provides, or one the application registered
func (s *LLMModelServer) embeddingModel(ctx context.Context, application, name string) (embeddings.Model, error) {
	if model, ok := embeddings.FindBuiltIn(name); ok {
		return model, nil
	}
	if embeddings.CheckName(name) == nil {
		stored, err := s.embeddings.store.Get(ctx, applications.Qualify(application, name))
		if err == nil {
			return registeredModel(application, stored), nil
		}
		if !errors.Is(err, utils.ErrEmbeddingModelNotFound) {
			return embeddings.Model{}, status.Errorf(codes.Internal, "failed to find the embedding model %q: %v", name, err)
		}
	}
	return embeddings.Model{}, status.Errorf(codes.NotFound, "there is no embedding model %q", name)
}

// embeddingKey is the key the model is called with, if it needs one: the application's own, or the
// platform's OpenAI key for calls made without keys
func (s *LLMModelServer) embeddingKey(ctx context.Context, application string, model embeddings.Model) (string, error) {
	if model.CredentialRef == "" {
		return "", nil
	}
	if model.BuiltIn {
		if application == applications.Default {
			if s.embeddings.platformOpenAIKey == "" {
				return "", status.Errorf(codes.FailedPrecondition, "gogi has no OpenAI key: set OPENAI_API_KEY")
			}
			return s.embeddings.platformOpenAIKey, nil
		}
		return s.applicationKey(ctx, application, model.CredentialRef)
	}

	if s.credentialStore == nil {
		return "", status.Error(codes.FailedPrecondition, "no credential store is configured for applications' keys")
	}
	credential, err := s.credentialStore.Retrieve(ctx, applications.Qualify(application, model.CredentialRef))
	if errors.Is(err, credentials.ErrCredentialNotFound) {
		return "", status.Errorf(codes.FailedPrecondition, "there is no credential %q", model.CredentialRef)
	}
	if err != nil {
		return "", status.Errorf(codes.Unavailable, "failed to read the credential %q: %v", model.CredentialRef, err)
	}
	return credential.Value, nil
}

// registeredModel is the stored model, named as the application named it
func registeredModel(application string, stored postgres.EmbeddingModel) embeddings.Model {
	return embeddings.Model{
		Name:           applications.Unqualify(application, stored.Name),
		Dimensions:     stored.Dimensions,
		MaxInputTokens: stored.MaxInputTokens,
		URL:            stored.URL,
		ServedName:     stored.ServedName,
		CredentialRef:  applications.Unqualify(application, stored.CredentialRef),
	}
}

func toEmbeddingModel(model embeddings.Model, available bool) *gogiv1.EmbeddingModel {
	listed := &gogiv1.EmbeddingModel{
		Name:           model.Name,
		Dimensions:     int32(model.Dimensions),
		MaxInputTokens: int32(model.MaxInputTokens),
		CredentialRef:  model.CredentialRef,
		Available:      available,
	}
	// where gogi's own models run is gogi's business
	if !model.BuiltIn {
		listed.Url = model.URL
	}
	return listed
}

// embeddingURLError tells the application why a model at the URL can't be registered
func embeddingURLError(url string, err error) error {
	var answered *embeddings.StatusError
	switch {
	case errors.Is(err, embeddings.ErrURLNotAllowed):
		return status.Errorf(codes.InvalidArgument, "%v", err)
	case errors.As(err, &answered):
		return status.Errorf(codes.FailedPrecondition, "the model at %s did not embed a text: %v", url, err)
	default:
		return status.Errorf(codes.Unavailable, "the model at %s could not be reached: %v", url, err)
	}
}

// embeddingCallError tells the caller why a model did not embed its texts
func embeddingCallError(model string, err error) error {
	var answered *embeddings.StatusError
	switch {
	case errors.As(err, &answered) && (answered.Code == http.StatusUnauthorized || answered.Code == http.StatusForbidden):
		return status.Errorf(codes.PermissionDenied, "the embedding model %q refused the key: %v", model, err)
	case errors.As(err, &answered) && answered.Code < 500 && answered.Code != http.StatusTooManyRequests:
		return status.Errorf(codes.InvalidArgument, "the embedding model %q did not embed the texts: %v", model, err)
	case errors.Is(err, embeddings.ErrURLNotAllowed):
		return status.Errorf(codes.FailedPrecondition, "the embedding model %q is no longer at an allowed address: %v", model, err)
	default:
		return status.Errorf(codes.Unavailable, "the embedding model %q could not be reached: %v", model, err)
	}
}
