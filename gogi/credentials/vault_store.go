package credentials

import (
	"context"
	"errors"
	"fmt"

	vault "github.com/hashicorp/vault/api"
)

// vaultValueKey is the key of the credential in the secret's data
const vaultValueKey = "value"

// VaultStore keeps each credential as a secret in a HashiCorp Vault KV v2 secrets
// engine, at path prefix + credential name, with the credential under the "value" key
type VaultStore struct {
	kv     *vault.KVv2
	prefix string
}

// NewVaultStore creates a store for the KV v2 secrets engine mounted at mount. The
// Vault address and token come from the environment (VAULT_ADDR and VAULT_TOKEN)
func NewVaultStore(mount, prefix string) (*VaultStore, error) {
	client, err := vault.NewClient(vault.DefaultConfig())
	if err != nil {
		return nil, fmt.Errorf("failed to create the Vault client: %w", err)
	}
	return &VaultStore{kv: client.KVv2(mount), prefix: prefix}, nil
}

func (s *VaultStore) secretPath(name string) string {
	return s.prefix + name
}

// get returns the secret of the credential, or ErrCredentialNotFound
func (s *VaultStore) get(ctx context.Context, name string) (*vault.KVSecret, error) {
	secret, err := s.kv.Get(ctx, s.secretPath(name))
	if errors.Is(err, vault.ErrSecretNotFound) {
		return nil, fmt.Errorf("%w: %q", ErrCredentialNotFound, name)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read credential %q: %w", name, err)
	}
	return secret, nil
}

func (s *VaultStore) Store(ctx context.Context, name, value string) error {
	_, err := s.get(ctx, name)
	if err == nil {
		return fmt.Errorf("%w: %q", ErrCredentialExists, name)
	}
	if !errors.Is(err, ErrCredentialNotFound) {
		return err
	}

	// check-and-set 0 writes the secret only if it does not exist yet
	_, err = s.kv.Put(ctx, s.secretPath(name), map[string]interface{}{vaultValueKey: value},
		vault.WithCheckAndSet(0))
	if err != nil {
		return fmt.Errorf("failed to store credential %q: %w", name, err)
	}
	return nil
}

func (s *VaultStore) Retrieve(ctx context.Context, name string) (Credential, error) {
	secret, err := s.get(ctx, name)
	if err != nil {
		return Credential{}, err
	}

	value, ok := secret.Data[vaultValueKey].(string)
	if !ok {
		return Credential{}, fmt.Errorf("credential %q has no string %q key", name, vaultValueKey)
	}
	return Credential{Name: name, Value: value}, nil
}

func (s *VaultStore) Rotate(ctx context.Context, name, newValue string) error {
	secret, err := s.get(ctx, name)
	if err != nil {
		return err
	}

	// check-and-set against the version read fails if the secret changed in between
	_, err = s.kv.Put(ctx, s.secretPath(name), map[string]interface{}{vaultValueKey: newValue},
		vault.WithCheckAndSet(secret.VersionMetadata.Version))
	if err != nil {
		return fmt.Errorf("failed to rotate credential %q: %w", name, err)
	}
	return nil
}
