# 1. Build Stage
#创建构造镜像，并命名为builder
FROM golang:1.25-alpine AS builder
#设置工作目录
WORKDIR /app
#设置代理对象网站
ENV GOPROXY=https://goproxy.cn,direct
#复制go.mod,go.sum到/app路径下
COPY go.mod go.sum ./
#进行依赖下载
RUN go mod download
#复制源码到/app目录下
COPY . .
#构建follow可执行文件
RUN go build -o follow-service ./cmd/follow-service/main.go

# 2. Run Stage
FROM alpine:latest

WORKDIR /app

COPY --from=builder /app/follow-service .
COPY --from=builder /app/configs ./configs
#暴露在9093服务端口，意味着大家可以通过这个端口访问这个服务
EXPOSE 9093

CMD ["./follow-service"]
