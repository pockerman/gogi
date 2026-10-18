package impl

import (
	"context"
	"errors"

	"gogi/gogi/applications"
	"gogi/gogi/credentials"
	gogiv1 "gogi/gogi/gogi/v1"
	"gogi/gogi/llm"
	"gogi/gogi/llm/providers"
	"gogi/gogi/storage/postgres"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// An application's models are its own: the models it registers, and the credentials they need,
// are kept qualified by the application, and its calls to the built-in providers are made with the
// keys it stored, never the platform's. Calls for the Default application, as when gogi runs
// without keys, use the platform's keys

// forApplication is the request and its context for the call's application
func (s *LLMModelServer) forApplication(ctx context.Context, req *gogiv1.LLMRunRequest) (context.Context, *gogiv1.LLMRunRequest) {
	application := applications.FromContext(ctx)
	if application == applications.Default {
		return ctx, req
	}

	req = proto.Clone(req).(*gogiv1.LLMRunRequest)
	if config := req.GetConfig(); config != nil {
		if !s.providerRouter.IsBuiltinProvider(config.GetProvider()) {
			config.Model = applications.Qualify(application, config.GetModel())
		}
		for _, model := range config.GetFallback().GetModels() {
			if !s.providerRouter.IsBuiltinProvider(model.GetProvider()) {
				model.Model = applications.Qualify(application, model.GetModel())
			}
		}
	}

	return providers.WithApplicationKeys(ctx, func(provider string) (string, error) {
		return s.applicationKey(ctx, application, provider)
	}), req
}

// applicationKey is the key the application stored for the built-in provider, as the credential
// named after it
func (s *LLMModelServer) applicationKey(ctx context.Context, application, provider string) (string, error) {
	if s.credentialStore == nil {
		return "", status.Error(codes.FailedPrecondition, "no credential store is configured for applications' keys")
	}
	credential, err := s.credentialStore.Retrieve(ctx, applications.Qualify(application, provider))
	if errors.Is(err, credentials.ErrCredentialNotFound) {
		return "", status.Errorf(codes.FailedPrecondition,
			"there is no %s key: store one as the credential %q", provider, provider)
	}
	if err != nil {
		return "", status.Errorf(codes.Unavailable, "failed to read the %s key: %v", provider, err)
	}
	return credential.Value, nil
}

// registeredOfApplication are the models the call's application registered, named as it named them
func (s *LLMModelServer) registeredOfApplication(ctx context.Context) ([]*postgres.GogiRegisteredLLM, error) {
	registered, err := s.registeredLLMs.ListRegisteredLLMs(ctx)
	if err != nil {
		return nil, err
	}
	application := applications.FromContext(ctx)
	if application == applications.Default {
		return registered, nil
	}

	own := make([]*postgres.GogiRegisteredLLM, 0, len(registered))
	for _, model := range registered {
		if !applications.Owns(application, model.Name) {
			continue
		}
		named := *model
		named.Name = applications.Unqualify(application, model.Name)
		named.CredentialRef = applications.Unqualify(application, model.CredentialRef)
		own = append(own, &named)
	}
	return own, nil
}

// applicationStream gives the router the context of the call's application, and the application
// back the names of its models
type applicationStream struct {
	grpc.ServerStreamingServer[gogiv1.LLMStreamChunkResponse]
	ctx context.Context
}

func (s *applicationStream) Context() context.Context {
	return s.ctx
}

func (s *applicationStream) Send(chunk *gogiv1.LLMStreamChunkResponse) error {
	chunk.Model = applications.Unqualify(applications.FromContext(s.ctx), chunk.GetModel())
	return s.ServerStreamingServer.Send(chunk)
}

// servedName is what a registered model's server knows it by, if not the name it is kept under
func servedName(kept, registered string) string {
	if kept == registered {
		return ""
	}
	return registered
}

// servedAs calls a registered model by the name its server knows it by, if not the name it is kept
// under
func servedAs(provider providers.ModelProvider, served string) providers.ModelProvider {
	if served == "" {
		return provider
	}
	return &renamedModel{ModelProvider: provider, served: served}
}

type renamedModel struct {
	providers.ModelProvider
	served string
}

func (m *renamedModel) Run(ctx context.Context, messages []llm.LLMMessage, config llm.LLMModelConfig) (llm.LLModelResponse, error) {
	config.ModelName = m.served
	return m.ModelProvider.Run(ctx, messages, config)
}

func (m *renamedModel) RunStream(messages []llm.LLMMessage, config llm.LLMModelConfig,
	stream grpc.ServerStreamingServer[gogiv1.LLMStreamChunkResponse]) error {
	config.ModelName = m.served
	return m.ModelProvider.RunStream(messages, config, stream)
}
