package sessions

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"gogi/gogi/utils"
)

// MemoryStore is a Store that keeps everything in memory, for tests and development
type MemoryStore struct {
	mu        sync.Mutex
	sessions  map[string]*Session
	messages  map[string][]Message
	summaries map[string]Summary
	memories  map[string]Memory
	seq       int64
	now       func() time.Time
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		sessions:  map[string]*Session{},
		messages:  map[string][]Message{},
		summaries: map[string]Summary{},
		memories:  map[string]Memory{},
		now:       time.Now,
	}
}

func (s *MemoryStore) GetOrCreateSession(ctx context.Context, userID, sessionID string) (Session, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if sessionID == "" {
		sessionID = utils.NewUUIDString()
	}
	if session, ok := s.sessions[sessionID]; ok {
		if session.UserID != userID {
			return Session{}, false, fmt.Errorf("%w: %s", ErrSessionOfAnotherUser, sessionID)
		}
		return *session, false, nil
	}

	now := s.now()
	session := &Session{ID: sessionID, UserID: userID, CreatedAt: now, UpdatedAt: now}
	s.sessions[sessionID] = session
	return *session, true, nil
}

func (s *MemoryStore) GetSession(ctx context.Context, sessionID string) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[sessionID]
	if !ok {
		return Session{}, fmt.Errorf("%w: %s", ErrSessionNotFound, sessionID)
	}
	return *session, nil
}

func (s *MemoryStore) ListSessions(ctx context.Context, userID string, limit, offset int) ([]Session, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sessions := make([]Session, 0)
	for _, session := range s.sessions {
		if session.UserID == userID {
			sessions = append(sessions, *session)
		}
	}
	sort.Slice(sessions, func(i, j int) bool {
		if !sessions[i].UpdatedAt.Equal(sessions[j].UpdatedAt) {
			return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
		}
		return sessions[i].ID < sessions[j].ID
	})
	return page(sessions, limit, offset), len(sessions), nil
}

func page[T any](items []T, limit, offset int) []T {
	if offset >= len(items) {
		return []T{}
	}
	items = items[offset:]
	if limit > 0 && limit < len(items) {
		items = items[:limit]
	}
	return items
}

func (s *MemoryStore) DeleteSession(ctx context.Context, sessionID string, deleteMemories bool) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[sessionID]
	if !ok {
		return false, nil
	}
	delete(s.sessions, sessionID)
	delete(s.messages, sessionID)
	for key, summary := range s.summaries {
		if summary.SessionID == sessionID {
			delete(s.summaries, key)
		}
	}
	if deleteMemories {
		for key, memory := range s.memories {
			if memory.UserID == session.UserID && memory.SessionID == sessionID {
				delete(s.memories, key)
			}
		}
	}
	return true, nil
}

func (s *MemoryStore) AddMessages(ctx context.Context, sessionID string, messages []Message) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[sessionID]
	if !ok {
		return 0, fmt.Errorf("%w: %s", ErrSessionNotFound, sessionID)
	}
	for _, message := range messages {
		s.seq++
		message.Seq = s.seq
		s.messages[sessionID] = append(s.messages[sessionID], message)
	}
	session.MessageCount += len(messages)
	session.UpdatedAt = s.now()
	return session.MessageCount, nil
}

func (s *MemoryStore) GetMessages(ctx context.Context, sessionID string, limit, offset int) ([]Message, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.sessions[sessionID]; !ok {
		return nil, 0, fmt.Errorf("%w: %s", ErrSessionNotFound, sessionID)
	}
	messages := s.messages[sessionID]
	return append([]Message(nil), page(messages, limit, offset)...), len(messages), nil
}

func (s *MemoryStore) GetSummary(ctx context.Context, sessionID, focusHash string) (*Summary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	summary, ok := s.summaries[sessionID+"/"+focusHash]
	if !ok {
		return nil, nil
	}
	return &summary, nil
}

func (s *MemoryStore) SaveSummary(ctx context.Context, summary Summary) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.sessions[summary.SessionID]; !ok {
		return fmt.Errorf("%w: %s", ErrSessionNotFound, summary.SessionID)
	}
	s.summaries[summary.SessionID+"/"+summary.FocusHash] = summary
	return nil
}

func memoryKey(userID, sessionID, key string) string {
	return userID + "\x00" + sessionID + "\x00" + key
}

func (s *MemoryStore) SaveMemory(ctx context.Context, memory Memory, expectedUpdatedAt *int64) (Memory, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := memoryKey(memory.UserID, memory.SessionID, memory.Key)
	existing, exists := s.memories[id]
	if expectedUpdatedAt != nil {
		if (*expectedUpdatedAt == 0 && exists) || (*expectedUpdatedAt != 0 && (!exists || existing.UpdatedAt != *expectedUpdatedAt)) {
			return Memory{}, fmt.Errorf("%w: %s", ErrMemoryConflict, memory.Key)
		}
	}

	now := s.now().UnixMilli()
	memory.CreatedAt, memory.UpdatedAt = now, now
	if exists {
		memory.CreatedAt = existing.CreatedAt
		memory.UpdatedAt = max(now, existing.UpdatedAt+1)
	}
	s.memories[id] = memory
	return memory, nil
}

func (s *MemoryStore) matching(userID, key string, sessionID *string) []Memory {
	memories := make([]Memory, 0)
	for _, memory := range s.memories {
		if memory.UserID == userID && (key == "" || memory.Key == key) &&
			(sessionID == nil || memory.SessionID == *sessionID) {
			memories = append(memories, memory)
		}
	}
	sort.Slice(memories, func(i, j int) bool {
		if memories[i].Key != memories[j].Key {
			return memories[i].Key < memories[j].Key
		}
		return memories[i].SessionID < memories[j].SessionID
	})
	return memories
}

func (s *MemoryStore) GetMemories(ctx context.Context, userID, key string, sessionID *string) ([]Memory, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.matching(userID, key, sessionID), nil
}

func (s *MemoryStore) DeleteMemories(ctx context.Context, userID, key string, sessionID *string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	matching := s.matching(userID, key, sessionID)
	for _, memory := range matching {
		delete(s.memories, memoryKey(memory.UserID, memory.SessionID, memory.Key))
	}
	return len(matching), nil
}

func (s *MemoryStore) ClearUserMemory(ctx context.Context, userID string) (int, error) {
	return s.DeleteMemories(ctx, userID, "", nil)
}
