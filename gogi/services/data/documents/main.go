package main

import (
	"gogi/gogi/applications"
	"gogi/gogi/services/data/documents/impl"
	"gogi/gogi/storage/objects"
	"gogi/gogi/storage/postgres"
	"gogi/gogi/utils"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	gogiv1 "gogi/gogi/gogi/v1"

	"go.temporal.io/sdk/client"

	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

func main() {

	const SERVICE_NAME string = "documents"
	PORT := utils.GetEnv("GOGI_DOCUMENTS_PORT", ":50054")
	PROTOCOL := utils.GetEnv("GOGI_DOCUMENTS_PROTOCOL", "tcp")
	TEMPORAL_HOST := utils.GetEnv("TEMPORAL_HOST", "localhost:7233")
	MAX_UPLOAD_MB, err := strconv.Atoi(utils.GetEnv("GOGI_DOCUMENTS_MAX_UPLOAD_MB", "100"))
	if err != nil {
		log.Fatalf("GOGI_DOCUMENTS_MAX_UPLOAD_MB is not a number: %v", err)
	}

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
	// the uploaded files are kept in the object store
	store, err := objects.NewStoreFromEnv()
	if err != nil {
		log.Fatalf("failed to set up the object store: %v", err)
	}
	// the embedding models a document is ingested with are the model service's to know
	llmsAddress := utils.GetEnv("GOGI_LLM_SERVICE_ADDR", "llms:50057")
	models, err := impl.ModelServiceCatalog(llmsAddress)
	if err != nil {
		log.Fatalf("failed to connect to the model service at %s: %v", llmsAddress, err)
	}
	documentServer := impl.NewDocumentsServer(temporalClient, pool, store, models, int64(MAX_UPLOAD_MB)*1024*1024)

	// the indexes documents are in are the application's
	grpcServer := grpc.NewServer(applications.ScopeFromEnv("index_name").ServerOptions()...)
	gogiv1.RegisterDocumentServerServer(grpcServer, documentServer)

	// add the health server
	healthServer := health.NewServer()
	healthpb.RegisterHealthServer(grpcServer, healthServer)
	healthServer.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)

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
