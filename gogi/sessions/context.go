package sessions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strings"

	log "github.com/sirupsen/logrus"
)

// Context strategies
const (
	// StrategyNone returns a page of the history
	StrategyNone = "none"
	// StrategyTruncate keeps the most recent messages that fit the budget
	StrategyTruncate = "truncate"
	// StrategySummarize replaces the older messages with a summary
	StrategySummarize = "summarize"
	// StrategyHierarchical puts the user's memories first, then the summary, then the recent messages
	StrategyHierarchical = "hierarchical"
)

// Strategies are the context strategies, in order of sophistication
var Strategies = []string{StrategyNone, StrategyTruncate, StrategySummarize, StrategyHierarchical}

// Estimator estimates the tokens of messages from their length; it is only an estimate,
// as every model tokenizes differently
type Estimator struct {
	// CharsPerToken is how many characters make a token, on average
	CharsPerToken float64
	// PerMessage is the overhead of a message: its role and separators
	PerMessage int
}

func DefaultEstimator() Estimator {
	return Estimator{CharsPerToken: 4, PerMessage: 4}
}

// Text estimates the tokens of a text
func (e Estimator) Text(text string) int {
	return int(math.Ceil(float64(len([]rune(text))) / e.CharsPerToken))
}

// Message estimates the tokens of a message, its tool calls included
func (e Estimator) Message(message Message) int {
	chars := message.Text()
	if message.Name != nil {
		chars += *message.Name
	}
	for _, call := range message.ToolCalls {
		chars += call.Name + call.Arguments
	}
	return e.Text(chars) + e.PerMessage
}

func (e Estimator) Messages(messages []Message) int {
	total := 0
	for _, message := range messages {
		total += e.Message(message)
	}
	return total
}

// Summarizer writes a summary of messages, extending a previous summary if there is one
type Summarizer interface {
	Summarize(ctx context.Context, request SummaryRequest) (string, error)
	// Model names the model that summarizes
	Model() string
}

// SummaryRequest asks for a summary of the messages, which follow those the previous
// summary covers, in at most MaxTokens tokens
type SummaryRequest struct {
	Previous  string
	Messages  []Message
	Focus     string
	MaxTokens int
}

// Assembler assembles the context of a model call from a session's messages
type Assembler struct {
	Store     Store
	Estimator Estimator
	// Summarizer writes summaries; nil means summaries are not available
	Summarizer Summarizer
	// MemoryShare and SummaryShare are the shares of the budget memories and the summary
	// may take; the recent messages get the rest, and whatever the others leave
	MemoryShare  float64
	SummaryShare float64
	// ChunkTokens is the most tokens of messages summarized in one call
	ChunkTokens int
}

// ContextRequest asks for the context of a session's next model call
type ContextRequest struct {
	SessionID string
	UserID    string
	Strategy  string
	Budget    int
	// Limit is the most recent messages to keep; 0 means no limit
	Limit int
	Focus string
}

// ContextResult is an assembled context
type ContextResult struct {
	Messages []Message
	Strategy string
	// Omitted is the number of stored messages the context leaves out, or covers with the summary
	Omitted    int
	Summarized int
	Summary    string
	Tokens     int
	Notes      []string
}

// Assemble assembles the context of the session's next model call from its messages,
// oldest first, within the request's budget
func (a *Assembler) Assemble(ctx context.Context, messages []Message, request ContextRequest) (ContextResult, error) {

	result := ContextResult{Strategy: request.Strategy, Notes: make([]string, 0)}

	// the system prompt, the system messages before the conversation starts, is always kept
	pinnedEnd := 0
	for pinnedEnd < len(messages) && messages[pinnedEnd].Role == RoleSystem {
		pinnedEnd++
	}
	pinned, conversation := messages[:pinnedEnd], messages[pinnedEnd:]
	budget := request.Budget - a.Estimator.Messages(pinned)
	if budget < 0 {
		result.Notes = append(result.Notes, "the system messages alone exceed the token budget")
		budget = 0
	}

	var memory *Message
	if request.Strategy == StrategyHierarchical {
		memoryMessage, used, note, err := a.memoryMessage(ctx, request, int(float64(request.Budget)*a.MemoryShare))
		if err != nil {
			return ContextResult{}, err
		}
		if note != "" {
			result.Notes = append(result.Notes, note)
		}
		memory = memoryMessage
		budget -= used
	}

	summaryBudget := 0
	if a.Summarizer != nil && (request.Strategy == StrategySummarize || request.Strategy == StrategyHierarchical) {
		summaryBudget = int(float64(request.Budget) * a.SummaryShare)
	}

	// budget is set aside for the summary only if the conversation does not fit without one
	start, note := a.recentStart(conversation, budget, request.Limit)
	if start > 0 && summaryBudget > 0 {
		start, note = a.recentStart(conversation, max(budget-summaryBudget, 0), request.Limit)
	}
	if note != "" {
		result.Notes = append(result.Notes, note)
	}
	older, recent := conversation[:start], conversation[start:]

	wantsSummary := request.Strategy == StrategySummarize || request.Strategy == StrategyHierarchical
	if wantsSummary && a.Summarizer == nil && len(older) > 0 {
		result.Notes = append(result.Notes, fmt.Sprintf("no summary model is configured; %s leaves the %d older "+
			"messages out instead of summarizing them", request.Strategy, len(older)))
		if request.Strategy == StrategySummarize {
			result.Strategy = StrategyTruncate
		}
	}

	var summary *Message
	if summaryBudget > 0 && len(older) > 0 {
		text, err := a.summary(ctx, request, older, summaryBudget)
		if err != nil {
			log.Warnf("failed to summarize session %s, truncating instead: %v", request.SessionID, err)
			result.Notes = append(result.Notes, "the summary could not be written; the older messages are left out")
			if request.Strategy == StrategySummarize {
				result.Strategy = StrategyTruncate
			}
		} else {
			result.Summary, result.Summarized = text, len(older)
			content := fmt.Sprintf("Summary of the %d earlier messages of this conversation:\n%s", len(older), text)
			summary = &Message{Role: RoleSystem, Content: &content}
		}
	}

	result.Messages = append(result.Messages, pinned...)
	if memory != nil {
		result.Messages = append(result.Messages, *memory)
	}
	if summary != nil {
		result.Messages = append(result.Messages, *summary)
	}
	result.Messages = append(result.Messages, recent...)
	result.Omitted = len(older)
	result.Tokens = a.Estimator.Messages(result.Messages)
	return result, nil
}

// recentStart returns where the most recent messages that fit the budget, and the limit,
// start. The context never starts with tool results, whose calls it would leave out, and
// always keeps the last message, even if it alone exceeds the budget
func (a *Assembler) recentStart(messages []Message, budget, limit int) (int, string) {
	if len(messages) == 0 {
		return 0, ""
	}

	start, used := len(messages), 0
	for start > 0 {
		tokens := a.Estimator.Message(messages[start-1])
		if used+tokens > budget || (limit > 0 && len(messages)-start >= limit) {
			break
		}
		used += tokens
		start--
	}

	note := ""
	if start == len(messages) {
		start = len(messages) - 1
		note = "the latest message alone exceeds the token budget"
	}
	// a tool result without the call it answers is rejected by the providers
	for start < len(messages)-1 && messages[start].Role == RoleTool {
		start++
	}
	return start, note
}

// memoryMessage returns a system message with the user's memories, for every session and
// for this one, that fit the budget; the most recently updated come first
func (a *Assembler) memoryMessage(ctx context.Context, request ContextRequest, budget int) (*Message, int, string, error) {

	all, err := a.Store.GetMemories(ctx, request.UserID, "", nil)
	if err != nil {
		return nil, 0, "", err
	}
	memories := make([]Memory, 0, len(all))
	for _, memory := range all {
		if memory.SessionID == "" || memory.SessionID == request.SessionID {
			memories = append(memories, memory)
		}
	}
	if len(memories) == 0 {
		return nil, 0, "", nil
	}
	sort.SliceStable(memories, func(i, j int) bool { return memories[i].UpdatedAt > memories[j].UpdatedAt })

	header := "Facts saved about the user:"
	lines := make([]string, 0, len(memories))
	used := a.Estimator.Text(header) + a.Estimator.PerMessage
	for _, memory := range memories {
		line := fmt.Sprintf("- %s: %s", memory.Key, memory.Value)
		tokens := a.Estimator.Text(line + "\n")
		if used+tokens > budget {
			break
		}
		lines = append(lines, line)
		used += tokens
	}

	note := ""
	if left := len(memories) - len(lines); left > 0 {
		note = fmt.Sprintf("%d of %d memories do not fit their share of the budget and are left out", left, len(memories))
	}
	if len(lines) == 0 {
		return nil, 0, note, nil
	}
	content := header + "\n" + strings.Join(lines, "\n")
	return &Message{Role: RoleSystem, Content: &content}, used, note, nil
}

// FocusHash identifies what a summary is asked to keep
func FocusHash(focus string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(focus)))
	return hex.EncodeToString(sum[:8])
}

// summary returns the summary of the older messages. The summary is kept and rolled
// forward: when more messages age out, only those are summarized, onto the previous summary
func (a *Assembler) summary(ctx context.Context, request ContextRequest, older []Message, maxTokens int) (string, error) {

	focusHash := FocusHash(request.Focus)
	last := older[len(older)-1].Seq
	cached, err := a.Store.GetSummary(ctx, request.SessionID, focusHash)
	if err != nil {
		return "", err
	}

	previous, pending := "", older
	switch {
	case cached != nil && cached.UpToSeq == last:
		return cached.Text, nil
	case cached != nil && cached.UpToSeq < last:
		// only the messages that aged out since the summary was written
		previous = cached.Text
		pending = make([]Message, 0)
		for _, message := range older {
			if message.Seq > cached.UpToSeq {
				pending = append(pending, message)
			}
		}
	}
	// otherwise there is no summary, or it covers messages that are recent now, e.g. as
	// the budget grew: it is written again from the start

	text := previous
	for _, chunk := range a.chunks(pending) {
		text, err = a.Summarizer.Summarize(ctx, SummaryRequest{Previous: text, Messages: chunk, Focus: request.Focus,
			MaxTokens: maxTokens})
		if err != nil {
			return "", err
		}
	}

	if err := a.Store.SaveSummary(ctx, Summary{SessionID: request.SessionID, FocusHash: focusHash, UpToSeq: last,
		MessageCount: len(older), Text: text, Model: a.Summarizer.Model()}); err != nil {
		log.Warnf("failed to save the summary of session %s: %v", request.SessionID, err)
	}
	return text, nil
}

// chunks splits messages into runs of at most ChunkTokens tokens, so that summarizing a
// long history does not exceed the summary model's context
func (a *Assembler) chunks(messages []Message) [][]Message {
	chunks := make([][]Message, 0)
	current, used := make([]Message, 0), 0
	for _, message := range messages {
		tokens := a.Estimator.Message(message)
		if len(current) > 0 && used+tokens > a.ChunkTokens {
			chunks = append(chunks, current)
			current, used = make([]Message, 0), 0
		}
		current = append(current, message)
		used += tokens
	}
	if len(current) > 0 {
		chunks = append(chunks, current)
	}
	return chunks
}
