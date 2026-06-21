# 1. Build Stage
FROM golang:1.25-alpine AS builder

WORKDIR /app

# 复制所有源码（包含已缓存的 vendor 目录）
COPY . .

# 编译 canal-relay 服务，使用 -mod=vendor
RUN go build -mod=vendor -o canal-relay ./cmd/canal-relay/main.go

# 2. Run Stage
FROM alpine:latest

WORKDIR /app

# 复制二进制产物和配置
COPY --from=builder /app/canal-relay .
COPY --from=builder /app/configs ./configs

CMD ["./canal-relay"]
