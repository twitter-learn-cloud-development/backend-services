# Twitter Clone 云原生微服务项目 — 进展与规划

## 📋 项目概述

基于 Go 语言的 Twitter 仿真微服务系统，采用云原生架构部署在 Kubernetes (Minikube) 上。

| 维度 | 详情 |
|------|------|
| **语言** | Go 1.25 |
| **通信** | gRPC (服务间) + REST/Gin (对外 API) |
| **容器** | Docker + Minikube (K8s) |
| **部署** | Helm Chart + ArgoCD (GitOps) |
| **CI/CD** | GitHub Actions → Docker Hub → ArgoCD 自动同步 |

---

## 🏗 架构总览

```
┌─────────────────────────────────────────────────────────┐
│                    Kubernetes Cluster                     │
│                                                          │
│  ┌──────────┐   gRPC    ┌──────────────┐                │
│  │ Gateway   │──────────│ User Service │                │
│  │ (BFF+API) │──────────│ :9091        │                │
│  │ :9638     │  gRPC    ├──────────────┤                │
│  │           │──────────│ Tweet Service│                │
│  │ Gin/REST  │──────────│ :9092        │                │
│  │ JWT Auth  │  gRPC    ├──────────────┤                │
│  │ Sentinel  │──────────│ Follow Svc   │                │
│  │ OTel      │          │ :9093        │                │
│  └──────────┘          └──────────────┘                │
│       │                       │                         │
│       │                       ▼                         │
│  ┌─────────┐   ┌───────┐ ┌───────┐ ┌──────────┐       │
│  │ Ingress │   │ MySQL │ │ Redis │ │ RabbitMQ │       │
│  │ (nginx) │   │ :3306 │ │ :6379 │ │ :5672    │       │
│  └─────────┘   └───────┘ └───────┘ └────┬─────┘       │
│                                          │              │
│                                    ┌─────▼────┐        │
│                                    │ Consumer  │        │
│                                    │ (异步消费) │        │
│                                    └──────────┘        │
│                                                         │
│  ┌──────────────── 可观测性 ──────────────────┐         │
│  │ Prometheus │ Jaeger │ Consul │ Grafana(TBD)│         │
│  └────────────────────────────────────────────┘         │
└─────────────────────────────────────────────────────────┘
```

---

## ✅ 已完成项目

### 阶段 1：核心微服务开发
- [x] **User Service** — 用户注册、登录、JWT 认证、资料管理
- [x] **Tweet Service** — 推文 CRUD、用户时间线、关注 Feed 流
- [x] **Follow Service** — 关注/取关、粉丝列表、关注统计
- [x] **Consumer** — RabbitMQ 异步消息消费（事件驱动）
- [x] **Gateway (BFF)** — API 聚合网关，含 `/full_profile` BFF 端点

### 阶段 2：服务治理
- [x] **gRPC 服务间通信** — 3 个 proto 定义，16 个 RPC 方法
- [x] **Consul 服务发现** — 自动注册与发现
- [x] **Sentinel 熔断降级** — 错误比率 & 慢请求熔断
- [x] **JWT 认证中间件** — 基于 Gin 的 Bearer Token 认证
- [x] **Redis 分布式限流** — 1000 req/min per IP

### 阶段 3：Kubernetes 部署
- [x] **Dockerfile** — 5 个微服务的多阶段构建
- [x] **Helm Chart** — 完整的参数化部署模板
- [x] **Ingress (Nginx)** — 对外暴露 API
- [x] **HPA 自动弹性伸缩** — CPU 80% 触发，1-3 副本
- [x] **资源配额** — requests/limits 设定

### 阶段 4：CI/CD & GitOps
- [x] **GitHub Actions** — 自动测试 → 构建镜像 → 推送 Docker Hub → 更新 Helm values
- [x] **ArgoCD** — 监听 Git 仓库，自动同步集群状态（self-heal + auto-prune）

### 阶段 5：可观测性（部分完成）
- [x] **Prometheus** — 指标采集 + `/metrics` 端点
- [x] **Jaeger** — OpenTelemetry 分布式链路追踪
- [x] **kube-state-metrics** — K8s 集群指标
- [ ] **Grafana** — 数据可视化面板（待部署）
- [ ] **PLG Stack (Loki + Promtail)** — 分布式日志（待部署）

### 阶段 6：业务功能扩展
- [x] **Like System** — 点赞推文 (Redis 缓存 + 异步持久化)
- [x] **Comment System** — 二级评论、评论计数
- [x] **Notification System** — 互动通知 (RabbitMQ + WebSocket 实时推送)
- [x] **Simple Search** — 简易推文搜索 (MySQL Like / FullText)
- [x] **Trending Topics** — 热门话题排行榜 (Redis Sorted Set + 异步计算)
- [x] **User Profile** — 用户资料完善 (头像/简介)
- [x] **Media Upload** — 媒体上传服务 (Local Storage)
- [x] **Retweet System** — 转发推文 (gateway 直连 DB + 前端切换)
- [x] **Profile Tabs** — 个人资料页 Tabs (帖子/回复/媒体/喜欢)
- [x] **Messenger System** — 私信系统 (gRPC + WebSocket 实时聊天)

---

## 🚧 当前阶段：分布式日志系统 (PLG Stack)

### 目标
引入 **Promtail + Loki + Grafana** 构建完整的日志可观测性。

### 任务清单

| # | 任务 | 状态 |
|---|------|------|
| 1 | 部署 Loki (日志存储引擎) | ⬜ 待开始 |
| 2 | 部署 Promtail (日志采集 DaemonSet) | ⬜ 待开始 |
| 3 | 部署 Grafana (可视化面板) | ⬜ 待开始 |
| 4 | 配置 Loki 数据源到 Grafana | ⬜ 待开始 |
| 5 | 配置 Jaeger 数据源到 Grafana | ⬜ 待开始 |
| 6 | Go 日志增强：结构化日志 + TraceID 注入 | ⬜ 待开始 |
| 7 | 日志与链路追踪联动 (Logs → Traces) | ⬜ 待开始 |

---

## 🔮 未来规划

| 方向 | 描述 | 优先级 |
|------|------|--------|
| **KEDA 事件驱动弹性** | 根据 RabbitMQ 队列长度自动扩缩容 Consumer | ⭐⭐⭐ |
| **Service Mesh (Istio)** | 替代 Consul/Sentinel，Sidecar 代理无侵入式治理 | ⭐⭐ |
| **Grafana Dashboard 模板** | 预置 CPU/内存/QPS/延迟/错误率面板 | ⭐⭐⭐ |
| **告警规则 (AlertManager)** | Prometheus 告警规则 + 通知渠道 | ⭐⭐ |
| **安全加固** | RBAC、NetworkPolicy、Secret 管理 | ⭐⭐ |

---

## 🛠 技术栈一览

| 类别 | 技术 |
|------|------|
| 语言 | Go 1.25 |
| Web 框架 | Gin |
| RPC | gRPC + Protobuf |
| 数据库 | MySQL 8.0 (GORM) |
| 缓存 | Redis |
| 消息队列 | RabbitMQ 3.13 |
| 服务发现 | Consul |
| 熔断降级 | Sentinel-Go |
| 链路追踪 | OpenTelemetry + Jaeger 1.66 |
| 指标监控 | Prometheus + kube-state-metrics |
| 容器化 | Docker + Minikube |
| 编排 | Kubernetes (Helm Chart) |
| CI/CD | GitHub Actions |
| GitOps | ArgoCD |
| Ingress | Nginx Ingress Controller |
