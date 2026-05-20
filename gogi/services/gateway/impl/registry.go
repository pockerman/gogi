package impl

import "fmt"

// Registry of available backend services.
// Tracks both:
// - Platform services (sessions, models, data, etc.) for internal gRPC routing
// - Workflow services (user-defined workflows) for external HTTP routing

type ServiceRegistry struct {
	services  map[string]string // Map of service names to their addresses
	workflows map[string]string // Map of workflow names to their addresses
}

func NewServiceRegistry() *ServiceRegistry {
	return &ServiceRegistry{
		services:  make(map[string]string),
		workflows: make(map[string]string),
	}
}

func (r *ServiceRegistry) RegisterService(name string, address string) {

	if _, ok := r.services[name]; !ok {
		r.services[name] = address
	} // Add service to registry
}

func (r *ServiceRegistry) RegisterWorkflow(name string, address string) {

	if _, ok := r.workflows[name]; !ok {
		r.workflows[name] = address
	} // Add workflow to registry
}
func (r *ServiceRegistry) GetServiceAddress(name string) (string, error) {
	if address, ok := r.services[name]; ok {
		return address, nil
	}
	return "", fmt.Errorf("service not found: %s", name)
}

func (r *ServiceRegistry) GetWorkflowAddress(name string) (string, error) {
	if address, ok := r.workflows[name]; ok {
		return address, nil
	}
	return "", fmt.Errorf("workflow not found: %s", name)
}
