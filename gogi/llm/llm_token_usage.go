package llm

type TokenUsage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

func NewTokenUsage(promptTokens, completeTokens, totalTokens int) TokenUsage {
	return TokenUsage{PromptTokens: promptTokens, CompletionTokens: completeTokens, TotalTokens: totalTokens}
}
