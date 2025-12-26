# 1. Build Stage
FROM golang:1.25-alpine AS builder

WORKDIR /app

ENV GOPROXY=https://goproxy.cn,direct

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -o consumer ./cmd/consumer/main.go

# 2. Run Stage
FROM alpine:latest

WORKDIR /app

COPY --from=builder /app/consumer .
COPY --from=builder /app/configs ./configs

CMD ["./consumer"]