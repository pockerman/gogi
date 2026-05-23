package services

import (
	"fmt"
)

var SERVICE_PORTS = map[string]string{
	"documents": ":50051",
	"indexes":   ":50052",
	"ingestion": ":50053",
	"search":    ":50054",
}

func GetServicePort(serviceName string) (string, error) {
	if port, exists := SERVICE_PORTS[serviceName]; exists {
		return port, nil
	}
	return "", fmt.Errorf("port for service %s not found", serviceName)
}
