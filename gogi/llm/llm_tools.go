package llm

type LLMToolCallFunction struct {
	Name      string
	Arguments string
}

type LLMToolCall struct {
	Id       string
	ToolType string
	Function LLMToolCallFunction
}

func NewLLMToolCall(id, toolType, name, arguments string) *LLMToolCall {
	return &LLMToolCall{Id: id, ToolType: toolType,
		Function: LLMToolCallFunction{Name: name, Arguments: arguments}}
}
