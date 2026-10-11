package impl

import (
	"context"
	"errors"
	"fmt"
	gogiv1 "gogi/gogi/gogi/v1"
	"gogi/gogi/llm/pricing"
	"gogi/gogi/storage/postgres"
	"gogi/gogi/utils"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ObservabilityServer implements the observability service.
// The server provides the following endpoints
//   - IngestLogs, RecordMetrics, RecordSpan, RecordGeneration and RecordScore take telemetry
//     in batches; every item is checked on its own, so one bad item does not lose the batch
//   - GetTrace, QueryTraces, QueryGenerations, QueryScores, QueryLogs and QueryMetrics query it
//   - GetCostReport, SetBudgetAlert, GetBudgetStatus, ListBudgetAlerts and DeleteBudgetAlert
//     report and watch spending
//   - GetServiceHealth reports the health of the platform's services from their spans

const (
	maxBatch          = 1000
	maxTextLength     = 64 * 1024
	defaultQueryLimit = 100
	maxQueryLimit     = 1000
	defaultTraceLimit = 50
	maxTraceLimit     = 500
)

var idPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
var budgetNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)

// Store stores the telemetry; the Postgres repository is one
type Store interface {
	InsertSpans(ctx context.Context, spans []*gogiv1.Span) error
	InsertGenerations(ctx context.Context, generations []postgres.PricedGeneration) error
	GetTrace(ctx context.Context, traceID string) (*gogiv1.Trace, error)
	QueryTraces(ctx context.Context, req *gogiv1.QueryTracesRequest, limit int) ([]*gogiv1.Trace, string, error)
	QueryGenerations(ctx context.Context, req *gogiv1.QueryGenerationsRequest, limit int) ([]*gogiv1.Generation, string, error)
	InsertScores(ctx context.Context, scores []*gogiv1.Score) error
	QueryScores(ctx context.Context, req *gogiv1.QueryScoresRequest) ([]*gogiv1.Score, string, error)
	ScoreSummaries(ctx context.Context, req *gogiv1.QueryScoresRequest) ([]*gogiv1.ScoreSummary, error)
	InsertLogs(ctx context.Context, events []*gogiv1.LogEvent) error
	QueryLogs(ctx context.Context, req *gogiv1.QueryLogsRequest, limit int) ([]*gogiv1.LogEvent, string, error)
	InsertMetricPoints(ctx context.Context, points []*gogiv1.MetricPoint) error
	QueryMetrics(ctx context.Context, req *gogiv1.QueryMetricsRequest, aggregation string) ([]*gogiv1.MetricSeries, error)
	CostReport(ctx context.Context, req *gogiv1.CostReportRequest) (*gogiv1.CostReport, error)
	Spend(ctx context.Context, filters map[string]string, from, to time.Time) (float64, error)
	UpsertBudget(ctx context.Context, alert *gogiv1.BudgetAlert) (*gogiv1.BudgetAlert, error)
	GetBudget(ctx context.Context, name string) (*gogiv1.BudgetAlert, error)
	ListBudgets(ctx context.Context) ([]*gogiv1.BudgetAlert, error)
	DeleteBudget(ctx context.Context, name string) (bool, error)
	MarkNotified(ctx context.Context, name string, periodStart time.Time, threshold float64) (bool, error)
	UnmarkNotified(ctx context.Context, name string, periodStart time.Time, threshold float64) error
	ServiceActivity(ctx context.Context, service string, since time.Time) ([]postgres.ServiceActivity, error)
	DeleteOlderThan(ctx context.Context, logs, metrics, traces time.Time) (map[string]int64, error)
}

type ObservabilityServer struct {
	gogiv1.UnimplementedObservabilityServiceServer
	store  Store
	prices *pricing.Table
	config Config
	now    func() time.Time
}

func NewObservabilityServer(store Store, prices *pricing.Table, config Config) *ObservabilityServer {
	return &ObservabilityServer{store: store, prices: prices, config: config, now: time.Now}
}

// storeError maps a store error to a gRPC status error
func storeError(err error, action string) error {
	switch {
	case errors.Is(err, postgres.ErrTraceNotFound), errors.Is(err, postgres.ErrBudgetNotFound):
		return status.Errorf(codes.NotFound, "%v", err)
	case errors.Is(err, postgres.ErrInvalidCursor):
		return status.Errorf(codes.InvalidArgument, "%v", err)
	}
	return status.Errorf(codes.Internal, "failed to %s: %v", action, err)
}

func checkBatch(count int) error {
	if count > maxBatch {
		return status.Errorf(codes.InvalidArgument, "at most %d items can be sent at once", maxBatch)
	}
	return nil
}

func truncate(text string) string {
	if len(text) <= maxTextLength {
		return text
	}
	return text[:maxTextLength] + "…[truncated]"
}

func limit(requested int32, byDefault, most int) (int, error) {
	switch {
	case requested < 0:
		return 0, status.Error(codes.InvalidArgument, "limit must not be negative")
	case requested == 0:
		return byDefault, nil
	}
	return min(int(requested), most), nil
}

// checkSpan checks a span and normalizes it, returning what is wrong with it
func checkSpan(span *gogiv1.Span) string {
	switch {
	case span == nil:
		return "span is missing"
	case !idPattern.MatchString(span.GetTraceId()):
		return "trace_id must be 1 to 64 letters, digits, '_' or '-'"
	case !idPattern.MatchString(span.GetSpanId()):
		return "span_id must be 1 to 64 letters, digits, '_' or '-'"
	case span.GetParentSpanId() != "" && !idPattern.MatchString(span.GetParentSpanId()):
		return "parent_span_id is malformed"
	case span.GetParentSpanId() == span.GetSpanId():
		return "a span cannot be its own parent"
	case span.GetOperation() == "":
		return "operation is required"
	case span.GetStartTime() <= 0 || span.GetEndTime() < span.GetStartTime():
		return "start_time is required, and end_time must not be before it"
	}
	if span.GetService() == "" {
		span.Service = "unknown"
	}
	span.Input, span.Output = truncate(span.GetInput()), truncate(span.GetOutput())
	return ""
}

func (s *ObservabilityServer) RecordSpan(ctx context.Context, req *gogiv1.RecordSpanRequest) (*gogiv1.RecordSpanResponse, error) {
	if err := checkBatch(len(req.GetSpans())); err != nil {
		return nil, err
	}
	accepted := make([]*gogiv1.Span, 0, len(req.GetSpans()))
	rejected := make([]string, 0)
	for i, span := range req.GetSpans() {
		if problem := checkSpan(span); problem != "" {
			rejected = append(rejected, fmt.Sprintf("span %d: %s", i, problem))
			continue
		}
		accepted = append(accepted, span)
	}
	if len(accepted) > 0 {
		if err := s.store.InsertSpans(ctx, accepted); err != nil {
			return nil, storeError(err, "record the spans")
		}
	}
	return &gogiv1.RecordSpanResponse{Accepted: int32(len(accepted)), Rejected: rejected}, nil
}

// price settles a generation's cost: the reporter's, or the price table's
func (s *ObservabilityServer) price(generation *gogiv1.Generation) postgres.PricedGeneration {
	priced := postgres.PricedGeneration{Generation: generation, Priced: true}
	if generation.CostUsd != nil {
		priced.CostUSD = generation.GetCostUsd()
		return priced
	}
	cost, ok := s.prices.Cost(generation.GetProvider(), generation.GetModel(), int64(generation.GetPromptTokens()),
		int64(generation.GetCompletionTokens()))
	priced.CostUSD, priced.Estimated, priced.Priced = cost, ok, ok
	return priced
}

func (s *ObservabilityServer) RecordGeneration(ctx context.Context, req *gogiv1.RecordGenerationRequest) (*gogiv1.RecordGenerationResponse, error) {
	if err := checkBatch(len(req.GetGenerations())); err != nil {
		return nil, err
	}
	accepted := make([]postgres.PricedGeneration, 0, len(req.GetGenerations()))
	rejected := make([]string, 0)
	for i, generation := range req.GetGenerations() {
		problem := checkSpan(generation.GetSpan())
		switch {
		case problem != "":
		case generation.GetModel() == "":
			problem = "model is required"
		case generation.GetPromptTokens() < 0 || generation.GetCompletionTokens() < 0:
			problem = "token counts must not be negative"
		case generation.CostUsd != nil && (generation.GetCostUsd() < 0 || math.IsNaN(generation.GetCostUsd()) ||
			math.IsInf(generation.GetCostUsd(), 0)):
			problem = "cost_usd must be a non-negative number"
		}
		if problem != "" {
			rejected = append(rejected, fmt.Sprintf("generation %d: %s", i, problem))
			continue
		}
		accepted = append(accepted, s.price(generation))
	}
	if len(accepted) > 0 {
		if err := s.store.InsertGenerations(ctx, accepted); err != nil {
			return nil, storeError(err, "record the generations")
		}
	}
	return &gogiv1.RecordGenerationResponse{Accepted: int32(len(accepted)), Rejected: rejected}, nil
}

func (s *ObservabilityServer) GetTrace(ctx context.Context, req *gogiv1.GetTraceRequest) (*gogiv1.Trace, error) {
	if !idPattern.MatchString(req.GetTraceId()) {
		return nil, status.Error(codes.InvalidArgument, "trace_id is required")
	}
	trace, err := s.store.GetTrace(ctx, req.GetTraceId())
	if err != nil {
		return nil, storeError(err, "get the trace")
	}
	return trace, nil
}

func (s *ObservabilityServer) QueryTraces(ctx context.Context, req *gogiv1.QueryTracesRequest) (*gogiv1.QueryTracesResponse, error) {
	n, err := limit(req.GetLimit(), defaultTraceLimit, maxTraceLimit)
	if err != nil {
		return nil, err
	}
	if order := req.GetOrderBy(); order != "" && order != "recent" && order != "duration" && order != "cost" {
		return nil, status.Errorf(codes.InvalidArgument, "order_by %q must be recent, duration or cost", order)
	}
	if (req.MinScore != nil || req.MaxScore != nil) && req.GetScoreName() == "" {
		return nil, status.Error(codes.InvalidArgument, "min_score and max_score need a score_name")
	}
	traces, next, err := s.store.QueryTraces(ctx, req, n)
	if err != nil {
		return nil, storeError(err, "query the traces")
	}
	return &gogiv1.QueryTracesResponse{Traces: traces, NextCursor: next}, nil
}

func (s *ObservabilityServer) QueryGenerations(ctx context.Context, req *gogiv1.QueryGenerationsRequest) (*gogiv1.QueryGenerationsResponse, error) {
	n, err := limit(req.GetLimit(), defaultQueryLimit, maxQueryLimit)
	if err != nil {
		return nil, err
	}
	generations, next, err := s.store.QueryGenerations(ctx, req, n)
	if err != nil {
		return nil, storeError(err, "query the generations")
	}
	return &gogiv1.QueryGenerationsResponse{Generations: generations, NextCursor: next}, nil
}

func (s *ObservabilityServer) RecordScore(ctx context.Context, req *gogiv1.RecordScoreRequest) (*gogiv1.RecordScoreResponse, error) {
	if err := checkBatch(len(req.GetScores())); err != nil {
		return nil, err
	}
	accepted := make([]*gogiv1.Score, 0, len(req.GetScores()))
	rejected := make([]string, 0)
	ids := make([]string, 0, len(req.GetScores()))
	for i, score := range req.GetScores() {
		problem := ""
		switch value := score.GetValue().(type) {
		case nil:
			problem = "a numeric, categorical or boolean value is required"
		case *gogiv1.Score_NumericValue:
			if math.IsNaN(value.NumericValue) || math.IsInf(value.NumericValue, 0) {
				problem = "numeric_value must be a number"
			}
		case *gogiv1.Score_CategoricalValue:
			if value.CategoricalValue == "" {
				problem = "categorical_value must not be empty"
			}
		}
		switch {
		case problem != "":
		case !idPattern.MatchString(score.GetTraceId()):
			problem = "trace_id is required"
		case score.GetName() == "":
			problem = "name is required"
		case score.GetSource() == gogiv1.ScoreSource_SCORE_SOURCE_UNSPECIFIED:
			problem = "source is required, so automated scores can be calibrated against human ones"
		case score.GetScoreId() != "" && !idPattern.MatchString(score.GetScoreId()):
			problem = "score_id is malformed"
		}
		if problem != "" {
			rejected = append(rejected, fmt.Sprintf("score %d: %s", i, problem))
			continue
		}
		if score.GetScoreId() == "" {
			score.ScoreId = utils.NewUUIDString()
		}
		if score.GetTimestamp() == 0 {
			score.Timestamp = s.now().UnixMilli()
		}
		score.Comment = truncate(score.GetComment())
		accepted = append(accepted, score)
		ids = append(ids, score.GetScoreId())
	}
	if len(accepted) > 0 {
		if err := s.store.InsertScores(ctx, accepted); err != nil {
			return nil, storeError(err, "record the scores")
		}
	}
	return &gogiv1.RecordScoreResponse{ScoreIds: ids, Rejected: rejected}, nil
}

func (s *ObservabilityServer) QueryScores(ctx context.Context, req *gogiv1.QueryScoresRequest) (*gogiv1.QueryScoresResponse, error) {
	n, err := limit(req.GetLimit(), defaultQueryLimit, maxQueryLimit)
	if err != nil {
		return nil, err
	}
	req.Limit = int32(n)
	scores, next, err := s.store.QueryScores(ctx, req)
	if err != nil {
		return nil, storeError(err, "query the scores")
	}
	summaries, err := s.store.ScoreSummaries(ctx, req)
	if err != nil {
		return nil, storeError(err, "summarize the scores")
	}
	return &gogiv1.QueryScoresResponse{Scores: scores, NextCursor: next, Summaries: summaries}, nil
}

func checkLabels(labels map[string]string) string {
	for key := range labels {
		if !postgres.DimensionPattern.MatchString(key) {
			return fmt.Sprintf("label %q must start with a letter and be up to 64 letters, digits, '_', '.' or '-'", key)
		}
	}
	return ""
}

func (s *ObservabilityServer) IngestLogs(ctx context.Context, req *gogiv1.IngestLogsRequest) (*gogiv1.IngestLogsResponse, error) {
	if err := checkBatch(len(req.GetEvents())); err != nil {
		return nil, err
	}
	accepted := make([]*gogiv1.LogEvent, 0, len(req.GetEvents()))
	rejected := make([]string, 0)
	for i, event := range req.GetEvents() {
		problem := ""
		switch {
		case event.GetEventId() != "" && !idPattern.MatchString(event.GetEventId()):
			problem = "event_id is malformed"
		case event.GetTraceId() != "" && !idPattern.MatchString(event.GetTraceId()):
			problem = "trace_id is malformed"
		case event.GetMessage() == "" && event.GetEventType() == "":
			problem = "a message or an event_type is required"
		case event.GetSeverity() < 0 || event.GetSeverity() > gogiv1.Severity_SEVERITY_CRITICAL:
			problem = "unknown severity"
		}
		if problem != "" {
			rejected = append(rejected, fmt.Sprintf("event %d: %s", i, problem))
			continue
		}
		if event.GetEventId() == "" {
			event.EventId = utils.NewUUIDString()
		}
		if event.GetTimestamp() == 0 {
			event.Timestamp = s.now().UnixMilli()
		}
		if event.GetSeverity() == gogiv1.Severity_SEVERITY_UNSPECIFIED {
			event.Severity = gogiv1.Severity_SEVERITY_INFO
		}
		event.Message = truncate(event.GetMessage())
		accepted = append(accepted, event)
	}
	if len(accepted) > 0 {
		if err := s.store.InsertLogs(ctx, accepted); err != nil {
			return nil, storeError(err, "ingest the log events")
		}
	}
	return &gogiv1.IngestLogsResponse{Accepted: int32(len(accepted)), Rejected: rejected}, nil
}

func (s *ObservabilityServer) QueryLogs(ctx context.Context, req *gogiv1.QueryLogsRequest) (*gogiv1.QueryLogsResponse, error) {
	n, err := limit(req.GetLimit(), defaultQueryLimit, maxQueryLimit)
	if err != nil {
		return nil, err
	}
	events, next, err := s.store.QueryLogs(ctx, req, n)
	if err != nil {
		return nil, storeError(err, "query the log events")
	}
	return &gogiv1.QueryLogsResponse{Events: events, NextCursor: next}, nil
}

var metricNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_.]{0,127}$`)

// metricTypeOf infers a metric's type from its name: counters end in _total
func metricTypeOf(name string) gogiv1.MetricType {
	if strings.HasSuffix(name, "_total") {
		return gogiv1.MetricType_METRIC_TYPE_COUNTER
	}
	return gogiv1.MetricType_METRIC_TYPE_HISTOGRAM
}

func (s *ObservabilityServer) RecordMetrics(ctx context.Context, req *gogiv1.RecordMetricsRequest) (*gogiv1.RecordMetricsResponse, error) {
	if err := checkBatch(len(req.GetPoints())); err != nil {
		return nil, err
	}
	accepted := make([]*gogiv1.MetricPoint, 0, len(req.GetPoints()))
	rejected := make([]string, 0)
	for i, point := range req.GetPoints() {
		problem := checkLabels(point.GetLabels())
		switch {
		case problem != "":
		case !metricNamePattern.MatchString(point.GetName()):
			problem = "name must be lowercase letters, digits, '_' and '.', e.g. gogi.models.request_duration_ms"
		case math.IsNaN(point.GetValue()) || math.IsInf(point.GetValue(), 0):
			problem = "value must be a number"
		}
		if problem != "" {
			rejected = append(rejected, fmt.Sprintf("point %d: %s", i, problem))
			continue
		}
		if point.GetType() == gogiv1.MetricType_METRIC_TYPE_UNSPECIFIED {
			point.Type = metricTypeOf(point.GetName())
		}
		if point.GetTimestamp() == 0 {
			point.Timestamp = s.now().UnixMilli()
		}
		accepted = append(accepted, point)
	}
	if len(accepted) > 0 {
		if err := s.store.InsertMetricPoints(ctx, accepted); err != nil {
			return nil, storeError(err, "record the metric points")
		}
	}
	return &gogiv1.RecordMetricsResponse{Accepted: int32(len(accepted)), Rejected: rejected}, nil
}

func (s *ObservabilityServer) QueryMetrics(ctx context.Context, req *gogiv1.QueryMetricsRequest) (*gogiv1.QueryMetricsResponse, error) {
	if !metricNamePattern.MatchString(req.GetName()) {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	if problem := checkLabels(req.GetLabelFilters()); problem != "" {
		return nil, status.Error(codes.InvalidArgument, problem)
	}
	for _, label := range req.GetGroupBy() {
		if !postgres.DimensionPattern.MatchString(label) {
			return nil, status.Errorf(codes.InvalidArgument, "group_by label %q is malformed", label)
		}
	}
	if req.GetStepMs() < 0 {
		return nil, status.Error(codes.InvalidArgument, "step_ms must not be negative")
	}
	if req.GetStartTime() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "start_time is required")
	}
	if req.GetStepMs() > 0 && req.GetStepMs() < 1000 {
		return nil, status.Error(codes.InvalidArgument, "step_ms must be at least 1000")
	}
	end := req.GetEndTime()
	if end == 0 {
		end = s.now().UnixMilli()
	}
	if req.GetStepMs() > 0 && (end-req.GetStartTime())/req.GetStepMs() > 10_000 {
		return nil, status.Error(codes.InvalidArgument, "the range holds more than 10000 steps; use a larger step_ms")
	}

	aggregation := strings.ToLower(req.GetAggregation())
	if aggregation == "" {
		aggregation = "avg"
		if strings.HasSuffix(req.GetName(), "_total") {
			aggregation = "sum"
		}
	}
	if _, ok := postgres.Aggregations[aggregation]; !ok {
		return nil, status.Errorf(codes.InvalidArgument,
			"aggregation %q must be sum, count, avg, min, max, p50, p90, p95 or p99", req.GetAggregation())
	}
	series, err := s.store.QueryMetrics(ctx, req, aggregation)
	if err != nil {
		return nil, storeError(err, "query the metric")
	}
	return &gogiv1.QueryMetricsResponse{Series: series, Aggregation: aggregation}, nil
}

func checkDimensions(dimensions []string, filters map[string]string) error {
	for _, dimension := range dimensions {
		if !postgres.DimensionPattern.MatchString(dimension) {
			return status.Errorf(codes.InvalidArgument, "dimension %q is malformed", dimension)
		}
	}
	for dimension := range filters {
		if !postgres.DimensionPattern.MatchString(dimension) {
			return status.Errorf(codes.InvalidArgument, "filter %q is malformed", dimension)
		}
	}
	return nil
}

func (s *ObservabilityServer) GetCostReport(ctx context.Context, req *gogiv1.CostReportRequest) (*gogiv1.CostReport, error) {
	if err := checkDimensions(req.GetGroupBy(), req.GetFilters()); err != nil {
		return nil, err
	}
	if !postgres.ValidGranularity(req.GetGranularity()) {
		return nil, status.Errorf(codes.InvalidArgument, "granularity %q must be none, hourly, daily, weekly or monthly",
			req.GetGranularity())
	}
	if req.GetStartTime() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "start_time is required")
	}
	report, err := s.store.CostReport(ctx, req)
	if err != nil {
		return nil, storeError(err, "report the cost")
	}
	return report, nil
}

// --- budgets ---

var defaultThresholds = []float64{0.7, 0.9, 1.0}

func (s *ObservabilityServer) SetBudgetAlert(ctx context.Context, req *gogiv1.SetBudgetAlertRequest) (*gogiv1.SetBudgetAlertResponse, error) {
	alert := req.GetAlert()
	switch {
	case alert == nil:
		return nil, status.Error(codes.InvalidArgument, "alert is required")
	case !budgetNamePattern.MatchString(alert.GetName()):
		return nil, status.Error(codes.InvalidArgument, "name must be lowercase letters, digits, '.', '_' or '-'")
	case alert.GetPeriod() != "daily" && alert.GetPeriod() != "weekly" && alert.GetPeriod() != "monthly":
		return nil, status.Errorf(codes.InvalidArgument, "period %q must be daily, weekly or monthly", alert.GetPeriod())
	case !(alert.GetLimitUsd() > 0) || math.IsInf(alert.GetLimitUsd(), 0):
		return nil, status.Error(codes.InvalidArgument, "limit_usd must be positive")
	case alert.GetWebhookUrl() != "" && !strings.HasPrefix(alert.GetWebhookUrl(), "https://") &&
		!strings.HasPrefix(alert.GetWebhookUrl(), "http://"):
		return nil, status.Error(codes.InvalidArgument, "webhook_url must be an http or https URL")
	}
	if err := checkDimensions(nil, alert.GetFilters()); err != nil {
		return nil, err
	}

	thresholds := alert.GetThresholds()
	if len(thresholds) == 0 {
		thresholds = defaultThresholds
	}
	seen := map[float64]bool{}
	unique := make([]float64, 0, len(thresholds))
	for _, threshold := range thresholds {
		if !(threshold > 0) || threshold > 10 {
			return nil, status.Errorf(codes.InvalidArgument, "threshold %v must be above 0 and at most 10", threshold)
		}
		if !seen[threshold] {
			seen[threshold] = true
			unique = append(unique, threshold)
		}
	}
	sort.Float64s(unique)
	alert.Thresholds = unique

	stored, err := s.store.UpsertBudget(ctx, alert)
	if err != nil {
		return nil, storeError(err, "set the budget alert")
	}
	return &gogiv1.SetBudgetAlertResponse{Alert: stored}, nil
}

// Period returns the bounds of the period, in UTC, that contains now
func Period(period string, now time.Time) (time.Time, time.Time) {
	now = now.UTC()
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	switch period {
	case "weekly":
		// weeks start on Monday
		start := day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7))
		return start, start.AddDate(0, 0, 7)
	case "monthly":
		start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		return start, start.AddDate(0, 1, 0)
	}
	return day, day.AddDate(0, 0, 1)
}

// minimumElapsed keeps projections sane at the start of a period: an hour's spend is the
// least a projection extrapolates from
const minimumElapsed = time.Hour

// BudgetStatusAt computes a budget's status from its spend in the period so far
func BudgetStatusAt(alert *gogiv1.BudgetAlert, spent float64, now time.Time) *gogiv1.BudgetStatus {
	start, end := Period(alert.GetPeriod(), now)
	elapsed := max(now.Sub(start), minimumElapsed)
	total := end.Sub(start)
	projected := spent
	if elapsed < total {
		projected = spent / elapsed.Hours() * total.Hours()
	}

	result := &gogiv1.BudgetStatus{Alert: alert, PeriodStart: start.UnixMilli(), PeriodEnd: end.UnixMilli(),
		SpentUsd: spent, ProjectedUsd: projected, State: "ok"}
	result.Fraction = spent / alert.GetLimitUsd()
	result.ProjectedFraction = projected / alert.GetLimitUsd()
	for _, threshold := range alert.GetThresholds() {
		if result.Fraction >= threshold {
			result.CrossedThresholds = append(result.CrossedThresholds, threshold)
			result.State = "warning"
		}
	}
	if result.Fraction >= 1 {
		result.State = "exceeded"
	}
	return result
}

func (s *ObservabilityServer) budgetStatus(ctx context.Context, alert *gogiv1.BudgetAlert) (*gogiv1.BudgetStatus, error) {
	now := s.now()
	start, end := Period(alert.GetPeriod(), now)
	spent, err := s.store.Spend(ctx, alert.GetFilters(), start, end)
	if err != nil {
		return nil, err
	}
	return BudgetStatusAt(alert, spent, now), nil
}

func (s *ObservabilityServer) GetBudgetStatus(ctx context.Context, req *gogiv1.GetBudgetStatusRequest) (*gogiv1.BudgetStatus, error) {
	alert, err := s.store.GetBudget(ctx, req.GetName())
	if err != nil {
		return nil, storeError(err, "get the budget alert")
	}
	result, err := s.budgetStatus(ctx, alert)
	if err != nil {
		return nil, storeError(err, "compute the budget's spend")
	}
	return result, nil
}

func (s *ObservabilityServer) ListBudgetAlerts(ctx context.Context, req *gogiv1.ListBudgetAlertsRequest) (*gogiv1.ListBudgetAlertsResponse, error) {
	alerts, err := s.store.ListBudgets(ctx)
	if err != nil {
		return nil, storeError(err, "list the budget alerts")
	}
	return &gogiv1.ListBudgetAlertsResponse{Alerts: alerts}, nil
}

func (s *ObservabilityServer) DeleteBudgetAlert(ctx context.Context, req *gogiv1.DeleteBudgetAlertRequest) (*gogiv1.DeleteBudgetAlertResponse, error) {
	deleted, err := s.store.DeleteBudget(ctx, req.GetName())
	if err != nil {
		return nil, storeError(err, "delete the budget alert")
	}
	return &gogiv1.DeleteBudgetAlertResponse{Deleted: deleted}, nil
}

// --- health ---

// Health classifies a service from its activity in a window
func Health(activity postgres.ServiceActivity, window time.Duration, config Config) *gogiv1.ServiceHealth {
	health := &gogiv1.ServiceHealth{Service: activity.Service, LastSeen: activity.LastSeen.UnixMilli(),
		SpanCount: activity.Spans, P95DurationMs: activity.P95DurationMs}
	if activity.Spans == 0 {
		health.Status = "unknown"
		health.Reason = fmt.Sprintf("no spans in the last %s", window)
		return health
	}
	health.ErrorRate = float64(activity.Errors) / float64(activity.Spans)
	switch {
	case health.ErrorRate >= config.UnhealthyErrorRate:
		health.Status = "unhealthy"
	case health.ErrorRate >= config.DegradedErrorRate:
		health.Status = "degraded"
	default:
		health.Status = "healthy"
	}
	health.Reason = fmt.Sprintf("%d of %d spans failed in the last %s", activity.Errors, activity.Spans, window)
	return health
}

func (s *ObservabilityServer) GetServiceHealth(ctx context.Context, req *gogiv1.ServiceHealthRequest) (*gogiv1.ServiceHealthResponse, error) {
	if req.GetWindowMs() < 0 {
		return nil, status.Error(codes.InvalidArgument, "window_ms must not be negative")
	}
	window := 15 * time.Minute
	if req.GetWindowMs() > 0 {
		window = time.Duration(req.GetWindowMs()) * time.Millisecond
	}
	activity, err := s.store.ServiceActivity(ctx, req.GetService(), s.now().Add(-window))
	if err != nil {
		return nil, storeError(err, "get the services' activity")
	}

	response := &gogiv1.ServiceHealthResponse{}
	for _, a := range activity {
		response.Services = append(response.Services, Health(a, window, s.config))
	}
	if req.GetService() != "" && len(response.Services) == 0 {
		response.Services = append(response.Services, &gogiv1.ServiceHealth{Service: req.GetService(),
			Status: "unknown", Reason: "no span of the service was ever recorded"})
	}
	return response, nil
}
