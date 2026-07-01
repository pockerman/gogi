package impl

import (
	"context"
	gogiv1 "gogi/gogi/gogi/v1"
	"gogi/gogi/utils"
	"time"

	"gogi/gogi/storage/postgres"

	"github.com/jackc/pgx/v5/pgxpool"
)

type LLMSessionServer struct {
	gogiv1.UnimplementedLLMSessionServerServer
	gogiLLMSessionRepo postgres.GogiLLMSessionRepository
}

func NewLLMSessionServer(dbClient *pgxpool.Pool) *LLMSessionServer {
	return &LLMSessionServer{
		gogiLLMSessionRepo: *postgres.NewGogiLLMSessionRepository(dbClient),
	}
}

func (s *LLMSessionServer) GetOrCreateSession(ctx context.Context, req *gogiv1.GetOrCreateSessionRequest) (*gogiv1.GetOrCreateSessionResponse, error) {

	now := time.Now().Unix()
	session := gogiv1.Session{
		SessionId: utils.NewUUIDString(),
		UserId:    utils.NewUUIDString(),
		CreatedAt: now,
		UpdatedAt: now,
	}
	return &gogiv1.GetOrCreateSessionResponse{Session: &session}, nil
}

func (s *LLMSessionServer) ListSessions(ctx context.Context, req *gogiv1.ListSessionsRequest) (*gogiv1.ListSessionsResponse, error) {

	panic("Not implemented")
}
func (s *LLMSessionServer) AddMessages(ctx context.Context, req *gogiv1.AddMessagesRequest) (*gogiv1.AddMessagesResponse, error) {
	return &gogiv1.AddMessagesResponse{Success: true, MessageCount: 10}, nil
}

func (s *LLMSessionServer) GetMessages(ctx context.Context, req *gogiv1.GetMessagesRequest) (*gogiv1.GetMessagesResponse, error) {
	panic("Not implemented")
}

func (s *LLMSessionServer) DeleteSession(ctx context.Context, req *gogiv1.DeleteSessionRequest) (*gogiv1.DeleteSessionResponse, error) {
	return &gogiv1.DeleteSessionResponse{Success: true}, nil
}

// Memory ops
func (s *LLMSessionServer) SaveMemory(ctx context.Context, req *gogiv1.SaveMemoryRequest) (*gogiv1.SaveMemoryResponse, error) {
	panic("Not implemented")
}
func (s *LLMSessionServer) GetMemory(ctx context.Context, req *gogiv1.GetMemoryRequest) (*gogiv1.GetMemoryResponse, error) {
	panic("Not implemented")
}
func (s *LLMSessionServer) DeleteMemory(ctx context.Context, req *gogiv1.DeleteMemoryRequest) (*gogiv1.DeleteMemoryResponse, error) {
	return &gogiv1.DeleteMemoryResponse{Success: true}, nil
}
func (s *LLMSessionServer) ClearUserMemory(ctx context.Context, req *gogiv1.ClearUserMemoryRequest) (*gogiv1.ClearUserMemoryResponse, error) {
	return &gogiv1.ClearUserMemoryResponse{Count: 1}, nil
}
