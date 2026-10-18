package credentials

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azsecrets"
)

// Key Vault's names are at most 127 characters
const keyVaultMaxNameLength = 127

// AzureKeyVaultStore keeps each credential as a secret in Azure Key Vault, with the credential as its value. Key
// Vault's names take only letters, digits and dashes, and ignore case, so prefix + credential name is encoded
// (keyVaultSecretName). Rotating a credential adds a version to its secret; Key Vault keeps the earlier ones
type AzureKeyVaultStore struct {
	client *azsecrets.Client
	prefix string
}

// NewAzureKeyVaultStore creates a store for the vault at vaultURL, e.g. https://gogi.vault.azure.net/, signed in
// with credential, e.g. the server's managed identity; options may be nil
func NewAzureKeyVaultStore(
	vaultURL, prefix string, credential azcore.TokenCredential, options *azsecrets.ClientOptions,
) (*AzureKeyVaultStore, error) {
	client, err := azsecrets.NewClient(vaultURL, credential, options)
	if err != nil {
		return nil, fmt.Errorf("failed to create the Azure Key Vault client: %w", err)
	}
	return &AzureKeyVaultStore{client: client, prefix: prefix}, nil
}

// NewAzureKeyVaultStoreFromDefaults creates a store signed in as Azure's tools are, from the environment: on an
// Azure VM, its managed identity (AZURE_CLIENT_ID chooses one of several); elsewhere, e.g. AZURE_TENANT_ID,
// AZURE_CLIENT_ID and AZURE_CLIENT_SECRET, or the Azure CLI's sign-in
func NewAzureKeyVaultStoreFromDefaults(vaultURL, prefix string) (*AzureKeyVaultStore, error) {
	credential, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to find Azure credentials: %w", err)
	}
	return NewAzureKeyVaultStore(vaultURL, prefix, credential, nil)
}

// keyVaultSecretName is the secret's name for a credential's: lowercase letters and digits are kept, and every
// other byte, the dash too, is a dash and its two hex digits. So names that differ, in case too, are kept apart
func keyVaultSecretName(name string) (string, error) {
	var secret strings.Builder
	for _, b := range []byte(name) {
		if ('a' <= b && b <= 'z') || ('0' <= b && b <= '9') {
			secret.WriteByte(b)
		} else {
			fmt.Fprintf(&secret, "-%02x", b)
		}
	}
	if secret.Len() > keyVaultMaxNameLength {
		return "", fmt.Errorf("credential %q is too long a name for Azure Key Vault", name)
	}
	return secret.String(), nil
}

func (s *AzureKeyVaultStore) secretName(name string) (string, error) {
	return keyVaultSecretName(s.prefix + name)
}

func isStatus(err error, status int) bool {
	var response *azcore.ResponseError
	return errors.As(err, &response) && response.StatusCode == status
}

// exists says whether the secret is there, and not deleted
func (s *AzureKeyVaultStore) exists(ctx context.Context, name, secret string) (bool, error) {
	_, err := s.client.GetSecret(ctx, secret, "", nil)
	if isStatus(err, http.StatusNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to look up credential %q: %w", name, err)
	}
	return true, nil
}

func (s *AzureKeyVaultStore) set(ctx context.Context, name, secret, value string) error {
	_, err := s.client.SetSecret(ctx, secret, azsecrets.SetSecretParameters{Value: &value}, nil)
	if isStatus(err, http.StatusConflict) {
		// deleted, but kept by Key Vault for recovery until it's purged
		return fmt.Errorf("%w: %q was deleted, and Azure Key Vault keeps it until it is purged", ErrCredentialExists, name)
	}
	if err != nil {
		return fmt.Errorf("failed to store credential %q: %w", name, err)
	}
	return nil
}

func (s *AzureKeyVaultStore) Store(ctx context.Context, name, value string) error {
	secret, err := s.secretName(name)
	if err != nil {
		return err
	}
	exists, err := s.exists(ctx, name, secret)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("%w: %q", ErrCredentialExists, name)
	}
	return s.set(ctx, name, secret, value)
}

func (s *AzureKeyVaultStore) Retrieve(ctx context.Context, name string) (Credential, error) {
	secret, err := s.secretName(name)
	if err != nil {
		return Credential{}, err
	}
	response, err := s.client.GetSecret(ctx, secret, "", nil)
	if isStatus(err, http.StatusNotFound) {
		return Credential{}, fmt.Errorf("%w: %q", ErrCredentialNotFound, name)
	}
	if err != nil {
		return Credential{}, fmt.Errorf("failed to retrieve credential %q: %w", name, err)
	}
	if response.Value == nil {
		return Credential{}, fmt.Errorf("credential %q has no value", name)
	}
	return Credential{Name: name, Value: *response.Value}, nil
}

func (s *AzureKeyVaultStore) Rotate(ctx context.Context, name, newValue string) error {
	secret, err := s.secretName(name)
	if err != nil {
		return err
	}
	exists, err := s.exists(ctx, name, secret)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("%w: %q", ErrCredentialNotFound, name)
	}
	// the new version is the current one; requests in flight complete with the one they have
	return s.set(ctx, name, secret, newValue)
}
