package impl

import (
	"context"
	gogiv1 "gogi/gogi/gogi/v1"
)

type PromptsServiceProxy struct {
	gogiv1.UnimplementedPromptServerServer
	proxy *GenericGRPCProxy
}

func (p *PromptsServiceProxy) RegisterPrompt(ctx context.Context, req *gogiv1.PromptRegistrationRequest) (*gogiv1.PromptRegistrationResponse, error) {
	return p.proxy.ForwardRegisterPrompt(ctx, req)
}

func (p *PromptsServiceProxy) GetPrompt(ctx context.Context, req *gogiv1.PromptGetRequest) (*gogiv1.PromptGetResponse, error) {
	return p.proxy.ForwardGetPrompt(ctx, req)
}

func (p *PromptsServiceProxy) DeletePrompt(ctx context.Context, req *gogiv1.PromptDeleteRequest) (*gogiv1.PromptDeleteResponse, error) {
	return p.proxy.ForwardDeletePrompt(ctx, req)
}
