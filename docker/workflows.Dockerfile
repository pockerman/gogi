FROM golang:1.25 AS builder

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -o /workflows-service ./gogi/services/workflows

FROM alpine:latest

WORKDIR /root

RUN apk --no-cache add ca-certificates

COPY --from=builder /workflows-service /root/workflows-service

RUN chmod +x /root/workflows-service

EXPOSE 50053

CMD ["/root/workflows-service"]