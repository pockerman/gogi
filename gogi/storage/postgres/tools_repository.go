package postgres

import (
	"context"
	"errors"
	"gogi/gogi/utils"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const GOGI_TOOLS_TABLE_NAME string = "gogi_tools"
const GOGI_TOOL_TASKS_TABLE_NAME string = "gogi_tool_tasks"

const toolColumns = `id, name, version, owner, description, parameters_json, returns_json, behavior,
	rate_limits, cost, execution_limits, required_permissions, capabilities, tags, endpoint,
	credential_ref, mcp_server_url, mcp_tool_name, created_at, updated_at`

const toolTaskColumns = `id, tool_name, tool_version, session_id, status, input_json, result_json, error,
	created_at, updated_at`

// pgUniqueViolation is the PostgreSQL error code of a unique constraint violation
const pgUniqueViolation = "23505"

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

// InsertTool registers a version of a tool. Versions are immutable, so registering a
// version that is already registered fails with ErrToolVersionExists
func (r *GogiToolsRepository) InsertTool(ctx context.Context, tool *GogiTool) (*GogiTool, error) {
	tool.ID = utils.NewUUIDString()
	now := time.Now()
	tool.CreatedAt = now
	tool.UpdatedAt = now

	_, err := r.pool.Exec(ctx,
		`INSERT INTO gogi_tools (`+toolColumns+`)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)`,
		tool.ID, tool.Name, tool.Version, tool.Owner, tool.Description, tool.ParametersJSON,
		tool.ReturnsJSON, tool.Behavior, tool.RateLimits, tool.Cost, tool.ExecutionLimits,
		nonNil(tool.RequiredPermissions), nonNil(tool.Capabilities), nonNil(tool.Tags), tool.Endpoint,
		tool.CredentialRef, tool.MCPServerURL, tool.MCPToolName, tool.CreatedAt, tool.UpdatedAt,
	)

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
		return nil, utils.ErrToolVersionExists
	}
	if err != nil {
		return nil, err
	}
	return tool, nil
}

// ListTools returns every version of the tools whose name starts with namePrefix,
// e.g. "healthcare.scheduling." for the tools in that namespace, or of all tools if
// namePrefix is empty
func (r *GogiToolsRepository) ListTools(ctx context.Context, namePrefix string) ([]*GogiTool, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+toolColumns+` FROM gogi_tools WHERE starts_with(name, $1) ORDER BY name, created_at`,
		namePrefix)
	if err != nil {
		return nil, err
	}
	return scanTools(rows)
}

// ListToolVersions returns every version of the tool, or ErrToolNotFound if there are none
func (r *GogiToolsRepository) ListToolVersions(ctx context.Context, name string) ([]*GogiTool, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+toolColumns+` FROM gogi_tools WHERE name = $1 ORDER BY created_at`, name)
	if err != nil {
		return nil, err
	}
	tools, err := scanTools(rows)
	if err != nil {
		return nil, err
	}
	if len(tools) == 0 {
		return nil, utils.ErrToolNotFound
	}
	return tools, nil
}

func scanTools(rows pgx.Rows) ([]*GogiTool, error) {
	defer rows.Close()

	tools := make([]*GogiTool, 0)
	for rows.Next() {
		var t GogiTool
		if err := rows.Scan(
			&t.ID, &t.Name, &t.Version, &t.Owner, &t.Description, &t.ParametersJSON, &t.ReturnsJSON,
			&t.Behavior, &t.RateLimits, &t.Cost, &t.ExecutionLimits, &t.RequiredPermissions,
			&t.Capabilities, &t.Tags, &t.Endpoint, &t.CredentialRef, &t.MCPServerURL, &t.MCPToolName,
			&t.CreatedAt, &t.UpdatedAt,
		); err != nil {
			return nil, err
		}
		tools = append(tools, &t)
	}
	return tools, rows.Err()
}

// CreateToolTask records an asynchronous tool call
func (r *GogiToolsRepository) CreateToolTask(ctx context.Context, task *GogiToolTask) (*GogiToolTask, error) {
	task.ID = utils.NewUUIDString()
	now := time.Now()
	task.CreatedAt = now
	task.UpdatedAt = now

	_, err := r.pool.Exec(ctx,
		`INSERT INTO gogi_tool_tasks (`+toolTaskColumns+`) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		task.ID, task.ToolName, task.ToolVersion, task.SessionID, task.Status, task.InputJson,
		task.ResultJson, task.Error, task.CreatedAt, task.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return task, nil
}

func (r *GogiToolsRepository) GetToolTask(ctx context.Context, id string) (*GogiToolTask, error) {
	var task GogiToolTask

	err := r.pool.QueryRow(ctx,
		`SELECT `+toolTaskColumns+` FROM gogi_tool_tasks WHERE id = $1`, id,
	).Scan(
		&task.ID, &task.ToolName, &task.ToolVersion, &task.SessionID, &task.Status,
		&task.InputJson, &task.ResultJson, &task.Error, &task.CreatedAt, &task.UpdatedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, utils.ErrToolTaskNotFound
	}
	if err != nil {
		return nil, err
	}
	return &task, nil
}

// UpdateToolTask records the status of an asynchronous tool call, and its result or error
func (r *GogiToolsRepository) UpdateToolTask(ctx context.Context, id, status string, resultJson *string, errorMessage string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE gogi_tool_tasks SET status = $2, result_json = $3, error = $4, updated_at = NOW() WHERE id = $1`,
		id, status, resultJson, errorMessage,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return utils.ErrToolTaskNotFound
	}
	return nil
}

// nonNil returns an empty slice for nil, so NOT NULL array columns get '{}'
func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
