package postgres

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	gogiv1 "gogi/gogi/gogi/v1"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrTraceNotFound is returned for a trace no span was recorded for
var ErrTraceNotFound = errors.New("trace not found")

// ErrBudgetNotFound is returned for a budget alert that does not exist
var ErrBudgetNotFound = errors.New("budget alert not found")

// ErrInvalidCursor is returned for a cursor this repository did not issue
var ErrInvalidCursor = errors.New("invalid cursor")

// DimensionPattern is what a group-by or filter dimension, or a label, may look like
var DimensionPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,63}$`)

// generationColumns are the dimensions of generations with a column of their own; any
// other dimension is an attribute of the generation's span
var generationColumns = map[string]string{
	"model": "model", "provider": "provider", "workflow_id": "workflow_id", "user_id": "user_id",
	"session_id": "session_id", "service": "service",
}

// GogiObservabilityRepository stores the telemetry of the platform
type GogiObservabilityRepository struct {
	pool *pgxpool.Pool
}

func NewGogiObservabilityRepository(pool *pgxpool.Pool) *GogiObservabilityRepository {
	return &GogiObservabilityRepository{pool: pool}
}

func millis(t int64) time.Time {
	return time.UnixMilli(t).UTC()
}

func jsonOf(value any) string {
	data, err := json.Marshal(value)
	if err != nil || string(data) == "null" {
		return "{}"
	}
	return string(data)
}

func spanStatus(status gogiv1.SpanStatus) string {
	if status == gogiv1.SpanStatus_SPAN_STATUS_ERROR {
		return "error"
	}
	return "ok"
}

func toSpanStatus(status string) gogiv1.SpanStatus {
	if status == "error" {
		return gogiv1.SpanStatus_SPAN_STATUS_ERROR
	}
	return gogiv1.SpanStatus_SPAN_STATUS_OK
}

// traceUpsert adds a newly inserted span to its trace: the trace spans the earliest start
// to the latest end of its spans, takes the root span's input and output, and the first
// non-empty session, workflow and user. $1 to $15 are the trace's values from the span
const traceUpsert = `
INSERT INTO obs_traces (trace_id, session_id, workflow_id, user_id, input, output, tags, start_time, end_time,
    span_count, error_count, services, total_cost_usd, total_tokens)
SELECT $1, $2, $3, $4, $5, $6, $7::text[], $8, $9, 1, $10, ARRAY[$11::text], $12, $13
WHERE EXISTS (SELECT 1 FROM inserted)
ON CONFLICT (trace_id) DO UPDATE SET
    session_id = CASE WHEN obs_traces.session_id = '' THEN EXCLUDED.session_id ELSE obs_traces.session_id END,
    workflow_id = CASE WHEN obs_traces.workflow_id = '' THEN EXCLUDED.workflow_id ELSE obs_traces.workflow_id END,
    user_id = CASE WHEN obs_traces.user_id = '' THEN EXCLUDED.user_id ELSE obs_traces.user_id END,
    input = CASE WHEN ($14 AND EXCLUDED.input <> '') OR obs_traces.input = '' THEN EXCLUDED.input ELSE obs_traces.input END,
    output = CASE WHEN ($14 AND EXCLUDED.output <> '') OR obs_traces.output = '' THEN EXCLUDED.output ELSE obs_traces.output END,
    tags = ARRAY(SELECT DISTINCT unnest(obs_traces.tags || EXCLUDED.tags) ORDER BY 1),
    start_time = LEAST(obs_traces.start_time, EXCLUDED.start_time),
    end_time = GREATEST(obs_traces.end_time, EXCLUDED.end_time),
    span_count = obs_traces.span_count + 1,
    error_count = obs_traces.error_count + EXCLUDED.error_count,
    services = CASE WHEN $11::text = ANY(obs_traces.services) THEN obs_traces.services
        ELSE obs_traces.services || ARRAY[$11::text] END,
    total_cost_usd = obs_traces.total_cost_usd + EXCLUDED.total_cost_usd,
    total_tokens = obs_traces.total_tokens + EXCLUDED.total_tokens,
    updated_at = NOW()`

const spanInsert = `INSERT INTO obs_spans (trace_id, span_id, parent_span_id, kind, service, operation, start_time,
    end_time, duration_ms, status, error_message, attributes, numeric_attributes, events)
    VALUES ($1, $23, $24, $15, $11, $16, $8, $9, $17, $18, $19, $20::jsonb, $21::jsonb, $22::jsonb)
    ON CONFLICT (trace_id, span_id) DO NOTHING RETURNING 1`

// spanArgs returns the arguments of a span's statements: $1 to $14 are the trace's values
// (traceUpsert), $15 to $24 the span's own (spanInsert); a generation's follow from $25
func spanArgs(span *gogiv1.Span, kind string, costUSD float64, tokens int64) []any {
	errors := 0
	if span.GetStatus() == gogiv1.SpanStatus_SPAN_STATUS_ERROR {
		errors = 1
	}
	tags := span.GetTags()
	if tags == nil {
		tags = []string{}
	}
	events := make([]map[string]any, 0, len(span.GetEvents()))
	for _, event := range span.GetEvents() {
		events = append(events, map[string]any{"name": event.GetName(), "timestamp": event.GetTimestamp(),
			"attributes": event.GetAttributes()})
	}
	eventsJSON, _ := json.Marshal(events)
	return []any{
		span.GetTraceId(), span.GetSessionId(), span.GetWorkflowId(), span.GetUserId(), span.GetInput(),
		span.GetOutput(), tags, millis(span.GetStartTime()), millis(span.GetEndTime()), errors, span.GetService(),
		costUSD, tokens, span.GetParentSpanId() == "",
		kind, span.GetOperation(), float64(span.GetEndTime() - span.GetStartTime()), spanStatus(span.GetStatus()),
		span.GetErrorMessage(), jsonOf(span.GetAttributes()), jsonOf(span.GetNumericAttributes()), string(eventsJSON),
		span.GetSpanId(), span.GetParentSpanId(),
	}
}

// InsertSpans stores spans and adds them to their traces, in one transaction. A span sent
// again, e.g. by a client retrying a batch, is ignored
func (r *GogiObservabilityRepository) InsertSpans(ctx context.Context, spans []*gogiv1.Span) error {
	batch := &pgx.Batch{}
	for _, span := range spans {
		batch.Queue(`WITH inserted AS (`+spanInsert+`) `+traceUpsert, spanArgs(span, "span", 0, 0)...)
		queueBackfill(batch, span)
	}
	return r.sendBatch(ctx, batch)
}

// queueBackfill gives the generations of a span's trace the session, workflow and user they
// lack: a root span usually ends, and so is recorded, after the generations it contains
func queueBackfill(batch *pgx.Batch, span *gogiv1.Span) {
	if span.GetSessionId() == "" && span.GetWorkflowId() == "" && span.GetUserId() == "" {
		return
	}
	batch.Queue(`UPDATE obs_generations SET
			session_id = CASE WHEN session_id = '' THEN $2 ELSE session_id END,
			workflow_id = CASE WHEN workflow_id = '' THEN $3 ELSE workflow_id END,
			user_id = CASE WHEN user_id = '' THEN $4 ELSE user_id END
		WHERE trace_id = $1 AND ((session_id = '' AND $2 <> '') OR (workflow_id = '' AND $3 <> '') OR (user_id = '' AND $4 <> ''))`,
		span.GetTraceId(), span.GetSessionId(), span.GetWorkflowId(), span.GetUserId())
}

// PricedGeneration is a generation with its cost settled
type PricedGeneration struct {
	Generation *gogiv1.Generation
	CostUSD    float64
	Estimated  bool
	Priced     bool
}

// InsertGenerations stores generations, as spans with their LLM fields, and adds them to
// their traces with their cost and tokens
func (r *GogiObservabilityRepository) InsertGenerations(ctx context.Context, generations []PricedGeneration) error {
	batch := &pgx.Batch{}
	for _, priced := range generations {
		g := priced.Generation
		span := g.GetSpan()
		tokens := int64(g.GetPromptTokens()) + int64(g.GetCompletionTokens())
		args := spanArgs(span, "generation", priced.CostUSD, tokens)
		args = append(args, g.GetModel(), g.GetProvider(), g.GetRequestedModel(), g.GetPromptTokens(),
			g.GetCompletionTokens(), priced.CostUSD, priced.Estimated, priced.Priced, g.GetCacheHit(),
			g.GetFallbackUsed(), g.GetTimeToFirstTokenMs())
		batch.Queue(`WITH inserted AS (`+spanInsert+`),
			generation AS (INSERT INTO obs_generations (trace_id, span_id, service, model, provider, requested_model,
				prompt_tokens, completion_tokens, cost_usd, cost_estimated, priced, cache_hit, fallback_used,
				time_to_first_token_ms, session_id, workflow_id, user_id, attributes, start_time)
				SELECT $1, $23, $11, $25, $26, $27, $28, $29, $30, $31, $32, $33, $34, $35,
					COALESCE(NULLIF($2, ''), t.session_id, ''), COALESCE(NULLIF($3, ''), t.workflow_id, ''),
					COALESCE(NULLIF($4, ''), t.user_id, ''), $20::jsonb, $8
				FROM (SELECT 1) AS one LEFT JOIN obs_traces t ON t.trace_id = $1
				WHERE EXISTS (SELECT 1 FROM inserted))
			`+traceUpsert, args...)
	}
	return r.sendBatch(ctx, batch)
}

func (r *GogiObservabilityRepository) sendBatch(ctx context.Context, batch *pgx.Batch) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := tx.SendBatch(ctx, batch).Close(); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// --- cursors ---

// encodeCursor encodes the position after the last row of a page
func encodeCursor(parts ...string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strings.Join(parts, "\x00")))
}

func decodeCursor(cursor string, parts int) ([]string, error) {
	if cursor == "" {
		return nil, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, ErrInvalidCursor
	}
	values := strings.Split(string(data), "\x00")
	if len(values) != parts {
		return nil, ErrInvalidCursor
	}
	return values, nil
}

// query builds the WHERE clause of a query from conditions and their arguments
type query struct {
	conditions []string
	args       []any
}

// add adds a condition; %s in it stands for the next argument's placeholder
func (q *query) add(condition string, args ...any) {
	placeholders := make([]any, 0, len(args))
	for _, arg := range args {
		q.args = append(q.args, arg)
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(q.args)))
	}
	q.conditions = append(q.conditions, fmt.Sprintf(condition, placeholders...))
}

func (q *query) arg(value any) string {
	q.args = append(q.args, value)
	return fmt.Sprintf("$%d", len(q.args))
}

func (q *query) where() string {
	if len(q.conditions) == 0 {
		return "TRUE"
	}
	return strings.Join(q.conditions, " AND ")
}

func (q *query) timeRange(column string, start, end int64) {
	if start > 0 {
		q.add(column+" >= %s", millis(start))
	}
	if end > 0 {
		q.add(column+" < %s", millis(end))
	}
}

// --- traces ---

const traceColumns = `trace_id, session_id, workflow_id, user_id, input, output, tags, start_time, end_time,
	duration_ms, total_cost_usd, total_tokens, span_count, error_count`

func scanTrace(row pgx.Row) (*gogiv1.Trace, error) {
	trace := &gogiv1.Trace{}
	var start, end time.Time
	var tokens int64
	err := row.Scan(&trace.TraceId, &trace.SessionId, &trace.WorkflowId, &trace.UserId, &trace.Input, &trace.Output,
		&trace.Tags, &start, &end, &trace.TotalDurationMs, &trace.TotalCostUsd, &tokens, &trace.SpanCount, &trace.ErrorCount)
	trace.StartTime, trace.EndTime, trace.TotalTokens = start.UnixMilli(), end.UnixMilli(), int32(tokens)
	return trace, err
}

const spanColumns = `s.trace_id, s.span_id, s.parent_span_id, s.service, s.operation, s.start_time, s.end_time,
	s.status, s.error_message, s.attributes, s.numeric_attributes, s.events`

func scanSpan(row pgx.Row, extra ...any) (*gogiv1.Span, error) {
	span := &gogiv1.Span{}
	var start, end time.Time
	var status string
	var attributes, numeric, events []byte
	dest := append([]any{&span.TraceId, &span.SpanId, &span.ParentSpanId, &span.Service, &span.Operation, &start,
		&end, &status, &span.ErrorMessage, &attributes, &numeric, &events}, extra...)
	if err := row.Scan(dest...); err != nil {
		return nil, err
	}
	span.StartTime, span.EndTime, span.Status = start.UnixMilli(), end.UnixMilli(), toSpanStatus(status)
	if err := json.Unmarshal(attributes, &span.Attributes); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(numeric, &span.NumericAttributes); err != nil {
		return nil, err
	}
	var decoded []struct {
		Name       string            `json:"name"`
		Timestamp  int64             `json:"timestamp"`
		Attributes map[string]string `json:"attributes"`
	}
	if err := json.Unmarshal(events, &decoded); err != nil {
		return nil, err
	}
	for _, event := range decoded {
		span.Events = append(span.Events, &gogiv1.SpanEvent{Name: event.Name, Timestamp: event.Timestamp,
			Attributes: event.Attributes})
	}
	return span, nil
}

const generationColumnsSQL = `g.model, g.provider, g.requested_model, g.prompt_tokens, g.completion_tokens, g.cost_usd,
	g.cost_estimated, g.cache_hit, g.fallback_used, g.time_to_first_token_ms, g.session_id, g.workflow_id, g.user_id`

func scanGeneration(row pgx.Row) (*gogiv1.Generation, error) {
	generation := &gogiv1.Generation{}
	var prompt, completion int64
	var cost float64
	var session, workflow, user string
	span, err := scanSpan(row, &generation.Model, &generation.Provider, &generation.RequestedModel, &prompt,
		&completion, &cost, &generation.CostEstimated, &generation.CacheHit, &generation.FallbackUsed,
		&generation.TimeToFirstTokenMs, &session, &workflow, &user)
	if err != nil {
		return nil, err
	}
	span.SessionId, span.WorkflowId, span.UserId = session, workflow, user
	generation.Span, generation.PromptTokens, generation.CompletionTokens = span, int32(prompt), int32(completion)
	generation.CostUsd = &cost
	return generation, nil
}

// GetTrace returns the trace with its spans, generations and scores
func (r *GogiObservabilityRepository) GetTrace(ctx context.Context, traceID string) (*gogiv1.Trace, error) {
	trace, err := scanTrace(r.pool.QueryRow(ctx, `SELECT `+traceColumns+` FROM obs_traces WHERE trace_id = $1`, traceID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w: %s", ErrTraceNotFound, traceID)
	}
	if err != nil {
		return nil, err
	}

	rows, err := r.pool.Query(ctx, `SELECT `+spanColumns+` FROM obs_spans s
		WHERE s.trace_id = $1 AND s.kind = 'span' ORDER BY s.start_time, s.span_id`, traceID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		span, err := scanSpan(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		trace.Spans = append(trace.Spans, span)
	}
	rows.Close()

	rows, err = r.pool.Query(ctx, `SELECT `+spanColumns+`, `+generationColumnsSQL+` FROM obs_spans s
		JOIN obs_generations g ON g.trace_id = s.trace_id AND g.span_id = s.span_id
		WHERE s.trace_id = $1 ORDER BY s.start_time, s.span_id`, traceID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		generation, err := scanGeneration(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		trace.Generations = append(trace.Generations, generation)
	}
	rows.Close()

	scores, _, err := r.QueryScores(ctx, &gogiv1.QueryScoresRequest{TraceId: traceID, Limit: 1000})
	if err != nil {
		return nil, err
	}
	trace.Scores = scores
	return trace, nil
}

// QueryTraces returns a page of traces, without their spans, generations and scores
func (r *GogiObservabilityRepository) QueryTraces(ctx context.Context, req *gogiv1.QueryTracesRequest, limit int) ([]*gogiv1.Trace, string, error) {
	q := &query{}
	q.timeRange("t.start_time", req.GetStartTime(), req.GetEndTime())
	for column, value := range map[string]string{"t.session_id": req.GetSessionId(), "t.workflow_id": req.GetWorkflowId(),
		"t.user_id": req.GetUserId()} {
		if value != "" {
			q.add(column+" = %s", value)
		}
	}
	if req.GetService() != "" {
		q.add("%s = ANY(t.services)", req.GetService())
	}
	if req.GetTag() != "" {
		q.add("%s = ANY(t.tags)", req.GetTag())
	}
	if req.GetMinDurationMs() > 0 {
		q.add("t.duration_ms >= %s", req.GetMinDurationMs())
	}
	if req.GetMinCostUsd() > 0 {
		q.add("t.total_cost_usd >= %s", req.GetMinCostUsd())
	}
	if req.GetErrorsOnly() {
		q.add("t.error_count > 0")
	}
	if req.GetScoreName() != "" {
		condition := "EXISTS (SELECT 1 FROM obs_scores sc WHERE sc.trace_id = t.trace_id AND sc.name = " +
			q.arg(req.GetScoreName()) + " AND sc.numeric_value IS NOT NULL"
		if req.MinScore != nil {
			condition += " AND sc.numeric_value >= " + q.arg(req.GetMinScore())
		}
		if req.MaxScore != nil {
			condition += " AND sc.numeric_value <= " + q.arg(req.GetMaxScore())
		}
		q.conditions = append(q.conditions, condition+")")
	}

	order := "t.start_time DESC, t.trace_id DESC"
	offset := 0
	switch req.GetOrderBy() {
	case "duration", "cost":
		column := map[string]string{"duration": "t.duration_ms", "cost": "t.total_cost_usd"}[req.GetOrderBy()]
		order = column + " DESC, t.trace_id DESC"
		// these orders page by offset
		if parts, err := decodeCursor(req.GetCursor(), 1); err != nil {
			return nil, "", err
		} else if parts != nil {
			if offset, err = strconv.Atoi(parts[0]); err != nil || offset < 0 {
				return nil, "", ErrInvalidCursor
			}
		}
	default:
		parts, err := decodeCursor(req.GetCursor(), 2)
		if err != nil {
			return nil, "", err
		}
		if parts != nil {
			at, err := strconv.ParseInt(parts[0], 10, 64)
			if err != nil {
				return nil, "", ErrInvalidCursor
			}
			q.add("(t.start_time, t.trace_id) < (%s, %s)", time.UnixMicro(at).UTC(), parts[1])
		}
	}

	rows, err := r.pool.Query(ctx, `SELECT `+strings.ReplaceAll(traceColumns, "trace_id,", "t.trace_id,")+
		` FROM obs_traces t WHERE `+q.where()+` ORDER BY `+order+
		fmt.Sprintf(" LIMIT %d OFFSET %d", limit+1, offset), q.args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	traces := make([]*gogiv1.Trace, 0, limit)
	starts := make([]int64, 0, limit)
	for rows.Next() {
		var start time.Time
		trace := &gogiv1.Trace{}
		var end time.Time
		var tokens int64
		if err := rows.Scan(&trace.TraceId, &trace.SessionId, &trace.WorkflowId, &trace.UserId, &trace.Input,
			&trace.Output, &trace.Tags, &start, &end, &trace.TotalDurationMs, &trace.TotalCostUsd, &tokens,
			&trace.SpanCount, &trace.ErrorCount); err != nil {
			return nil, "", err
		}
		trace.StartTime, trace.EndTime, trace.TotalTokens = start.UnixMilli(), end.UnixMilli(), int32(tokens)
		traces = append(traces, trace)
		starts = append(starts, start.UnixMicro())
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}

	next := ""
	if len(traces) > limit {
		traces, starts = traces[:limit], starts[:limit]
		last := traces[limit-1]
		if req.GetOrderBy() == "duration" || req.GetOrderBy() == "cost" {
			next = encodeCursor(strconv.Itoa(offset + limit))
		} else {
			next = encodeCursor(strconv.FormatInt(starts[limit-1], 10), last.TraceId)
		}
	}
	return traces, next, nil
}

// QueryGenerations returns a page of generations, newest first
func (r *GogiObservabilityRepository) QueryGenerations(ctx context.Context, req *gogiv1.QueryGenerationsRequest, limit int) ([]*gogiv1.Generation, string, error) {
	q := &query{}
	q.timeRange("g.start_time", req.GetStartTime(), req.GetEndTime())
	for column, value := range map[string]string{"g.model": req.GetModel(), "g.provider": req.GetProvider(),
		"g.workflow_id": req.GetWorkflowId(), "g.user_id": req.GetUserId(), "g.session_id": req.GetSessionId()} {
		if value != "" {
			q.add(column+" = %s", value)
		}
	}
	if req.GetMinCostUsd() > 0 {
		q.add("g.cost_usd >= %s", req.GetMinCostUsd())
	}
	if req.FallbackUsed != nil {
		q.add("g.fallback_used = %s", req.GetFallbackUsed())
	}
	parts, err := decodeCursor(req.GetCursor(), 3)
	if err != nil {
		return nil, "", err
	}
	if parts != nil {
		at, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return nil, "", ErrInvalidCursor
		}
		q.add("(g.start_time, g.trace_id, g.span_id) < (%s, %s, %s)", time.UnixMicro(at).UTC(), parts[1], parts[2])
	}

	rows, err := r.pool.Query(ctx, `SELECT `+spanColumns+`, `+generationColumnsSQL+`, g.start_time FROM obs_generations g
		JOIN obs_spans s ON s.trace_id = g.trace_id AND s.span_id = g.span_id
		WHERE `+q.where()+` ORDER BY g.start_time DESC, g.trace_id DESC, g.span_id DESC`+fmt.Sprintf(" LIMIT %d", limit+1),
		q.args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	generations := make([]*gogiv1.Generation, 0, limit)
	starts := make([]int64, 0, limit)
	for rows.Next() {
		var start time.Time
		generation := &gogiv1.Generation{}
		var prompt, completion int64
		var cost float64
		var session, workflow, user string
		span, err := scanSpan(rows, &generation.Model, &generation.Provider, &generation.RequestedModel, &prompt,
			&completion, &cost, &generation.CostEstimated, &generation.CacheHit, &generation.FallbackUsed,
			&generation.TimeToFirstTokenMs, &session, &workflow, &user, &start)
		if err != nil {
			return nil, "", err
		}
		span.SessionId, span.WorkflowId, span.UserId = session, workflow, user
		generation.Span, generation.PromptTokens, generation.CompletionTokens = span, int32(prompt), int32(completion)
		generation.CostUsd = &cost
		generations = append(generations, generation)
		starts = append(starts, start.UnixMicro())
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}

	next := ""
	if len(generations) > limit {
		generations = generations[:limit]
		last := generations[limit-1].Span
		next = encodeCursor(strconv.FormatInt(starts[limit-1], 10), last.TraceId, last.SpanId)
	}
	return generations, next, nil
}

// --- scores ---

func scoreSource(source gogiv1.ScoreSource) string {
	return strings.TrimPrefix(source.String(), "SCORE_SOURCE_")
}

func toScoreSource(source string) gogiv1.ScoreSource {
	return gogiv1.ScoreSource(gogiv1.ScoreSource_value["SCORE_SOURCE_"+source])
}

// InsertScores stores scores; a score sent again is ignored
func (r *GogiObservabilityRepository) InsertScores(ctx context.Context, scores []*gogiv1.Score) error {
	batch := &pgx.Batch{}
	for _, score := range scores {
		var numeric *float64
		var categorical *string
		var boolean *bool
		switch value := score.GetValue().(type) {
		case *gogiv1.Score_NumericValue:
			numeric = &value.NumericValue
		case *gogiv1.Score_CategoricalValue:
			categorical = &value.CategoricalValue
		case *gogiv1.Score_BooleanValue:
			boolean = &value.BooleanValue
		}
		batch.Queue(`INSERT INTO obs_scores (score_id, trace_id, span_id, generation_id, name, numeric_value,
			categorical_value, boolean_value, source, comment, metadata, timestamp)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb, $12) ON CONFLICT (score_id) DO NOTHING`,
			score.GetScoreId(), score.GetTraceId(), score.GetSpanId(), score.GetGenerationId(), score.GetName(),
			numeric, categorical, boolean, scoreSource(score.GetSource()), score.GetComment(),
			jsonOf(score.GetMetadata()), millis(score.GetTimestamp()))
	}
	return r.sendBatch(ctx, batch)
}

func scoreQuery(req *gogiv1.QueryScoresRequest) *query {
	q := &query{}
	q.timeRange("sc.timestamp", req.GetStartTime(), req.GetEndTime())
	if req.GetTraceId() != "" {
		q.add("sc.trace_id = %s", req.GetTraceId())
	}
	if req.GetName() != "" {
		q.add("sc.name = %s", req.GetName())
	}
	if req.GetSource() != gogiv1.ScoreSource_SCORE_SOURCE_UNSPECIFIED {
		q.add("sc.source = %s", scoreSource(req.GetSource()))
	}
	if req.GetWorkflowId() != "" {
		q.add("EXISTS (SELECT 1 FROM obs_traces t WHERE t.trace_id = sc.trace_id AND t.workflow_id = %s)", req.GetWorkflowId())
	}
	return q
}

// QueryScores returns a page of scores, newest first
func (r *GogiObservabilityRepository) QueryScores(ctx context.Context, req *gogiv1.QueryScoresRequest) ([]*gogiv1.Score, string, error) {
	limit := int(req.GetLimit())
	q := scoreQuery(req)
	parts, err := decodeCursor(req.GetCursor(), 2)
	if err != nil {
		return nil, "", err
	}
	if parts != nil {
		at, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return nil, "", ErrInvalidCursor
		}
		q.add("(sc.timestamp, sc.score_id) < (%s, %s)", time.UnixMicro(at).UTC(), parts[1])
	}

	rows, err := r.pool.Query(ctx, `SELECT score_id, trace_id, span_id, generation_id, name, numeric_value,
		categorical_value, boolean_value, source, comment, metadata, timestamp FROM obs_scores sc
		WHERE `+q.where()+` ORDER BY sc.timestamp DESC, sc.score_id DESC`+fmt.Sprintf(" LIMIT %d", limit+1), q.args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	scores := make([]*gogiv1.Score, 0, limit)
	stamps := make([]int64, 0, limit)
	for rows.Next() {
		score := &gogiv1.Score{}
		var numeric *float64
		var categorical *string
		var boolean *bool
		var source string
		var metadata []byte
		var timestamp time.Time
		if err := rows.Scan(&score.ScoreId, &score.TraceId, &score.SpanId, &score.GenerationId, &score.Name, &numeric,
			&categorical, &boolean, &source, &score.Comment, &metadata, &timestamp); err != nil {
			return nil, "", err
		}
		switch {
		case numeric != nil:
			score.Value = &gogiv1.Score_NumericValue{NumericValue: *numeric}
		case categorical != nil:
			score.Value = &gogiv1.Score_CategoricalValue{CategoricalValue: *categorical}
		case boolean != nil:
			score.Value = &gogiv1.Score_BooleanValue{BooleanValue: *boolean}
		}
		score.Source, score.Timestamp = toScoreSource(source), timestamp.UnixMilli()
		if err := json.Unmarshal(metadata, &score.Metadata); err != nil {
			return nil, "", err
		}
		scores = append(scores, score)
		stamps = append(stamps, timestamp.UnixMicro())
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}

	next := ""
	if len(scores) > limit {
		scores = scores[:limit]
		next = encodeCursor(strconv.FormatInt(stamps[limit-1], 10), scores[limit-1].ScoreId)
	}
	return scores, next, nil
}

// ScoreSummaries summarizes every score that matches the request, by name and source
func (r *GogiObservabilityRepository) ScoreSummaries(ctx context.Context, req *gogiv1.QueryScoresRequest) ([]*gogiv1.ScoreSummary, error) {
	q := scoreQuery(req)
	rows, err := r.pool.Query(ctx, `SELECT sc.name, sc.source, COUNT(*), COALESCE(AVG(sc.numeric_value), 0),
		COALESCE(AVG(CASE WHEN sc.boolean_value THEN 1.0 WHEN sc.boolean_value IS NOT NULL THEN 0.0 END), 0),
		COALESCE(jsonb_object_agg(sc.categorical_value, 1) FILTER (WHERE sc.categorical_value IS NOT NULL), '{}')
		FROM obs_scores sc WHERE `+q.where()+` GROUP BY sc.name, sc.source ORDER BY sc.name, sc.source`, q.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	summaries := make([]*gogiv1.ScoreSummary, 0)
	for rows.Next() {
		summary := &gogiv1.ScoreSummary{}
		var source string
		var ignored []byte
		if err := rows.Scan(&summary.Name, &source, &summary.Count, &summary.Mean, &summary.TrueRatio, &ignored); err != nil {
			return nil, err
		}
		summary.Source = toScoreSource(source)
		summaries = append(summaries, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// the counts of each category
	categories, err := r.pool.Query(ctx, `SELECT sc.name, sc.source, sc.categorical_value, COUNT(*) FROM obs_scores sc
		WHERE `+q.where()+` AND sc.categorical_value IS NOT NULL GROUP BY 1, 2, 3`, q.args...)
	if err != nil {
		return nil, err
	}
	defer categories.Close()
	for categories.Next() {
		var name, source, category string
		var count int64
		if err := categories.Scan(&name, &source, &category, &count); err != nil {
			return nil, err
		}
		for _, summary := range summaries {
			if summary.Name == name && summary.Source == toScoreSource(source) {
				if summary.Categories == nil {
					summary.Categories = map[string]int64{}
				}
				summary.Categories[category] = count
			}
		}
	}
	return summaries, categories.Err()
}

// --- logs ---

// InsertLogs stores log events; an event sent again is ignored
func (r *GogiObservabilityRepository) InsertLogs(ctx context.Context, events []*gogiv1.LogEvent) error {
	batch := &pgx.Batch{}
	for _, event := range events {
		batch.Queue(`INSERT INTO obs_logs (event_id, trace_id, span_id, timestamp, service, severity, event_type,
			message, attributes, numeric_attributes, workflow_id, user_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb, $10::jsonb, $11, $12) ON CONFLICT (event_id) DO NOTHING`,
			event.GetEventId(), event.GetTraceId(), event.GetSpanId(), millis(event.GetTimestamp()), event.GetService(),
			int16(event.GetSeverity()), event.GetEventType(), event.GetMessage(), jsonOf(event.GetAttributes()),
			jsonOf(event.GetNumericAttributes()), event.GetWorkflowId(), event.GetUserId())
	}
	return r.sendBatch(ctx, batch)
}

// QueryLogs returns a page of log events, newest first
func (r *GogiObservabilityRepository) QueryLogs(ctx context.Context, req *gogiv1.QueryLogsRequest, limit int) ([]*gogiv1.LogEvent, string, error) {
	q := &query{}
	q.timeRange("timestamp", req.GetStartTime(), req.GetEndTime())
	for column, value := range map[string]string{"trace_id": req.GetTraceId(), "span_id": req.GetSpanId(),
		"service": req.GetService(), "event_type": req.GetEventType(), "workflow_id": req.GetWorkflowId(),
		"user_id": req.GetUserId()} {
		if value != "" {
			q.add(column+" = %s", value)
		}
	}
	if req.GetMinSeverity() > 0 {
		q.add("severity >= %s", int16(req.GetMinSeverity()))
	}
	if len(req.GetAttributes()) > 0 {
		q.add("attributes @> %s::jsonb", jsonOf(req.GetAttributes()))
	}
	parts, err := decodeCursor(req.GetCursor(), 2)
	if err != nil {
		return nil, "", err
	}
	if parts != nil {
		at, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return nil, "", ErrInvalidCursor
		}
		q.add("(timestamp, event_id) < (%s, %s)", time.UnixMicro(at).UTC(), parts[1])
	}

	rows, err := r.pool.Query(ctx, `SELECT event_id, trace_id, span_id, timestamp, service, severity, event_type,
		message, attributes, numeric_attributes, workflow_id, user_id FROM obs_logs WHERE `+q.where()+
		` ORDER BY timestamp DESC, event_id DESC`+fmt.Sprintf(" LIMIT %d", limit+1), q.args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	events := make([]*gogiv1.LogEvent, 0, limit)
	stamps := make([]int64, 0, limit)
	for rows.Next() {
		event := &gogiv1.LogEvent{}
		var timestamp time.Time
		var severity int16
		var attributes, numeric []byte
		if err := rows.Scan(&event.EventId, &event.TraceId, &event.SpanId, &timestamp, &event.Service, &severity,
			&event.EventType, &event.Message, &attributes, &numeric, &event.WorkflowId, &event.UserId); err != nil {
			return nil, "", err
		}
		event.Timestamp, event.Severity = timestamp.UnixMilli(), gogiv1.Severity(severity)
		if err := json.Unmarshal(attributes, &event.Attributes); err != nil {
			return nil, "", err
		}
		if err := json.Unmarshal(numeric, &event.NumericAttributes); err != nil {
			return nil, "", err
		}
		events = append(events, event)
		stamps = append(stamps, timestamp.UnixMicro())
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}

	next := ""
	if len(events) > limit {
		events = events[:limit]
		next = encodeCursor(strconv.FormatInt(stamps[limit-1], 10), events[limit-1].EventId)
	}
	return events, next, nil
}

// --- metrics ---

func metricType(metric gogiv1.MetricType) string {
	if metric == gogiv1.MetricType_METRIC_TYPE_COUNTER {
		return "counter"
	}
	return "histogram"
}

// InsertMetricPoints stores metric points
func (r *GogiObservabilityRepository) InsertMetricPoints(ctx context.Context, points []*gogiv1.MetricPoint) error {
	rows := make([][]any, 0, len(points))
	for _, point := range points {
		rows = append(rows, []any{point.GetName(), metricType(point.GetType()), point.GetValue(),
			jsonOf(point.GetLabels()), point.GetService(), millis(point.GetTimestamp())})
	}
	batch := &pgx.Batch{}
	for _, row := range rows {
		batch.Queue(`INSERT INTO obs_metric_points (name, type, value, labels, service, timestamp)
			VALUES ($1, $2, $3, $4::jsonb, $5, $6)`, row...)
	}
	return r.sendBatch(ctx, batch)
}

// Aggregations are the aggregations of metric queries, as SQL
var Aggregations = map[string]string{
	"sum": "SUM(value)", "count": "COUNT(*)::double precision", "avg": "AVG(value)", "min": "MIN(value)",
	"max": "MAX(value)",
	"p50": "percentile_cont(0.5) WITHIN GROUP (ORDER BY value)",
	"p90": "percentile_cont(0.9) WITHIN GROUP (ORDER BY value)",
	"p95": "percentile_cont(0.95) WITHIN GROUP (ORDER BY value)",
	"p99": "percentile_cont(0.99) WITHIN GROUP (ORDER BY value)",
}

// QueryMetrics aggregates a metric's points into series, a series per combination of the
// group-by labels, a sample per time bucket
func (r *GogiObservabilityRepository) QueryMetrics(ctx context.Context, req *gogiv1.QueryMetricsRequest, aggregation string) ([]*gogiv1.MetricSeries, error) {
	q := &query{}
	q.add("name = %s", req.GetName())
	q.timeRange("timestamp", req.GetStartTime(), req.GetEndTime())
	if len(req.GetLabelFilters()) > 0 {
		q.add("labels @> %s::jsonb", jsonOf(req.GetLabelFilters()))
	}

	var bucket string
	if req.GetStepMs() == 0 {
		bucket = q.arg(millis(req.GetStartTime())) + "::timestamptz"
	} else {
		bucket = fmt.Sprintf("date_bin(make_interval(secs => %s), timestamp, TIMESTAMPTZ '1970-01-01 00:00:00+00')",
			q.arg(float64(req.GetStepMs())/1000))
	}
	selects := []string{bucket + " AS bucket"}
	groups := []string{"1"}
	for i, label := range req.GetGroupBy() {
		selects = append(selects, fmt.Sprintf("COALESCE(labels->>%s, '') AS g%d", q.arg(label), i))
		groups = append(groups, strconv.Itoa(i+2))
	}

	rows, err := r.pool.Query(ctx, `SELECT `+strings.Join(selects, ", ")+`, `+Aggregations[aggregation]+`, COUNT(*)
		FROM obs_metric_points WHERE `+q.where()+` GROUP BY `+strings.Join(groups, ", ")+
		` ORDER BY `+strings.Join(append(groups[1:], "1"), ", "), q.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	series := make([]*gogiv1.MetricSeries, 0)
	index := map[string]*gogiv1.MetricSeries{}
	for rows.Next() {
		var at time.Time
		values := make([]string, len(req.GetGroupBy()))
		var value float64
		var count int64
		dest := []any{&at}
		for i := range values {
			dest = append(dest, &values[i])
		}
		dest = append(dest, &value, &count)
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		key := strings.Join(values, "\x00")
		s, ok := index[key]
		if !ok {
			labels := map[string]string{}
			for i, label := range req.GetGroupBy() {
				labels[label] = values[i]
			}
			s = &gogiv1.MetricSeries{Labels: labels}
			index[key] = s
			series = append(series, s)
		}
		s.Samples = append(s.Samples, &gogiv1.MetricSample{Timestamp: at.UnixMilli(), Value: value, Count: count})
	}
	return series, rows.Err()
}

// --- cost ---

// generationDimension returns the SQL of a dimension of generations
func generationDimension(q *query, dimension string) string {
	if column, ok := generationColumns[dimension]; ok {
		return "g." + column
	}
	return "COALESCE(g.attributes->>" + q.arg(dimension) + ", '')"
}

func generationFilters(q *query, filters map[string]string) {
	for dimension, value := range filters {
		q.conditions = append(q.conditions, generationDimension(q, dimension)+" = "+q.arg(value))
	}
}

var granularities = map[string]string{"hourly": "hour", "daily": "day", "weekly": "week", "monthly": "month"}

// ValidGranularity returns true if the granularity of cost reports is known
func ValidGranularity(granularity string) bool {
	_, ok := granularities[granularity]
	return ok || granularity == "" || granularity == "none"
}

// CostReport aggregates the cost of generations by period and dimensions
func (r *GogiObservabilityRepository) CostReport(ctx context.Context, req *gogiv1.CostReportRequest) (*gogiv1.CostReport, error) {
	q := &query{}
	q.timeRange("g.start_time", req.GetStartTime(), req.GetEndTime())
	generationFilters(q, req.GetFilters())

	period := "TIMESTAMPTZ '1970-01-01 00:00:00+00'"
	if unit, ok := granularities[req.GetGranularity()]; ok {
		period = fmt.Sprintf("date_trunc('%s', g.start_time, 'UTC')", unit)
	}
	selects := []string{period + " AS period"}
	groups := []string{"1"}
	for i, dimension := range req.GetGroupBy() {
		selects = append(selects, generationDimension(q, dimension)+fmt.Sprintf(" AS d%d", i))
		groups = append(groups, strconv.Itoa(i+2))
	}

	rows, err := r.pool.Query(ctx, `SELECT `+strings.Join(selects, ", ")+`, SUM(g.cost_usd), SUM(g.prompt_tokens),
		SUM(g.completion_tokens), COUNT(*), COUNT(*) FILTER (WHERE NOT g.priced)
		FROM obs_generations g WHERE `+q.where()+` GROUP BY `+strings.Join(groups, ", ")+
		` ORDER BY 1, `+strconv.Itoa(len(selects)+1)+` DESC`, q.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	report := &gogiv1.CostReport{}
	for rows.Next() {
		var at time.Time
		values := make([]string, len(req.GetGroupBy()))
		bucket := &gogiv1.CostBucket{Group: map[string]string{}}
		var unpriced int64
		dest := []any{&at}
		for i := range values {
			dest = append(dest, &values[i])
		}
		dest = append(dest, &bucket.CostUsd, &bucket.PromptTokens, &bucket.CompletionTokens, &bucket.Generations, &unpriced)
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		if _, ok := granularities[req.GetGranularity()]; ok {
			bucket.PeriodStart = at.UnixMilli()
		}
		for i, dimension := range req.GetGroupBy() {
			bucket.Group[dimension] = values[i]
		}
		report.Buckets = append(report.Buckets, bucket)
		report.TotalCostUsd += bucket.CostUsd
		report.PromptTokens += bucket.PromptTokens
		report.CompletionTokens += bucket.CompletionTokens
		report.Generations += bucket.Generations
		report.UnpricedGenerations += unpriced
	}
	return report, rows.Err()
}

// Spend returns the cost of the generations that match the filters in [from, to)
func (r *GogiObservabilityRepository) Spend(ctx context.Context, filters map[string]string, from, to time.Time) (float64, error) {
	q := &query{}
	q.add("g.start_time >= %s", from)
	q.add("g.start_time < %s", to)
	generationFilters(q, filters)
	var spend float64
	err := r.pool.QueryRow(ctx, `SELECT COALESCE(SUM(g.cost_usd), 0) FROM obs_generations g WHERE `+q.where(),
		q.args...).Scan(&spend)
	return spend, err
}

// --- budgets ---

func scanBudget(row pgx.Row) (*gogiv1.BudgetAlert, error) {
	alert := &gogiv1.BudgetAlert{}
	var filters []byte
	var created, updated time.Time
	if err := row.Scan(&alert.Name, &filters, &alert.Period, &alert.LimitUsd, &alert.Thresholds, &alert.WebhookUrl,
		&created, &updated); err != nil {
		return nil, err
	}
	alert.CreatedAt, alert.UpdatedAt = created.UnixMilli(), updated.UnixMilli()
	return alert, json.Unmarshal(filters, &alert.Filters)
}

const budgetColumns = `name, filters, period, limit_usd, thresholds, webhook_url, created_at, updated_at`

// UpsertBudget adds the budget alert, or replaces the one with the same name
func (r *GogiObservabilityRepository) UpsertBudget(ctx context.Context, alert *gogiv1.BudgetAlert) (*gogiv1.BudgetAlert, error) {
	return scanBudget(r.pool.QueryRow(ctx, `INSERT INTO obs_budget_alerts (name, filters, period, limit_usd, thresholds, webhook_url)
		VALUES ($1, $2::jsonb, $3, $4, $5, $6)
		ON CONFLICT (name) DO UPDATE SET filters = EXCLUDED.filters, period = EXCLUDED.period,
			limit_usd = EXCLUDED.limit_usd, thresholds = EXCLUDED.thresholds, webhook_url = EXCLUDED.webhook_url,
			updated_at = NOW()
		RETURNING `+budgetColumns,
		alert.GetName(), jsonOf(alert.GetFilters()), alert.GetPeriod(), alert.GetLimitUsd(), alert.GetThresholds(),
		alert.GetWebhookUrl()))
}

func (r *GogiObservabilityRepository) GetBudget(ctx context.Context, name string) (*gogiv1.BudgetAlert, error) {
	alert, err := scanBudget(r.pool.QueryRow(ctx, `SELECT `+budgetColumns+` FROM obs_budget_alerts WHERE name = $1`, name))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w: %s", ErrBudgetNotFound, name)
	}
	return alert, err
}

func (r *GogiObservabilityRepository) ListBudgets(ctx context.Context) ([]*gogiv1.BudgetAlert, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+budgetColumns+` FROM obs_budget_alerts ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	alerts := make([]*gogiv1.BudgetAlert, 0)
	for rows.Next() {
		alert, err := scanBudget(rows)
		if err != nil {
			return nil, err
		}
		alerts = append(alerts, alert)
	}
	return alerts, rows.Err()
}

func (r *GogiObservabilityRepository) DeleteBudget(ctx context.Context, name string) (bool, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM obs_budget_alerts WHERE name = $1`, name)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// MarkNotified records that a threshold of a budget was notified in a period, and returns
// false if it already was
func (r *GogiObservabilityRepository) MarkNotified(ctx context.Context, name string, periodStart time.Time, threshold float64) (bool, error) {
	tag, err := r.pool.Exec(ctx, `INSERT INTO obs_budget_notifications (name, period_start, threshold)
		VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, name, periodStart, threshold)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// UnmarkNotified forgets a notification, so it is sent again, e.g. after the webhook failed
func (r *GogiObservabilityRepository) UnmarkNotified(ctx context.Context, name string, periodStart time.Time, threshold float64) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM obs_budget_notifications WHERE name = $1 AND period_start = $2 AND threshold = $3`,
		name, periodStart, threshold)
	return err
}

// --- health ---

// ServiceActivity is what a service's spans in a window say about it
type ServiceActivity struct {
	Service       string
	Spans         int64
	Errors        int64
	P95DurationMs float64
	LastSeen      time.Time
}

// ServiceActivity returns the activity of each service, or of one, since a time; and when
// each service was last seen at all
func (r *GogiObservabilityRepository) ServiceActivity(ctx context.Context, service string, since time.Time) ([]ServiceActivity, error) {
	rows, err := r.pool.Query(ctx, `
		WITH recent AS (
			SELECT service, COUNT(*) AS spans, COUNT(*) FILTER (WHERE status = 'error') AS errors,
				percentile_cont(0.95) WITHIN GROUP (ORDER BY duration_ms) AS p95
			FROM obs_spans WHERE start_time >= $1 AND ($2 = '' OR service = $2) AND service <> ''
			GROUP BY service),
		seen AS (
			SELECT service, MAX(start_time) AS last_seen FROM obs_spans
			WHERE ($2 = '' OR service = $2) AND service <> '' GROUP BY service)
		SELECT seen.service, COALESCE(recent.spans, 0), COALESCE(recent.errors, 0), COALESCE(recent.p95, 0), seen.last_seen
		FROM seen LEFT JOIN recent ON recent.service = seen.service
		WHERE recent.service IS NOT NULL OR $2 <> ''
		ORDER BY seen.service`, since, service)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	activity := make([]ServiceActivity, 0)
	for rows.Next() {
		var a ServiceActivity
		if err := rows.Scan(&a.Service, &a.Spans, &a.Errors, &a.P95DurationMs, &a.LastSeen); err != nil {
			return nil, err
		}
		activity = append(activity, a)
	}
	return activity, rows.Err()
}

// --- retention ---

// DeleteOlderThan deletes log events, metric points and traces older than their cutoffs,
// and returns how many rows of each kind it deleted
func (r *GogiObservabilityRepository) DeleteOlderThan(ctx context.Context, logs, metrics, traces time.Time) (map[string]int64, error) {
	deleted := map[string]int64{}
	statements := []struct {
		kind, sql string
		cutoff    time.Time
	}{
		{"logs", `DELETE FROM obs_logs WHERE timestamp < $1`, logs},
		{"metric points", `DELETE FROM obs_metric_points WHERE timestamp < $1`, metrics},
		{"scores", `DELETE FROM obs_scores WHERE trace_id IN (SELECT trace_id FROM obs_traces WHERE end_time < $1)`, traces},
		{"generations", `DELETE FROM obs_generations WHERE trace_id IN (SELECT trace_id FROM obs_traces WHERE end_time < $1)`, traces},
		{"spans", `DELETE FROM obs_spans WHERE trace_id IN (SELECT trace_id FROM obs_traces WHERE end_time < $1)`, traces},
		{"traces", `DELETE FROM obs_traces WHERE end_time < $1`, traces},
	}
	for _, statement := range statements {
		tag, err := r.pool.Exec(ctx, statement.sql, statement.cutoff)
		if err != nil {
			return deleted, fmt.Errorf("delete %s: %w", statement.kind, err)
		}
		deleted[statement.kind] = tag.RowsAffected()
	}
	return deleted, nil
}
