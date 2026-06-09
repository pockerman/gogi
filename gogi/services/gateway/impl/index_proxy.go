package impl

import (
	"context"
	gogiv1 "gogi/gogi/gogi/v1"
)

type IndexesProxy struct {
	gogiv1.UnimplementedIndexServiceServer
	proxy *GenericGRPCProxy
}

func (p *IndexesProxy) CreateIndex(ctx context.Context, req *gogiv1.CreateIndexRequest) (*gogiv1.IndexResponse, error) {
	return p.proxy.ForwardCreateIndex(ctx, req)
}

func (p *IndexesProxy) GetIndexByName(ctx context.Context, req *gogiv1.GetIndexByNameRequest) (*gogiv1.IndexResponse, error) {
	return p.proxy.ForwardGetIndexByName(ctx, req)
}

func (p *IndexesProxy) GetIndexById(ctx context.Context, req *gogiv1.GetIndexByIdRequest) (*gogiv1.IndexResponse, error) {
	return p.proxy.ForwardGetIndexById(ctx, req)
}

func (p *IndexesProxy) ListIndexes(ctx context.Context, req *gogiv1.ListIndexesRequest) (*gogiv1.ListIndexesResponse, error) {
	return p.proxy.ForwardListIndexes(ctx, req)
}

func (p *IndexesProxy) DeleteIndexById(ctx context.Context, req *gogiv1.DeleteIndexByIdRequest) (*gogiv1.DeleteIndexResponse, error) {
	return p.proxy.ForwardDeleteIndexById(ctx, req)
}

func (p *IndexesProxy) DeleteIndexByName(ctx context.Context, req *gogiv1.DeleteIndexByNameRequest) (*gogiv1.DeleteIndexResponse, error) {
	return p.proxy.ForwardDeleteIndexByName(ctx, req)
}

func (p *IndexesProxy) DeleteOwnerIndexes(ctx context.Context, req *gogiv1.DeleteOwnerIndexesRequest) (*gogiv1.DeleteIndexResponse, error) {
	return p.proxy.ForwardDeleteOwnerIndexes(ctx, req)
}
