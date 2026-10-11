package observability

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	gogiv1 "gogi/gogi/gogi/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// fakeExporter records what it is sent, and fails while failing is set
type fakeExporter struct {
	mu          sync.Mutex
	failing     bool
	spans       []*gogiv1.Span
	generations []*gogiv1.Generation
	logs        []*gogiv1.LogEvent
	metrics     []*gogiv1.MetricPoint
	calls       int
}

func (e *fakeExporter) fail() error {
	e.calls++
	if e.failing {
		return errors.New("unavailable")
	}
	return nil
}

func (e *fakeExporter) RecordSpan(ctx context.Context, in *gogiv1.RecordSpanRequest, opts ...grpc.CallOption) (*gogiv1.RecordSpanResponse, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.fail(); err != nil {
		return nil, err
	}
	e.spans = append(e.spans, in.Spans...)
	return &gogiv1.RecordSpanResponse{}, nil
}

func (e *fakeExporter) RecordGeneration(ctx context.Context, in *gogiv1.RecordGenerationRequest, opts ...grpc.CallOption) (*gogiv1.RecordGenerationResponse, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.fail(); err != nil {
		return nil, err
	}
	e.generations = append(e.generations, in.Generations...)
	return &gogiv1.RecordGenerationResponse{}, nil
}

func (e *fakeExporter) IngestLogs(ctx context.Context, in *gogiv1.IngestLogsRequest, opts ...grpc.CallOption) (*gogiv1.IngestLogsResponse, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.fail(); err != nil {
		return nil, err
	}
	e.logs = append(e.logs, in.Events...)
	return &gogiv1.IngestLogsResponse{}, nil
}

func (e *fakeExporter) RecordMetrics(ctx context.Context, in *gogiv1.RecordMetricsRequest, opts ...grpc.CallOption) (*gogiv1.RecordMetricsResponse, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.fail(); err != nil {
		return nil, err
	}
	e.metrics = append(e.metrics, in.Points...)
	return &gogiv1.RecordMetricsResponse{}, nil
}

func (e *fakeExporter) spanCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.spans)
}

func newTestClient(exporter Exporter, configure func(*Config)) *Client {
	config := DefaultConfig("llms")
	config.FlushInterval = time.Hour
	if configure != nil {
		configure(&config)
	}
	return NewClient(exporter, config)
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in time")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestNilClientDoesNothing(t *testing.T) {
	var client *Client
	ctx, span := client.StartSpan(context.Background(), "x")
	span.SetAttribute("a", "b")
	span.End(errors.New("boom"))
	generation := client.StartGeneration(ctx, "y")
	generation.SetModel("openai", "gpt-4o", "gpt-4o")
	generation.End(nil)
	client.Log(ctx, SeverityInfo, "e", "m", nil, nil)
	client.Counter("c", 1, nil)
	client.Flush()
	client.Close()
	if FromContext(ctx).TraceID != "" {
		t.Errorf("a nil client should not start traces")
	}
}

func TestSpansNest(t *testing.T) {
	exporter := &fakeExporter{}
	client := newTestClient(exporter, nil)

	ctx := WithTraceContext(context.Background(), TraceContext{SessionID: "s-1", UserID: "u-1"})
	ctx, root := client.StartSpan(ctx, "workflow.answer")
	childCtx, child := client.StartSpan(ctx, "guardrails.ValidateInput")
	generation := client.StartGeneration(childCtx, "llms.Run")
	generation.SetModel("anthropic", "claude-haiku-4-5", "claude-sonnet-5-5")
	generation.SetUsage(1200, 80)
	generation.SetFallback(true)
	generation.End(nil)
	child.End(errors.New("timeout"))
	root.End(nil)
	root.End(nil) // a span ends once
	client.Close()

	if len(exporter.spans) != 2 || len(exporter.generations) != 1 {
		t.Fatalf("expected 2 spans and 1 generation, got %d and %d", len(exporter.spans), len(exporter.generations))
	}
	childSpan, rootSpan := exporter.spans[0], exporter.spans[1]
	gen := exporter.generations[0]
	if rootSpan.ParentSpanId != "" || childSpan.ParentSpanId != rootSpan.SpanId || gen.Span.ParentSpanId != childSpan.SpanId {
		t.Errorf("spans should nest: root %s, child parent %s, generation parent %s", rootSpan.SpanId,
			childSpan.ParentSpanId, gen.Span.ParentSpanId)
	}
	if childSpan.TraceId != rootSpan.TraceId || gen.Span.TraceId != rootSpan.TraceId || len(rootSpan.TraceId) != 32 {
		t.Errorf("spans should share the trace id")
	}
	if rootSpan.SessionId != "s-1" || gen.Span.UserId != "u-1" {
		t.Errorf("the trace metadata should carry over, got %+v", rootSpan)
	}
	if childSpan.Status != gogiv1.SpanStatus_SPAN_STATUS_ERROR || childSpan.ErrorMessage != "timeout" ||
		rootSpan.Status != gogiv1.SpanStatus_SPAN_STATUS_OK {
		t.Errorf("wrong statuses %v %v", childSpan.Status, rootSpan.Status)
	}
	if gen.Model != "claude-haiku-4-5" || gen.RequestedModel != "claude-sonnet-5-5" || !gen.FallbackUsed ||
		gen.PromptTokens != 1200 || gen.CostUsd != nil {
		t.Errorf("wrong generation %+v", gen)
	}
}

func TestBatchesAreSentWhenFull(t *testing.T) {
	exporter := &fakeExporter{}
	client := newTestClient(exporter, func(c *Config) { c.BatchSize = 3 })
	defer client.Close()

	for i := 0; i < 3; i++ {
		_, span := client.StartSpan(context.Background(), "op")
		span.End(nil)
	}
	waitFor(t, func() bool { return exporter.spanCount() == 3 })
}

func TestBuffersAreSentPeriodically(t *testing.T) {
	exporter := &fakeExporter{}
	client := newTestClient(exporter, func(c *Config) { c.FlushInterval = 20 * time.Millisecond })
	defer client.Close()

	client.Counter(MetricModelRequests, 1, map[string]string{"model": "gpt-4o"})
	_, span := client.StartSpan(context.Background(), "op")
	span.End(nil)
	waitFor(t, func() bool {
		exporter.mu.Lock()
		defer exporter.mu.Unlock()
		return len(exporter.spans) == 1 && len(exporter.metrics) == 1
	})
	if exporter.metrics[0].Service != "llms" || exporter.metrics[0].Type != gogiv1.MetricType_METRIC_TYPE_COUNTER {
		t.Errorf("wrong metric %+v", exporter.metrics[0])
	}
}

func TestFailedSendsAreRetriedAndTheOldestDropped(t *testing.T) {
	exporter := &fakeExporter{failing: true}
	client := newTestClient(exporter, func(c *Config) { c.MaxBuffer = 5; c.BatchSize = 2 })
	defer client.Close()

	for i := 0; i < 8; i++ {
		ctx := WithTraceContext(context.Background(), TraceContext{TraceID: "t", SpanID: ""})
		client.Log(ctx, SeverityWarning, "model_fallback", string(rune('a'+i)), nil, nil)
	}
	client.Flush()
	if len(exporter.logs) != 0 {
		t.Fatalf("nothing should be sent while the service is down")
	}

	// once it is back, what is buffered is sent: the 5 newest
	exporter.mu.Lock()
	exporter.failing = false
	exporter.mu.Unlock()
	client.Flush()
	if len(exporter.logs) != 5 || exporter.logs[0].Message != "d" || exporter.logs[4].Message != "h" {
		messages := []string{}
		for _, l := range exporter.logs {
			messages = append(messages, l.Message)
		}
		t.Errorf("expected the 5 newest events in order, got %v", messages)
	}
	if exporter.logs[0].TraceId != "t" || exporter.logs[0].Service != "llms" {
		t.Errorf("the trace context and service should be filled in, got %+v", exporter.logs[0])
	}
}

func TestContextPropagation(t *testing.T) {
	trace := TraceContext{TraceID: "abc123", SpanID: "def456", SessionID: "s-9", WorkflowID: "wf", UserID: "u-3"}
	outgoing := OutgoingContext(WithTraceContext(context.Background(), trace))
	md, _ := metadata.FromOutgoingContext(outgoing)

	incoming := ContextFromIncoming(metadata.NewIncomingContext(context.Background(), md))
	if got := FromContext(incoming); got != trace {
		t.Errorf("the trace context should round-trip, got %+v", got)
	}

	// malformed ids start a new trace
	bad := metadata.NewIncomingContext(context.Background(), metadata.Pairs(MetadataTraceID, "not valid!", MetadataSpanID, "x"))
	if got := FromContext(ContextFromIncoming(bad)); got.TraceID != "" || got.SpanID != "" {
		t.Errorf("malformed ids should be ignored, got %+v", got)
	}
	// without a trace there is nothing to send
	if ctx := OutgoingContext(context.Background()); ctx != context.Background() {
		if _, ok := metadata.FromOutgoingContext(ctx); ok {
			t.Errorf("no metadata should be added without a trace")
		}
	}
}

func TestUnaryServerInterceptor(t *testing.T) {
	exporter := &fakeExporter{}
	client := newTestClient(exporter, nil)
	interceptor := client.UnaryServerInterceptor()
	md := metadata.Pairs(MetadataTraceID, "trace1", MetadataSpanID, "parent1", MetadataSessionID, "s-4")
	ctx := metadata.NewIncomingContext(context.Background(), md)

	var handlerTrace TraceContext
	handler := func(err error) grpc.UnaryHandler {
		return func(ctx context.Context, req any) (any, error) {
			handlerTrace = FromContext(ctx)
			return "ok", err
		}
	}
	info := &grpc.UnaryServerInfo{FullMethod: "/gogi.v1.LLMModelServer/Run"}

	interceptor(ctx, nil, info, handler(nil))
	if handlerTrace.SpanID == "parent1" || handlerTrace.TraceID != "trace1" {
		t.Errorf("the handler should see the RPC's span as the current one, got %+v", handlerTrace)
	}
	interceptor(ctx, nil, info, handler(status.Error(codes.InvalidArgument, "bad model")))
	interceptor(ctx, nil, info, handler(status.Error(codes.Unavailable, "provider down")))
	interceptor(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/grpc.health.v1.Health/Check"}, handler(nil))
	client.Close()

	if len(exporter.spans) != 3 {
		t.Fatalf("expected a span per RPC, health checks excluded, got %d", len(exporter.spans))
	}
	ok, invalid, unavailable := exporter.spans[0], exporter.spans[1], exporter.spans[2]
	if ok.TraceId != "trace1" || ok.ParentSpanId != "parent1" || ok.Operation != "llms.Run" || ok.SessionId != "s-4" {
		t.Errorf("the span should continue the caller's trace, got %+v", ok)
	}
	if invalid.Status != gogiv1.SpanStatus_SPAN_STATUS_OK || invalid.Attributes["rpc.code"] != "InvalidArgument" {
		t.Errorf("a caller's mistake is not a service failure, got %+v", invalid)
	}
	if unavailable.Status != gogiv1.SpanStatus_SPAN_STATUS_ERROR || unavailable.ErrorMessage == "" {
		t.Errorf("a service failure should fail the span, got %+v", unavailable)
	}
}
