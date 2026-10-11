package observability

import (
	"context"
	"strings"

	gogiv1 "gogi/gogi/gogi/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// clientFaults are the codes of calls that failed because of the caller: the service worked
var clientFaults = map[codes.Code]bool{
	codes.Canceled: true, codes.InvalidArgument: true, codes.NotFound: true, codes.AlreadyExists: true,
	codes.PermissionDenied: true, codes.Unauthenticated: true, codes.FailedPrecondition: true,
	codes.Aborted: true, codes.OutOfRange: true,
}

// methodName returns the method of a full gRPC method name, e.g. Run of /gogi.v1.LLMModelServer/Run
func methodName(fullMethod string) string {
	return fullMethod[strings.LastIndex(fullMethod, "/")+1:]
}

// skipped are the methods that get no span: health checks would drown everything else
func skipped(fullMethod string) bool {
	return strings.HasPrefix(fullMethod, "/grpc.health.v1.Health/")
}

// endRPC ends the span of an RPC: a failure caused by the caller leaves the span ok, as the
// service worked, but records the code
func endRPC(span *Span, err error) {
	code := status.Code(err)
	span.SetAttribute("rpc.code", code.String())
	if err != nil && clientFaults[code] {
		span.SetAttribute("rpc.error", status.Convert(err).Message())
		span.End(nil)
		return
	}
	span.End(err)
}

func (c *Client) startRPC(ctx context.Context, fullMethod string) (context.Context, *Span) {
	ctx, span := c.StartSpan(ContextFromIncoming(ctx), c.config.Service+"."+methodName(fullMethod))
	span.SetAttribute("rpc.method", fullMethod)
	return ctx, span
}

// UnaryServerInterceptor gives every unary RPC of the service a span, continuing the trace
// the caller sent, so services are traced without code of their own
func (c *Client) UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if c == nil || skipped(info.FullMethod) {
			return handler(ctx, req)
		}
		ctx, span := c.startRPC(ctx, info.FullMethod)
		response, err := handler(ctx, req)
		endRPC(span, err)
		return response, err
	}
}

// tracedStream gives a streaming handler the context of the RPC's span
type tracedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *tracedStream) Context() context.Context {
	return s.ctx
}

// StreamServerInterceptor gives every streaming RPC of the service a span
func (c *Client) StreamServerInterceptor() grpc.StreamServerInterceptor {
	return func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if c == nil || skipped(info.FullMethod) {
			return handler(srv, stream)
		}
		ctx, span := c.startRPC(stream.Context(), info.FullMethod)
		span.SetAttribute("rpc.streaming", "true")
		err := handler(srv, &tracedStream{ServerStream: stream, ctx: ctx})
		endRPC(span, err)
		return err
	}
}

// ServerOptions returns the options that trace a gRPC server's RPCs
func (c *Client) ServerOptions() []grpc.ServerOption {
	return []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(c.UnaryServerInterceptor()),
		grpc.ChainStreamInterceptor(c.StreamServerInterceptor()),
	}
}

// UnaryClientInterceptor sends the trace context with every unary call, so the service
// called continues the trace. It works whether or not telemetry is on
func UnaryClientInterceptor() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		return invoker(OutgoingContext(ctx), method, req, reply, cc, opts...)
	}
}

// StreamClientInterceptor sends the trace context with every streaming call
func StreamClientInterceptor() grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string,
		streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		return streamer(OutgoingContext(ctx), desc, cc, method, opts...)
	}
}

// DialOptions returns the options that propagate the trace context on a client connection
func DialOptions() []grpc.DialOption {
	return []grpc.DialOption{
		grpc.WithChainUnaryInterceptor(UnaryClientInterceptor()),
		grpc.WithChainStreamInterceptor(StreamClientInterceptor()),
	}
}

// Metric names. Names follow gogi.{service}.{measurement}: those ending in _total are
// counters, the others histograms
const (
	MetricModelRequests         = "gogi.models.requests_total"
	MetricModelRequestDuration  = "gogi.models.request_duration_ms"
	MetricModelPromptTokens     = "gogi.models.prompt_tokens_total"
	MetricModelCompletionTokens = "gogi.models.completion_tokens_total"
	MetricModelCost             = "gogi.models.cost_usd_total"
	MetricModelFallbacks        = "gogi.models.fallbacks_total"
	MetricModelErrors           = "gogi.models.errors_total"
)

// Severities, for brevity
const (
	SeverityInfo    = gogiv1.Severity_SEVERITY_INFO
	SeverityWarning = gogiv1.Severity_SEVERITY_WARNING
	SeverityError   = gogiv1.Severity_SEVERITY_ERROR
)
