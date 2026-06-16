package impl

import (
	"context"
	gogiv1 "gogi/gogi/gogi/v1"

	"google.golang.org/grpc"
)

type LLMServiceProxy struct {
	gogiv1.UnimplementedLLMModelServerServer
	proxy *GenericGRPCProxy
}

func (p *LLMServiceProxy) Run(ctx context.Context, req *gogiv1.LLMRunRequest) (*gogiv1.LLMRunResponse, error) {
	return p.proxy.ForwardLLMRun(ctx, req)
}

func (p *LLMServiceProxy) RunStream(req *gogiv1.LLMRunRequest,
	stream grpc.ServerStreamingServer[gogiv1.LLMStreamChunkResponse]) error {
	return p.proxy.ForwardLLMRunStream(stream.Context(), req, stream)
}

func (p *LLMServiceProxy) GetLLMCapabilities(ctx context.Context, req *gogiv1.GetLLMCapabilitiesRequest) (*gogiv1.LLMCapabilitiesResponse, error) {
	panic("Not implemented")
}

func (p *LLMServiceProxy) GetLLMProviders(ctx context.Context, req *gogiv1.GetLLMProvidersRequest) (*gogiv1.LLMProvidersResponse, error) {
	return p.proxy.ForwardGetLLMProviders(ctx, req)
}

func (p *LLMServiceProxy) ListLLMs(ctx context.Context, req *gogiv1.ListLLMsRequest) (*gogiv1.ListLLMsResponse, error) {
	panic("Not implemented")
}

func (p *LLMServiceProxy) RegisterLLM(ctx context.Context, req *gogiv1.RegisterLLMRequest) (*gogiv1.RegisterLLMResponse, error) {
	return p.proxy.ForwardRegisterLLM(ctx, req)
}

func (p *LLMServiceProxy) ListRegisteredLLMs(ctx context.Context, req *gogiv1.ListRegisteredLLMsRequest) (*gogiv1.ListRegisteredLLMsResponse, error) {
	panic("Not implemented")
}

func (p *LLMServiceProxy) GetLLMStatus(ctx context.Context, req *gogiv1.GetLLMStatusRequest) (*gogiv1.LLMStatusResponse, error) {
	panic("Not implemented")
}

// func (p *IndexesProxy) DeleteOwnerIndexes(ctx context.Context, req *gogiv1.DeleteOwnerIndexesRequest) (*gogiv1.DeleteIndexResponse, error) {
// 	return p.proxy.ForwardDeleteOwnerIndexes(ctx, req)
//}
