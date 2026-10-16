package impl

import (
	"context"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

// GatewayHealth serves the standard gRPC health protocol (grpc.health.v1) for the whole platform.
//
// Check with an empty service reports the gateway itself. Check with the name of a registered service, e.g.
// "llms", asks that service's own health server, so a client needs only the gateway's address to know whether a
// service is up. List reports the gateway and every registered service at once. A service that cannot be reached
// in time is reported as NOT_SERVING rather than failing the call.
type GatewayHealth struct {
	healthpb.UnimplementedHealthServer
	registry *ServiceRegistry
	timeout  time.Duration
	// asks the service at address for its health; replaced in tests
	checkService func(ctx context.Context, address string) (healthpb.HealthCheckResponse_ServingStatus, error)
}

func NewGatewayHealth(registry *ServiceRegistry, timeout time.Duration) *GatewayHealth {
	return &GatewayHealth{registry: registry, timeout: timeout, checkService: checkServiceHealth}
}

func checkServiceHealth(ctx context.Context, address string) (healthpb.HealthCheckResponse_ServingStatus, error) {
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return healthpb.HealthCheckResponse_UNKNOWN, err
	}
	defer conn.Close()

	response, err := healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{})
	if err != nil {
		return healthpb.HealthCheckResponse_UNKNOWN, err
	}
	return response.GetStatus(), nil
}

func (h *GatewayHealth) Check(ctx context.Context, req *healthpb.HealthCheckRequest) (*healthpb.HealthCheckResponse, error) {
	name := req.GetService()
	if name == "" {
		return &healthpb.HealthCheckResponse{Status: healthpb.HealthCheckResponse_SERVING}, nil
	}

	address, err := h.registry.ResolveService(name)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "unknown service %q", name)
	}

	return &healthpb.HealthCheckResponse{Status: h.serviceStatus(ctx, name, address)}, nil
}

func (h *GatewayHealth) List(ctx context.Context, _ *healthpb.HealthListRequest) (*healthpb.HealthListResponse, error) {
	services := h.registry.Services()

	var mu sync.Mutex
	statuses := make(map[string]*healthpb.HealthCheckResponse, len(services)+1)
	statuses[""] = &healthpb.HealthCheckResponse{Status: healthpb.HealthCheckResponse_SERVING}

	var wg sync.WaitGroup
	for name, address := range services {
		wg.Add(1)
		go func() {
			defer wg.Done()
			serving := h.serviceStatus(ctx, name, address)
			mu.Lock()
			statuses[name] = &healthpb.HealthCheckResponse{Status: serving}
			mu.Unlock()
		}()
	}
	wg.Wait()

	return &healthpb.HealthListResponse{Statuses: statuses}, nil
}

func (h *GatewayHealth) serviceStatus(ctx context.Context, name string, address string) healthpb.HealthCheckResponse_ServingStatus {
	ctx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()

	serving, err := h.checkService(ctx, address)
	if err != nil {
		log.Warnf("health check of %s at %s failed: %v", name, address, err)
		return healthpb.HealthCheckResponse_NOT_SERVING
	}
	return serving
}
