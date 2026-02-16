# Multi-stage build for identity service
FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o identity ./internal/identity/

FROM alpine:latest
WORKDIR /root/
COPY --from=builder /app/identity .
COPY --from=builder /app/internal/identity/migrations ./migrations/
EXPOSE 8082
CMD ["./identity"]