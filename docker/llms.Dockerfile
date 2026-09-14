FROM golang:1.25 AS builder

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -o /llms-service ./gogi/services/llms

FROM alpine:latest

WORKDIR /root

RUN apk --no-cache add ca-certificates

COPY --from=builder /llms-service /root/llms-service

RUN chmod +x /root/llms-service

EXPOSE 50057

CMD ["/root/llms-service"]