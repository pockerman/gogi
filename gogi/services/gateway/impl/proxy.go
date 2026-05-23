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

type DocumentsProxy struct {
	gogiv1.UnimplementedDocumentServerServer
	proxy *GenericGRPCProxy
}

func (p *DocumentsProxy) ListDocuments(
	ctx context.Context,
	req *gogiv1.ListDocumentsRequest,
) (*gogiv1.ListDocumentsResponse, error) {

	return p.proxy.ForwardListDocuments(ctx, req)
}
