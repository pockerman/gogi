package providers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"

	gogiv1 "gogi/gogi/gogi/v1"
	LLM "gogi/gogi/llm"
)

// ErrInvalidRequest is returned when the messages or the tools of a request are invalid
var ErrInvalidRequest = errors.New("invalid request")

// toolNamePattern are the function names all the providers accept
var toolNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// noParametersSchema is the parameters of a tool that takes no arguments
var noParametersSchema = json.RawMessage(`{"type":"object","properties":{}}`)

func invalidRequest(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidRequest, fmt.Sprintf(format, args...))
}

// convertLLMMessages converts and checks the messages of a request
func convertLLMMessages(messages []*gogiv1.LLMMessage) ([]LLM.LLMMessage, error) {

	converted := make([]LLM.LLMMessage, len(messages))
	for i, msg := range messages {
		message := LLM.LLMMessage{
			Role:       msg.GetRole(),
			Content:    msg.GetContent(),
			ToolCallId: msg.GetToolCallId(),
			Name:       msg.GetName(),
		}

		switch message.Role {
		case LLM.RoleSystem, LLM.RoleUser:
		case LLM.RoleAssistant:
			for _, call := range msg.GetToolCalls() {
				if call.GetId() == "" || call.GetFunction().GetName() == "" {
					return nil, invalidRequest("message %d: a tool call needs an id and a function name", i)
				}
				message.ToolCalls = append(message.ToolCalls, *LLM.NewLLMToolCall(call.GetId(),
					LLM.ToolTypeFunction, call.GetFunction().GetName(), call.GetFunction().GetArguments()))
			}
		case LLM.RoleTool:
			if message.ToolCallId == "" {
				return nil, invalidRequest("message %d: a tool message needs the tool_call_id of the call it answers", i)
			}
		default:
			return nil, invalidRequest("message %d: unsupported role %q, the roles are system, user, assistant and tool",
				i, message.Role)
		}

		if message.Role != LLM.RoleAssistant && len(msg.GetToolCalls()) > 0 {
			return nil, invalidRequest("message %d: only assistant messages have tool calls", i)
		}

		if len(msg.GetParts()) > 0 && message.Role != LLM.RoleUser {
			return nil, invalidRequest("message %d: only user messages have parts, e.g. images", i)
		}
		for j, part := range msg.GetParts() {
			converted, err := convertPart(part)
			if err != nil {
				return nil, invalidRequest("message %d, part %d: %v", i, j, err)
			}
			message.Parts = append(message.Parts, converted)
		}
		converted[i] = message
	}
	return converted, nil
}

// convertResponseFormat converts and checks the response format of a request: none, "text", "json_object",
// or "json_schema" with a JSON schema of an object
func convertResponseFormat(format *gogiv1.ResponseFormat) (*LLM.ResponseFormat, error) {

	switch format.GetType() {
	case "", "text":
		return nil, nil
	case LLM.ResponseFormatJSONObject:
		return &LLM.ResponseFormat{Type: LLM.ResponseFormatJSONObject}, nil
	case LLM.ResponseFormatJSONSchema:
		var schema map[string]any
		if err := json.Unmarshal([]byte(format.GetSchemaJson()), &schema); err != nil {
			return nil, invalidRequest("the response format's schema is not a JSON object: %v", err)
		}
		if schemaType, ok := schema["type"]; ok && schemaType != "object" {
			return nil, invalidRequest("the response format's schema must describe an object, not %v", schemaType)
		}
		return &LLM.ResponseFormat{Type: LLM.ResponseFormatJSONSchema, Schema: json.RawMessage(format.GetSchemaJson())}, nil
	default:
		return nil, invalidRequest("unsupported response format %q, the formats are text, %s and %s",
			format.GetType(), LLM.ResponseFormatJSONObject, LLM.ResponseFormatJSONSchema)
	}
}

// convertPart converts and checks a part of a message's content
func convertPart(part *gogiv1.ContentPart) (LLM.ContentPart, error) {

	image := part.GetImage()
	if image == nil {
		if part.GetText() == "" {
			return LLM.ContentPart{}, errors.New("a part is text or an image")
		}
		return LLM.ContentPart{Text: part.GetText()}, nil
	}

	converted := &LLM.Image{Data: image.GetData(), MediaType: image.GetMediaType()}
	if reference := image.GetDocument(); reference != nil {
		if reference.GetIndexName() == "" || reference.GetDocumentId() == "" {
			return LLM.ContentPart{}, errors.New("an image's document needs its index_name and document_id")
		}
		converted.Data = nil
		converted.Document = &LLM.DocumentReference{IndexName: reference.GetIndexName(), DocumentID: reference.GetDocumentId()}
	} else if len(converted.Data) == 0 {
		return LLM.ContentPart{}, errors.New("an image needs its data or a document")
	}
	if converted.Document == nil {
		if err := checkImage(converted); err != nil {
			return LLM.ContentPart{}, err
		}
	}
	return LLM.ContentPart{Image: converted}, nil
}

// checkImage works out an image's media type if it's not given, and checks every provider takes it
func checkImage(image *LLM.Image) error {
	if image.MediaType == "" {
		image.MediaType = http.DetectContentType(image.Data)
	}
	if !LLM.ImageMediaTypes[image.MediaType] {
		return fmt.Errorf("unsupported image type %q, the types are image/jpeg, image/png, image/gif and image/webp",
			image.MediaType)
	}
	return nil
}

// convertTools converts and checks the tools of a request
func convertTools(tools []*gogiv1.ToolDefinition) ([]LLM.LLMToolDefinition, error) {

	converted := make([]LLM.LLMToolDefinition, 0, len(tools))
	names := make(map[string]bool, len(tools))
	for i, tool := range tools {
		if tool.GetType() != "" && tool.GetType() != LLM.ToolTypeFunction {
			return nil, invalidRequest("tool %d: unsupported type %q, tools are of type %q",
				i, tool.GetType(), LLM.ToolTypeFunction)
		}

		function := tool.GetFunction()
		if !toolNamePattern.MatchString(function.GetName()) {
			return nil, invalidRequest("tool %d: the name %q must be 1 to 64 letters, digits, '_' or '-'",
				i, function.GetName())
		}
		if names[function.GetName()] {
			return nil, invalidRequest("tool %d: the name %q is used by another tool", i, function.GetName())
		}
		names[function.GetName()] = true

		parameters, err := toolParameters(function.GetParametersJson())
		if err != nil {
			return nil, invalidRequest("tool %q: %v", function.GetName(), err)
		}

		converted = append(converted, LLM.LLMToolDefinition{
			Name:        function.GetName(),
			Description: function.GetDescription(),
			Parameters:  parameters,
		})
	}
	return converted, nil
}

// toolParameters checks that the parameters of a tool are the JSON Schema of an object.
// A schema without a type is given the type object, and no schema means no parameters
func toolParameters(parametersJSON string) (json.RawMessage, error) {

	if parametersJSON == "" {
		return noParametersSchema, nil
	}

	var schema map[string]any
	if err := json.Unmarshal([]byte(parametersJSON), &schema); err != nil || schema == nil {
		return nil, fmt.Errorf("the parameters must be a JSON Schema object")
	}

	schemaType, ok := schema["type"]
	if !ok {
		schema["type"] = "object"
		return json.Marshal(schema)
	}
	if schemaType != "object" {
		return nil, fmt.Errorf("the parameters must be the schema of an object, not of type %v", schemaType)
	}
	return json.RawMessage(parametersJSON), nil
}

// ToGRPCToolCalls converts the tool calls of a model to their protobuf messages
func ToGRPCToolCalls(toolCalls []LLM.LLMToolCall) []*gogiv1.ToolCall {

	converted := make([]*gogiv1.ToolCall, 0, len(toolCalls))
	for _, call := range toolCalls {
		converted = append(converted, &gogiv1.ToolCall{
			Id:   call.Id,
			Type: call.ToolType,
			Function: &gogiv1.ToolCallFunction{
				Name:      call.Function.Name,
				Arguments: call.Function.Arguments,
			},
		})
	}
	return converted
}
