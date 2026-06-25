FROM golang:1.25 AS builder

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -o /llms-sessions-service ./gogi/services/llm_sessions

FROM alpine:latest

WORKDIR /root

RUN apk --no-cache add ca-certificates

COPY --from=builder /llms-sessions-service /root/llms-sessions-service

RUN chmod +x /root/llms-sessions-service

EXPOSE 50059

CMD ["/root/llms-sessions-service"]