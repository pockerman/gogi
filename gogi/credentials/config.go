package credentials

import (
	"context"
	"fmt"
	"gogi/gogi/utils"
	"time"
)

const (
	// CredentialStoreAWS selects AWS Secrets Manager
	CredentialStoreAWS = "aws"
	// CredentialStoreVault selects HashiCorp Vault
	CredentialStoreVault = "vault"
	// CredentialStoreNone disables credentials
	CredentialStoreNone = "none"

	defaultCredentialPrefix   = "gogi/credentials/"
	defaultVaultKVMount       = "secret"
	defaultCredentialCacheTTL = time.Minute
)

// NewCredentialStoreFromEnv creates the credential store selected by
// GOGI_CREDENTIAL_STORE: "aws", "vault", or "none" (the default), in which case
// it returns nil. The store caches credentials for GOGI_CREDENTIAL_CACHE_TTL
//
// Other settings:
//   - GOGI_CREDENTIAL_PREFIX: prefix of the secret names/paths, "gogi/credentials/" by default
//   - aws: the standard AWS variables, e.g. AWS_REGION, AWS_ENDPOINT_URL for LocalStack
//   - vault: VAULT_ADDR, VAULT_TOKEN and GOGI_VAULT_KV_MOUNT, "secret" by default
func NewCredentialStoreFromEnv(ctx context.Context) (CredentialStore, error) {

	backend := utils.GetEnv("GOGI_CREDENTIAL_STORE", CredentialStoreNone)
	prefix := utils.GetEnv("GOGI_CREDENTIAL_PREFIX", defaultCredentialPrefix)

	ttl, err := time.ParseDuration(utils.GetEnv("GOGI_CREDENTIAL_CACHE_TTL", defaultCredentialCacheTTL.String()))
	if err != nil {
		return nil, fmt.Errorf("invalid GOGI_CREDENTIAL_CACHE_TTL: %w", err)
	}

	var store CredentialStore
	switch backend {
	case CredentialStoreNone, "":
		return nil, nil
	case CredentialStoreAWS:
		store, err = NewAWSSecretsManagerStore(ctx, prefix)
	case CredentialStoreVault:
		store, err = NewVaultStore(utils.GetEnv("GOGI_VAULT_KV_MOUNT", defaultVaultKVMount), prefix)
	default:
		return nil, fmt.Errorf("unknown GOGI_CREDENTIAL_STORE %q, expected %q, %q or %q",
			backend, CredentialStoreAWS, CredentialStoreVault, CredentialStoreNone)
	}
	if err != nil {
		return nil, err
	}

	return NewCachingCredentialStore(store, ttl), nil
}
