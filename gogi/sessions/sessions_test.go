package sessions

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	gogiv1 "gogi/gogi/gogi/v1"

	"google.golang.org/grpc"
)

// The fixtures are a user planning a kitchen renovation with an assistant

func text(value string) *string {
	return &value
}

func message(role, content string) Message {
	return Message{Role: role, Content: text(content)}
}

// fakeSummarizer records what it is asked to summarize
type fakeSummarizer struct {
	requests []SummaryRequest
	err      error
}

func (s *fakeSummarizer) Summarize(ctx context.Context, request SummaryRequest) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	s.requests = append(s.requests, request)
	parts := make([]string, 0)
	if request.Previous != "" {
		parts = append(parts, request.Previous)
	}
	for _, m := range request.Messages {
		parts = append(parts, m.Text())
	}
	return strings.Join(parts, " | "), nil
}

func (s *fakeSummarizer) Model() string {
	return "fake/summarizer"
}

// newConversation stores a session with a system prompt and count exchanges of a user
// and the assistant, each message 40 characters, about 14 tokens with its overhead
func newConversation(t *testing.T, store *MemoryStore, count int) (Session, []Message) {
	t.Helper()
	ctx := context.Background()
	session, _, err := store.GetOrCreateSession(ctx, "u-17", "kitchen")
	if err != nil {
		t.Fatal(err)
	}
	messages := []Message{message(RoleSystem, "You help people plan home renovations.")}
	for i := 0; i < count; i++ {
		messages = append(messages,
			message(RoleUser, fmt.Sprintf("question %02d about the kitchen worktops?", i)),
			message(RoleAssistant, fmt.Sprintf("answer %02d: quartz suits a busy kitchen.", i)))
	}
	if _, err := store.AddMessages(ctx, session.ID, messages); err != nil {
		t.Fatal(err)
	}
	stored, _, _ := store.GetMessages(ctx, session.ID, 0, 0)
	return session, stored
}

func newAssembler(store Store, summarizer Summarizer) *Assembler {
	assembler := &Assembler{Store: store, Estimator: DefaultEstimator(), MemoryShare: 0.1, SummaryShare: 0.2,
		ChunkTokens: 6000}
	if summarizer != nil {
		assembler.Summarizer = summarizer
	}
	return assembler
}

func texts(messages []Message) []string {
	values := make([]string, 0, len(messages))
	for _, m := range messages {
		values = append(values, m.Text())
	}
	return values
}

func TestEstimator(t *testing.T) {
	estimator := DefaultEstimator()
	if got := estimator.Text("12345678"); got != 2 {
		t.Errorf("8 characters should be 2 tokens, got %d", got)
	}
	if got := estimator.Text("123456789"); got != 3 {
		t.Errorf("9 characters should round up to 3 tokens, got %d", got)
	}
	call := Message{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "c1", Name: "quote", Arguments: `{"m2":4}`}}}
	if got := estimator.Message(call); got != estimator.Text(`quote{"m2":4}`)+4 {
		t.Errorf("tool calls should count, got %d", got)
	}
}

func TestTruncateKeepsTheMostRecentMessages(t *testing.T) {
	store := NewMemoryStore()
	session, messages := newConversation(t, store, 10)
	assembler := newAssembler(store, nil)

	result, err := assembler.Assemble(context.Background(), messages, ContextRequest{SessionID: session.ID,
		UserID: "u-17", Strategy: StrategyTruncate, Budget: 100})
	if err != nil {
		t.Fatal(err)
	}
	// the system prompt is always kept, then as many recent messages as fit
	if result.Messages[0].Role != RoleSystem || result.Tokens > 100 || result.Omitted == 0 {
		t.Errorf("wrong context: %d messages, %d tokens, %d omitted", len(result.Messages), result.Tokens, result.Omitted)
	}
	last := result.Messages[len(result.Messages)-1]
	if last.Text() != messages[len(messages)-1].Text() {
		t.Errorf("the latest message should be last, got %q", last.Text())
	}
	if result.Omitted+len(result.Messages) != len(messages) {
		t.Errorf("every message is either kept or omitted: %d + %d != %d", result.Omitted, len(result.Messages), len(messages))
	}

	// a limit keeps fewer
	result, _ = assembler.Assemble(context.Background(), messages, ContextRequest{SessionID: session.ID,
		Strategy: StrategyTruncate, Budget: 10000, Limit: 3})
	if len(result.Messages) != 4 || result.Omitted != len(messages)-4 {
		t.Errorf("expected the system prompt and 3 messages, got %v", texts(result.Messages))
	}
}

func TestTruncateDoesNotSplitToolCalls(t *testing.T) {
	estimator := DefaultEstimator()
	call := Message{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "c1", Type: "function", Name: "price_worktop",
		Arguments: `{"material":"quartz","length_m":3.2}`}}}
	result := Message{Role: RoleTool, Content: text(`{"price_eur": 1840}`), ToolCallID: text("c1")}
	answer := message(RoleAssistant, "A 3.2 m quartz worktop costs about 1840 euros.")
	messages := []Message{message(RoleUser, "How much for quartz?"), call, result, answer}

	// a budget that fits the tool result and the answer, but not the call
	budget := estimator.Message(result) + estimator.Message(answer)
	assembler := newAssembler(NewMemoryStore(), nil)
	context, err := assembler.Assemble(t.Context(), messages, ContextRequest{Strategy: StrategyTruncate, Budget: budget})
	if err != nil {
		t.Fatal(err)
	}
	if len(context.Messages) != 1 || context.Messages[0].Text() != answer.Text() {
		t.Errorf("a tool result without its call should be left out, got %v", texts(context.Messages))
	}
}

func TestTruncateKeepsTheLatestMessageEvenOverBudget(t *testing.T) {
	assembler := newAssembler(NewMemoryStore(), nil)
	long := message(RoleUser, strings.Repeat("measurements ", 100))

	result, _ := assembler.Assemble(t.Context(), []Message{message(RoleUser, "hi"), long},
		ContextRequest{Strategy: StrategyTruncate, Budget: 20})
	if len(result.Messages) != 1 || !slices.Contains(result.Notes, "the latest message alone exceeds the token budget") {
		t.Errorf("expected the latest message with a note, got %v, %v", texts(result.Messages), result.Notes)
	}
}

func TestSummarizeRollsTheSummaryForward(t *testing.T) {
	store := NewMemoryStore()
	session, messages := newConversation(t, store, 10)
	summarizer := &fakeSummarizer{}
	assembler := newAssembler(store, summarizer)
	request := ContextRequest{SessionID: session.ID, UserID: "u-17", Strategy: StrategySummarize, Budget: 150}

	result, err := assembler.Assemble(t.Context(), messages, request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Strategy != StrategySummarize || result.Summarized == 0 || result.Summarized != result.Omitted {
		t.Fatalf("expected a summary of the omitted messages, got %+v", result)
	}
	// the system prompt, then the summary, then the recent messages
	if result.Messages[0].Role != RoleSystem || !strings.HasPrefix(result.Messages[1].Text(), "Summary of the") ||
		!strings.Contains(result.Messages[1].Text(), "question 00") {
		t.Errorf("wrong context %v", texts(result.Messages)[:2])
	}
	if len(summarizer.requests) != 1 || summarizer.requests[0].Previous != "" {
		t.Fatalf("expected one summary from scratch, got %+v", summarizer.requests)
	}

	// asking again changes nothing: the summary is cached
	if _, err := assembler.Assemble(t.Context(), messages, request); err != nil || len(summarizer.requests) != 1 {
		t.Errorf("the cached summary should be used, got %d summaries, %v", len(summarizer.requests), err)
	}

	// as more messages age out, only those are summarized, onto the previous summary
	ctx := context.Background()
	store.AddMessages(ctx, session.ID, []Message{message(RoleUser, "question 10 about the kitchen worktops?"),
		message(RoleAssistant, "answer 10: quartz suits a busy kitchen.")})
	messages, _, _ = store.GetMessages(ctx, session.ID, 0, 0)
	result, _ = assembler.Assemble(t.Context(), messages, request)
	if len(summarizer.requests) != 2 {
		t.Fatalf("expected the summary to be extended, got %d summaries", len(summarizer.requests))
	}
	extended := summarizer.requests[1]
	if extended.Previous == "" || len(extended.Messages) != 2 || result.Summarized != result.Omitted {
		t.Errorf("only the newly aged-out messages should be summarized, got %d messages, previous %q",
			len(extended.Messages), extended.Previous)
	}
}

func TestSummarizeOnlyWhenNeeded(t *testing.T) {
	store := NewMemoryStore()
	session, messages := newConversation(t, store, 2)
	summarizer := &fakeSummarizer{}

	result, _ := newAssembler(store, summarizer).Assemble(t.Context(), messages,
		ContextRequest{SessionID: session.ID, Strategy: StrategySummarize, Budget: 1000})
	if len(summarizer.requests) != 0 || result.Omitted != 0 || len(result.Messages) != len(messages) {
		t.Errorf("a conversation that fits needs no summary, got %+v", result)
	}
}

func TestSummarizeFallsBackToTruncation(t *testing.T) {
	store := NewMemoryStore()
	session, messages := newConversation(t, store, 10)
	request := ContextRequest{SessionID: session.ID, Strategy: StrategySummarize, Budget: 150}

	// without a summary model
	result, _ := newAssembler(store, nil).Assemble(t.Context(), messages, request)
	if result.Strategy != StrategyTruncate || result.Summary != "" || len(result.Notes) == 0 {
		t.Errorf("expected truncation with a note, got %+v", result)
	}

	// when the summary model fails
	result, err := newAssembler(store, &fakeSummarizer{err: errors.New("unavailable")}).Assemble(t.Context(), messages, request)
	if err != nil || result.Strategy != StrategyTruncate || !strings.Contains(strings.Join(result.Notes, " "), "could not be written") {
		t.Errorf("expected truncation with a note, got %+v, %v", result, err)
	}
}

func TestSummaryPerFocus(t *testing.T) {
	store := NewMemoryStore()
	session, messages := newConversation(t, store, 10)
	summarizer := &fakeSummarizer{}
	assembler := newAssembler(store, summarizer)

	for _, focus := range []string{"", "prices and measurements", ""} {
		assembler.Assemble(t.Context(), messages, ContextRequest{SessionID: session.ID, Strategy: StrategySummarize,
			Budget: 150, Focus: focus})
	}
	if len(summarizer.requests) != 2 || summarizer.requests[1].Focus != "prices and measurements" {
		t.Errorf("each focus has its own summary, got %d summaries", len(summarizer.requests))
	}
}

func TestLongHistoriesAreSummarizedInChunks(t *testing.T) {
	store := NewMemoryStore()
	session, messages := newConversation(t, store, 30)
	summarizer := &fakeSummarizer{}
	assembler := newAssembler(store, summarizer)
	assembler.ChunkTokens = 200

	result, _ := assembler.Assemble(t.Context(), messages, ContextRequest{SessionID: session.ID,
		Strategy: StrategySummarize, Budget: 200})
	if len(summarizer.requests) < 2 || summarizer.requests[1].Previous == "" || result.Summarized == 0 {
		t.Errorf("expected several chunks, each extending the summary, got %d", len(summarizer.requests))
	}
}

func TestHierarchicalPutsMemoriesFirst(t *testing.T) {
	store := NewMemoryStore()
	session, messages := newConversation(t, store, 10)
	ctx := context.Background()
	store.SaveMemory(ctx, Memory{UserID: "u-17", Key: "budget_eur", Value: "12000"}, nil)
	store.SaveMemory(ctx, Memory{UserID: "u-17", SessionID: session.ID, Key: "worktop", Value: `"quartz"`}, nil)
	other, _, _ := store.GetOrCreateSession(ctx, "u-17", "bathroom")
	store.SaveMemory(ctx, Memory{UserID: "u-17", SessionID: other.ID, Key: "tiles", Value: `"slate"`}, nil)
	store.SaveMemory(ctx, Memory{UserID: "u-99", Key: "budget_eur", Value: "500"}, nil)

	result, err := newAssembler(store, &fakeSummarizer{}).Assemble(t.Context(), messages,
		ContextRequest{SessionID: session.ID, UserID: "u-17", Strategy: StrategyHierarchical, Budget: 250})
	if err != nil {
		t.Fatal(err)
	}
	if result.Strategy != StrategyHierarchical {
		t.Errorf("wrong strategy %s", result.Strategy)
	}
	// the system prompt, the memories, the summary, then the recent messages
	memories := result.Messages[1].Text()
	if !strings.HasPrefix(memories, "Facts saved about the user:") || !strings.Contains(memories, "budget_eur: 12000") ||
		!strings.Contains(memories, `worktop: "quartz"`) {
		t.Errorf("expected the user's and this session's memories, got %q", memories)
	}
	if strings.Contains(memories, "slate") || strings.Contains(memories, "500") {
		t.Errorf("another session's or user's memories should not be included: %q", memories)
	}
	if !strings.HasPrefix(result.Messages[2].Text(), "Summary of the") {
		t.Errorf("the summary should follow the memories, got %q", result.Messages[2].Text())
	}
}

func TestHierarchicalLeavesOutMemoriesOverTheirShare(t *testing.T) {
	store := NewMemoryStore()
	clock := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
	store.now = func() time.Time { clock = clock.Add(time.Second); return clock }
	session, messages := newConversation(t, store, 2)
	for i := 0; i < 20; i++ {
		store.SaveMemory(context.Background(), Memory{UserID: "u-17", Key: fmt.Sprintf("appliance_%02d", i),
			Value: `"a long description of a kitchen appliance"`}, nil)
	}

	result, _ := newAssembler(store, nil).Assemble(t.Context(), messages,
		ContextRequest{SessionID: session.ID, UserID: "u-17", Strategy: StrategyHierarchical, Budget: 500})
	if !strings.Contains(strings.Join(result.Notes, " "), "memories do not fit") {
		t.Errorf("expected a note about the memories left out, got %v", result.Notes)
	}
	// the most recently updated memories are kept
	if !strings.Contains(result.Messages[1].Text(), "appliance_19") || strings.Contains(result.Messages[1].Text(), "appliance_00") {
		t.Errorf("the most recent memories should be kept, got %q", result.Messages[1].Text())
	}
}

func TestMemoryStoreSessions(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	clock := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
	store.now = func() time.Time { clock = clock.Add(time.Second); return clock }

	first, created, _ := store.GetOrCreateSession(ctx, "u-17", "")
	if !created || first.ID == "" {
		t.Fatalf("expected a new session, got %+v", first)
	}
	again, created, _ := store.GetOrCreateSession(ctx, "u-17", first.ID)
	if created || again.ID != first.ID {
		t.Errorf("expected the same session, got %+v", again)
	}
	if _, _, err := store.GetOrCreateSession(ctx, "u-99", first.ID); !errors.Is(err, ErrSessionOfAnotherUser) {
		t.Errorf("expected ErrSessionOfAnotherUser, got %v", err)
	}

	second, _, _ := store.GetOrCreateSession(ctx, "u-17", "garden")
	store.AddMessages(ctx, first.ID, []Message{message(RoleUser, "hello")})
	sessions, total, _ := store.ListSessions(ctx, "u-17", 0, 0)
	if total != 2 || sessions[0].ID != first.ID || sessions[0].MessageCount != 1 || sessions[1].ID != second.ID {
		t.Errorf("the most recently active session comes first, got %+v", sessions)
	}
	paged, _, _ := store.ListSessions(ctx, "u-17", 1, 1)
	if len(paged) != 1 || paged[0].ID != second.ID {
		t.Errorf("wrong page %+v", paged)
	}

	if _, err := store.AddMessages(ctx, "missing", []Message{message(RoleUser, "hi")}); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestMemoryStoreMemories(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	session, _, _ := store.GetOrCreateSession(ctx, "u-17", "kitchen")

	saved, _ := store.SaveMemory(ctx, Memory{UserID: "u-17", Key: "style", Value: `"scandinavian"`}, nil)
	store.SaveMemory(ctx, Memory{UserID: "u-17", SessionID: session.ID, Key: "style", Value: `"industrial"`}, nil)

	all, _ := store.GetMemories(ctx, "u-17", "", nil)
	userScope := ""
	user, _ := store.GetMemories(ctx, "u-17", "", &userScope)
	scoped, _ := store.GetMemories(ctx, "u-17", "style", &session.ID)
	if len(all) != 2 || len(user) != 1 || len(scoped) != 1 || scoped[0].Value != `"industrial"` {
		t.Errorf("wrong memories: all %d, user %d, session %+v", len(all), len(user), scoped)
	}

	// optimistic concurrency: a save based on a stale read fails
	stale := saved.UpdatedAt
	updated, err := store.SaveMemory(ctx, Memory{UserID: "u-17", Key: "style", Value: `"japandi"`}, &stale)
	if err != nil || updated.UpdatedAt <= stale || updated.CreatedAt != saved.CreatedAt {
		t.Fatalf("the save should succeed and move updated_at on, got %+v, %v", updated, err)
	}
	if _, err := store.SaveMemory(ctx, Memory{UserID: "u-17", Key: "style", Value: `"rustic"`}, &stale); !errors.Is(err, ErrMemoryConflict) {
		t.Errorf("expected ErrMemoryConflict, got %v", err)
	}
	zero := int64(0)
	if _, err := store.SaveMemory(ctx, Memory{UserID: "u-17", Key: "style", Value: `"rustic"`}, &zero); !errors.Is(err, ErrMemoryConflict) {
		t.Errorf("0 means the memory must not exist yet, got %v", err)
	}

	// deleting the session's memories keeps the user's
	store.DeleteSession(ctx, session.ID, true)
	if all, _ := store.GetMemories(ctx, "u-17", "", nil); len(all) != 1 || all[0].SessionID != "" {
		t.Errorf("only the user's memory should be left, got %+v", all)
	}
	if count, _ := store.ClearUserMemory(ctx, "u-17"); count != 1 {
		t.Errorf("expected 1 memory cleared, got %d", count)
	}
}

// fakeRunner records the model call
type fakeRunner struct {
	request *gogiv1.LLMRunRequest
	content string
}

func (r *fakeRunner) Run(ctx context.Context, in *gogiv1.LLMRunRequest, opts ...grpc.CallOption) (*gogiv1.LLMRunResponse, error) {
	r.request = in
	return &gogiv1.LLMRunResponse{Content: r.content}, nil
}

func TestModelSummarizer(t *testing.T) {
	runner := &fakeRunner{content: "  The user wants a quartz worktop.  "}
	summarizer := &ModelSummarizer{Runner: runner, Provider: "openai", ModelID: "gpt-4o-mini", Timeout: time.Second}

	call := Message{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "c1", Name: "price_worktop", Arguments: `{"m":3}`}}}
	result := Message{Role: RoleTool, Name: text("price_worktop"), Content: text(`{"eur":1840}`)}
	summary, err := summarizer.Summarize(t.Context(), SummaryRequest{Previous: "The user is renovating a kitchen.",
		Messages: []Message{message(RoleUser, "Price a 3 m quartz worktop"), call, result}, Focus: "prices",
		MaxTokens: 200})
	if err != nil || summary != "The user wants a quartz worktop." {
		t.Fatalf("wrong summary %q, %v", summary, err)
	}

	prompt := runner.request.GetMessages()[0].GetContent()
	for _, expected := range []string{"The summary so far:\nThe user is renovating a kitchen.", "Above all, keep: prices.",
		"user: Price a 3 m quartz worktop", `assistant called: price_worktop({"m":3})`, `[price_worktop result] {"eur":1840}`} {
		if !strings.Contains(prompt, expected) {
			t.Errorf("the prompt should contain %q:\n%s", expected, prompt)
		}
	}
	config := runner.request.GetConfig()
	if config.GetProvider() != "openai" || config.GetModel() != "gpt-4o-mini" || config.GetMaxTokens() != 200 {
		t.Errorf("wrong config %+v", config)
	}

	runner.content = "  "
	if _, err := summarizer.Summarize(t.Context(), SummaryRequest{Messages: []Message{message(RoleUser, "hi")}}); err == nil {
		t.Errorf("an empty summary should be an error")
	}
}

func TestNoFallbackNoteWhenNothingIsLeftOut(t *testing.T) {
	store := NewMemoryStore()
	session, messages := newConversation(t, store, 2)

	for _, strategy := range []string{StrategySummarize, StrategyHierarchical} {
		result, _ := newAssembler(store, nil).Assemble(t.Context(), messages,
			ContextRequest{SessionID: session.ID, UserID: "u-17", Strategy: strategy, Budget: 1000})
		if result.Strategy != strategy || len(result.Notes) != 0 || result.Omitted != 0 {
			t.Errorf("%s: a conversation that fits needs no summary and no note, got %+v", strategy, result)
		}
	}
}
