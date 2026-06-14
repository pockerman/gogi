package providers

import (
	"bytes"
	"encoding/json"
	"fmt"
	gogiv1 "gogi/gogi/gogi/v1"
	"gogi/gogi/llm"
	"net/http"

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
}

func NewAnthropicLLMModelProvider(apiKey string) *AnthropicLLMModelProvider {
	return &AnthropicLLMModelProvider{
		apiKey:     apiKey,
		httpClient: &http.Client{},
	}
}

func (provider *AnthropicLLMModelProvider) SetApiKey(apiKey string) {
	provider.apiKey = apiKey
}

func (provider *AnthropicLLMModelProvider) Run(messages []llm.LLMMessage,
	config llm.LLMModelConfig) (llm.LLModelResponse, error) {

	body, _ := provider.preparePayload(messages, config)

	url := anthropicBaseURL + anthropicMessagesPath
	request, _ := http.NewRequest("POST",
		url,
		bytes.NewBuffer(body))

	provider.prepareHeaders(request)

	resp, err := provider.httpClient.Do(request)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	fmt.Println(result)

	return llm.LLModelResponse{
		Provider: "ANTHROPIC",
		Response: result,
	}, nil

}

func (provider *AnthropicLLMModelProvider) RunStream(
	messages []llm.LLMMessage,
	config llm.LLMModelConfig,
	stream grpc.ServerStreamingServer[gogiv1.LLMStreamChunkResponse],
) error {
	return nil
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
