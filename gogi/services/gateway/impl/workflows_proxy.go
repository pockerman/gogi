package impl

import (
	"context"
	gogiv1 "gogi/gogi/gogi/v1"
)

type WorkflowsProxy struct {
	gogiv1.UnimplementedWorkflowServerServer
	proxy *GenericGRPCProxy
}

func (p *WorkflowsProxy) RegisterWorkflow(ctx context.Context, req *gogiv1.RegisterWorkflowRequest) (*gogiv1.RegisterWorkflowResponse, error) {
	return p.proxy.ForwardRegisterWorkflow(ctx, req)
}

func (p *WorkflowsProxy) GetWorkflow(ctx context.Context, req *gogiv1.GetWorkflowRequest) (*gogiv1.GetWorkflowResponse, error) {
	return p.proxy.ForwardGetWorkflow(ctx, req)
}

func (p *WorkflowsProxy) ListWorkflows(ctx context.Context, req *gogiv1.ListWorkflowsRequest) (*gogiv1.ListWorkflowsResponse, error) {
	return p.proxy.ForwardListWorkflows(ctx, req)
}

func (p *WorkflowsProxy) UpdateWorkflow(ctx context.Context, req *gogiv1.UpdateWorkflowRequest) (*gogiv1.UpdateWorkflowResponse, error) {
	return p.proxy.ForwardUpdateWorkflow(ctx, req)
}

func (p *WorkflowsProxy) DeleteWorkflow(ctx context.Context, req *gogiv1.DeleteWorkflowRequest) (*gogiv1.DeleteWorkflowResponse, error) {
	return p.proxy.ForwardDeleteWorkflow(ctx, req)
}

func (p *WorkflowsProxy) DeployWorkflow(ctx context.Context, req *gogiv1.DeployWorkflowRequest) (*gogiv1.DeployWorkflowResponse, error) {
	return p.proxy.ForwardDeployWorkflow(ctx, req)
}

func (p *WorkflowsProxy) GetDeploymentStatus(ctx context.Context, req *gogiv1.GetDeploymentStatusRequest) (*gogiv1.GetDeploymentStatusResponse, error) {
	return p.proxy.ForwardGetDeploymentStatus(ctx, req)
}

func (p *WorkflowsProxy) RollbackWorkflow(ctx context.Context, req *gogiv1.RollbackWorkflowRequest) (*gogiv1.RollbackWorkflowResponse, error) {
	return p.proxy.ForwardRollbackWorkflow(ctx, req)
}

func (p *WorkflowsProxy) RegisterRoute(ctx context.Context, req *gogiv1.RegisterRouteRequest) (*gogiv1.RegisterRouteResponse, error) {
	return p.proxy.ForwardRegisterRoute(ctx, req)
}

func (p *WorkflowsProxy) ListRoutes(ctx context.Context, req *gogiv1.ListRoutesRequest) (*gogiv1.ListRoutesResponse, error) {
	return p.proxy.ForwardListRoutes(ctx, req)
}

func (p *WorkflowsProxy) CreateJob(ctx context.Context, req *gogiv1.CreateJobRequest) (*gogiv1.CreateJobResponse, error) {
	return p.proxy.ForwardCreateJob(ctx, req)
}

func (p *WorkflowsProxy) GetJobStatus(ctx context.Context, req *gogiv1.GetJobStatusRequest) (*gogiv1.GetJobStatusResponse, error) {
	return p.proxy.ForwardGetJobStatus(ctx, req)
}

func (p *WorkflowsProxy) UpdateJobProgress(ctx context.Context, req *gogiv1.UpdateJobProgressRequest) (*gogiv1.UpdateJobProgressResponse, error) {
	return p.proxy.ForwardUpdateJobProgress(ctx, req)
}

func (p *WorkflowsProxy) SaveJobCheckpoint(ctx context.Context, req *gogiv1.SaveJobCheckpointRequest) (*gogiv1.SaveJobCheckpointResponse, error) {
	return p.proxy.ForwardSaveJobCheckpoint(ctx, req)
}

func (p *WorkflowsProxy) CompleteJob(ctx context.Context, req *gogiv1.CompleteJobRequest) (*gogiv1.CompleteJobResponse, error) {
	return p.proxy.ForwardCompleteJob(ctx, req)
}

func (p *WorkflowsProxy) FailJob(ctx context.Context, req *gogiv1.FailJobRequest) (*gogiv1.FailJobResponse, error) {
	return p.proxy.ForwardFailJob(ctx, req)
}

func (p *WorkflowsProxy) CancelJob(ctx context.Context, req *gogiv1.CancelJobRequest) (*gogiv1.CancelJobResponse, error) {
	return p.proxy.ForwardCancelJob(ctx, req)
}
