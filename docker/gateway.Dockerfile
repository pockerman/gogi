# API Gateway container.
#
# Exposes 8080 (external HTTP -> workflows) and 50051 (internal gRPC ->
# platform services). The only platform service whose ports are mapped to
# the host in docker-compose, since external clients live on the host.