# 1. Build Stage
FROM golang:1.25-alpine AS builder

WORKDIR /app



COPY . .

RUN go build -mod=vendor -o messenger-service ./cmd/messenger-service/main.go

# 2. Run Stage
FROM alpine:latest

WORKDIR /app

COPY --from=builder /app/messenger-service .
COPY --from=builder /app/configs ./configs

EXPOSE 9094

CMD ["./messenger-service"]
