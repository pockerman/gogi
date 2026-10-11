package impl

import (
	"context"
	gogiv1 "gogi/gogi/gogi/v1"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ObservabilityServiceProxy forwards the calls to the observability service
type ObservabilityServiceProxy struct {
	gogiv1.UnimplementedObservabilityServiceServer
	proxy *GenericGRPCProxy
}

// forwardObservability calls the observability service with a client connected for the call
func forwardObservability[Req any, Resp any](ctx context.Context, p *GenericGRPCProxy, req Req,
	call func(gogiv1.ObservabilityServiceClient, context.Context, Req) (Resp, error)) (Resp, error) {

	conn, err := p.buildConnection("observability")
	if err != nil {
		var zero Resp
		return zero, status.Errorf(codes.Unavailable, "observability service unavailable: %v", err)
	}
	defer conn.Close()
	return call(gogiv1.NewObservabilityServiceClient(conn), ctx, req)
}

func (p *ObservabilityServiceProxy) IngestLogs(ctx context.Context, req *gogiv1.IngestLogsRequest) (*gogiv1.IngestLogsResponse, error) {
	return forwardObservability(ctx, p.proxy, req, func(c gogiv1.ObservabilityServiceClient, ctx context.Context, req *gogiv1.IngestLogsRequest) (*gogiv1.IngestLogsResponse, error) {
		return c.IngestLogs(ctx, req)
	})
}

func (p *ObservabilityServiceProxy) QueryLogs(ctx context.Context, req *gogiv1.QueryLogsRequest) (*gogiv1.QueryLogsResponse, error) {
	return forwardObservability(ctx, p.proxy, req, func(c gogiv1.ObservabilityServiceClient, ctx context.Context, req *gogiv1.QueryLogsRequest) (*gogiv1.QueryLogsResponse, error) {
		return c.QueryLogs(ctx, req)
	})
}

func (p *ObservabilityServiceProxy) RecordMetrics(ctx context.Context, req *gogiv1.RecordMetricsRequest) (*gogiv1.RecordMetricsResponse, error) {
	return forwardObservability(ctx, p.proxy, req, func(c gogiv1.ObservabilityServiceClient, ctx context.Context, req *gogiv1.RecordMetricsRequest) (*gogiv1.RecordMetricsResponse, error) {
		return c.RecordMetrics(ctx, req)
	})
}

func (p *ObservabilityServiceProxy) QueryMetrics(ctx context.Context, req *gogiv1.QueryMetricsRequest) (*gogiv1.QueryMetricsResponse, error) {
	return forwardObservability(ctx, p.proxy, req, func(c gogiv1.ObservabilityServiceClient, ctx context.Context, req *gogiv1.QueryMetricsRequest) (*gogiv1.QueryMetricsResponse, error) {
		return c.QueryMetrics(ctx, req)
	})
}

func (p *ObservabilityServiceProxy) RecordSpan(ctx context.Context, req *gogiv1.RecordSpanRequest) (*gogiv1.RecordSpanResponse, error) {
	return forwardObservability(ctx, p.proxy, req, func(c gogiv1.ObservabilityServiceClient, ctx context.Context, req *gogiv1.RecordSpanRequest) (*gogiv1.RecordSpanResponse, error) {
		return c.RecordSpan(ctx, req)
	})
}

func (p *ObservabilityServiceProxy) RecordGeneration(ctx context.Context, req *gogiv1.RecordGenerationRequest) (*gogiv1.RecordGenerationResponse, error) {
	return forwardObservability(ctx, p.proxy, req, func(c gogiv1.ObservabilityServiceClient, ctx context.Context, req *gogiv1.RecordGenerationRequest) (*gogiv1.RecordGenerationResponse, error) {
		return c.RecordGeneration(ctx, req)
	})
}

func (p *ObservabilityServiceProxy) GetTrace(ctx context.Context, req *gogiv1.GetTraceRequest) (*gogiv1.Trace, error) {
	return forwardObservability(ctx, p.proxy, req, func(c gogiv1.ObservabilityServiceClient, ctx context.Context, req *gogiv1.GetTraceRequest) (*gogiv1.Trace, error) {
		return c.GetTrace(ctx, req)
	})
}

func (p *ObservabilityServiceProxy) QueryTraces(ctx context.Context, req *gogiv1.QueryTracesRequest) (*gogiv1.QueryTracesResponse, error) {
	return forwardObservability(ctx, p.proxy, req, func(c gogiv1.ObservabilityServiceClient, ctx context.Context, req *gogiv1.QueryTracesRequest) (*gogiv1.QueryTracesResponse, error) {
		return c.QueryTraces(ctx, req)
	})
}

func (p *ObservabilityServiceProxy) QueryGenerations(ctx context.Context, req *gogiv1.QueryGenerationsRequest) (*gogiv1.QueryGenerationsResponse, error) {
	return forwardObservability(ctx, p.proxy, req, func(c gogiv1.ObservabilityServiceClient, ctx context.Context, req *gogiv1.QueryGenerationsRequest) (*gogiv1.QueryGenerationsResponse, error) {
		return c.QueryGenerations(ctx, req)
	})
}

func (p *ObservabilityServiceProxy) RecordScore(ctx context.Context, req *gogiv1.RecordScoreRequest) (*gogiv1.RecordScoreResponse, error) {
	return forwardObservability(ctx, p.proxy, req, func(c gogiv1.ObservabilityServiceClient, ctx context.Context, req *gogiv1.RecordScoreRequest) (*gogiv1.RecordScoreResponse, error) {
		return c.RecordScore(ctx, req)
	})
}

func (p *ObservabilityServiceProxy) QueryScores(ctx context.Context, req *gogiv1.QueryScoresRequest) (*gogiv1.QueryScoresResponse, error) {
	return forwardObservability(ctx, p.proxy, req, func(c gogiv1.ObservabilityServiceClient, ctx context.Context, req *gogiv1.QueryScoresRequest) (*gogiv1.QueryScoresResponse, error) {
		return c.QueryScores(ctx, req)
	})
}

func (p *ObservabilityServiceProxy) GetCostReport(ctx context.Context, req *gogiv1.CostReportRequest) (*gogiv1.CostReport, error) {
	return forwardObservability(ctx, p.proxy, req, func(c gogiv1.ObservabilityServiceClient, ctx context.Context, req *gogiv1.CostReportRequest) (*gogiv1.CostReport, error) {
		return c.GetCostReport(ctx, req)
	})
}

func (p *ObservabilityServiceProxy) SetBudgetAlert(ctx context.Context, req *gogiv1.SetBudgetAlertRequest) (*gogiv1.SetBudgetAlertResponse, error) {
	return forwardObservability(ctx, p.proxy, req, func(c gogiv1.ObservabilityServiceClient, ctx context.Context, req *gogiv1.SetBudgetAlertRequest) (*gogiv1.SetBudgetAlertResponse, error) {
		return c.SetBudgetAlert(ctx, req)
	})
}

func (p *ObservabilityServiceProxy) GetBudgetStatus(ctx context.Context, req *gogiv1.GetBudgetStatusRequest) (*gogiv1.BudgetStatus, error) {
	return forwardObservability(ctx, p.proxy, req, func(c gogiv1.ObservabilityServiceClient, ctx context.Context, req *gogiv1.GetBudgetStatusRequest) (*gogiv1.BudgetStatus, error) {
		return c.GetBudgetStatus(ctx, req)
	})
}

func (p *ObservabilityServiceProxy) ListBudgetAlerts(ctx context.Context, req *gogiv1.ListBudgetAlertsRequest) (*gogiv1.ListBudgetAlertsResponse, error) {
	return forwardObservability(ctx, p.proxy, req, func(c gogiv1.ObservabilityServiceClient, ctx context.Context, req *gogiv1.ListBudgetAlertsRequest) (*gogiv1.ListBudgetAlertsResponse, error) {
		return c.ListBudgetAlerts(ctx, req)
	})
}

func (p *ObservabilityServiceProxy) DeleteBudgetAlert(ctx context.Context, req *gogiv1.DeleteBudgetAlertRequest) (*gogiv1.DeleteBudgetAlertResponse, error) {
	return forwardObservability(ctx, p.proxy, req, func(c gogiv1.ObservabilityServiceClient, ctx context.Context, req *gogiv1.DeleteBudgetAlertRequest) (*gogiv1.DeleteBudgetAlertResponse, error) {
		return c.DeleteBudgetAlert(ctx, req)
	})
}

func (p *ObservabilityServiceProxy) GetServiceHealth(ctx context.Context, req *gogiv1.ServiceHealthRequest) (*gogiv1.ServiceHealthResponse, error) {
	return forwardObservability(ctx, p.proxy, req, func(c gogiv1.ObservabilityServiceClient, ctx context.Context, req *gogiv1.ServiceHealthRequest) (*gogiv1.ServiceHealthResponse, error) {
		return c.GetServiceHealth(ctx, req)
	})
}
