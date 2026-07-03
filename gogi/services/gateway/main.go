package main

import (
	"gogi/gogi/services/gateway/impl"
	"gogi/gogi/storage/postgres"
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

	documents_addr := utils.GetEnv("GOGI_DOCUMENT_SERVICE_ADDR", "documents:50054")
	registry.RegisterService("documents", documents_addr)

	indexes_addr := utils.GetEnv("GOGI_INDEX_SERVICE_ADDR", "indexes:50055")
	registry.RegisterService("indexes", indexes_addr)

	llms_addr := utils.GetEnv("GOGI_LLM_SERVICE_ADDR", "llms:50057")
	registry.RegisterService("llms", llms_addr)

	prompts_addr := utils.GetEnv("GOGI_PROMPTS_SERVICE_ADDR", "prompts:50058")
	registry.RegisterService("prompts", prompts_addr)

	llms_session_addr := utils.GetEnv("GOGI_LLM_SESSIONS_ADDR", "llm-sessions:50059")
	registry.RegisterService("llm-sessions", llms_session_addr)
}

func main() {

	const SERVICE_NAME string = "gateway"

	PORT := utils.GetEnv("GOGI_GATEWAY_PORT", ":50051")
	PROTOCOL := utils.GetEnv("GOGI_GATEWAY_PROTOCOL", "tcp")

	// 1. initialize the logger for the API Gateway
	utils.InitLogger()

	// 2. Connect to the DB
	pool, err := postgres.NewPool(
		utils.GetDatabaseURL(),
	)

	if err != nil {
		log.Fatalf("failed to connect to postgres: %v", err)
	}

	// services do not create the tables. The API service does
	defer pool.Close()

	// 3. Create the Repositories we need
	// TODO: This may not be correct here
	postgres.NewJobsRepository(pool)

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

	log.Infof("Service %s started and running on port %s", SERVICE_NAME, PORT)

	// Use something like this to wait for an interrupt signal
	if err := grpcServer.Server().Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
	log.Println("Shutting down server...")

}
