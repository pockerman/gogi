package providers

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	gogiv1 "gogi/gogi/gogi/v1"
	"gogi/gogi/llm"
)

const quizSchema = `{"type":"object","properties":{"questions":{"type":"array","items":{"type":"string"}}},"required":["questions"]}`

// anthropicStructuredStream is an SSE stream of an answer given as the response tool's input
var anthropicStructuredStream = []string{
	`event: message_start
data: {"type":"message_start","message":{"id":"msg_3","type":"message","role":"assistant","model":"claude-opus-5-5","content":[],"stop_reason":null,"usage":{"input_tokens":5,"output_tokens":1}}}`,
	`event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_9","name":"structured_response","input":{}}}`,
	`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"questions\":"}}`,
	`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"[\"What is a shape function?\"]}"}}`,
	`event: content_block_stop
data: {"type":"content_block_stop","index":0}`,
	`event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":9}}`,
	`event: message_stop
data: {"type":"message_stop"}`,
}

func schemaFormat() *llm.ResponseFormat {
	return &llm.ResponseFormat{Type: llm.ResponseFormatJSONSchema, Schema: json.RawMessage(quizSchema)}
}

func TestConvertResponseFormat(t *testing.T) {
	tests := []struct {
		name    string
		format  *gogiv1.ResponseFormat
		want    string
		invalid bool
	}{
		{"none", nil, "", false},
		{"text", &gogiv1.ResponseFormat{Type: "text"}, "", false},
		{"any object", &gogiv1.ResponseFormat{Type: "json_object"}, llm.ResponseFormatJSONObject, false},
		{"a schema", &gogiv1.ResponseFormat{Type: "json_schema", SchemaJson: quizSchema}, llm.ResponseFormatJSONSchema, false},
		{"a schema that isn't JSON", &gogiv1.ResponseFormat{Type: "json_schema", SchemaJson: "{"}, "", true},
		{"a schema of an array", &gogiv1.ResponseFormat{Type: "json_schema", SchemaJson: `{"type":"array"}`}, "", true},
		{"an unknown type", &gogiv1.ResponseFormat{Type: "xml"}, "", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			format, err := convertResponseFormat(test.format)
			if test.invalid {
				if !errors.Is(err, ErrInvalidRequest) {
					t.Errorf("err = %v, want ErrInvalidRequest", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got := ""
			if format != nil {
				got = format.Type
			}
			if got != test.want {
				t.Errorf("type = %q, want %q", got, test.want)
			}
		})
	}
}

func TestAnthropicAsksForAJSONAnswerWithItsOnlyTool(t *testing.T) {
	provider := &AnthropicLLMModelProvider{}

	params, err := provider.preparePayload([]llm.LLMMessage{{Role: "user", Content: "Quiz me"}},
		llm.LLMModelConfig{ModelName: "claude-opus-5-5", ResponseFormat: schemaFormat()})
	if err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(params)
	var result struct {
		Tools []struct {
			Name        string         `json:"name"`
			InputSchema map[string]any `json:"input_schema"`
		} `json:"tools"`
		ToolChoice struct {
			Type string `json:"type"`
			Name string `json:"name"`
		} `json:"tool_choice"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Tools) != 1 || result.Tools[0].Name != anthropicResponseTool || result.Tools[0].InputSchema["properties"] == nil {
		t.Errorf("tools = %+v, want only the response tool with the schema", result.Tools)
	}
	if result.ToolChoice.Type != "tool" || result.ToolChoice.Name != anthropicResponseTool {
		t.Errorf("tool_choice = %+v, want the response tool", result.ToolChoice)
	}
}

func TestAnthropicResponseFormatCannotBeCombinedWithTools(t *testing.T) {
	provider := &AnthropicLLMModelProvider{}

	_, err := provider.preparePayload([]llm.LLMMessage{{Role: "user", Content: "Quiz me"}},
		llm.LLMModelConfig{ModelName: "claude-opus-5-5", ResponseFormat: schemaFormat(),
			Tools: []llm.LLMToolDefinition{{Name: "calendar"}}})

	if !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("err = %v, want ErrInvalidRequest", err)
	}
}

func TestAnthropicJSONAnswerIsTheContent(t *testing.T) {
	provider := newTestAnthropicProvider(t, serveAnthropicStream(t, anthropicStructuredStream))

	response, err := provider.Run(context.Background(), []llm.LLMMessage{{Role: "user", Content: "Quiz me"}},
		llm.LLMModelConfig{ModelName: "claude-opus-5-5", ResponseFormat: schemaFormat()})
	if err != nil {
		t.Fatal(err)
	}

	if response.Content != `{"questions":["What is a shape function?"]}` {
		t.Errorf("content = %q, want the response tool's input", response.Content)
	}
	if len(response.ToolCalls) != 0 || response.FinishReason != "stop" {
		t.Errorf("tool calls = %+v, finish reason = %q; want none and stop", response.ToolCalls, response.FinishReason)
	}
}

func TestAnthropicStreamsTheJSONAnswer(t *testing.T) {
	provider := newTestAnthropicProvider(t, serveAnthropicStream(t, anthropicStructuredStream))

	stream := &fakeChunkStream{}
	err := provider.RunStream([]llm.LLMMessage{{Role: "user", Content: "Quiz me"}},
		llm.LLMModelConfig{ModelName: "claude-opus-5-5", ResponseFormat: schemaFormat()}, stream)
	if err != nil {
		t.Fatal(err)
	}

	answer := ""
	for _, chunk := range stream.chunks {
		answer += chunk.Token
	}
	final := stream.chunks[len(stream.chunks)-1]
	if answer != `{"questions":["What is a shape function?"]}` || len(final.GetToolCalls()) != 0 || final.GetFinishReason() != "stop" {
		t.Errorf("answer = %q, final chunk = %+v", answer, final)
	}
}

func TestOpenAIAsksForAJSONAnswer(t *testing.T) {
	provider := &OpenAILLMModelProvider{}

	body, err := provider.preparePayload(nil, llm.LLMModelConfig{ModelName: "gpt-4o-mini", ResponseFormat: schemaFormat()}, false)
	if err != nil {
		t.Fatal(err)
	}

	var result struct {
		ResponseFormat struct {
			Type       string `json:"type"`
			JSONSchema struct {
				Name   string         `json:"name"`
				Schema map[string]any `json:"schema"`
			} `json:"json_schema"`
		} `json:"response_format"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	if result.ResponseFormat.Type != "json_schema" || result.ResponseFormat.JSONSchema.Schema["properties"] == nil {
		t.Errorf("response_format = %+v", result.ResponseFormat)
	}

	body, _ = provider.preparePayload(nil, llm.LLMModelConfig{ModelName: "gpt-4o-mini"}, false)
	var plain map[string]any
	if json.Unmarshal(body, &plain); plain["response_format"] != nil {
		t.Errorf("a request without a response format has one: %s", body)
	}
}
