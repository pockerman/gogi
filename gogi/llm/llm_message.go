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
	// a user message's text and images, in order, after Content if it's set
	Parts []ContentPart
}

// ContentPart is a part of a message's content: text, or an image if Image is set
type ContentPart struct {
	Text  string
	Image *Image
}

// Image is an image in a message: its bytes and media type, or, until the model service reads it, a
// document of the platform's
type Image struct {
	Data      []byte
	MediaType string
	Document  *DocumentReference
}

type DocumentReference struct {
	IndexName  string
	DocumentID string
}

// ImageMediaTypes are the media types of the images every provider takes
var ImageMediaTypes = map[string]bool{"image/jpeg": true, "image/png": true, "image/gif": true, "image/webp": true}
