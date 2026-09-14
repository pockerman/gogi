package postgres

import "github.com/jackc/pgx/v5/pgxpool"

const GOGI_TOOLS_TABLE_NAME string = "gogi_tools"

type GogiToolsRepository struct {
	pool *pgxpool.Pool
}

func NewGogiToolsRepository(
	pool *pgxpool.Pool,
) *GogiToolsRepository {

	return &GogiToolsRepository{
		pool: pool,
	}
}
