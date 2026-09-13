FROM golang:1.25 AS builder

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -o /llm-tools-service ./gogi/services/llm_tools

FROM alpine:latest

WORKDIR /root

RUN apk --no-cache add ca-certificates

COPY --from=builder /llm-tools-service /root/llm-tools-service

RUN chmod +x /root/llm-tools-service

EXPOSE 50060

CMD ["/root/llm-tools-service"]
