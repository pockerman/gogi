# syntax=docker/dockerfile:1.7
# Stage 1: Build the binary
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Copy dependency files first for better caching
COPY go.mod go.sum ./

RUN --mount=type=cache,target=/root/go/pkg/mod \
    go mod download

COPY . .

# Build a static binary
# CGO_ENABLED=0 ensures no external C dependencies are needed
RUN --mount=type=cache,target=/root/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -o /ingest-document-worker ./gogi/workers/ingest_document

# Stage 2: Minimal runtime image
FROM alpine:3.19

# Install CA certificates (required for HTTPS calls to Temporal Cloud or external APIs)
RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Copy the binary from the builder stage
COPY --from=builder  /ingest-document-worker /root/ingest-document-worker

RUN chmod +x /root/ingest-document-worker

# The entrypoint runs the worker binary
# It expects TEMPORAL_HOST env var to be set by Docker/K8s
CMD ["/root/ingest-document-worker"]   