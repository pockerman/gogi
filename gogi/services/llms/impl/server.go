package impl

import (
	"context"
	gogiv1 "gogi/gogi/gogi/v1"
	LLM "gogi/gogi/llm"
	"gogi/gogi/llm/providers"

	"gogi/gogi/storage/postgres"
	"gogi/gogi/storage/vector_storage"

	"github.com/jackc/pgx/v5/pgxpool"
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

func toGRPCLLMRunResponse(LLM.LLModelResponse) gogiv1.LLMRunResponse {
	return gogiv1.LLMRunResponse{}
}

type LLMModelServer struct {
	gogiv1.UnimplementedIndexServiceServer
	chromaDBClient *vector_storage.ChromaDBClient
	gogiIndexRepo  postgres.GogiIndexRepository
	providerRouter providers.LLMProviderRouter
}

func NewLLMModelServer(chromaDBClient *vector_storage.ChromaDBClient, dbClient *pgxpool.Pool) *LLMModelServer {
	return &LLMModelServer{
		chromaDBClient: chromaDBClient,
		gogiIndexRepo:  *postgres.NewGogiIndexesRepository(dbClient),
	}
}

func (s *LLMModelServer) Run(ctx context.Context, req *gogiv1.LLMRunRequest) (gogiv1.LLMRunResponse, error) {

	// the request should specify the model and the provider
	response, _ := s.providerRouter.Run(req)

	return toGRPCLLMRunResponse(response), nil

}

func (s *LLMModelServer) RunStream(ctx context.Context, req *gogiv1.LLMRunRequest,
	stream grpc.ServerStreamingServer[gogiv1.LLMStreamChunkResponse]) error {

}

func (s *LLMModelServer) ListLLMs(ctx context.Context, req *gogiv1.ListLLMsRequest) (gogiv1.ListLLMsResponse, error) {

}

func (s *LLMModelServer) GetLLMCapabilities(ctx context.Context, req *gogiv1.GetLLMCapabilitiesRequest) (gogiv1.LLMCapabilitiesResponse, error) {

}

func (s *LLMModelServer) GetLLMProviders(ctx context.Context, req *gogiv1.GetLLMProvidersRequest) (gogiv1.LLMProvidersResponse, error) {

}

func (s *LLMModelServer) RegisterLLM(ctx context.Context, req *gogiv1.RegisterLLMRequest) (gogiv1.RegisterLLMResponse, error) {

}

func (s *LLMModelServer) ListRegisteredLLMs(ctx context.Context, req *gogiv1.ListRegisteredLLMsRequest) (gogiv1.ListRegisteredLLMsResponse, error) {

}

func (s *LLMModelServer) GetLLMStatus(ctx context.Context, req *gogiv1.GetLLMStatusRequest) (gogiv1.LLMStatusResponse, error) {

}
