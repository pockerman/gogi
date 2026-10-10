package providers

import (
	"bufio"
	"bytes"
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
	Messages            []llm.LLMMessage         `json:"messages"`
	MaxCompletionTokens int                      `json:"max_completion_tokens,omitempty"`
	Temperature         *float32                 `json:"temperature,omitempty"`
	TopP                *float32                 `json:"top_p,omitempty"`
	Stream              bool                     `json:"stream,omitempty"`
	StreamOptions       *openAIChatStreamOptions `json:"stream_options,omitempty"`
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
	Id       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
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
	Usage openAIUsage `json:"usage"`
}

// openAIChatChunk is a single server-sent event of a streamed response
type openAIChatChunk struct {
	Model   string `json:"model"`
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
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

func (provider *OpenAILLMModelProvider) Run(messages []llm.LLMMessage,
	config llm.LLMModelConfig) (llm.LLModelResponse, error) {

	body, err := provider.preparePayload(messages, config, false)
	if err != nil {
		return llm.LLModelResponse{}, err
	}

	resp, err := provider.doRequest(body)
	if err != nil {
		return llm.LLModelResponse{}, err
	}
	defer resp.Body.Close()

	var result openAIChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return llm.LLModelResponse{}, fmt.Errorf("openai: failed to decode response: %w", err)
	}

	if len(result.Choices) == 0 {
		return llm.LLModelResponse{}, fmt.Errorf("openai: response contained no choices")
	}

	choice := result.Choices[0]
	content := ""
	if choice.Message.Content != nil {
		content = *choice.Message.Content
	} else if choice.Message.Refusal != nil {
		content = *choice.Message.Refusal
	}

	toolCalls := make([]llm.LLMToolCall, 0, len(choice.Message.ToolCalls))
	for _, call := range choice.Message.ToolCalls {
		toolCalls = append(toolCalls,
			*llm.NewLLMToolCall(call.Id, call.Type, call.Function.Name, call.Function.Arguments))
	}

	tokenUsage := llm.NewTokenUsage(result.Usage.PromptTokens,
		result.Usage.CompletionTokens, result.Usage.TotalTokens)

	response := llm.NewLLMModelResponse(
		provider.Name(),
		result.Model,
		content,
		choice.FinishReason,
		tokenUsage,
		toolCalls,
	)

	Log.Debugf("Provider response %+v", response)
	return *response, nil
}

func (provider *OpenAILLMModelProvider) RunStream(
	messages []llm.LLMMessage,
	config llm.LLMModelConfig,
	stream grpc.ServerStreamingServer[gogiv1.LLMStreamChunkResponse],
) error {

	body, err := provider.preparePayload(messages, config, true)
	if err != nil {
		return err
	}

	resp, err := provider.doRequest(body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	model := config.ModelName
	var finishReason *string
	var usage *openAIUsage

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
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
			return fmt.Errorf("openai: failed to decode stream chunk: %w", err)
		}

		if chunk.Model != "" {
			model = chunk.Model
		}
		if chunk.Usage != nil {
			usage = chunk.Usage
		}

		for _, choice := range chunk.Choices {
			if choice.FinishReason != nil {
				finishReason = choice.FinishReason
			}
			if choice.Delta.Content == "" {
				continue
			}
			if err := stream.Send(&gogiv1.LLMStreamChunkResponse{
				Token: choice.Delta.Content,
				Model: model,
			}); err != nil {
				return err
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("openai: failed to read stream: %w", err)
	}

	final := &gogiv1.LLMStreamChunkResponse{
		Model:        model,
		FinishReason: finishReason,
	}
	if usage != nil {
		final.Usage = &gogiv1.TokenUsage{
			PromptTokens:     int32(usage.PromptTokens),
			CompletionTokens: int32(usage.CompletionTokens),
			TotalTokens:      int32(usage.TotalTokens),
		}
	}

	return stream.Send(final)
}

// doRequest sends the payload to the Chat Completions endpoint and
// returns the response if the status is 200 OK
func (provider *OpenAILLMModelProvider) doRequest(body []byte) (*http.Response, error) {

	url := provider.baseURL + openAIChatCompletionsPath
	request, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("openai: failed to create request: %w", err)
	}

	if err := provider.prepareHeaders(request); err != nil {
		return nil, err
	}

	resp, err := provider.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("openai: request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, parseOpenAIError(resp)
	}

	return resp, nil
}

func parseOpenAIError(resp *http.Response) error {

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))

	var apiErr openAIErrorResponse
	if err := json.Unmarshal(raw, &apiErr); err == nil && apiErr.Error.Message != "" {
		return fmt.Errorf("openai: API error (status %d, type %s): %s",
			resp.StatusCode, apiErr.Error.Type, apiErr.Error.Message)
	}

	return fmt.Errorf("openai: API error (status %d): %s", resp.StatusCode, strings.TrimSpace(string(raw)))
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
		Messages:            messages,
		MaxCompletionTokens: config.MaxTokens,
		Stream:              stream,
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

func (provider *OpenAILLMModelProvider) EstimateCost(tokens []string, model string) (float64, error) {
	return 0.0, fmt.Errorf("openai: cost estimation is not supported yet")
}
