package providers

import (
	"encoding/json"
	"gogi/gogi/llm"
	"net/http"
	"testing"
)

func TestPreparePayload(t *testing.T) {
	provider := &AnthropicLLMModelProvider{}

	messages := []llm.LLMMessage{
		{Role: "user", Content: "Hello"},
	}

	config := llm.LLMModelConfig{
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
