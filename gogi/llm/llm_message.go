package llm

// LLMMessage specify a message to an LLM model
type LLMMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
