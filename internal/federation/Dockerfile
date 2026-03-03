# Multi-stage build for small image size
FROM golang:1.25-alpine AS builder
WORKDIR /app
ENV GOTOOLCHAIN=auto
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o federation ./cmd/federation/

FROM alpine:latest
WORKDIR /root/
COPY --from=builder /app/federation .
RUN chmod +x ./federation
EXPOSE 8081
CMD ["./federation"]