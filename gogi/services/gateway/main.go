package main

import (
	"gogi/gogi/services/gateway/impl"
	"gogi/gogi/utils"
	"os"
	"os/signal"
	"syscall"

	log "github.com/sirupsen/logrus"
)

// Initialize the gateway server and start listening for requests
// The API Gateway is the single entry point for all platform traffic, serving two purposes:
// External HTTP traffic → Routes HTTP requests from external clients to workflow containers based on API paths
// Internal gRPC traffic → Routes gRPC calls from workflows to platform services (sessions, models, etc.) based on x-target-service metadata

func RegisterPlatformServices(registry *impl.ServiceRegistry) {
	// Register core platform services (sessions, models, data, etc.) with the registry
	// This allows the gateway to route gRPC calls to the correct service based on x-target-service header
	registry.RegisterService("documents", "localhost:50051")
	registry.RegisterService("indexes", "localhost:50052")
	registry.RegisterService("ingestion", "localhost:50053")
	registry.RegisterService("search", "localhost:50054")
}

func main() {

	// initialize the logger for the API Gateway
	utils.InitLogger()

	// Run the API Gateway server
	// - Listen for incoming HTTP requests on a specified port
	// - For each request, determine the target workflow based on the URL path and route it accordingly
	// - For gRPC calls, inspect the x-target-service header to route to the correct platform service

	// create a new service registry to track available services and workflows
	registry := *impl.NewServiceRegistry()

	// Register core platform services with the registry
	RegisterPlatformServices(&registry)

	// figure out the ports for the HTTP server and gRPC server from the environment or config
	// start the HTTP server in a separate goroutine to handle external traffic
	// start the gRPC server in a separate goroutine to handle internal traffic from workflows

	log.Infof("API Gateway started and running")

	// Instead of: select {}

	// Use something like this to wait for an interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

}
