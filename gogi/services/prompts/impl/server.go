package impl

import (
	"context"
	gogiv1 "gogi/gogi/gogi/v1"
	"gogi/gogi/storage/minio"
	"gogi/gogi/storage/postgres"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PromptServer struct {
	gogiv1.UnimplementedPromptServerServer
	gogiPromptsRepo postgres.GogiPromptsRepository
	minioClient     *minio.GogiMinIOClient
}

func NewPromptServer(dbClient *pgxpool.Pool, minioClient *minio.GogiMinIOClient) *PromptServer {
	return &PromptServer{
		gogiPromptsRepo: *postgres.NewGogiPromptsRepository(dbClient),
		minioClient:     minioClient,
	}
}

func (s *PromptServer) RegisterPrompt(ctx context.Context, req *gogiv1.PromptRegistrationRequest) (*gogiv1.PromptRegistrationResponse, error) {

	return &gogiv1.PromptRegistrationResponse{
		PromptId: uuid.New().String(),
	}, nil
}

func (s *PromptServer) GetPrompt(ctx context.Context, req *gogiv1.PromptGetRequest) (*gogiv1.PromptGetResponse, error) {

	return &gogiv1.PromptGetResponse{
		PromptId:      req.PromptId,
		PromptName:    "Stub-Name",
		PromptVersion: "v1.0.0",
		GogiIndex:     "my-index",
		Content: []byte(`
You are a helpful assistant.

Answer the user's questions clearly and concisely.
`),
		Metadata: &gogiv1.PromptMetadata{
			Author: "alex",
			Model:  "claude-sonnet-3.5",
			Parameters: &gogiv1.PromptParameters{
				Temperature:      0.2,
				MaxTokens:        4096,
				StopSequences:    []string{"<END>"},
				FrequencyPenalty: 0.0,
				PresencePenalty:  0.0,
			},
			TestInfo: &gogiv1.PromptTestInfo{
				TestSetId:   "test-set-1",
				TestSetPath: "/data/test_sets/test-set-1.json",
				Metrics: map[string]float64{
					"accuracy": 0.95,
					"latency":  120.5,
				},
			},
		},
	}, nil
}
func (s *PromptServer) DeletePrompt(ctx context.Context, req *gogiv1.PromptDeleteRequest) (*gogiv1.PromptDeleteResponse, error) {
	return &gogiv1.PromptDeleteResponse{Deleted: true}, nil
}
