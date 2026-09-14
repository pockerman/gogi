package postgres

import "github.com/jackc/pgx/v5/pgxpool"

const GOGI_LLM_SESSION_TABLE_NAME string = "gogi_llm_session"

type GogiLLMSessionRepository struct {
	pool *pgxpool.Pool
}

func NewGogiLLMSessionRepository(
	pool *pgxpool.Pool,
) *GogiLLMSessionRepository {

	return &GogiLLMSessionRepository{
		pool: pool,
	}
}
