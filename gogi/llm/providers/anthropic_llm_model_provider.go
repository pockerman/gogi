package providers

import (
	"context"
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
			toolCalls = append(toolCalls,
				*llm.NewLLMToolCall(variant.ID, string(variant.Type), variant.Name, string(variant.Input)))
		}
	}

	response := llm.NewLLMModelResponse(
		provider.Name(),
		string(message.Model),
		content.String(),
		string(message.StopReason),
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
	finishReason := string(message.StopReason)

	return stream.Send(&gogiv1.LLMStreamChunkResponse{
		Model:        string(message.Model),
		FinishReason: &finishReason,
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
// them in the conversation
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
		switch msg.Role {
		case "system":
			params.System = append(params.System, anthropic.TextBlockParam{Text: msg.Content})
		case "user":
			params.Messages = append(params.Messages,
				anthropic.NewUserMessage(anthropic.NewTextBlock(msg.Content)))
		case "assistant":
			params.Messages = append(params.Messages,
				anthropic.NewAssistantMessage(anthropic.NewTextBlock(msg.Content)))
		default:
			return anthropic.MessageNewParams{}, fmt.Errorf("anthropic: unsupported message role %q", msg.Role)
		}
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

func (provider *AnthropicLLMModelProvider) EstimateCost(tokens []string, model string) (float64, error) {
	return 0.0, fmt.Errorf("anthropic: cost estimation is not supported yet")
}
