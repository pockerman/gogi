package llm

import "encoding/json"

const (
	// ResponseFormatJSONSchema asks for a JSON object that matches a schema
	ResponseFormatJSONSchema = "json_schema"
	// ResponseFormatJSONObject asks for any JSON object
	ResponseFormatJSONObject = "json_object"
)

// ResponseFormat asks the model to answer with a JSON object, matching Schema if the type is
// ResponseFormatJSONSchema. The response's content is the object
type ResponseFormat struct {
	Type   string
	Schema json.RawMessage
}

type LLMModelConfig struct {
	ModelName   string
	MaxTokens   int
	Temperature float32
	TopP        float32
	// Tools are the tools the model may call
	Tools []LLMToolDefinition
	// ResponseFormat, if not nil, asks for a JSON answer
	ResponseFormat *ResponseFormat
}
