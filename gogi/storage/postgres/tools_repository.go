package postgres

import (
	"context"
	"errors"
	"fmt"
	"gogi/gogi/utils"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

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

func (r *GogiToolsRepository) RegisterTool(ctx context.Context, tool *GogiTool) (*GogiTool, error) {
	tool.ID = utils.NewUUIDString()
	now := time.Now()
	tool.CreatedAt = now
	tool.UpdatedAt = now

	_, err := r.pool.Exec(ctx,
		`INSERT INTO gogi_tools (
			id, name, version, owner, description, is_read_only, is_idempotent,
			capabilities, tags, endpoint, schema_json, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		tool.ID, tool.Name, tool.Version, tool.Owner,
		tool.Description, tool.IsReadOnly, tool.IsIdempotent,
		tool.Capabilities, tool.Tags, tool.Endpoint, tool.SchemaJson,
		tool.CreatedAt, tool.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return tool, nil
}

func (r *GogiToolsRepository) ListTools(ctx context.Context, tags, capabilities []string) ([]*GogiTool, error) {
	baseQuery := `SELECT id, name, version, owner, description, is_read_only, is_idempotent,
	                     capabilities, tags, endpoint, schema_json, created_at, updated_at
	              FROM gogi_tools`

	args := make([]any, 0)
	var conditions []string

	if len(tags) > 0 {
		args = append(args, tags)
		conditions = append(conditions, fmt.Sprintf("tags && $%d", len(args)))
	}
	if len(capabilities) > 0 {
		args = append(args, capabilities)
		conditions = append(conditions, fmt.Sprintf("capabilities && $%d", len(args)))
	}

	query := baseQuery
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY created_at DESC"

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tools []*GogiTool
	for rows.Next() {
		var t GogiTool
		var description, endpoint, schemaJson *string
		if err := rows.Scan(
			&t.ID, &t.Name, &t.Version, &t.Owner,
			&description, &t.IsReadOnly, &t.IsIdempotent,
			&t.Capabilities, &t.Tags, &endpoint, &schemaJson,
			&t.CreatedAt, &t.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if description != nil {
			t.Description = *description
		}
		if endpoint != nil {
			t.Endpoint = *endpoint
		}
		if schemaJson != nil {
			t.SchemaJson = *schemaJson
		}
		tools = append(tools, &t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return tools, nil
}

func (r *GogiToolsRepository) GetToolByName(ctx context.Context, name, owner string) (*GogiTool, error) {
	var t GogiTool
	var description, endpoint, schemaJson *string

	err := r.pool.QueryRow(ctx,
		`SELECT id, name, version, owner, description, is_read_only, is_idempotent,
		        capabilities, tags, endpoint, schema_json, created_at, updated_at
		 FROM gogi_tools
		 WHERE name = $1 AND owner = $2`,
		name, owner,
	).Scan(
		&t.ID, &t.Name, &t.Version, &t.Owner,
		&description, &t.IsReadOnly, &t.IsIdempotent,
		&t.Capabilities, &t.Tags, &endpoint, &schemaJson,
		&t.CreatedAt, &t.UpdatedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, utils.ErrJobNotFound
	}
	if err != nil {
		return nil, err
	}
	if description != nil {
		t.Description = *description
	}
	if endpoint != nil {
		t.Endpoint = *endpoint
	}
	if schemaJson != nil {
		t.SchemaJson = *schemaJson
	}
	return &t, nil
}

func (r *GogiToolsRepository) CreateTask(ctx context.Context, toolName string) (*GogiToolTask, error) {
	now := time.Now()
	task := &GogiToolTask{
		ID:        utils.NewUUIDString(),
		ToolName:  toolName,
		Status:    "pending",
		CreatedAt: now,
		UpdatedAt: now,
	}

	_, err := r.pool.Exec(ctx,
		`INSERT INTO gogi_tool_tasks (id, tool_name, status, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		task.ID, task.ToolName, task.Status, task.CreatedAt, task.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return task, nil
}

func (r *GogiToolsRepository) GetTaskByID(ctx context.Context, id string) (*GogiToolTask, error) {
	var task GogiToolTask

	err := r.pool.QueryRow(ctx,
		`SELECT id, tool_name, status, input_json, result_json, created_at, updated_at
		 FROM gogi_tool_tasks
		 WHERE id = $1`,
		id,
	).Scan(
		&task.ID, &task.ToolName, &task.Status,
		&task.InputJson, &task.ResultJson,
		&task.CreatedAt, &task.UpdatedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, utils.ErrJobNotFound
	}
	if err != nil {
		return nil, err
	}
	return &task, nil
}

func (r *GogiToolsRepository) UpdateTaskStatus(ctx context.Context, id, status string, resultJson *string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE gogi_tool_tasks
		 SET status = $1, result_json = $2, updated_at = NOW()
		 WHERE id = $3`,
		status, resultJson, id,
	)
	return err
}
