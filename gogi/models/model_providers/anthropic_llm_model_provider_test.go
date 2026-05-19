package model_providers

import (
	"encoding/json"
	"gogenai/model_service"
	"net/http"
	"testing"
)

func TestPreparePayload(t *testing.T) {
	provider := &AnthropicLLMModelProvider{}

	messages := []model_service.LLMMessage{
		{Role: "user", Content: "Hello"},
	}

	config := model_service.LLMModelConfig{
		ModelName: "claude",
		MaxTokens: 100,
	}

	body, err := provider.preparePayload(messages, config)
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]interface{}
	json.Unmarshal(body, &result)

	if result["model"] != "claude" {
		t.Errorf("wrong model")
	}
}

func TestPrepareHeaders(t *testing.T) {
	provider := &AnthropicLLMModelProvider{
		apiKey: "test-key",
	}

	req, _ := http.NewRequest("POST", "/", nil)
	provider.prepareHeaders(req)

	if req.Header.Get("x-api-key") != "test-key" {
		t.Errorf("API key not set")
	}
}
