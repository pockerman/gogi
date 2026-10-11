package impl

import (
	"context"
	"sync"
	"testing"
	"time"

	gogiv1 "gogi/gogi/gogi/v1"
	"gogi/gogi/llm/pricing"
	"gogi/gogi/observability"

	"google.golang.org/grpc"
)

// recordingExporter keeps the telemetry it is sent
type recordingExporter struct {
	mu          sync.Mutex
	generations []*gogiv1.Generation
	metrics     []*gogiv1.MetricPoint
	logs        []*gogiv1.LogEvent
}

func (e *recordingExporter) RecordSpan(ctx context.Context, in *gogiv1.RecordSpanRequest, opts ...grpc.CallOption) (*gogiv1.RecordSpanResponse, error) {
	return &gogiv1.RecordSpanResponse{}, nil
}

func (e *recordingExporter) RecordGeneration(ctx context.Context, in *gogiv1.RecordGenerationRequest, opts ...grpc.CallOption) (*gogiv1.RecordGenerationResponse, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.generations = append(e.generations, in.Generations...)
	return &gogiv1.RecordGenerationResponse{}, nil
}

func (e *recordingExporter) IngestLogs(ctx context.Context, in *gogiv1.IngestLogsRequest, opts ...grpc.CallOption) (*gogiv1.IngestLogsResponse, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.logs = append(e.logs, in.Events...)
	return &gogiv1.IngestLogsResponse{}, nil
}

func (e *recordingExporter) RecordMetrics(ctx context.Context, in *gogiv1.RecordMetricsRequest, opts ...grpc.CallOption) (*gogiv1.RecordMetricsResponse, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.metrics = append(e.metrics, in.Points...)
	return &gogiv1.RecordMetricsResponse{}, nil
}

func TestRunRecordsAGeneration(t *testing.T) {
	ollama := newFakeOllama(t)
	server := newTestServer(newMemoryRegisteredLLMStore())
	ctx := context.Background()
	if _, err := server.RegisterLLM(ctx, ollamaRegistration(ollama.URL)); err != nil {
		t.Fatal(err)
	}

	exporter := &recordingExporter{}
	config := observability.DefaultConfig("llms")
	config.FlushInterval = time.Hour
	telemetry := observability.NewClient(exporter, config)
	prices := pricing.Default()
	prices.Set("ollama", "llama3.2", pricing.Price{InputPerMTok: 1, OutputPerMTok: 2})
	server.SetTelemetry(telemetry, prices)

	traced := observability.WithTraceContext(ctx, observability.TraceContext{TraceID: "trace-1", SpanID: "run-span",
		WorkflowID: "tutor"})
	if _, err := server.Run(traced, ollamaRunRequest()); err != nil {
		t.Fatal(err)
	}
	failing := ollamaRunRequest()
	failing.Config.Model = "not-registered"
	server.Run(traced, failing)
	telemetry.Close()

	if len(exporter.generations) != 2 {
		t.Fatalf("expected a generation per call, got %d", len(exporter.generations))
	}
	generation, failed := exporter.generations[0], exporter.generations[1]
	if generation.Span.TraceId != "trace-1" || generation.Span.ParentSpanId != "run-span" ||
		generation.Span.WorkflowId != "tutor" || generation.Provider != "ollama" || generation.Model != "llama3.2" ||
		generation.PromptTokens != 4 || generation.CompletionTokens != 3 || generation.FallbackUsed {
		t.Errorf("wrong generation %+v", generation)
	}
	if generation.CostUsd == nil || *generation.CostUsd != (4*1+3*2)/1e6 {
		t.Errorf("the generation should be priced, got %v", generation.CostUsd)
	}
	if failed.Span.Status != gogiv1.SpanStatus_SPAN_STATUS_ERROR {
		t.Errorf("a failed call should fail its generation, got %+v", failed.Span)
	}

	names := map[string]float64{}
	for _, metric := range exporter.metrics {
		names[metric.Name] += metric.Value
	}
	if names[observability.MetricModelRequests] != 2 || names[observability.MetricModelErrors] != 1 ||
		names[observability.MetricModelPromptTokens] != 4 {
		t.Errorf("wrong metrics %v", names)
	}
}

func TestUsedFallback(t *testing.T) {
	request := &gogiv1.LLMRunRequest{Config: &gogiv1.LLMRunRequestConfig{Provider: "openai", Model: "gpt-4o-mini"}}
	cases := map[[2]string]bool{
		{"openai", "gpt-4o-mini"}:            false,
		{"openai", "gpt-4o-mini-2024-07-18"}: false,
		{"openai", "gpt-4o"}:                 true,
		{"anthropic", "claude-haiku-4-5"}:    true,
		{"", ""}:                             false,
	}
	for answered, expected := range cases {
		if got := usedFallback(request, answered[0], answered[1]); got != expected {
			t.Errorf("%v: expected %v, got %v", answered, expected, got)
		}
	}
}
