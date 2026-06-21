# 1. Build Stage
FROM golang:1.25-alpine AS builder

WORKDIR /app



COPY . .

RUN go build -mod=vendor -o user-service ./cmd/user-service/main.go

# 2. Run Stage
FROM alpine:latest

WORKDIR /app

COPY --from=builder /app/user-service .
COPY --from=builder /app/configs ./configs

EXPOSE 9091

CMD ["./user-service"]
