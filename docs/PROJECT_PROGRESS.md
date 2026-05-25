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

### 阶段 5：可观测性（已完成）
- [x] **Prometheus** — 指标采集 + `/metrics` 端点
- [x] **Jaeger** — OpenTelemetry 分布式链路追踪（已修复 API Gateway 层 OTel Trace 级联穿透截断问题）
- [x] **kube-state-metrics** — K8s 集群指标
- [x] **Grafana** — 数据可视化面板 (Provisioning Dashboard)
- [x] **PLG Stack (Loki + Promtail)** — 分布式日志与 Trace 级联跳转

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

### 阶段 7：AI Agent Service
- [x] **Agent 核心能力** — 4 种 AI 模式（直接对话 / RAG 搜索 / AI 写推文 / 多 Agent 协作）
- [x] **MCP Server** — 5 个 Tool（语义搜索 / 混合搜索 / 发推 / 获取用户推文 / 批量获取推文）
- [x] **对话历史持久化** — MongoDB 存储对话会话和消息，支持多轮上下文（Phase 1 ✅）
- [x] **模型信息 + 文件解析** — GetModelDetailedInformation / AnalysisFiles（Phase 2 ✅）
- [x] **MCP Tools 模块化重构** — 从 server.go 拆分到 tools 包（Phase 3 ✅）
- [x] **Gateway 路由补全** — 对话列表/详情/模型信息/文件解析等（Phase 4 ✅）
- [x] **前端 AI Agent 页面** — Vue 3 交互式 Agent 会话页面（Phase 5 ✅）
- [x] **MCP Tool 鉴权隔离与 SSE 连接池** — 废除 LLM user_id 权限越权漏洞，基于 RWMutex 和 DCL 实现长连接复用与异常自愈（Phase 6 ✅）
- [x] **RAG 语义二次精排 (Reranker)** — 引入 Rerank 漏斗检索机制，支持阿里百炼、硅基流动并实现基于 Bigram Jaccard 的 LocalMock 与 1.5s 超时优雅降级，降低 Context 噪声并杜绝幻觉（P2 ✅）

### 阶段 8：分布式日志系统 (PLG Stack)
- [x] **Loki & Promtail 部署** — 本地一键拉起 Loki 与 Promtail 服务，通过 Docker SD 实现容器日志自动收集与 Pipeline 解包
- [x] **Grafana 预置配置** — 基于 provisioning 预置 Loki/Jaeger/Prometheus 数据源，简化配库流程
- [x] **Logs ➔ Traces 级联跳转** — 配置 derivedFields 提取日志中的 trace_id，生成一键跳转至 Jaeger 的超链接
- [x] **Go 结构化日志与协程 Trace 延续** — 重构 follow-service 并使用 OTel Span 传递在异步拉取协程中传递 Trace 上下文，修复 Trace 截断漏洞

### 阶段 9：消息队列韧性治理 (Resilience Governance)
- [x] **基础库参数扩展** — 在 `rabbitmq.go` 中引入 `DeclareQueueWithArgs`，完美兼容现有功能，支持自定义控制参数声明队列。
- [x] **指数退避延迟重试** — 废除业务消费端 `msg.Nack(false, true)` 的即时重入队，引入 `retry.events` 交换机与带有单条消息 Expiration 的重试暂存队列，实现指数级退避重试（1s, 2s, 4s...），解除 100% CPU 重试风暴对消息通道的阻塞。
- [x] **死信队列分流归档** — 限制核心队列消息重试次数上限为 3 次，超限后消息自动分流到专用死信队列 `*.dlq` 中，实现非阻塞的错误隔离，彻底杜绝数据静默丢失（Silent Loss）。

### 阶段 10：微服务数据强隔离与 API Gateway 重构 (DB Isolation)
- [x] **API Gateway 去数据库化** — 彻底下架 GORM 初始化、数据库连接及表自动迁移，Gateway 转型为纯粹的 gRPC BFF 代理。
- [x] **Notification 业务下沉** — 扩展 `notification-service` 的 gRPC 能力，将通知列表、标记已读及未读数等业务全部移入服务内层。
- [x] **Bookmark / Retweet 子域收口** — 书签与转发的数据库操作全面归宿至 `tweet-service`，在服务底层批量聚合点赞/书签/转发等交互状态，杜绝 BFF 二阶段 SQL 拼装。
- [x] **时间线合并下沉** — 推文与转发推文的混合分页排序逻辑由 `tweet-service` 统一计算返回，Gateway 仅负责用户信息的最终 Enrich 聚合。

### 阶段 11：大V推特混合 Feed 流与双阈值防抖架构 (Hybrid Push/Pull)
- [x] **Redis 缓存大V列表** — 引入 `global:celebrities` 与 `user:celebrities:ID` 缓存，彻底干掉大V读写链路上的 SQL 聚合计算，消除了百万粉丝高频拉取导致的数据库 CPU 100% 雪崩隐患。
- [x] **双阈值防抖门限** — 引入 5000 粉丝晋升 / 4500 粉丝降级的非对称滞后带宽防抖，杜绝粉丝在阈值点边缘反复关注/取关引起的状态频繁更新抖动。
- [x] **晋升与降级异步广播** — 粉丝数越过阈值时通过 Redis Pipeline 批量异步更新/清理老粉丝的大V关注缓存；同时消除普通用户写扩散原本硬编码限 1000 活跃粉丝的推送截断 Bug。
- [x] **定期校准对账任务** — 编写定时对账任务 `SyncGlobalCelebrities`，在系统空闲期通过差分对齐 DB 与 Redis 大V及关联关系状态，确保数据最终一致性。
- [x] **大V状态 L1 本地二级缓存化** — 在 Go 进程内存中集成了带 TTL（1分钟）和并发锁的 `celebrityLocalCache`，高频大V状态查询优先“在本地内存解决战斗”，Redis 查询 QPS 降低数个数量级。

### 阶段 12：ES 向量同步事务发件箱模式 (Transactional Outbox)
- [x] **发件箱表与 GORM 仓储实现** — 引入 `outbox_tasks` 任务关系表与基于 ANSI SQL 标准 `CASE WHEN` 兼容的指数级退避延迟提取算法，100% 兼顾 SQLite 和 MySQL。
- [x] **写扩散与 ES 向量化同步解耦** — 重构 Timeline 消费端发帖消费，将原本的同步调大模型 Embedding 接口改为持久化写入发件箱任务，消费端平均延迟降低至微秒级，提高队列吞吐量。
- [x] **后台守护对账协程 (OutboxWorker)** — 实现后台轮询守护协程，拉取任务进行向量化并写入 ES。
- [x] **指数级退避与存储空间物理清理** — 针对失败的同步任务使用指数级延迟重试，达到 5 次上限后封存用于人工审计；针对成功同步的任务物理删除 (DELETE) 以保护数据库表空间。

### 阶段 13：云原生生产级深度治理 (Cloud-Native Production Governance)
- [x] **Redis ZSet Trending 热 Key 优化** — 引入本地 HashtagBatcher 并发缓冲与异步 pipeline 定时落盘，优雅退避单点 CPU 雪崩。
- [x] **AlertManager 黄金指标告警与 Webhook 推送** — 配置 QPS 骤跌、高延迟、高错误率三大告警规则，并在 API Gateway 注入带安全 Header 校验的 `/alerts` 告警接收端。
- [x] **Service Mesh (Istio) 流量管理配置** — 新增 VirtualService 与 DestinationRule，支持 v1/v2 子集 90:10 灰度发布流量切分及基于 OutlierDetection 的生产级熔断防护。
- [x] **网络边界安全防护 (NetworkPolicy)** — 拆分 MySQL、Redis、RabbitMQ 专用网络白名单，并在服务侧 NetworkPolicy 兼容 Istio 控制面、双向 TLS 与 Envoy 拦截端口，严格实行最小权限容器隔离。
- [x] **Grafana Dashboard 看板预置与抓取加固** — 开启 Grafana Sidecar 动态加载 ConfigMap，并在 Deployment 模板注入 Prometheus Scrape 注解以修复抓取盲区，同时将 gRPC 核心服务指标（QPS, Latency, Error Rate）纳入全站看板。

---


## 🚧 当前阶段：KEDA 事件驱动弹性

### 目标
根据 RabbitMQ 队列中积压的消息长度，通过 KEDA (Kubernetes Event-driven Autoscaling) 实现 Timeline 异步写扩散消费者的自动水平扩缩容 (Autoscaling)。

### 任务清单

| # | 任务 | 状态 |
|---|------|------|
| 1 | 在 K8s 中部署并启用 KEDA Controller | ✅ 已完成 (已在 Minikube 中部署 KEDA Controller) |
| 2 | 定义 RabbitMQ 凭证与 ScaledObject | ✅ 已完成 (定义了安全凭证 Secret 与 ScaledObject 规范) |
| 3 | 编写 KEDA 部署资源声明，并在 Helm Chart 中集成 | ✅ 已完成 (在 values.yaml 中添加配置开关，支持副本条件隔离) |
| 4 | 进行高并发写扩散压测，验证 Consumer 的自动伸缩与缩容至 0 | ✅ 已完成 (成功通过并发压测触发 consumer 从 0 扩容至 5 并最终缩容至 0 副本) |

---

## 🔮 未来规划

| 方向 | 描述 | 优先级 |
|------|------|--------|
| **KEDA 事件驱动弹性** | 根据 RabbitMQ 队列长度自动扩缩容 Consumer | ⭐⭐⭐ |
| **Service Mesh (Istio)** | ✅ 适配并定义灰度流量管理 VS/DR 与熔断拦截规则 | ⭐⭐ |
| **Grafana Dashboard 模板** | ✅ 预置 CPU/内存/QPS/延迟/错误率面板 | ⭐⭐⭐ |
| **告警规则 (AlertManager)** | ✅ 配置三大黄金监控指标告警规则与安全 Webhook | ⭐⭐ |
| **安全加固** | ✅ 定义 MySQL/Redis/RabbitMQ/微服务的 NetworkPolicy 白名单 | ⭐⭐ |

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
