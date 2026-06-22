package postgres

import "github.com/jackc/pgx/v5/pgxpool"

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
