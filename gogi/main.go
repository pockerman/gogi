package main

import (
	"fmt"
	"gogenai/gogenai/utils"
	"net"

	genaiv1 "gogenai/genai/v1"

	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
)

type ChatServer struct {
	//genaiv1.UnimplementedChatServiceServer
}

func (s *ChatServer) Chat() (string, error) {
	fmt.Sprintf("This is the message")
}

func main() {

	const PORT string = ":50051"
	const PROTOCOL string = "tcp"

	// initialize the logger for the platform
	utils.InitLogger()

	lis, err := net.Listen(PROTOCOL, PORT)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	server := grpc.NewServer()
	genaiv1.RegisterChatServiceServer(server, &ChatServer{})

	log.Infof("gRPC server running on: %s", PORT)
	if err := server.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
