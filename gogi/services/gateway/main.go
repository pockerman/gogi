package main

import (
	"gogi/gogi/services/gateway/impl"
	"gogi/gogi/utils"
	"net"

	log "github.com/sirupsen/logrus"
)

// Initialize the gateway server and start listening for requests
// The API Gateway is the single entry point for all platform traffic, serving two purposes:
// External HTTP traffic → Routes HTTP requests from external clients to workflow containers based on API paths
// Internal gRPC traffic → Routes gRPC calls from workflows to platform services (sessions, models, etc.) based on x-target-service metadata

func RegisterPlatformServices(registry *impl.ServiceRegistry) {
	// Register core platform services (sessions, models, data, etc.) with the registry
	// This allows the gateway to route gRPC calls to the correct service based on x-target-service header
	registry.RegisterService("documents", "documents:50054")
	registry.RegisterService("indexes", "localhost:50055")
	//registry.RegisterService("ingestion", "localhost:50053")
	//registry.RegisterService("search", "localhost:50054")
}

func main() {

	const PORT string = ":50051"
	const PROTOCOL string = "tcp"

	// initialize the logger for the API Gateway
	utils.InitLogger()

	// Run the API Gateway server
	// - Listen for incoming HTTP requests on a specified port
	// - For each request, determine the target workflow based on the URL path and route it accordingly
	// - For gRPC calls, inspect the x-target-service header to route to the correct platform service

	// create a new service registry to track available services and workflows
	registry := impl.NewServiceRegistry()

	// Register core platform services with the registry
	RegisterPlatformServices(registry)

	grpcServer := impl.NewGrpcServer(registry, impl.NewGenericProxy(registry))

	lis, err := net.Listen(PROTOCOL, PORT)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	// figure out the ports for the HTTP server and gRPC server from the environment or config
	// start the HTTP server in a separate goroutine to handle external traffic
	// start the gRPC server in a separate goroutine to handle internal traffic from workflows

	log.Infof("API Gateway started and running")

	// Instead of: select {}

	// Use something like this to wait for an interrupt signal
	if err := grpcServer.Server().Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
	log.Println("Shutting down server...")

}
