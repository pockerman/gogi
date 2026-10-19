package main

import (
	"gogi/gogi/applications"
	gogiv1 "gogi/gogi/gogi/v1"
	"gogi/gogi/observability"
	"gogi/gogi/services/prompts/impl"
	"gogi/gogi/storage/objects"
	"gogi/gogi/storage/postgres"
	"gogi/gogi/utils"
	"net"

	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

func main() {

	const SERVICE_NAME string = "prompts"

	PORT := utils.GetEnv("GOGI_PROMPTS_PORT", ":50058")
	PROTOCOL := utils.GetEnv("GOGI_PROMPTS_PROTOCOL", "tcp")

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

	// the prompts' texts are kept in the object store
	store, err := objects.NewStoreFromEnv()
	if err != nil {
		log.Fatalf("failed to set up the object store: %v", err)
	}

	// every RPC gets a span, continuing the trace of the request
	telemetry, closeTelemetry, err := observability.FromEnv(SERVICE_NAME)
	if err != nil {
		log.Fatalf("failed to set up telemetry: %v", err)
	}
	defer closeTelemetry()

	// prompts are the application's; they are found by id, so the server checks whose they are
	grpcServer := grpc.NewServer(append(telemetry.ServerOptions(), applications.ScopeFromEnv(impl.ScopedFields...).ServerOptions()...)...)
	gogiv1.RegisterPromptServerServer(grpcServer, impl.NewPromptServer(pool, store))

	// add the health server
	healthServer := health.NewServer()
	healthpb.RegisterHealthServer(grpcServer, healthServer)
	healthServer.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)

	log.Infof("%s server running on: %s", SERVICE_NAME, PORT)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
