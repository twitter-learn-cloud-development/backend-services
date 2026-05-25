# ==============================================================================
# Makefile for Twitter Clone Monorepo
# Supports Windows (Git Bash/MinGW) and Linux/macOS
# ==============================================================================

BIN_DIR = bin
DOCKER_COMPOSE = docker-compose

.PHONY: all build clean up down test run-gateway run-user run-tweet run-follow run-notification run-messenger run-agent run-consumer

all: build

# 1. 一键后台启动所有基础设施依赖容器
up:
	@echo "🚀 Starting infrastructure containers (MySQL, Redis, MQ, ES, Consul, Jaeger)..."
	$(DOCKER_COMPOSE) up -d mysql redis mongodb elasticsearch rabbitmq consul jaeger sentinel prometheus kibana grafana
	@echo "⏳ Waiting for services to be ready..."
	@echo "   - Consul Dashboard: http://localhost:8500"
	@echo "   - RabbitMQ Admin:  http://localhost:15672 (guest/guest)"
	@echo "   - Jaeger Tracing:  http://localhost:16686"

# 2. 一键停止并销毁本地基础设施容器
down:
	@echo "🛑 Stopping and removing containers..."
	$(DOCKER_COMPOSE) down

# 3. 编译所有微服务二进制到 bin/ 目录（跨平台 mkdir 兼容）
build:
	@echo "🔨 Building all microservices..."
	@mkdir $(BIN_DIR) 2>nul || mkdir -p $(BIN_DIR) 2>/dev/null || true
	go build -o $(BIN_DIR)/gateway cmd/gateway/main.go
	go build -o $(BIN_DIR)/user-service cmd/user-service/main.go
	go build -o $(BIN_DIR)/tweet-service cmd/tweet-service/main.go
	go build -o $(BIN_DIR)/follow-service cmd/follow-service/main.go
	go build -o $(BIN_DIR)/notification-service cmd/notification-service/main.go
	go build -o $(BIN_DIR)/messenger-service cmd/messenger-service/main.go
	go build -o $(BIN_DIR)/agent-service cmd/agent-service/main.go
	go build -o $(BIN_DIR)/consumer cmd/consumer/main.go
	@echo "✅ All binaries built successfully in $(BIN_DIR)/"

# 4. 清理编译二进制文件以及根目录下的历史残留（跨平台 rm 兼容）
clean:
	@echo "🧹 Cleaning up built binaries and remnants..."
	@rm -rf $(BIN_DIR) 2>/dev/null || rmdir /s /q $(BIN_DIR) 2>nul || true
	@rm -f agent-service.exe consumer main.exe 2>/dev/null || del /f agent-service.exe consumer main.exe 2>nul || true
	@echo "✨ Clean completed"

# 5. 一键运行所有单元测试
test:
	go test -v ./internal/... ./pkg/...

# 6. 本地独立运行各微服务指令
run-gateway:
	go run cmd/gateway/main.go

run-user:
	go run cmd/user-service/main.go

run-tweet:
	go run cmd/tweet-service/main.go

run-follow:
	go run cmd/follow-service/main.go

run-notification:
	go run cmd/notification-service/main.go

run-messenger:
	go run cmd/messenger-service/main.go

run-agent:
	go run cmd/agent-service/main.go

run-consumer:
	go run cmd/consumer/main.go
