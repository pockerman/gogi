FROM golang:1.25 AS builder

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -o /prompts-service ./gogi/services/prompts

FROM alpine:latest

WORKDIR /root

RUN apk --no-cache add ca-certificates

COPY --from=builder /prompts-service /root/prompts-service

RUN chmod +x /root/prompts-service

EXPOSE 50058

CMD ["/root/prompts-service"]