package impl

import (
	"context"
	gogiv1 "gogi/gogi/gogi/v1"
	"gogi/gogi/storage/postgres"
	"gogi/gogi/utils"

	"github.com/jackc/pgx/v5/pgxpool"
)

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

	return &gogiv1.RegisterWorkflowResponse{
		WorkflowId: utils.NewUUIDString(),
		Version:    req.GetSpec().GetVersion(),
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

	return &gogiv1.CreateJobResponse{
		JobId: utils.NewUUIDString(),
	}, nil
}

func (s *WorkflowServer) GetJobStatus(ctx context.Context, req *gogiv1.GetJobStatusRequest) (*gogiv1.GetJobStatusResponse, error) {

	return &gogiv1.GetJobStatusResponse{
		Job: &gogiv1.WorkflowJob{
			JobId:  req.GetJobId(),
			Status: "pending",
		},
	}, nil
}

func (s *WorkflowServer) UpdateJobProgress(ctx context.Context, req *gogiv1.UpdateJobProgressRequest) (*gogiv1.UpdateJobProgressResponse, error) {

	return &gogiv1.UpdateJobProgressResponse{
		Success: true,
	}, nil
}

func (s *WorkflowServer) SaveJobCheckpoint(ctx context.Context, req *gogiv1.SaveJobCheckpointRequest) (*gogiv1.SaveJobCheckpointResponse, error) {

	return &gogiv1.SaveJobCheckpointResponse{
		Success: true,
	}, nil
}

func (s *WorkflowServer) CompleteJob(ctx context.Context, req *gogiv1.CompleteJobRequest) (*gogiv1.CompleteJobResponse, error) {

	return &gogiv1.CompleteJobResponse{
		Success: true,
	}, nil
}

func (s *WorkflowServer) FailJob(ctx context.Context, req *gogiv1.FailJobRequest) (*gogiv1.FailJobResponse, error) {

	return &gogiv1.FailJobResponse{
		Success: true,
	}, nil
}

func (s *WorkflowServer) CancelJob(ctx context.Context, req *gogiv1.CancelJobRequest) (*gogiv1.CancelJobResponse, error) {

	return &gogiv1.CancelJobResponse{
		Success: true,
	}, nil
}
