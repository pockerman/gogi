package providers

import (
	"encoding/json"
	"fmt"
	"gogi/gogi/llm"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// anthropicTextStream is an SSE stream for a text-only response
var anthropicTextStream = []string{
	`event: message_start
data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5-5","content":[],"stop_reason":null,"usage":{"input_tokens":3,"cache_read_input_tokens":2,"output_tokens":1}}}`,
	`event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
	`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`,
	`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" world"}}`,
	`event: content_block_stop
data: {"type":"content_block_stop","index":0}`,
	`event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":4}}`,
	`event: message_stop
data: {"type":"message_stop"}`,
}

// anthropicToolStream is an SSE stream for a response with a tool call
var anthropicToolStream = []string{
	`event: message_start
data: {"type":"message_start","message":{"id":"msg_2","type":"message","role":"assistant","model":"claude-opus-5-5","content":[],"stop_reason":null,"usage":{"input_tokens":5,"output_tokens":1}}}`,
	`event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
	`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Checking"}}`,
	`event: content_block_stop
data: {"type":"content_block_stop","index":0}`,
	`event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"calendar","input":{}}}`,
	`event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"date\":"}}`,
	`event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"2026-06-14\"}"}}`,
	`event: content_block_stop
data: {"type":"content_block_stop","index":1}`,
	`event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":7}}`,
	`event: message_stop
data: {"type":"message_stop"}`,
}

func newTestAnthropicProvider(t *testing.T, handler http.HandlerFunc) *AnthropicLLMModelProvider {
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	provider := NewAnthropicLLMModelProvider("test-key")
	provider.SetBaseURL(server.URL)
	return provider
}

func serveAnthropicStream(t *testing.T, events []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("wrong path %s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("wrong x-api-key header")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range events {
			fmt.Fprintf(w, "%s\n\n", event)
		}
	}
}

func TestAnthropicPreparePayload(t *testing.T) {
	provider := &AnthropicLLMModelProvider{}

	messages := []llm.LLMMessage{
		{Role: "system", Content: "Be brief"},
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi"},
		{Role: "user", Content: "How are you?"},
	}

	config := llm.LLMModelConfig{
		ModelName:   "claude-haiku-4-5",
		MaxTokens:   100,
		Temperature: 0.5,
	}

	params, err := provider.preparePayload(messages, config)
	if err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(params)
	var result map[string]interface{}
	json.Unmarshal(body, &result)

	if result["model"] != "claude-haiku-4-5" {
		t.Errorf("wrong model %v", result["model"])
	}
	if result["max_tokens"] != float64(100) {
		t.Errorf("wrong max_tokens %v", result["max_tokens"])
	}
	if result["temperature"] != float64(0.5) {
		t.Errorf("wrong temperature %v", result["temperature"])
	}
	if _, ok := result["top_p"]; ok {
		t.Errorf("top_p should be omitted when unset")
	}

	system, _ := result["system"].([]interface{})
	if len(system) != 1 {
		t.Fatalf("expected 1 system block, got %v", result["system"])
	}
	if msgs, _ := result["messages"].([]interface{}); len(msgs) != 3 {
		t.Errorf("expected 3 messages, got %d", len(msgs))
	}
}

func TestAnthropicPreparePayloadDefaults(t *testing.T) {
	provider := &AnthropicLLMModelProvider{}

	params, err := provider.preparePayload(nil, llm.LLMModelConfig{})
	if err != nil {
		t.Fatal(err)
	}

	if params.Model != anthropicDefaultModel || params.MaxTokens != anthropicDefaultMaxTokens {
		t.Errorf("wrong defaults %s/%d", params.Model, params.MaxTokens)
	}
}

func TestAnthropicPreparePayloadUnknownRole(t *testing.T) {
	provider := &AnthropicLLMModelProvider{}

	_, err := provider.preparePayload([]llm.LLMMessage{{Role: "function", Content: "x"}}, llm.LLMModelConfig{})
	if err == nil {
		t.Errorf("expected an error for an unsupported role")
	}
}

func TestAnthropicRun(t *testing.T) {
	provider := newTestAnthropicProvider(t, serveAnthropicStream(t, anthropicToolStream))

	response, err := provider.Run([]llm.LLMMessage{{Role: "user", Content: "Hello"}},
		llm.LLMModelConfig{ModelName: "claude-opus-5-5"})
	if err != nil {
		t.Fatal(err)
	}

	if response.Provider != "anthropic" || response.Model != "claude-opus-5-5" {
		t.Errorf("wrong provider/model %s/%s", response.Provider, response.Model)
	}
	if response.Content != "Checking" || response.FinishReason != "tool_calls" {
		t.Errorf("wrong content/finish reason %q/%q", response.Content, response.FinishReason)
	}
	if response.TokenUsage != llm.NewTokenUsage(5, 7, 12) {
		t.Errorf("wrong token usage %+v", response.TokenUsage)
	}
	if len(response.ToolCalls) != 1 {
		t.Fatalf("wrong tool calls %+v", response.ToolCalls)
	}

	call := response.ToolCalls[0]
	if call.Id != "toolu_1" || call.Function.Name != "calendar" {
		t.Errorf("wrong tool call %+v", call)
	}

	var args map[string]string
	if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil || args["date"] != "2026-06-14" {
		t.Errorf("wrong tool arguments %q", call.Function.Arguments)
	}
}

func TestAnthropicRunAPIError(t *testing.T) {
	provider := newTestAnthropicProvider(t, func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`)
	})

	_, err := provider.Run(nil, llm.LLMModelConfig{ModelName: "claude-opus-5-5"})
	if err == nil || !strings.Contains(err.Error(), "status 401") {
		t.Errorf("expected API error, got %v", err)
	}
}

func TestAnthropicRunStream(t *testing.T) {
	provider := newTestAnthropicProvider(t, serveAnthropicStream(t, anthropicTextStream))

	stream := &fakeChunkStream{}
	err := provider.RunStream([]llm.LLMMessage{{Role: "user", Content: "Hello"}},
		llm.LLMModelConfig{ModelName: "claude-opus-5-5"}, stream)
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
	// 3 input + 2 cache read prompt tokens, 4 output tokens
	if final.GetUsage().GetPromptTokens() != 5 || final.GetUsage().GetTotalTokens() != 9 {
		t.Errorf("wrong usage %+v", final.GetUsage())
	}
}
