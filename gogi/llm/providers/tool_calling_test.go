package providers

import (
	"encoding/json"
	"errors"
	"fmt"
	gogiv1 "gogi/gogi/gogi/v1"
	"gogi/gogi/llm"
	"net/http"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

const calendarParameters = `{"type":"object","properties":{"date":{"type":"string"}},"required":["date"]}`

// toolConversation is a conversation in which the model called two tools and got their results
var toolConversation = []llm.LLMMessage{
	{Role: llm.RoleSystem, Content: "Be brief"},
	{Role: llm.RoleUser, Content: "Am I free on 2026-06-14?"},
	{Role: llm.RoleAssistant, ToolCalls: []llm.LLMToolCall{
		*llm.NewLLMToolCall("call_1", llm.ToolTypeFunction, "calendar", `{"date":"2026-06-14"}`),
		*llm.NewLLMToolCall("call_2", llm.ToolTypeFunction, "weather", ""),
	}},
	{Role: llm.RoleTool, ToolCallId: "call_1", Name: "calendar", Content: `{"events":[]}`},
	{Role: llm.RoleTool, ToolCallId: "call_2", Name: "weather", Content: `{"forecast":"sun"}`},
}

var calendarTool = llm.LLMToolDefinition{
	Name: "calendar", Description: "Events on a date", Parameters: json.RawMessage(calendarParameters),
}

func text(value string) *string {
	return &value
}

func toolRequest(messages []*gogiv1.LLMMessage, tools ...*gogiv1.ToolDefinition) *gogiv1.LLMRunRequest {
	request := runRequest("openai", "gpt-4o-mini")
	request.Messages = messages
	request.Tools = tools
	return request
}

func functionTool(name, parametersJSON string) *gogiv1.ToolDefinition {
	return &gogiv1.ToolDefinition{Type: "function",
		Function: &gogiv1.LLMFunctionDefinition{Name: name, Description: "A tool", ParametersJson: parametersJSON}}
}

func TestRequestInput(t *testing.T) {
	content := `{"events":[]}`
	request := toolRequest([]*gogiv1.LLMMessage{
		{Role: "user", Content: text("Am I free?")},
		{Role: "assistant", ToolCalls: []*gogiv1.ToolCall{
			{Id: "call_1", Type: "function", Function: &gogiv1.ToolCallFunction{Name: "calendar", Arguments: "{}"}},
		}},
		{Role: "tool", ToolCallId: text("call_1"), Content: &content},
	}, functionTool("calendar", calendarParameters), functionTool("ping", ""),
		functionTool("lookup", `{"properties":{"id":{"type":"string"}}}`))

	messages, config, err := requestInput(request)
	if err != nil {
		t.Fatal(err)
	}

	if len(messages) != 3 || messages[1].ToolCalls[0].Function.Name != "calendar" ||
		messages[2].ToolCallId != "call_1" || messages[2].Content != content {
		t.Errorf("wrong messages %+v", messages)
	}
	if len(config.Tools) != 3 || string(config.Tools[0].Parameters) != calendarParameters {
		t.Fatalf("wrong tools %+v", config.Tools)
	}
	// a tool without parameters takes none, and a schema without a type is an object
	if string(config.Tools[1].Parameters) != string(noParametersSchema) {
		t.Errorf("wrong parameters of a tool without parameters %s", config.Tools[1].Parameters)
	}
	if !strings.Contains(string(config.Tools[2].Parameters), `"type":"object"`) {
		t.Errorf("wrong parameters of a schema without a type %s", config.Tools[2].Parameters)
	}
}

func TestRequestInputInvalid(t *testing.T) {
	user := []*gogiv1.LLMMessage{{Role: "user", Content: text("Hi")}}

	requests := map[string]*gogiv1.LLMRunRequest{
		"unknown role":            toolRequest([]*gogiv1.LLMMessage{{Role: "function"}}),
		"tool without call id":    toolRequest([]*gogiv1.LLMMessage{{Role: "tool", Content: text("{}")}}),
		"user with tool calls":    toolRequest([]*gogiv1.LLMMessage{{Role: "user", ToolCalls: []*gogiv1.ToolCall{{Id: "c"}}}}),
		"tool call without name":  toolRequest([]*gogiv1.LLMMessage{{Role: "assistant", ToolCalls: []*gogiv1.ToolCall{{Id: "c"}}}}),
		"tool type":               toolRequest(user, &gogiv1.ToolDefinition{Type: "retrieval", Function: &gogiv1.LLMFunctionDefinition{Name: "a"}}),
		"tool name with dots":     toolRequest(user, functionTool("healthcare.book", "")),
		"duplicate tool name":     toolRequest(user, functionTool("a", ""), functionTool("a", "")),
		"parameters not JSON":     toolRequest(user, functionTool("a", "{")),
		"parameters not object":   toolRequest(user, functionTool("a", `{"type":"string"}`)),
		"parameters a JSON array": toolRequest(user, functionTool("a", `[]`)),
	}

	for name, request := range requests {
		if _, _, err := requestInput(request); !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("%s: expected an invalid request, got %v", name, err)
		}
	}
}

func TestRouterInvalidRequest(t *testing.T) {
	router := NewLLMProviderRouter(map[string]ModelProvider{"openai": &fakeProvider{name: "openai"}})

	request := toolRequest([]*gogiv1.LLMMessage{{Role: "user", Content: text("Hi")}}, functionTool("a.b", ""))
	if _, err := router.Run(request); !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("expected an invalid request, got %v", err)
	}
	if err := router.RunStream(request, &fakeChunkStream{}); !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("expected an invalid request, got %v", err)
	}
}

func TestOpenAIPreparePayloadTools(t *testing.T) {
	provider := &OpenAILLMModelProvider{}

	body, err := provider.preparePayload(toolConversation,
		llm.LLMModelConfig{ModelName: "gpt-4o-mini", Tools: []llm.LLMToolDefinition{calendarTool}}, false)
	if err != nil {
		t.Fatal(err)
	}

	var payload struct {
		Messages []map[string]any `json:"messages"`
		Tools    []struct {
			Type     string `json:"type"`
			Function struct {
				Name        string         `json:"name"`
				Description string         `json:"description"`
				Parameters  map[string]any `json:"parameters"`
			} `json:"function"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}

	if len(payload.Tools) != 1 || payload.Tools[0].Type != "function" || payload.Tools[0].Function.Name != "calendar" ||
		payload.Tools[0].Function.Parameters["type"] != "object" {
		t.Errorf("wrong tools %+v", payload.Tools)
	}

	if len(payload.Messages) != 5 {
		t.Fatalf("expected 5 messages, got %v", payload.Messages)
	}
	// an assistant message that only calls tools has null content
	assistant := payload.Messages[2]
	if content, ok := assistant["content"]; !ok || content != nil {
		t.Errorf("the content of the assistant message should be null, got %v", assistant)
	}
	calls, _ := assistant["tool_calls"].([]any)
	if len(calls) != 2 {
		t.Fatalf("wrong tool calls %v", assistant["tool_calls"])
	}
	call := calls[0].(map[string]any)
	function := call["function"].(map[string]any)
	if call["id"] != "call_1" || call["type"] != "function" || function["name"] != "calendar" ||
		function["arguments"] != `{"date":"2026-06-14"}` {
		t.Errorf("wrong tool call %v", call)
	}

	tool := payload.Messages[3]
	if tool["role"] != "tool" || tool["tool_call_id"] != "call_1" || tool["content"] != `{"events":[]}` {
		t.Errorf("wrong tool message %v", tool)
	}
	if _, ok := tool["name"]; ok {
		t.Errorf("a tool message should have no name, got %v", tool)
	}

	// messages without tools are unchanged
	user := payload.Messages[1]
	if user["content"] != "Am I free on 2026-06-14?" || user["tool_calls"] != nil || user["tool_call_id"] != nil {
		t.Errorf("wrong user message %v", user)
	}

	body, _ = provider.preparePayload(toolConversation[:2], llm.LLMModelConfig{ModelName: "gpt-4o-mini"}, false)
	if strings.Contains(string(body), `"tools"`) {
		t.Errorf("tools should be omitted when there are none: %s", body)
	}
}

func TestOpenAIRunStreamToolCalls(t *testing.T) {
	provider := newTestOpenAIProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		events := []string{
			`{"model":"gpt-4o-mini","choices":[{"delta":{"role":"assistant","content":null,"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"calendar","arguments":""}}]},"finish_reason":null}]}`,
			`{"model":"gpt-4o-mini","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"date\":"}}]},"finish_reason":null}]}`,
			`{"model":"gpt-4o-mini","choices":[{"delta":{"tool_calls":[{"index":1,"id":"call_2","type":"function","function":{"name":"weather","arguments":"{}"}}]},"finish_reason":null}]}`,
			`{"model":"gpt-4o-mini","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"2026-06-14\"}"}}]},"finish_reason":null}]}`,
			`{"model":"gpt-4o-mini","choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
			`[DONE]`,
		}
		for _, event := range events {
			fmt.Fprintf(w, "data: %s\n\n", event)
		}
	})

	stream := &fakeChunkStream{}
	err := provider.RunStream([]llm.LLMMessage{{Role: "user", Content: "Am I free?"}},
		llm.LLMModelConfig{ModelName: "gpt-4o-mini", Tools: []llm.LLMToolDefinition{calendarTool}}, stream)
	if err != nil {
		t.Fatal(err)
	}

	// no text, so the only chunk is the last one, with the tool calls
	if len(stream.chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(stream.chunks))
	}
	final := stream.chunks[0]
	if final.GetFinishReason() != "tool_calls" || len(final.GetToolCalls()) != 2 {
		t.Fatalf("wrong last chunk %+v", final)
	}
	call := final.GetToolCalls()[0]
	if call.GetId() != "call_1" || call.GetType() != "function" || call.GetFunction().GetName() != "calendar" ||
		call.GetFunction().GetArguments() != `{"date":"2026-06-14"}` {
		t.Errorf("wrong tool call %+v", call)
	}
	if final.GetToolCalls()[1].GetFunction().GetName() != "weather" {
		t.Errorf("wrong second tool call %+v", final.GetToolCalls()[1])
	}
}

func TestAnthropicPreparePayloadTools(t *testing.T) {
	provider := &AnthropicLLMModelProvider{}

	conversation := append(toolConversation[1:], llm.LLMMessage{Role: llm.RoleUser, Content: "And on Monday?"})
	parameters := `{"type":"object","properties":{"date":{"type":"string"}},"required":["date"],
		"additionalProperties":false}`
	tools := []llm.LLMToolDefinition{
		{Name: "calendar", Description: "Events on a date", Parameters: json.RawMessage(parameters)},
		{Name: "ping", Parameters: noParametersSchema},
	}

	params, err := provider.preparePayload(conversation, llm.LLMModelConfig{ModelName: "claude-opus-5-5", Tools: tools})
	if err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(params)
	var payload struct {
		Messages []struct {
			Role    string           `json:"role"`
			Content []map[string]any `json:"content"`
		} `json:"messages"`
		Tools []map[string]any `json:"tools"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}

	if len(payload.Tools) != 2 {
		t.Fatalf("wrong tools %v", payload.Tools)
	}
	calendar := payload.Tools[0]
	schema, _ := calendar["input_schema"].(map[string]any)
	if calendar["name"] != "calendar" || calendar["description"] != "Events on a date" || schema["type"] != "object" ||
		schema["properties"] == nil || fmt.Sprint(schema["required"]) != "[date]" ||
		schema["additionalProperties"] != false {
		t.Errorf("wrong tool %v", calendar)
	}
	if _, ok := payload.Tools[1]["description"]; ok {
		t.Errorf("a tool without a description should have none, got %v", payload.Tools[1])
	}

	// user, assistant with the tool calls, then one user message with the results and the question
	if len(payload.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %+v", payload.Messages)
	}

	assistant := payload.Messages[1]
	if assistant.Role != "assistant" || len(assistant.Content) != 2 {
		t.Fatalf("wrong assistant message %+v", assistant)
	}
	toolUse := assistant.Content[0]
	if toolUse["type"] != "tool_use" || toolUse["id"] != "call_1" || toolUse["name"] != "calendar" ||
		toolUse["input"].(map[string]any)["date"] != "2026-06-14" {
		t.Errorf("wrong tool use %v", toolUse)
	}
	if input, ok := assistant.Content[1]["input"].(map[string]any); !ok || len(input) != 0 {
		t.Errorf("a call without arguments should have an empty input, got %v", assistant.Content[1])
	}

	results := payload.Messages[2]
	if results.Role != "user" || len(results.Content) != 3 {
		t.Fatalf("wrong results message %+v", results)
	}
	result := results.Content[0]
	if result["type"] != "tool_result" || result["tool_use_id"] != "call_1" ||
		!strings.Contains(fmt.Sprint(result["content"]), `{"events":[]}`) {
		t.Errorf("wrong tool result %v", result)
	}
	if results.Content[1]["tool_use_id"] != "call_2" || results.Content[2]["text"] != "And on Monday?" {
		t.Errorf("wrong results message %+v", results.Content)
	}
}

func TestAnthropicPreparePayloadInvalidArguments(t *testing.T) {
	provider := &AnthropicLLMModelProvider{}

	_, err := provider.preparePayload([]llm.LLMMessage{{Role: llm.RoleAssistant, ToolCalls: []llm.LLMToolCall{
		*llm.NewLLMToolCall("call_1", llm.ToolTypeFunction, "calendar", "{date"),
	}}}, llm.LLMModelConfig{})
	if err == nil {
		t.Errorf("expected an error for arguments that are not JSON")
	}
}

func TestAnthropicRunStreamToolCalls(t *testing.T) {
	provider := newTestAnthropicProvider(t, serveAnthropicStream(t, anthropicToolStream))

	stream := &fakeChunkStream{}
	err := provider.RunStream([]llm.LLMMessage{{Role: "user", Content: "Hello"}},
		llm.LLMModelConfig{ModelName: "claude-opus-5-5", Tools: []llm.LLMToolDefinition{calendarTool}}, stream)
	if err != nil {
		t.Fatal(err)
	}

	final := stream.chunks[len(stream.chunks)-1]
	if final.GetFinishReason() != "tool_calls" || len(final.GetToolCalls()) != 1 {
		t.Fatalf("wrong last chunk %+v", final)
	}
	call := final.GetToolCalls()[0]
	if call.GetId() != "toolu_1" || call.GetType() != "function" || call.GetFunction().GetName() != "calendar" ||
		call.GetFunction().GetArguments() != `{"date":"2026-06-14"}` {
		t.Errorf("wrong tool call %+v", call)
	}
}

func TestAnthropicFinishReason(t *testing.T) {
	reasons := map[string]string{
		"end_turn": "stop", "stop_sequence": "stop", "max_tokens": "length", "tool_use": "tool_calls",
		"refusal": "content_filter", "model_context_window_exceeded": "length", "pause_turn": "pause_turn",
	}
	for stopReason, finishReason := range reasons {
		if got := anthropicFinishReason(anthropic.StopReason(stopReason)); got != finishReason {
			t.Errorf("%s: expected %s, got %s", stopReason, finishReason, got)
		}
	}
}
