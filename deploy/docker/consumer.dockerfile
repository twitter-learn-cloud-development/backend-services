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
#构建consumer可执行文件
RUN go build -o consumer ./cmd/consumer/main.go

# 2. Run Stage
#获得运行环境镜像
FROM alpine:latest
#设置工作目录
WORKDIR /app
#复制builder中/app/consumer到当前目录
COPY --from=builder /app/consumer .
#复制builder中/app/configs到当前目录下的configs目录
COPY --from=builder /app/configs ./configs
#运行consumer
CMD ["./consumer"]