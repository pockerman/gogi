package impl

import (
	"context"
	gogiv1 "gogi/gogi/gogi/v1"
	LLM "gogi/gogi/llm"
	"gogi/gogi/llm/providers"

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
	panic("Not implemented")
	//return gogiv1.ListLLMsResponse{}, nil
}

func (s *LLMModelServer) GetLLMCapabilities(ctx context.Context, req *gogiv1.GetLLMCapabilitiesRequest) (*gogiv1.LLMCapabilitiesResponse, error) {
	panic("Not implemented")
	//turn gogiv1.LLMCapabilitiesResponse{}, nil
}

func (s *LLMModelServer) GetLLMProviders(ctx context.Context, req *gogiv1.GetLLMProvidersRequest) (*gogiv1.LLMProvidersResponse, error) {
	panic("Not implemented")
	//return gogiv1.LLMProvidersResponse{}, nil
}

func (s *LLMModelServer) RegisterLLM(ctx context.Context, req *gogiv1.RegisterLLMRequest) (*gogiv1.RegisterLLMResponse, error) {
	panic("Not implemented")
	//return gogiv1.RegisterLLMResponse{}, nil

}

func (s *LLMModelServer) ListRegisteredLLMs(ctx context.Context, req *gogiv1.ListRegisteredLLMsRequest) (*gogiv1.ListRegisteredLLMsResponse, error) {
	panic("Not implemented")
	//return gogiv1.ListRegisteredLLMsResponse{}, nil
}

func (s *LLMModelServer) GetLLMStatus(ctx context.Context, req *gogiv1.GetLLMStatusRequest) (*gogiv1.LLMStatusResponse, error) {
	panic("Not implemented")
	//return gogiv1.LLMStatusResponse{}, nil
}
