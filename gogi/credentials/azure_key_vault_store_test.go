package credentials

import (
	"context"
	"crypto/tls"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azsecrets"

	"gogi/gogi/utils"
)

func TestKeyVaultSecretNames(t *testing.T) {
	names := map[string]string{
		"gogi/credentials/qubit-notes.anthropic": "gogi-2fcredentials-2fqubit-2dnotes-2eanthropic",
		"openai":                                 "openai",
		"OpenAI":                                 "-4fpen-41-49",
	}
	for name, want := range names {
		got, err := keyVaultSecretName(name)
		if err != nil || got != want {
			t.Errorf("%q: expected %q, got %q, %v", name, want, got, err)
		}
	}
}

func TestKeyVaultSecretNamesAreDistinct(t *testing.T) {
	// Key Vault takes only letters, digits and dashes, and ignores case: names that differ must not meet
	names := []string{"a.b", "a-b", "a/b", "a-2eb", "A-b", "a-B", "ab", "a--b", "é", "-c3-a9"}
	seen := map[string]string{}
	for _, name := range names {
		secret, err := keyVaultSecretName(name)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Trim(secret, "abcdefghijklmnopqrstuvwxyz0123456789-") != "" {
			t.Errorf("%q: %q is not a Key Vault name", name, secret)
		}
		if other, ok := seen[strings.ToLower(secret)]; ok {
			t.Errorf("%q and %q are both kept as %q", other, name, secret)
		}
		seen[strings.ToLower(secret)] = name
	}
}

func TestKeyVaultSecretNamesFit(t *testing.T) {
	if _, err := keyVaultSecretName(strings.Repeat("a", 127)); err != nil {
		t.Errorf("a name of 127 letters fits: %v", err)
	}
	if _, err := keyVaultSecretName(strings.Repeat("a", 126) + "."); err == nil {
		t.Errorf("expected an error for a name too long for Key Vault")
	}
}

// anyToken is accepted by Lowkey Vault, which doesn't check tokens
type anyToken struct{}

func (anyToken) GetToken(context.Context, policy.TokenRequestOptions) (azcore.AccessToken, error) {
	return azcore.AccessToken{Token: "test", ExpiresOn: time.Now().Add(time.Hour)}, nil
}

// The test runs against Azure Key Vault, or Lowkey Vault, its emulator:
// docker run -d -p 8443:8443 nagyesta/lowkey-vault:5.0.0
// GOGI_TEST_AZURE_KEY_VAULT_URL=https://localhost:8443 go test ./gogi/credentials/
func TestAzureKeyVaultStore(t *testing.T) {
	vaultURL := os.Getenv("GOGI_TEST_AZURE_KEY_VAULT_URL")
	if vaultURL == "" {
		t.Skip("GOGI_TEST_AZURE_KEY_VAULT_URL is not set")
	}

	// the emulator's certificate is its own, its address isn't a vault's, and it doesn't know the SDK's newest API
	transport := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	options := &azsecrets.ClientOptions{
		ClientOptions: azcore.ClientOptions{
			Transport:  &http.Client{Transport: transport},
			APIVersion: utils.GetEnv("GOGI_TEST_AZURE_KEY_VAULT_API_VERSION", "7.6"),
		},
		DisableChallengeResourceVerification: true,
	}
	store, err := NewAzureKeyVaultStore(vaultURL, "gogi/test/", anyToken{}, options)
	if err != nil {
		t.Fatal(err)
	}
	testCredentialStoreContract(t, store)
}
