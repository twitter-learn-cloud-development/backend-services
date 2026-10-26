# 1. Build Stage
ARG BUILDER_IMAGE=golang:1.25.5-alpine
ARG RUNTIME_IMAGE=alpine:latest
FROM ${BUILDER_IMAGE} AS builder

WORKDIR /app

# 复制所有源码（包含已缓存的 vendor 目录）
COPY . .

# 编译 canal-relay 服务，使用 -mod=vendor
RUN CGO_ENABLED=0 go build -mod=vendor -trimpath -o canal-relay ./cmd/canal-relay

# 2. Run Stage
FROM ${RUNTIME_IMAGE}
RUN apk add --no-cache ca-certificates

WORKDIR /app

# 复制二进制产物和配置
COPY --from=builder /app/canal-relay .
COPY --from=builder /app/configs ./configs

CMD ["./canal-relay"]
