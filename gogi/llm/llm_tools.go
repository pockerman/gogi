package llm

import "encoding/json"

// ToolTypeFunction is the type of the tools a model can call, and of its tool calls
const ToolTypeFunction = "function"

// LLMToolDefinition is a tool a model may call
type LLMToolDefinition struct {
	Name        string
	Description string
	// Parameters is the JSON Schema of the tool's arguments, an object schema
	Parameters json.RawMessage
}

type LLMToolCallFunction struct {
	Name string
	// Arguments are the arguments of the call, as a JSON object
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
