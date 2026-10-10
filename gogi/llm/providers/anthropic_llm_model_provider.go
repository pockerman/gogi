package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	gogiv1 "gogi/gogi/gogi/v1"
	"gogi/gogi/llm"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	Log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
)

const (
	anthropicDefaultModel     = "claude-opus-5-5"
	anthropicDefaultMaxTokens = 16000
	anthropicRequestTimeout   = 10 * time.Minute
)

// AnthropicLLMModelProvider provider for
// Anthropic's LLMs via the Messages API
type AnthropicLLMModelProvider struct {
	apiKey  string
	baseURL string
	client  anthropic.Client
	name    string
}

func NewAnthropicLLMModelProvider(apiKey string) *AnthropicLLMModelProvider {
	provider := &AnthropicLLMModelProvider{
		apiKey: apiKey,
		name:   "anthropic",
	}
	provider.buildClient()
	return provider
}

func (provider *AnthropicLLMModelProvider) Name() string {
	return provider.name
}

func (provider *AnthropicLLMModelProvider) SetApiKey(apiKey string) {
	provider.apiKey = apiKey
	provider.buildClient()
}

// SetBaseURL overrides the API base URL, e.g. for gateways or tests
func (provider *AnthropicLLMModelProvider) SetBaseURL(baseURL string) {
	provider.baseURL = strings.TrimRight(baseURL, "/")
	provider.buildClient()
}

func (provider *AnthropicLLMModelProvider) buildClient() {
	opts := []option.RequestOption{
		option.WithAPIKey(provider.apiKey),
		option.WithRequestTimeout(anthropicRequestTimeout),
	}
	if provider.baseURL != "" {
		opts = append(opts, option.WithBaseURL(provider.baseURL))
	}
	provider.client = anthropic.NewClient(opts...)
}

// Run returns the complete response. The request is streamed under the
// hood and accumulated, so large max_tokens values do not hit HTTP timeouts
func (provider *AnthropicLLMModelProvider) Run(messages []llm.LLMMessage,
	config llm.LLMModelConfig) (llm.LLModelResponse, error) {

	message, err := provider.streamMessage(messages, config, nil)
	if err != nil {
		return llm.LLModelResponse{}, err
	}

	content := strings.Builder{}
	toolCalls := make([]llm.LLMToolCall, 0)
	for _, block := range message.Content {
		switch variant := block.AsAny().(type) {
		case anthropic.TextBlock:
			content.WriteString(variant.Text)
		case anthropic.ToolUseBlock:
			toolCalls = append(toolCalls, anthropicToolCall(variant))
		}
	}

	response := llm.NewLLMModelResponse(
		provider.Name(),
		string(message.Model),
		content.String(),
		anthropicFinishReason(message.StopReason),
		anthropicTokenUsage(message.Usage),
		toolCalls,
	)

	Log.Debugf("Provider response %+v", response)
	return *response, nil
}

func (provider *AnthropicLLMModelProvider) RunStream(
	messages []llm.LLMMessage,
	config llm.LLMModelConfig,
	stream grpc.ServerStreamingServer[gogiv1.LLMStreamChunkResponse],
) error {

	model := config.ModelName
	message, err := provider.streamMessage(messages, config, func(text string) error {
		return stream.Send(&gogiv1.LLMStreamChunkResponse{
			Token: text,
			Model: model,
		})
	})
	if err != nil {
		return err
	}

	usage := anthropicTokenUsage(message.Usage)
	finishReason := anthropicFinishReason(message.StopReason)

	toolCalls := make([]llm.LLMToolCall, 0)
	for _, block := range message.Content {
		if toolUse, ok := block.AsAny().(anthropic.ToolUseBlock); ok {
			toolCalls = append(toolCalls, anthropicToolCall(toolUse))
		}
	}

	return stream.Send(&gogiv1.LLMStreamChunkResponse{
		Model:        string(message.Model),
		FinishReason: &finishReason,
		ToolCalls:    ToGRPCToolCalls(toolCalls),
		Usage: &gogiv1.TokenUsage{
			PromptTokens:     int32(usage.PromptTokens),
			CompletionTokens: int32(usage.CompletionTokens),
			TotalTokens:      int32(usage.TotalTokens),
		},
	})
}

// streamMessage sends a streaming Messages API request, calls onText for
// every text delta (if not nil) and returns the accumulated message
func (provider *AnthropicLLMModelProvider) streamMessage(messages []llm.LLMMessage,
	config llm.LLMModelConfig, onText func(string) error) (anthropic.Message, error) {

	params, err := provider.preparePayload(messages, config)
	if err != nil {
		return anthropic.Message{}, err
	}

	stream := provider.client.Messages.NewStreaming(context.Background(), params)
	defer stream.Close()

	message := anthropic.Message{}
	for stream.Next() {
		event := stream.Current()
		if err := message.Accumulate(event); err != nil {
			return anthropic.Message{}, fmt.Errorf("anthropic: failed to accumulate stream event: %w", err)
		}

		if onText == nil {
			continue
		}

		if delta, ok := event.AsAny().(anthropic.ContentBlockDeltaEvent); ok {
			if text, ok := delta.Delta.AsAny().(anthropic.TextDelta); ok && text.Text != "" {
				if err := onText(text.Text); err != nil {
					return anthropic.Message{}, err
				}
			}
		}
	}

	if err := stream.Err(); err != nil {
		return anthropic.Message{}, wrapAnthropicError(err)
	}

	if message.StopReason == anthropic.StopReasonRefusal {
		Log.Warnf("anthropic: request refused (category %q): %s",
			message.StopDetails.Category, message.StopDetails.Explanation)
	}

	return message, nil
}

func wrapAnthropicError(err error) error {

	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		return fmt.Errorf("anthropic: API error (status %d): %w", apiErr.StatusCode, err)
	}

	return fmt.Errorf("anthropic: request failed: %w", err)
}

// anthropicToolCall maps a tool use block to a tool call in the platform's format
func anthropicToolCall(toolUse anthropic.ToolUseBlock) llm.LLMToolCall {
	arguments := string(toolUse.Input)
	if arguments == "" {
		arguments = "{}"
	}
	return *llm.NewLLMToolCall(toolUse.ID, llm.ToolTypeFunction, toolUse.Name, arguments)
}

// anthropicFinishReason maps the stop reason of the Messages API to the finish reason
// of the platform's format, the one of OpenAI, so that applications check one set of values
func anthropicFinishReason(stopReason anthropic.StopReason) string {
	switch stopReason {
	case anthropic.StopReasonEndTurn, anthropic.StopReasonStopSequence:
		return "stop"
	case anthropic.StopReasonMaxTokens, anthropic.StopReasonModelContextWindowExceeded:
		return "length"
	case anthropic.StopReasonToolUse:
		return "tool_calls"
	case anthropic.StopReasonRefusal:
		return "content_filter"
	default:
		return string(stopReason)
	}
}

// anthropicTokenUsage maps the Messages API usage to TokenUsage. Cached
// input tokens are reported separately by the API, so they are added to
// the prompt tokens
func anthropicTokenUsage(usage anthropic.Usage) llm.TokenUsage {
	promptTokens := int(usage.InputTokens + usage.CacheCreationInputTokens + usage.CacheReadInputTokens)
	completionTokens := int(usage.OutputTokens)
	return llm.NewTokenUsage(promptTokens, completionTokens, promptTokens+completionTokens)
}

// preparePayload builds the Messages API request. Messages with role
// "system" go to the top-level system prompt, as the API does not accept
// them in the conversation. Tool calls are tool_use blocks of the assistant's
// message, and tool results are tool_result blocks of a user message
func (provider *AnthropicLLMModelProvider) preparePayload(messages []llm.LLMMessage,
	config llm.LLMModelConfig) (anthropic.MessageNewParams, error) {

	model := config.ModelName
	if model == "" {
		model = anthropicDefaultModel
	}

	maxTokens := config.MaxTokens
	if maxTokens <= 0 {
		maxTokens = anthropicDefaultMaxTokens
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: int64(maxTokens),
	}

	for _, msg := range messages {
		if msg.Role == llm.RoleSystem {
			params.System = append(params.System, anthropic.TextBlockParam{Text: msg.Content})
			continue
		}

		role, blocks, err := anthropicContent(msg)
		if err != nil {
			return anthropic.MessageNewParams{}, err
		}

		// consecutive messages of a role are one message, e.g. the results of
		// the tool calls of an assistant's message
		last := len(params.Messages) - 1
		if last >= 0 && params.Messages[last].Role == role {
			params.Messages[last].Content = append(params.Messages[last].Content, blocks...)
			continue
		}
		params.Messages = append(params.Messages, anthropic.MessageParam{Role: role, Content: blocks})
	}

	for _, tool := range config.Tools {
		toolParam, err := anthropicTool(tool)
		if err != nil {
			return anthropic.MessageNewParams{}, err
		}
		params.Tools = append(params.Tools, anthropic.ToolUnionParam{OfTool: &toolParam})
	}

	// zero values mean "not set"; let the API apply its defaults.
	// Note: newer models (e.g. Claude Opus 4.7+) reject sampling parameters
	if config.Temperature != 0 {
		params.Temperature = anthropic.Float(float64(config.Temperature))
	}
	if config.TopP != 0 {
		params.TopP = anthropic.Float(float64(config.TopP))
	}

	return params, nil
}

// anthropicContent returns the role and the content blocks of a message of the conversation
func anthropicContent(msg llm.LLMMessage) (anthropic.MessageParamRole, []anthropic.ContentBlockParamUnion, error) {

	switch msg.Role {
	case llm.RoleUser:
		return anthropic.MessageParamRoleUser, []anthropic.ContentBlockParamUnion{anthropic.NewTextBlock(msg.Content)}, nil

	case llm.RoleAssistant:
		blocks := make([]anthropic.ContentBlockParamUnion, 0, len(msg.ToolCalls)+1)
		// the API rejects empty text blocks, and a message that only calls tools has no text
		if msg.Content != "" || len(msg.ToolCalls) == 0 {
			blocks = append(blocks, anthropic.NewTextBlock(msg.Content))
		}
		for _, call := range msg.ToolCalls {
			arguments := call.Function.Arguments
			if arguments == "" {
				arguments = "{}"
			}
			if !json.Valid([]byte(arguments)) {
				return "", nil, fmt.Errorf("anthropic: the arguments of tool call %q are not JSON", call.Id)
			}
			blocks = append(blocks, anthropic.NewToolUseBlock(call.Id, json.RawMessage(arguments), call.Function.Name))
		}
		return anthropic.MessageParamRoleAssistant, blocks, nil

	case llm.RoleTool:
		result := anthropic.ToolResultBlockParam{ToolUseID: msg.ToolCallId}
		if msg.Content != "" {
			result.Content = []anthropic.ToolResultBlockParamContentUnion{
				{OfText: &anthropic.TextBlockParam{Text: msg.Content}},
			}
		}
		return anthropic.MessageParamRoleUser, []anthropic.ContentBlockParamUnion{{OfToolResult: &result}}, nil

	default:
		return "", nil, fmt.Errorf("anthropic: unsupported message role %q", msg.Role)
	}
}

// anthropicTool maps a tool to a tool of the Messages API. The JSON Schema of its
// parameters becomes the input schema: properties and required are fields of the
// schema param, and any other keyword, e.g. $defs, is kept as an extra field
func anthropicTool(tool llm.LLMToolDefinition) (anthropic.ToolParam, error) {

	var schema map[string]any
	if len(tool.Parameters) > 0 {
		if err := json.Unmarshal(tool.Parameters, &schema); err != nil {
			return anthropic.ToolParam{}, fmt.Errorf("anthropic: the parameters of tool %q are not a JSON object: %w",
				tool.Name, err)
		}
	}

	inputSchema := anthropic.ToolInputSchemaParam{ExtraFields: make(map[string]any)}
	for keyword, value := range schema {
		switch keyword {
		case "type":
		case "properties":
			inputSchema.Properties = value
		case "required":
			required, ok := value.([]any)
			if !ok {
				return anthropic.ToolParam{}, fmt.Errorf("anthropic: required of tool %q is not a list", tool.Name)
			}
			for _, name := range required {
				if name, ok := name.(string); ok {
					inputSchema.Required = append(inputSchema.Required, name)
				}
			}
		default:
			inputSchema.ExtraFields[keyword] = value
		}
	}

	toolParam := anthropic.ToolParam{Name: tool.Name, InputSchema: inputSchema}
	if tool.Description != "" {
		toolParam.Description = anthropic.String(tool.Description)
	}
	return toolParam, nil
}

func (provider *AnthropicLLMModelProvider) EstimateCost(tokens []string, model string) (float64, error) {
	return 0.0, fmt.Errorf("anthropic: cost estimation is not supported yet")
}
