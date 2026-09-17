package postgres

import (
	"context"
	"errors"
	"gogi/gogi/utils"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const GOGI_LLM_SESSION_TABLE_NAME string = "gogi_llm_sessions"

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

func (r *GogiLLMSessionRepository) CreateSession(ctx context.Context, userID string) (*LLMSession, error) {
	now := time.Now()
	session := &LLMSession{
		ID:        utils.NewUUIDString(),
		UserID:    userID,
		CreatedAt: now,
		UpdatedAt: now,
	}

	_, err := r.pool.Exec(ctx,
		`INSERT INTO gogi_llm_sessions (id, user_id, created_at, updated_at)
		 VALUES ($1, $2, $3, $4)`,
		session.ID, session.UserID, session.CreatedAt, session.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return session, nil
}

func (r *GogiLLMSessionRepository) GetSessionByID(ctx context.Context, sessionID string) (*LLMSession, error) {
	var s LLMSession

	err := r.pool.QueryRow(ctx,
		`SELECT id, user_id, created_at, updated_at
		 FROM gogi_llm_sessions
		 WHERE id = $1`,
		sessionID,
	).Scan(&s.ID, &s.UserID, &s.CreatedAt, &s.UpdatedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, utils.ErrJobNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *GogiLLMSessionRepository) GetSessionsByUserID(ctx context.Context, userID string) ([]*LLMSession, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, user_id, created_at, updated_at
		 FROM gogi_llm_sessions
		 WHERE user_id = $1
		 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []*LLMSession
	for rows.Next() {
		var s LLMSession
		if err := rows.Scan(&s.ID, &s.UserID, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		sessions = append(sessions, &s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return sessions, nil
}

func (r *GogiLLMSessionRepository) DeleteSession(ctx context.Context, sessionID string) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM gogi_llm_sessions WHERE id = $1`,
		sessionID,
	)
	return err
}

func (r *GogiLLMSessionRepository) AddMessages(ctx context.Context, sessionID string, messages []*LLMMessage) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	for _, msg := range messages {
		msg.ID = utils.NewUUIDString()
		msg.SessionID = sessionID
		msg.CreatedAt = time.Now()

		_, err := tx.Exec(ctx,
			`INSERT INTO gogi_llm_messages (id, session_id, role, content, name, timestamp, created_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			msg.ID, msg.SessionID, msg.Role, msg.Content, msg.Name, msg.Timestamp, msg.CreatedAt,
		)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (r *GogiLLMSessionRepository) GetMessages(ctx context.Context, sessionID string) ([]*LLMMessage, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, session_id, role, content, name, timestamp, created_at
		 FROM gogi_llm_messages
		 WHERE session_id = $1
		 ORDER BY timestamp ASC`,
		sessionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []*LLMMessage
	for rows.Next() {
		var m LLMMessage
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.Content, &m.Name, &m.Timestamp, &m.CreatedAt); err != nil {
			return nil, err
		}
		messages = append(messages, &m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return messages, nil
}

func (r *GogiLLMSessionRepository) SaveMemory(ctx context.Context, userID, key, value string) error {
	now := time.Now()
	_, err := r.pool.Exec(ctx,
		`INSERT INTO gogi_user_memory (id, user_id, key, value, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (user_id, key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()`,
		utils.NewUUIDString(), userID, key, value, now, now,
	)
	return err
}

func (r *GogiLLMSessionRepository) GetMemory(ctx context.Context, userID string) (map[string]string, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT key, value FROM gogi_user_memory WHERE user_id = $1`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	memory := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		memory[k] = v
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return memory, nil
}

func (r *GogiLLMSessionRepository) DeleteMemory(ctx context.Context, userID, key string) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM gogi_user_memory WHERE user_id = $1 AND key = $2`,
		userID, key,
	)
	return err
}

func (r *GogiLLMSessionRepository) ClearUserMemory(ctx context.Context, userID string) (int64, error) {
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM gogi_user_memory WHERE user_id = $1`,
		userID,
	)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
