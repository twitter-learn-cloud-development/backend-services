# 1. Build Stage
FROM golang:1.25-alpine AS builder

WORKDIR /app



COPY . .

RUN go build -mod=vendor -o auth-service ./cmd/auth-service/main.go

# 2. Run Stage
FROM alpine:latest

WORKDIR /app

COPY --from=builder /app/auth-service .
COPY --from=builder /app/configs ./configs

EXPOSE 9097
EXPOSE 8081

CMD ["./auth-service"]
