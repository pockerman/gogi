package main

import (
	"context"
	"gogi/gogi/applications"
	"gogi/gogi/credentials"
	"gogi/gogi/embeddings"
	"gogi/gogi/llm/pricing"
	LLM_PROVIDERS "gogi/gogi/llm/providers"
	"gogi/gogi/observability"
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

	// how failed calls are retried, and the models they fall back to by default
	resilienceConfig, err := LLM_PROVIDERS.ResilienceConfigFromEnv()
	if err != nil {
		log.Fatalf("failed to read the resilience configuration: %v", err)
	}
	modelRouter.SetResilienceConfig(resilienceConfig)
	log.Infof("Retrying failed model calls up to %d times; default fallback models: %v",
		resilienceConfig.Retry.MaxRetries, resilienceConfig.Fallback)

	// stops calling models that keep failing, so requests fall back without waiting
	breaker, err := utils.NewCircuitBreakerFromEnv("GOGI_LLM")
	if err != nil {
		log.Fatalf("failed to create the circuit breaker: %v", err)
	}
	modelRouter.SetCircuitBreaker(breaker)

	// images messages refer to by document are read from the documents service, for the
	// application whose call it is; not through the gateway, which needs an application's key
	documentsAddress := utils.GetEnv("GOGI_DOCUMENT_SERVICE_ADDR", "documents:50054")
	imageFetcher, err := impl.DocumentImageFetcher(documentsAddress)
	if err != nil {
		log.Fatalf("failed to connect to the documents service at %s: %v", documentsAddress, err)
	}
	modelRouter.SetImageFetcher(imageFetcher)

	// the credentials of registered models, e.g. in AWS Secrets Manager or HashiCorp Vault
	credentialStore, err := credentials.NewCredentialStoreFromEnv(context.Background())
	if err != nil {
		log.Fatalf("failed to create the credential store: %v", err)
	}
	if credentialStore == nil {
		log.Infof("No credential store configured; registered models cannot reference credentials")
	}

	// every RPC gets a span, continuing the trace of the request
	telemetry, closeTelemetry, err := observability.FromEnv(SERVICE_NAME)
	if err != nil {
		log.Fatalf("failed to set up telemetry: %v", err)
	}
	defer closeTelemetry()

	// requests can carry images, larger than gRPC's default message size
	// models and credentials are the application's whose call it is
	serverOptions := append(telemetry.ServerOptions(), applications.ScopeFromEnv().ServerOptions()...)
	grpcServer := grpc.NewServer(append(serverOptions, grpc.MaxRecvMsgSize(utils.MaxMessageBytes()))...)
	// the prices of the models, to put a cost on every call
	prices, err := pricing.Load(utils.GetEnv("GOGI_MODEL_PRICES_FILE", ""))
	if err != nil {
		log.Fatalf("failed to load the model prices: %v", err)
	}
	modelServer := impl.NewLLMModelServer(chromaDBClient, pool, modelRouter, credentialStore)
	modelServer.SetTelemetry(telemetry, prices)
	// text embedding models: the ones gogi runs and OpenAI's, and those applications host and
	// register by URL, which may be on private addresses only where the operator allows it
	allowPrivate, _ := strconv.ParseBool(utils.GetEnv("GOGI_EMBEDDINGS_ALLOW_PRIVATE_URLS", "false"))
	modelServer.SetEmbeddingModels(postgres.NewEmbeddingModelsRepository(pool),
		embeddings.URLPolicy{AllowPrivate: allowPrivate}, utils.GetEnv("OPENAI_API_KEY", ""))
	gogiv1.RegisterLLMModelServerServer(grpcServer, modelServer)
	// each application's own keys, e.g. for the built-in providers
	gogiv1.RegisterCredentialServerServer(grpcServer, impl.NewCredentialServer(credentialStore, pool))

	// add the health server
	healthServer := health.NewServer()
	healthpb.RegisterHealthServer(grpcServer, healthServer)
	healthServer.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)

	log.Infof("%s server running on: %s", SERVICE_NAME, PORT)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
