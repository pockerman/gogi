package impl

import (
	"bytes"
	"context"
	"fmt"
	"gogi/gogi/applications"
	gogiv1 "gogi/gogi/gogi/v1"
	"gogi/gogi/storage/objects"
	"gogi/gogi/storage/postgres"
	"io"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ScopedFields hold the names an application gives its prompts and the indexes they are for. A
// prompt's content is kept under its qualified name, so applications' prompts of the same name and
// version are kept apart
var ScopedFields = []string{"prompt_name", "gogi_index"}

type PromptServer struct {
	gogiv1.UnimplementedPromptServerServer
	gogiPromptsRepo postgres.GogiPromptsRepository
	// the prompts' texts, in the bucket
	objects objects.Store
	bucket  string
}

func NewPromptServer(dbClient *pgxpool.Pool, store objects.Store) *PromptServer {
	return &PromptServer{
		gogiPromptsRepo: *postgres.NewGogiPromptsRepository(dbClient),
		objects:         store,
		bucket:          objects.PromptsBucket(),
	}
}

func (s *PromptServer) RegisterPrompt(ctx context.Context, req *gogiv1.PromptRegistrationRequest) (*gogiv1.PromptRegistrationResponse, error) {

	objectPath := fmt.Sprintf("%s/%s", req.GetPromptName(), req.GetPromptVersion())
	content := req.GetContent()
	if _, err := s.objects.Put(ctx, s.bucket, objectPath, bytes.NewReader(content), int64(len(content)), "text/plain"); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to store prompt content: %v", err)
	}

	meta := req.GetMetadata()
	params := meta.GetParameters()
	testInfo := meta.GetTestInfo()

	prompt, err := s.gogiPromptsRepo.CreatePrompt(ctx, &postgres.GogiPrompt{
		Name:             req.GetPromptName(),
		Version:          req.GetPromptVersion(),
		Application:      applications.FromContext(ctx),
		GogiIndex:        req.GetGogiIndex(),
		MinioPath:        objectPath,
		Author:           meta.GetAuthor(),
		Model:            meta.GetModel(),
		Temperature:      params.GetTemperature(),
		MaxTokens:        params.GetMaxTokens(),
		StopSequences:    params.GetStopSequences(),
		FrequencyPenalty: params.GetFrequencyPenalty(),
		PresencePenalty:  params.GetPresencePenalty(),
		TestSetID:        testInfo.GetTestSetId(),
		TestSetPath:      testInfo.GetTestSetPath(),
		Metrics:          testInfo.GetMetrics(),
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to register prompt: %v", err)
	}

	return &gogiv1.PromptRegistrationResponse{
		PromptId: prompt.ID,
	}, nil
}

// promptOfApplication is the prompt with the id, if the application registered it
func (s *PromptServer) promptOfApplication(ctx context.Context, id string) (*postgres.GogiPrompt, error) {
	prompt, err := s.gogiPromptsRepo.GetPromptByID(ctx, id)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "prompt not found: %v", err)
	}
	if prompt.Application != applications.FromContext(ctx) {
		return nil, status.Errorf(codes.NotFound, "prompt not found: %s", id)
	}
	return prompt, nil
}

func (s *PromptServer) GetPrompt(ctx context.Context, req *gogiv1.PromptGetRequest) (*gogiv1.PromptGetResponse, error) {

	prompt, err := s.promptOfApplication(ctx, req.GetPromptId())
	if err != nil {
		return nil, err
	}

	content, err := s.content(ctx, prompt.MinioPath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to load prompt content: %v", err)
	}

	return &gogiv1.PromptGetResponse{
		PromptId:      prompt.ID,
		PromptName:    prompt.Name,
		PromptVersion: prompt.Version,
		GogiIndex:     prompt.GogiIndex,
		Content:       content,
		Metadata: &gogiv1.PromptMetadata{
			Author: prompt.Author,
			Model:  prompt.Model,
			Parameters: &gogiv1.PromptParameters{
				Temperature:      prompt.Temperature,
				MaxTokens:        prompt.MaxTokens,
				StopSequences:    prompt.StopSequences,
				FrequencyPenalty: prompt.FrequencyPenalty,
				PresencePenalty:  prompt.PresencePenalty,
			},
			TestInfo: &gogiv1.PromptTestInfo{
				TestSetId:   prompt.TestSetID,
				TestSetPath: prompt.TestSetPath,
				Metrics:     prompt.Metrics,
			},
		},
	}, nil
}

func (s *PromptServer) DeletePrompt(ctx context.Context, req *gogiv1.PromptDeleteRequest) (*gogiv1.PromptDeleteResponse, error) {

	prompt, err := s.promptOfApplication(ctx, req.GetPromptId())
	if err != nil {
		return nil, err
	}

	if err := s.objects.Remove(ctx, s.bucket, prompt.MinioPath); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to delete prompt content: %v", err)
	}

	if err := s.gogiPromptsRepo.DeletePromptByID(ctx, req.GetPromptId()); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to delete prompt: %v", err)
	}

	return &gogiv1.PromptDeleteResponse{Deleted: true}, nil
}

func (s *PromptServer) content(ctx context.Context, key string) ([]byte, error) {
	file, err := s.objects.Get(ctx, s.bucket, key, 0, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(file)
}
