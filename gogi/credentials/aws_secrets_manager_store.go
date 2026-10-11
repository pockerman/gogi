package credentials

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
)

// AWSSecretsManagerStore keeps each credential as a secret in AWS Secrets Manager,
// named prefix + credential name, with the credential as its secret string
type AWSSecretsManagerStore struct {
	client *secretsmanager.Client
	prefix string
}

// NewAWSSecretsManagerStore creates a store using the default AWS configuration:
// the region, credentials and endpoint come from the environment (AWS_REGION,
// AWS_ACCESS_KEY_ID, ..., and AWS_ENDPOINT_URL, e.g. for LocalStack)
func NewAWSSecretsManagerStore(ctx context.Context, prefix string) (*AWSSecretsManagerStore, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load the AWS configuration: %w", err)
	}
	return &AWSSecretsManagerStore{client: secretsmanager.NewFromConfig(cfg), prefix: prefix}, nil
}

func (s *AWSSecretsManagerStore) secretID(name string) string {
	return s.prefix + name
}

func (s *AWSSecretsManagerStore) Store(ctx context.Context, name, value string) error {
	_, err := s.client.CreateSecret(ctx, &secretsmanager.CreateSecretInput{
		Name:         aws.String(s.secretID(name)),
		SecretString: aws.String(value),
	})

	var exists *types.ResourceExistsException
	if errors.As(err, &exists) {
		return fmt.Errorf("%w: %q", ErrCredentialExists, name)
	}
	if err != nil {
		return fmt.Errorf("failed to store credential %q: %w", name, err)
	}
	return nil
}

func (s *AWSSecretsManagerStore) Retrieve(ctx context.Context, name string) (Credential, error) {
	output, err := s.client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(s.secretID(name)),
	})

	var notFound *types.ResourceNotFoundException
	if errors.As(err, &notFound) {
		return Credential{}, fmt.Errorf("%w: %q", ErrCredentialNotFound, name)
	}
	if err != nil {
		return Credential{}, fmt.Errorf("failed to retrieve credential %q: %w", name, err)
	}
	if output.SecretString == nil {
		return Credential{}, fmt.Errorf("credential %q is not a string secret", name)
	}
	return Credential{Name: name, Value: *output.SecretString}, nil
}

func (s *AWSSecretsManagerStore) Rotate(ctx context.Context, name, newValue string) error {
	// the new version becomes AWSCURRENT; the previous one stays available as AWSPREVIOUS
	_, err := s.client.PutSecretValue(ctx, &secretsmanager.PutSecretValueInput{
		SecretId:     aws.String(s.secretID(name)),
		SecretString: aws.String(newValue),
	})

	var notFound *types.ResourceNotFoundException
	if errors.As(err, &notFound) {
		return fmt.Errorf("%w: %q", ErrCredentialNotFound, name)
	}
	if err != nil {
		return fmt.Errorf("failed to rotate credential %q: %w", name, err)
	}
	return nil
}
