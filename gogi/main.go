package main

import (
	"gogi/gogi/data/documents"
	"gogi/gogi/utils"
	"net"

	gogiv1 "gogi/gogi/gogi/v1"

	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
)

func main() {

	const PORT string = ":50051"
	const PROTOCOL string = "tcp"

	// initialize the logger for the platform
	utils.InitLogger()

	lis, err := net.Listen(PROTOCOL, PORT)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	documentServer := grpc.NewServer()
	// indexServer := grpc.NewServer()
	// ingestionServer := grpc.NewServer()
	// searchServer := grpc.NewServer()

	gogiv1.RegisterDocumentServerServer(documentServer, &documents.DocumentsServer{})
	// genaiv1.RegisterChatServiceServer(indexServer, &indexes.IndexServer{})
	// genaiv1.RegisterChatServiceServer(ingestionServer, &ingestion.IngestionServer{})
	// genaiv1.RegisterChatServiceServer(searchServer, &search.SearchServer{})

	log.Infof("gRPC server running on: %s", PORT)
	if err := documentServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
