package main

import (
	"gogi/gogi/services/data/indexes/impl"
	"gogi/gogi/storage/vector_storage"
	"gogi/gogi/utils"
	"net"

	gogiv1 "gogi/gogi/gogi/v1"

	"strconv"

	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
)

func main() {

	const SERVICE_NAME string = "indexes"

	PORT := utils.GetEnv("GOGI_INDEXES_PORT", ":50055")
	PROTOCOL := utils.GetEnv("GOGI_INDEXES_PROTOCOL", "tcp")

	// initialize the logger for the platform
	utils.InitLogger()

	vectorDBConnectionDetails, _ := utils.GetVectorDBStorageConnectionDetails()
	port, _ := strconv.Atoi(vectorDBConnectionDetails.GOGI_VECTOR_DB_PORT)
	chromaDBClient := vector_storage.NewChromaDBClient(vectorDBConnectionDetails.GOGI_VECTOR_DB_HOST,
		port)

	lis, err := net.Listen(PROTOCOL, PORT)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	indexServer := grpc.NewServer()

	gogiv1.RegisterIndexServiceServer(indexServer, impl.NewIndexServer(chromaDBClient))

	log.Infof("%s server running on: %s", SERVICE_NAME, PORT)
	if err := indexServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
