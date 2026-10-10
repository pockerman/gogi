package postgres

import (
	"context"
	"errors"
	"gogi/gogi/utils"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const GOGI_REGISTERED_LLMS_TABLE_NAME string = "gogi_registered_llms"

const registeredLLMColumns = `id, name, provider, context_window, supports_vision, supports_tools,
	supports_streaming, supports_json_mode, endpoint, health_check, adapter_type, credential_ref, status,
	last_checked_at, created_at, updated_at`

type GogiRegisteredLLMsRepository struct {
	pool *pgxpool.Pool
}

func NewGogiRegisteredLLMsRepository(
	pool *pgxpool.Pool,
) *GogiRegisteredLLMsRepository {

	return &GogiRegisteredLLMsRepository{
		pool: pool,
	}
}

// UpsertRegisteredLLM registers the model, or updates its registration if a model with
// the same name is already registered. Either way the model is in the given status
// until its health is next checked
func (r *GogiRegisteredLLMsRepository) UpsertRegisteredLLM(ctx context.Context,
	model *GogiRegisteredLLM) (*GogiRegisteredLLM, error) {

	now := time.Now()

	// on conflict the existing id and created_at are kept and returned
	err := r.pool.QueryRow(ctx,
		`INSERT INTO gogi_registered_llms (`+registeredLLMColumns+`)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,NULL,$14,$14)
		 ON CONFLICT (name) DO UPDATE SET
		     provider = EXCLUDED.provider,
		     context_window = EXCLUDED.context_window,
		     supports_vision = EXCLUDED.supports_vision,
		     supports_tools = EXCLUDED.supports_tools,
		     supports_streaming = EXCLUDED.supports_streaming,
		     supports_json_mode = EXCLUDED.supports_json_mode,
		     endpoint = EXCLUDED.endpoint,
		     health_check = EXCLUDED.health_check,
		     adapter_type = EXCLUDED.adapter_type,
		     credential_ref = EXCLUDED.credential_ref,
		     status = EXCLUDED.status,
		     last_checked_at = NULL,
		     updated_at = EXCLUDED.updated_at
		 RETURNING id, created_at, updated_at`,
		utils.NewUUIDString(), model.Name, model.Provider, model.ContextWindow,
		model.SupportsVision, model.SupportsTools, model.SupportsStreaming, model.SupportsJSONMode,
		model.Endpoint, model.HealthCheck, model.AdapterType, model.CredentialRef, model.Status, now,
	).Scan(&model.ID, &model.CreatedAt, &model.UpdatedAt)
	model.LastCheckedAt = nil

	if err != nil {
		return nil, err
	}
	return model, nil
}

// UpdateRegisteredLLMStatus records the result of a health check of the model
func (r *GogiRegisteredLLMsRepository) UpdateRegisteredLLMStatus(ctx context.Context,
	name, status string, checkedAt time.Time) error {

	tag, err := r.pool.Exec(ctx,
		`UPDATE gogi_registered_llms SET status = $2, last_checked_at = $3 WHERE name = $1`,
		name, status, checkedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return utils.ErrRegisteredLLMNotFound
	}
	return nil
}

func (r *GogiRegisteredLLMsRepository) GetRegisteredLLMByName(ctx context.Context,
	name string) (*GogiRegisteredLLM, error) {

	row := r.pool.QueryRow(ctx,
		`SELECT `+registeredLLMColumns+` FROM gogi_registered_llms WHERE name = $1`, name)

	model, err := scanRegisteredLLM(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, utils.ErrRegisteredLLMNotFound
	}
	if err != nil {
		return nil, err
	}
	return model, nil
}

func (r *GogiRegisteredLLMsRepository) ListRegisteredLLMs(ctx context.Context) ([]*GogiRegisteredLLM, error) {

	rows, err := r.pool.Query(ctx,
		`SELECT `+registeredLLMColumns+` FROM gogi_registered_llms ORDER BY provider, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	models := make([]*GogiRegisteredLLM, 0)
	for rows.Next() {
		model, err := scanRegisteredLLM(rows)
		if err != nil {
			return nil, err
		}
		models = append(models, model)
	}
	return models, rows.Err()
}

func scanRegisteredLLM(row pgx.Row) (*GogiRegisteredLLM, error) {
	var m GogiRegisteredLLM
	err := row.Scan(
		&m.ID, &m.Name, &m.Provider, &m.ContextWindow,
		&m.SupportsVision, &m.SupportsTools, &m.SupportsStreaming, &m.SupportsJSONMode,
		&m.Endpoint, &m.HealthCheck, &m.AdapterType, &m.CredentialRef, &m.Status, &m.LastCheckedAt,
		&m.CreatedAt, &m.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &m, nil
}
