package impl

import (
	"context"
	"fmt"
	gogiv1 "gogi/gogi/gogi/v1"
	"gogi/gogi/sessions"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// The fixtures are a user planning a vegetable garden with an assistant

type echoSummarizer struct{ calls int }

func (s *echoSummarizer) Summarize(ctx context.Context, request sessions.SummaryRequest) (string, error) {
	s.calls++
	return fmt.Sprintf("%d earlier messages about the garden", len(request.Messages)), nil
}

func (s *echoSummarizer) Model() string { return "fake/summarizer" }

func newTestServer(summarizer sessions.Summarizer) *LLMSessionServer {
	store := sessions.NewMemoryStore()
	assembler := &sessions.Assembler{Store: store, Estimator: sessions.DefaultEstimator(), MemoryShare: 0.1,
		SummaryShare: 0.2, ChunkTokens: 6000, Summarizer: summarizer}
	server := NewLLMSessionServer(store, assembler, DefaultConfig())
	server.now = func() time.Time { return time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC) }
	return server
}

func requireCode(t *testing.T, err error, code codes.Code) {
	t.Helper()
	if status.Code(err) != code {
		t.Errorf("expected code %s, got %v", code, err)
	}
}

func userMessage(content string) *gogiv1.LLMMessage {
	return &gogiv1.LLMMessage{Role: "user", Content: proto.String(content)}
}

func assistantMessage(content string) *gogiv1.LLMMessage {
	return &gogiv1.LLMMessage{Role: "assistant", Content: proto.String(content)}
}

func createSession(t *testing.T, server *LLMSessionServer, userID, sessionID string) *gogiv1.Session {
	t.Helper()
	request := &gogiv1.GetOrCreateSessionRequest{UserId: userID}
	if sessionID != "" {
		request.SessionId = proto.String(sessionID)
	}
	response, err := server.GetOrCreateSession(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	return response.Session
}

func TestGetOrCreateSession(t *testing.T) {
	server := newTestServer(nil)
	ctx := context.Background()

	created, err := server.GetOrCreateSession(ctx, &gogiv1.GetOrCreateSessionRequest{UserId: "u-5"})
	if err != nil || !created.Created || created.Session.SessionId == "" || created.Session.UserId != "u-5" ||
		created.Session.CreatedAt == 0 {
		t.Fatalf("wrong response %+v, %v", created, err)
	}

	resumed, _ := server.GetOrCreateSession(ctx, &gogiv1.GetOrCreateSessionRequest{UserId: "u-5",
		SessionId: proto.String(created.Session.SessionId)})
	if resumed.Created || resumed.Session.SessionId != created.Session.SessionId {
		t.Errorf("expected the same session, got %+v", resumed)
	}

	// a client may choose the id of a new session
	chosen, _ := server.GetOrCreateSession(ctx, &gogiv1.GetOrCreateSessionRequest{UserId: "u-5",
		SessionId: proto.String("garden-2026")})
	if !chosen.Created || chosen.Session.SessionId != "garden-2026" {
		t.Errorf("expected a session with the chosen id, got %+v", chosen)
	}

	// another user's session is never returned
	_, err = server.GetOrCreateSession(ctx, &gogiv1.GetOrCreateSessionRequest{UserId: "u-6",
		SessionId: proto.String("garden-2026")})
	requireCode(t, err, codes.PermissionDenied)

	_, err = server.GetOrCreateSession(ctx, &gogiv1.GetOrCreateSessionRequest{})
	requireCode(t, err, codes.InvalidArgument)
	_, err = server.GetOrCreateSession(ctx, &gogiv1.GetOrCreateSessionRequest{UserId: "u-5", SessionId: proto.String("no spaces")})
	requireCode(t, err, codes.InvalidArgument)
}

func TestListAndDeleteSessions(t *testing.T) {
	server := newTestServer(nil)
	ctx := context.Background()
	for _, id := range []string{"beds", "compost", "greenhouse"} {
		createSession(t, server, "u-5", id)
	}
	createSession(t, server, "u-6", "orchard")

	listed, err := server.ListSessions(ctx, &gogiv1.ListSessionsRequest{UserId: "u-5", Limit: 2})
	if err != nil || len(listed.Sessions) != 2 || listed.TotalCount != 3 {
		t.Errorf("wrong list %+v, %v", listed, err)
	}
	_, err = server.ListSessions(ctx, &gogiv1.ListSessionsRequest{UserId: "u-5", Limit: -1})
	requireCode(t, err, codes.InvalidArgument)

	deleted, _ := server.DeleteSession(ctx, &gogiv1.DeleteSessionRequest{SessionId: "compost"})
	again, _ := server.DeleteSession(ctx, &gogiv1.DeleteSessionRequest{SessionId: "compost"})
	if !deleted.Success || again.Success {
		t.Errorf("wrong deletions %v %v", deleted.Success, again.Success)
	}
}

func TestAddAndGetMessages(t *testing.T) {
	server := newTestServer(nil)
	ctx := context.Background()
	session := createSession(t, server, "u-5", "beds")

	added, err := server.AddMessages(ctx, &gogiv1.AddMessagesRequest{SessionId: session.SessionId, Messages: []*gogiv1.LLMMessage{
		{Role: "system", Content: proto.String("You help people grow vegetables.")},
		userMessage("When should I sow carrots?"),
		{Role: "assistant", ToolCalls: []*gogiv1.ToolCall{{Id: "c1", Function: &gogiv1.ToolCallFunction{
			Name: "frost_dates", Arguments: `{"postcode":"EH1"}`}}}},
		{Role: "tool", ToolCallId: proto.String("c1"), Name: proto.String("frost_dates"), Content: proto.String(`{"last_frost":"04-20"}`)},
		assistantMessage("Sow carrots outdoors from late April."),
	}})
	if err != nil || added.MessageCount != 5 || added.TotalCount != 5 {
		t.Fatalf("wrong response %+v, %v", added, err)
	}

	page, err := server.GetMessages(ctx, &gogiv1.GetMessagesRequest{SessionId: session.SessionId,
		Limit: proto.Int32(2), Offset: proto.Int32(2)})
	if err != nil || len(page.Messages) != 2 || page.TotalCount != 5 || page.Strategy != sessions.StrategyNone {
		t.Fatalf("wrong page %+v, %v", page, err)
	}
	call, result := page.Messages[0], page.Messages[1]
	if call.ToolCalls[0].GetFunction().GetName() != "frost_dates" || call.ToolCalls[0].GetType() != "function" ||
		result.GetToolCallId() != "c1" || result.GetName() != "frost_dates" {
		t.Errorf("tool calls and results should round-trip, got %+v %+v", call, result)
	}
	// a message without a timestamp gets the time it is stored
	if call.Timestamp != server.now().UnixMilli() {
		t.Errorf("wrong timestamp %d", call.Timestamp)
	}

	all, _ := server.GetMessages(ctx, &gogiv1.GetMessagesRequest{SessionId: session.SessionId})
	listed, _ := server.ListSessions(ctx, &gogiv1.ListSessionsRequest{UserId: "u-5"})
	if len(all.Messages) != 5 || listed.Sessions[0].MessageCount != 5 {
		t.Errorf("expected all 5 messages, got %d, count %d", len(all.Messages), listed.Sessions[0].MessageCount)
	}

	_, err = server.AddMessages(ctx, &gogiv1.AddMessagesRequest{SessionId: "missing", Messages: []*gogiv1.LLMMessage{userMessage("hi")}})
	requireCode(t, err, codes.NotFound)
	_, err = server.GetMessages(ctx, &gogiv1.GetMessagesRequest{SessionId: "missing"})
	requireCode(t, err, codes.NotFound)
}

func TestAddMessagesValidation(t *testing.T) {
	server := newTestServer(nil)
	session := createSession(t, server, "u-5", "beds")

	invalid := map[string][]*gogiv1.LLMMessage{
		"no messages":            {},
		"unknown role":           {{Role: "gardener", Content: proto.String("hi")}},
		"user without content":   {{Role: "user"}},
		"tool without call id":   {{Role: "tool", Content: proto.String("{}")}},
		"empty assistant":        {{Role: "assistant"}},
		"user calling tools":     {{Role: "user", Content: proto.String("x"), ToolCalls: []*gogiv1.ToolCall{{Id: "c", Function: &gogiv1.ToolCallFunction{Name: "f"}}}}},
		"tool call without name": {{Role: "assistant", ToolCalls: []*gogiv1.ToolCall{{Id: "c"}}}},
	}
	for name, messages := range invalid {
		_, err := server.AddMessages(context.Background(), &gogiv1.AddMessagesRequest{SessionId: session.SessionId, Messages: messages})
		if status.Code(err) != codes.InvalidArgument {
			t.Errorf("%s: expected InvalidArgument, got %v", name, err)
		}
	}

	// nothing is stored when one message is invalid
	server.AddMessages(context.Background(), &gogiv1.AddMessagesRequest{SessionId: session.SessionId,
		Messages: []*gogiv1.LLMMessage{userMessage("fine"), {Role: "user"}}})
	if all, _ := server.GetMessages(context.Background(), &gogiv1.GetMessagesRequest{SessionId: session.SessionId}); all.TotalCount != 0 {
		t.Errorf("no message should be stored, got %d", all.TotalCount)
	}
}

// addExchanges adds count questions and answers of about 14 tokens each
func addExchanges(t *testing.T, server *LLMSessionServer, sessionID string, count int) {
	t.Helper()
	messages := make([]*gogiv1.LLMMessage, 0, 2*count)
	for i := 0; i < count; i++ {
		messages = append(messages, userMessage(fmt.Sprintf("question %02d: which beans climb best?", i)),
			assistantMessage(fmt.Sprintf("answer %02d: runner beans climb fast.", i)))
	}
	if _, err := server.AddMessages(context.Background(), &gogiv1.AddMessagesRequest{SessionId: sessionID, Messages: messages}); err != nil {
		t.Fatal(err)
	}
}

func TestGetMessagesStrategies(t *testing.T) {
	summarizer := &echoSummarizer{}
	server := newTestServer(summarizer)
	ctx := context.Background()
	session := createSession(t, server, "u-5", "beds")
	addExchanges(t, server, session.SessionId, 12)
	server.SaveMemory(ctx, &gogiv1.SaveMemoryRequest{UserId: "u-5", Key: "soil", Value: `"clay"`})

	truncated, err := server.GetMessages(ctx, &gogiv1.GetMessagesRequest{SessionId: session.SessionId,
		Strategy: proto.String("truncate"), TokenBudget: proto.Int32(100)})
	if err != nil || truncated.Strategy != "truncate" || truncated.OmittedCount == 0 || truncated.EstimatedTokens > 100 ||
		int(truncated.OmittedCount)+len(truncated.Messages) != 24 {
		t.Fatalf("wrong truncation %+v, %v", truncated, err)
	}

	summarized, err := server.GetMessages(ctx, &gogiv1.GetMessagesRequest{SessionId: session.SessionId,
		Strategy: proto.String("SUMMARIZE"), TokenBudget: proto.Int32(150)})
	if err != nil || summarized.Strategy != "summarize" || summarized.SummarizedCount == 0 ||
		!strings.Contains(summarized.Summary, "earlier messages about the garden") ||
		!strings.HasPrefix(summarized.Messages[0].GetContent(), "Summary of the") {
		t.Fatalf("wrong summary %+v, %v", summarized, err)
	}

	hierarchical, err := server.GetMessages(ctx, &gogiv1.GetMessagesRequest{SessionId: session.SessionId,
		Strategy: proto.String("hierarchical"), TokenBudget: proto.Int32(200), SummaryFocus: proto.String("plant varieties")})
	if err != nil || !strings.Contains(hierarchical.Messages[0].GetContent(), `soil: "clay"`) ||
		!strings.HasPrefix(hierarchical.Messages[1].GetContent(), "Summary of the") {
		t.Fatalf("wrong hierarchical context %+v, %v", hierarchical, err)
	}

	// the default budget applies when none is given: this conversation fits it
	full, _ := server.GetMessages(ctx, &gogiv1.GetMessagesRequest{SessionId: session.SessionId, Strategy: proto.String("truncate")})
	if full.OmittedCount != 0 || len(full.Messages) != 24 {
		t.Errorf("the conversation fits the default budget, got %d omitted", full.OmittedCount)
	}

	for name, request := range map[string]*gogiv1.GetMessagesRequest{
		"unknown strategy": {SessionId: session.SessionId, Strategy: proto.String("everything")},
		"zero budget":      {SessionId: session.SessionId, Strategy: proto.String("truncate"), TokenBudget: proto.Int32(0)},
		"offset":           {SessionId: session.SessionId, Strategy: proto.String("truncate"), Offset: proto.Int32(3)},
		"negative limit":   {SessionId: session.SessionId, Limit: proto.Int32(-1)},
	} {
		if _, err := server.GetMessages(ctx, request); status.Code(err) != codes.InvalidArgument {
			t.Errorf("%s: expected InvalidArgument, got %v", name, err)
		}
	}
}

func TestSummarizeWithoutModel(t *testing.T) {
	server := newTestServer(nil)
	session := createSession(t, server, "u-5", "beds")
	addExchanges(t, server, session.SessionId, 12)

	response, err := server.GetMessages(context.Background(), &gogiv1.GetMessagesRequest{SessionId: session.SessionId,
		Strategy: proto.String("summarize"), TokenBudget: proto.Int32(150)})
	if err != nil || response.Strategy != "truncate" || response.Summary != "" || len(response.Notes) == 0 {
		t.Errorf("expected truncation with a note, got %+v, %v", response, err)
	}
}

func TestMemories(t *testing.T) {
	server := newTestServer(nil)
	ctx := context.Background()
	session := createSession(t, server, "u-5", "beds")
	createSession(t, server, "u-6", "orchard")

	saved, err := server.SaveMemory(ctx, &gogiv1.SaveMemoryRequest{UserId: "u-5", Key: "soil", Value: `"clay"`})
	if err != nil || !saved.Success || saved.Entry.SessionId != "" || saved.Entry.UpdatedAt == 0 {
		t.Fatalf("wrong response %+v, %v", saved, err)
	}
	if _, err := server.SaveMemory(ctx, &gogiv1.SaveMemoryRequest{UserId: "u-5", Key: "soil", Value: `"loam"`,
		SessionId: proto.String(session.SessionId)}); err != nil {
		t.Fatal(err)
	}
	server.SaveMemory(ctx, &gogiv1.SaveMemoryRequest{UserId: "u-5", Key: "plot_m2", Value: `24`})

	all, _ := server.GetMemory(ctx, &gogiv1.GetMemoryRequest{UserId: "u-5"})
	if len(all.Entries) != 3 || all.Memories["soil"] != `"loam"` || all.Memories["plot_m2"] != "24" {
		t.Errorf("the most recently updated value of a key should be given, got %+v", all.Memories)
	}
	scoped, _ := server.GetMemory(ctx, &gogiv1.GetMemoryRequest{UserId: "u-5", SessionId: proto.String(session.SessionId)})
	if len(scoped.Entries) != 1 || scoped.Memories["soil"] != `"loam"` {
		t.Errorf("only the session's memories should be given, got %+v", scoped.Memories)
	}

	// optimistic concurrency
	stale := saved.Entry.UpdatedAt
	_, err = server.SaveMemory(ctx, &gogiv1.SaveMemoryRequest{UserId: "u-5", Key: "soil", Value: `"sand"`, ExpectedUpdatedAt: &stale})
	if err != nil {
		t.Fatal(err)
	}
	_, err = server.SaveMemory(ctx, &gogiv1.SaveMemoryRequest{UserId: "u-5", Key: "soil", Value: `"silt"`, ExpectedUpdatedAt: &stale})
	requireCode(t, err, codes.Aborted)

	// a memory for another user's session, or for a session that does not exist
	_, err = server.SaveMemory(ctx, &gogiv1.SaveMemoryRequest{UserId: "u-5", Key: "k", Value: "1", SessionId: proto.String("orchard")})
	requireCode(t, err, codes.PermissionDenied)
	_, err = server.SaveMemory(ctx, &gogiv1.SaveMemoryRequest{UserId: "u-5", Key: "k", Value: "1", SessionId: proto.String("missing")})
	requireCode(t, err, codes.NotFound)
	_, err = server.SaveMemory(ctx, &gogiv1.SaveMemoryRequest{UserId: "u-5", Key: "k", Value: "clay"})
	requireCode(t, err, codes.InvalidArgument)
	_, err = server.SaveMemory(ctx, &gogiv1.SaveMemoryRequest{UserId: "u-5", Value: "1"})
	requireCode(t, err, codes.InvalidArgument)

	deleted, _ := server.DeleteMemory(ctx, &gogiv1.DeleteMemoryRequest{UserId: "u-5", Key: "soil"})
	if !deleted.Success || deleted.Count != 2 {
		t.Errorf("deleting a key without a session deletes it in every scope, got %+v", deleted)
	}
	cleared, _ := server.ClearUserMemory(ctx, &gogiv1.ClearUserMemoryRequest{UserId: "u-5"})
	if cleared.Count != 1 {
		t.Errorf("expected 1 memory cleared, got %d", cleared.Count)
	}
}

func TestDeleteSessionMemories(t *testing.T) {
	server := newTestServer(nil)
	ctx := context.Background()
	session := createSession(t, server, "u-5", "beds")
	server.SaveMemory(ctx, &gogiv1.SaveMemoryRequest{UserId: "u-5", Key: "soil", Value: `"clay"`})
	server.SaveMemory(ctx, &gogiv1.SaveMemoryRequest{UserId: "u-5", Key: "bed_count", Value: "4", SessionId: proto.String(session.SessionId)})

	server.DeleteSession(ctx, &gogiv1.DeleteSessionRequest{SessionId: session.SessionId, DeleteMemories: true})
	left, _ := server.GetMemory(ctx, &gogiv1.GetMemoryRequest{UserId: "u-5"})
	if len(left.Entries) != 1 || left.Entries[0].Key != "soil" {
		t.Errorf("only the user's memories should be left, got %+v", left.Entries)
	}
}

func TestConfigFromEnv(t *testing.T) {
	t.Setenv("GOGI_SESSIONS_DEFAULT_TOKEN_BUDGET", "12000")
	t.Setenv("GOGI_SESSIONS_SUMMARY_PROVIDER", "openai")
	t.Setenv("GOGI_SESSIONS_SUMMARY_MODEL", "gpt-4o-mini")
	config, assembler, summary, err := ConfigFromEnv()
	if err != nil || config.DefaultTokenBudget != 12000 || assembler.Estimator.CharsPerToken != 4 ||
		summary.Model != "gpt-4o-mini" || summary.Timeout != time.Minute {
		t.Errorf("wrong config %+v %+v %+v, %v", config, assembler, summary, err)
	}

	t.Setenv("GOGI_SESSIONS_SUMMARY_SHARE", "0.95")
	if _, _, _, err := ConfigFromEnv(); err == nil {
		t.Errorf("expected an error for shares that leave nothing for recent messages")
	}
	t.Setenv("GOGI_SESSIONS_SUMMARY_SHARE", "0.2")
	t.Setenv("GOGI_SESSIONS_SUMMARY_PROVIDER", "")
	if _, _, _, err := ConfigFromEnv(); err == nil {
		t.Errorf("expected an error for a model without a provider")
	}
}
