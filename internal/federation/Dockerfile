# Multi-stage build for small image size
FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o federation ./internal/federation/

FROM alpine:latest
WORKDIR /root/
COPY --from=builder /app/federation .
EXPOSE 8081
CMD ["./federation"]