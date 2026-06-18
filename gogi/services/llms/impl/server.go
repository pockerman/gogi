package impl

import (
	"context"
	gogiv1 "gogi/gogi/gogi/v1"
	LLM "gogi/gogi/llm"
	"gogi/gogi/llm/providers"
	"time"

	"gogi/gogi/storage/postgres"
	"gogi/gogi/storage/vector_storage"

	"github.com/jackc/pgx/v5/pgxpool"
	Log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
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

type LLMModelServer struct {
	gogiv1.UnimplementedLLMModelServerServer
	chromaDBClient *vector_storage.ChromaDBClient
	gogiIndexRepo  postgres.GogiIndexRepository
	providerRouter *providers.LLMProviderRouter
}

func NewLLMModelServer(chromaDBClient *vector_storage.ChromaDBClient, dbClient *pgxpool.Pool,
	providerRouter *providers.LLMProviderRouter) *LLMModelServer {
	return &LLMModelServer{
		chromaDBClient: chromaDBClient,
		gogiIndexRepo:  *postgres.NewGogiIndexesRepository(dbClient),
		providerRouter: providerRouter,
	}
}

func (s *LLMModelServer) Run(ctx context.Context, req *gogiv1.LLMRunRequest) (*gogiv1.LLMRunResponse, error) {

	Log.Infof("Using provider %s", req.Config.Provider)

	// the request should specify the model and the provider
	response, _ := s.providerRouter.Run(req)
	return toGRPCLLMRunResponse(response), nil

}

func (s *LLMModelServer) RunStream(req *gogiv1.LLMRunRequest,
	stream grpc.ServerStreamingServer[gogiv1.LLMStreamChunkResponse]) error {
	err := s.providerRouter.RunStream(req, stream)
	return err

}

func (s *LLMModelServer) ListLLMs(ctx context.Context, req *gogiv1.ListLLMsRequest) (*gogiv1.ListLLMsResponse, error) {

	models := make([]*gogiv1.ModelInfo, 0, 2)

	models = append(models, &gogiv1.ModelInfo{Name: "Model-1",
		Provider: "anthropic",
		Capabilities: &gogiv1.LLMCapabilities{
			ContextWindow:     1000,
			SupportsVision:    true,
			SupportsTools:     true,
			SupportsStreaming: true,
			SupportsJsonMode:  true,
		}})
	models = append(models, &gogiv1.ModelInfo{Name: "Model-1",
		Provider: "openai",
		Capabilities: &gogiv1.LLMCapabilities{
			ContextWindow:     1000,
			SupportsVision:    true,
			SupportsTools:     true,
			SupportsStreaming: true,
			SupportsJsonMode:  true,
		}})
	return &gogiv1.ListLLMsResponse{Models: models}, nil
}

func (s *LLMModelServer) GetLLMCapabilities(ctx context.Context, req *gogiv1.GetLLMCapabilitiesRequest) (*gogiv1.LLMCapabilitiesResponse, error) {

	return &gogiv1.LLMCapabilitiesResponse{
		Capabilities: &gogiv1.LLMCapabilities{
			ContextWindow:     1000,
			SupportsVision:    true,
			SupportsTools:     true,
			SupportsStreaming: true,
			SupportsJsonMode:  true,
		},
	}, nil

}

// Return the list of LLM providers the platform supports
func (s *LLMModelServer) GetLLMProviders(ctx context.Context, req *gogiv1.GetLLMProvidersRequest) (*gogiv1.LLMProvidersResponse, error) {

	names := make([]string, 0, 2)
	names = append(names, "anthropic")
	names = append(names, "openai")

	providers := make([]*gogiv1.Provider, 0, len(names))

	for _, name := range names {

		p := &gogiv1.Provider{
			Name: name,
		}

		if req.FetchModels {
			models := []string{"claude-sonnet-3.5", "claude-opus-3.5", "claude-sonnet-4.5"}
			p.Models = models
		}

		providers = append(providers, p)
	}

	return &gogiv1.LLMProvidersResponse{
		Providers: providers,
	}, nil
}

func (s *LLMModelServer) RegisterLLM(ctx context.Context, req *gogiv1.RegisterLLMRequest) (*gogiv1.RegisterLLMResponse, error) {

	response := &gogiv1.RegisterLLMResponse{Name: req.Info.Name, Status: "registered",
		RegisteredAt: time.Now().Format(time.RFC3339)}

	Log.Infof("Sending  RegisterLLM response: %s", response)
	return response, nil

}

func (s *LLMModelServer) ListRegisteredLLMs(ctx context.Context, req *gogiv1.ListRegisteredLLMsRequest) (*gogiv1.ListRegisteredLLMsResponse, error) {

	models := make([]*gogiv1.RegisteredLLM, 0, 2)
	models = append(models, &gogiv1.RegisteredLLM{
		Endpoint:     "http://localhost:8000",
		HealthCheck:  "http://localhost:8000/health",
		Status:       "registered",
		AdapterType:  "",
		RegisteredAt: time.Now().Format(time.RFC3339),
		Info: &gogiv1.ModelInfo{Name: "Model-1",
			Provider: "Ollama",
			Capabilities: &gogiv1.LLMCapabilities{
				ContextWindow:     1000,
				SupportsVision:    true,
				SupportsTools:     true,
				SupportsStreaming: true,
				SupportsJsonMode:  true,
			}}})

	models = append(models, &gogiv1.RegisteredLLM{
		Endpoint:     "http://localhost:8000",
		HealthCheck:  "http://localhost:8000/health",
		Status:       "registered",
		AdapterType:  "",
		RegisteredAt: time.Now().Format(time.RFC3339),
		Info: &gogiv1.ModelInfo{Name: "Model-1",
			Provider: "Ollama",
			Capabilities: &gogiv1.LLMCapabilities{
				ContextWindow:     1000,
				SupportsVision:    true,
				SupportsTools:     true,
				SupportsStreaming: true,
				SupportsJsonMode:  true,
			}}})

	// query the db for the available registered models
	return &gogiv1.ListRegisteredLLMsResponse{
		Models: models,
	}, nil
}

func (s *LLMModelServer) GetLLMStatus(ctx context.Context, req *gogiv1.GetLLMStatusRequest) (*gogiv1.LLMStatusResponse, error) {

	return &gogiv1.LLMStatusResponse{Name: req.GetName(), Status: "registered",
		LastChecked: time.Now().Format(time.RFC3339), Endpoint: "http://localhost:8000"}, nil
}
