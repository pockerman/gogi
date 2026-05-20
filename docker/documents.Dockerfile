FROM golang:1.25 AS builder

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -o /documents-service ./gogi/services/data/documents

FROM alpine:latest

WORKDIR /root

RUN apk --no-cache add ca-certificates

COPY --from=builder /documents-service /root/documents-service

RUN chmod +x /root/documents-service

EXPOSE 50054

CMD ["/root/documents-service"]