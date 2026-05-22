package impl

import (
	gogiv1 "gogi/gogi/gogi/v1"

	"google.golang.org/grpc"
)

type GrpcServer struct {
	// This struct can hold references to the service registry, proxy, and any other components needed to handle gRPC requests
	registry *ServiceRegistry
	proxy    *GenericGRPCProxy
	server   *grpc.Server
}

func NewGrpcServer(registry *ServiceRegistry, proxy *GenericGRPCProxy) *GrpcServer {

	// create underlying grpc server
	server := grpc.NewServer()

	grpcServer := &GrpcServer{
		registry: registry,
		proxy:    proxy,
		server:   server,
	}

	////
	// Register proxy handlers
	//

	gogiv1.RegisterDocumentServerServer(
		server,
		&DocumentsProxy{proxy: proxy},
	)

	return grpcServer

}

func (g *GrpcServer) Server() *grpc.Server {
	return g.server
}
