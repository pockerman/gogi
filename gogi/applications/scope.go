package applications

import (
	"context"
	"os"
	"strconv"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Scope keeps a service's names apart by application: the named string fields of the requests it
// is given are qualified by the call's application, and those of its responses unqualified, at
// any depth of the messages. A service names the fields that hold names an application chooses,
// e.g. index_name, user_id
type Scope struct {
	fields map[protoreflect.Name]bool
	// a call without an application is refused rather than taken to be for the Default one
	required bool
}

// NewScope scopes the fields. If required, every call must say which application it is for
func NewScope(required bool, fields ...string) *Scope {
	names := make(map[protoreflect.Name]bool, len(fields))
	for _, field := range fields {
		names[protoreflect.Name(field)] = true
	}
	return &Scope{fields: names, required: required}
}

// ScopeFromEnv scopes the fields; every call must say which application it is for if
// GOGI_REQUIRE_APPLICATION is true, as where the gateway requires keys
func ScopeFromEnv(fields ...string) *Scope {
	required, _ := strconv.ParseBool(os.Getenv("GOGI_REQUIRE_APPLICATION"))
	return NewScope(required, fields...)
}

// ServerOptions are the service's interceptors
func (s *Scope) ServerOptions() []grpc.ServerOption {
	return []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(s.unary),
		grpc.ChainStreamInterceptor(s.stream),
	}
}

func (s *Scope) application(ctx context.Context, method string) (string, error) {
	application := fromHeader(ctx)
	if application == "" {
		if s.required && !strings.HasPrefix(method, healthService) {
			return "", status.Error(codes.Unauthenticated, "the call does not say which application it is for")
		}
		application = Default
	}
	return application, nil
}

func (s *Scope) unary(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	application, err := s.application(ctx, info.FullMethod)
	if err != nil {
		return nil, err
	}
	s.Qualify(application, req)
	resp, err := handler(WithApplication(ctx, application), req)
	if err != nil {
		return nil, unqualifyError(application, err)
	}
	s.Unqualify(application, resp)
	return resp, nil
}

func (s *Scope) stream(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	application, err := s.application(ss.Context(), info.FullMethod)
	if err != nil {
		return err
	}
	scoped := &scopedStream{ServerStream: ss, ctx: WithApplication(ss.Context(), application), scope: s, application: application}
	return unqualifyError(application, handler(srv, scoped))
}

// Qualify qualifies the message's scoped fields by the application
func (s *Scope) Qualify(application string, message any) {
	s.rewrite(message, func(name string) string { return Qualify(application, name) })
}

// Unqualify gives the message's scoped fields back as the application named them
func (s *Scope) Unqualify(application string, message any) {
	s.rewrite(message, func(name string) string { return Unqualify(application, name) })
}

func (s *Scope) rewrite(message any, change func(string) string) {
	m, ok := message.(proto.Message)
	if !ok || m == nil {
		return
	}
	s.rewriteMessage(m.ProtoReflect(), change)
}

func (s *Scope) rewriteMessage(m protoreflect.Message, change func(string) string) {
	if !m.IsValid() {
		return
	}
	// the message's own fields are changed once it has been ranged over
	var renamed []protoreflect.FieldDescriptor
	defer func() {
		for _, field := range renamed {
			m.Set(field, protoreflect.ValueOfString(change(m.Get(field).String())))
		}
	}()
	m.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		switch {
		case field.IsMap():
			if field.MapValue().Kind() == protoreflect.MessageKind {
				value.Map().Range(func(_ protoreflect.MapKey, v protoreflect.Value) bool {
					s.rewriteMessage(v.Message(), change)
					return true
				})
			}
		case field.Kind() == protoreflect.MessageKind || field.Kind() == protoreflect.GroupKind:
			if field.IsList() {
				for i := 0; i < value.List().Len(); i++ {
					s.rewriteMessage(value.List().Get(i).Message(), change)
				}
			} else {
				s.rewriteMessage(value.Message(), change)
			}
		case field.Kind() == protoreflect.StringKind && s.fields[field.Name()]:
			if field.IsList() {
				list := value.List()
				for i := 0; i < list.Len(); i++ {
					list.Set(i, protoreflect.ValueOfString(change(list.Get(i).String())))
				}
			} else {
				renamed = append(renamed, field)
			}
		}
		return true
	})
}

// unqualifyError gives an error's message back with the application's names as it named them
func unqualifyError(application string, err error) error {
	if err == nil || application == Default {
		return err
	}
	st, ok := status.FromError(err)
	if !ok {
		return err
	}
	return status.Error(st.Code(), strings.ReplaceAll(st.Message(), application+separator, ""))
}

// scopedStream qualifies the messages a stream receives and unqualifies those it sends
type scopedStream struct {
	grpc.ServerStream
	ctx         context.Context
	scope       *Scope
	application string
}

func (s *scopedStream) Context() context.Context {
	return s.ctx
}

func (s *scopedStream) RecvMsg(m any) error {
	if err := s.ServerStream.RecvMsg(m); err != nil {
		return err
	}
	s.scope.Qualify(s.application, m)
	return nil
}

func (s *scopedStream) SendMsg(m any) error {
	s.scope.Unqualify(s.application, m)
	return s.ServerStream.SendMsg(m)
}

// UnaryClientInterceptor sends the application of the call a service is serving on with the
// calls it makes to other services for it
func UnaryClientInterceptor() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		return invoker(OutgoingContext(ctx, FromContext(ctx)), method, req, reply, cc, opts...)
	}
}
