package credentials

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"gogi/gogi/utils"
)

// testCredentialStoreContract checks the behaviour every CredentialStore must have
func testCredentialStoreContract(t *testing.T, store CredentialStore) {
	ctx := context.Background()
	// unique names, so the test can rerun against a persistent secrets manager
	name := "contract-test-" + utils.NewUUIDString()

	if _, err := store.Retrieve(ctx, name); !errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("expected ErrCredentialNotFound, got %v", err)
	}
	if err := store.Rotate(ctx, name, "value"); !errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("rotating a missing credential: expected ErrCredentialNotFound, got %v", err)
	}

	if err := store.Store(ctx, name, "secret-1"); err != nil {
		t.Fatal(err)
	}
	if err := store.Store(ctx, name, "secret-2"); !errors.Is(err, ErrCredentialExists) {
		t.Fatalf("expected ErrCredentialExists, got %v", err)
	}

	credential, err := store.Retrieve(ctx, name)
	if err != nil || credential.Name != name || credential.Value != "secret-1" {
		t.Fatalf("wrong credential %+v, %v", credential, err)
	}

	if err := store.Rotate(ctx, name, "secret-3"); err != nil {
		t.Fatal(err)
	}
	credential, err = store.Retrieve(ctx, name)
	if err != nil || credential.Value != "secret-3" {
		t.Fatalf("credential not rotated %+v, %v", credential, err)
	}
}

func TestMemoryCredentialStore(t *testing.T) {
	testCredentialStoreContract(t, NewMemoryCredentialStore())
}

func TestCachingCredentialStoreContract(t *testing.T) {
	testCredentialStoreContract(t, NewCachingCredentialStore(NewMemoryCredentialStore(), time.Hour))
}

// countingStore counts the calls to Retrieve
type countingStore struct {
	CredentialStore
	retrieves int
}

func (s *countingStore) Retrieve(ctx context.Context, name string) (Credential, error) {
	s.retrieves++
	return s.CredentialStore.Retrieve(ctx, name)
}

func TestCachingCredentialStore(t *testing.T) {
	ctx := context.Background()
	backend := &countingStore{CredentialStore: NewMemoryCredentialStore()}
	cache := NewCachingCredentialStore(backend, time.Minute)

	now := time.Now()
	cache.now = func() time.Time { return now }

	if err := backend.Store(ctx, "api-key", "v1"); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 3; i++ {
		if credential, err := cache.Retrieve(ctx, "api-key"); err != nil || credential.Value != "v1" {
			t.Fatalf("wrong credential %+v, %v", credential, err)
		}
	}
	if backend.retrieves != 1 {
		t.Errorf("expected 1 call to the backend, got %d", backend.retrieves)
	}

	// rotated directly in the secrets manager: picked up when the cache entry expires
	if err := backend.Rotate(ctx, "api-key", "v2"); err != nil {
		t.Fatal(err)
	}
	if credential, _ := cache.Retrieve(ctx, "api-key"); credential.Value != "v1" {
		t.Errorf("expected the cached value before the entry expires, got %q", credential.Value)
	}
	now = now.Add(2 * time.Minute)
	if credential, _ := cache.Retrieve(ctx, "api-key"); credential.Value != "v2" {
		t.Errorf("expected the rotated value after the entry expires, got %q", credential.Value)
	}

	// rotated through the store: picked up at once
	if err := cache.Rotate(ctx, "api-key", "v3"); err != nil {
		t.Fatal(err)
	}
	if credential, _ := cache.Retrieve(ctx, "api-key"); credential.Value != "v3" {
		t.Errorf("expected the rotated value at once, got %q", credential.Value)
	}
}

// The test runs against AWS Secrets Manager, e.g. LocalStack:
// GOGI_TEST_AWS_ENDPOINT_URL=http://localhost:4566 go test ./gogi/credentials/
func TestAWSSecretsManagerStore(t *testing.T) {
	endpoint := os.Getenv("GOGI_TEST_AWS_ENDPOINT_URL")
	if endpoint == "" {
		t.Skip("GOGI_TEST_AWS_ENDPOINT_URL is not set")
	}
	t.Setenv("AWS_ENDPOINT_URL", endpoint)
	t.Setenv("AWS_REGION", utils.GetEnv("AWS_REGION", "us-east-1"))
	t.Setenv("AWS_ACCESS_KEY_ID", utils.GetEnv("AWS_ACCESS_KEY_ID", "test"))
	t.Setenv("AWS_SECRET_ACCESS_KEY", utils.GetEnv("AWS_SECRET_ACCESS_KEY", "test"))

	store, err := NewAWSSecretsManagerStore(context.Background(), "gogi/test/")
	if err != nil {
		t.Fatal(err)
	}
	testCredentialStoreContract(t, store)
}

// The test runs against a Vault server, e.g. a dev server started with
// vault server -dev -dev-root-token-id=root:
// GOGI_TEST_VAULT_ADDR=http://localhost:8200 GOGI_TEST_VAULT_TOKEN=root go test ./gogi/credentials/
func TestVaultStore(t *testing.T) {
	address := os.Getenv("GOGI_TEST_VAULT_ADDR")
	if address == "" {
		t.Skip("GOGI_TEST_VAULT_ADDR is not set")
	}
	t.Setenv("VAULT_ADDR", address)
	t.Setenv("VAULT_TOKEN", utils.GetEnv("GOGI_TEST_VAULT_TOKEN", "root"))

	store, err := NewVaultStore("secret", "gogi/test/")
	if err != nil {
		t.Fatal(err)
	}
	testCredentialStoreContract(t, store)
}

func TestNewCredentialStoreFromEnv(t *testing.T) {
	ctx := context.Background()

	t.Setenv("GOGI_CREDENTIAL_STORE", "")
	if store, err := NewCredentialStoreFromEnv(ctx); store != nil || err != nil {
		t.Errorf("expected no store by default, got %v, %v", store, err)
	}

	t.Setenv("GOGI_CREDENTIAL_STORE", "vault")
	t.Setenv("VAULT_ADDR", "http://localhost:8200")
	store, err := NewCredentialStoreFromEnv(ctx)
	if err != nil {
		t.Fatal(err)
	}
	caching, ok := store.(*CachingCredentialStore)
	if !ok || caching.ttl != defaultCredentialCacheTTL {
		t.Fatalf("expected a caching store, got %T", store)
	}
	if vaultStore, ok := caching.store.(*VaultStore); !ok || vaultStore.prefix != defaultCredentialPrefix {
		t.Errorf("expected a Vault store, got %T", caching.store)
	}

	t.Setenv("GOGI_CREDENTIAL_STORE", "azure")
	if _, err := NewCredentialStoreFromEnv(ctx); err == nil {
		t.Errorf("expected an error for an unknown store")
	}

	t.Setenv("GOGI_CREDENTIAL_STORE", "vault")
	t.Setenv("GOGI_CREDENTIAL_CACHE_TTL", "soon")
	if _, err := NewCredentialStoreFromEnv(ctx); err == nil {
		t.Errorf("expected an error for an invalid cache TTL")
	}
}
