package impl

import (
	"context"
	gogiv1 "gogi/gogi/gogi/v1"
)

type ToolServiceProxy struct {
	gogiv1.UnimplementedToolServerServer
	proxy *GenericGRPCProxy
}

func (p *ToolServiceProxy) RegisterTool(ctx context.Context, req *gogiv1.RegisterToolRequest) (*gogiv1.RegisterToolResponse, error) {
	return p.proxy.ForwardRegisterTool(ctx, req)
}

func (p *ToolServiceProxy) DiscoverTools(ctx context.Context, req *gogiv1.DiscoverToolsRequest) (*gogiv1.DiscoverToolsResponse, error) {
	return p.proxy.ForwardDiscoverTools(ctx, req)
}

func (p *ToolServiceProxy) ExecuteTool(ctx context.Context, req *gogiv1.ExecuteToolRequest) (*gogiv1.ExecuteToolResponse, error) {
	return p.proxy.ForwardExecuteTool(ctx, req)
}

func (p *ToolServiceProxy) ValidateTool(ctx context.Context, req *gogiv1.ValidateToolRequest) (*gogiv1.ValidateToolResponse, error) {
	return p.proxy.ForwardValidateTool(ctx, req)
}

func (p *ToolServiceProxy) ExecuteToolAsync(ctx context.Context, req *gogiv1.ExecuteToolRequest) (*gogiv1.ExecuteToolAsyncResponse, error) {
	return p.proxy.ForwardExecuteToolAsync(ctx, req)
}

func (p *ToolServiceProxy) GetTask(ctx context.Context, req *gogiv1.GetTaskRequest) (*gogiv1.GetTaskResponse, error) {
	return p.proxy.ForwardGetTask(ctx, req)
}

func (p *ToolServiceProxy) RegisterMcpServer(ctx context.Context, req *gogiv1.RegisterMcpServerRequest) (*gogiv1.RegisterMcpServerResponse, error) {
	return p.proxy.ForwardRegisterMcpServer(ctx, req)
}
