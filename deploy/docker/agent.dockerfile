# 1. Build Stage
FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -mod=vendor -o agent-service ./cmd/agent-service/main.go
# 2. Run Stage
FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/agent-service .
COPY --from=builder /app/configs ./configs
EXPOSE 9100
EXPOSE 9200
CMD ["./agent-service"]
