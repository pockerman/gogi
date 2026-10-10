package main

import (
	"context"
	"gogi/gogi/credentials"
	LLM_PROVIDERS "gogi/gogi/llm/providers"
	"gogi/gogi/services/llms/impl"
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

func buildModelRouterProvider() *LLM_PROVIDERS.LLMProviderRouter {

	anthropicProvider := LLM_PROVIDERS.NewAnthropicLLMModelProvider(utils.GetEnv("ANTHROPIC_API_KEY", ""))
	if anthropicBaseURL := utils.GetEnv("ANTHROPIC_BASE_URL", ""); anthropicBaseURL != "" {
		anthropicProvider.SetBaseURL(anthropicBaseURL)
	}

	openAIProvider := LLM_PROVIDERS.NewOpenAILLMModelProvider(utils.GetEnv("OPENAI_API_KEY", ""))
	if openAIBaseURL := utils.GetEnv("OPENAI_BASE_URL", ""); openAIBaseURL != "" {
		openAIProvider.SetBaseURL(openAIBaseURL)
	}

	providers := map[string]LLM_PROVIDERS.ModelProvider{
		"anthropic": anthropicProvider,
		"openai":    openAIProvider,
	}
	return LLM_PROVIDERS.NewLLMProviderRouter(providers)

}

func main() {

	const SERVICE_NAME string = "llms"

	PORT := utils.GetEnv("GOGI_LLMS_PORT", ":50057")
	PROTOCOL := utils.GetEnv("GOGI_LLMS_PROTOCOL", "tcp")

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

	modelRouter := buildModelRouterProvider()

	// the credentials of registered models, e.g. in AWS Secrets Manager or HashiCorp Vault
	credentialStore, err := credentials.NewCredentialStoreFromEnv(context.Background())
	if err != nil {
		log.Fatalf("failed to create the credential store: %v", err)
	}
	if credentialStore == nil {
		log.Infof("No credential store configured; registered models cannot reference credentials")
	}

	grpcServer := grpc.NewServer()
	gogiv1.RegisterLLMModelServerServer(grpcServer, impl.NewLLMModelServer(chromaDBClient, pool, modelRouter,
		credentialStore))

	// add the health server
	healthServer := health.NewServer()
	healthpb.RegisterHealthServer(grpcServer, healthServer)
	healthServer.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)

	log.Infof("%s server running on: %s", SERVICE_NAME, PORT)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
