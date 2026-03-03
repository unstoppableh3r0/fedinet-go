# Multi-stage build for identity service
FROM golang:1.25-alpine AS builder
WORKDIR /app
ENV GOTOOLCHAIN=auto

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o identity ./cmd/identity/

FROM alpine:latest
WORKDIR /root/

COPY --from=builder /app/identity .
COPY --from=builder /app/internal/identity/migrations ./migrations/

RUN chmod +x ./identity

EXPOSE 8082
CMD ["./identity"]