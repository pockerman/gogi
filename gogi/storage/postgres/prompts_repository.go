package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"gogi/gogi/utils"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const GOGI_PROMPTS_TABLE_NAME string = "gogi_prompts"

type GogiPromptsRepository struct {
	pool *pgxpool.Pool
}

func NewGogiPromptsRepository(
	pool *pgxpool.Pool,
) *GogiPromptsRepository {

	return &GogiPromptsRepository{
		pool: pool,
	}
}

func (r *GogiPromptsRepository) CreatePrompt(ctx context.Context, prompt *GogiPrompt) (*GogiPrompt, error) {
	prompt.ID = utils.NewUUIDString()
	now := time.Now()
	prompt.CreatedAt = now
	prompt.UpdatedAt = now

	metricsJSON, err := json.Marshal(prompt.Metrics)
	if err != nil {
		return nil, err
	}

	_, err = r.pool.Exec(ctx,
		`INSERT INTO gogi_prompts (
			id, name, version, owner, gogi_index, minio_path, author, model,
			temperature, max_tokens, stop_sequences, frequency_penalty, presence_penalty,
			test_set_id, test_set_path, metrics, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)`,
		prompt.ID, prompt.Name, prompt.Version, prompt.Owner,
		prompt.GogiIndex, prompt.MinioPath, prompt.Author, prompt.Model,
		prompt.Temperature, prompt.MaxTokens, prompt.StopSequences,
		prompt.FrequencyPenalty, prompt.PresencePenalty,
		prompt.TestSetID, prompt.TestSetPath,
		json.RawMessage(metricsJSON), prompt.CreatedAt, prompt.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return prompt, nil
}

func (r *GogiPromptsRepository) GetPromptByID(ctx context.Context, id string) (*GogiPrompt, error) {
	var p GogiPrompt
	var gogiIndex, author, model, testSetID, testSetPath *string
	var temperature, frequencyPenalty, presencePenalty *float64
	var maxTokens *int32
	var metricsRaw json.RawMessage

	err := r.pool.QueryRow(ctx,
		`SELECT id, name, version, owner, gogi_index, minio_path, author, model,
		        temperature, max_tokens, stop_sequences, frequency_penalty, presence_penalty,
		        test_set_id, test_set_path, metrics, created_at, updated_at
		 FROM gogi_prompts
		 WHERE id = $1`,
		id,
	).Scan(
		&p.ID, &p.Name, &p.Version, &p.Owner,
		&gogiIndex, &p.MinioPath, &author, &model,
		&temperature, &maxTokens, &p.StopSequences,
		&frequencyPenalty, &presencePenalty,
		&testSetID, &testSetPath,
		&metricsRaw, &p.CreatedAt, &p.UpdatedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, utils.ErrJobNotFound
	}
	if err != nil {
		return nil, err
	}

	if gogiIndex != nil {
		p.GogiIndex = *gogiIndex
	}
	if author != nil {
		p.Author = *author
	}
	if model != nil {
		p.Model = *model
	}
	if temperature != nil {
		p.Temperature = *temperature
	}
	if maxTokens != nil {
		p.MaxTokens = *maxTokens
	}
	if frequencyPenalty != nil {
		p.FrequencyPenalty = *frequencyPenalty
	}
	if presencePenalty != nil {
		p.PresencePenalty = *presencePenalty
	}
	if testSetID != nil {
		p.TestSetID = *testSetID
	}
	if testSetPath != nil {
		p.TestSetPath = *testSetPath
	}
	if metricsRaw != nil {
		if err := json.Unmarshal(metricsRaw, &p.Metrics); err != nil {
			return nil, err
		}
	}

	return &p, nil
}

func (r *GogiPromptsRepository) DeletePromptByID(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM gogi_prompts WHERE id = $1`,
		id,
	)
	return err
}
