package main

import (
	gogiv1 "gogi/gogi/gogi/v1"
	"gogi/gogi/services/prompts/impl"
	"gogi/gogi/storage/minio"
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
	MINIO_HOST := utils.GetEnv("GOGI_MINIO_HOST", "minio:9000")
	MINIO_USER := utils.GetEnv("GOGI_MINIO_ROOT_USER", "admin")
	MINIO_ROOT_PASSWORD := utils.GetEnv("GOGI_MINIO_HOST", "password")

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

	minioClient := minio.NewGogiMinIOClient(MINIO_HOST, MINIO_USER, MINIO_ROOT_PASSWORD)

	grpcServer := grpc.NewServer()
	gogiv1.RegisterPromptServerServer(grpcServer, impl.NewPromptServer(pool, minioClient))

	// add the health server
	healthServer := health.NewServer()
	healthpb.RegisterHealthServer(grpcServer, healthServer)
	healthServer.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)

	log.Infof("%s server running on: %s", SERVICE_NAME, PORT)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
