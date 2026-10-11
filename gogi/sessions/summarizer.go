package sessions

import (
	"context"
	"fmt"
	"strings"
	"time"

	gogiv1 "gogi/gogi/gogi/v1"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

// ModelRunner runs a model; the model service's client is one
type ModelRunner interface {
	Run(ctx context.Context, in *gogiv1.LLMRunRequest, opts ...grpc.CallOption) (*gogiv1.LLMRunResponse, error)
}

// ModelSummarizer summarizes with a model of the model service
type ModelSummarizer struct {
	Runner   ModelRunner
	Provider string
	ModelID  string
	Timeout  time.Duration
}

const summaryInstructions = `You maintain a running summary of a conversation between a user and an AI assistant.
The summary replaces the messages it covers, so whoever reads it must be able to carry on the conversation.
Write it in plain prose, in the third person ("the user asked...", "the assistant suggested..."), without a heading.
Keep what later turns may depend on: what the user wants and why, facts the user gave about themselves,
decisions and agreements, open questions, and what tools returned that is still relevant.
Leave out greetings, small talk and anything the conversation has moved past.`

func (s *ModelSummarizer) Model() string {
	return s.Provider + "/" + s.ModelID
}

func (s *ModelSummarizer) Summarize(ctx context.Context, request SummaryRequest) (string, error) {

	var prompt strings.Builder
	prompt.WriteString(summaryInstructions)
	if focus := strings.TrimSpace(request.Focus); focus != "" {
		fmt.Fprintf(&prompt, "\nAbove all, keep: %s.", focus)
	}
	fmt.Fprintf(&prompt, "\nUse at most about %d words.\n\n", max(request.MaxTokens*3/4, 30))
	if request.Previous != "" {
		fmt.Fprintf(&prompt, "The summary so far:\n%s\n\nExtend it with these later messages:\n", request.Previous)
	} else {
		prompt.WriteString("Summarize these messages:\n")
	}
	prompt.WriteString(Transcript(request.Messages))

	if s.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.Timeout)
		defer cancel()
	}
	response, err := s.Runner.Run(ctx, &gogiv1.LLMRunRequest{
		Messages: []*gogiv1.LLMMessage{{Role: RoleUser, Content: proto.String(prompt.String())}},
		Config: &gogiv1.LLMRunRequestConfig{Provider: s.Provider, Model: s.ModelID,
			MaxTokens: int32(max(request.MaxTokens, 64))},
	})
	if err != nil {
		return "", fmt.Errorf("summary model %s: %w", s.Model(), err)
	}
	text := strings.TrimSpace(response.GetContent())
	if text == "" {
		return "", fmt.Errorf("summary model %s returned an empty summary", s.Model())
	}
	return text, nil
}

// Transcript renders messages as text, one per line, with their tool calls and results
func Transcript(messages []Message) string {
	var text strings.Builder
	for _, message := range messages {
		switch {
		case message.Role == RoleTool:
			name := "tool"
			if message.Name != nil && *message.Name != "" {
				name = *message.Name
			}
			fmt.Fprintf(&text, "[%s result] %s\n", name, message.Text())
		case len(message.ToolCalls) > 0:
			calls := make([]string, 0, len(message.ToolCalls))
			for _, call := range message.ToolCalls {
				calls = append(calls, fmt.Sprintf("%s(%s)", call.Name, call.Arguments))
			}
			if content := message.Text(); content != "" {
				fmt.Fprintf(&text, "%s: %s\n", message.Role, content)
			}
			fmt.Fprintf(&text, "%s called: %s\n", message.Role, strings.Join(calls, ", "))
		default:
			fmt.Fprintf(&text, "%s: %s\n", message.Role, message.Text())
		}
	}
	return text.String()
}
