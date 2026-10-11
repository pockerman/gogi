package observability

import (
	"context"
	"sync"
	"time"

	gogiv1 "gogi/gogi/gogi/v1"
)

// Span records a unit of work. A nil *Span, from a nil client, does nothing
type Span struct {
	client *Client
	mu     sync.Mutex
	span   *gogiv1.Span
	ended  bool
}

// newSpan starts a span of the operation, as a child of the context's span, or as the
// root of a new trace
func (c *Client) newSpan(ctx context.Context, operation string) (TraceContext, *gogiv1.Span) {
	trace := FromContext(ctx)
	if trace.TraceID == "" {
		trace = TraceContext{TraceID: NewTraceID(), SessionID: trace.SessionID, WorkflowID: trace.WorkflowID,
			UserID: trace.UserID}
	}
	span := &gogiv1.Span{TraceId: trace.TraceID, SpanId: NewSpanID(), ParentSpanId: trace.SpanID,
		Service: c.config.Service, Operation: operation, StartTime: c.now().UnixMilli(),
		SessionId: trace.SessionID, WorkflowId: trace.WorkflowID, UserId: trace.UserID,
		Attributes: map[string]string{}, NumericAttributes: map[string]float64{}}
	trace.SpanID = span.SpanId
	return trace, span
}

// StartSpan starts a span of the operation, e.g. "guardrails.CheckPolicy", and returns a
// context for the work inside it: spans started from it, and calls made with it, nest
// under this span. End the span when the work is done
func (c *Client) StartSpan(ctx context.Context, operation string) (context.Context, *Span) {
	if c == nil {
		return ctx, nil
	}
	trace, span := c.newSpan(ctx, operation)
	return WithTraceContext(ctx, trace), &Span{client: c, span: span}
}

// SetAttribute sets an attribute of the span
func (s *Span) SetAttribute(key, value string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.span.Attributes[key] = value
}

// SetNumeric sets a numeric attribute of the span
func (s *Span) SetNumeric(key string, value float64) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.span.NumericAttributes[key] = value
}

// AddEvent records something that happened during the span
func (s *Span) AddEvent(name string, attributes map[string]string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.span.Events = append(s.span.Events, &gogiv1.SpanEvent{Name: name, Timestamp: s.client.now().UnixMilli(),
		Attributes: attributes})
}

// finish sets the span's end and status once, and returns the span; nil if it already ended
func (s *Span) finish(err error) *gogiv1.Span {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ended {
		return nil
	}
	s.ended = true
	s.span.EndTime = s.client.now().UnixMilli()
	s.span.Status = gogiv1.SpanStatus_SPAN_STATUS_OK
	if err != nil {
		s.span.Status = gogiv1.SpanStatus_SPAN_STATUS_ERROR
		s.span.ErrorMessage = err.Error()
	}
	return s.span
}

// End ends the span, failed if err is not nil, and buffers it to be sent. A span ends once
func (s *Span) End(err error) {
	if s == nil {
		return
	}
	if span := s.finish(err); span != nil {
		s.client.RecordSpan(span)
	}
}

// Generation records an LLM call: a span with the model, tokens and cost
type Generation struct {
	Span
	generation *gogiv1.Generation
}

// StartGeneration starts a generation, a leaf of the trace: nothing nests under an LLM call
func (c *Client) StartGeneration(ctx context.Context, operation string) *Generation {
	if c == nil {
		return nil
	}
	_, span := c.newSpan(ctx, operation)
	return &Generation{Span: Span{client: c, span: span}, generation: &gogiv1.Generation{Span: span}}
}

// SetModel sets the model asked for, and the provider and model that answered
func (g *Generation) SetModel(provider, model, requestedModel string) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.generation.Provider, g.generation.Model, g.generation.RequestedModel = provider, model, requestedModel
}

// SetUsage sets the tokens of the call
func (g *Generation) SetUsage(promptTokens, completionTokens int) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.generation.PromptTokens, g.generation.CompletionTokens = int32(promptTokens), int32(completionTokens)
}

// SetCost sets the cost of the call; without it, the observability service prices it
func (g *Generation) SetCost(costUSD float64) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.generation.CostUsd = &costUSD
}

// SetFallback records that another model answered than the one asked for
func (g *Generation) SetFallback(used bool) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.generation.FallbackUsed = used
}

// SetCacheHit records that the response came from a cache
func (g *Generation) SetCacheHit(hit bool) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.generation.CacheHit = hit
}

// MarkFirstToken records the time to the first token of a streamed response, the first time it is called
func (g *Generation) MarkFirstToken() {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.generation.TimeToFirstTokenMs == 0 {
		g.generation.TimeToFirstTokenMs = float64(g.client.now().UnixMilli() - g.span.StartTime)
	}
}

// End ends the generation, failed if err is not nil, and buffers it to be sent
func (g *Generation) End(err error) {
	if g == nil {
		return
	}
	if span := g.finish(err); span != nil {
		g.client.RecordGeneration(g.generation)
	}
}

// Elapsed returns how long the span has run
func (s *Span) Elapsed() time.Duration {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return time.Duration(s.client.now().UnixMilli()-s.span.StartTime) * time.Millisecond
}
