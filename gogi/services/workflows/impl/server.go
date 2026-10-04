package impl

import (
	"context"
	"errors"
	gogiv1 "gogi/gogi/gogi/v1"
	"gogi/gogi/storage/postgres"
	"gogi/gogi/utils"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func derefOr(s *string, fallback string) string {
	if s == nil {
		return fallback
	}
	return *s
}

// WorkflowServer implements the WorkflowServer service.
// The server provides the following endpoints:
//
//	// Registry
//	RegisterWorkflow(RegisterWorkflowRequest) returns (RegisterWorkflowResponse);
//	GetWorkflow(GetWorkflowRequest) returns (GetWorkflowResponse);
//	ListWorkflows(ListWorkflowsRequest) returns (ListWorkflowsResponse);
//	UpdateWorkflow(UpdateWorkflowRequest) returns (UpdateWorkflowResponse);
//	DeleteWorkflow(DeleteWorkflowRequest) returns (DeleteWorkflowResponse);
//
//	// Deployment
//	DeployWorkflow(DeployWorkflowRequest) returns (DeployWorkflowResponse);
//	GetDeploymentStatus(GetDeploymentStatusRequest) returns (GetDeploymentStatusResponse);
//	RollbackWorkflow(RollbackWorkflowRequest) returns (RollbackWorkflowResponse);
//
//	// Routing — Workflow Service pushes routes; gateway re-hydrates on startup.
//	RegisterRoute(RegisterRouteRequest) returns (RegisterRouteResponse);
//	ListRoutes(ListRoutesRequest) returns (ListRoutesResponse);
//
//	// Async jobs
//	CreateJob(CreateJobRequest) returns (CreateJobResponse);
//	GetJobStatus(GetJobStatusRequest) returns (GetJobStatusResponse);
//	UpdateJobProgress(UpdateJobProgressRequest) returns (UpdateJobProgressResponse);
//	SaveJobCheckpoint(SaveJobCheckpointRequest) returns (SaveJobCheckpointResponse);
//	CompleteJob(CompleteJobRequest) returns (CompleteJobResponse);
//	FailJob(FailJobRequest) returns (FailJobResponse);
//	CancelJob(CancelJobRequest) returns (CancelJobResponse);
type WorkflowServer struct {
	gogiv1.UnimplementedWorkflowServerServer
	gogiWorkflowsRepo postgres.GogiWorkflowsRepository
}

func NewWorkflowServer(dbClient *pgxpool.Pool) *WorkflowServer {
	return &WorkflowServer{
		gogiWorkflowsRepo: *postgres.NewGogiWorkflowsRepository(dbClient),
	}
}

// Registry

func (s *WorkflowServer) RegisterWorkflow(ctx context.Context, req *gogiv1.RegisterWorkflowRequest) (*gogiv1.RegisterWorkflowResponse, error) {

	spec := req.GetSpec()

	// Registering is idempotent by name: re-registering an already-known workflow (e.g. a
	// client that registers it on every startup) updates it in place instead of erroring on
	// the name's unique constraint.
	existing, err := s.gogiWorkflowsRepo.GetWorkflowByName(ctx, spec.GetName())
	if err != nil && !errors.Is(err, utils.ErrJobNotFound) {
		return nil, status.Errorf(codes.Internal, "failed to look up workflow: %v", err)
	}

	var w *postgres.GogiWorkflow
	if existing != nil {
		existing.ApiPath = spec.GetApiPath()
		existing.ContainerImage = spec.GetContainerImage()
		existing.ResponseMode = spec.GetResponseMode()
		w, err = s.gogiWorkflowsRepo.UpdateWorkflow(ctx, existing)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to update workflow: %v", err)
		}
	} else {
		w, err = s.gogiWorkflowsRepo.RegisterWorkflow(ctx, &postgres.GogiWorkflow{
			Name:           spec.GetName(),
			ApiPath:        spec.GetApiPath(),
			ContainerImage: spec.GetContainerImage(),
			ResponseMode:   spec.GetResponseMode(),
			Version:        spec.GetVersion(),
		})
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to register workflow: %v", err)
		}
	}

	return &gogiv1.RegisterWorkflowResponse{
		WorkflowId: w.ID,
		Version:    w.Version,
	}, nil
}

func (s *WorkflowServer) GetWorkflow(ctx context.Context, req *gogiv1.GetWorkflowRequest) (*gogiv1.GetWorkflowResponse, error) {

	return &gogiv1.GetWorkflowResponse{
		Spec: &gogiv1.WorkflowSpec{
			Name:           req.GetName(),
			ApiPath:        "/workflows/" + req.GetName(),
			ContainerImage: "gogi/workflows/" + req.GetName() + ":latest",
			ResponseMode:   "sync",
			Version:        1,
		},
	}, nil
}

func (s *WorkflowServer) ListWorkflows(ctx context.Context, req *gogiv1.ListWorkflowsRequest) (*gogiv1.ListWorkflowsResponse, error) {

	return &gogiv1.ListWorkflowsResponse{
		Specs: []*gogiv1.WorkflowSpec{},
	}, nil
}

func (s *WorkflowServer) UpdateWorkflow(ctx context.Context, req *gogiv1.UpdateWorkflowRequest) (*gogiv1.UpdateWorkflowResponse, error) {

	return &gogiv1.UpdateWorkflowResponse{
		Version: req.GetSpec().GetVersion() + 1,
	}, nil
}

func (s *WorkflowServer) DeleteWorkflow(ctx context.Context, req *gogiv1.DeleteWorkflowRequest) (*gogiv1.DeleteWorkflowResponse, error) {

	return &gogiv1.DeleteWorkflowResponse{
		Success: true,
	}, nil
}

// Deployment

func (s *WorkflowServer) DeployWorkflow(ctx context.Context, req *gogiv1.DeployWorkflowRequest) (*gogiv1.DeployWorkflowResponse, error) {

	return &gogiv1.DeployWorkflowResponse{
		Deployment: &gogiv1.WorkflowDeployment{
			WorkflowId:       req.GetWorkflowId(),
			DeploymentId:     utils.NewUUIDString(),
			Version:          req.GetVersion(),
			Status:           "deploying",
			CurrentReplicas:  0,
			DesiredReplicas:  1,
			HealthyEndpoints: []string{},
		},
	}, nil
}

func (s *WorkflowServer) GetDeploymentStatus(ctx context.Context, req *gogiv1.GetDeploymentStatusRequest) (*gogiv1.GetDeploymentStatusResponse, error) {

	return &gogiv1.GetDeploymentStatusResponse{
		Deployment: &gogiv1.WorkflowDeployment{
			WorkflowId:       req.GetWorkflowId(),
			Status:           "healthy",
			CurrentReplicas:  1,
			DesiredReplicas:  1,
			HealthyEndpoints: []string{},
		},
	}, nil
}

func (s *WorkflowServer) RollbackWorkflow(ctx context.Context, req *gogiv1.RollbackWorkflowRequest) (*gogiv1.RollbackWorkflowResponse, error) {

	return &gogiv1.RollbackWorkflowResponse{
		Deployment: &gogiv1.WorkflowDeployment{
			WorkflowId:      req.GetWorkflowId(),
			Version:         req.GetTargetVersion(),
			Status:          "deploying",
			DesiredReplicas: 1,
		},
	}, nil
}

// Routing

func (s *WorkflowServer) RegisterRoute(ctx context.Context, req *gogiv1.RegisterRouteRequest) (*gogiv1.RegisterRouteResponse, error) {

	return &gogiv1.RegisterRouteResponse{
		Success: true,
	}, nil
}

func (s *WorkflowServer) ListRoutes(ctx context.Context, req *gogiv1.ListRoutesRequest) (*gogiv1.ListRoutesResponse, error) {

	return &gogiv1.ListRoutesResponse{
		Routes: []*gogiv1.Route{},
	}, nil
}

// Async jobs

func (s *WorkflowServer) CreateJob(ctx context.Context, req *gogiv1.CreateJobRequest) (*gogiv1.CreateJobResponse, error) {

	job, err := s.gogiWorkflowsRepo.CreateJob(ctx, nilIfEmpty(req.GetWorkflowId()))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create job: %v", err)
	}

	return &gogiv1.CreateJobResponse{
		JobId: job.ID,
	}, nil
}

func (s *WorkflowServer) GetJobStatus(ctx context.Context, req *gogiv1.GetJobStatusRequest) (*gogiv1.GetJobStatusResponse, error) {

	job, err := s.gogiWorkflowsRepo.GetJobByID(ctx, req.GetJobId())
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "job not found: %v", err)
	}

	return &gogiv1.GetJobStatusResponse{
		Job: &gogiv1.WorkflowJob{
			JobId:          job.ID,
			WorkflowId:     derefOr(job.WorkflowID, ""),
			Status:         strings.ToUpper(job.Status),
			ResultJson:     derefOr(job.ResultJson, ""),
			Error:          derefOr(job.ErrorMessage, ""),
			CheckpointJson: derefOr(job.CheckpointJson, ""),
			CreatedAt:      job.CreatedAt.UnixMilli(),
			UpdatedAt:      job.UpdatedAt.UnixMilli(),
		},
	}, nil
}

func (s *WorkflowServer) UpdateJobProgress(ctx context.Context, req *gogiv1.UpdateJobProgressRequest) (*gogiv1.UpdateJobProgressResponse, error) {
	// progress_message is a free-form human-readable string; gogi_workflow_jobs only
	// tracks a numeric percentage, so there is nowhere to persist it yet.
	return &gogiv1.UpdateJobProgressResponse{
		Success: true,
	}, nil
}

func (s *WorkflowServer) SaveJobCheckpoint(ctx context.Context, req *gogiv1.SaveJobCheckpointRequest) (*gogiv1.SaveJobCheckpointResponse, error) {

	if err := s.gogiWorkflowsRepo.SaveJobCheckpoint(ctx, req.GetJobId(), req.GetCheckpointJson()); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to save checkpoint: %v", err)
	}

	return &gogiv1.SaveJobCheckpointResponse{
		Success: true,
	}, nil
}

func (s *WorkflowServer) CompleteJob(ctx context.Context, req *gogiv1.CompleteJobRequest) (*gogiv1.CompleteJobResponse, error) {

	if err := s.gogiWorkflowsRepo.CompleteJob(ctx, req.GetJobId(), req.GetResultJson()); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to complete job: %v", err)
	}

	return &gogiv1.CompleteJobResponse{
		Success: true,
	}, nil
}

func (s *WorkflowServer) FailJob(ctx context.Context, req *gogiv1.FailJobRequest) (*gogiv1.FailJobResponse, error) {

	if err := s.gogiWorkflowsRepo.FailJob(ctx, req.GetJobId(), req.GetError()); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to fail job: %v", err)
	}

	return &gogiv1.FailJobResponse{
		Success: true,
	}, nil
}

func (s *WorkflowServer) CancelJob(ctx context.Context, req *gogiv1.CancelJobRequest) (*gogiv1.CancelJobResponse, error) {

	if err := s.gogiWorkflowsRepo.CancelJob(ctx, req.GetJobId()); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to cancel job: %v", err)
	}

	return &gogiv1.CancelJobResponse{
		Success: true,
	}, nil
}
