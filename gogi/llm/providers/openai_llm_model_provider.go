package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	gogiv1 "gogi/gogi/gogi/v1"
	"gogi/gogi/llm"

	Log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
)

const (
	// the base URL includes the API version, as in the OpenAI SDKs
	openAIBaseURL             = "https://api.openai.com/v1"
	openAIChatCompletionsPath = "/chat/completions"
	openAIRequestTimeout      = 5 * time.Minute
)

// OpenAILLMModelProvider provider for OpenAI's LLMs via the Chat Completions
// API, and for any server with an OpenAI-compatible API, e.g. vLLM, TGI or Ollama
type OpenAILLMModelProvider struct {
	apiKey       string
	apiKeySource APIKeySource
	baseURL      string
	httpClient   *http.Client
	name         string
}

// APIKeySource returns the API key to send with a request. It is called for every
// request, so a key rotated in a credential store is used without a restart
type APIKeySource func() (string, error)

func NewOpenAILLMModelProvider(apiKey string) *OpenAILLMModelProvider {
	return NewOpenAICompatibleLLMModelProvider("openai", apiKey, openAIBaseURL)
}

// NewOpenAICompatibleLLMModelProvider creates a provider for the OpenAI-compatible
// API at baseURL, which includes the API version, e.g. http://localhost:8000/v1.
// name is the provider name reported in the responses. apiKey may be empty for
// servers that do not require authentication
func NewOpenAICompatibleLLMModelProvider(name, apiKey, baseURL string) *OpenAILLMModelProvider {
	return &OpenAILLMModelProvider{
		apiKey:     apiKey,
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{Timeout: openAIRequestTimeout},
		name:       name,
	}
}

func (provider *OpenAILLMModelProvider) Name() string {
	return provider.name
}

func (provider *OpenAILLMModelProvider) SetApiKey(apiKey string) {
	provider.apiKey = apiKey
}

// SetAPIKeySource makes the provider get the API key from source for every
// request, instead of using a fixed key
func (provider *OpenAILLMModelProvider) SetAPIKeySource(source APIKeySource) {
	provider.apiKeySource = source
}

// SetBaseURL overrides the API base URL, e.g. for Azure/OpenAI-compatible
// gateways or tests. The base URL includes the API version, e.g. https://api.openai.com/v1
func (provider *OpenAILLMModelProvider) SetBaseURL(baseURL string) {
	provider.baseURL = strings.TrimRight(baseURL, "/")
}

// openAIChatRequest is the Chat Completions request body
type openAIChatRequest struct {
	Model               string                   `json:"model"`
	Messages            []openAIMessage          `json:"messages"`
	Tools               []openAITool             `json:"tools,omitempty"`
	MaxCompletionTokens int                      `json:"max_completion_tokens,omitempty"`
	Temperature         *float32                 `json:"temperature,omitempty"`
	TopP                *float32                 `json:"top_p,omitempty"`
	Stream              bool                     `json:"stream,omitempty"`
	StreamOptions       *openAIChatStreamOptions `json:"stream_options,omitempty"`
}

// openAIMessage is a message of the conversation. The content of an assistant
// message that only calls tools is null
type openAIMessage struct {
	Role       string           `json:"role"`
	Content    *string          `json:"content"`
	Name       string           `json:"name,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallId string           `json:"tool_call_id,omitempty"`
}

type openAITool struct {
	Type     string             `json:"type"`
	Function openAIToolFunction `json:"function"`
}

type openAIToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

type openAIChatStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type openAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type openAIToolCall struct {
	Id       string                 `json:"id"`
	Type     string                 `json:"type"`
	Function openAIToolCallFunction `json:"function"`
}

type openAIToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// openAIToolCallDelta is a part of a tool call in a streamed response: the first
// part of a call has its id and name, and the parts of its arguments follow
type openAIToolCallDelta struct {
	Index    int                    `json:"index"`
	Id       string                 `json:"id"`
	Type     string                 `json:"type"`
	Function openAIToolCallFunction `json:"function"`
}

// openAIChatChunk is a single server-sent event of a streamed response
type openAIChatChunk struct {
	Model   string `json:"model"`
	Choices []struct {
		Delta struct {
			Content   string                `json:"content"`
			Refusal   string                `json:"refusal"`
			ToolCalls []openAIToolCallDelta `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *openAIUsage `json:"usage"`
}

type openAIErrorResponse struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    any    `json:"code"`
	} `json:"error"`
}

// Run returns the complete response. The request is streamed under the hood and
// accumulated, as for RunStream, so a call that stalls is noticed while the model
// is still generating
func (provider *OpenAILLMModelProvider) Run(ctx context.Context, messages []llm.LLMMessage,
	config llm.LLMModelConfig) (llm.LLModelResponse, error) {

	result, err := provider.streamChat(ctx, messages, config, nil)
	if err != nil {
		return llm.LLModelResponse{}, err
	}

	content := result.content.String()
	if content == "" {
		content = result.refusal.String()
	}

	response := llm.NewLLMModelResponse(
		provider.Name(),
		result.model,
		content,
		result.finishReason,
		result.tokenUsage(),
		result.toolCalls.toolCalls(),
	)

	Log.Debugf("Provider response %+v", response)
	return *response, nil
}

func (provider *OpenAILLMModelProvider) RunStream(
	messages []llm.LLMMessage,
	config llm.LLMModelConfig,
	stream grpc.ServerStreamingServer[gogiv1.LLMStreamChunkResponse],
) error {

	result, err := provider.streamChat(stream.Context(), messages, config, func(token, model string) error {
		return stream.Send(&gogiv1.LLMStreamChunkResponse{
			Token: token,
			Model: model,
		})
	})
	if err != nil {
		return err
	}

	final := &gogiv1.LLMStreamChunkResponse{
		Model:     result.model,
		ToolCalls: ToGRPCToolCalls(result.toolCalls.toolCalls()),
	}
	if result.finishReason != "" {
		final.FinishReason = &result.finishReason
	}
	if result.usage != nil {
		usage := result.tokenUsage()
		final.Usage = &gogiv1.TokenUsage{
			PromptTokens:     int32(usage.PromptTokens),
			CompletionTokens: int32(usage.CompletionTokens),
			TotalTokens:      int32(usage.TotalTokens),
		}
	}

	return stream.Send(final)
}

// openAIStreamResult is a streamed response, put together
type openAIStreamResult struct {
	model        string
	content      strings.Builder
	refusal      strings.Builder
	finishReason string
	usage        *openAIUsage
	toolCalls    *openAIToolCallAccumulator
}

func (result *openAIStreamResult) tokenUsage() llm.TokenUsage {
	if result.usage == nil {
		return llm.TokenUsage{}
	}
	return llm.NewTokenUsage(result.usage.PromptTokens, result.usage.CompletionTokens, result.usage.TotalTokens)
}

// openAIChatResponse is the non-streaming Chat Completions response body
type openAIChatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Content   *string          `json:"content"`
			Refusal   *string          `json:"refusal"`
			ToolCalls []openAIToolCall `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *openAIUsage `json:"usage"`
}

// decodeResponse puts together a non-streaming response
func (result *openAIStreamResult) decodeResponse(body io.Reader) error {
	var response openAIChatResponse
	if err := json.NewDecoder(body).Decode(&response); err != nil {
		return fmt.Errorf("openai: failed to decode response: %w", err)
	}
	if len(response.Choices) == 0 {
		return fmt.Errorf("openai: response contained no choices")
	}

	if response.Model != "" {
		result.model = response.Model
	}
	result.usage = response.Usage

	choice := response.Choices[0]
	result.finishReason = choice.FinishReason
	if choice.Message.Content != nil {
		result.content.WriteString(*choice.Message.Content)
	}
	if choice.Message.Refusal != nil {
		result.refusal.WriteString(*choice.Message.Refusal)
	}
	for i, call := range choice.Message.ToolCalls {
		result.toolCalls.add([]openAIToolCallDelta{{Index: i, Id: call.Id, Type: call.Type, Function: call.Function}})
	}
	return nil
}

// streamChat sends a streaming Chat Completions request, calls onToken for every
// content delta (if not nil) and returns the accumulated response. Every line the
// server sends, including keep-alive comments, tells the stall watchdog the call is alive
func (provider *OpenAILLMModelProvider) streamChat(ctx context.Context, messages []llm.LLMMessage,
	config llm.LLMModelConfig, onToken func(token, model string) error) (*openAIStreamResult, error) {

	body, err := provider.preparePayload(messages, config, true)
	if err != nil {
		return nil, err
	}

	resp, err := provider.doRequest(ctx, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	result := &openAIStreamResult{model: config.ModelName, toolCalls: newOpenAIToolCallAccumulator()}

	// some OpenAI-compatible servers ignore stream and answer with the whole response
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		if err := result.decodeResponse(resp.Body); err != nil {
			return nil, err
		}
		if content := result.content.String(); onToken != nil && content != "" {
			if err := onToken(content, result.model); err != nil {
				return nil, err
			}
		}
		return result, nil
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		touch(ctx)

		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}

		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}

		var chunk openAIChatChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return nil, fmt.Errorf("openai: failed to decode stream chunk: %w", err)
		}

		if chunk.Model != "" {
			result.model = chunk.Model
		}
		if chunk.Usage != nil {
			result.usage = chunk.Usage
		}

		for _, choice := range chunk.Choices {
			if choice.FinishReason != nil {
				result.finishReason = *choice.FinishReason
			}
			result.toolCalls.add(choice.Delta.ToolCalls)
			result.refusal.WriteString(choice.Delta.Refusal)
			if choice.Delta.Content == "" {
				continue
			}
			result.content.WriteString(choice.Delta.Content)
			if onToken != nil {
				if err := onToken(choice.Delta.Content, result.model); err != nil {
					return nil, err
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, &ProviderError{Err: fmt.Errorf("openai: failed to read stream: %w", err)}
	}
	return result, nil
}

// doRequest sends the payload to the Chat Completions endpoint and
// returns the response if the status is 200 OK
func (provider *OpenAILLMModelProvider) doRequest(ctx context.Context, body []byte) (*http.Response, error) {

	url := provider.baseURL + openAIChatCompletionsPath
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("openai: failed to create request: %w", err)
	}

	if err := provider.prepareHeaders(request); err != nil {
		return nil, err
	}

	resp, err := provider.httpClient.Do(request)
	if err != nil {
		return nil, &ProviderError{Err: fmt.Errorf("openai: request failed: %w", err)}
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, parseOpenAIError(resp)
	}

	return resp, nil
}

func parseOpenAIError(resp *http.Response) error {

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	providerErr := &ProviderError{StatusCode: resp.StatusCode, RetryAfter: parseRetryAfter(resp.Header)}

	var apiErr openAIErrorResponse
	if err := json.Unmarshal(raw, &apiErr); err == nil && apiErr.Error.Message != "" {
		providerErr.Err = fmt.Errorf("openai: API error (status %d, type %s): %s",
			resp.StatusCode, apiErr.Error.Type, apiErr.Error.Message)
		return providerErr
	}

	providerErr.Err = fmt.Errorf("openai: API error (status %d): %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	return providerErr
}

func (provider *OpenAILLMModelProvider) prepareHeaders(request *http.Request) error {

	request.Header.Set("Content-Type", "application/json")

	apiKey := provider.apiKey
	if provider.apiKeySource != nil {
		var err error
		if apiKey, err = provider.apiKeySource(); err != nil {
			return fmt.Errorf("openai: failed to get the API key: %w", err)
		}
	}

	if apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+apiKey)
	}
	return nil
}

func (provider *OpenAILLMModelProvider) preparePayload(messages []llm.LLMMessage,
	config llm.LLMModelConfig, stream bool) ([]byte, error) {

	payload := openAIChatRequest{
		Model:               config.ModelName,
		Messages:            toOpenAIMessages(messages),
		MaxCompletionTokens: config.MaxTokens,
		Stream:              stream,
	}

	for _, tool := range config.Tools {
		payload.Tools = append(payload.Tools, openAITool{
			Type: llm.ToolTypeFunction,
			Function: openAIToolFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.Parameters,
			},
		})
	}

	// zero values mean "not set"; let the API apply its defaults
	if config.Temperature != 0 {
		payload.Temperature = &config.Temperature
	}
	if config.TopP != 0 {
		payload.TopP = &config.TopP
	}
	if stream {
		payload.StreamOptions = &openAIChatStreamOptions{IncludeUsage: true}
	}

	return json.Marshal(payload)
}

func toOpenAIMessages(messages []llm.LLMMessage) []openAIMessage {

	converted := make([]openAIMessage, 0, len(messages))
	for _, message := range messages {
		content := message.Content
		converted = append(converted, openAIMessage{
			Role:       message.Role,
			Content:    &content,
			ToolCallId: message.ToolCallId,
		})

		last := &converted[len(converted)-1]
		// the API does not take the name of a tool message
		if message.Role != llm.RoleTool {
			last.Name = message.Name
		}
		if message.Role == llm.RoleAssistant && len(message.ToolCalls) > 0 && message.Content == "" {
			last.Content = nil
		}
		for _, call := range message.ToolCalls {
			last.ToolCalls = append(last.ToolCalls, openAIToolCall{
				Id:       call.Id,
				Type:     llm.ToolTypeFunction,
				Function: openAIToolCallFunction{Name: call.Function.Name, Arguments: call.Function.Arguments},
			})
		}
	}
	return converted
}

// openAIToolCallAccumulator puts together the tool calls of a streamed response
type openAIToolCallAccumulator struct {
	calls map[int]*llm.LLMToolCall
	order []int
}

func newOpenAIToolCallAccumulator() *openAIToolCallAccumulator {
	return &openAIToolCallAccumulator{calls: make(map[int]*llm.LLMToolCall)}
}

func (accumulator *openAIToolCallAccumulator) add(deltas []openAIToolCallDelta) {
	for _, delta := range deltas {
		call, ok := accumulator.calls[delta.Index]
		if !ok {
			call = llm.NewLLMToolCall("", llm.ToolTypeFunction, "", "")
			accumulator.calls[delta.Index] = call
			accumulator.order = append(accumulator.order, delta.Index)
		}
		if delta.Id != "" {
			call.Id = delta.Id
		}
		if delta.Function.Name != "" {
			call.Function.Name = delta.Function.Name
		}
		call.Function.Arguments += delta.Function.Arguments
	}
}

// toolCalls returns the tool calls in the order the model made them
func (accumulator *openAIToolCallAccumulator) toolCalls() []llm.LLMToolCall {
	toolCalls := make([]llm.LLMToolCall, 0, len(accumulator.order))
	for _, index := range accumulator.order {
		toolCalls = append(toolCalls, *accumulator.calls[index])
	}
	return toolCalls
}

func (provider *OpenAILLMModelProvider) EstimateCost(tokens []string, model string) (float64, error) {
	return 0.0, fmt.Errorf("openai: cost estimation is not supported yet")
}
