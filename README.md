# Twitter Clone (Twitter-Cloud)

这是一个基于 Go 语言、gRPC 和微服务架构构建的高性能云原生推特克隆（Twitter Clone）项目。项目涵盖了从后端微服务治理到前端响应式界面的完整实现。

## 🚀 项目亮点

- **云原生微服务架构**：服务化拆分（用户、推文、关注、私信、通知等），解耦业务逻辑。
- **全链路追踪 & 监控**：集成 Jaeger (OpenTelemetry)、Prometheus 和 Grafana，实现可观测性。
- **高性能时间线算法**：利用 RabbitMQ 异步扇出（Fanout）技术和 Redis 缓存，支持极速 Feed 流刷新。
- **现代化前端**：基于 Vue 3 + Vite + Tailwind CSS 构建，提供流畅的用户体验。

## 🏗 服务架构

项目采用微服务架构设计，核心组件包括：

- **API Gateway**: 统一的 RESTful 入口，负责路由转发、JWT 认证、限流 (Rate Limiting) 和统一错误处理。
- **User Service**: 用户注册、登录、个人资料管理及社交属性扩展。
- **Tweet Service**: 推文发布（支持文本/图片/视频/投票）、点赞、评论、转发及全站搜索。
- **Follow Service**: 处理用户间的关注、取关关系及统计汇总。
- **Messenger Service**: 实现私信（Direct Messages）功能，支持 WebSocket 实时通信。
- **Notification Service**: 实时通知系统，通过 RabbitMQ 监听事件并分发。
- **Consumer**: 异步逻辑处理中心（如推文扇出到粉丝时间线、热门话题 `#Hashtag` 提取）。

## 🛠 技术栈

| 类别 | 技术 |
| :--- | :--- |
| **后端代码** | Go (Golang) |
| **Web 框架** | Gin (Gateway), gRPC (Internal Communication) |
| **数据库** | MySQL 8.0 (Persistence), Redis 7.0 (Caching) |
| **消息队列** | RabbitMQ (Asynchronous Events) |
| **服务治理** | Consul (Service Discovery), Sentinel (Circuit Breaker) |
| **链路追踪** | Jaeger / OpenTelemetry |
| **监控报警** | Prometheus & Grafana |
| **前端框架** | Vue 3, Pinia (State Management), Vue Router |
| **前端样式** | Tailwind CSS |
| **虚拟化** | Docker & Docker Compose |

## 📦 快速开始

### 1. 克隆项目
```bash
git clone <project-url>
cd twitter-clone
```

### 2. 基础设施启动
项目依赖 MySQL, Redis, RabbitMQ 等，建议通过 Docker 一键启动：
```bash
docker-compose up -d
```

### 3. 运行后端服务
你可以手动编译各服务二进制文件，或在 IDE 中直接运行：
- **API Gateway**: `cmd/gateway/main.go` (端口: 9638)
- **其他微服务**: 各个 `cmd/*-service/main.go`

### 4. 运行前端
```bash
cd web
npm install
npm run dev
```
访问 `http://localhost:5173` 即可进入系统。

## 📖 学习记忆指南

如果你是为了学习该项目，建议遵循以下阅读路径：
1. **入门**: `docker-compose.yaml` 了解全局组件。
2. **骨架**: `api/*.proto` 了解各服务间的契约。
3. **接口**: `internal/gateway/router/router.go` 查看所有功能入口。
4. **核心逻辑**: `internal/module/tweet/service/` 观察如何处理推文业务。
5. **高性能**: `internal/mq/consumer/timeline_consumer.go` 学习推特核心的时间线推送方案。

## 📄 开源协议
本项目采用 [MIT](LICENSE) 协议。