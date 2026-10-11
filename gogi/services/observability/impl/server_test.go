package impl

import (
	"context"
	"encoding/json"
	"fmt"
	gogiv1 "gogi/gogi/gogi/v1"
	"gogi/gogi/llm/pricing"
	"gogi/gogi/storage/postgres"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// The fixtures are a language-learning app: a tutor answers questions, a grader checks
// exercises, both call models

var t0 = time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC) // a Wednesday

func ms(offset time.Duration) int64 {
	return t0.Add(offset).UnixMilli()
}

func near(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

func requireCode(t *testing.T, err error, code codes.Code) {
	t.Helper()
	if status.Code(err) != code {
		t.Errorf("expected code %s, got %v", code, err)
	}
}

// --- pure functions ---

func TestPeriod(t *testing.T) {
	start, end := Period("daily", t0)
	if !start.Equal(time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)) || end.Sub(start) != 24*time.Hour {
		t.Errorf("wrong day %v %v", start, end)
	}
	start, end = Period("weekly", t0)
	if start.Weekday() != time.Monday || !start.Equal(time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)) ||
		end.Sub(start) != 7*24*time.Hour {
		t.Errorf("weeks start on Monday, got %v %v", start, end)
	}
	// a Sunday belongs to the week that started the Monday before
	if start, _ := Period("weekly", time.Date(2026, 10, 18, 23, 0, 0, 0, time.UTC)); start.Day() != 12 {
		t.Errorf("wrong week for a Sunday %v", start)
	}
	start, end = Period("monthly", t0)
	if start.Day() != 1 || start.Month() != time.October || end.Month() != time.November {
		t.Errorf("wrong month %v %v", start, end)
	}
}

func TestBudgetStatusAt(t *testing.T) {
	alert := &gogiv1.BudgetAlert{Name: "tutor", Period: "daily", LimitUsd: 100, Thresholds: []float64{0.7, 0.9, 1.0}}

	// 10:00, $50 spent: projected $120 by midnight
	result := BudgetStatusAt(alert, 50, t0)
	if !near(result.Fraction, 0.5) || !near(result.ProjectedUsd, 120) || result.State != "ok" ||
		len(result.CrossedThresholds) != 0 {
		t.Errorf("wrong status %+v", result)
	}
	result = BudgetStatusAt(alert, 95, t0)
	if result.State != "warning" || len(result.CrossedThresholds) != 2 {
		t.Errorf("expected two thresholds crossed, got %+v", result)
	}
	if result := BudgetStatusAt(alert, 130, t0); result.State != "exceeded" || len(result.CrossedThresholds) != 3 {
		t.Errorf("expected exceeded, got %+v", result)
	}
	// minutes into a period, a projection extrapolates from at least an hour
	early := time.Date(2026, 10, 14, 0, 5, 0, 0, time.UTC)
	if result := BudgetStatusAt(alert, 2, early); !near(result.ProjectedUsd, 48) {
		t.Errorf("expected $2/hour projected over 24 hours, got %v", result.ProjectedUsd)
	}
}

func TestHealth(t *testing.T) {
	config := DefaultConfig()
	window := 15 * time.Minute
	cases := map[postgres.ServiceActivity]string{
		{Service: "llms", Spans: 100, Errors: 1}:  "healthy",
		{Service: "llms", Spans: 100, Errors: 10}: "degraded",
		{Service: "llms", Spans: 100, Errors: 40}: "unhealthy",
		{Service: "llms", Spans: 0}:               "unknown",
	}
	for activity, expected := range cases {
		if got := Health(activity, window, config); got.Status != expected {
			t.Errorf("%+v: expected %s, got %s (%s)", activity, expected, got.Status, got.Reason)
		}
	}
}

func TestPrice(t *testing.T) {
	server := NewObservabilityServer(nil, pricing.Default(), DefaultConfig())

	reported := server.price(&gogiv1.Generation{Provider: "openai", Model: "gpt-4o", CostUsd: proto.Float64(0.5)})
	if reported.CostUSD != 0.5 || reported.Estimated || !reported.Priced {
		t.Errorf("a reported cost should be kept, got %+v", reported)
	}
	estimated := server.price(&gogiv1.Generation{Provider: "anthropic", Model: "claude-sonnet-5-5",
		PromptTokens: 2000, CompletionTokens: 500})
	if !near(estimated.CostUSD, (2000*2+500*10)/1e6) || !estimated.Estimated || !estimated.Priced {
		t.Errorf("an unreported cost should be priced, got %+v", estimated)
	}
	unknown := server.price(&gogiv1.Generation{Provider: "ollama", Model: "llama3.2", PromptTokens: 10})
	if unknown.CostUSD != 0 || unknown.Priced {
		t.Errorf("a model without a price should be marked unpriced, got %+v", unknown)
	}
}

func TestConfigFromEnv(t *testing.T) {
	t.Setenv("GOGI_OBSERVABILITY_LOG_RETENTION", "48h")
	config, err := ConfigFromEnv()
	if err != nil || config.LogRetention != 48*time.Hour || config.TraceRetention != 90*24*time.Hour {
		t.Errorf("wrong config %+v, %v", config, err)
	}
	t.Setenv("GOGI_OBSERVABILITY_DEGRADED_ERROR_RATE", "0.5")
	if _, err := ConfigFromEnv(); err == nil {
		t.Errorf("degraded above unhealthy should be rejected")
	}
}

// --- against Postgres ---

// The tests below run against a migrated database, e.g.
// GOGI_TEST_POSTGRES_DSN=postgres://postgres:test@localhost:5432/gogi?sslmode=disable
func newTestServer(t *testing.T) *ObservabilityServer {
	dsn := os.Getenv("GOGI_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("GOGI_TEST_POSTGRES_DSN is not set")
	}
	pool, err := postgres.NewPool(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	clear := func() {
		for _, table := range []string{"obs_spans", "obs_generations", "obs_traces", "obs_scores", "obs_logs",
			"obs_metric_points", "obs_budget_alerts"} {
			if _, err := pool.Exec(context.Background(), "DELETE FROM "+table); err != nil {
				t.Fatal(err)
			}
		}
	}
	clear()
	t.Cleanup(clear)
	server := NewObservabilityServer(postgres.NewGogiObservabilityRepository(pool), pricing.Default(), DefaultConfig())
	server.now = func() time.Time { return t0.Add(time.Hour) }
	return server
}

func span(trace, id, parent, service, operation string, start, end time.Duration) *gogiv1.Span {
	return &gogiv1.Span{TraceId: trace, SpanId: id, ParentSpanId: parent, Service: service, Operation: operation,
		StartTime: ms(start), EndTime: ms(end), Status: gogiv1.SpanStatus_SPAN_STATUS_OK,
		Attributes: map[string]string{}, NumericAttributes: map[string]float64{}}
}

func generation(trace, id, parent, model string, start, end time.Duration, prompt, completion int32) *gogiv1.Generation {
	return &gogiv1.Generation{Span: span(trace, id, parent, "llms", "llms.Run", start, end), Provider: "anthropic",
		Model: model, RequestedModel: model, PromptTokens: prompt, CompletionTokens: completion}
}

// recordTutorTrace records a tutor request: a root span, a guardrail check, a model call
func recordTutorTrace(t *testing.T, server *ObservabilityServer, trace string, offset time.Duration, model string,
	fail bool) {
	t.Helper()
	ctx := context.Background()
	root := span(trace, trace+"-root", "", "tutor-app", "tutor.answer", offset, offset+1500*time.Millisecond)
	root.SessionId, root.WorkflowId, root.UserId = "session-"+trace, "tutor", "learner-7"
	root.Input, root.Output, root.Tags = "How do I say 'library' in Spanish?", "Biblioteca.", []string{"spanish"}
	check := span(trace, trace+"-check", trace+"-root", "guardrails", "guardrails.ValidateInput", offset+10*time.Millisecond,
		offset+40*time.Millisecond)
	if fail {
		check.Status, check.ErrorMessage = gogiv1.SpanStatus_SPAN_STATUS_ERROR, "policy store unavailable"
	}
	if _, err := server.RecordSpan(ctx, &gogiv1.RecordSpanRequest{Spans: []*gogiv1.Span{root, check}}); err != nil {
		t.Fatal(err)
	}
	gen := generation(trace, trace+"-gen", trace+"-root", model, offset+50*time.Millisecond, offset+1400*time.Millisecond, 1000, 100)
	gen.Span.Attributes["team"] = "languages"
	if _, err := server.RecordGeneration(ctx, &gogiv1.RecordGenerationRequest{Generations: []*gogiv1.Generation{gen}}); err != nil {
		t.Fatal(err)
	}
}

func TestTraces(t *testing.T) {
	server := newTestServer(t)
	ctx := context.Background()
	recordTutorTrace(t, server, "trace-a", 0, "claude-haiku-4-5", false)
	recordTutorTrace(t, server, "trace-b", time.Minute, "claude-sonnet-5-5", true)

	trace, err := server.GetTrace(ctx, &gogiv1.GetTraceRequest{TraceId: "trace-a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(trace.Spans) != 2 || len(trace.Generations) != 1 || trace.SpanCount != 3 || trace.ErrorCount != 0 {
		t.Fatalf("wrong trace %+v", trace)
	}
	if trace.SessionId != "session-trace-a" || trace.WorkflowId != "tutor" || trace.Input == "" ||
		trace.Output != "Biblioteca." || trace.Tags[0] != "spanish" || !near(trace.TotalDurationMs, 1500) {
		t.Errorf("the trace should take the root span's metadata, got %+v", trace)
	}
	gen := trace.Generations[0]
	if !near(trace.TotalCostUsd, (1000*1+100*5)/1e6) || trace.TotalTokens != 1100 || !gen.CostEstimated ||
		gen.Span.ParentSpanId != "trace-a-root" || gen.Span.SessionId != "session-trace-a" {
		t.Errorf("the generation should be priced and nested, got %+v, cost %v", gen, trace.TotalCostUsd)
	}

	// a span sent again is ignored
	recordTutorTrace(t, server, "trace-a", 0, "claude-haiku-4-5", false)
	again, _ := server.GetTrace(ctx, &gogiv1.GetTraceRequest{TraceId: "trace-a"})
	if again.SpanCount != 3 || !near(again.TotalCostUsd, trace.TotalCostUsd) {
		t.Errorf("a resent span should not count twice, got %d spans, %v", again.SpanCount, again.TotalCostUsd)
	}

	_, err = server.GetTrace(ctx, &gogiv1.GetTraceRequest{TraceId: "missing"})
	requireCode(t, err, codes.NotFound)

	listed, err := server.QueryTraces(ctx, &gogiv1.QueryTracesRequest{WorkflowId: "tutor"})
	if err != nil || len(listed.Traces) != 2 || listed.Traces[0].TraceId != "trace-b" || listed.Traces[0].Spans != nil {
		t.Fatalf("the most recent trace comes first, without its spans, got %+v, %v", listed, err)
	}
	if errs, _ := server.QueryTraces(ctx, &gogiv1.QueryTracesRequest{ErrorsOnly: true}); len(errs.Traces) != 1 ||
		errs.Traces[0].TraceId != "trace-b" {
		t.Errorf("expected only the failed trace, got %+v", errs.Traces)
	}
	if byService, _ := server.QueryTraces(ctx, &gogiv1.QueryTracesRequest{Service: "guardrails"}); len(byService.Traces) != 2 {
		t.Errorf("both traces have a guardrails span")
	}
	if costly, _ := server.QueryTraces(ctx, &gogiv1.QueryTracesRequest{OrderBy: "cost", Limit: 1}); costly.Traces[0].TraceId != "trace-b" ||
		costly.NextCursor == "" {
		t.Errorf("the sonnet trace costs more, got %+v", costly.Traces)
	}

	// paging with the cursor
	first, _ := server.QueryTraces(ctx, &gogiv1.QueryTracesRequest{Limit: 1})
	second, _ := server.QueryTraces(ctx, &gogiv1.QueryTracesRequest{Limit: 1, Cursor: first.NextCursor})
	if first.Traces[0].TraceId != "trace-b" || second.Traces[0].TraceId != "trace-a" || second.NextCursor != "" {
		t.Errorf("wrong pages %v %v", first.Traces[0].TraceId, second.Traces[0].TraceId)
	}
	_, err = server.QueryTraces(ctx, &gogiv1.QueryTracesRequest{Cursor: "garbage!"})
	requireCode(t, err, codes.InvalidArgument)

	generations, err := server.QueryGenerations(ctx, &gogiv1.QueryGenerationsRequest{Model: "claude-sonnet-5-5"})
	if err != nil || len(generations.Generations) != 1 || generations.Generations[0].Span.TraceId != "trace-b" {
		t.Errorf("wrong generations %+v, %v", generations, err)
	}
	if cheap, _ := server.QueryGenerations(ctx, &gogiv1.QueryGenerationsRequest{MinCostUsd: 0.002}); len(cheap.Generations) != 1 {
		t.Errorf("only the sonnet call costs more than $0.002, got %d", len(cheap.Generations))
	}
}

func TestRecordValidation(t *testing.T) {
	server := newTestServer(t)
	ctx := context.Background()

	good := span("trace-v", "s1", "", "grader", "grader.check", 0, time.Second)
	selfParent := span("trace-v", "s2", "s2", "grader", "grader.check", 0, time.Second)
	backwards := span("trace-v", "s3", "", "grader", "grader.check", time.Second, 0)
	noOperation := span("trace-v", "s4", "", "grader", "", 0, time.Second)
	badID := span("trace v", "s5", "", "grader", "grader.check", 0, time.Second)

	response, err := server.RecordSpan(ctx, &gogiv1.RecordSpanRequest{Spans: []*gogiv1.Span{good, selfParent, backwards, noOperation, badID}})
	if err != nil || response.Accepted != 1 || len(response.Rejected) != 4 {
		t.Errorf("one bad span should not lose the others, got %+v, %v", response, err)
	}

	gens, _ := server.RecordGeneration(ctx, &gogiv1.RecordGenerationRequest{Generations: []*gogiv1.Generation{
		{Span: span("trace-v", "g1", "s1", "llms", "llms.Run", 0, time.Second)},
		{Span: span("trace-v", "g2", "s1", "llms", "llms.Run", 0, time.Second), Model: "m", CostUsd: proto.Float64(-1)},
	}})
	if gens.Accepted != 0 || len(gens.Rejected) != 2 {
		t.Errorf("expected both generations rejected, got %+v", gens)
	}

	scores, _ := server.RecordScore(ctx, &gogiv1.RecordScoreRequest{Scores: []*gogiv1.Score{
		{TraceId: "trace-v", Name: "accuracy", Source: gogiv1.ScoreSource_SCORE_SOURCE_AUTOMATED},
		{TraceId: "trace-v", Name: "accuracy", Value: &gogiv1.Score_NumericValue{NumericValue: 0.8}},
		{TraceId: "trace-v", Name: "accuracy", Value: &gogiv1.Score_NumericValue{NumericValue: 0.8},
			Source: gogiv1.ScoreSource_SCORE_SOURCE_AUTOMATED},
	}})
	if len(scores.ScoreIds) != 1 || len(scores.Rejected) != 2 {
		t.Errorf("a score needs a value and a source, got %+v", scores)
	}

	big := make([]*gogiv1.Span, maxBatch+1)
	_, err = server.RecordSpan(ctx, &gogiv1.RecordSpanRequest{Spans: big})
	requireCode(t, err, codes.InvalidArgument)
}

func TestScores(t *testing.T) {
	server := newTestServer(t)
	ctx := context.Background()
	recordTutorTrace(t, server, "trace-a", 0, "claude-haiku-4-5", false)
	recordTutorTrace(t, server, "trace-b", time.Minute, "claude-haiku-4-5", false)

	_, err := server.RecordScore(ctx, &gogiv1.RecordScoreRequest{Scores: []*gogiv1.Score{
		{TraceId: "trace-a", Name: "helpfulness", Value: &gogiv1.Score_NumericValue{NumericValue: 0.9},
			Source: gogiv1.ScoreSource_SCORE_SOURCE_MODEL_JUDGE, Metadata: map[string]string{"judge": "claude-sonnet-5-5"}},
		{TraceId: "trace-b", Name: "helpfulness", Value: &gogiv1.Score_NumericValue{NumericValue: 0.3},
			Source: gogiv1.ScoreSource_SCORE_SOURCE_MODEL_JUDGE},
		{TraceId: "trace-b", Name: "helpfulness", Value: &gogiv1.Score_NumericValue{NumericValue: 0.4},
			Source: gogiv1.ScoreSource_SCORE_SOURCE_HUMAN},
		{TraceId: "trace-a", Name: "answer_in_spanish", Value: &gogiv1.Score_BooleanValue{BooleanValue: true},
			Source: gogiv1.ScoreSource_SCORE_SOURCE_AUTOMATED},
		{TraceId: "trace-b", Name: "answer_in_spanish", Value: &gogiv1.Score_BooleanValue{BooleanValue: false},
			Source: gogiv1.ScoreSource_SCORE_SOURCE_AUTOMATED},
		{TraceId: "trace-a", Name: "level", Value: &gogiv1.Score_CategoricalValue{CategoricalValue: "beginner"},
			Source: gogiv1.ScoreSource_SCORE_SOURCE_HUMAN},
	}})
	if err != nil {
		t.Fatal(err)
	}

	// traces by quality: helpfulness below 0.5
	low, _ := server.QueryTraces(ctx, &gogiv1.QueryTracesRequest{ScoreName: "helpfulness", MaxScore: proto.Float64(0.5)})
	if len(low.Traces) != 1 || low.Traces[0].TraceId != "trace-b" {
		t.Errorf("expected only trace-b, got %+v", low.Traces)
	}
	_, err = server.QueryTraces(ctx, &gogiv1.QueryTracesRequest{MaxScore: proto.Float64(0.5)})
	requireCode(t, err, codes.InvalidArgument)

	response, err := server.QueryScores(ctx, &gogiv1.QueryScoresRequest{WorkflowId: "tutor"})
	if err != nil || len(response.Scores) != 6 {
		t.Fatalf("wrong scores %+v, %v", response, err)
	}
	summaries := map[string]*gogiv1.ScoreSummary{}
	for _, summary := range response.Summaries {
		summaries[summary.Name+"/"+summary.Source.String()] = summary
	}
	judge := summaries["helpfulness/SCORE_SOURCE_MODEL_JUDGE"]
	booleans := summaries["answer_in_spanish/SCORE_SOURCE_AUTOMATED"]
	levels := summaries["level/SCORE_SOURCE_HUMAN"]
	if judge == nil || judge.Count != 2 || !near(judge.Mean, 0.6) || booleans == nil || !near(booleans.TrueRatio, 0.5) ||
		levels == nil || levels.Categories["beginner"] != 1 {
		t.Errorf("wrong summaries %+v", response.Summaries)
	}

	trace, _ := server.GetTrace(ctx, &gogiv1.GetTraceRequest{TraceId: "trace-a"})
	if len(trace.Scores) != 3 {
		t.Errorf("the trace should carry its scores, got %d", len(trace.Scores))
	}
	human, _ := server.QueryScores(ctx, &gogiv1.QueryScoresRequest{Source: gogiv1.ScoreSource_SCORE_SOURCE_HUMAN, Limit: 1})
	if len(human.Scores) != 1 || human.NextCursor == "" || len(human.Summaries) != 2 {
		t.Errorf("summaries cover every match, not the page, got %+v", human)
	}
}

func TestLogs(t *testing.T) {
	server := newTestServer(t)
	ctx := context.Background()

	events := []*gogiv1.LogEvent{
		{TraceId: "trace-a", SpanId: "trace-a-gen", Timestamp: ms(0), Service: "llms", Severity: gogiv1.Severity_SEVERITY_WARNING,
			EventType: "model_fallback", Message: "fell back", Attributes: map[string]string{"from": "anthropic", "to": "openai"}},
		{TraceId: "trace-a", Timestamp: ms(time.Second), Service: "guardrails", EventType: "rule_evaluated",
			Message: "rule passed", NumericAttributes: map[string]float64{"confidence": 0.97}},
		{Timestamp: ms(2 * time.Second), Service: "llms", Severity: gogiv1.Severity_SEVERITY_ERROR, Message: "provider down"},
		{Service: "llms"},
	}
	response, err := server.IngestLogs(ctx, &gogiv1.IngestLogsRequest{Events: events})
	if err != nil || response.Accepted != 3 || len(response.Rejected) != 1 {
		t.Fatalf("wrong response %+v, %v", response, err)
	}

	byTrace, _ := server.QueryLogs(ctx, &gogiv1.QueryLogsRequest{TraceId: "trace-a"})
	if len(byTrace.Events) != 2 || byTrace.Events[0].EventType != "rule_evaluated" ||
		byTrace.Events[0].Severity != gogiv1.Severity_SEVERITY_INFO || byTrace.Events[0].NumericAttributes["confidence"] != 0.97 {
		t.Errorf("wrong events %+v", byTrace.Events)
	}
	warnings, _ := server.QueryLogs(ctx, &gogiv1.QueryLogsRequest{MinSeverity: gogiv1.Severity_SEVERITY_WARNING})
	if len(warnings.Events) != 2 {
		t.Errorf("expected the warning and the error, got %d", len(warnings.Events))
	}
	fallbacks, _ := server.QueryLogs(ctx, &gogiv1.QueryLogsRequest{Attributes: map[string]string{"to": "openai"}})
	if len(fallbacks.Events) != 1 || fallbacks.Events[0].Attributes["from"] != "anthropic" {
		t.Errorf("wrong attribute match %+v", fallbacks.Events)
	}
	page, _ := server.QueryLogs(ctx, &gogiv1.QueryLogsRequest{Limit: 2})
	rest, _ := server.QueryLogs(ctx, &gogiv1.QueryLogsRequest{Limit: 2, Cursor: page.NextCursor})
	if len(page.Events) != 2 || len(rest.Events) != 1 || rest.Events[0].Message != "fell back" {
		t.Errorf("wrong pages %d %d", len(page.Events), len(rest.Events))
	}
}

func TestMetrics(t *testing.T) {
	server := newTestServer(t)
	ctx := context.Background()

	points := make([]*gogiv1.MetricPoint, 0)
	for i := 0; i < 100; i++ {
		model := "claude-haiku-4-5"
		if i%2 == 1 {
			model = "claude-sonnet-5-5"
		}
		points = append(points,
			&gogiv1.MetricPoint{Name: "gogi.models.request_duration_ms", Value: float64(i + 1), Timestamp: ms(time.Duration(i) * time.Second),
				Labels: map[string]string{"model": model, "workflow_id": "tutor"}},
			&gogiv1.MetricPoint{Name: "gogi.models.requests_total", Value: 1, Timestamp: ms(time.Duration(i) * time.Second),
				Labels: map[string]string{"model": model}})
	}
	points = append(points, &gogiv1.MetricPoint{Name: "Bad Name", Value: 1}, &gogiv1.MetricPoint{Name: "ok_total",
		Value: math.NaN()})
	response, err := server.RecordMetrics(ctx, &gogiv1.RecordMetricsRequest{Points: points})
	if err != nil || response.Accepted != 200 || len(response.Rejected) != 2 {
		t.Fatalf("wrong response %+v, %v", response, err)
	}

	p95, err := server.QueryMetrics(ctx, &gogiv1.QueryMetricsRequest{Name: "gogi.models.request_duration_ms",
		StartTime: ms(0), EndTime: ms(time.Hour), Aggregation: "p95"})
	if err != nil || len(p95.Series) != 1 || !near(p95.Series[0].Samples[0].Value, 95.05) ||
		p95.Series[0].Samples[0].Count != 100 {
		t.Fatalf("wrong p95 %+v, %v", p95, err)
	}

	// a series per model, a sample per 50 seconds; counters sum by default
	requests, err := server.QueryMetrics(ctx, &gogiv1.QueryMetricsRequest{Name: "gogi.models.requests_total",
		StartTime: ms(0), EndTime: ms(time.Hour), GroupBy: []string{"model"}, StepMs: 50_000})
	if err != nil {
		t.Fatal(err)
	}
	if requests.Aggregation != "sum" || len(requests.Series) != 2 || len(requests.Series[0].Samples) != 2 ||
		requests.Series[0].Samples[0].Value != 25 || requests.Series[0].Labels["model"] != "claude-haiku-4-5" {
		t.Errorf("wrong series %+v", requests)
	}

	filtered, _ := server.QueryMetrics(ctx, &gogiv1.QueryMetricsRequest{Name: "gogi.models.request_duration_ms",
		StartTime: ms(0), LabelFilters: map[string]string{"model": "claude-sonnet-5-5"}, Aggregation: "max"})
	if filtered.Series[0].Samples[0].Value != 100 {
		t.Errorf("expected the slowest sonnet call, got %+v", filtered.Series)
	}

	for name, request := range map[string]*gogiv1.QueryMetricsRequest{
		"aggregation": {Name: "gogi.models.requests_total", StartTime: ms(0), Aggregation: "median"},
		"tiny step":   {Name: "gogi.models.requests_total", StartTime: ms(0), StepMs: 10},
		"no start":    {Name: "gogi.models.requests_total"},
		"bad label":   {Name: "gogi.models.requests_total", StartTime: ms(0), GroupBy: []string{"1bad"}},
	} {
		if _, err := server.QueryMetrics(ctx, request); status.Code(err) != codes.InvalidArgument {
			t.Errorf("%s: expected InvalidArgument, got %v", name, err)
		}
	}
}

func TestCostReport(t *testing.T) {
	server := newTestServer(t)
	ctx := context.Background()
	recordTutorTrace(t, server, "trace-a", 0, "claude-haiku-4-5", false)
	recordTutorTrace(t, server, "trace-b", 26*time.Hour, "claude-sonnet-5-5", false)
	// a grader call with a reported cost, and one of a model without a price
	grader := generation("trace-c", "trace-c-gen", "", "gpt-4o", time.Hour, time.Hour+time.Second, 500, 50)
	grader.Provider, grader.CostUsd = "openai", proto.Float64(0.25)
	grader.Span.WorkflowId = "grader"
	local := generation("trace-d", "trace-d-gen", "", "llama3.2", time.Hour, time.Hour+time.Second, 500, 50)
	local.Provider = "ollama"
	local.Span.WorkflowId = "grader"
	server.RecordGeneration(ctx, &gogiv1.RecordGenerationRequest{Generations: []*gogiv1.Generation{grader, local}})

	haiku, sonnet := (1000*1+100*5)/1e6, (1000*2+100*10)/1e6
	report, err := server.GetCostReport(ctx, &gogiv1.CostReportRequest{StartTime: ms(-time.Hour), EndTime: ms(48 * time.Hour),
		GroupBy: []string{"workflow_id"}})
	if err != nil {
		t.Fatal(err)
	}
	if !near(report.TotalCostUsd, haiku+sonnet+0.25) || report.Generations != 4 || report.UnpricedGenerations != 1 ||
		len(report.Buckets) != 2 || report.Buckets[0].Group["workflow_id"] != "grader" {
		t.Errorf("wrong report %+v", report)
	}

	// drill down: the languages team's spend, by model, by day
	daily, _ := server.GetCostReport(ctx, &gogiv1.CostReportRequest{StartTime: ms(-time.Hour), EndTime: ms(48 * time.Hour),
		GroupBy: []string{"model"}, Filters: map[string]string{"team": "languages"}, Granularity: "daily"})
	if len(daily.Buckets) != 2 || daily.Buckets[0].Group["model"] != "claude-haiku-4-5" ||
		daily.Buckets[1].PeriodStart != time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC).UnixMilli() ||
		!near(daily.TotalCostUsd, haiku+sonnet) {
		t.Errorf("wrong drill-down %+v", daily)
	}

	_, err = server.GetCostReport(ctx, &gogiv1.CostReportRequest{StartTime: ms(0), Granularity: "yearly"})
	requireCode(t, err, codes.InvalidArgument)
	_, err = server.GetCostReport(ctx, &gogiv1.CostReportRequest{StartTime: ms(0), GroupBy: []string{"bad dimension"}})
	requireCode(t, err, codes.InvalidArgument)
}

func TestBudgets(t *testing.T) {
	server := newTestServer(t)
	ctx := context.Background()
	recordTutorTrace(t, server, "trace-a", 0, "claude-sonnet-5-5", false) // $0.003 today

	var mu sync.Mutex
	received := make([]budgetNotification, 0)
	failing := true
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if failing {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var notification budgetNotification
		json.Unmarshal(body, &notification)
		received = append(received, notification)
	}))
	defer hook.Close()

	set, err := server.SetBudgetAlert(ctx, &gogiv1.SetBudgetAlertRequest{Alert: &gogiv1.BudgetAlert{Name: "tutor-daily",
		Period: "daily", LimitUsd: 0.004, Thresholds: []float64{0.9, 0.5, 0.5}, WebhookUrl: hook.URL,
		Filters: map[string]string{"workflow_id": "tutor"}}})
	if err != nil || len(set.Alert.Thresholds) != 2 || set.Alert.Thresholds[0] != 0.5 {
		t.Fatalf("thresholds should be sorted and unique, got %+v, %v", set, err)
	}

	budget, err := server.GetBudgetStatus(ctx, &gogiv1.GetBudgetStatusRequest{Name: "tutor-daily"})
	if err != nil || !near(budget.SpentUsd, 0.003) || budget.State != "warning" || len(budget.CrossedThresholds) != 1 {
		t.Fatalf("wrong status %+v, %v", budget, err)
	}

	// the webhook fails: the notification is retried on the next check
	server.CheckBudgets(ctx, hook.Client())
	mu.Lock()
	failing = false
	mu.Unlock()
	server.CheckBudgets(ctx, hook.Client())
	server.CheckBudgets(ctx, hook.Client())
	if len(received) != 1 || received[0].Threshold != 0.5 || received[0].Budget != "tutor-daily" || received[0].Text == "" {
		t.Errorf("expected one notification at 50%%, got %+v", received)
	}

	for name, alert := range map[string]*gogiv1.BudgetAlert{
		"period":    {Name: "b", Period: "yearly", LimitUsd: 1},
		"limit":     {Name: "b", Period: "daily", LimitUsd: 0},
		"threshold": {Name: "b", Period: "daily", LimitUsd: 1, Thresholds: []float64{-1}},
		"webhook":   {Name: "b", Period: "daily", LimitUsd: 1, WebhookUrl: "ftp://example.com"},
		"name":      {Name: "Bad Name", Period: "daily", LimitUsd: 1},
	} {
		if _, err := server.SetBudgetAlert(ctx, &gogiv1.SetBudgetAlertRequest{Alert: alert}); status.Code(err) != codes.InvalidArgument {
			t.Errorf("%s: expected InvalidArgument, got %v", name, err)
		}
	}

	listed, _ := server.ListBudgetAlerts(ctx, &gogiv1.ListBudgetAlertsRequest{})
	deleted, _ := server.DeleteBudgetAlert(ctx, &gogiv1.DeleteBudgetAlertRequest{Name: "tutor-daily"})
	if len(listed.Alerts) != 1 || !deleted.Deleted {
		t.Errorf("wrong list or deletion")
	}
	_, err = server.GetBudgetStatus(ctx, &gogiv1.GetBudgetStatusRequest{Name: "tutor-daily"})
	requireCode(t, err, codes.NotFound)
}

func TestServiceHealthAndRetention(t *testing.T) {
	server := newTestServer(t)
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		recordTutorTrace(t, server, fmt.Sprintf("trace-%d", i), 50*time.Minute+time.Duration(i)*time.Second,
			"claude-haiku-4-5", i < 3)
	}

	health, err := server.GetServiceHealth(ctx, &gogiv1.ServiceHealthRequest{})
	if err != nil {
		t.Fatal(err)
	}
	statuses := map[string]string{}
	for _, service := range health.Services {
		statuses[service.Service] = service.Status
	}
	// 3 of 10 guardrails spans failed
	if statuses["guardrails"] != "unhealthy" || statuses["llms"] != "healthy" || statuses["tutor-app"] != "healthy" {
		t.Errorf("wrong statuses %v", statuses)
	}
	quiet, _ := server.GetServiceHealth(ctx, &gogiv1.ServiceHealthRequest{Service: "guardrails", WindowMs: 60_000})
	if quiet.Services[0].Status != "unknown" || quiet.Services[0].LastSeen == 0 {
		t.Errorf("no span in the last minute: unknown, with when it was last seen, got %+v", quiet.Services)
	}
	never, _ := server.GetServiceHealth(ctx, &gogiv1.ServiceHealthRequest{Service: "data"})
	if never.Services[0].Status != "unknown" {
		t.Errorf("a service never seen is unknown, got %+v", never.Services)
	}

	// everything is older than a 1 minute retention
	server.config.TraceRetention, server.config.LogRetention, server.config.MetricRetention = time.Minute, time.Minute, time.Minute
	server.ApplyRetention(ctx)
	if left, _ := server.QueryTraces(ctx, &gogiv1.QueryTracesRequest{}); len(left.Traces) != 0 {
		t.Errorf("old traces should be deleted, %d left", len(left.Traces))
	}
}
