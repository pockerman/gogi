package postgres

import (
	"context"
	"errors"
	"gogi/gogi/sessions"
	"os"
	"sync"
	"testing"
)

// The test runs against a migrated database, e.g.
// GOGI_TEST_POSTGRES_DSN=postgres://postgres:test@localhost:5432/gogi?sslmode=disable
func newTestSessionRepository(t *testing.T) *GogiLLMSessionRepository {
	dsn := os.Getenv("GOGI_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("GOGI_TEST_POSTGRES_DSN is not set")
	}

	pool, err := NewPool(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	// the tables are emptied before and after the test, so use a dedicated test database

	clear := func() {
		for _, table := range []string{"gogi_llm_memories", "gogi_llm_sessions"} {
			if _, err := pool.Exec(context.Background(), "DELETE FROM "+table); err != nil {
				t.Fatal(err)
			}
		}
	}
	clear()
	t.Cleanup(clear)
	return NewGogiLLMSessionRepository(pool)
}

func text(value string) *string {
	return &value
}

func TestSessionRepositorySessionsAndMessages(t *testing.T) {
	repo := newTestSessionRepository(t)
	ctx := context.Background()

	session, created, err := repo.GetOrCreateSession(ctx, "u-5", "")
	if err != nil || !created || session.ID == "" {
		t.Fatalf("expected a new session, got %+v, %v", session, err)
	}
	again, created, _ := repo.GetOrCreateSession(ctx, "u-5", session.ID)
	if created || again.ID != session.ID {
		t.Errorf("expected the same session, got %+v", again)
	}
	if _, _, err := repo.GetOrCreateSession(ctx, "u-6", session.ID); !errors.Is(err, sessions.ErrSessionOfAnotherUser) {
		t.Errorf("expected ErrSessionOfAnotherUser, got %v", err)
	}

	total, err := repo.AddMessages(ctx, session.ID, []sessions.Message{
		{Role: sessions.RoleUser, Content: text("Which tomatoes for a cold greenhouse?"), Timestamp: 1},
		{Role: sessions.RoleAssistant, ToolCalls: []sessions.ToolCall{{ID: "c1", Type: "function", Name: "varieties",
			Arguments: `{"climate":"cold"}`}}, Timestamp: 2},
		{Role: sessions.RoleTool, Content: text(`["Sungold"]`), ToolCallID: text("c1"), Name: text("varieties"), Timestamp: 3},
	})
	if err != nil || total != 3 {
		t.Fatalf("wrong total %d, %v", total, err)
	}
	if total, _ = repo.AddMessages(ctx, session.ID, []sessions.Message{{Role: sessions.RoleAssistant,
		Content: text("Try Sungold."), Timestamp: 4}}); total != 4 {
		t.Errorf("expected 4 messages, got %d", total)
	}

	messages, count, err := repo.GetMessages(ctx, session.ID, 0, 0)
	if err != nil || count != 4 || len(messages) != 4 || messages[0].Seq >= messages[1].Seq {
		t.Fatalf("wrong messages %+v, %d, %v", messages, count, err)
	}
	if messages[1].ToolCalls[0] != (sessions.ToolCall{ID: "c1", Type: "function", Name: "varieties", Arguments: `{"climate":"cold"}`}) ||
		*messages[2].ToolCallID != "c1" || messages[0].ToolCalls != nil {
		t.Errorf("tool calls should round-trip, got %+v %+v", messages[1], messages[2])
	}
	page, _, _ := repo.GetMessages(ctx, session.ID, 2, 1)
	if len(page) != 2 || page[0].Timestamp != 2 {
		t.Errorf("wrong page %+v", page)
	}

	if _, err := repo.AddMessages(ctx, "missing", []sessions.Message{{Role: sessions.RoleUser, Content: text("hi")}}); !errors.Is(err, sessions.ErrSessionNotFound) {
		t.Errorf("expected ErrSessionNotFound, got %v", err)
	}

	other, _, _ := repo.GetOrCreateSession(ctx, "u-5", "herbs")
	list, total, err := repo.ListSessions(ctx, "u-5", 1, 0)
	if err != nil || total != 2 || len(list) != 1 || list[0].ID != other.ID {
		t.Errorf("the most recently active session comes first, got %+v, %d, %v", list, total, err)
	}

	// summaries
	if summary, err := repo.GetSummary(ctx, session.ID, "f"); err != nil || summary != nil {
		t.Errorf("expected no summary, got %+v, %v", summary, err)
	}
	for _, upTo := range []int64{messages[1].Seq, messages[2].Seq} {
		if err := repo.SaveSummary(ctx, sessions.Summary{SessionID: session.ID, FocusHash: "f", UpToSeq: upTo,
			MessageCount: 2, Text: "The user grows tomatoes.", Model: "fake/model"}); err != nil {
			t.Fatal(err)
		}
	}
	if summary, _ := repo.GetSummary(ctx, session.ID, "f"); summary == nil || summary.UpToSeq != messages[2].Seq {
		t.Errorf("the summary should be replaced, got %+v", summary)
	}

	deleted, err := repo.DeleteSession(ctx, session.ID, false)
	if err != nil || !deleted {
		t.Fatalf("expected the session to be deleted, got %v", err)
	}
	if _, _, err := repo.GetMessages(ctx, session.ID, 0, 0); !errors.Is(err, sessions.ErrSessionNotFound) {
		t.Errorf("the messages should go with the session, got %v", err)
	}
	if summary, _ := repo.GetSummary(ctx, session.ID, "f"); summary != nil {
		t.Errorf("the summary should go with the session")
	}
}

func TestSessionRepositoryConcurrentCreate(t *testing.T) {
	repo := newTestSessionRepository(t)

	var wg sync.WaitGroup
	created := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, isNew, err := repo.GetOrCreateSession(context.Background(), "u-5", "shared")
			if err != nil {
				t.Error(err)
			}
			created <- isNew
		}()
	}
	wg.Wait()
	close(created)
	count := 0
	for isNew := range created {
		if isNew {
			count++
		}
	}
	if count != 1 {
		t.Errorf("exactly one request should create the session, got %d", count)
	}
}

func TestSessionRepositoryMemories(t *testing.T) {
	repo := newTestSessionRepository(t)
	ctx := context.Background()
	session, _, _ := repo.GetOrCreateSession(ctx, "u-5", "greenhouse")

	saved, err := repo.SaveMemory(ctx, sessions.Memory{UserID: "u-5", Key: "zone", Value: `{"hardiness": "8a"}`}, nil)
	if err != nil || saved.Value != `{"hardiness": "8a"}` || saved.UpdatedAt == 0 || saved.CreatedAt != saved.UpdatedAt {
		t.Fatalf("wrong memory %+v, %v", saved, err)
	}
	updated, _ := repo.SaveMemory(ctx, sessions.Memory{UserID: "u-5", Key: "zone", Value: `"8b"`}, nil)
	if updated.UpdatedAt <= saved.UpdatedAt || updated.CreatedAt != saved.CreatedAt || updated.Value != `"8b"` {
		t.Errorf("updated_at should move on and created_at stay, got %+v", updated)
	}
	repo.SaveMemory(ctx, sessions.Memory{UserID: "u-5", SessionID: session.ID, Key: "zone", Value: `"9a"`}, nil)

	// optimistic concurrency
	stale := saved.UpdatedAt
	if _, err := repo.SaveMemory(ctx, sessions.Memory{UserID: "u-5", Key: "zone", Value: `"7"`}, &stale); !errors.Is(err, sessions.ErrMemoryConflict) {
		t.Errorf("expected ErrMemoryConflict, got %v", err)
	}
	current := updated.UpdatedAt
	if _, err := repo.SaveMemory(ctx, sessions.Memory{UserID: "u-5", Key: "zone", Value: `"7"`}, &current); err != nil {
		t.Errorf("a save based on the current value should succeed, got %v", err)
	}
	zero := int64(0)
	if _, err := repo.SaveMemory(ctx, sessions.Memory{UserID: "u-5", Key: "zone", Value: `"7"`}, &zero); !errors.Is(err, sessions.ErrMemoryConflict) {
		t.Errorf("0 means the memory must not exist, got %v", err)
	}
	if _, err := repo.SaveMemory(ctx, sessions.Memory{UserID: "u-5", Key: "frost", Value: `"late"`}, &zero); err != nil {
		t.Errorf("a new memory with 0 should be saved, got %v", err)
	}

	all, _ := repo.GetMemories(ctx, "u-5", "", nil)
	userScope := ""
	user, _ := repo.GetMemories(ctx, "u-5", "", &userScope)
	scoped, _ := repo.GetMemories(ctx, "u-5", "zone", &session.ID)
	if len(all) != 3 || len(user) != 2 || len(scoped) != 1 || scoped[0].Value != `"9a"` {
		t.Errorf("wrong memories: %d %d %+v", len(all), len(user), scoped)
	}

	// deleting the session with its memories keeps the user's
	repo.DeleteSession(ctx, session.ID, true)
	if left, _ := repo.GetMemories(ctx, "u-5", "", nil); len(left) != 2 {
		t.Errorf("expected the 2 user memories, got %+v", left)
	}
	if count, _ := repo.DeleteMemories(ctx, "u-5", "zone", nil); count != 1 {
		t.Errorf("expected 1 memory deleted, got %d", count)
	}
	if count, _ := repo.ClearUserMemory(ctx, "u-5"); count != 1 {
		t.Errorf("expected 1 memory cleared, got %d", count)
	}
}
