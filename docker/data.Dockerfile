# syntax=docker/dockerfile:1.7
# Data Service container — pgvector-backed when VECTOR_STORE=pgvector.
# ==========================================
# Build stage
# ==========================================
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Copy go module files
COPY go.mod go.sum ./

# Download dependencies
RUN --mount=type=cache,target=/root/go/pkg/mod \
    go mod download

# Copy source code
COPY . .

# Build data service binary
RUN --mount=type=cache,target=/root/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux \
    go build -o data-service ./gogi

# ==========================================
# Runtime stage
# ==========================================
FROM alpine:latest

WORKDIR /root/

RUN apk --no-cache add ca-certificates

COPY --from=builder /app/data-service .

# gRPC port
EXPOSE 50051

CMD ["./data-service"]