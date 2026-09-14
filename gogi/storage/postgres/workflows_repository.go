package postgres

import "github.com/jackc/pgx/v5/pgxpool"

const GOGI_WORKFLOWS_TABLE_NAME string = "gogi_workflows"

type GogiWorkflowsRepository struct {
	pool *pgxpool.Pool
}

func NewGogiWorkflowsRepository(
	pool *pgxpool.Pool,
) *GogiWorkflowsRepository {

	return &GogiWorkflowsRepository{
		pool: pool,
	}
}
