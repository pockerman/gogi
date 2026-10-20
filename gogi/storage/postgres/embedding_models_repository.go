package postgres

import (
	"context"
	"errors"
	"gogi/gogi/utils"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// EmbeddingModel is a text embedding model an application registered, kept under its qualified
// name
type EmbeddingModel struct {
	Name           string
	URL            string
	ServedName     string
	CredentialRef  string
	Dimensions     int
	MaxInputTokens int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type EmbeddingModelsRepository struct {
	pool *pgxpool.Pool
}

func NewEmbeddingModelsRepository(pool *pgxpool.Pool) *EmbeddingModelsRepository {
	return &EmbeddingModelsRepository{pool: pool}
}

const embeddingModelColumns = `name, url, served_name, credential_ref, dimensions, max_input_tokens, created_at, updated_at`

// Upsert registers the model, or replaces the registration of the model of the same name
func (r *EmbeddingModelsRepository) Upsert(ctx context.Context, model EmbeddingModel) (EmbeddingModel, error) {
	rows, err := r.pool.Query(ctx, `
		INSERT INTO gogi_embedding_models (name, url, served_name, credential_ref, dimensions, max_input_tokens)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (name) DO UPDATE SET url = EXCLUDED.url, served_name = EXCLUDED.served_name,
			credential_ref = EXCLUDED.credential_ref, dimensions = EXCLUDED.dimensions,
			max_input_tokens = EXCLUDED.max_input_tokens, updated_at = NOW()
		RETURNING `+embeddingModelColumns,
		model.Name, model.URL, model.ServedName, model.CredentialRef, model.Dimensions, model.MaxInputTokens)
	if err != nil {
		return EmbeddingModel{}, err
	}
	return pgx.CollectExactlyOneRow(rows, scanEmbeddingModel)
}

// Get returns the model, or ErrEmbeddingModelNotFound
func (r *EmbeddingModelsRepository) Get(ctx context.Context, name string) (EmbeddingModel, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+embeddingModelColumns+` FROM gogi_embedding_models WHERE name = $1`, name)
	if err != nil {
		return EmbeddingModel{}, err
	}
	model, err := pgx.CollectExactlyOneRow(rows, scanEmbeddingModel)
	if errors.Is(err, pgx.ErrNoRows) {
		return EmbeddingModel{}, utils.ErrEmbeddingModelNotFound
	}
	return model, err
}

// List returns the models whose names start with prefix, e.g. an application's, "qubit-notes."
func (r *EmbeddingModelsRepository) List(ctx context.Context, prefix string) ([]EmbeddingModel, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+embeddingModelColumns+` FROM gogi_embedding_models
		WHERE starts_with(name, $1) ORDER BY name`, prefix)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanEmbeddingModel)
}

func scanEmbeddingModel(row pgx.CollectableRow) (EmbeddingModel, error) {
	var model EmbeddingModel
	err := row.Scan(&model.Name, &model.URL, &model.ServedName, &model.CredentialRef, &model.Dimensions,
		&model.MaxInputTokens, &model.CreatedAt, &model.UpdatedAt)
	return model, err
}
