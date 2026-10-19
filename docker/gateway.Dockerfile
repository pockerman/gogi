# syntax=docker/dockerfile:1.7
# API Gateway container.
#
# Exposes 8080 (external HTTP -> workflows) and 50051 (internal gRPC ->
# platform services). The only platform service whose ports are mapped to
# the host in docker-compose, since external clients live on the host.
# ==========================================
# Build stage
# ==========================================
FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./

RUN --mount=type=cache,target=/root/go/pkg/mod \
    go mod download

COPY . .

RUN --mount=type=cache,target=/root/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -o /gateway ./gogi/services/gateway && \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /gogi-admin ./gogi/cmd/gogi-admin

FROM alpine:latest

WORKDIR /root

RUN apk --no-cache add ca-certificates

COPY --from=builder /gateway /root/gateway
# the operator's tool for the applications that use gogi and their keys
COPY --from=builder /gogi-admin /root/gogi-admin

RUN chmod +x /root/gateway /root/gogi-admin

EXPOSE 8080
EXPOSE 50051

CMD ["/root/gateway"]