# Data Service container — pgvector-backed when VECTOR_STORE=pgvector.
# ==========================================
# Build stage
# ==========================================
FROM golang:1.25 AS builder

WORKDIR /app

# Copy go module files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build data service binary
RUN CGO_ENABLED=0 GOOS=linux \
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