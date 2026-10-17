package impl

import (
	gogiv1 "gogi/gogi/gogi/v1"
	"time"

	"google.golang.org/grpc"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

// how long the gateway waits for a service to answer a health check
const healthCheckTimeout = 2 * time.Second

type GrpcServer struct {
	// This struct can hold references to the service registry, proxy, and any other components needed to handle gRPC requests
	registry *ServiceRegistry
	proxy    *GenericGRPCProxy
	server   *grpc.Server
}

func NewGrpcServer(registry *ServiceRegistry, proxy *GenericGRPCProxy, options ...grpc.ServerOption) *GrpcServer {

	// create underlying grpc server
	server := grpc.NewServer(options...)

	grpcServer := &GrpcServer{
		registry: registry,
		proxy:    proxy,
		server:   server,
	}

	//
	// Register proxy handlers
	//

	gogiv1.RegisterDocumentServerServer(
		server,
		&DocumentsProxy{proxy: proxy},
	)

	gogiv1.RegisterIndexServiceServer(
		server,
		&IndexesProxy{proxy: proxy},
	)

	gogiv1.RegisterLLMModelServerServer(server,
		&LLMServiceProxy{proxy: proxy})

	gogiv1.RegisterPromptServerServer(server,
		&PromptsServiceProxy{proxy: proxy})

	gogiv1.RegisterLLMSessionServerServer(server,
		&LLMSessionServiceProxy{proxy: proxy})

	gogiv1.RegisterWorkflowServerServer(server,
		&WorkflowsProxy{proxy: proxy})

	gogiv1.RegisterToolServerServer(server,
		&ToolsServiceProxy{proxy: proxy})

	gogiv1.RegisterGuardrailsServiceServer(server,
		&GuardrailsServiceProxy{proxy: proxy})

	gogiv1.RegisterObservabilityServiceServer(server,
		&ObservabilityServiceProxy{proxy: proxy})

	// the platform's health: the gateway's own, and that of each service behind it
	healthpb.RegisterHealthServer(server, NewGatewayHealth(registry, healthCheckTimeout))

	return grpcServer

}

func (g *GrpcServer) Server() *grpc.Server {
	return g.server
}
