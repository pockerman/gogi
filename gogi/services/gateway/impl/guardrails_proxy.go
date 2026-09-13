package impl

import (
	"context"
	gogiv1 "gogi/gogi/gogi/v1"
)

type GuardrailsServiceProxy struct {
	gogiv1.UnimplementedGuardrailsServiceServer
	proxy *GenericGRPCProxy
}

func (p *GuardrailsServiceProxy) ValidateInput(ctx context.Context, req *gogiv1.ValidateInputRequest) (*gogiv1.ValidateInputResponse, error) {
	return p.proxy.ForwardValidateInput(ctx, req)
}

func (p *GuardrailsServiceProxy) FilterOutput(ctx context.Context, req *gogiv1.FilterOutputRequest) (*gogiv1.FilterOutputResponse, error) {
	return p.proxy.ForwardFilterOutput(ctx, req)
}

func (p *GuardrailsServiceProxy) CheckPolicy(ctx context.Context, req *gogiv1.CheckPolicyRequest) (*gogiv1.CheckPolicyResponse, error) {
	return p.proxy.ForwardCheckPolicy(ctx, req)
}

func (p *GuardrailsServiceProxy) ReportViolation(ctx context.Context, req *gogiv1.ReportViolationRequest) (*gogiv1.ReportViolationResponse, error) {
	return p.proxy.ForwardReportViolation(ctx, req)
}
