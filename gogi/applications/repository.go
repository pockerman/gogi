package applications

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	// ErrNotFound is returned for an application gogi does not have
	ErrNotFound = errors.New("application not found")
	// ErrExists is returned when registering an application whose name is taken
	ErrExists = errors.New("application already exists")
	// ErrKeyNotFound is returned when revoking a key the application does not have
	ErrKeyNotFound = errors.New("key not found")
	// ErrNotAuthenticated is returned for a key gogi did not give, or that no longer works
	ErrNotAuthenticated = errors.New("not a key of an application of gogi")
)

// Application is an application that uses gogi
type Application struct {
	Name      string
	Disabled  bool
	CreatedAt time.Time
	Keys      []Key
}

// Key is what gogi keeps of a key: never the key itself
type Key struct {
	ID        string
	Shown     string
	CreatedAt time.Time
	RevokedAt *time.Time
}

// Repository keeps the applications and their keys, in the gogi_applications and
// gogi_application_keys tables
type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// Register adds the application, and gives it its first key. The key is not kept, so this is
// the only time it is seen
func (r *Repository) Register(ctx context.Context, name string) (string, error) {
	if err := CheckName(name); err != nil {
		return "", err
	}

	key := NewKey()
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO gogi_applications (name) VALUES ($1)`, name); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				return fmt.Errorf("%w: %s", ErrExists, name)
			}
			return err
		}
		return addKey(ctx, tx, name, key)
	})
	if err != nil {
		return "", err
	}
	return key, nil
}

// NewKey gives the application another key; its other keys keep working until they are revoked
func (r *Repository) NewKey(ctx context.Context, name string) (string, error) {
	if _, err := r.Get(ctx, name); err != nil {
		return "", err
	}
	key := NewKey()
	if err := addKey(ctx, r.pool, name, key); err != nil {
		return "", err
	}
	return key, nil
}

type executor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

func addKey(ctx context.Context, db executor, application, key string) error {
	_, err := db.Exec(ctx,
		`INSERT INTO gogi_application_keys (id, application, hash, prefix) VALUES ($1, $2, $3, $4)`,
		uuid.New(), application, Hash(key), Shown(key))
	return err
}

// RevokeKey stops the application's key that starts with what is shown of it from working
func (r *Repository) RevokeKey(ctx context.Context, name, shown string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE gogi_application_keys SET revoked_at = NOW()
		  WHERE application = $1 AND prefix = $2 AND revoked_at IS NULL`, name, Shown(shown))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: %s has no key %s", ErrKeyNotFound, name, Shown(shown))
	}
	return nil
}

// SetDisabled stops a disabled application's keys from working, or lets them work again
func (r *Repository) SetDisabled(ctx context.Context, name string, disabled bool) error {
	tag, err := r.pool.Exec(ctx, `UPDATE gogi_applications SET disabled = $2 WHERE name = $1`, name, disabled)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	return nil
}

// Delete removes the application, its keys and the list of its credentials. What it made stays in
// the platform, and its credentials' values in the credential store
func (r *Repository) Delete(ctx context.Context, name string) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM gogi_credentials WHERE application = $1`, name); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `DELETE FROM gogi_applications WHERE name = $1`, name)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("%w: %s", ErrNotFound, name)
		}
		return nil
	})
}

// Get is the application with its keys
func (r *Repository) Get(ctx context.Context, name string) (Application, error) {
	applications, err := r.list(ctx, `WHERE name = $1`, name)
	if err != nil {
		return Application{}, err
	}
	if len(applications) == 0 {
		return Application{}, fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	return applications[0], nil
}

// List is every application, with its keys
func (r *Repository) List(ctx context.Context) ([]Application, error) {
	return r.list(ctx, "")
}

func (r *Repository) list(ctx context.Context, where string, args ...any) ([]Application, error) {
	rows, err := r.pool.Query(ctx, `SELECT name, disabled, created_at FROM gogi_applications `+where+` ORDER BY name`, args...)
	if err != nil {
		return nil, err
	}
	applications, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Application, error) {
		var application Application
		err := row.Scan(&application.Name, &application.Disabled, &application.CreatedAt)
		return application, err
	})
	if err != nil {
		return nil, err
	}

	for i := range applications {
		rows, err := r.pool.Query(ctx,
			`SELECT id, prefix, created_at, revoked_at FROM gogi_application_keys
			  WHERE application = $1 ORDER BY created_at`, applications[i].Name)
		if err != nil {
			return nil, err
		}
		applications[i].Keys, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Key, error) {
			var key Key
			var id uuid.UUID
			err := row.Scan(&id, &key.Shown, &key.CreatedAt, &key.RevokedAt)
			key.ID = id.String()
			return key, err
		})
		if err != nil {
			return nil, err
		}
	}
	return applications, nil
}

// ApplicationOf is the application the key was given to, or ErrNotAuthenticated if gogi did not
// give it, it was revoked, or its application is disabled
func (r *Repository) ApplicationOf(ctx context.Context, key string) (string, error) {
	if !looksLikeKey(key) {
		return "", ErrNotAuthenticated
	}
	var application string
	err := r.pool.QueryRow(ctx,
		`SELECT a.name FROM gogi_application_keys k JOIN gogi_applications a ON a.name = k.application
		  WHERE k.hash = $1 AND k.revoked_at IS NULL AND NOT a.disabled`, Hash(key)).Scan(&application)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotAuthenticated
	}
	return application, err
}
