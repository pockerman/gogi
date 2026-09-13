package postgres

import "github.com/jackc/pgx/v5/pgxpool"

const GOGI_GUARDRAILS_TABLE_NAME string = "gogi_guardrails"

type GogiGuardrailsRepository struct {
	pool *pgxpool.Pool
}

func NewGogiGuardrailsRepository(
	pool *pgxpool.Pool,
) *GogiGuardrailsRepository {

	return &GogiGuardrailsRepository{
		pool: pool,
	}
}
