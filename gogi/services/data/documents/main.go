package main

import (
	"context"
	"gogi/gogi/applications"
	"gogi/gogi/documents/ingestion"
	"gogi/gogi/services/data/documents/impl"
	"gogi/gogi/storage/objects"
	"gogi/gogi/storage/postgres"
	"gogi/gogi/storage/vector_storage"
	"gogi/gogi/utils"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	gogiv1 "gogi/gogi/gogi/v1"

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

	// 3. the queue the ingestion worker is handed documents through
	queue, err := ingestion.FromEnv(context.Background(), pool)
	if err != nil {
		log.Fatalf("failed to set up the ingestion queue: %v", err)
	}

	// searches and deletes go to the vector store directly
	vectorDB, _ := utils.GetVectorDBStorageConnectionDetails()
	vectorDBPort, _ := strconv.Atoi(vectorDB.GOGI_VECTOR_DB_PORT)
	vectors := vector_storage.NewChromaDBClient(vectorDB.GOGI_VECTOR_DB_HOST, vectorDBPort)

	// 4. Initialize gRPC Server
	lis, err := net.Listen(PROTOCOL, PORT)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

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
	documentServer := impl.NewDocumentsServer(queue, vectors, pool, store, models, int64(MAX_UPLOAD_MB)*1024*1024)

	// the indexes documents are in are the application's
	grpcServer := grpc.NewServer(applications.ScopeFromEnv("index_name").ServerOptions()...)
	gogiv1.RegisterDocumentServerServer(grpcServer, documentServer)

	// add the health server
	healthServer := health.NewServer()
	healthpb.RegisterHealthServer(grpcServer, healthServer)
	healthServer.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)

	// Enable reflection for tools like grpcurl and BloomRPC
	reflection.Register(grpcServer)

	// 5. Graceful Shutdown Handling
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
