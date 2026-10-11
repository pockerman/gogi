package main

import (
	gogiv1 "gogi/gogi/gogi/v1"
	"gogi/gogi/observability"
	"gogi/gogi/services/llm_sessions/impl"
	"gogi/gogi/sessions"

	"gogi/gogi/storage/postgres"
	"gogi/gogi/utils"
	"net"

	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

func main() {

	const SERVICE_NAME string = "llm-sessions"

	PORT := utils.GetEnv("GOGI_LLMS_SESSION_PORT", ":50059")
	PROTOCOL := utils.GetEnv("GOGI_LLMS_SESSION_PROTOCOL", "tcp")

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

	config, assembler, summary, err := impl.ConfigFromEnv()
	if err != nil {
		log.Fatalf("invalid configuration: %v", err)
	}
	store := postgres.NewGogiLLMSessionRepository(pool)
	assembler.Store = store

	// summaries are written by a model of the model service
	if summary.Model != "" {
		llmsAddr := utils.GetEnv("GOGI_LLM_SERVICE_ADDR", "llms:50057")
		conn, err := grpc.NewClient(llmsAddr, append(observability.DialOptions(),
			grpc.WithTransportCredentials(insecure.NewCredentials()))...)
		if err != nil {
			log.Fatalf("failed to connect to the model service at %s: %v", llmsAddr, err)
		}
		defer conn.Close()
		assembler.Summarizer = &sessions.ModelSummarizer{Runner: gogiv1.NewLLMModelServerClient(conn),
			Provider: summary.Provider, ModelID: summary.Model, Timeout: summary.Timeout}
		log.Infof("Summaries are written by %s/%s", summary.Provider, summary.Model)
	} else {
		log.Infof("No summary model configured; the summarize strategy truncates instead")
	}

	// every RPC gets a span, continuing the trace of the request
	telemetry, closeTelemetry, err := observability.FromEnv(SERVICE_NAME)
	if err != nil {
		log.Fatalf("failed to set up telemetry: %v", err)
	}
	defer closeTelemetry()

	grpcServer := grpc.NewServer(telemetry.ServerOptions()...)
	gogiv1.RegisterLLMSessionServerServer(grpcServer, impl.NewLLMSessionServer(store, assembler, config))

	// add the health server
	healthServer := health.NewServer()
	healthpb.RegisterHealthServer(grpcServer, healthServer)
	healthServer.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)

	log.Infof("%s server running on: %s", SERVICE_NAME, PORT)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
