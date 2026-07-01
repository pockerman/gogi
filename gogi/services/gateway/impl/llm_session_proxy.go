package impl

import (
	"context"
	gogiv1 "gogi/gogi/gogi/v1"
)

type LLMSessionServiceProxy struct {
	gogiv1.UnimplementedLLMSessionServerServer
	proxy *GenericGRPCProxy
}

func (p *LLMSessionServiceProxy) GetOrCreateSession(ctx context.Context, req *gogiv1.GetOrCreateSessionRequest) (*gogiv1.GetOrCreateSessionResponse, error) {
	return p.proxy.ForwardGetOrCreateSession(ctx, req)
}

func (p *LLMSessionServiceProxy) ListSessions(ctx context.Context, req *gogiv1.ListSessionsRequest) (*gogiv1.ListSessionsResponse, error) {
	panic("Not implemented")
}

func (p *LLMSessionServiceProxy) AddMessages(ctx context.Context, req *gogiv1.AddMessagesRequest) (*gogiv1.AddMessagesResponse, error) {
	return p.proxy.ForwardAddMessages(ctx, req)
}

func (p *LLMSessionServiceProxy) GetMessages(ctx context.Context, req *gogiv1.GetMessagesRequest) (*gogiv1.GetMessagesResponse, error) {
	panic("Not implemented")
}

func (p *LLMSessionServiceProxy) DeleteSession(ctx context.Context, req *gogiv1.DeleteSessionRequest) (*gogiv1.DeleteSessionResponse, error) {
	return p.proxy.ForwardDeleteSession(ctx, req)
}

func (p *LLMSessionServiceProxy) SaveMemory(ctx context.Context, req *gogiv1.SaveMemoryRequest) (*gogiv1.SaveMemoryResponse, error) {
	panic("Not implemented")
}

func (p *LLMSessionServiceProxy) GetMemory(ctx context.Context, req *gogiv1.GetMemoryRequest) (*gogiv1.GetMemoryResponse, error) {
	panic("Not implemented")
}

func (p *LLMSessionServiceProxy) DeleteMemory(ctx context.Context, req *gogiv1.DeleteMemoryRequest) (*gogiv1.DeleteMemoryResponse, error) {
	return p.proxy.ForwardDeleteMemory(ctx, req)
}

func (p *LLMSessionServiceProxy) ClearUserMemory(ctx context.Context, req *gogiv1.ClearUserMemoryRequest) (*gogiv1.ClearUserMemoryResponse, error) {
	return p.proxy.ForwardClearUserMemory(ctx, req)
}
