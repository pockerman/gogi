package main

import (
	"context"
	"gogi/gogi/credentials"
	gogiv1 "gogi/gogi/gogi/v1"
	"gogi/gogi/services/llm_tools/impl"
	"gogi/gogi/tools"

	"gogi/gogi/storage/postgres"
	"gogi/gogi/utils"
	"net"

	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

func main() {

	const SERVICE_NAME string = "llm-tools"

	PORT := utils.GetEnv("GOGI_LLM_TOOLS_PORT", ":50060")
	PROTOCOL := utils.GetEnv("GOGI_LLM_TOOLS_PROTOCOL", "tcp")

	// initialize the logger for the platform
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

	lis, err := net.Listen(PROTOCOL, PORT)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	// the credentials of tools, e.g. in AWS Secrets Manager or HashiCorp Vault
	credentialStore, err := credentials.NewCredentialStoreFromEnv(context.Background())
	if err != nil {
		log.Fatalf("failed to create the credential store: %v", err)
	}
	if credentialStore == nil {
		log.Infof("No credential store configured; tools cannot reference credentials")
	}

	breaker, err := tools.NewCircuitBreakerFromEnv()
	if err != nil {
		log.Fatalf("%v", err)
	}
	executorConfig, err := tools.ExecutorConfigFromEnv()
	if err != nil {
		log.Fatalf("%v", err)
	}

	grpcServer := grpc.NewServer()
	gogiv1.RegisterToolServerServer(grpcServer, impl.NewToolServer(pool, credentialStore, breaker, executorConfig))

	// add the health server
	healthServer := health.NewServer()
	healthpb.RegisterHealthServer(grpcServer, healthServer)
	healthServer.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)

	log.Infof("%s server running on: %s", SERVICE_NAME, PORT)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
