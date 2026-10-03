package providers

import (
	"encoding/json"
	"fmt"
	gogiv1 "gogi/gogi/gogi/v1"
	"gogi/gogi/llm"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/grpc"
)

// fakeChunkStream collects the chunks sent by RunStream
type fakeChunkStream struct {
	grpc.ServerStream
	chunks []*gogiv1.LLMStreamChunkResponse
}

func (s *fakeChunkStream) Send(chunk *gogiv1.LLMStreamChunkResponse) error {
	s.chunks = append(s.chunks, chunk)
	return nil
}

func newTestOpenAIProvider(t *testing.T, handler http.HandlerFunc) *OpenAILLMModelProvider {
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	provider := NewOpenAILLMModelProvider("test-key")
	provider.SetBaseURL(server.URL)
	return provider
}

func TestOpenAIPreparePayload(t *testing.T) {
	provider := &OpenAILLMModelProvider{}

	messages := []llm.LLMMessage{
		{Role: "user", Content: "Hello"},
	}

	config := llm.LLMModelConfig{
		ModelName:   "gpt-4o-mini",
		MaxTokens:   100,
		Temperature: 0.5,
	}

	body, err := provider.preparePayload(messages, config, false)
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]interface{}
	json.Unmarshal(body, &result)

	if result["model"] != "gpt-4o-mini" {
		t.Errorf("wrong model")
	}
	if result["max_completion_tokens"] != float64(100) {
		t.Errorf("wrong max_completion_tokens %v", result["max_completion_tokens"])
	}
	if result["temperature"] != float64(0.5) {
		t.Errorf("wrong temperature %v", result["temperature"])
	}
	if _, ok := result["top_p"]; ok {
		t.Errorf("top_p should be omitted when unset")
	}
	if _, ok := result["stream"]; ok {
		t.Errorf("stream should be omitted for non-streaming requests")
	}
}

func TestOpenAIPreparePayloadStream(t *testing.T) {
	provider := &OpenAILLMModelProvider{}

	body, err := provider.preparePayload(nil, llm.LLMModelConfig{ModelName: "gpt-4o-mini"}, true)
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]interface{}
	json.Unmarshal(body, &result)

	if result["stream"] != true {
		t.Errorf("stream not set")
	}
	options, _ := result["stream_options"].(map[string]interface{})
	if options["include_usage"] != true {
		t.Errorf("stream_options.include_usage not set")
	}
}

func TestOpenAIPrepareHeaders(t *testing.T) {
	provider := &OpenAILLMModelProvider{
		apiKey: "test-key",
	}

	req, _ := http.NewRequest("POST", "/", nil)
	provider.prepareHeaders(req)

	if req.Header.Get("Authorization") != "Bearer test-key" {
		t.Errorf("API key not set")
	}
}

func TestOpenAIRun(t *testing.T) {
	provider := newTestOpenAIProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != openAIChatCompletionsPath {
			t.Errorf("wrong path %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("wrong authorization header")
		}
		fmt.Fprint(w, `{
			"model": "gpt-4o-mini-2024-07-18",
			"choices": [{
				"message": {
					"role": "assistant",
					"content": "Hi there",
					"tool_calls": [{
						"id": "call_1",
						"type": "function",
						"function": {"name": "calendar", "arguments": "{\"date\":\"2026-06-14\"}"}
					}]
				},
				"finish_reason": "tool_calls"
			}],
			"usage": {"prompt_tokens": 5, "completion_tokens": 7, "total_tokens": 12}
		}`)
	})

	response, err := provider.Run([]llm.LLMMessage{{Role: "user", Content: "Hello"}},
		llm.LLMModelConfig{ModelName: "gpt-4o-mini"})
	if err != nil {
		t.Fatal(err)
	}

	if response.Provider != "openai" || response.Model != "gpt-4o-mini-2024-07-18" {
		t.Errorf("wrong provider/model %s/%s", response.Provider, response.Model)
	}
	if response.Content != "Hi there" || response.FinishReason != "tool_calls" {
		t.Errorf("wrong content/finish reason %q/%q", response.Content, response.FinishReason)
	}
	if response.TokenUsage != llm.NewTokenUsage(5, 7, 12) {
		t.Errorf("wrong token usage %+v", response.TokenUsage)
	}
	if len(response.ToolCalls) != 1 || response.ToolCalls[0].Function.Name != "calendar" {
		t.Errorf("wrong tool calls %+v", response.ToolCalls)
	}
}

func TestOpenAIRunAPIError(t *testing.T) {
	provider := newTestOpenAIProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error": {"message": "Incorrect API key provided", "type": "invalid_request_error"}}`)
	})

	_, err := provider.Run(nil, llm.LLMModelConfig{ModelName: "gpt-4o-mini"})
	if err == nil || !strings.Contains(err.Error(), "Incorrect API key provided") {
		t.Errorf("expected API error, got %v", err)
	}
}

func TestOpenAIRunStream(t *testing.T) {
	provider := newTestOpenAIProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		events := []string{
			`{"model":"gpt-4o-mini","choices":[{"delta":{"role":"assistant","content":""},"finish_reason":null}]}`,
			`{"model":"gpt-4o-mini","choices":[{"delta":{"content":"Hello"},"finish_reason":null}]}`,
			`{"model":"gpt-4o-mini","choices":[{"delta":{"content":" world"},"finish_reason":null}]}`,
			`{"model":"gpt-4o-mini","choices":[{"delta":{},"finish_reason":"stop"}]}`,
			`{"model":"gpt-4o-mini","choices":[],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`,
			`[DONE]`,
		}
		for _, event := range events {
			fmt.Fprintf(w, "data: %s\n\n", event)
		}
	})

	stream := &fakeChunkStream{}
	err := provider.RunStream([]llm.LLMMessage{{Role: "user", Content: "Hello"}},
		llm.LLMModelConfig{ModelName: "gpt-4o-mini"}, stream)
	if err != nil {
		t.Fatal(err)
	}

	if len(stream.chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(stream.chunks))
	}
	if stream.chunks[0].Token != "Hello" || stream.chunks[1].Token != " world" {
		t.Errorf("wrong tokens %q %q", stream.chunks[0].Token, stream.chunks[1].Token)
	}

	final := stream.chunks[2]
	if final.GetFinishReason() != "stop" {
		t.Errorf("wrong finish reason %q", final.GetFinishReason())
	}
	if final.GetUsage().GetTotalTokens() != 5 {
		t.Errorf("wrong usage %+v", final.GetUsage())
	}
}
