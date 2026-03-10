# Multi-stage build for identity service
FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o identity ./cmd/identity/

FROM alpine:latest
WORKDIR /root/
COPY --from=builder /app/identity .
RUN chmod +x ./identity
COPY --from=builder /app/internal/identity/migrations ./migrations/
EXPOSE 8082
CMD ["./identity"]