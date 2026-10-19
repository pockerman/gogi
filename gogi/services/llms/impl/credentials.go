package impl

import (
	"context"
	"errors"
	"regexp"
	"time"

	"gogi/gogi/applications"
	"gogi/gogi/credentials"
	gogiv1 "gogi/gogi/gogi/v1"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var credentialNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)

// CredentialServer keeps the credentials of each application: its values in the credential store,
// qualified by the application, and which it has in the gogi_credentials table, to list them
type CredentialServer struct {
	gogiv1.UnimplementedCredentialServerServer
	store credentials.CredentialStore
	pool  *pgxpool.Pool
}

func NewCredentialServer(store credentials.CredentialStore, pool *pgxpool.Pool) *CredentialServer {
	return &CredentialServer{store: store, pool: pool}
}

func (s *CredentialServer) StoreCredential(ctx context.Context, req *gogiv1.StoreCredentialRequest) (*gogiv1.StoreCredentialResponse, error) {
	if !credentialNamePattern.MatchString(req.GetName()) {
		return nil, status.Errorf(codes.InvalidArgument,
			"a credential's name is up to 63 lowercase letters, digits, '-' and '_', not %q", req.GetName())
	}
	if req.GetValue() == "" {
		return nil, status.Error(codes.InvalidArgument, "a credential needs a value")
	}
	if s.store == nil {
		return nil, status.Error(codes.FailedPrecondition, "no credential store is configured")
	}

	application := applications.FromContext(ctx)
	name := applications.Qualify(application, req.GetName())
	err := s.store.Store(ctx, name, req.GetValue())
	if errors.Is(err, credentials.ErrCredentialExists) {
		err = s.store.Rotate(ctx, name, req.GetValue())
	}
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "failed to store the credential %q: %v", req.GetName(), err)
	}

	var credential gogiv1.Credential
	var createdAt, updatedAt time.Time
	err = s.pool.QueryRow(ctx,
		`INSERT INTO gogi_credentials (application, name) VALUES ($1, $2)
		 ON CONFLICT (application, name) DO UPDATE SET updated_at = NOW()
		 RETURNING created_at, updated_at`, application, req.GetName()).Scan(&createdAt, &updatedAt)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to record the credential %q: %v", req.GetName(), err)
	}
	credential.Name, credential.CreatedAt, credential.UpdatedAt = req.GetName(), createdAt.Format(time.RFC3339), updatedAt.Format(time.RFC3339)
	return &gogiv1.StoreCredentialResponse{Credential: &credential}, nil
}

func (s *CredentialServer) ListCredentials(ctx context.Context, _ *gogiv1.ListCredentialsRequest) (*gogiv1.ListCredentialsResponse, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT name, created_at, updated_at FROM gogi_credentials WHERE application = $1 ORDER BY name`,
		applications.FromContext(ctx))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list the credentials: %v", err)
	}
	listed, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (*gogiv1.Credential, error) {
		var name string
		var createdAt, updatedAt time.Time
		if err := row.Scan(&name, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		return &gogiv1.Credential{Name: name, CreatedAt: createdAt.Format(time.RFC3339), UpdatedAt: updatedAt.Format(time.RFC3339)}, nil
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list the credentials: %v", err)
	}
	return &gogiv1.ListCredentialsResponse{Credentials: listed}, nil
}
