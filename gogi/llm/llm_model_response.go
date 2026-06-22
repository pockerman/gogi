package llm

type LLModelResponse struct {
	Provider     string
	Model        string
	Content      string
	FinishReason string
	TokenUsage   TokenUsage
	ToolCalls    []LLMToolCall
}

func NewLLMModelResponse(provider, model, content, finishReason string, tokenUsage TokenUsage,
	toolCalls []LLMToolCall) *LLModelResponse {
	return &LLModelResponse{Provider: provider, Model: model, Content: content,
		FinishReason: finishReason, TokenUsage: tokenUsage, ToolCalls: toolCalls}
}
