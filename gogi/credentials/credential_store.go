// Package credentials manages the secrets the platform uses to call external
// services, e.g. the API key of a self-hosted model. Secrets are referenced by
// name (a credential_ref) so they never appear in registrations, logs or responses.
package credentials

import (
	"context"
	"errors"
)

var (
	// ErrCredentialNotFound is returned when no credential has the requested name
	ErrCredentialNotFound = errors.New("credential not found")
	// ErrCredentialExists is returned when storing a credential whose name is taken
	ErrCredentialExists = errors.New("credential already exists")
)

// Credential is a named secret
type Credential struct {
	Name  string
	Value string
}

// CredentialStore is the platform's credential management (Designing AI Systems,
// listing 6.14). Implementations wrap an existing secrets manager, e.g. AWS Secrets
// Manager or HashiCorp Vault, rather than storing secrets themselves
type CredentialStore interface {
	// Store creates the credential; it returns ErrCredentialExists if the name is taken
	Store(ctx context.Context, name, value string) error

	// Retrieve returns the current value of the credential, or ErrCredentialNotFound
	Retrieve(ctx context.Context, name string) (Credential, error)

	// Rotate replaces the value of an existing credential, or returns ErrCredentialNotFound.
	// Requests in flight complete with the old value; new requests get the new one
	Rotate(ctx context.Context, name, newValue string) error
}
