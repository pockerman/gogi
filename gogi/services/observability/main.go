package main

import (
	"context"
	gogiv1 "gogi/gogi/gogi/v1"
	"gogi/gogi/llm/pricing"
	"gogi/gogi/services/observability/impl"
	"gogi/gogi/storage/postgres"
	"gogi/gogi/utils"
	"net"

	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

func main() {

	const SERVICE_NAME string = "observability"

	PORT := utils.GetEnv("GOGI_OBSERVABILITY_PORT", ":50062")
	PROTOCOL := utils.GetEnv("GOGI_OBSERVABILITY_PROTOCOL", "tcp")

	// initialize the logger for the platform
	utils.InitLogger()

	config, err := impl.ConfigFromEnv()
	if err != nil {
		log.Fatalf("invalid configuration: %v", err)
	}
	prices, err := pricing.Load(config.PriceFile)
	if err != nil {
		log.Fatalf("failed to load the model prices: %v", err)
	}

	pool, err := postgres.NewPool(utils.GetDatabaseURL())
	if err != nil {
		log.Fatalf("failed to connect to postgres: %v", err)
	}
	// services do not create the tables; the migrations do
	defer pool.Close()

	lis, err := net.Listen(PROTOCOL, PORT)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	server := impl.NewObservabilityServer(postgres.NewGogiObservabilityRepository(pool), prices, config)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go server.RunBackground(ctx)

	// the observability service does not trace itself: its own telemetry would feed back into it
	grpcServer := grpc.NewServer()
	gogiv1.RegisterObservabilityServiceServer(grpcServer, server)

	healthServer := health.NewServer()
	healthpb.RegisterHealthServer(grpcServer, healthServer)
	healthServer.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)

	log.Infof("%s server running on: %s", SERVICE_NAME, PORT)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
