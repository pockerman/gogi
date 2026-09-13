package impl

import (
	"context"
	gogiv1 "gogi/gogi/gogi/v1"
	"gogi/gogi/utils"

	"gogi/gogi/storage/postgres"

	"github.com/jackc/pgx/v5/pgxpool"
)

// GuardrailsServer implements the GuardrailsService server.
// The server provides the following endpoints
//  ValidateInput(ValidateInputRequest) returns (ValidateInputResponse);
//  FilterOutput(FilterOutputRequest) returns (FilterOutputResponse);
//  CheckPolicy(CheckPolicyRequest) returns (CheckPolicyResponse);
//  ReportViolation(ReportViolationRequest) returns (ReportViolationResponse);

type GuardrailsServer struct {
	gogiv1.UnimplementedGuardrailsServiceServer
	gogiGuardrailsRepo postgres.GogiGuardrailsRepository
}

func NewGuardrailsServer(dbClient *pgxpool.Pool) *GuardrailsServer {
	return &GuardrailsServer{
		gogiGuardrailsRepo: *postgres.NewGogiGuardrailsRepository(dbClient),
	}
}

func (s *GuardrailsServer) ValidateInput(ctx context.Context, req *gogiv1.ValidateInputRequest) (*gogiv1.ValidateInputResponse, error) {

	return &gogiv1.ValidateInputResponse{
		Allowed: true,
	}, nil
}

func (s *GuardrailsServer) FilterOutput(ctx context.Context, req *gogiv1.FilterOutputRequest) (*gogiv1.FilterOutputResponse, error) {

	return &gogiv1.FilterOutputResponse{
		Content:  req.GetContent(),
		Modified: false,
	}, nil
}

func (s *GuardrailsServer) CheckPolicy(ctx context.Context, req *gogiv1.CheckPolicyRequest) (*gogiv1.CheckPolicyResponse, error) {

	return &gogiv1.CheckPolicyResponse{
		Allowed: true,
	}, nil
}

func (s *GuardrailsServer) ReportViolation(ctx context.Context, req *gogiv1.ReportViolationRequest) (*gogiv1.ReportViolationResponse, error) {

	return &gogiv1.ReportViolationResponse{
		ViolationId: utils.NewUUIDString(),
		Recorded:    true,
	}, nil
}
