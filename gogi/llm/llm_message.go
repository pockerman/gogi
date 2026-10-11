package llm

const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	// RoleTool is the role of a message that gives the model the result of a tool call
	RoleTool = "tool"
)

// LLMMessage specify a message to an LLM model.
// An assistant message has the tools the model called, if any, in ToolCalls; a tool
// message has the result of the call ToolCallId in Content
type LLMMessage struct {
	Role       string
	Content    string
	ToolCalls  []LLMToolCall
	ToolCallId string
	Name       string
}
