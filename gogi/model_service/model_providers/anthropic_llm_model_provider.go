package model_providers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"gogenai/model_service"
	"net/http"
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

func (provider *AnthropicLLMModelProvider) SetApiKey(apiKey string) {
	provider.apiKey = apiKey
}

func (provider *AnthropicLLMModelProvider) Run(messages []model_service.LLMMessage,
	config model_service.LLMModelConfig) model_service.LLModelResponse {

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

	return model_service.LLModelResponse{
		Provider: "ANTHROPIC",
		Response: result,
	}

}

func (provider *AnthropicLLMModelProvider) prepareHeaders(request *http.Request) {

	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("x-api-key", provider.apiKey)
	request.Header.Set("anthropic-version", "2023-06-01")

}

func (provider *AnthropicLLMModelProvider) preparePayload(messages []model_service.LLMMessage,
	config model_service.LLMModelConfig) ([]byte, error) {

	payload := map[string]interface{}{
		"model":       config.ModelName,
		"max_tokens":  config.MaxTokens,
		"temperature": config.Temperature,
		"messages":    messages,
	}

	body, err := json.Marshal(payload)
	return body, err
}
