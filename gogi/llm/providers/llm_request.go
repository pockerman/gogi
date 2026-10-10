package providers

import (
	"encoding/json"
	"errors"
	"fmt"
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
		converted[i] = message
	}
	return converted, nil
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
