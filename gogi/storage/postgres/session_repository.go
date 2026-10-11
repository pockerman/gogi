package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"gogi/gogi/sessions"
	"gogi/gogi/utils"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const GOGI_LLM_SESSION_TABLE_NAME string = "gogi_llm_sessions"

var _ sessions.Store = (*GogiLLMSessionRepository)(nil)

// GogiLLMSessionRepository stores sessions, their messages and summaries, and memories.
// It implements sessions.Store
type GogiLLMSessionRepository struct {
	pool *pgxpool.Pool
}

func NewGogiLLMSessionRepository(
	pool *pgxpool.Pool,
) *GogiLLMSessionRepository {

	return &GogiLLMSessionRepository{
		pool: pool,
	}
}

const sessionColumns = `id, user_id, message_count, created_at, updated_at`

func scanSession(row pgx.Row) (sessions.Session, error) {
	var session sessions.Session
	err := row.Scan(&session.ID, &session.UserID, &session.MessageCount, &session.CreatedAt, &session.UpdatedAt)
	return session, err
}

// GetOrCreateSession returns the session, or creates it. Two requests that create the same
// session at once both get it: the second insert does nothing and the select finds the first
func (r *GogiLLMSessionRepository) GetOrCreateSession(ctx context.Context, userID, sessionID string) (sessions.Session, bool, error) {
	if sessionID == "" {
		sessionID = utils.NewUUIDString()
	}

	tag, err := r.pool.Exec(ctx,
		`INSERT INTO gogi_llm_sessions (id, user_id) VALUES ($1, $2) ON CONFLICT (id) DO NOTHING`,
		sessionID, userID)
	if err != nil {
		return sessions.Session{}, false, err
	}

	session, err := r.GetSession(ctx, sessionID)
	if err != nil {
		return sessions.Session{}, false, err
	}
	if session.UserID != userID {
		return sessions.Session{}, false, fmt.Errorf("%w: %s", sessions.ErrSessionOfAnotherUser, sessionID)
	}
	return session, tag.RowsAffected() == 1, nil
}

func (r *GogiLLMSessionRepository) GetSession(ctx context.Context, sessionID string) (sessions.Session, error) {
	session, err := scanSession(r.pool.QueryRow(ctx,
		`SELECT `+sessionColumns+` FROM gogi_llm_sessions WHERE id = $1`, sessionID))
	if errors.Is(err, pgx.ErrNoRows) {
		return sessions.Session{}, fmt.Errorf("%w: %s", sessions.ErrSessionNotFound, sessionID)
	}
	return session, err
}

func (r *GogiLLMSessionRepository) ListSessions(ctx context.Context, userID string, limit, offset int) ([]sessions.Session, int, error) {
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM gogi_llm_sessions WHERE user_id = $1`, userID).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := `SELECT ` + sessionColumns + ` FROM gogi_llm_sessions WHERE user_id = $1
		ORDER BY updated_at DESC, id OFFSET $2`
	args := []any{userID, offset}
	if limit > 0 {
		query += ` LIMIT $3`
		args = append(args, limit)
	}
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	list := make([]sessions.Session, 0)
	for rows.Next() {
		session, err := scanSession(rows)
		if err != nil {
			return nil, 0, err
		}
		list = append(list, session)
	}
	return list, total, rows.Err()
}

// DeleteSession deletes the session; its messages and summaries go with it (ON DELETE CASCADE)
func (r *GogiLLMSessionRepository) DeleteSession(ctx context.Context, sessionID string, deleteMemories bool) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	var userID string
	err = tx.QueryRow(ctx, `DELETE FROM gogi_llm_sessions WHERE id = $1 RETURNING user_id`, sessionID).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if deleteMemories {
		if _, err := tx.Exec(ctx, `DELETE FROM gogi_llm_memories WHERE user_id = $1 AND session_id = $2`,
			userID, sessionID); err != nil {
			return false, err
		}
	}
	return true, tx.Commit(ctx)
}

// AddMessages appends the messages in one transaction, and counts them on the session
func (r *GogiLLMSessionRepository) AddMessages(ctx context.Context, sessionID string, messages []sessions.Message) (int, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	// the update locks the session's row, so concurrent appends to a session do not interleave
	var total int
	err = tx.QueryRow(ctx,
		`UPDATE gogi_llm_sessions SET message_count = message_count + $2, updated_at = NOW()
		 WHERE id = $1 RETURNING message_count`, sessionID, len(messages)).Scan(&total)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("%w: %s", sessions.ErrSessionNotFound, sessionID)
	}
	if err != nil {
		return 0, err
	}

	batch := &pgx.Batch{}
	for _, message := range messages {
		toolCalls, err := json.Marshal(nonNilToolCalls(message.ToolCalls))
		if err != nil {
			return 0, err
		}
		batch.Queue(`INSERT INTO gogi_llm_messages (session_id, role, content, tool_calls, tool_call_id, name, timestamp)
			VALUES ($1, $2, $3, $4::jsonb, $5, $6, $7)`,
			sessionID, message.Role, message.Content, string(toolCalls), message.ToolCallID, message.Name, message.Timestamp)
	}
	if err := tx.SendBatch(ctx, batch).Close(); err != nil {
		return 0, err
	}
	return total, tx.Commit(ctx)
}

func nonNilToolCalls(calls []sessions.ToolCall) []sessions.ToolCall {
	if calls == nil {
		return []sessions.ToolCall{}
	}
	return calls
}

func (r *GogiLLMSessionRepository) GetMessages(ctx context.Context, sessionID string, limit, offset int) ([]sessions.Message, int, error) {
	session, err := r.GetSession(ctx, sessionID)
	if err != nil {
		return nil, 0, err
	}

	query := `SELECT seq, role, content, tool_calls, tool_call_id, name, timestamp FROM gogi_llm_messages
		WHERE session_id = $1 ORDER BY seq OFFSET $2`
	args := []any{sessionID, offset}
	if limit > 0 {
		query += ` LIMIT $3`
		args = append(args, limit)
	}
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	messages := make([]sessions.Message, 0)
	for rows.Next() {
		var message sessions.Message
		var toolCalls []byte
		if err := rows.Scan(&message.Seq, &message.Role, &message.Content, &toolCalls, &message.ToolCallID,
			&message.Name, &message.Timestamp); err != nil {
			return nil, 0, err
		}
		if err := json.Unmarshal(toolCalls, &message.ToolCalls); err != nil {
			return nil, 0, err
		}
		if len(message.ToolCalls) == 0 {
			message.ToolCalls = nil
		}
		messages = append(messages, message)
	}
	return messages, session.MessageCount, rows.Err()
}

func (r *GogiLLMSessionRepository) GetSummary(ctx context.Context, sessionID, focusHash string) (*sessions.Summary, error) {
	summary := sessions.Summary{SessionID: sessionID, FocusHash: focusHash}
	err := r.pool.QueryRow(ctx,
		`SELECT up_to_seq, message_count, summary, model FROM gogi_llm_session_summaries
		 WHERE session_id = $1 AND focus_hash = $2`, sessionID, focusHash,
	).Scan(&summary.UpToSeq, &summary.MessageCount, &summary.Text, &summary.Model)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &summary, nil
}

func (r *GogiLLMSessionRepository) SaveSummary(ctx context.Context, summary sessions.Summary) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO gogi_llm_session_summaries (session_id, focus_hash, up_to_seq, message_count, summary, model)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (session_id, focus_hash) DO UPDATE SET up_to_seq = EXCLUDED.up_to_seq,
		     message_count = EXCLUDED.message_count, summary = EXCLUDED.summary, model = EXCLUDED.model,
		     updated_at = NOW()`,
		summary.SessionID, summary.FocusHash, summary.UpToSeq, summary.MessageCount, summary.Text, summary.Model)
	return err
}

const memoryColumns = `user_id, session_id, key, value::text, created_at, updated_at`

func scanMemory(row pgx.Row) (sessions.Memory, error) {
	var memory sessions.Memory
	err := row.Scan(&memory.UserID, &memory.SessionID, &memory.Key, &memory.Value, &memory.CreatedAt, &memory.UpdatedAt)
	return memory, err
}

// SaveMemory saves the memory. updated_at strictly increases with every save, so that a
// caller that read a memory can tell whether it changed since
func (r *GogiLLMSessionRepository) SaveMemory(ctx context.Context, memory sessions.Memory, expectedUpdatedAt *int64) (sessions.Memory, error) {
	now := time.Now().UnixMilli()

	var row pgx.Row
	switch {
	case expectedUpdatedAt == nil:
		row = r.pool.QueryRow(ctx,
			`INSERT INTO gogi_llm_memories (user_id, session_id, key, value, created_at, updated_at)
			 VALUES ($1, $2, $3, $4::jsonb, $5, $5)
			 ON CONFLICT (user_id, session_id, key) DO UPDATE SET value = EXCLUDED.value,
			     updated_at = GREATEST(EXCLUDED.updated_at, gogi_llm_memories.updated_at + 1)
			 RETURNING `+memoryColumns,
			memory.UserID, memory.SessionID, memory.Key, memory.Value, now)
	case *expectedUpdatedAt == 0:
		row = r.pool.QueryRow(ctx,
			`INSERT INTO gogi_llm_memories (user_id, session_id, key, value, created_at, updated_at)
			 VALUES ($1, $2, $3, $4::jsonb, $5, $5)
			 ON CONFLICT (user_id, session_id, key) DO NOTHING
			 RETURNING `+memoryColumns,
			memory.UserID, memory.SessionID, memory.Key, memory.Value, now)
	default:
		row = r.pool.QueryRow(ctx,
			`UPDATE gogi_llm_memories SET value = $4::jsonb, updated_at = GREATEST($5, updated_at + 1)
			 WHERE user_id = $1 AND session_id = $2 AND key = $3 AND updated_at = $6
			 RETURNING `+memoryColumns,
			memory.UserID, memory.SessionID, memory.Key, memory.Value, now, *expectedUpdatedAt)
	}

	saved, err := scanMemory(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return sessions.Memory{}, fmt.Errorf("%w: %s", sessions.ErrMemoryConflict, memory.Key)
	}
	return saved, err
}

func (r *GogiLLMSessionRepository) GetMemories(ctx context.Context, userID, key string, sessionID *string) ([]sessions.Memory, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+memoryColumns+` FROM gogi_llm_memories
		 WHERE user_id = $1 AND ($2 = '' OR key = $2) AND ($3::text IS NULL OR session_id = $3)
		 ORDER BY key, session_id`, userID, key, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	memories := make([]sessions.Memory, 0)
	for rows.Next() {
		memory, err := scanMemory(rows)
		if err != nil {
			return nil, err
		}
		memories = append(memories, memory)
	}
	return memories, rows.Err()
}

func (r *GogiLLMSessionRepository) DeleteMemories(ctx context.Context, userID, key string, sessionID *string) (int, error) {
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM gogi_llm_memories
		 WHERE user_id = $1 AND ($2 = '' OR key = $2) AND ($3::text IS NULL OR session_id = $3)`,
		userID, key, sessionID)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

func (r *GogiLLMSessionRepository) ClearUserMemory(ctx context.Context, userID string) (int, error) {
	return r.DeleteMemories(ctx, userID, "", nil)
}
