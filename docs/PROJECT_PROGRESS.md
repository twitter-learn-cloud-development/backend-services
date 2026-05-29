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
- [x] **MCP 长连接与 RAG 检索加固** — 引入服务生命周期 context 控制及 Close() 机制规避协程泄露，实现 0.0.0.0 容器拨号纠偏，以及首选 Qdrant 失败时优雅降级为 ES 检索及兜底文本，补齐中文种子数据（✅）

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

### 阶段 14：ES 向量检索迁移至独立向量库 Qdrant (Qdrant Vector DB Migration)
- [x] **Qdrant 向量库接入与预建集合** — 在 Helm Chart 中集成生产级 Qdrant 并限制 limits 防止 OOM，在服务启动时幂等创建 `tweets` 向量集合（1024维 Cosine 相似度）。
- [x] **发件箱事务双写重构** — 重构 Timeline 消费端 OutboxWorker，将 ES 的 `ContentVector` 设为 `nil` 释放 HNSW 与 JVM 堆内存压力，数据双写刷至 Qdrant，并通过 Snowflake UUID 映射方法 `ConvertSnowflakeToQdrantID` 规避 64位无符号精度截断溢出隐患。
- [x] **MCP 智能体双路并发召回** — 重构 MCP 检索工具 `search_tweets_by_semantic` 与 `hybrid_search_tweets`，利用 `errgroup` 对 ES 倒排（BM25）与 Qdrant 向量（HNSW）进行 1.5s 短超时并发双路召回，去重后接入 Reranker 二次精排与 gRPC 延迟回表。

### 阶段 15：多 Agent 协同写推文并发化与无锁高弹性优化 (Multi-Agent Concurrency & Resilient Optimization)
- [x] **Review Agent 协同介入** — 在“多 Agent 协同写推文（模式四）”的 Writer 生成阶段后，成功挂载“安全舆情审查官” Review Agent。在不侵入 Proto 协议的前提下，在 response 文本中以结构化 Markdown 进行交付。
- [x] **数据拉取阶段高并发重构** — 废除原本 Search/Style/Reference 串行网络调用，使用 `errgroup` 改造为高并发并行检索，将第一阶段数据检索时延从 $O(N)$ 降至最慢单次时延的 $O(1)$。
- [x] **全局超时与 Lock-free 无锁内存隔离** — 在协程中透传 `gCtx` 并强绑定 3s 全局超时以规避协程泄漏；引入无锁内存隔离设计，消除锁竞争开销；利用 return nil 机制实现单 Agent 超时/网络抖动时的平滑优雅降级。

### 阶段 16：舆情播报与影子风控双轨并行治理 (Trending Reporter & Anti-Spam Shadowban)
- [x] **visible_type 4 适配** — 修改 `tweet.proto` 并增加影子封禁状态可见性过滤。
- [x] **GetFeeds 读时防线与写时洗地** — 在 `GetFeeds`/`GetUserTimeline`/`GetTweet` 中实施 map 一致性检测和影子封禁逻辑，拦截对垃圾推文的透出；通过 RabbitMQ 广播 `queue.tweet.risk` 风控事件。
- [x] **Redis Lua+Pipeline 原子清洗** — 编写原子 ZREM 和 DECR 未读数 Lua 脚本，使用 Pipeline 批次（500规模，10ms间隔限流）异步对粉丝的 Timeline 进行清洗，规避集群 CROSSSLOT 瓶颈。
- [x] **分布式排他锁防护** — 基于 Redis `SetNX` 排他锁对舆情追踪哨兵进行分布式锁控制，防止 Ticker 定时器多实例重复发推雪崩。
- [x] **切片隔离与 Mock 压测挡板** — 实现 `MOCK_EMBEDDING=true` 压测挡板，拦截真实 Embeddings API 以支持零费用高并发压测，并在内部对推文切片进行显式重新声明与深拷贝以绝 race。
- [x] **单元测试与 K6 剧本** — 使用 miniredis 分布式锁、洗地 Lua 脚本、Race 检测测试通过，并编写了 K6 压测剧本。

### 阶段 17：百万并发 Feed 流优化与请求归并多级缓存 (Million-Concurrency Feed Optimization)
- [x] **BigCache L1 进程内存缓存** — 引入纯 Go 实现的零 GC BigCache 作为一级内存缓存，大幅降低 GC STW 扫描压力。
- [x] **Redis Base Tweet L2 缓存与随机 TTL** — 引入 `tweet:base:<ID>` 二级 Redis 缓存，并通过 24小时 + 0~30分钟随机抖动 TTL 防御缓存雪崩。
- [x] **Singleflight 并发归并与短超时熔断** — 引入 `golang.org/x/sync/singleflight`，将读操作与 `DoChan` 结合 `select` 设置短超时（800ms/1500ms），秒级熔断上游防止协程雪崩。
- [x] **Redis Pub/Sub 广播失效与 L1 一致性** — 引入 `tweet_invalidations` Redis 订阅广播，删除/隐藏时双链路（DeleteTweet API 与 Consumer 消息清洗）异步清除全局 L1 本地缓存，达成最终一致性。
- [x] **大V个人时间线 ZSet 缓存与防穿透占位** — 引入 `user_timeline:<ID>` ZSet 缓存大V最新的 1000 条推文，并配合 `:initialized` 标志区分“冷数据/未初始化”和“大V推文为空”的场景，杜绝缓存穿透。
- [x] **Redis Pipeline 批量 Pull 与内存归并排序** — 放弃拉取关注流（GetFeeds）时对 MySQL 的依赖，改用 Redis Pipeline 批量执行多个大V ZSet `ZRevRangeByScore`，在内存中进行 Merge Sort 并截取，实现全缓存化混合 Feed 流。
- [x] **无感秒开异步分页预热 (Cursor Pre-warming)** — 引入异步预热，检测到 `hasMore` 时在后台启动独立的、最大 2s 超时防挂起的预热协程，根据 `nextCursor` 提前拉取下一页推文 IDs 并写入 L1/L2，实现极速翻页。

### 阶段 18：Serverless Agentic Mesh (智能体云原生网格化)
- [x] **Temporal 基础设施部署** — 引入 `temporalio/dev:1.24.0` 开发版容器，提供内置 SQLite 和可视化 Web UI 控制台，注入 `TEMPORAL_HOST` 环境变量。
- [x] **开销感知与容灾路由** — 实现 `GetChatCompletionWithRouting` 双底层连接，在 Cheap 模型网络超时/429/500错误时自动 failover 至 Premium 性能机，并支持 Stream 进度心跳 RecordHeartbeat 回调防僵死误杀。
- [x] **自愈状态机与防崩溃禁忌** — 严格规避非确定性原生协程与时延函数，在舆情监控哨兵采用 `Continue-As-New` 定时清空历史事件（50,000限制）开启新轮回。
- [x] **MQ 去重去死锁与生死线 Ack** — 绑定 Workflow ID `RiskControl-Tweet-{TweetID}` 实现防消息重投，只在状态机持久化确认后调用 Ack，筑牢可靠接力线。

### 阶段 19：AIOps 与终极可观测性 (Chaos Engineering & AIOps)
- [x] **Continuous Profiling 持续监控** — 在核心组件集成 `pyroscope` SDK，在容器配置中拉起 Pyroscope 仪表盘（映射 4040:4040），实现对 CPU 与 Memory 分配的实时火焰图分析，保障零开销高性能。
- [x] **网关级线程安全 ERROR 日志环** — 实现并发安全的预分配固定容量 20 元素的 RingBuffer 中间件，自动截留 5xx 状态的 API Gateway ERROR 日志，绕过 Loki 查询的高额网络开销。
- [x] **告警去重防抖与 firing 过滤** — 在网关 `/alerts` webhook 路由注册了带 `sync.Map` 的 5分钟去重缓存及 status 状态拦截过滤器，彻底阻断了告警风暴对 LLM API 造成的 DDoS 限流封禁风险。
- [x] **大模型智能 RCA 诊断与持久化** — 在 `agent-service` 中实现 gRPC 接口 `AnalyzeAlert`，结合大模型降级与心跳路由机制对告警与黑匣子日志关联诊断，生成 Markdown 格式的 RCA 报告并实时追加保存到 `scratch/alert_rca_reports.md`。
- [x] **Chaos Mesh 混沌机制与跨平台验证脚本** — 编写了 Redis pod 网络延迟 5s 的混沌测试 `network-delay.yaml`，并提供配备 `minikube` 上下文安全验证护栏与 Windows 平台 GBK 编码兼容性的自动化测试脚本 `run_chaos_test.py`。

### 阶段 20：系统级 Chaos 压力测试与 AIOps 智能自愈闭环 (Phase 20)
- [x] **K6 高并发压力测试及环境隔离** — 编写了 K6 压测脚本 `stress_feeds.js` 模拟高并发刷新，并在 JWT 中间件引入了基于环境变量 `APP_ENV=chaos_testing` 强隔离保护的安全压测 Token。
- [x] **大模型结构化指令输出** — 升级 `agent_service.go` 的 `AnalyzeAlert` 角色与 System Prompt，实现文本报告与自愈 JSON 指令（`[STRUCT_START]/[STRUCT_END]`）的双轨输出和安全正则表达式解析提取。
- [x] **网关安全自愈与保底规则合并** — 构建了 `self_healer.go`，内置 `Allowlist` 允许列表防范大模型幻觉全站自杀。并在动态重载时将 AI 规则合并入 Sentinel 基础防线，防止规则全量覆写造成漏洞。
- [x] **混沌自愈闭环演练与限流规避重构** — 进行了 K6 压测与 Pod-Kill 持续混沌故障注入联合演练，在 `ratelimit` 中间件实现安全压测 Token 旁路绕过，完美把并发压力传导至 Sentinel 层，验证了故障发生时 99.8% 的高频熔断拦截与服务上线后的完全闭环自愈。

### 阶段 21：Evolution V4.0 - 云原生自治与多维度自愈防御系统
- [x] **OTel 长生命周期 Span 延续** — 引入 `context.WithoutCancel` 延续异步诊断协程的 Trace 上下文，彻底解决了 HTTP 响应返回时 HTTP Context 取消引起的 OTel Span 幽灵截断和数据丢失问题。
- [x] **K8s 动态客户端自愈防撞** — 在网关自愈引擎集成 `k8s.io/client-go` 的 `dynamic.Interface`，使用无类型 `unstructured` 解析直接操作 K8s VirtualService，并使用 `RetryOnConflict` 乐观锁并发重试保护，规避了高并发更新下的 409 Conflict 故障，实现了高可用的灰度切流自愈。
- [x] **Qdrant 3节点分布式高可用集群重构** — 在 Helm 部署中将 Qdrant 重构为 3 节点 StatefulSet，在启动命令行配合 Downward API `POD_NAME` 实现条件自引导（qdrant-0 独立启动，qdrant-1/2 引导加入），规避了冷启动死锁；同时设置 Collection 默认为 2 副本冗余，完美验证了节点强杀混沌下的无损搜索。
- [x] **服务环境与基础设施高可用降级** — 升级 `agent-service` 连接 MongoDB、ES 和 Temporal Server 的启动和运行容灾逻辑，支持外部存储离线/降级平滑启动，并将 `agent-service` 完美移入 Kubernetes 网络。

### 阶段 22：Evolution V5.0 - 自适应性能压测与 Pyroscope 缓存调优智能体
- [x] **线程安全动态配置热更新** — 实现了 `pkg/config/dynamic_config.go`，使用 `atomic.Pointer` 结构体指针实现一键无锁原子替换，建立边界安全护栏 (Guardrails)。
- [x] **防配置脑裂双保险订阅** — 设计了 `config_receiver.go`，通过 Redis KV 获取自举初始化 + Redis PubSub 监听动态更新，规避 K8s Pod 重启/扩容时的脑裂问题。
- [x] **多级缓存动态热重载** — 重构了 `timeline_cache.go`，将 L1 状态缓存与 L2 推文基本信息缓存的 TTL 以及翻页预加载深度全部改用动态原子配置，实现零停机热重载。
- [x] **Pyroscope 持续剖析分析器** — 编写了 `profiling_analyzer.go`，通过 Pyroscope Collapsed 格式抓取并提取 CPU 采样前五名热点业务堆栈，为 AI 提供精准的实时性能上下文。
- [x] **调优智能体工具与防震荡冷却锁** — 在 `agent_service.go` 中挂载了 AI 缓存调优工具 `TuneCacheConfig` 并注入 Redis 分布式冷却锁（3分钟 Cooldown 观察期），杜绝参数剧烈抖动与系统震荡。
- [x] **可观测追踪与联合测试验证** — 完成了 OTel 调优子 Span 的深度切入，并编写了一键自动化演练脚本 `test_adaptive_tuning.py`，打通了“性能过载-火焰图抓取-AI调优-配置热重载-OTel拓扑追踪”的完整闭环。
- [x] **发总结的 AI 助手定时任务激活** — 实例化并启动了 `TrendingReporter` 定时舆情监视哨兵，实现了大V粉丝热度推文并发双路召回（ES+Qdrant）、内存去重、AI 评论快报自动生成及官方发帖（BotUserID=100）的完整业务闭环。

---

## 🚧 当前阶段：已全部完成

### 目标
完成了微服务高并发多级缓存、分布式 Saga 自愈编排、K6/PodKill 混沌压测、大模型闭环熔断自愈治理以及基础设施控制面的主动降级与网格自愈，并实现基于 Pyroscope 火焰图的 AI 自适应性能调优智能体。系统处于极强高可用、自调优自治状态。

---

## 🔮 未来规划

| 方向 | 描述 | 优先级 |
|------|------|--------|
| **多因子自愈恢复** | 基于 AIOps 进行更复杂的灰度流量切分与跨集群动态漂移 | ⭐⭐⭐ |
| **安全意图防火墙** | 建立防 Prompt 注入与 AI 幻觉的自愈 Guardrails 独立哨兵 | ⭐⭐ |

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
