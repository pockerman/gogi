package postgres

import (
	"context"
	"errors"
	"gogi/gogi/utils"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

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

func (r *GogiWorkflowsRepository) RegisterWorkflow(ctx context.Context, w *GogiWorkflow) (*GogiWorkflow, error) {
	w.ID = utils.NewUUIDString()
	now := time.Now()
	w.CreatedAt = now
	w.UpdatedAt = now

	_, err := r.pool.Exec(ctx,
		`INSERT INTO gogi_workflows (id, name, api_path, container_image, response_mode, version, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		w.ID, w.Name, w.ApiPath, w.ContainerImage, w.ResponseMode, w.Version, w.CreatedAt, w.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return w, nil
}

func (r *GogiWorkflowsRepository) GetWorkflowByID(ctx context.Context, id string) (*GogiWorkflow, error) {
	var w GogiWorkflow

	err := r.pool.QueryRow(ctx,
		`SELECT id, name, api_path, container_image, response_mode, version, created_at, updated_at
		 FROM gogi_workflows
		 WHERE id = $1`,
		id,
	).Scan(&w.ID, &w.Name, &w.ApiPath, &w.ContainerImage, &w.ResponseMode, &w.Version, &w.CreatedAt, &w.UpdatedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, utils.ErrJobNotFound
	}
	if err != nil {
		return nil, err
	}
	return &w, nil
}

func (r *GogiWorkflowsRepository) GetWorkflowByName(ctx context.Context, name string) (*GogiWorkflow, error) {
	var w GogiWorkflow

	err := r.pool.QueryRow(ctx,
		`SELECT id, name, api_path, container_image, response_mode, version, created_at, updated_at
		 FROM gogi_workflows
		 WHERE name = $1`,
		name,
	).Scan(&w.ID, &w.Name, &w.ApiPath, &w.ContainerImage, &w.ResponseMode, &w.Version, &w.CreatedAt, &w.UpdatedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, utils.ErrJobNotFound
	}
	if err != nil {
		return nil, err
	}
	return &w, nil
}

func (r *GogiWorkflowsRepository) ListWorkflows(ctx context.Context) ([]*GogiWorkflow, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, name, api_path, container_image, response_mode, version, created_at, updated_at
		 FROM gogi_workflows
		 ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var workflows []*GogiWorkflow
	for rows.Next() {
		var w GogiWorkflow
		if err := rows.Scan(&w.ID, &w.Name, &w.ApiPath, &w.ContainerImage, &w.ResponseMode, &w.Version, &w.CreatedAt, &w.UpdatedAt); err != nil {
			return nil, err
		}
		workflows = append(workflows, &w)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return workflows, nil
}

func (r *GogiWorkflowsRepository) UpdateWorkflow(ctx context.Context, w *GogiWorkflow) (*GogiWorkflow, error) {
	var updated GogiWorkflow

	err := r.pool.QueryRow(ctx,
		`UPDATE gogi_workflows
		 SET name=$1, api_path=$2, container_image=$3, response_mode=$4,
		     version=version+1, updated_at=NOW()
		 WHERE id=$5
		 RETURNING id, name, api_path, container_image, response_mode, version, created_at, updated_at`,
		w.Name, w.ApiPath, w.ContainerImage, w.ResponseMode, w.ID,
	).Scan(
		&updated.ID, &updated.Name, &updated.ApiPath, &updated.ContainerImage,
		&updated.ResponseMode, &updated.Version, &updated.CreatedAt, &updated.UpdatedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, utils.ErrJobNotFound
	}
	if err != nil {
		return nil, err
	}
	return &updated, nil
}

func (r *GogiWorkflowsRepository) DeleteWorkflow(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM gogi_workflows WHERE id = $1`,
		id,
	)
	return err
}

func (r *GogiWorkflowsRepository) CreateDeployment(ctx context.Context, workflowID string, version int32) (*GogiWorkflowDeployment, error) {
	now := time.Now()
	d := &GogiWorkflowDeployment{
		ID:              utils.NewUUIDString(),
		WorkflowID:      workflowID,
		Version:         version,
		Status:          "deploying",
		DesiredReplicas: 1,
		CurrentReplicas: 0,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	_, err := r.pool.Exec(ctx,
		`INSERT INTO gogi_workflow_deployments (id, workflow_id, version, status, desired_replicas, current_replicas, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		d.ID, d.WorkflowID, d.Version, d.Status, d.DesiredReplicas, d.CurrentReplicas, d.CreatedAt, d.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return d, nil
}

func (r *GogiWorkflowsRepository) GetLatestDeployment(ctx context.Context, workflowID string) (*GogiWorkflowDeployment, error) {
	var d GogiWorkflowDeployment

	err := r.pool.QueryRow(ctx,
		`SELECT id, workflow_id, version, status, desired_replicas, current_replicas, created_at, updated_at
		 FROM gogi_workflow_deployments
		 WHERE workflow_id = $1
		 ORDER BY created_at DESC
		 LIMIT 1`,
		workflowID,
	).Scan(&d.ID, &d.WorkflowID, &d.Version, &d.Status, &d.DesiredReplicas, &d.CurrentReplicas, &d.CreatedAt, &d.UpdatedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, utils.ErrJobNotFound
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (r *GogiWorkflowsRepository) UpdateDeployment(ctx context.Context, d *GogiWorkflowDeployment) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE gogi_workflow_deployments
		 SET status=$1, desired_replicas=$2, current_replicas=$3, updated_at=NOW()
		 WHERE id=$4`,
		d.Status, d.DesiredReplicas, d.CurrentReplicas, d.ID,
	)
	return err
}

func (r *GogiWorkflowsRepository) RegisterRoute(ctx context.Context, workflowID, path, method string) (*GogiRoute, error) {
	route := &GogiRoute{
		ID:         utils.NewUUIDString(),
		WorkflowID: workflowID,
		Path:       path,
		Method:     method,
		CreatedAt:  time.Now(),
	}

	_, err := r.pool.Exec(ctx,
		`INSERT INTO gogi_routes (id, workflow_id, path, method, created_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		route.ID, route.WorkflowID, route.Path, route.Method, route.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return route, nil
}

func (r *GogiWorkflowsRepository) ListRoutes(ctx context.Context, workflowID string) ([]*GogiRoute, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, workflow_id, path, method, created_at
		 FROM gogi_routes
		 WHERE workflow_id = $1`,
		workflowID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var routes []*GogiRoute
	for rows.Next() {
		var rt GogiRoute
		if err := rows.Scan(&rt.ID, &rt.WorkflowID, &rt.Path, &rt.Method, &rt.CreatedAt); err != nil {
			return nil, err
		}
		routes = append(routes, &rt)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return routes, nil
}

func (r *GogiWorkflowsRepository) CreateJob(ctx context.Context, workflowID *string) (*GogiWorkflowJob, error) {
	now := time.Now()
	job := &GogiWorkflowJob{
		ID:         utils.NewUUIDString(),
		WorkflowID: workflowID,
		Status:     "pending",
		Progress:   0,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	_, err := r.pool.Exec(ctx,
		`INSERT INTO gogi_workflow_jobs (id, workflow_id, status, progress, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		job.ID, job.WorkflowID, job.Status, job.Progress, job.CreatedAt, job.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return job, nil
}

func (r *GogiWorkflowsRepository) GetJobByID(ctx context.Context, id string) (*GogiWorkflowJob, error) {
	var job GogiWorkflowJob

	err := r.pool.QueryRow(ctx,
		`SELECT id, workflow_id, status, progress, checkpoint_json, result_json, error_message, created_at, updated_at
		 FROM gogi_workflow_jobs
		 WHERE id = $1`,
		id,
	).Scan(
		&job.ID, &job.WorkflowID, &job.Status, &job.Progress,
		&job.CheckpointJson, &job.ResultJson, &job.ErrorMessage,
		&job.CreatedAt, &job.UpdatedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, utils.ErrJobNotFound
	}
	if err != nil {
		return nil, err
	}
	return &job, nil
}

func (r *GogiWorkflowsRepository) UpdateJobProgress(ctx context.Context, id string, progress int32) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE gogi_workflow_jobs SET progress=$1, updated_at=NOW() WHERE id=$2`,
		progress, id,
	)
	return err
}

func (r *GogiWorkflowsRepository) SaveJobCheckpoint(ctx context.Context, id, checkpointJson string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE gogi_workflow_jobs SET checkpoint_json=$1, updated_at=NOW() WHERE id=$2`,
		checkpointJson, id,
	)
	return err
}

func (r *GogiWorkflowsRepository) CompleteJob(ctx context.Context, id, resultJson string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE gogi_workflow_jobs SET status='completed', result_json=$1, updated_at=NOW() WHERE id=$2`,
		resultJson, id,
	)
	return err
}

func (r *GogiWorkflowsRepository) FailJob(ctx context.Context, id, errorMessage string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE gogi_workflow_jobs SET status='failed', error_message=$1, updated_at=NOW() WHERE id=$2`,
		errorMessage, id,
	)
	return err
}

func (r *GogiWorkflowsRepository) CancelJob(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE gogi_workflow_jobs SET status='cancelled', updated_at=NOW() WHERE id=$1`,
		id,
	)
	return err
}
