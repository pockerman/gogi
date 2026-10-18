package applications

import (
	"context"
	"errors"
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	// KeysRequired: every call to the gateway carries a key of an application
	KeysRequired = "required"
	// KeysOff: gogi runs without keys, every call is for the Default application
	KeysOff = "off"
)

// the gateway's health is answered without a key, so load balancers can check it
const healthService = "/grpc.health.v1.Health/"

// KeyChecker finds the application a key was given to
type KeyChecker interface {
	ApplicationOf(ctx context.Context, key string) (string, error)
}

// Gateway authenticates the calls to the gateway and sends their application on to the services
type Gateway struct {
	keys     KeyChecker
	required bool
}

// NewGateway checks keys with the checker if mode is KeysRequired; with KeysOff it needs none
func NewGateway(mode string, keys KeyChecker) (*Gateway, error) {
	switch mode {
	case KeysRequired:
		if keys == nil {
			return nil, errors.New("keys are required but there is nothing to check them with")
		}
		return &Gateway{keys: keys, required: true}, nil
	case KeysOff, "":
		return &Gateway{}, nil
	default:
		return nil, fmt.Errorf("unknown mode %q for application keys, expected %q or %q", mode, KeysRequired, KeysOff)
	}
}

// ServerOptions are the gateway's interceptors
func (g *Gateway) ServerOptions() []grpc.ServerOption {
	return []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(g.unary),
		grpc.ChainStreamInterceptor(g.stream),
	}
}

func (g *Gateway) unary(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	ctx, err := g.authenticate(ctx, info.FullMethod)
	if err != nil {
		return nil, err
	}
	return handler(ctx, req)
}

func (g *Gateway) stream(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	ctx, err := g.authenticate(ss.Context(), info.FullMethod)
	if err != nil {
		return err
	}
	return handler(srv, &contextStream{ServerStream: ss, ctx: ctx})
}

// authenticate is the context of the call, sending its application on to the services. A header
// naming the application, sent by the caller, is not passed on: only what the key says counts
func (g *Gateway) authenticate(ctx context.Context, method string) (context.Context, error) {
	if !g.required {
		return OutgoingContext(WithApplication(ctx, Default), Default), nil
	}
	if strings.HasPrefix(method, healthService) {
		return ctx, nil
	}

	key, err := bearer(ctx)
	if err != nil {
		return nil, err
	}
	application, err := g.keys.ApplicationOf(ctx, key)
	if errors.Is(err, ErrNotAuthenticated) {
		return nil, status.Error(codes.Unauthenticated, "the key is not a key of an application of gogi, or no longer works")
	}
	if err != nil {
		log.Errorf("failed to check a key: %v", err)
		return nil, status.Error(codes.Unavailable, "failed to check the key")
	}
	return OutgoingContext(WithApplication(ctx, application), application), nil
}

func bearer(ctx context.Context) (string, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	values := md.Get("authorization")
	if len(values) == 0 {
		return "", status.Error(codes.Unauthenticated, "a call to gogi needs a key of its application: authorization: Bearer gogi_...")
	}
	scheme, key, found := strings.Cut(values[0], " ")
	if !found || !strings.EqualFold(scheme, "bearer") || strings.TrimSpace(key) == "" {
		return "", status.Error(codes.Unauthenticated, "the authorization header is not Bearer and a key")
	}
	return strings.TrimSpace(key), nil
}

// contextStream is a server stream with another context
type contextStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *contextStream) Context() context.Context {
	return s.ctx
}
