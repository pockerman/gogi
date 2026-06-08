package impl

import (
	"context"
	gogiv1 "gogi/gogi/gogi/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

//
// Generic proxy that routes gRPC calls to platform services.
// Routes based on x-target-service metadata from request context.
// Maintains Protocol Buffer efficiency by forwarding binary data.

type GenericGRPCProxy struct {
	registry      *ServiceRegistry
	stubFactories map[string]func() (interface{}, error) // Map of service names to functions that create gRPC client stubs``
}

func NewGenericProxy(registry *ServiceRegistry) *GenericGRPCProxy {
	return &GenericGRPCProxy{registry: registry}
}

// Documents

func (p *GenericGRPCProxy) ForwardListDocuments(
	ctx context.Context,
	req *gogiv1.ListDocumentsRequest,
) (*gogiv1.ListDocumentsResponse, error) {

	target, err := p.registry.ResolveService("documents")
	if err != nil {
		return nil, err
	}

	conn, err := grpc.Dial(
		target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)

	if err != nil {
		return nil, err
	}

	defer conn.Close()

	client := gogiv1.NewDocumentServerClient(conn)
	return client.ListDocuments(ctx, req)
}

func (p *GenericGRPCProxy) ForwardGetDocument(
	ctx context.Context,
	req *gogiv1.GetDocumentRequest,
) (*gogiv1.GetDocumentResponse, error) {

	target, err := p.registry.ResolveService("documents")
	if err != nil {
		return nil, err
	}

	conn, err := grpc.Dial(
		target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)

	if err != nil {
		return nil, err
	}

	defer conn.Close()

	client := gogiv1.NewDocumentServerClient(conn)
	return client.GetDocument(ctx, req)
}

func (p *GenericGRPCProxy) ForwardDeleteDocument(
	ctx context.Context,
	req *gogiv1.DeleteDocumentRequest,
) (*gogiv1.DeleteDocumentResponse, error) {

	target, err := p.registry.ResolveService("documents")
	if err != nil {
		return nil, err
	}

	conn, err := grpc.Dial(
		target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)

	if err != nil {
		return nil, err
	}

	defer conn.Close()

	client := gogiv1.NewDocumentServerClient(conn)
	return client.DeleteDocument(ctx, req)
}

func (p *GenericGRPCProxy) ForwardIngestDocument(ctx context.Context, req *gogiv1.IngestDocumentRequest) (*gogiv1.IngestDocumentJobResponse, error) {

	target, err := p.registry.ResolveService("documents")
	if err != nil {
		return nil, err
	}

	conn, err := grpc.Dial(
		target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)

	if err != nil {
		return nil, err
	}

	defer conn.Close()

	client := gogiv1.NewDocumentServerClient(conn)
	return client.IngestDocument(ctx, req)
}

func (p *GenericGRPCProxy) ForwardGetDocumentIngestJob(ctx context.Context, req *gogiv1.GetIngestDocumentJobRequest) (*gogiv1.IngestDocumentJobResponse, error) {
	target, err := p.registry.ResolveService("documents")
	if err != nil {
		return nil, err
	}

	conn, err := grpc.Dial(
		target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)

	if err != nil {
		return nil, err
	}

	defer conn.Close()

	client := gogiv1.NewDocumentServerClient(conn)
	return client.GetDocumentIngestJob(ctx, req)
}

// ===============================================================================

// Indexes
func (p *GenericGRPCProxy) ForwardCreateIndex(
	ctx context.Context,
	req *gogiv1.CreateIndexRequest,
) (*gogiv1.IndexResponse, error) {

	target, err := p.registry.ResolveService("indexes")
	if err != nil {
		return nil, err
	}

	conn, err := grpc.Dial(
		target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)

	if err != nil {
		return nil, err
	}

	defer conn.Close()

	client := gogiv1.NewIndexServiceClient(conn)
	return client.CreateIndex(ctx, req)
}

func (p *GenericGRPCProxy) ForwardGetIndexByName(
	ctx context.Context,
	req *gogiv1.GetIndexByNameRequest,
) (*gogiv1.IndexResponse, error) {

	target, err := p.registry.ResolveService("indexes")
	if err != nil {
		return nil, err
	}

	conn, err := grpc.Dial(
		target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)

	if err != nil {
		return nil, err
	}

	defer conn.Close()

	client := gogiv1.NewIndexServiceClient(conn)
	return client.GetIndexByName(ctx, req)
}

func (p *GenericGRPCProxy) ForwardGetIndexById(
	ctx context.Context,
	req *gogiv1.GetIndexByIdRequest,
) (*gogiv1.IndexResponse, error) {

	target, err := p.registry.ResolveService("indexes")
	if err != nil {
		return nil, err
	}

	conn, err := grpc.Dial(
		target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)

	if err != nil {
		return nil, err
	}

	defer conn.Close()

	client := gogiv1.NewIndexServiceClient(conn)
	return client.GetIndexById(ctx, req)
}

func (p *GenericGRPCProxy) ForwardListIndexes(
	ctx context.Context,
	req *gogiv1.ListIndexesRequest,
) (*gogiv1.ListIndexesResponse, error) {

	target, err := p.registry.ResolveService("indexes")
	if err != nil {
		return nil, err
	}

	conn, err := grpc.Dial(
		target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)

	if err != nil {
		return nil, err
	}

	defer conn.Close()

	client := gogiv1.NewIndexServiceClient(conn)
	return client.ListIndexes(ctx, req)
}

func (p *GenericGRPCProxy) ForwardDeleteIndex(
	ctx context.Context,
	req *gogiv1.DeleteIndexRequest,
) (*gogiv1.DeleteIndexResponse, error) {

	target, err := p.registry.ResolveService("indexes")
	if err != nil {
		return nil, err
	}

	conn, err := grpc.Dial(
		target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)

	if err != nil {
		return nil, err
	}

	defer conn.Close()

	client := gogiv1.NewIndexServiceClient(conn)
	return client.DeleteIndex(ctx, req)
}

// Similar forwarding methods would be implemented for other services (sessions, models, indexes, etc.)

//
// Example of how to implement a specific proxy method for the "documents" service.
// Similar methods would be implemented for other services (sessions, models, indexes, etc.)
// Each method extracts the target service from the registry, creates a gRPC client stub,
// and forwards the request while preserving the Protocol Buffer structure.
