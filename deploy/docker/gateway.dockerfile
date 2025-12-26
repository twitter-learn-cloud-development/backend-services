# 1. Build Stage
FROM golang:1.25-alpine AS builder

WORKDIR /app

# 设置代理，加速下载
ENV GOPROXY=https://goproxy.cn,direct

# 预下载依赖
COPY go.mod go.sum ./

# 下载依赖（会被 Docker 缓存）
RUN go mod download

COPY . .

# 编译
RUN go build -o gateway ./cmd/gateway/main.go

# 2. Run Stage
FROM alpine:latest

WORKDIR /app

# 从 builder 阶段复制二进制文件
COPY --from=builder /app/gateway .
COPY --from=builder /app/configs ./configs
# 如果有 .env，docker-compose 会注入环境变量，不需要复制

EXPOSE 9638

CMD ["./gateway"]