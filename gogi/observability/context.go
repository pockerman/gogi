// Package observability reports the telemetry of a service to the observability service:
// spans and generations of distributed traces, structured logs and metrics. Telemetry is
// buffered and sent in batches in the background, so reporting never slows a request down
// or fails it. A nil *Client is valid and reports nothing, so telemetry can be turned off
// without changing the code that reports it.
package observability

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"regexp"

	"google.golang.org/grpc/metadata"
)

// The gRPC metadata keys that carry the trace context from service to service
const (
	MetadataTraceID    = "x-trace-id"
	MetadataSpanID     = "x-span-id"
	MetadataSessionID  = "x-session-id"
	MetadataWorkflowID = "x-workflow-id"
	MetadataUserID     = "x-user-id"
)

// TraceContext is the trace a request belongs to, and the span that is doing the work now
type TraceContext struct {
	TraceID string
	// SpanID is the current span: the parent of the spans started from this context
	SpanID     string
	SessionID  string
	WorkflowID string
	UserID     string
}

type traceContextKey struct{}

// WithTraceContext returns a context that carries the trace context
func WithTraceContext(ctx context.Context, trace TraceContext) context.Context {
	return context.WithValue(ctx, traceContextKey{}, trace)
}

// FromContext returns the trace context of the context; an empty one if it has none
func FromContext(ctx context.Context) TraceContext {
	trace, _ := ctx.Value(traceContextKey{}).(TraceContext)
	return trace
}

var idPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
var metadataPattern = regexp.MustCompile(`^[^\x00-\x1f]{1,256}$`)

func first(md metadata.MD, key string, pattern *regexp.Regexp) string {
	values := md.Get(key)
	if len(values) == 0 || !pattern.MatchString(values[0]) {
		return ""
	}
	return values[0]
}

// ContextFromIncoming returns the context with the trace context the caller sent in the
// request's gRPC metadata. Malformed ids are ignored, so a bad caller starts a new trace
// rather than corrupting another
func ContextFromIncoming(ctx context.Context) context.Context {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ctx
	}
	trace := TraceContext{
		TraceID:    first(md, MetadataTraceID, idPattern),
		SpanID:     first(md, MetadataSpanID, idPattern),
		SessionID:  first(md, MetadataSessionID, metadataPattern),
		WorkflowID: first(md, MetadataWorkflowID, metadataPattern),
		UserID:     first(md, MetadataUserID, metadataPattern),
	}
	if trace.TraceID == "" {
		trace.SpanID = ""
	}
	return WithTraceContext(ctx, trace)
}

// OutgoingContext returns the context with its trace context added to the outgoing gRPC
// metadata, so the service called continues the trace
func OutgoingContext(ctx context.Context) context.Context {
	trace := FromContext(ctx)
	if trace.TraceID == "" {
		return ctx
	}
	pairs := []string{MetadataTraceID, trace.TraceID}
	for key, value := range map[string]string{MetadataSpanID: trace.SpanID, MetadataSessionID: trace.SessionID,
		MetadataWorkflowID: trace.WorkflowID, MetadataUserID: trace.UserID} {
		if value != "" {
			pairs = append(pairs, key, value)
		}
	}
	return metadata.AppendToOutgoingContext(ctx, pairs...)
}

func newID(bytes int) string {
	id := make([]byte, bytes)
	if _, err := rand.Read(id); err != nil {
		panic(err)
	}
	return hex.EncodeToString(id)
}

// NewTraceID returns a random trace id
func NewTraceID() string {
	return newID(16)
}

// NewSpanID returns a random span id
func NewSpanID() string {
	return newID(8)
}
