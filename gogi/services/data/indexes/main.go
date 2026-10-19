package main

import (
	"gogi/gogi/applications"
	"gogi/gogi/services/data/indexes/impl"
	"gogi/gogi/storage/objects"
	"gogi/gogi/storage/postgres"
	"gogi/gogi/storage/vector_storage"
	"gogi/gogi/utils"
	"net"

	gogiv1 "gogi/gogi/gogi/v1"

	"strconv"

	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

func main() {

	const SERVICE_NAME string = "indexes"

	PORT := utils.GetEnv("GOGI_INDEXES_PORT", ":50055")
	PROTOCOL := utils.GetEnv("GOGI_INDEXES_PROTOCOL", "tcp")

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

	vectorDBConnectionDetails, _ := utils.GetVectorDBStorageConnectionDetails()
	port, _ := strconv.Atoi(vectorDBConnectionDetails.GOGI_VECTOR_DB_PORT)
	chromaDBClient := vector_storage.NewChromaDBClient(vectorDBConnectionDetails.GOGI_VECTOR_DB_HOST,
		port)

	lis, err := net.Listen(PROTOCOL, PORT)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	// indexes, and their owners and members, are the application's
	indexServer := grpc.NewServer(applications.ScopeFromEnv(impl.ScopedFields...).ServerOptions()...)

	// deleting an index deletes its documents' files
	store, err := objects.NewStoreFromEnv()
	if err != nil {
		log.Fatalf("failed to set up the object store: %v", err)
	}
	gogiv1.RegisterIndexServiceServer(indexServer, impl.NewIndexServer(chromaDBClient, pool, store))

	// add the health server
	healthServer := health.NewServer()
	healthpb.RegisterHealthServer(indexServer, healthServer)
	healthServer.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)

	log.Infof("%s server running on: %s", SERVICE_NAME, PORT)
	if err := indexServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
