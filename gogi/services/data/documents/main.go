package main

import (
	"gogi/gogi/services/data/documents/impl"
	"gogi/gogi/storage/postgres"
	"gogi/gogi/utils"
	"net"
	"os"
	"os/signal"
	"syscall"

	gogiv1 "gogi/gogi/gogi/v1"

	"go.temporal.io/sdk/client"

	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

func main() {

	const SERVICE_NAME string = "documents"
	PORT := utils.GetEnv("GOGI_DOCUMENTS_PORT", ":50054")
	PROTOCOL := utils.GetEnv("GOGI_DOCUMENTS_PROTOCOL", "tcp")
	TEMPORAL_HOST := utils.GetEnv("TEMPORAL_HOST", "localhost:7233")

	// 1. initialize the logger for the platform
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

	// 3. Dial Temporal Client
	// This creates the heavyweight client object once per process
	log.Infof("Connecting to Temporal at %s ", TEMPORAL_HOST)
	temporalClient, err := client.Dial(client.Options{
		HostPort: TEMPORAL_HOST,
	})

	if err != nil {
		log.Fatalf("Failed to create Temporal client: %v", err)
	}
	defer temporalClient.Close()

	log.Infof("Connected to Temporal at %s", TEMPORAL_HOST)

	// 3. Initialize gRPC Server
	lis, err := net.Listen(PROTOCOL, PORT)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	// Inject the temporalClient into your DocumentsServer
	documentServer := impl.NewDocumentsServer(temporalClient, pool)

	grpcServer := grpc.NewServer()
	gogiv1.RegisterDocumentServerServer(grpcServer, documentServer)

	// Enable reflection for tools like grpcurl and BloomRPC
	reflection.Register(grpcServer)

	// 4. Graceful Shutdown Handling
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan
		log.Info("Shutting down gRPC server...")
		grpcServer.GracefulStop()
	}()

	log.Infof("%s server running on: %s", SERVICE_NAME, PORT)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
