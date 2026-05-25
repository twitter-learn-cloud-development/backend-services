# Environment Context (研发与部署环境上下文)

本文档整理了本地研发环境、中间件依赖以及各微服务的编译运行指南，为开发者与 Agent 提供一致的本地调试上下文。

---

## 1. 配置文件管理

项目使用两种层级的配置：

1. **全局环境变量 (`.env`)**：
   * **物理位置**：项目根目录 [.env](file:///e:/GOProject/云原生/twitter-clone/.env)。
   * **作用**：控制系统运行模式（`APP_ENV`），定义 MySQL、Redis、RabbitMQ、Elasticsearch 等服务的连接串、微服务的注册端口、大模型 API 凭证（阿里云百炼 Key、LM Studio 地址）等。
2. **服务级 YAML 配置 (`configs/config.yaml`)**：
   * **物理位置**：[configs/config.yaml](file:///e:/GOProject/云原生/twitter-clone/configs/config.yaml)。
   * **作用**：控制底层的数据库连接池细粒度参数（`max_idle_conns`, `max_open_conns`, `conn_max_lifetime`）、日志等级以及慢 SQL 阈值等。

---

## 2. 本地依赖基础设施 (Docker Compose)

系统依赖的数据库和中间件均可通过根目录下的 [docker-compose.yaml](file:///e:/GOProject/云原生/twitter-clone/docker-compose.yaml) 统一拉起：

```bash
# 启动所有依赖中间件（后台运行）
docker-compose up -d mysql redis mongodb elasticsearch rabbitmq consul jaeger sentinel prometheus kibana grafana
```

### 基础设施端口暴露与后台控制台：

* **核心存储与通道**：
  * **MySQL**：`127.0.0.1:3307` (用户名 `root`, 密码由 `.env` 中 `${DB_PASSWORD}` 定义)
  * **Redis**：`127.0.0.1:6379`
  * **MongoDB**：`127.0.0.1:27017`
  * **Elasticsearch**：`127.0.0.1:9200`
  * **RabbitMQ**：`127.0.0.1:5672` (后台管理控制台：`http://localhost:15672` 账户密码 `guest/guest`)
* **微服务治理与可观测性**：
  * **Consul (注册中心)**：控制台 `http://localhost:8500`
  * **Jaeger (链路追踪)**：控制台 `http://localhost:16686`
  * **Sentinel (流量防卫与限流)**：控制台 `http://localhost:8858`
  * **Prometheus (时序指标)**：控制台 `http://localhost:9090`
  * **Grafana (看板展示)**：控制台 `http://localhost:3000` (账户密码 `admin/admin`)
  * **Kibana (ES 可视化)**：控制台 `http://localhost:5601`

---

## 3. 本地微服务调试与运行

所有微服务均支持通过 Go 命令本地直接运行。在本地调试时，请确保本地已配置好 Go 环境变量且 `.env` 配置正确。

### 核心微服务本地启动命令：

```bash
# 1. 启动用户服务 (gRPC: 9091)
go run cmd/user-service/main.go

# 2. 启动关注关系服务 (gRPC: 9093)
go run cmd/follow-service/main.go

# 3. 启动推文服务 (gRPC: 9092)
go run cmd/tweet-service/main.go

# 4. 启动通知服务 (RabbitMQ 监听)
go run cmd/notification-service/main.go

# 5. 启动私信消息服务 (gRPC: 9094)
go run cmd/messenger-service/main.go

# 6. 启动智能体服务 (gRPC: 9100, MCP: 9200)
go run cmd/agent-service/main.go

# 7. 启动异步 Timeline 写扩散消费者
go run cmd/consumer/main.go

# 8. 启动 API Gateway (HTTP: 9638, Websocket)
go run cmd/gateway/main.go
```

### 调试入口说明：
* 外网或前端联调时，统一访问网关地址：`http://localhost:9638`
* 接口交互规范、路径参数和 JSON 契约请参阅：[docs/API_REFERENCE.md](file:///e:/GOProject/云原生/twitter-clone/docs/API_REFERENCE.md)
