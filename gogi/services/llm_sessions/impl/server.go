package impl

import (
	"context"
	gogiv1 "gogi/gogi/gogi/v1"

	"gogi/gogi/storage/postgres"

	"github.com/jackc/pgx/v5/pgxpool"
)

type LLMSessionServer struct {
	gogiv1.UnimplementedSessionServerServer
	gogiLLMSessionRepo postgres.GogiLLMSessionRepository
}

func NewLLMSessionServer(dbClient *pgxpool.Pool) *LLMSessionServer {
	return &LLMSessionServer{
		gogiLLMSessionRepo: *postgres.NewGogiLLMSessionRepository(dbClient),
	}
}

func (s *LLMSessionServer) GetOrCreateSession(ctx context.Context, req *gogiv1.GetOrCreateSessionRequest) (*gogiv1.GetOrCreateSessionResponse, error) {

	panic("Not implemented")
}

func (s *LLMSessionServer) ListSessions(ctx context.Context, req *gogiv1.ListSessionsRequest) (*gogiv1.ListSessionsResponse, error) {

	panic("Not implemented")
}
func (s *LLMSessionServer) AddMessages(ctx context.Context, req *gogiv1.AddMessagesRequest) (*gogiv1.AddMessagesResponse, error) {
	panic("Not implemented")
}

func (s *LLMSessionServer) GetMessages(ctx context.Context, req *gogiv1.GetMessagesRequest) (*gogiv1.GetMessagesResponse, error) {
	panic("Not implemented")
}

func (s *LLMSessionServer) DeleteSession(ctx context.Context, req *gogiv1.DeleteSessionRequest) (*gogiv1.DeleteSessionResponse, error) {
	panic("Not implemented")
}

// Memory ops
func (s *LLMSessionServer) SaveMemory(ctx context.Context, req *gogiv1.SaveMemoryRequest) (*gogiv1.SaveMemoryResponse, error) {
	panic("Not implemented")
}
func (s *LLMSessionServer) GetMemory(ctx context.Context, req *gogiv1.GetMemoryRequest) (*gogiv1.GetMemoryResponse, error) {
	panic("Not implemented")
}
func (s *LLMSessionServer) DeleteMemory(ctx context.Context, req *gogiv1.DeleteMemoryRequest) (*gogiv1.DeleteMemoryResponse, error) {
	panic("Not implemented")
}
func (s *LLMSessionServer) ClearUserMemory(ctx context.Context, req *gogiv1.ClearUserMemoryRequest) (*gogiv1.ClearUserMemoryResponse, error) {
	panic("Not implemented")
}
