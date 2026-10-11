package impl

import (
	"context"
	gogiv1 "gogi/gogi/gogi/v1"
	"gogi/gogi/llm/pricing"
	"gogi/gogi/observability"
	"strings"

	"google.golang.org/grpc"
)

// SetTelemetry makes the server record a generation, with model, tokens and cost, and
// the model metrics, for every call; prices are the table's
func (s *LLMModelServer) SetTelemetry(telemetry *observability.Client, prices *pricing.Table) {
	s.telemetry, s.prices = telemetry, prices
}

// startGeneration starts the generation of a call
func (s *LLMModelServer) startGeneration(ctx context.Context, req *gogiv1.LLMRunRequest, operation string) *observability.Generation {
	generation := s.telemetry.StartGeneration(ctx, operation)
	generation.SetModel(req.GetConfig().GetProvider(), req.GetConfig().GetModel(), req.GetConfig().GetModel())
	return generation
}

// usedFallback returns true if another model answered than the one asked for. Providers
// report versioned names, e.g. gpt-4o-mini-2024-07-18 for gpt-4o-mini, which are not fallbacks
func usedFallback(req *gogiv1.LLMRunRequest, provider, model string) bool {
	requested := req.GetConfig()
	if provider != "" && provider != requested.GetProvider() {
		return true
	}
	return requested.GetModel() != "" && model != "" && model != requested.GetModel() &&
		!strings.HasPrefix(model, requested.GetModel()+"-")
}

// endGeneration ends the generation of a call, and records its metrics and, if another
// model answered, a fallback event
func (s *LLMModelServer) endGeneration(ctx context.Context, generation *observability.Generation,
	req *gogiv1.LLMRunRequest, provider, model string, usage *gogiv1.TokenUsage, err error) {
	if s.telemetry == nil {
		return
	}
	requested := req.GetConfig()
	if provider == "" {
		provider = requested.GetProvider()
	}
	if model == "" {
		model = requested.GetModel()
	}
	labels := map[string]string{"provider": provider, "model": model,
		"workflow_id": observability.FromContext(ctx).WorkflowID}

	s.telemetry.Histogram(observability.MetricModelRequestDuration, float64(generation.Elapsed().Milliseconds()), labels)
	s.telemetry.Counter(observability.MetricModelRequests, 1, labels)
	if err != nil {
		s.telemetry.Counter(observability.MetricModelErrors, 1, labels)
		generation.End(err)
		return
	}

	fallback := usedFallback(req, provider, model)
	generation.SetModel(provider, model, requested.GetModel())
	generation.SetFallback(fallback)
	prompt, completion := int(usage.GetPromptTokens()), int(usage.GetCompletionTokens())
	generation.SetUsage(prompt, completion)
	if cost, ok := s.prices.Cost(provider, model, int64(prompt), int64(completion)); ok {
		generation.SetCost(cost)
		s.telemetry.Counter(observability.MetricModelCost, cost, labels)
	}
	s.telemetry.Counter(observability.MetricModelPromptTokens, float64(prompt), labels)
	s.telemetry.Counter(observability.MetricModelCompletionTokens, float64(completion), labels)
	if fallback {
		s.telemetry.Counter(observability.MetricModelFallbacks, 1, labels)
		s.telemetry.Log(ctx, observability.SeverityWarning, "model_fallback",
			requested.GetProvider()+"/"+requested.GetModel()+" failed, "+provider+"/"+model+" answered",
			map[string]string{"requested_provider": requested.GetProvider(), "requested_model": requested.GetModel(),
				"provider": provider, "model": model}, nil)
	}
	generation.End(nil)
}

// meteredStream notes the first token of a streamed response, and keeps the last chunk's
// model and usage for the generation
type meteredStream struct {
	grpc.ServerStreamingServer[gogiv1.LLMStreamChunkResponse]
	generation *observability.Generation
	provider   string
	model      string
	usage      *gogiv1.TokenUsage
}

func (s *meteredStream) Send(chunk *gogiv1.LLMStreamChunkResponse) error {
	if chunk.GetToken() != "" {
		s.generation.MarkFirstToken()
	}
	if chunk.GetProvider() != "" {
		s.provider = chunk.GetProvider()
	}
	if chunk.GetModel() != "" {
		s.model = chunk.GetModel()
	}
	if chunk.Usage != nil {
		s.usage = chunk.GetUsage()
	}
	return s.ServerStreamingServer.Send(chunk)
}
