package credentials

import (
	"context"
	"sync"
	"time"
)

type cachedCredential struct {
	credential Credential
	expiresAt  time.Time
}

// CachingCredentialStore caches retrieved credentials for a time to live, so a
// secrets manager is not called on every request. A credential rotated through
// the store is refreshed at once; one rotated directly in the secrets manager,
// e.g. by the operations team, is picked up when its cache entry expires
type CachingCredentialStore struct {
	store CredentialStore
	ttl   time.Duration
	now   func() time.Time

	mu    sync.Mutex
	cache map[string]cachedCredential
}

func NewCachingCredentialStore(store CredentialStore, ttl time.Duration) *CachingCredentialStore {
	return &CachingCredentialStore{
		store: store,
		ttl:   ttl,
		now:   time.Now,
		cache: make(map[string]cachedCredential),
	}
}

func (s *CachingCredentialStore) Store(ctx context.Context, name, value string) error {
	if err := s.store.Store(ctx, name, value); err != nil {
		return err
	}
	s.invalidate(name)
	return nil
}

func (s *CachingCredentialStore) Retrieve(ctx context.Context, name string) (Credential, error) {
	s.mu.Lock()
	cached, ok := s.cache[name]
	s.mu.Unlock()

	if ok && s.now().Before(cached.expiresAt) {
		return cached.credential, nil
	}

	credential, err := s.store.Retrieve(ctx, name)
	if err != nil {
		return Credential{}, err
	}

	s.mu.Lock()
	s.cache[name] = cachedCredential{credential: credential, expiresAt: s.now().Add(s.ttl)}
	s.mu.Unlock()
	return credential, nil
}

func (s *CachingCredentialStore) Rotate(ctx context.Context, name, newValue string) error {
	if err := s.store.Rotate(ctx, name, newValue); err != nil {
		return err
	}
	s.invalidate(name)
	return nil
}

func (s *CachingCredentialStore) invalidate(name string) {
	s.mu.Lock()
	delete(s.cache, name)
	s.mu.Unlock()
}
