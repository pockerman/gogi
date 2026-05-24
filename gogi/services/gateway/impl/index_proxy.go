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

func (p *IndexesProxy) GetIndex(ctx context.Context, req *gogiv1.GetIndexRequest) (*gogiv1.IndexResponse, error) {
	return p.proxy.ForwardGetIndex(ctx, req)
}

func (p *IndexesProxy) ListIndexes(ctx context.Context, req *gogiv1.ListIndexesRequest) (*gogiv1.ListIndexesResponse, error) {
	return p.proxy.ForwardListIndexes(ctx, req)
}

func (p *IndexesProxy) DeleteIndex(ctx context.Context, req *gogiv1.DeleteIndexRequest) (*gogiv1.DeleteIndexResponse, error) {
	return p.proxy.ForwardDeleteIndex(ctx, req)
}
