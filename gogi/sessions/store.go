package sessions

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrSessionNotFound is returned for a session that does not exist
	ErrSessionNotFound = errors.New("session not found")
	// ErrSessionOfAnotherUser is returned when a user asks for a session that belongs to another user
	ErrSessionOfAnotherUser = errors.New("session belongs to another user")
	// ErrMemoryConflict is returned when a memory changed since the caller read it
	ErrMemoryConflict = errors.New("memory changed since it was read")
)

// Message roles
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

// Session is a conversation of a user with an AI application
type Session struct {
	ID           string
	UserID       string
	MessageCount int
	CreatedAt    time.Time
	// UpdatedAt is the last time messages were added
	UpdatedAt time.Time
}

// ToolCall is a tool an assistant message calls, in the OpenAI format
type ToolCall struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Message is a message of a session. Seq orders the messages of a session; the store assigns it
type Message struct {
	Seq        int64
	Role       string
	Content    *string
	ToolCalls  []ToolCall
	ToolCallID *string
	Name       *string
	// Timestamp is when the message was produced, in unix millis
	Timestamp int64
}

// Text returns the message's content, or "" if it has none
func (m Message) Text() string {
	if m.Content == nil {
		return ""
	}
	return *m.Content
}

// Memory is a fact saved about a user, for every session, or for one if SessionID is set
type Memory struct {
	UserID    string
	SessionID string
	Key       string
	// Value is a JSON document
	Value     string
	CreatedAt int64 // unix millis
	UpdatedAt int64 // unix millis; strictly increases with every save
}

// Summary is a summary of the oldest messages of a session, up to and including UpToSeq
type Summary struct {
	SessionID string
	// FocusHash identifies what the summary was asked to keep
	FocusHash    string
	UpToSeq      int64
	MessageCount int
	Text         string
	Model        string
}

// Store stores sessions, their messages and summaries, and memories
type Store interface {
	// GetOrCreateSession returns the session, or creates it for the user if it does not
	// exist; sessionID "" creates a session with a new id. It returns whether the session
	// was created, and an error wrapping ErrSessionOfAnotherUser for another user's session
	GetOrCreateSession(ctx context.Context, userID, sessionID string) (Session, bool, error)
	// GetSession returns the session, or an error wrapping ErrSessionNotFound
	GetSession(ctx context.Context, sessionID string) (Session, error)
	// ListSessions returns a page of the user's sessions, most recently active first, and
	// the number of sessions of the user; limit 0 means all
	ListSessions(ctx context.Context, userID string, limit, offset int) ([]Session, int, error)
	// DeleteSession deletes the session, its messages and summaries, and its memories if asked
	DeleteSession(ctx context.Context, sessionID string, deleteMemories bool) (bool, error)

	// AddMessages appends the messages to the session, all of them or none, and returns
	// the number of messages of the session
	AddMessages(ctx context.Context, sessionID string, messages []Message) (int, error)
	// GetMessages returns a page of the session's messages, oldest first, and the number
	// of messages of the session; limit 0 means all
	GetMessages(ctx context.Context, sessionID string, limit, offset int) ([]Message, int, error)

	// GetSummary returns the summary of the session for the focus, or nil if there is none
	GetSummary(ctx context.Context, sessionID, focusHash string) (*Summary, error)
	// SaveSummary saves the summary, replacing the one of the same session and focus
	SaveSummary(ctx context.Context, summary Summary) error

	// SaveMemory saves the memory. If expectedUpdatedAt is not nil, the save only succeeds
	// if the memory was last updated then, or does not exist if it is 0; otherwise it
	// returns an error wrapping ErrMemoryConflict
	SaveMemory(ctx context.Context, memory Memory, expectedUpdatedAt *int64) (Memory, error)
	// GetMemories returns the user's memories with the key, or all if key is ""; of the
	// session's scope only if sessionID is not nil ("" is the user's own scope), of every
	// scope otherwise. Ordered by key and scope
	GetMemories(ctx context.Context, userID, key string, sessionID *string) ([]Memory, error)
	// DeleteMemories deletes the user's memories with the key, of the session's scope only
	// if sessionID is not nil, of every scope otherwise
	DeleteMemories(ctx context.Context, userID, key string, sessionID *string) (int, error)
	// ClearUserMemory deletes every memory of the user
	ClearUserMemory(ctx context.Context, userID string) (int, error)
}
