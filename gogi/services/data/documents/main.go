package main

import (
	"gogi/gogi/services/data/documents/impl"
	"gogi/gogi/utils"
	"net"

	gogiv1 "gogi/gogi/gogi/v1"

	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
)

func main() {

	const PORT string = ":50054"
	const PROTOCOL string = "tcp"
	const SERVICE_NAME string = "documents"

	// initialize the logger for the platform
	utils.InitLogger()

	lis, err := net.Listen(PROTOCOL, PORT)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	documentServer := grpc.NewServer()
	gogiv1.RegisterDocumentServerServer(documentServer, &impl.DocumentsServer{})

	log.Infof("%s server running on: %s", SERVICE_NAME, PORT)
	if err := documentServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
