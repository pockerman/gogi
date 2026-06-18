package providers

import (
	"encoding/json"
	"time"

	gogiv1 "gogi/gogi/gogi/v1"
	"gogi/gogi/llm"
	"net/http"

	"github.com/gogo/protobuf/proto"
	"github.com/google/uuid"
	Log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
)

const (
	anthropicBaseURL      = "https://api.anthropic.com"
	anthropicMessagesPath = "/v1/messages"
)

// AnthropicLLMModelProvider provider for
// Antropic's LLMs
type AnthropicLLMModelProvider struct {
	apiKey     string
	httpClient *http.Client
	name       string
}

func NewAnthropicLLMModelProvider(apiKey string) *AnthropicLLMModelProvider {
	return &AnthropicLLMModelProvider{
		apiKey:     apiKey,
		httpClient: &http.Client{},
		name:       "anthropic",
	}
}

func (provider *AnthropicLLMModelProvider) Name() string {

	return provider.name
}

func (provider *AnthropicLLMModelProvider) SetApiKey(apiKey string) {
	provider.apiKey = apiKey
}

func (provider *AnthropicLLMModelProvider) Run(messages []llm.LLMMessage,
	config llm.LLMModelConfig) (llm.LLModelResponse, error) {

	//body, _ := provider.preparePayload(messages, config)

	// url := anthropicBaseURL + anthropicMessagesPath
	// request, _ := http.NewRequest("POST",
	// 	url,
	// 	bytes.NewBuffer(body))

	// provider.prepareHeaders(request)

	// resp, err := provider.httpClient.Do(request)
	// if err != nil {
	// 	panic(err)
	// }
	// defer resp.Body.Close()

	// var result map[string]interface{}
	// json.NewDecoder(resp.Body).Decode(&result)

	// fmt.Println(result)

	tokenUsage := llm.NewTokenUsage(10, 20, 30)
	// toolCall := llm.NewLLMToolCall(uuid.New().String(), "Test-Tool", "Calendar", "2026-06-14")
	// respone := llm.NewLLMModelResponse(provider.Name(), config.ModelName,
	// 	"This is the model response",
	// 	"Test-API", tokenUsage, toolCall)

	// Log.Infof("Provider response %s", respone)
	// return *respone, nil

	toolCalls := []llm.LLMToolCall{
		*llm.NewLLMToolCall(
			uuid.New().String(),
			"Test-Tool",
			"Calendar",
			"2026-06-14",
		),
	}

	response := llm.NewLLMModelResponse(
		provider.Name(),
		config.ModelName,
		"This is the model response",
		"Test-API",
		tokenUsage,
		toolCalls,
	)

	Log.Infof("Provider response %s", response)
	return *response, nil

}

func (provider *AnthropicLLMModelProvider) RunStream(
	messages []llm.LLMMessage,
	config llm.LLMModelConfig,
	stream grpc.ServerStreamingServer[gogiv1.LLMStreamChunkResponse],
) error {
	tokens := []string{
		"This ",
		"is ",
		"a ",
		"streamed ",
		"response.",
	}

	for _, token := range tokens {
		err := stream.Send(&gogiv1.LLMStreamChunkResponse{
			Token: token,
			Model: config.ModelName,
		})
		if err != nil {
			return err
		}

		time.Sleep(100 * time.Millisecond)
	}

	return stream.Send(&gogiv1.LLMStreamChunkResponse{
		Model:        config.ModelName,
		FinishReason: proto.String("stop"),
		Usage: &gogiv1.TokenUsage{
			PromptTokens:     10,
			CompletionTokens: int32(len(tokens)),
			TotalTokens:      10 + int32(len(tokens)),
		},
	})
}

func (provider *AnthropicLLMModelProvider) prepareHeaders(request *http.Request) {

	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("x-api-key", provider.apiKey)
	request.Header.Set("anthropic-version", "2023-06-01")

}

func (provider *AnthropicLLMModelProvider) preparePayload(messages []llm.LLMMessage,
	config llm.LLMModelConfig) ([]byte, error) {

	payload := map[string]interface{}{
		"model":       config.ModelName,
		"max_tokens":  config.MaxTokens,
		"temperature": config.Temperature,
		"messages":    messages,
	}

	body, err := json.Marshal(payload)
	return body, err
}

func (provider *AnthropicLLMModelProvider) EstimateCost(tokens []string, model string) (float64, error) {
	return 10.0, nil
}
