package impl

import (
	"context"
	gogiv1 "gogi/gogi/gogi/v1"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ToolsServiceProxy forwards the calls to the tools service
type ToolsServiceProxy struct {
	gogiv1.UnimplementedToolServerServer
	proxy *GenericGRPCProxy
}

// forwardTool calls the tools service with a client connected for the call
func forwardTool[Req any, Resp any](ctx context.Context, p *GenericGRPCProxy, req Req,
	call func(gogiv1.ToolServerClient, context.Context, Req) (Resp, error)) (Resp, error) {

	conn, err := p.buildConnection("llm-tools")
	if err != nil {
		var zero Resp
		return zero, status.Errorf(codes.Unavailable, "tools service unavailable: %v", err)
	}
	defer conn.Close()
	return call(gogiv1.NewToolServerClient(conn), ctx, req)
}

func (p *ToolsServiceProxy) RegisterTool(ctx context.Context, req *gogiv1.RegisterToolRequest) (*gogiv1.RegisterToolResponse, error) {
	return forwardTool(ctx, p.proxy, req, func(c gogiv1.ToolServerClient, ctx context.Context, req *gogiv1.RegisterToolRequest) (*gogiv1.RegisterToolResponse, error) {
		return c.RegisterTool(ctx, req)
	})
}

func (p *ToolsServiceProxy) DiscoverTools(ctx context.Context, req *gogiv1.DiscoverToolsRequest) (*gogiv1.DiscoverToolsResponse, error) {
	return forwardTool(ctx, p.proxy, req, func(c gogiv1.ToolServerClient, ctx context.Context, req *gogiv1.DiscoverToolsRequest) (*gogiv1.DiscoverToolsResponse, error) {
		return c.DiscoverTools(ctx, req)
	})
}

func (p *ToolsServiceProxy) ExecuteTool(ctx context.Context, req *gogiv1.ExecuteToolRequest) (*gogiv1.ExecuteToolResponse, error) {
	return forwardTool(ctx, p.proxy, req, func(c gogiv1.ToolServerClient, ctx context.Context, req *gogiv1.ExecuteToolRequest) (*gogiv1.ExecuteToolResponse, error) {
		return c.ExecuteTool(ctx, req)
	})
}

func (p *ToolsServiceProxy) ValidateTool(ctx context.Context, req *gogiv1.ValidateToolRequest) (*gogiv1.ValidateToolResponse, error) {
	return forwardTool(ctx, p.proxy, req, func(c gogiv1.ToolServerClient, ctx context.Context, req *gogiv1.ValidateToolRequest) (*gogiv1.ValidateToolResponse, error) {
		return c.ValidateTool(ctx, req)
	})
}

func (p *ToolsServiceProxy) ExecuteToolAsync(ctx context.Context, req *gogiv1.ExecuteToolRequest) (*gogiv1.ExecuteToolAsyncResponse, error) {
	return forwardTool(ctx, p.proxy, req, func(c gogiv1.ToolServerClient, ctx context.Context, req *gogiv1.ExecuteToolRequest) (*gogiv1.ExecuteToolAsyncResponse, error) {
		return c.ExecuteToolAsync(ctx, req)
	})
}

func (p *ToolsServiceProxy) GetTask(ctx context.Context, req *gogiv1.GetTaskRequest) (*gogiv1.GetTaskResponse, error) {
	return forwardTool(ctx, p.proxy, req, func(c gogiv1.ToolServerClient, ctx context.Context, req *gogiv1.GetTaskRequest) (*gogiv1.GetTaskResponse, error) {
		return c.GetTask(ctx, req)
	})
}

func (p *ToolsServiceProxy) RegisterMcpServer(ctx context.Context, req *gogiv1.RegisterMcpServerRequest) (*gogiv1.RegisterMcpServerResponse, error) {
	return forwardTool(ctx, p.proxy, req, func(c gogiv1.ToolServerClient, ctx context.Context, req *gogiv1.RegisterMcpServerRequest) (*gogiv1.RegisterMcpServerResponse, error) {
		return c.RegisterMcpServer(ctx, req)
	})
}
