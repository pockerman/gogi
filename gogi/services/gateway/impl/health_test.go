package impl

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

// a gateway whose services report the given statuses; a service missing from statuses can't be reached
func newTestGatewayHealth(statuses map[string]healthpb.HealthCheckResponse_ServingStatus) *GatewayHealth {
	registry := NewServiceRegistry()
	registry.RegisterService("llms", "llms:50057")
	registry.RegisterService("documents", "documents:50054")

	gatewayHealth := NewGatewayHealth(registry, time.Second)
	gatewayHealth.checkService = func(_ context.Context, address string) (healthpb.HealthCheckResponse_ServingStatus, error) {
		serving, ok := statuses[address]
		if !ok {
			return healthpb.HealthCheckResponse_UNKNOWN, errors.New("connection refused")
		}
		return serving, nil
	}
	return gatewayHealth
}

func TestCheckGatewayItself(t *testing.T) {
	gatewayHealth := newTestGatewayHealth(nil)

	response, err := gatewayHealth.Check(context.Background(), &healthpb.HealthCheckRequest{})

	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if response.GetStatus() != healthpb.HealthCheckResponse_SERVING {
		t.Errorf("status = %v, want SERVING", response.GetStatus())
	}
}

func TestCheckService(t *testing.T) {
	gatewayHealth := newTestGatewayHealth(map[string]healthpb.HealthCheckResponse_ServingStatus{
		"llms:50057": healthpb.HealthCheckResponse_SERVING,
	})

	tests := []struct {
		service string
		want    healthpb.HealthCheckResponse_ServingStatus
	}{
		{"llms", healthpb.HealthCheckResponse_SERVING},
		// registered but not answering
		{"documents", healthpb.HealthCheckResponse_NOT_SERVING},
	}
	for _, test := range tests {
		response, err := gatewayHealth.Check(context.Background(), &healthpb.HealthCheckRequest{Service: test.service})
		if err != nil {
			t.Fatalf("Check(%s): %v", test.service, err)
		}
		if response.GetStatus() != test.want {
			t.Errorf("Check(%s) = %v, want %v", test.service, response.GetStatus(), test.want)
		}
	}
}

func TestCheckUnknownService(t *testing.T) {
	gatewayHealth := newTestGatewayHealth(nil)

	_, err := gatewayHealth.Check(context.Background(), &healthpb.HealthCheckRequest{Service: "nope"})

	if status.Code(err) != codes.NotFound {
		t.Errorf("code = %v, want NotFound", status.Code(err))
	}
}

func TestList(t *testing.T) {
	gatewayHealth := newTestGatewayHealth(map[string]healthpb.HealthCheckResponse_ServingStatus{
		"llms:50057": healthpb.HealthCheckResponse_SERVING,
	})

	response, err := gatewayHealth.List(context.Background(), &healthpb.HealthListRequest{})

	if err != nil {
		t.Fatalf("List: %v", err)
	}
	want := map[string]healthpb.HealthCheckResponse_ServingStatus{
		"":          healthpb.HealthCheckResponse_SERVING,
		"llms":      healthpb.HealthCheckResponse_SERVING,
		"documents": healthpb.HealthCheckResponse_NOT_SERVING,
	}
	if len(response.GetStatuses()) != len(want) {
		t.Fatalf("statuses = %v, want %v", response.GetStatuses(), want)
	}
	for name, serving := range want {
		if got := response.GetStatuses()[name].GetStatus(); got != serving {
			t.Errorf("status of %q = %v, want %v", name, got, serving)
		}
	}
}

// the gateway asks a real service's health server
func TestCheckServiceHealthOverGRPC(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	healthServer := health.NewServer()
	healthpb.RegisterHealthServer(server, healthServer)
	healthServer.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
	go server.Serve(lis)
	defer server.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	serving, err := checkServiceHealth(ctx, lis.Addr().String())

	if err != nil {
		t.Fatalf("checkServiceHealth: %v", err)
	}
	if serving != healthpb.HealthCheckResponse_NOT_SERVING {
		t.Errorf("status = %v, want NOT_SERVING", serving)
	}
}
