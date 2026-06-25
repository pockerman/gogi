FROM golang:1.25 AS builder

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -o /llm-sessions-service ./gogi/services/llm_sessions

FROM alpine:latest

WORKDIR /root

RUN apk --no-cache add ca-certificates

COPY --from=builder /llm-sessions-service /root/llm-sessions-service

RUN chmod +x /root/llm-sessions-service

EXPOSE 50058

CMD ["/root/llm-sessions-service"]