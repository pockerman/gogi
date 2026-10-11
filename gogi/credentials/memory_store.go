package credentials

import (
	"context"
	"fmt"
	"sync"
)

// MemoryCredentialStore keeps credentials in memory, for development and tests
type MemoryCredentialStore struct {
	mu          sync.RWMutex
	credentials map[string]string
}

func NewMemoryCredentialStore() *MemoryCredentialStore {
	return &MemoryCredentialStore{credentials: make(map[string]string)}
}

func (s *MemoryCredentialStore) Store(ctx context.Context, name, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.credentials[name]; ok {
		return fmt.Errorf("%w: %q", ErrCredentialExists, name)
	}
	s.credentials[name] = value
	return nil
}

func (s *MemoryCredentialStore) Retrieve(ctx context.Context, name string) (Credential, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	value, ok := s.credentials[name]
	if !ok {
		return Credential{}, fmt.Errorf("%w: %q", ErrCredentialNotFound, name)
	}
	return Credential{Name: name, Value: value}, nil
}

func (s *MemoryCredentialStore) Rotate(ctx context.Context, name, newValue string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.credentials[name]; !ok {
		return fmt.Errorf("%w: %q", ErrCredentialNotFound, name)
	}
	s.credentials[name] = newValue
	return nil
}
