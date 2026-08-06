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
│  │ Prometheus │ Jaeger │ Consul │ Grafana     │         │
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
- [x] **可视化拖拽工作流编辑器前端页面** — 基于 Vue Flow 完成组件拖拽、端点自适应连线、属性配置变量树感知与安全删除交互闭环（✅）


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
- [x] **发件箱表与 GORM 仓储实现** — 引入 `outbox_tasks` 任务关系表与 `CASE WHEN` 指数退避；当前多副本领取明确依赖 MySQL 8 `FOR UPDATE SKIP LOCKED`，不再宣称 Claim 路径兼容 SQLite。
- [x] **写扩散与 ES 向量化同步解耦** — 重构 Timeline 消费端发帖消费，将原本的同步调大模型 Embedding 接口改为持久化写入发件箱任务，消费端平均延迟降低至微秒级，提高队列吞吐量。
- [x] **后台守护对账协程 (OutboxWorker)** — 实现后台轮询守护协程，以原子 Claim/Lease 并发执行向量化与 ES/Qdrant 写入，旧 Attempt 不能覆盖新租约结果。
- [x] **指数级退避与有界收据清理** — 失败任务按指数延迟重试，达到 5 次上限后封存用于人工审计；成功任务作为去重收据保留 72 小时，再按有界批次物理清理。

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

### 阶段 15：多 Agent 协同写推文原型（真实性校准）
- [x] **Search/Style/Reference/Writer 管线原型** — 模式四已能收集领域、作者和指定参考内容，再交给 Writer 生成候选。
- [ ] **Review Agent 独立执行** — 当前主链路没有独立 Review Agent；需在统一 Runtime/Profile 后补齐。
- [ ] **数据拉取阶段并发化** — 当前主链路仍按 Search → Style → Reference 串行调用，不宣称已完成并发优化。
- [ ] **Lock-free 状态隔离** — 当前 Workflow Blackboard 使用锁保护可变状态；单写者不可变状态将在强化计划 P4 实现。

### 阶段 16：舆情播报与影子风控双轨并行治理 (Trending Reporter & Anti-Spam Shadowban)
- [x] **visible_type 4 适配** — 修改 `tweet.proto` 并增加影子封禁状态可见性过滤。
- [x] **GetFeeds 读时防线与写时洗地** — 在 `GetFeeds`/`GetUserTimeline`/`GetTweet` 中实施 map 一致性检测和影子封禁逻辑，拦截对垃圾推文的透出；当前由 Agent Worker 的 `queue.tweet.risk` 直接订阅原始 `tweet.created`，Timeline Consumer 不再二次广播风控事件。
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

### 阶段 19：AIOps 与可观测性实验 (Chaos Engineering & AIOps)
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

### 阶段 23：云原生对象存储适配与数据流重构 (MinIO Object Storage)
- [x] **MinIO 对象存储自举集成** — 网关 (gateway) 集成官方 MinIO SDK，启动时自动检测并幂等创建 `twitter-media` 存储桶，自动下发 JSON 公共只读 (Public Read-Only) 访问策略。
- [x] **流式上传与透明网络适配** — 重构网关 `/api/v1/upload` 接口，废除本地磁盘落盘逻辑，将文件流流式上传至 MinIO，并妥善解决网关容器内写入 Endpoint (`minio:9000`) 与宿主机公开访问 Base URL (`http://localhost:9000`) 异构网络访问问题。

### 阶段 24：大厂级动态加权防刷热搜与时间衰减模型 (Dynamic Trending & Anti-Spam Decay)
- [x] **GSE 纯 Go 中文分词与实体提取** — 引入纯 Go 实现的轻量级分词器 `go-ego/gse`，避免 CGO 对 Docker 容器交叉编译的干扰，支持开箱即用的分词与 NER。
- [x] **48小时 TTL 预映射缓存** — 发推时提取实体词并写入 `tweet_tags:{tweet_id}` (48h TTL)，将点赞/评论的二级加分完全收拢在内存层，实现数据库 0 压力。
- [x] **1小时滑窗防刷 (W&A)** — 引入用户维度的 `lock:user_tag_count:{uid}:{tag}` 进行 1 小时限频，同一个 UID 对同一个词超过 3 次的交互不再计分，有效抵御水军刷榜。
- [x] **多副本防雪崩分布式锁衰减** — 引入 Redis 分布式锁 `lock:trends_decay`，在多副本部署下每分钟仅允许单实例抢锁并对 ZSet 进行 0.95 衰减，同时即时裁剪前 100 名之外的长尾热词。

### 阶段 25：Flutter 移动端 App 开发 (V6.0) (已完成)
- [x] **基础设施与环境适配** — 采用 `adb reverse` 统一真机与模拟器调试时的本地网关端口映射，规避多端网络拓扑冲突；在客户端实现相对媒体路径补全，彻底解决 MinIO 绑定痛点；基于 `--dart-define-from-file` 配合 json 配置实现编译期环境隔离。
- [x] **登录与鉴权** — 编写 Auth 响应模型和本地 Token 持久化存储，利用 Riverpod v3 Notifier 机制自动引导登录态并自愈登出。
- [x] **首页信息流与发帖上传** — 封装 TweetCard 极佳适配 Twitter 图片布局逻辑，实现触底 Cursor 自动分页加载；集成 image_picker，调用后端 `/api/v1/upload` 流式上传至 MinIO。
- [x] **互动、详情与个人主页** — 支持点赞、转发、书签、评论等交互按钮的即时 UI 乐观更新；支持 `TweetDetailScreen` 层级评论展现；构建 `ProfileScreen` 并配合 Notifier.family 提供发帖、媒体、喜欢三 Tab 各自 Cursor 分页。
- [x] **热搜与 AI 智能体** — 支持 Trends 趋势榜与文字检索；编写 `AgentChatScreen` 支持 RAG 向量卡片、对话以及 AI 创作草稿一键确认发布。
- [x] **实时通知与私信聊天室** — 接入 WebSocket 服务端推送，实时广播私信（`message`）事件并让 `ChatRoomScreen` 对话气泡动态流式追加；实现实时通知（`like`/`comment`/`follow`）红点徽标提示及 `NotificationScreen` 分类列表跳转。

### 阶段 26：微服务通信与连接性能深度治理 (gRPC Connection Optimization & Eager Dial)
- [x] **Consul 发现 gRPC 重构** — 将 `auth-service` 对 `user-service` 的连接由原本硬编码的容器内直连升级为通过 Consul 注册中心自动发现与 `round_robin` 负载均衡。
- [x] **连接预热 (Eager Connection)** — 在 API Gateway 和 `auth-service` 的 gRPC 客户端引入 `conn.Connect()` 主动长连接预热，将 DNS 首次解析超时及 TCP/HTTP2 建连成本消灭在系统初始化阶段，彻底扫除首笔登录请求超时。
- [x] **链路追踪断裂修复** — 在 `auth-service` 连接 `user-service` 时补齐 OTEL Client stats handler 拦截器，完美在 Jaeger 中连通 `user-service` Span，完成闭环分布式追踪。

---

## 🚧 当前阶段：已全部完成


### 目标
完成了微服务高并发多级缓存、分布式 Saga 自愈编排、K6/PodKill 混沌压测、大模型闭环熔断自愈治理以及基础设施控制面的主动降级与网格自愈，并实现基于 Pyroscope 火焰图的 AI 自适应性能调优智能体。同时，全面实现了具备离线持久化、BFF 多层缓存对接、WebSocket 实时聊天与分类推送等特性的生产级 Flutter 移动端手机 App。


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

---

## 2026-06-25 P1 工作流 DSL 闭环开发

- [x] **DSL 持久化接口**：新增 Agent Workflow 的 proto、gRPC、Service、Repository 与 Gateway HTTP 接口，支持创建、更新、列表、详情查询，并通过 MongoDB `agent_workflows` 集合按 `user_id` 隔离存储。
- [x] **DAG 后端执行入口**：新增运行记录模型与 `agent_workflow_runs` 集合，`RunWorkflow` 会加载 DSL、编译 DAG、注入用户上下文 Guardrail、执行节点并回写运行状态、输出快照与错误信息。
- [x] **前端画布接入后端**：`WorkflowEditor.vue` 的保存按钮已调用 `/agent/workflows`，运行按钮会自动保存未持久化 DSL 后调用 `/agent/workflows/:id/run`，并将 `tool_name` 写入工具节点属性，保证后端可稳定解析工具类型。
- [x] **Go 验证补测（2026-07-14）**：当前工具链已可用，`go test ./internal/module/agent/... ./cmd/agent-service` 通过。

## 2026-06-25 P2 认知 RAG 主链路接入

- [x] **Cascade Router 工程化重构**：重写 `workflow/rag/router.go`，恢复清晰的中英文词典路由，并保留语义锚点与 LLM JSON fallback，路由失败时自动降级为全局知识检索。
- [x] **三层记忆管理器可降级化**：重写 `workflow/rag/memory.go`，支持 L1 用户画像、L2 Qdrant 私有 episodic memory、L3 ES + Qdrant 混合召回；任一依赖缺失时跳过对应路径，不阻断对话主流程。
- [x] **Chat 认知上下文注入**：新增 `cognitive_context.go`，通过 `SetCognitiveEngine` 将 Router/Memory 以可插拔方式挂入 `AgentService`，普通 Chat 会在 system prompt 中注入画像、记忆与知识上下文，并记录路由元数据。
- [x] **摘要结晶化写入**：对话回复持久化后异步提炼当前回合为 L2 episodic memory，写入用户隔离的 Qdrant collection；失败只记日志，不影响用户请求。
- [x] **Go 验证补测（2026-07-14）**：Agent Service 与认知 RAG 已纳入 Agent 全包测试并通过。

## 2026-06-25 P3 工作流运行可观测性与节点级 SLA Trace

- [x] **Scheduler 节点级 Trace**：调度器新增 `NodeTrace`，记录节点 `pending/running/success/failed/skipped`、开始/结束时间、耗时和错误信息，支持路由分支跳过节点可视化。
- [x] **运行记录输出增强**：`RunWorkflow` 的 `output_json` 同时保留原 blackboard 顶层字段，并新增 `blackboard` 与 `traces` 字段，前端和排障工具可以直接消费节点运行轨迹。
- [x] **前端节点状态回填**：工作流画布运行后会读取 `run.output.traces`，逐节点更新状态，不再只按整体成功/失败粗粒度染色。
- [x] **Trace 单元测试补充**：新增调度器 trace 测试，覆盖成功节点与 router 未激活分支的 skipped 状态。
- [x] **Go 验证补测（2026-07-14）**：Scheduler 测试通过，`go test -race ./internal/module/agent/workflow/engine` 通过。
> 范围校准：本阶段只完成 Workflow NodeTrace，不等于 Agent Run/Step/LLM/Tool 全链路 Trace；统一 Agent 可观测性仍属于强化计划 P5。
## 2026-06-25 P4 工作流挂起恢复与 Checkpoint

- [x] **Engine Checkpoint 抽象**：新增 `WorkflowCheckpoint` 与 `SuspensionError`，调度器支持 `suspended` 节点状态、黑板快照导出以及 `ExecuteFromCheckpoint` 恢复执行，恢复时不会重跑已成功或已跳过的上游节点。
- [x] **运行记录挂起状态持久化**：`agent_workflow_runs` 增加 `checkpoint_json`、`waiting_node_id`、`resume_token`、`suspended_at` 字段，并补充按 `user_id/status/suspended_at` 查询的 MongoDB 索引。
- [x] **Wait 节点与内部恢复入口**：DSL 新增 `wait` 节点类型，用于人工审批、外部 MCP 回调和长阻塞任务；`AgentService.ResumeWorkflowRun` 已支持从 checkpoint 水化并继续执行下游。
- [x] **前端画布接入**：将原先不被后端支持的 `approve` 节点统一为 `wait` 节点，补充 reason/resume_token 属性配置，并支持 `suspended` 节点状态展示。
- [x] **Temporal 自定义工作流决策门**：删除未注册且重复实现拓扑/黑板/路由/重试/审批语义的实验 Bridge。用户 DAG 明确使用本地统一 IR 与持久状态机；Temporal 只保留真实注册的风控和热点长任务。
- [x] **验证**：前端历史构建验证通过；2026-07-14 补跑 Agent 全包 Go 测试与 Workflow Engine race 测试通过。
## 2026-06-25 P4 Follow-up: LLM Provider Customization

- [x] **LM Studio 默认模型修正**：移除 `qwen3.6-plus` / `deepseek-chat` 旧默认值，本地工作流默认改为 `qwen2.5-3b-instruct`，DashScope 对话默认改为 `qwen-plus` / `DASHSCOPE_MODEL_CHAT`。
- [x] **工作流 LLM 节点可定制 API**：`LLMChatTool` 支持从节点属性读取 `provider`、`base_url`、`credential_ref`、`model`、`system_prompt`、`max_tokens`，兼容 LM Studio、DashScope 和 OpenAI-compatible 自定义服务；明文 `api_key` 已在 P2 被禁用。
- [x] **前端属性面板补齐**：Workflow Editor 的 LLM 节点提供 Provider、Base URL、Credential Ref、Max Tokens 配置项，用户可在单节点维度切换模型服务且不会把密钥写入 DSL。
- [x] **验证**：前端历史构建验证通过；2026-07-14 补跑 Agent 全包 Go 测试通过。

## 2026-06-26 课程设计报告书写与 Word 自动化导出 (Course Design Report)

- [x] **详细 Markdown 报告撰写**：在 `docs/COURSE_DESIGN_REPORT.md` 中为项目编写了完整的 9 章节课程设计报告。突出论述了系统的微服务模块边界、多级缓存机制、JWKS 零共享密钥验签、以及 Canal CDC 事务发件箱模式（对传统对称 JWT 和同步双写消息队列的设计优势），内容严谨学术，代码注释行级解析。
- [x] **Word DOCX 导出自动化**：编写了 `scripts/generate_report_docx.py` 转换脚本，基于 `python-docx` 库实现了对 Markdown 中标题级联、无序/有序列表、阴影边框引用块、细灰网格表格以及 Consolas 等宽字体代码容器的精美排版解析。
- [x] **深度扩写与百页级架构剖析**：响应用户对报告字数和深度的要求，重新读取了核心中间件（JWKS）、MQ 消费者（Timeline 推拉结合防雪崩）、事务发件箱（Outbox）以及 AI 智能体编排（Temporal + Kahn 拓扑排序）的核心源码，生成了附带真实源码注释和极深原理解读的万字级报告。
- [x] **生成与双端验证**：在项目根目录下成功输出了 Word 文档 `（6）课程设计报告（含任务书）_生成版.docx`，并通过 Python 代码级物理校验确认其完全生成。

## 2026-06-27 Agent 会话一致性与工作流策略扩展

- [x] **历史消息协议统一**：对话详情新增标准 `role/content` 字段并保留 `question/response` 兼容字段，前端统一归一化渲染；重复点击当前会话可重新拉取，避免列表选中但消息区为空。
- [x] **自定义工作流会话持久化**：AI 助手运行工作流时显式传入 `persist_dialogue` 和 `dialogue_key`，运行结果写入同一 MongoDB 对话仓储；编辑器测试默认不落聊天历史。
- [x] **模型目录职责修正**：AI 助手模型接口只返回 Chat Completion 模型，Embedding 模型从用户选择器移除。
- [x] **MCP 工作流桥接**：将语义推文检索、混合推文检索、用户搜索、用户推文和按 ID 取推文接入统一 `AgentTool` 注册边界。
- [x] **策略组件补齐**：新增 Planner、ReAct、Plan Executor 组件；策略节点采用只读 MCP 白名单、1-8 次迭代上限和节点超时，写操作仍由独立 PublishTweet 节点承载。
- [x] **验证**：`npm run build` 通过；`go test ./internal/module/agent/... ./internal/gateway/handler ./cmd/agent-service` 通过。

## 2026-07-14 Agent Runtime 面试强化开发

详细方案见 [`docs/AGENT_RUNTIME_STRENGTHENING_PLAN.md`](./AGENT_RUNTIME_STRENGTHENING_PLAN.md)。评审已通过，当前完成 P0-P5；P6-P7 仍以实际代码和验收结果为准。

- [x] **P0 真实性基线与迁移护栏**：完成 ADR、五入口行为/冒烟基线、`AGENT_RUNTIME_V2_MODES`、Proto/Mongo 兼容契约测试与回滚说明；默认执行路径未改变。
- [x] **P1 统一 AgentRunner**：完成 Runtime 类型/错误模型、ReActRunner、四类 Action、预算取消边界、请求级模型提示、OpenAI-compatible/MCP Adapter、可注入 Tool Catalog、Consult/Workflow Strategy/Assist 灰度迁移，以及 Search/Style/Writer/Review Profile 收敛。
- [x] **P2 Message/Token/Model 治理**：统一消息构建、Token/成本预算、Provider 路由与凭证引用。
- [x] **P3 Tool Policy 与 Human Approval**：统一 ToolExecutor、审批持久化、批准/拒绝状态机、一次性恢复、审批 UI、持久幂等结果回放、TweetService 原生幂等、Tool 熔断指标、治理对账和 MCP 二次认证已完成。
- [x] **P4 Workflow Engine 语义收口**：已完成版本化 DSL、确定性本地 IR、单写者 Blackboard、内置 Reducer、不可变 Revision、指定版本运行、周期 Snapshot、StateEvent Replay 校验、Retry/Timeout/Cancel/Skip/Suspend 状态机、持久补偿、Temporal 决策门、公开只读回放、共享运行预算、后台补偿巡检和 Journal 人工控制。
- [x] **P5 Agent 可观测性**：已完成 Run 控制、独立 Run/Step/LLM/Tool Trace、脱敏查询、控制台、Agent gRPC 与 Provider/MCP HTTP OTel 传播、低基数指标、有界运行事件流、大型 Tool Result 引用、Blackboard 快照检索、Prompt Template 执行版本、安全预览采样和 Agent Grafana 面板。
- [ ] **P6 RAG/Memory Eval**：共享 Episodic Collection、Session 结晶、Embedding 迁移与评测闭环。
- [ ] **P7 产品化增强**：Profile/Prompt 版本、审批、项目 RBAC、运行指标与可选业务结果 A/B 自动回滚、归档 Eval 质量证据发布门禁已完成；工作流模板、外部 MCP、多租户额度、真实 WORM 基线、真实产品事件源验收和外部 IAM 待完成。

P0 验证：

- `go test ./internal/module/agent/... ./cmd/agent-service` 通过。
- `go test -race ./internal/module/agent/runtime ./internal/module/agent/workflow/engine` 通过。
- `go vet ./internal/module/agent/... ./cmd/agent-service` 通过。
- 本次变更文件定向 `git diff --check` 通过；全仓检查仍会命中两处本轮未修改的 Flutter 尾随空格。

P1 第一增量验证：

- `go test ./internal/module/agent/... ./cmd/agent-service -count=1` 通过。
- `go test -race ./internal/module/agent/runtime ./internal/module/agent/model ./internal/module/agent/service -count=1` 分包通过；Service 首次统一命令受 120 秒编译时限影响，单独提高时限后通过。
- `go vet ./internal/module/agent/... ./cmd/agent-service` 通过。
- Runtime 离线测试覆盖 FinalAnswer、单/多 ToolCall 配对、RAGSearch、AskHuman、非法动作、工具错误、MaxSteps、Timeout、Cancellation 与写工具 fail-closed。

P1 第二增量验证：

- `go test ./internal/module/agent/... ./cmd/agent-service -count=1` 通过。
- `go test -race ./internal/module/agent/runtime ./internal/module/agent/model ./internal/module/agent/profile ./internal/module/agent/service -count=1` 通过。
- `go vet ./internal/module/agent/... ./cmd/agent-service` 通过。
- 迁移测试确认 Assist 只获得只读工具；Workflow Strategy 保留节点模型、Token、迭代数、工具白名单和 `tool_trace`；所有测试使用 Fake Runner/Tool Catalog，不连接真实 MCP 或模型。
- Multi 的 Writer/Review Profile 组合保持原 System Prompt 文本，不增加模型调用次数；消息 metadata 追加记录本次使用的 Profile ID。

P2 第一、第二增量验证：

- [x] **Message Builder 与 Token Budget**：新增 System/Developer/Policy、当前输入、History、Persona/Memory、RAG、Tool Result、Blackboard 的统一装配边界；工具调用对原子保留，历史/工具结果可压缩，RAG Chunk 整块跳过。
- [x] **Runtime Usage 与预算预检**：新增单次输入/输出、Run 总 Token 和 `budget_exceeded`；调用前预留输出额度，Provider 无 Usage 时显式记录 `Estimated=true`，失败后的已发生调用仍保留 Step Usage。
- [x] **模型选择闭环**：`model_kind_id` 不再在 gRPC 层被丢弃，Chat/Consult/Assist 的 Legacy 与 Runtime 请求都使用前端所选 Chat 模型；Embedding 模型仍不进入用户选择器。
- [x] **Credential 与 SSRF 基础护栏**：DSL/运行输入拒绝明文 `api_key`，旧 DSL 查询自动脱敏，LLM Tool 与前端统一改为 `credential_ref`；Base URL 增加私网地址、DNS 解析结果和重定向防护。
- [x] **验证**：`go test ./internal/module/agent/... ./cmd/agent-service -count=1`、P2 五包 `go test -race`、`go vet ./internal/module/agent/... ./cmd/agent-service` 与 `web/npm run build` 通过。
- [x] **认知链路 Token 化**：`MemoryManager.BuildContextBlock` 接入统一 TokenCounter；Persona 按 Token 截断，RAG/Memory Chunk 保持整块装配，并返回实际入选 Chunk。
- [x] **Session Summary 结晶**：所有会话模式统一在消息持久化后调度；达到 12 条未摘要消息立即结晶，空闲 2 分钟触发短会话结晶，使用 Mongo 租约、版本游标和稳定 Qdrant Point ID 保证幂等重试；删除对话或服务关闭会取消 Timer 与在途摘要 Job。
- [x] **结构化 Episodic Payload**：摘要保存 `memory_type/facts/preferences/decisions/followups/source_dialogue/summary_version`，JSON 解析失败可降级纯文本。
- [x] **P2 第三增量验证**：Agent 全包测试、Repository/RAG/Service `go test -race` 与 `go vet` 通过。
- [x] **P2 第四增量**：Run 成本/并发预算、Catalog Provider/Fallback 路由、用户级 Provider Config 加密 CRUD、Key 轮换/撤销与工作流 `provider_config_id` 解析已完成。
- [x] **P2 第四增量验证**：Agent/Gateway/Cmd 全包测试与前端构建通过；Runtime、Model、Credential、Repository、Service、Workflow Tool 竞态检测通过；Agent/Gateway/Cmd `go vet` 通过；本轮文件定向 `git diff --check` 通过。全仓检查仅命中两个本轮未修改的 Flutter 文件尾随空格。
- [x] **P3 第一增量：统一工具治理基础**：完成 `ToolSpec + ToolHandler`、实例 Registry、统一 Executor、JSON Schema 校验、认证身份覆盖、权限、超时、保守重试、错误分类、审批 Gate、幂等键要求和敏感参数审计；Workflow、Runtime MCP 与 Legacy 模型驱动工具路径已统一接入。
- [x] **P3 第一增量安全验证**：无 Wait 节点的 Workflow `PublishTweet` 和 Legacy ReAct `create_tweet` 均在网络调用前返回 `approval_required`；Agent 全包测试、Tool/Runtime/Service race 和 `go vet` 通过。
- [x] **P3 第二增量：审批持久化与安全恢复**：新增 Approval/Execution Mongo 模型及索引、带租约审批状态机、脱敏审批参数、乐观锁决策、随机令牌哈希、Run 原子领取、审批后重试当前工具节点，以及查询/决策/恢复 gRPC 与 Gateway API。
- [x] **P3 第二增量并发与安全覆盖**：离线 Fake 测试覆盖跨用户审批、重复决策、错误令牌、并发双 Resume、拒绝终止和幂等结果回放；并发恢复仅一个成功且 TweetService 调用一次。
- [x] **P3 第二增量验证**：Agent/Gateway/Cmd 全包测试、Tool/Engine/Service `go test -race`、Agent/Gateway/Cmd `go vet`、`web/npm run build`、Go 格式检查及本轮文件定向 `git diff --check` 全部通过。
- [x] **P3 第三增量：审批工作台与下游原生幂等**：Web 新增审批收件箱、挂起 Run 详情和批准/拒绝/恢复交互；TweetService 新增用户级幂等键、唯一记录与同事务 Tweet/Poll/Outbox 提交，Agent 和 Temporal 发布链路透传稳定执行键。
- [x] **P3 第三增量缺陷修复**：Tweet/Poll Repository 接入 Unit of Work 事务 Context，并修复 TweetService 事务失败后仍返回成功的旧错误路径；相同键不同输入返回冲突，旧客户端不传键继续兼容。
- [x] **P3 第三增量验证**：Tweet/Agent/Gateway/Cmd 受影响模块全包测试、Tweet/Tool/Service `go test -race`、完整 `go vet`、Web 生产构建、Go 格式与定向 `git diff --check` 通过；审批抽屉完成 1280px 与 390px 实际页面验收。
- [x] **P3 第四增量：熔断、指标与治理对账**：新增工具级 Closed/Open/Half-Open 熔断状态机和低基数 Prometheus 指标；新增周期性 Mongo 对账，回收失效审批/执行租约并终止因审批过期而挂起的 Run。
- [x] **P3 第四增量：MCP 内部认证**：MCP 默认只绑定回环地址，SSE/Message HTTP 边界要求 Bearer Token，Tool Middleware 再校验认证上下文；进程内客户端自动携带同一令牌并支持优雅停机。
- [x] **P3 第四增量验证**：`go test ./...` 通过；Tool/MCP/Repository/Service `go test -race` 通过；`go vet ./internal/module/agent/... ./cmd/agent-service` 和本轮文件定向 `git diff --check` 通过。全仓 `go vet ./...` 仅命中既有 `internal/module/auth/grpc/auth.go:44` 不可达代码，已登记问题单。
- [x] **P4 第一增量：版本化 DSL 与 Compile IR**：DSL 增加版本、节点输入/输出 Schema、Retry、Policy、Profile/Provider Reference；旧 JSON 兼容为 v1，新保存时补齐版本并保留前端 `ui`。新增独立 IR 编译器，确定性校验拓扑、变量依赖与并行写冲突。
- [x] **P4 第一增量：单写者状态与确定性并发**：Blackboard 改为 copy-on-write 代际和 append-only 内存 StateEvent；节点只获得 `StateView`，Scheduler 以并行波次执行并由协调器按声明顺序合并 Delta，Checkpoint 记录状态版本。
- [x] **P4 第一增量：取消与失败收敛**：节点失败、挂起或外部取消后，Scheduler 等待本波次所有已启动 Goroutine 退出再返回；离线测试覆盖反向完成顺序、同代只读、内部失败等待、外部取消等待和恢复不重跑。
- [x] **P4 第一增量验证**：`go test ./internal/module/agent/... ./cmd/agent-service -count=1`、IR/Engine/Tool/Service `go test -race` 与 `go vet ./internal/module/agent/... ./cmd/agent-service` 通过。
- [x] **P4 第二增量：不可变 Workflow Revision**：新增 Revision 集合与当前版本指针，旧 DSL 懒迁移；保存使用条件更新防止并发覆盖，Run 固定 Revision，Resume 不再读取后续编辑的 DSL。
- [x] **P4 第二增量：持久 StateEvent**：新增 append-only 事件集合和 `(run_id, sequence)` 唯一约束，事件内容哈希冲突 fail-closed；Run/Checkpoint 以 `state_version` 对齐恢复序号。
- [x] **P4 第二增量验证**：Agent/Gateway/Agent Service 全包测试通过；Repository/Service/Engine/gRPC/Gateway Race 检测通过；Agent/Gateway/Agent Service `go vet` 与本轮定向 `git diff --check` 通过。
- [x] **P4 第三增量：Revision 查询与指定版本运行**：不可变版本的列表/详情接口贯通 gRPC、Gateway、Web 与 AI 助手，画布和自定义工作流模式不再隐式选择第一条或当前可变定义。
- [x] **P4 第三增量：周期 Snapshot 与恢复校验**：Engine 通过不可变 `StateCommit` 回调解耦存储；Mongo 新增快照集合和唯一版本索引，长流程按事件阈值落快照并推进 Run 游标，完成/挂起强制保存最终快照。
- [x] **P4 第三增量：StateEvent Replay**：Resume 从最近快照重放后续事件，校验事件序号、事件/快照哈希、Checkpoint 版本及状态摘要；持久状态不完整或被篡改时拒绝恢复，且不会重放 LLM/写工具副作用。
- [x] **P4 第三增量验证**：`go test ./internal/module/agent/... ./cmd/agent-service -count=1`、Engine/Repository/Service `go test -race`、`go vet ./internal/module/agent/... ./cmd/agent-service` 与本轮定向 `git diff --check` 通过；Revision 前后端增量的 Gateway 定向测试与 Web 生产构建同时通过。
- [x] **P4 第四增量：确定性 Reducer**：DSL/IR/Engine 支持 `append/sum/min/max/merge/first/last`；同路径并发写必须使用一致且非空的 Reducer，协调器按节点声明顺序归约，失败时不留下节点或全局状态的部分提交。
- [x] **P4 第四增量：编辑器契约保真**：属性抽屉可配置全局状态路径、输出来源和 Reducer；Workflow/Revision 加载再保存会保留 `writes` 及 Schema、Retry、Policy、Profile/Provider Reference 等节点顶层执行元数据。
- [x] **P4 第四增量验证**：`go test ./internal/module/agent/... ./cmd/agent-service -count=1`、IR/Engine/Service `go test -race`、`go vet ./internal/module/agent/... ./cmd/agent-service`、`web/npm run build` 与本轮定向 `git diff --check` 全部通过。
- [x] **P4 第五增量：节点执行状态机**：新增合法状态转换表、`retrying/timed_out`、Trace 尝试次数和确定性退避；仅重试明确可重试/临时网络错误，挂起、业务错误、取消与 Deadline 不重试，退避可被 Context 立即中断。
- [x] **P4 第五增量：Retry 配置闭环**：Workflow Editor 使用开关和数值控件编辑 DSL 顶层 `retry`，总尝试次数、初始/最大退避、倍数和确定性 Jitter 均可配置，且不混入 Tool Properties。
- [x] **P4 第五增量验证**：`go test ./internal/module/agent/... ./cmd/agent-service -count=1`、IR/Engine/Tool/Service `go test -race`、`go vet ./internal/module/agent/... ./cmd/agent-service`、`web/npm run build` 与本轮定向 `git diff --check` 全部通过。
- [x] **P4 第六增量：持久补偿契约与反向计划**：Tool 节点支持独立补偿工具、输入映射、Timeout/Retry；IR 拒绝非法引用或未注册工具，Engine 只对已成功节点生成确定性反向拓扑计划。
- [x] **P4 第六增量：Journal 与恢复闭环**：主 Run 失败先持久化 Compensation Journal，再按严格序号和租约领取；补偿复用 ToolExecutor、Policy、Approval、Breaker 和稳定独立幂等键，支持审批挂起、拒绝终止及失败后显式重试，且不会重放主 DAG。
- [x] **P4 第六增量验证**：IR/Engine/Repository/Service/Gateway 定向测试与竞态检测、Agent/Cmd `go vet`、Web 生产构建和本轮定向 `git diff --check` 全部通过。
- [x] **P4 持久补偿运维闭环**：审批回调、显式重试、过期执行租约后台扫描、独立脱敏 Journal API 和控制台已完成。无需审批的过期补偿可无人值守恢复，审批型补偿仍安全转人工，不宣称所有补偿都自动执行。
- [x] **P4 第七增量：Temporal 决策门**：实验证明旧 Bridge 与统一 IR/Reducer/Event/Approval/Compensation 语义冲突且从未注册；现已删除。用户 DAG 不再规划双执行后端，后台风控与热点任务继续独立使用 Temporal。
- [x] **P4 第七增量验证**：删除实验 Bridge 后 Agent/Cmd 全包测试、`go vet` 和本轮定向 `git diff --check` 通过，启动链路仍只注册风控与热点 Temporal Workflow。
- [x] **P4 第八增量：公开只读回放**：新增用户隔离 Replay RPC/HTTP API，返回固定 Revision、哈希校验 StateEvent、Snapshot 元数据与脱敏 Compensation Journal；篡改、缺序或超过 10,000 事件时 fail-closed，且绝不重放模型和工具副作用。
- [x] **P4 第八增量：回放 UI**：Workflow Editor 可查看完整性状态、Revision/Snapshot、状态事件时间线和补偿步骤；原始补偿输入/输出、幂等键与审批敏感参数不进入响应。
- [x] **P4 第九增量：工作流共享预算**：DSL/IR/Editor 增加节点尝试、并发、总超时、总 Token 和估算成本预算；Scheduler 使用线程安全共享账本限制重试与并发波次，Checkpoint/Run 输出持久化预算快照，恢复后继续累计。
- [x] **P4 第九增量：模型调用统一记账**：直接 LLM Tool、Runtime ReAct/Plan-Execute 和 Legacy 回退策略均在网络调用前预留额度并按 Provider Usage 或显式估算提交；并发分支不能绕过总额，实际调用超限仍保留已发生消耗。
- [x] **P4 第九增量验证**：Agent/Gateway/Cmd 全包测试、Runtime/Engine/Tool/Service 竞态检测、Agent/Gateway/Cmd `go vet`、Web 生产构建和定向格式检查通过。
- [x] **P4 第十增量：过期补偿租约扫描**：Mongo 只返回每个 Run 严格首个未完成且租约过期的执行记录；后台 Reconciler 复用原子 Claim、Attempt ID、ToolExecutor 和幂等键，多实例竞争不会重复副作用。
- [x] **P4 第十增量：审批安全降级**：无需审批的过期补偿自动继续；Write/Risky 或目录缺失工具不会由后台执行，而是转为 `compensation_failed`，保留用户显式重试和一次性 Resume Token 路径。
- [x] **P4 第十增量验证**：Repository/Service 定向测试与竞态检测、Agent 全包测试、Agent/Cmd `go vet` 和定向格式检查通过。
- [x] **P4 第十一增量：补偿 Journal 控制面**：新增用户隔离的 Journal RPC/HTTP 查询和专用重试端点，只返回运行摘要、状态、哈希、租约与错误；工作流/补偿输入输出、幂等键、Attempt ID 和审批参数均不进入响应。
- [x] **P4 第十一增量：人工恢复窗口**：`planned`、`failed` 与租约过期的 `executing` 可人工推进；有效租约与 `suspended` 禁止抢占。Workflow Editor 新增补偿日志视图，重试仍复用 ToolExecutor、原子 Claim、审批和一次性 Resume Token。
- [x] **P4 第十一增量验证**：Agent/Gateway 全包测试、Service/Repository/gRPC/Gateway Handler 竞态检测、Agent/Gateway/Cmd `go vet`、Web 生产构建和定向格式检查通过。
- [x] **P5 第一增量：Run 查询与 Trace 控制台**：新增用户隔离、Workflow/Status 过滤、稳定倒序分页的 Run 摘要 API；Workflow Editor 可查看历史运行、错误、预算和节点 Trace，并切换当前回放/补偿上下文。
- [x] **P5 第一增量：跨实例取消**：新增 `canceling/canceled`、取消原因与时间证据；Mongo 原子请求、执行侧有界轮询和 `status + revision` 终态提交避免取消被旧快照覆盖。周期 Snapshot 通过专用原子仓储接口只推进 `state_version + revision`，不再用旧 Run 对象覆盖状态与取消字段；游标提交失败会恢复本地版本并允许幂等重试。只允许取消 `running`，审批挂起与有效补偿租约不被绕过。
- [x] **P5 第一增量验证**：Agent/Gateway 全包测试、Service/Repository/gRPC/Gateway Handler 竞态检测、Agent/Gateway/Cmd `go vet`、Web 生产构建和定向格式检查通过。
- [x] **P5 第二增量：独立执行追踪模型**：新增与业务 `output_json` 解耦的 `RunRecord/StepRecord/LLMCallRecord/ToolCallRecord`、Mongo 四集合 Repository 与并发安全 InMemory Recorder；唯一键包含租户身份，避免相同 Run/Record ID 跨租户覆盖。
- [x] **P5 第二增量：Runtime/Workflow 统一采集**：Runtime ReAct 和 Workflow DAG 均记录 Run/Step；模型调用记录最终 Model/Provider、精确或估算 Token/成本、耗时与错误分类；ToolExecutor 只向 Trace 发送参数/输出摘要和长度，不保存原始 Prompt、Completion、工具参数或结果。
- [x] **P5 第二增量：租户隔离查询与控制台**：新增 `GET /api/v1/agent/workflow-runs/:id/traces`，读取前先按 Run 所有者鉴权；Workflow Editor 并行读取业务详情与独立 Trace，分别展示节点、模型和工具调用，旧 Run 继续回退到 `output_json.traces`。
- [x] **P5 第二增量验证**：Observability/Runtime/Repository/Tool/Service/gRPC/Gateway Handler 竞态检测、Agent/Gateway 全包测试、Agent/Gateway/Cmd `go vet` 与 Web 生产构建通过；离线测试覆盖跨租户同 ID、LLM/Tool 正文不落 Trace、Workflow 所有权校验和 Gateway 契约。
- [x] **P5 第三增量：OTel 与低基数指标**：Agent Service 初始化可 Flush 的 ParentBased OTLP gRPC Provider，Tweet/User Client 与 Agent Server 接入 `otelgrpc`；Mongo Recorder 扇出到 OTel Span 和 Prometheus，Run/Step/LLM 指标不使用用户、Run、Step、模型或错误原文 Label。
- [x] **P5 第三增量：传播与隐私验证**：内存 gRPC 联合测试验证客户端父 Trace ID 可传播到服务端；Span 测试验证 Prompt/Completion 摘要不会成为 Attribute，Metric 测试验证租户自定义值被归一为有限枚举。
- [x] **P5 第四增量：Provider/MCP HTTP 传播**：新增可组合 HTTP Client Transport 与 Server Middleware；默认/用户级 Provider、Reranker 和 MCP SSE/Tool 请求注入 `traceparent`，MCP 服务端提取父上下文，保留原 Endpoint Policy、Redirect、Timeout 与取消语义。
- [x] **P5 第四增量：安全与竞态验证**：离线 Client/Server 联合测试验证父子 Trace；Span 不包含 URL Path/Query、Authorization、Prompt 或正文，原始网络错误不进入遥测。`pkg/trace`、AI、Model、MCP、Tool 与 Service 竞态检测通过。
- [x] **P5 第五增量：有界运行事件流**：新增 Redis Stream Recorder/EventReader，按 `user_id + run_id` 哈希隔离 Key，使用服务端 Stream ID、约 2000 条长度上限和 24h TTL；Agent Service 提供心跳、连接窗口、终态关闭、游标重置和 Context 取消语义，Mongo Trace 继续作为完整查询事实源。
- [x] **P5 第五增量：gRPC/SSE 与控制台恢复**：追加服务端流式 RPC 和 `GET /api/v1/agent/workflow-runs/:id/events`；Gateway 保持 Bearer 鉴权并即时 Flush，Web 使用可取消 `fetch` 解析 SSE，按 `record_id` 幂等合并，切换 Run/关闭控制台时终止旧连接，窗口结束或网络中断后从最后游标恢复。
- [x] **P5 第五增量验证**：Observability/Repository/Service/gRPC/Gateway Handler/Router 竞态检测、Agent/Gateway/Cmd 全量测试与 `go vet`、Web 生产构建通过。本地 Vite 页面返回 200；Codex 内置 Playwright 依赖因宿主权限阻塞，未把自动化截图冒烟计为通过。
- [x] **P5 第六增量：大型 Tool Result 治理**：统一 ToolExecutor 对 MCP/ReAct/DAG 结果执行 JSON 体积硬上限；超过 64 KiB 的结果通过 Service Port 写入独立私有 MinIO 桶，Mongo 幂等记录保存内联结果或对象引用，回放时校验长度与 SHA-256。对象上传后若 Mongo 提交失败会回收未提交对象。
- [x] **P5 第六增量：脱敏引用 Trace**：Tool Trace、gRPC、Gateway SSE 与 Workflow 控制台追加 storage/reference/content-type 元数据，只暴露无凭证 `minio://` 哈希路径，不返回 Tool Result 正文；Docker 显式启用对象归档，独立开关提供 fail-closed 回滚。
- [x] **P5 第六增量验证**：Tool/ObjectStore/Repository/Service/Observability/gRPC/Gateway Handler 竞态检测、Agent/Gateway/Cmd 全量测试、`go vet`、Web 生产构建和 Compose 配置校验通过；所有对象存储测试使用离线 Fake，不连接真实 MinIO。
- [x] **P5 第七增量：可检索 Blackboard 快照**：新增目标版本之前最近快照 + `(after, target]` 有界事件重建链路，严格校验用户/Run 所有权、快照与事件哈希、连续序号和最终状态版本；支持路径前缀、关键词、页大小及绑定版本/过滤条件的稳定游标。
- [x] **P5 第七增量：脱敏查询与控制台**：新增 `GET /api/v1/agent/workflow-runs/:id/blackboard` 和对应 gRPC 契约；敏感键递归脱敏，单值预览、查询、字段数和重放事件数均有硬上限。Workflow Editor 运行控制台支持版本、路径、关键词和前后页检索。
- [x] **P5 第七增量验证**：Repository/Service/gRPC/Gateway Handler/Router 目标测试与竞态检测、Agent/Gateway/Cmd 完整测试、完整 `go vet`、Web 生产构建和本轮定向 `git diff --check` 通过；测试覆盖跨租户拒绝、篡改快照拒绝、运行中游标版本固定、敏感原值不可搜索和超大值正文省略。
- [x] **P5 第八增量：安全内容采样与模板身份**：Prompt/Completion 默认继续只存哈希和长度；可选采样按稳定键确定性执行，限制预览/扫描大小并拒绝疑似密钥、凭证、邮箱、手机号、身份证和带敏感结构 URL。Runtime Profile 与 Workflow Revision 的模板身份进入租户隔离 LLM Trace，但不进入 OTel/Metric/日志正文。
- [x] **P5 第八增量：Agent Runtime Dashboard**：新增 Docker/Helm 双份 16 面板 Grafana 看板，覆盖 Run、Step、LLM、Token/成本、Tool Policy、熔断和治理对账；Prometheus 补 Agent `9191` 抓取，Grafana datasource 使用固定 UID，面板查询不引入 `user_id/run_id/prompt` 高基数标签。
- [x] **P5 第八增量验证**：Observability/Service/Tool/gRPC/Gateway Handler 目标测试、相关竞态检测、Agent/Gateway/Cmd 全包、`go vet`、Web 生产构建、Docker Compose、Helm 模板、双份 Dashboard JSON/UID/面板数和高基数查询扫描通过。竞态首轮 gRPC 编译器进程瞬时退出，单包重跑通过并记录于 Issue 86。

## Codex 项目治理原生化（2026-07-16）

- [x] **仓库级 Skill 迁移**：将旧单数目录下的平铺 Skill 文档迁移为 `.agents/skills/<name>/SKILL.md`，并将架构审计工作流纳入 `architecture-audit` Skill。
- [x] **Skill 元数据规范化**：11 个 Skill 仅保留 `name/description` frontmatter，触发条件合并到 `description`，支持 Codex 渐进式发现与按需加载。
- [x] **Agent 文件职责收口**：项目 Context/Rule 统一迁入 `.agents/context`、`.agents/rules`；`AGENTS.md` 保持仓库级持久指令入口；`.codex/agents/test-runner.toml` 保持项目级自定义 Subagent 配置。
- [x] **迁移验证**：官方 `quick_validate.py` 在 `PYTHONUTF8=1` 下验证 11 个 Skill 全部通过；旧单数目录路径引用与遗留文件扫描为零。
## 2026-07-19 P3 审批跨设备恢复安全收口

- [x] **短期轮换恢复授权**：新增 `issueWorkflowResumeGrant` gRPC 与 Gateway API，只允许审批所有者为已批准、未过期且仍挂起的绑定 Run 按乐观锁 revision 签发；授权默认 5 分钟并受审批过期时间封顶。
- [x] **单次领取与旧令牌失效**：签发使用 Mongo 条件更新原子轮换 SHA-256 哈希并推进 revision，原始挂起令牌与此前 Grant 立即失效；恢复检查可选 Grant 过期时间，成功后清空授权元数据，旧 Run 无该字段时保持兼容。
- [x] **跨浏览器 Web 闭环**：审批收件箱先读取最新 Run，再签发授权并立即恢复；仅对 revision `409` 重新读取一次，已批准列表支持失败后再次继续。删除初始和后续令牌写入 `sessionStorage` 的死状态，只保留旧数据清理。
- [x] **敏感响应禁缓存**：工作流运行、补偿重试、恢复和 Grant 签发响应统一返回 `Cache-Control: no-store, max-age=0` 与 `Pragma: no-cache`。
- [x] **定向验证**：服务测试覆盖旧令牌失效、重复签发轮换、过期拒绝和最终副作用仅一次；Repository BSON、Proto 方法/字段与 Gateway 租户/revision/响应头契约测试通过。

## 2026-07-19 P6 共享 Episodic Collection 与 RAG Eval 基线

- [x] **共享 Episodic 写入契约**：新摘要写入 `agent_episodic_memory`，Payload 增加 `user_id`、Collection Schema、Embedding 模型/维度/版本；摘要重试继续覆盖稳定 Point ID。
- [x] **服务端租户隔离**：Episodic 检索通过 Qdrant `SearchWithFilter` 在存储侧注入 `user_id` 条件；旧 `episodic_user_<id>` 仅保留有界双读兼容，不在请求中枚举用户集合。
- [x] **离线迁移工具**：新增 `cmd/agent-memory-migrate`，按显式用户 ID 使用 Scroll + 原始 Point ID Upsert，支持批大小、dry-run 和迁移成功后的显式删除旧集合；不重新生成 Embedding。
- [x] **RAG Eval 基线**：新增 51 条覆盖中文、英文、混合语种、错别字、无答案、时态记忆和画像的离线数据集，以及 Recall@K、MRR、NDCG@K、空召回率和噪声率纯函数。
- [x] **P6 第二增量：可复现检索 Runner**：新增存储无关 `Retriever` 接口、逐 Case 错误、Recall/MRR/NDCG/Empty/Noise、P50/P95、稳定 RRF 融合与 JSON 报告；`cmd/agent-rag-eval` 可显式连接 ES/Qdrant/Embedding/Reranker，按固定顺序比较 BM25、Vector、RRF 和 RRF+Rerank，并记录环境、模型、数据集版本与随机种子。
- [x] **P6 第二增量：隔离与 Session End**：Fake Qdrant 共享集合覆盖双用户服务端 Filter 契约；新增 `POST /api/v1/agent/dialogues/:id/end`，取消并等待旧摘要 Job 后同步强制结晶，Mongo 租约/游标保证并发 End 最多写一份新摘要。
- [x] **P6 第三增量：Router 可评测化**：Cascade Router 改为固定词典优先级并通过 `RouteWithMetadata` 暴露 Lexical/Semantic/LLM Fallback/Default Stage；新增 34 条独立数据集、通用 Router Runner 与 `cmd/agent-router-eval`。离线词典/默认层基线为 31/34、Accuracy 91.18%、MisrouteRate 8.82%，L1/L2/L3 错投率分别为 22.22%/11.11%/0%。
- [x] **P6 第四增量：迁移只读验收**：`cmd/agent-memory-migrate --verify-only` 按显式用户 ID 使用共享集合服务端 `user_id` Filter，比较旧集合有效 Point ID 与目标集合，检查租户 Payload、共享 Schema、缺失/意外点，并可输出 JSON 报告；验证路径不写入、不删除数据。
- [x] **P6 第五增量：Router Provider 对照能力**：Cascade Router 改为 Embedding/Chat 窄接口与显式阈值配置，保留旧构造器兼容；`agent-router-eval` 支持 lexical/semantic/llm/full 四种模式，live 模式必须显式 `--allow-live`，API Key 只从环境读取。报告记录 Provider/Endpoint/模型、请求失败、Token、估算成本、Pricing Version、单 Case 超时和 Semantic/LLM 降级错误。
- [x] **P6 第六增量：RAG Live 评测护栏**：`cmd/agent-rag-eval` 现在必须显式传入 `--allow-live`；ES、Qdrant、Embedding、Reranker 使用受限 HTTP Client 和 Endpoint Policy，API Key 只从环境读取，Runner 支持单 Case Timeout，报告记录 Provider/Endpoint/模型/请求数/失败数/失败率。新增的客户端注入点保留旧构造器兼容，本轮未连接真实服务。
- [x] **P6 第七增量：策略选择与依赖懒初始化**：`agent-rag-eval` 支持 `--strategies` 子集执行并按依赖闭包初始化 Provider；BM25、Vector、RRF、RRF+Rerank 的报告顺序稳定，避免只验证单路召回时仍触发无关服务。
- [ ] **P6 未闭环项**：真实环境旧集合回填与跨用户隔离验收、保存真实 BM25/Vector/RRF/RRF+Rerank 基线报告，以及固定 Provider 下的 Semantic/LLM Router 对照；真实数据迁移与删除仍需人工确认。
- [x] **P6 第一增量验证**：`go test ./... -count=1`、RAG/Eval/Qdrant/迁移命令 `go test -race`、Agent/Qdrant/迁移命令 `go vet` 和本轮文件 `git diff --check` 通过；测试不连接真实 Qdrant、ES 或模型服务。
- [x] **P6 第二增量验证**：Eval/Qdrant/Session/gRPC/Gateway 目标测试、目标包 race、全仓 `go test ./... -count=1`、全仓 `go vet ./...` 和本轮文件 `git diff --check` 通过；默认 vendor 已同步缺失的 OTel `tracetest` 测试包。未运行真实 RAG 评测或迁移命令。
- [x] **P6 第四增量验证**：迁移命令与 Qdrant 目标测试、race、vet 及全仓 `go test ./... -count=1`、全仓 `go vet ./...` 通过；Fake Qdrant 覆盖服务端租户 Filter、Point ID 对账、错误租户/Schema 和 JSON 报告目录创建。本轮仍未连接真实 Qdrant 或执行真实迁移。
- [x] **P6 第五增量验证**：Router/RAG Eval/命令目标测试覆盖 Semantic 失败后 LLM 接管、无效 LLM JSON、ProviderErrorRate、单 Case Timeout、Fake OpenAI-compatible Embedding/Chat Usage 与成本统计、Endpoint Policy 和 live 显式开关；离线基线仍为 31/34、Accuracy 91.18%，本轮未调用真实 Provider。
- [x] **P6 第六增量验证**：Eval、AI、ES、Qdrant、RAG 命令目标测试通过；live 缺少 `--allow-live` 时在任何 Provider 初始化前拒绝；未调用真实 ES、Qdrant、Embedding 或 Reranker。
- [x] **P6 第七增量验证**：命令测试覆盖策略去重/稳定顺序、Vector-only 与 RRF+Rerank 依赖闭包；本轮未调用真实 ES、Qdrant、Embedding 或 Reranker。

## 2026-07-19 P7 Prompt/Profile 版本治理

- [x] **P7 第一增量：不可变 Profile Catalog**：新增 `profile_id + version` 唯一快照、Profile/Prompt/Tool 配置校验、返回副本深拷贝和无 Release 多版本拒绝策略；Catalog 不保存运行状态或用户数据。
- [x] **P7 第一增量：确定性灰度发布**：Release 使用 `0..10000` 基点和 `SHA-256(profile_id + salt + user_id)` 稳定分桶；部分灰度缺少身份或 salt 时 fail-closed，0/10000 支持全量回滚/切换。
- [x] **P7 第一增量：Runtime 与组合根接入**：内置 `assist.draft@v1/v2`，默认固定 v1；Assist/Workflow Runtime 统一通过 Resolver 选版，Agent Service 从严格 JSON 环境变量 `AGENT_PROFILE_RELEASES` 构造启动快照，非法配置拒绝启动。
- [x] **P7 第一增量：版本证据**：实际命中的 Prompt ID/Version 继续进入 LLM Trace；Assist 对话元数据新增 `agent_profile_version`，Prompt 中的用户变量只在选版后的副本渲染。
- [x] **P7 第一增量验证**：Profile/Service/Agent Service 目标测试、Profile/Service `go test -race`、全仓 `go test ./... -count=1` 与全仓 `go vet -p=1 ./...` 通过；测试只使用离线 Catalog/Runner，不调用真实模型、MCP、ES 或 Qdrant。
- [ ] **P7 Prompt 管理未闭环项**：运行错误率/P95/成本门禁、固定数据集 Eval 契约和归档回执候选审批已在第六至十增量补齐；真实 WORM 基线、业务效果信号和外部 IAM 仍待后续增量。

## 2026-07-20 P7 Profile/Prompt 持久化内核

- [x] **独立版本仓储**：新增 `agent_profile_versions` 与 `agent_profile_releases`，不扩大对话仓储接口；版本快照不可变并带 Schema/SHA-256，发布与 Release 更新使用 revision CAS。
- [x] **草稿与发布状态**：草稿创建不会进入运行目录；只有已发布版本参与 Catalog 构建，坏快照、身份不一致和非法 Release 均 fail-closed。
- [x] **原子目录替换**：请求通过 `AtomicResolver` 无锁读取；管理器完整构建、校验下一代目录后一次替换，失败继续服务旧目录。
- [x] **启动加载与回滚**：Agent Service 默认从 Mongo 加载；内置 < 持久化 < 环境覆盖，`AGENT_PROFILE_STORE_ENABLED=false` 可回退到内置目录。
- [x] **离线验证**：定向测试、Profile/Repository/Service 竞态检测、全仓测试、全仓 Vet、Compose 解析和 Helm 渲染通过；生命周期 Fake 不连接真实 Mongo、模型、MCP、ES 或 Qdrant。

## 2026-07-20 P7 Profile/Prompt 管理面与跨实例刷新

- [x] **受保护管理 API**：Proto/gRPC/Gateway 提供草稿创建、版本查询、发布 CAS、Release 查询/更新和审计查询；Gateway 使用 JWT 用户白名单，Agent Service 独立校验内部令牌，客户端无法提交或读取服务间凭据。
- [x] **Append-only 脱敏审计**：写操作在持久化变更前必须先记录 `requested`；随后记录 `succeeded/failed/activation_failed/propagation_failed`，仅保存操作元数据、revision 和 SHA-256，不保存 Prompt 正文或 Provider 凭据。
- [x] **跨实例最终一致刷新**：本地完整校验并原子激活后发布 Redis 失效提示；其他实例重新从 Mongo 构建 Catalog，默认每 30 秒周期反熵，通知丢失不会永久分叉。
- [x] **配置与部署契约**：Compose、`.env.example` 和 Helm 支持管理令牌、管理员 ID、通知频道与同步周期；Helm 令牌只允许引用已有 Secret。
- [x] **本轮验证**：管理相关定向测试、Profile/Repository/Service/gRPC/Gateway race、全仓 `go test ./... -count=1`、全仓 `go vet -p=1 ./...`、Compose 解析、Helm 默认及启用 Secret 双模式渲染均通过；事件总线测试仅使用 miniredis。
- [ ] **P7 后续**：运行指标自动回滚、归档 Eval 证据审批和业务效果信号契约已在后续增量补齐；组织目录 RBAC、真实 WORM 基线与真实产品事件源验收仍未完成。

## 2026-07-20 P7 Profile 双人发布审批与管理 UI

- [x] **快照绑定审批状态机**：新增 `agent_profile_publish_approvals`，申请绑定 Profile/version、草稿 revision 与 SHA-256；申请者不能审批自己的申请，重复提交和并发决策使用唯一键/revision CAS 拒绝。
- [x] **可恢复发布执行**：审批通过先进入带租约的 `applying`，成功进入 `applied`，失败进入 `apply_failed`；恢复时先检查目标版本是否已发布，覆盖“发布成功但审批收尾前崩溃”的窗口。
- [x] **项目级角色授权**：Gateway 分离 viewer/editor/approver/admin，admin 继承全部角色；直接发布默认关闭，仅由显式 `AGENT_PROFILE_DIRECT_PUBLISH_ENABLED` 开启为 break-glass。Agent Service 继续独立校验服务令牌。
- [x] **真实管理界面**：新增 `/agent/profiles`，接入草稿、发布申请、审批/拒绝/恢复、Release CAS 与审计接口；能力显隐读取后端 access API，不用前端状态替代授权。
- [x] **本轮验证**：审批状态机、Repository、Service、gRPC、Gateway 定向测试与竞态检测、全仓 `go test ./... -count=1`、全仓 `go vet -p=1 ./...`、Web 生产构建、Compose 解析、Helm 默认及角色 Secret 模式渲染均通过；`/agent/profiles` 已在 1280x720 浏览器视口完成实际渲染验收。
- [ ] **P7 后续**：运行指标自动回滚、归档 Eval 证据审批和业务效果信号契约已在后续增量补齐；外部组织目录/统一 IAM、真实 WORM 基线与真实产品事件源验收仍未完成。

## 2026-07-20 P7 动态项目 RBAC

- [x] **持久化角色绑定**：新增 `agent_profile_role_bindings`，按用户唯一索引保存 viewer/editor/approver/admin 动态角色，创建/更新/删除均使用 revision CAS；环境变量角色保持独立且不能被 API 覆盖或删除。
- [x] **服务端授权事实源**：Agent Service 合并静态 break-glass 与 Mongo 动态角色，并在所有 Profile 管理 RPC 内再次校验；Gateway 使用 JWT 用户查询权限，滚动升级遇到旧服务 `Unimplemented` 时仅回退到原静态角色。
- [x] **根管理员与审计护栏**：只有 `AGENT_PROFILE_ADMIN_USER_IDS` 中的根管理员可授予或撤销动态 admin；角色变更写入独立 append-only `agent_profile_role_audit_events`，不保存 JWT、内部令牌或请求正文。
- [x] **成员权限管理台**：`/agent/profiles` 新增成员权限、角色来源和角色审计视图；`AGENT_PROFILE_DYNAMIC_RBAC_ENABLED=false` 可回滚到纯静态角色模式。
- [x] **本轮验证**：Profile/Repository/Service/gRPC/Gateway 定向测试与竞态检测、全仓 `go test ./... -count=1`、全仓 `go vet -p=1 ./...`、Web 生产构建、Compose 解析、Helm 默认/根管理员模式渲染和缺少根管理员的失败模板均通过；测试不连接真实组织目录或外部 IAM。
- [ ] **P7 后续**：当前仍是项目级 RBAC；运行指标自动回滚、归档 Eval 证据审批和业务效果信号契约已补齐，外部 IAM、真实 WORM 基线与真实产品事件源验收仍未完成。

## 2026-07-20 P7 Profile 运行指标实验门禁

- [x] **Release 绑定实验状态机**：新增 `agent_profile_experiments` 与独立观测集合，实验快照绑定 stable/candidate、流量、salt 和 Release revision；每个 Profile 只允许一个 running 实验，状态更新使用 revision CAS。
- [x] **隐私最小化观测**：Runtime v2 Run 记录实际 Profile ID/version；旁路 Recorder 仅按 Run ID 幂等保存分组、成功标记、耗时和估算成本，不保存用户 ID、Prompt、Completion 或工具正文，记录失败不改变用户响应。
- [x] **自动止损**：达到每组最小样本后比较错误率、P95 延迟和平均成本；任一回归则通过 Release CAS 把候选基点设为 0。外部 Release 变更进入 superseded，目标样本通过只标记 passed，不自动提升候选。
- [x] **管理与观测入口**：Proto/gRPC/Gateway/Web 支持启动、分页查询、立即评估和停止；Prometheus 指标不使用 Profile/实验/用户等高基数标签。功能默认关闭，启用要求 Profile Store、管理令牌和静态管理员。
- [x] **本轮验证**：Profile/Repository/Service/gRPC/Gateway 目标竞态测试、全仓 `go test ./... -count=1`、全仓 `go vet -p=1 ./...`、Web 生产构建、Compose 解析及 Helm 默认/启用/缺失管理员失败模式通过；测试使用离线 Fake，未调用真实模型或运行线上实验。
- [ ] **仍未闭环**：运行指标门禁不代表答案语义质量；固定数据集 Eval 契约和归档回执候选审批已补齐，真实 WORM 基线、业务转化信号和外部 IAM 仍待后续增量。

## 2026-07-22 P6/P7 Agent Task Eval 质量证据层

- [x] **52 条固定任务集**：覆盖普通问答、平台检索、写作、澄清、工具失败、提示注入、越权发布、审批恢复和预算终止；数据集 ID 与版本固定，Loader 拒绝重复 ID、非法状态和不完整审批约束。
- [x] **存储无关 Runner 与隐私最小报告**：Executor 仅返回结构化 Outcome、工具调用、Step、Token 和内存输出；报告不保存回答正文，只保存 SHA-256、字符数、断言结果与聚合指标。
- [x] **行为与安全指标**：统计任务结果、工具选择/成功率、只读工具选择准确率、确定性语义断言、平均 Step/Token、预算终止、审批处理、越权写成功和虚构工具结果。
- [x] **稳定/候选质量门禁**：同数据集版本比较任务、工具和语义断言回归；只读工具选择准确率默认至少 90%，审批用例必须 100% 正确处理，越权写成功和虚构工具结果必须为 0。CLI 可在 CI 中通过非零退出码阻止失败候选，但不会自动发布 Profile。
- [x] **固定夹具验证**：`agent-task-eval` 对 52 条录制契约夹具生成 52/52 通过报告，14 个只读工具用例与 10 个审批用例通过，越权写/虚构工具结果为 0。该结果仅验证评分契约，不是实际 Provider/Profile 质量基线。
- [x] **本轮验证**：`go test` 定向与 Agent 全模块通过，Eval/CLI 竞态检测通过，`go test ./... -count=1` 与 `go vet -p=1 ./...` 整仓通过；实际 CLI 稳定/候选比较返回 `passed`。
- [x] **受控 Live Runtime Adapter**：显式 `--allow-live --runtime-config` 固定 Provider/Model/Profile，复用 Endpoint Policy、Provider Router、Profile Catalog 和 ReActRunner；配置严格拒绝明文 `api_key`，Credential 仅按环境变量引用读取，工具全部为 CLI 私有无副作用沙箱。
- [x] **报告完整性**：报告绑定数据集与脱敏执行配置 SHA-256，支持 HMAC-SHA256 签名、独立验签及已签名稳定报告加载；质量门禁拒绝空/不一致数据集哈希和未绑定执行配置的报告。
- [x] **Live HTTP 集成验证**：本地 OpenAI-compatible 测试服务完整走过受限 HTTP Client、固定模型/Profile、Runtime 和签名报告；同时修复本地 Provider 使用 `127.0.0.1` 时预检与拨号策略不一致的问题。
- [x] **本批验证**：Eval/Model/CLI 定向测试与竞态检测通过，Agent 全模块通过，`go test ./... -count=1` 和 `go vet -p=1 ./...` 整仓通过；实际 `v2` 签名报告 52/52、门禁 `passed`、独立验签成功且无 Input/Output 正文字段。
- [ ] **仍未闭环**：受控环境真实 52 条 Provider/Profile 基线、真实 Object Lock Bucket 验收、真实 Resume Token/MCP 集成验证及业务效果信号；归档回执候选审批已在第十增量补齐。

## 2026-07-22 P7 Agent Eval 版本化/WORM 归档代码闭环

- [x] **存储无关契约**：`eval` 只定义归档请求、版本回执和验证规则；MinIO SDK 隔离在 `objectstore` 适配器，评测 Runner/Profile 逻辑不依赖对象存储。
- [x] **强制不可变存储护栏**：专用 Bucket 必须启用 Versioning 与 Object Lock 且无 Bucket Policy；对象使用 `COMPLIANCE` 保留模式和 `If-None-Match: *` 写入，返回非空 `version_id`，适配器不提供删除能力，不满足条件时 fail-closed。
- [x] **端到端证据复验**：CLI 支持运行后归档、归档已有签名报告和按回执复验；上传后按精确版本回读，核对长度、SHA-256、保留策略与报告身份，再重新执行 HMAC 验签。本地回执使用 `O_EXCL` 创建，不能覆盖已有证据。
- [x] **密钥与隐私边界**：归档配置只保存 Access/Secret Key 的环境变量名，严格 JSON Loader 拒绝未知明文字段；对象键/回执只含哈希、版本、保留元数据与签名 Key ID，不保存 Prompt、回答、Base URL 或凭据。
- [x] **离线验证**：受影响包普通测试与 `go test -race`、Agent 全模块、`go test ./... -count=1` 和 `go vet -p=1 ./...` 整仓验证通过；Fake 覆盖缺 Versioning/Object Lock、Bucket Policy、GOVERNANCE 降级、回读篡改、重复归档和本地回执不可覆盖。
- [ ] **真实环境验收**：本机 LM Studio `127.0.0.1:1234` 可达且已加载 `qwen2.5-3b-instruct`；但独立 HMAC Key/Key ID、MinIO 凭据均未配置，`127.0.0.1:9000` 不可达，因此本轮未执行真实 52 条基线或声称已经完成真实 WORM 归档。

## 2026-07-22 P7 Eval 证据绑定 Profile 发布审批

- [x] **领域与基础设施解耦**：Eval 报告输出/签名成为可复用纯契约；Profile 只依赖 `QualityEvidenceVerifier`，MinIO 读取与 HMAC 验真留在 `objectstore`，Runtime 热路径不访问对象存储。
- [x] **发布门禁**：发布申请可绑定精确 Bucket/Key/Version ID/报告哈希；申请、批准和失败重试均校验有效 COMPLIANCE 保留期、`runtime_live` Profile 身份、passed Gate、至少 50 Case、零错误/越权写/虚构工具结果和审批用例全通过。
- [x] **持久化与 API 最小披露**：Mongo/Proto/Gateway 只保存和返回回执定位、Gate 与聚合指标，不保存数据集正文、Prompt、Completion、Base URL 或 Credential；请求唯一键包含证据对象身份。
- [x] **管理 UI 与部署**：`/agent/profiles` 支持粘贴归档回执并展示证据摘要；Compose/Helm/`.env.example` 新增默认关闭的 `AGENT_PROFILE_EVAL_EVIDENCE_REQUIRED` 及 Secret 配置，关闭开关可回滚到原双人审批策略。
- [x] **本轮验证**：Profile/ObjectStore/Repository/Service/gRPC/Gateway 定向测试、受影响包竞态检测、`go test ./... -count=1`、`go vet -p=1 ./...`、Web 生产构建和 Compose 解析通过；Helm 默认/启用 Secret 模式渲染及缺少管理员或质量证据 Secret 的失败关闭检查均通过。
- [ ] **真实环境验收**：仍需受控 MinIO Object Lock Bucket 和独立 HMAC Secret 生成、人工复核并提交第一份真实 52 条 LM Studio 基线。
- [ ] **后续闭环**：启动并验收专用 Object Lock Bucket，执行、人工复核并归档真实 52 条基线；补真实 Resume Token/MCP 与 Tweet/Agent 产品事件源接入验收。

## 2026-07-22 P7 Profile 实验业务效果信号

- [x] **向后兼容策略**：实验策略可选三类固定业务结果、每组最小结果样本和最大结果率下降阈值；旧记录零值继续只按错误率、P95 与成本评估。
- [x] **可信幂等回填**：新增 admin 保护的结果入口，只能更新已有 Run/Event 观测；同值重试返回幂等回放，冲突值、未知信号、未配置策略和终态实验失败关闭。
- [x] **自动止损与隐私边界**：业务结果样本不足时继续收集，候选结果率回归时复用 Release CAS 回滚；Mongo/API/Prometheus 不保存或标记用户、Prompt、Completion、工具正文及动态信号名。
- [x] **跨层契约与管理台**：Proto/gRPC/Gateway/Web 支持策略配置和结果率/样本展示；业务结果写入 API 不暴露在模型 Tool Catalog。
- [x] **本轮验证**：领域、Repository、Service、gRPC、Gateway 定向测试与受影响包竞态检测、全仓 `go test ./... -count=1`、全仓 `go vet -p=1 ./...`、Web 生产构建及定向 `git diff --check` 均通过。
- [ ] **真实事件验收**：Assist 草稿确认发布与外部点赞/评论正向归因均已接入；仍需受控环境 stable/candidate 样本、明确拒绝动作、DLQ 和回滚演练证据。

## 2026-07-22 P7 Assist 草稿发布产品结果归因

- [x] **来源运行契约**：Assist 响应和历史 assistant 消息暴露最小化 Run ID 与可发布标记；Confirm 请求追加可选 `source_run_id`，旧客户端可继续发布但不会被自动归因。
- [x] **可信确认发布**：Agent Service 校验来源 Run 属于当前用户、模式为 Assist 且状态已完成，再通过窄 `ConfirmedDraftPublisher` 直连 TweetService；幂等键绑定来源 Run 与最终编辑正文，返回真实 Tweet ID。
- [x] **真实正向信号**：TweetService 成功后，旁路记录 `draft_published=true`；只归入与 Run Profile 版本、实验窗口和策略信号匹配的运行中实验，同一 Run 重试幂等，不保存草稿正文或用户到实验观测。
- [x] **前端显式操作**：Web 优先使用结构化草稿候选，历史消息按常见草稿格式恢复；用户可选择、编辑并确认发布，研究摘要、风格分析和适用场景不会在结构化候选存在时被整体发出。
- [x] **失败隔离与可观测性**：来源非法返回 422；产品结果落库失败只记录脱敏日志和固定结果指标，不回滚已经发布的推文。未发布不自动写负样本，避免把用户暂未操作误判为拒绝。
- [x] **本轮验证**：Service/Repository/gRPC/Gateway/Cmd 定向测试、受影响包竞态检测、全仓 `go test ./... -count=1`、全仓 `go vet -p=1 ./...`、Web `vue-tsc + Vite` 生产构建和定向差异检查通过；测试使用 Fake，不调用真实模型、Mongo 或 TweetService。内置浏览器因本地网络隔离未完成视觉验收，见 Issue 93。
- [ ] **后续闭环**：定义明确拒绝动作后再接负样本；在受控流量中保存 stable/candidate、DLQ 重放与自动回滚演练证据。

## 2026-07-22 P7 内容互动产品结果归因

- [x] **可靠事件源**：点赞、评论业务记录与 `TWEET_LIKED`/`COMMENT_CREATED` Outbox 在 MySQL 同事务提交，Canal 统一发布到既有 Topic Exchange；载荷只 append-only 增加发生时间，Timeline 兼容消费。
- [x] **短期最小映射**：Assist 确认发布后保存带 TTL 的 Tweet、作者、来源 Run 和归因窗口，不复制正文；默认窗口 7 天，通过 `AGENT_PROFILE_CONTENT_ATTRIBUTION_WINDOW` 调整。
- [x] **正向语义**：窗口内首个非作者点赞或评论写入 `content_engaged=true`；普通推文、自赞、过期事件与 MQ 重投不增加实验样本，未互动不推断负值。
- [x] **消费护栏**：Agent 使用独立持久队列、16 条 Prefetch、1/2/4 秒退避和三次后 DLQ；Repository/Trace/Outcome 任一暂态失败均可重试，错误标签保持低基数。
- [x] **部署与验证**：Compose、Helm 和 `.env.example` 已同步归因窗口，RabbitMQ NetworkPolicy 已放行 Agent Service；Agent Processor/Consumer 和 Tweet Outbox 定向测试覆盖首次归因、重放、自赞、过期、格式错误、重试与 Outbox 失败。
- [x] **本轮验证**：定向测试、全仓 `go test ./... -count=1`、受影响包 `go test -race`、全仓 `go vet -p=1 ./...`、Compose 解析、Helm 渲染和定向 `git diff --check` 全部通过。
- [ ] **真实环境验收**：仍需启动 MySQL/Canal/RabbitMQ/Mongo 全链路，验证外部互动、DLQ 重放、stable/candidate 样本和自动回滚证据；转发与明确拒绝动作未纳入。

## 2026-07-23 P7 内容互动 DLQ 运维闭环

- [x] **安全检查与显式执行**：新增 `agent-profile-dlq-replay`；默认只检查并重新入队，执行要求操作人/原因，批次最多 100 条，累计重放次数最多 10 且默认 1。
- [x] **毒消息隔离**：只允许两个已知内容互动路由，发布前重新解析并校验事件；未知、格式错误、超大和达到重放上限的消息继续留在 DLQ，不会因单条毒消息删除后续消息。
- [x] **确认后 ACK**：重放使用独立 Publisher Confirm，Broker 确认接收后才 ACK 原 DLQ 消息；发布失败会保留当前及未处理消息，ACK 失败显式报告不确定窗口并依赖既有事件/Run 幂等。
- [x] **脱敏审计输出**：JSON 仅返回消息哈希、固定事件类型/错误码、操作人和原因哈希，保留 Trace/Correlation Header，不输出评论、推文、Prompt 或用户正文。
- [x] **代码验证**：定向普通测试与 `go test -race`、全仓 `go test ./... -count=1`、全仓 `go vet -p=1 ./...` 和定向差异检查通过；测试全部使用 Fake，不连接真实 RabbitMQ。
- [ ] **真实演练**：Docker Compose 当前无运行服务；真实 RabbitMQ/Canal/Mongo 重放与 stable/candidate 自动回滚证据仍待受控环境验收。

## 2026-07-23 P8 统一智能助手与能力生态

- [x] **产品与架构决策**：统一连续对话成为长期主入口；Chat/Consult/Assist/Multi/Workflow 五模式降级为兼容 API 或内部执行策略，不再继续新增同级模式。
- [x] **能力边界**：动态 Tool/Connector、确定性 Workflow、版本化 Skill 和受预算约束的 Multi-Agent 分层建模；Capability Hint 不授予权限，写工具仍由 Tool Policy/Approval fail-closed。
- [x] **迁移与回滚计划**：新增入口、旧接口保留、Dialogue ID 不迁移；前端切流可独立回滚，真实 Web Search、外部 MCP 与 Workflow-as-Tool 分 Feature Flag 演进。
- [x] **详细计划**：新增 `docs/agent/UNIFIED_AGENT_PRODUCT_PLAN.md`，明确 P8.0-P8.5、风险和验收门禁。
- [x] **P8.0 首批代码**：新增统一 `RunAgent` RPC/HTTP/Web Client 契约、可注入保守 Capability Planner、三种已实现能力映射、实际 Execution Profile/Capability 回传和跨层契约测试；旧五入口保持兼容。
- [x] **P8.0 验证**：Agent/Gateway 全模块普通测试通过；Service/gRPC/Handler/Router `go test -race`、目标 `go vet` 和 Web `npm run build` 通过。测试使用离线 Planner/Fake/现有单测，不调用真实模型、MCP 或公网搜索。

## 2026-07-24 P8.1 Capability Catalog 与首个组合任务

- [x] **不可变 Capability Catalog**：新增能力版本、状态、依赖与精确路由快照；重复能力、未知依赖、依赖环、不可用能力和未注册组合均 fail-closed，返回 Definition/Plan 使用副本隔离。
- [x] **站内研究后草拟**：`platform.search + content.draft` 解析为有序 `runtime.research_draft` 路由，复用现有 Runtime v2 和受治理 MCP Executor；Profile 只暴露 `hybrid_search_tweets`，不包含发布或其他写工具。
- [x] **证据与对话一致性门禁**：组合 Run 必须包含成功的站内检索 Tool Observation 才返回草稿；同一请求只运行一次 Runtime，并在最终阶段写入一组 user/assistant 消息，metadata 记录实际 Capability、Execution Profile 和版本证据。
- [x] **意图路由修复**：恢复统一 Planner 的中文搜索/草拟词典，中文“搜索资料并写推文”可自动进入组合路由；显式 Hint 的输入顺序不会改变 Catalog 规定的执行顺序。
- [x] **P8.1 第一增量验证**：Capability/Planner/组合路径目标测试、Agent Service 全包、Agent 全模块与 `cmd/agent-service` 普通测试、Service `go test -race`、Agent 全模块 `go vet` 均通过；全部使用 Fake Runner/Catalog，不连接真实模型、MCP 或公网。
- [x] **P8.1 第二增量：结构化执行证据**：站内搜索 MCP 工具输出版本化 `platform.tweet_search.v1` 结构，Runtime 独立透传 Structured Content；`RunAgent` 返回脱敏 Tool Activity 与站内 Citation，不解析最终回答、不暴露工具参数/原始结果/内部错误，并对来源工具、Tweet ID、数量和摘要长度执行硬校验。
- [x] **P8.1 第二增量契约与验证**：Proto 仅追加字段并重新生成；gRPC、Gateway 与 Web Client 同步。Agent/Gateway 全模块测试、受影响包 `go test -race`、Agent/Gateway `go vet` 和 Web 生产构建通过。
- [x] **P8.1 第三增量：Chat Runtime v2**：`conversation.reply` 使用显式 `runtime.chat` 路由和无工具 `conversation.reply@v1` Profile；统一入口固定复用 Runtime Provider Router、Message Builder、预算、Trace、认知上下文和 Session Summary。普通对话不读取 MCP Catalog、不获得搜索或写权限。
- [x] **P8.1 第三增量兼容与验证**：旧 `/chat` 保持原契约，并由 `AGENT_RUNTIME_V2_MODES=chat` 在 Runtime 与 Legacy 间切换；Runtime 消息持久化失败时不返回未保存答案。Chat/Profile/Planner 定向测试、Agent 全模块与 `cmd/agent-service` 普通测试、Service `go test -race` 和 Agent 全模块 `go vet` 均通过，测试未连接真实模型、Mongo、MCP 或公网。
- [x] **P8.1 第四增量：Artifact 与状态投影**：`content.draft` 从受灰度影响的兼容 Assist 路由迁移到显式 `runtime.draft`；只有已持久化且具有 `source_run_id` 的草稿才返回可发布标记和类型化 `content.draft` Artifact。`RunAgent` 同步返回 `run_status` 与脱敏 `approval_state`，普通响应不包含审批输入、凭据或一次性恢复令牌。
- [x] **P8.1 第四增量契约与验证**：Proto 仅追加 Artifact/Approval/Run Status 字段并重新生成，gRPC、Gateway 和 Web Client DTO 同步；草稿落库失败会整体失败。Agent/Gateway 全模块测试、受影响包 `go test -race`、目标 `go vet` 与 Web 生产构建通过。
- [x] **P8.1 第五增量：Web 统一入口**：AI 助手主界面删除 Chat/Consult/Assist/Multi/Workflow 五模式分支，所有自然语言请求统一调用 `RunAgent`；能力面板只发送可选 Hint，不授予 Tool 权限。Workflow 保持顶部独立“自动化”入口，旧五 API 继续保留为回滚兼容路径。
- [x] **P8.1 Web 执行证据与会话一致性**：助手消息展示实际 Capability、Run Status、脱敏 Tool Activity、站内 Citation 与可发布 Draft Artifact；发布仍携带 `source_run_id` 走显式确认。会话详情请求增加序号隔离，快速切换时旧响应不能覆盖当前 Dialogue；能力变化不会隐式新建会话；工作流审批恢复只刷新服务端会话，不再把无归属结果拼入当前聊天。
- [x] **P8.1 Web 验证**：`vue-tsc + vite build` 生产构建通过；使用本地临时 Mock Gateway 在 1440x900 与 390x844 视口验证历史会话、统一发送、结构化证据、草稿确认发布和 400ms 慢请求后的会话竞态，临时服务与测试文件已清理。
- [x] **P8.1 核心完成**：统一连续对话主入口、现有三能力及精确研究草拟组合、执行证据和 Artifact 展示已闭环。当前 Catalog 仍没有写 Capability，因此真实 `pending` 审批态需等受治理写能力接入后验收；公网 Citation/搜索与外部 MCP 分别进入 P8.2/P8.3。
- [x] **P8.2 第一增量：真实 Web Search Adapter**：新增 Provider-neutral `websearch.Provider` 和 Brave Web Search Adapter；旧 Workflow `WebSearch` 不再返回 Mock，未配置时 fail-closed。请求具备查询/结果/响应体/并发/超时硬上限，Provider HTTP 错误不回传响应正文。
- [x] **P8.2 出站安全与证据**：复用 Endpoint Policy 和受限 HTTP Client，拒绝私网/本地/凭据 URL、HTTP 重定向与 DNS Rebinding；新增 `web.search.v1` Structured Content、`web_page` Citation 和来源 URL/Schema/工具名校验，不从回答正文解析引用。
- [x] **P8.2 统一能力路由**：`web.search` 默认为 `planned`，仅在 Provider 启用且启动校验成功后变为 `available`；新增 `runtime.web_search` 与 `web.search + content.draft` 精确组合，Profile 只获得只读 `web_search`，没有发布权限。
- [x] **P8.2 Web/部署入口**：AI 助手增加“联网搜索/联网研究并草拟”能力 Hint；`.env.example`、Docker Compose 和 Helm 同步 Feature Flag、Provider、密钥引用和资源上限。默认关闭；平台 Brave Key 是可选兜底，未选用户配置且没有平台 Key 时调用 fail-closed。
- [x] **P8.2 第一增量验证**：Web Search/Workflow/MCP/Runtime/Service/Agent Service 普通测试、受影响包 `go test -race`、目标 `go vet`、Web 生产构建、Compose 解析及 Helm 模式渲染均通过；测试使用离线 Fake/HTTP Server，未调用真实 Brave API。
- [x] **P8.2 第二增量：受治理 Page Read**：新增 `web.page.v1`、`page_read` MCP 和 `PageRead` Workflow 组件；只读取公网 HTTP(S) 可见文本，拒绝凭据/Fragment、私网、本地地址、重定向、DNS Rebinding 与非文本响应，并限制 URL、响应体、字符、并发和超时。
- [x] **P8.2 Prompt Injection 与 Citation 边界**：移除脚本、样式、表单、导航及隐藏 HTML；常见注入信号写入 Safety 元数据，模型只接收去指令化且明确标记为不可信的文本。公网 Citation 只接受受信任工具的版本化 Structured Content，同 URL 的 Page Read 摘录可替换搜索摘要。
- [x] **P8.2 来源缓存与准入预算**：Search/Page Read 共享 Redis TTL 缓存和原子准入脚本；服务端从执行上下文注入用户/Run 身份，按用户窗口、Run 请求数及估算成本 fail-closed，DSL/MCP 输入无法伪造计费身份。
- [x] **P8.2 Workflow/部署接线**：Runtime 联网 Profile 可按需调用 `page_read`，普通 Assist 保持站内只读；Workflow Editor 增加 WebSearch/PageRead 组件，Compose/Helm/.env 同步缓存、Page Read 与治理配置。
- [x] **P8.2 第三增量：用户级搜索 Provider Config**：复用 AES-256-GCM、Keyring、Revision、撤销和租户所有权校验，新增 `kind=web_search` 的 Brave 配置；API Key 不进入 DSL、模型 Tool Schema、响应或日志，旧无 `kind` 配置按 `llm` 兼容。
- [x] **P8.2 租户路由与治理复用**：AI 助手和 Workflow WebSearch 可引用 `provider_config_id`；Runtime 仅从可信请求上下文向 MCP 注入配置 ID，模型不能伪造。动态 Provider 与平台兜底共享并发闸门、Redis Cache 和用户/Run Governor，缓存按用户、配置 Revision 和凭据版本隔离。
- [x] **P8.2 用户配置 UI/部署契约**：AI 助手新增个人联网 API 管理与选择，Workflow WebSearch 只列 `web_search` 配置，LLM 节点只列 `llm` 配置。Compose/Helm 支持 Provider Config 主密钥与 Keyring Secret；启用联网不再强制配置平台 Brave Secret。
- [ ] **P8.2 剩余**：在真实 Brave 凭据与受控公网环境完成质量、配额、缓存和生产 Redis 容量验收后，才能声明具体部署环境具备公网搜索。
- [x] **P8.3 第一增量：远程 MCP 连接控制面**：新增用户级 Connection、Mongo CRUD/CAS、独立 Feature Flag 和 HTTP API；仅接受 Streamable HTTP/SSE，明确拒绝 stdio 与未授权项目级作用域。Bearer Token 使用现有 AES-256-GCM Keyring，响应、Schema、日志与 DSL 均不返回凭据。
- [x] **P8.3 第一增量：发现与审核边界**：远程请求复用受限 HTTP Client，禁止重定向、私网/回环/DNS Rebinding，并支持独立 Egress Allowlist。Tool Discovery 对数量、名称、描述和 Schema 体积设硬上限，按名称规范化后生成 SHA-256 不可变 Snapshot；工具使用 `server_id.tool_name` 命名空间，Schema 变化保留旧 Active Snapshot 并要求重新审核。
- [x] **P8.3 第一增量：跨层契约与验证**：追加 gRPC/HTTP/Web API、Mongo 索引、Compose/Helm/.env 配置；离线测试覆盖租户隔离、密钥加密、Feature Flag、禁止 stdio/私网、真实 Streamable HTTP Discovery、稳定 Snapshot、重复发现复用和变更重新审核。Agent/Gateway 相关包完整测试、受影响包竞态检测、独立 `go vet`、Web 生产构建、Compose 解析及 Helm 渲染均通过。
- [x] **P8.3 第二增量：Snapshot Tool Policy**：Connection 保存绑定 Active Snapshot 的工具策略并使用 Revision CAS；只有服务端确认远端 `readOnlyHint=true` 且非 destructive 的工具可启用。Schema 变化暂停执行，新 Snapshot 审核后清空旧策略；写、风险和未知工具继续 fail-closed。
- [x] **P8.3 第二增量：动态目录与统一执行**：Repository 固定两次查询装配用户执行目录；`connector.mcp` 只在 Feature Flag 与 Manager 可用时路由到 `runtime.external_mcp`。Profile 每次 Run 只获得当前策略允许的精确工具名，远端调用复用 ToolExecutor 的 Schema、身份、超时、重试、熔断、结果治理、脱敏审计和 Trace。
- [x] **P8.3 第二增量：跨层 API 与 Web**：追加工具目录/策略 gRPC 与 HTTP 契约，Gateway 使用 JWT 用户身份；AI 助手增加外部 MCP 能力 Hint 和连接管理面，支持新增/编辑/撤销、发现、Schema 审核与只读启停，不回显凭据。
- [x] **P8.3 第二增量：离线验证**：真实本地 Streamable HTTP SDK Server 验证 Discover/Call/Bearer；Fake 覆盖租户隔离、Snapshot 绑定、只读声明、凭据仅调用时解密、策略失效、ToolExecutor Schema/审计和统一入口 Evidence。Web `vue-tsc + Vite` 生产构建通过；Agent 全模块与 `cmd/agent-service` 普通测试、Repository/Service/gRPC/Gateway/remote 竞态检测及目标 Vet 均通过。
- [x] **P8.3 第三增量：Workflow 高风险工具审批**：已审核 Active Snapshot 中未声明只读的工具可由用户显式标记为 `risky`；此类工具不进入统一 Agent 目录，只能作为 Workflow 动态 Tool 节点执行，并复用持久 Approval、Checkpoint、一次性 Resume Grant、租户身份和统一 ToolExecutor。
- [x] **P8.3 第三增量：恢复时重新授权**：审批前远端调用为 0；批准恢复后重新读取 Connection、Active Snapshot、Schema 和 Tool Policy，策略撤销或 Schema 变化均 fail-closed。高风险工具内部尝试固定为 1，节点错误显式标记不可重试，避免未知远端结果被 DSL 自动重放；外部 MCP 补偿仍禁止。平台 `user_id` 只参与租户授权、审批和审计，第三方请求仅携带远端 Schema 已校验的显式参数。
- [x] **P8.3 第三增量：工作流体验与验证**：组件库新增外部 MCP 工具，属性面板只列当前用户已审核且启用的 `read/risky` 工具，并支持通用 JSON 参数；管理面区分只读与“高风险·逐次审批”。Remote/Service 定向测试、Agent/Gateway/Cmd 扩大回归、Remote/Service `go test -race`、目标 `go vet`、定向 `git diff --check` 和 Web `vue-tsc + Vite` 生产构建通过；浏览器已验证组件卡片与连接管理弹窗，真实策略列表仍待可用第三方 MCP 连接验收。
- [x] **P8.3 第四增量：外部 MCP 幂等写入契约**：Discovery 同时记录标准 `idempotentHint` 与项目命名空间幂等参数元数据，并验证该参数是 Input Schema 的必填字符串；只有完整、已审核契约可启用 `write`，旧 Snapshot 零值继续 fail-closed。
- [x] **P8.3 第四增量：可信键注入与安全重试**：Workflow 为每个 Run/Step/Tool 生成稳定执行键，远端只收到域隔离 SHA-256 键；调用前再次解析租户、Snapshot 与 Policy 并覆盖 DSL/模型同名参数。写工具逐次审批、要求 ToolExecutor 持久幂等且允许同键有限重试；`risky` 仍严格单次，外部补偿仍禁止。
- [x] **P8.3 第四增量：跨层体验与离线验证**：Proto/gRPC/Gateway/Web 增量暴露声明、参数和支持状态；MCP 管理面与 Workflow Editor 区分只读、高风险和幂等写入。Fake 覆盖不完整契约拒绝、攻击者键覆盖、身份不泄漏、审批前零调用和两次重试键一致；Fresh 定向测试、Remote/Service `go test -race`、整仓 `go test ./...`、串行整仓 Vet、Web `vue-tsc + Vite` 构建和页面空态检查通过。
- [x] **P8.3 第五增量：有界 MCP Session Pool**：SDK Adapter 按 Connection、Transport、Endpoint 与 Credential Version 隔离复用 Session；全局容量、单连接容量、获取等待和空闲 TTL 均有硬上限。端点/凭据轮换及撤销后旧身份立即失效，池关闭后在途 Session 释放即销毁；独立开关可回退逐次建连。
- [x] **P8.3 第五增量：主动健康巡检**：Mongo 使用独立健康租约跨实例领取到期连接，不递增用户控制面 Revision；巡检具备批次、并发上限、超时、稳定抖动、指数退避和连续失败阈值。健康状态仅为 `unknown/healthy/degraded/unhealthy` 诊断信号，不自动改变 Snapshot、Tool Policy 或真实执行权限。
- [x] **P8.3 第五增量：观测与跨层体验**：新增无租户/连接高基数标签的健康与连接池 Prometheus 指标；Proto/Gateway/Web 显示固定健康分类、连续失败与检测时间，旧记录零值按 `unknown`。离线测试覆盖 Session 复用、容量背压、空闲回收、身份轮换失效、多实例租约、降级/恢复与标签收敛。
- [x] **P8.3 第五增量验证**：Remote/Repository/Service `go test -race`、整仓 `go test -vet=off ./...`、串行整仓 `go vet -p=1 ./...`、Web `vue-tsc + Vite` 生产构建、Compose 解析和 Helm Lint/渲染均通过。浏览器在桌面与 390px 移动视口验证 MCP 弹窗空态、服务不可用提示、滚动与表单布局；因本地 Agent Service/真实第三方 MCP 未启动，本轮未把真实连接健康状态和长连接代理行为计为通过。
- [x] **P8.3 第六增量：Unified Agent 权威 Run 生命周期基础**：新增独立 `agent_execution_runs`，不再把 Trace 或 DAG Workflow Run 当作模型驱动 Agent 的业务状态。Run 在模型执行前创建 `running@revision=1`，由租户、Run ID、状态和 Revision CAS 提交完成、失败、取消、等待人工或等待审批；同一 Run ID 贯穿 Runtime、Trace、对话元数据和状态记录。状态集合只保存路由、版本、Token/成本及按 Run ID 域隔离的正文摘要，不保存凭据、Tool 原始参数、输入/回复正文或恢复令牌。
- [x] **P8.3 第六增量：失败闭合与灰度**：`AGENT_RECOVERABLE_RUNS_ENABLED` 默认关闭；启用后创建状态失败时不调用模型，提交状态失败时不返回成功，模型完成后的对话落库失败会记录为 `failed`。请求取消时使用独立 3 秒提交窗口尝试落终态。该基础增量先固定 `resume_supported=false`，只准确记录挂起态，不提前伪装恢复能力。
- [x] **P8.3 第六增量验证**：Repository/Service/Cmd 定向测试、Repository/Service `go test -race -p=1`、整仓 `go test -vet=off -p=1 ./...`、串行整仓 `go vet -p=1 ./...`、Compose 解析、Helm Lint 与 Agent Deployment 环境变量渲染均通过。首次并行编译被 Windows 工具链派生权限拦截，改用串行测试后通过并记录于 `docs/ISSUES.md`。
- [x] **P8.3 第七增量：版本化加密 Checkpoint**：Runtime 新增 `react.v1` Checkpoint/Resume 契约，完整保留消息、步骤、Usage 与待处理动作但不保存 Tool Definition；状态密文使用独立 AES-256-GCM Keyring、租户与 Run 绑定 AAD、SHA-256 摘要及 256 KiB 默认硬上限。Checkpoint 缺失、损坏、超限、版本未知或待处理参数疑似包含凭据时 fail-closed。
- [x] **P8.3 第七增量：原子恢复与重新授权**：Mongo 通过 `status + revision + lease + attempt_id` CAS 领取 `ask_human` Run，过期租约可回收且旧 Attempt 不能提交。恢复继续累计原 Step/Token/成本，不重放已成功 Step；每次恢复重新解析 Profile/Prompt 并装配当前用户仍获授权的只读 Tool，版本漂移或授权撤销不会沿用 Checkpoint 中的旧能力。
- [x] **P8.3 第七增量：查询、恢复与 Web 连续对话**：新增租户隔离 `GET /agent/runs/:id` 与 `POST /agent/runs/:id/resume`，响应不暴露 Checkpoint/租约/Attempt。AI 助手重载历史后查询权威 Run，用户下一条消息可继续当前 `ask_human` Run；恢复失败时撤销乐观消息、恢复输入并重新读取服务端状态，若响应丢失但权威 Run 已完成则重载已持久化对话。该增量当时仍关闭 `approval_required` 与 Unified Agent 高风险/写工具，已由第八增量补齐。
- [x] **P8.3 第七增量验证**：Runtime/Credential/Repository/Service/gRPC/Gateway 目标 `go test -race -p=1`、整仓 `go test -vet=off -p=1 ./...`、串行整仓 `go vet -p=1 ./...` 和 Web `vue-tsc + Vite` 生产构建通过；Compose 解析、Helm Lint 及恢复关闭/启用 Keyring 两种 Agent Deployment 渲染通过。测试使用内存仓储、Fake Runner/Tool，不连接真实模型或第三方 MCP。
- [x] **P8.3 第八增量：Unified Agent 工具审批恢复**：新增默认关闭的 `AGENT_UNIFIED_APPROVAL_RECOVERY_ENABLED`，并要求可恢复 Run 与外部 MCP 同时启用。受治理 Catalog 可向 `runtime.external_mcp` 提供当前用户已授权的 `read/risky/write` 工具；Runtime 对待审批 `tool_call` 保存类型化 Checkpoint，批准后恢复同一步内的精确动作，不重跑产生动作的模型或此前成功步骤。
- [x] **P8.3 第八增量：权威绑定与一次性授权**：Agent Run 状态版本升级为 2，绑定 Pending Action、审批 ID、输入摘要、稳定幂等键与审批时限；Resume Grant 按 Run Revision 原子签发、只保存哈希并轮换旧令牌，恢复通过状态/Revision/审批/令牌/租约/Attempt 联合 CAS。拒绝或过期会终止 Run 并清除 Checkpoint、授权和租约。
- [x] **P8.3 第八增量：调用前重新授权与副作用边界**：授权签发、领取恢复和真实调用前重新读取当前 Profile/Prompt、Connection、Credential Version、Active Snapshot、Schema、Tool Policy 与审批绑定。`risky` 固定一次远端尝试；`write` 必须具备已审核的声明式幂等参数并在有限重试中复用平台稳定键。恢复只新增 assistant 消息，不伪造用户消息。
- [x] **P8.3 第八增量：跨层审批体验与离线验证**：Proto/gRPC/Gateway/Web 复用既有 Tool Approval 事实源；审批收件箱按 Workflow/Runtime 来源签发对应授权并立即在内存中消费，Agent 响应与授权端点设置 `no-store` 且不回显已消费令牌。离线测试覆盖错误令牌、单次消费、重放拒绝、授权漂移、风险重试、写入稳定键、拒绝/过期终止和 Gateway 契约；目标竞态、整仓测试、串行 Vet、Compose、Helm 与 Web 生产构建通过。应用内浏览器验证桌面与 390x844 审批抽屉空态/服务不可用状态；本地 Agent API 返回 500，因此未计为审批 API 端到端验收。
- [x] **P8.3 第九增量：持久 Agent 项目与真实成员校验**：新增独立 `agent/project` 领域、`agent_projects` Mongo 集合和 Revision CAS 成员表；角色固定为 `owner/editor/viewer`，Owner 不可移除或降级。新增/更新成员通过 User Service 精确校验目标用户，目录故障时 fail-closed；Gateway 只使用 JWT 操作者和路径目标用户，HTTP 用户 ID 保持字符串。
- [x] **P8.3 第九增量：项目级 MCP 共享与实时撤权**：`AGENT_EXTERNAL_MCP_PROJECT_SCOPE_ENABLED` 默认关闭并要求外部 MCP 同时启用。Owner/Editor 可治理项目连接，Viewer 可查看并使用；列表、按 ID/Server 解析、Workflow/Unified Agent 目录和真实调用均重新读取当前成员关系。移除成员后调用立即被拒绝，Connection/Snapshot 继续以创建者用户 ID 绑定加密 AAD，其他成员永不读取凭据。
- [x] **P8.3 第九增量：跨层体验与离线验证**：Proto/gRPC/Gateway/Web 增加项目、成员及连接作用域契约；MCP 管理面支持个人/项目创建、角色感知只读和项目成员管理。Manager/Fake 覆盖真实用户存在性、Owner 不变量、CAS、Editor 管理、Viewer 使用和撤权后目录/调用拒绝；Gateway 覆盖 JWT 身份、路径成员、CAS 与大整数 JSON。目标包 Race、整仓测试、串行 Vet、Web 生产构建、Compose 解析、Helm Lint、默认/开启渲染和非法开关 Guard 均通过；浏览器在 1440x900 和 390x844 验证 MCP 弹窗无横向溢出或控件越界。本地 Agent API 仍返回 500，因此未把真实 Mongo/User Service 项目创建计为端到端验收。
- [x] **P8.3 第十增量：部署托管项目凭据**：新增严格 JSON Registry 与只读文件 Resolver，只允许项目级 Bearer 连接以 `credential_source=managed + managed_credential_ref` 精确绑定项目、Endpoint 和认证方式。Connection 仅保存引用/版本，不保存托管 Token 或密文；未知字段、重复引用、路径越界、绑定不一致和缺失 Secret 均 fail-closed。
- [x] **P8.3 第十增量：轮换与会话隔离**：Discovery、健康巡检和真实调用均按次解析托管 Secret；Session Identity 绑定 Connection、Registry Version 与当前 Token 的单向摘要，同版本文件轮换后新调用不会复用旧 Session，更新/撤销会失效该 Connection 的全部身份。Registry Version 漂移会 fail-closed，Owner/Editor 通过 CAS 重新保存后才采纳新版本并清空 Snapshot/Policy；用户与托管来源也可显式 CAS 迁移。
- [x] **P8.3 第十增量：跨层与部署契约**：Proto/gRPC/Gateway/Web 追加兼容字段，项目连接管理面可选择用户或部署托管引用；`.env`、Compose 和 Helm 增加默认关闭开关，Helm 只读挂载现有 Secret 并拒绝非法开关组合、空 Registry 或缺失 Secret。离线测试覆盖 Registry 严格校验、项目/Endpoint 绑定、Secret 轮换身份、存储无密文、来源迁移、池全身份失效和 HTTP 不泄密。
- [x] **P8.3 第十增量验证**：Remote/Repository/Service/gRPC/Gateway 目标测试与 `go test -race -p=1`、整仓 `go test -vet=off -p=1 ./...`、串行整仓 `go vet -p=1 ./...`、Web `vue-tsc + Vite` 生产构建、Compose 解析、Helm Lint、合法只读 Secret 渲染和非法配置 Guard 均通过。验证只使用临时文件、内存/Fake 和模板渲染，不冒充真实 Kubernetes Secret 轮换或第三方 MCP live 证据。
- [x] **P8.3 第十一增量：生产验收 Runner**：新增显式 `--allow-live` 的独立命令，复用生产 Endpoint Policy、MCP SDK 与有界 Session Pool，验证 Ping、Discovery、只读调用、可选声明式幂等写入/状态计数及文件凭据轮换新身份；写探针额外要求 `--allow-write`，配置不接收明文凭据。
- [x] **P8.3 第十一增量：Conformance、证据与部署**：新增只绑定回环地址的 MCP Conformance Server，提供确定性只读、幂等写入、状态查询和故障工具；报告只保存摘要/固定错误码并支持 HMAC 签名验签。Agent 镜像增加运维二进制，Helm 增加默认关闭、`backoffLimit: 0`、整卷 Secret 挂载和非 root 的一次性 Job。
- [x] **P8.3 第十一增量：离线验证**：本地真实 MCP SDK HTTP 集成与 Fake 覆盖 Bearer、协议、目录、只读调用、写权限、状态计数、轮换新身份、超时脱敏、严格配置、签名篡改和 CLI Guard；目标包与 `mcp/remote` Race、整仓 `go test -vet=off -p=1 ./...`、整仓 `go vet -p=1 ./...`、Compose 解析、Helm 默认/合法渲染及空配置/缺签名 Secret/轮换缺凭据 Secret Guard 均通过。命令级烟测生成并验签 `passed` 报告（5 通过、0 跳过、0 失败）；操作手册及故障矩阵见 `docs/agent_mcp_acceptance.md`。该证据验证框架，不冒充真实第三方或 Kubernetes 演练。
- [ ] **P8.3 剩余**：使用新增框架在受控环境完成真实第三方 MCP、Kubernetes Projected Secret 轮换与旧 Token 撤销、多副本/代理故障、远端幂等履约和项目成员跨服务链路验收；如需控制台动态维护 Registry，再单独设计管理员 RBAC、审计与 KMS/Vault Adapter，不能把部署配置冒充运行时密钥管理平台。
- [ ] **真实性边界**：公网搜索与外部 MCP 已具备代码和离线安全测试，但当前未使用真实 Brave 密钥或公网第三方 MCP Server 做 live 验收；外部 MCP 可描述为“受 Feature Flag 控制的个人/项目级治理、部署托管 Secret 引用、实时成员撤权、Workflow 与 Unified Agent 逐次审批恢复、声明式幂等写入”，远端声明不是服务端行为证明，不能描述成已开放 MCP 市场、完整 KMS/Vault 管理、严格 exactly-once 或完整执行生态。

## 2026-07-30 至 2026-08-01 P8.4 Workflow-as-Tool、Skill 与 Multi-Agent 增量

- [x] **显式发布与不可变绑定**：新增独立 `agent_workflow_tool_publications` Mongo 集合、租户唯一键与 Revision CAS；发布记录固定 Workflow Revision、DSL Hash、稳定 Tool Name、描述和 Input Schema。继续保存草稿不会隐式改变已发布行为，停用只撤出 Runtime Catalog，不删除 Workflow 或历史 Revision。
- [x] **可发布性与调用时复验**：发布和真实调用均重新编译 DSL/IR 并检查节点。持续拒绝补偿、`agent`、递归 Workflow Tool、未知工具和外部回调；只读工具不得审批，写工具必须逐次审批且声明幂等，风险工具必须逐次审批，审批型发布还要求完整恢复基础设施。外部 MCP 按当前 Snapshot/Policy 做同等校验。Input Schema 拒绝 `$ref` 和平台身份字段。
- [x] **Unified Agent 与治理复用**：新增 `workflow.run` Capability、`runtime.workflow` Profile 和动态用户目录；调用复用统一 ToolExecutor 的 Schema、超时、幂等结果、熔断、结果体积、审计与 Trace。子 Workflow Run 保存父 Agent Run/Action 谱系；父子预算独立记账，父 Tool Timeout 同时约束子运行。
- [x] **跨层发布控制**：Proto/gRPC/Gateway/Web 增加发布、查询和停用契约；Workflow Editor 显式展示“发布给 Agent”与绑定版本。`AGENT_WORKFLOW_AS_TOOL_ENABLED` 默认关闭，关闭时新发布、目录发现和调用 fail-closed，但保留查询与停用路径；Compose、Helm 与 `.env.example` 已同步 Catalog 数量和 Tool Timeout。
- [x] **离线验证**：Repository/Service/gRPC/Gateway/Cmd 普通回归通过；Repository race、Workflow-as-Tool Service 目标 race、gRPC/Gateway 全包 race 和受影响包 Vet 通过。Web `vue-tsc + Vite` 生产构建、Compose 解析、Helm Lint 与开启态环境变量渲染通过；应用内浏览器验证桌面与 390px 视口，发布按钮可见且无水平溢出。测试使用内存仓储/Fake Runner，不连接真实模型、Mongo 或第三方 MCP。
- [x] **P8.4 第二增量：版本化 Skill 契约与只读目录**：新增独立 `agent/skill` 领域契约，由当前用户 Active Workflow-as-Tool 发布记录确定性投影 Skill，不新增可漂移的 Skill 数据库。版本指纹固定绑定发布 Revision、Workflow Revision/DSL Hash、单一允许工具、Profile/Prompt、预算、指令和输出 Schema；目录只支持精确 `skill_id + version`，没有 `latest` 或关键词自动选择。
- [x] **P8.4 第二增量：受治理执行与审计**：新增 `skill.run -> runtime.skill` 精确路由。规划前、Runtime 前和工具真实调用前均重新解析 Active 发布并校验版本绑定；Runtime 只获得 Skill 固定的单一 Workflow Tool，完成态还要求成功 Tool Observation 与结构化输出通过 Skill Schema。权威 Agent Run、gRPC/Gateway/Web 响应均记录实际 Skill ID/Version。
- [x] **P8.4 第二增量：跨层目录与回滚**：新增租户隔离 `GET /agent/skills` 和精确版本查询 API，AI 助手提供显式 Skill 选择器；Proto 只追加兼容字段。`AGENT_SKILL_CATALOG_ENABLED` 默认关闭且依赖 Workflow-as-Tool 开关，关闭后目录和执行路由立即 fail-closed，不修改发布元数据，也不影响普通对话路径。
- [x] **P8.4 第二增量验证**：Skill/Repository/Service/gRPC/Gateway/Router/Cmd 定向普通回归、Skill/Service 目标 Race 和受影响包 Vet 通过；Web `vue-tsc + Vite` 生产构建、Compose 解析、Helm Lint、合法开启态渲染及非法开关组合模板期拒绝通过。应用内浏览器检查 `1280px` 与 `390px` 降级页面均无水平溢出；本地 Agent API 返回 500，因此未把 Skill 列表与真实 Workflow 调用计为端到端证据。
- [x] **P8.4 第三增量：成功任务显式模板化**：新增独立 `agent_task_templates` 集合和 `agent.task_template.v1` 契约。用户只能从自己的 `completed` 权威 Agent Run 主动创建模板，并自行提供含唯一 `{{input}}` 的指令；模板不复制对话或模型输出，只保存源 Run Revision、结果摘要、执行 Profile、Capability 及 Skill/Profile/Prompt 版本证据。
- [x] **P8.4 第三增量：不可变复用与审计**：模板创建支持租户级幂等键，内容不可变，归档使用 Revision CAS。执行前重新校验模板状态、源 Run 完成证据和当前 Catalog 路由，再进入统一 RunAgent；模型和用户 Provider Config 仍由当次请求选择。新 Run、gRPC/Gateway/Web 响应均记录实际模板 ID/Revision。
- [x] **P8.4 第三增量：跨层控制与 UI**：新增创建、列出、归档和执行 API，AI 助手可从任意带 Run ID 的历史完成消息读取权威状态后保存模板，并在输入区选择或归档模板。`AGENT_TASK_TEMPLATES_ENABLED` 默认关闭且依赖 Recoverable Runs；关闭执行时保留只读列表与归档，不删除模板。
- [x] **P8.4 第三增量验证**：Repository/Service/gRPC/Gateway/Router/Cmd 定向测试和受影响包串行 Vet 通过；Repository/Service 目标 Race 通过，Gateway Race 在关闭隐式 Vet 后通过且独立 Vet 已补齐。Web `vue-tsc + Vite` 生产构建、Compose 解析、Helm Lint、合法开启态渲染及非法开关组合拒绝均通过。应用内浏览器使用一次性契约夹具验证任务模板选择与归档入口，`1280px` 和 `390x844` 均无水平溢出；本地完整 Agent 依赖未启动，因此未把真实 Mongo、模型调用或模板执行计为端到端证据。
- [x] **P8.4 第四增量：父子 Run 版本化核算**：权威 Agent Run 与 Workflow Run 直接持久化预算上限、Step/Node、精确或估算 Token/成本、Pricing Version 和 `execution.accounting.v1`。Workflow 挂起/恢复继续使用累计 BudgetSnapshot；旧记录不从 `output_json` 或 Trace 猜测回填。
- [x] **P8.4 第四增量：租户隔离只读聚合**：新增 `GET /agent/runs/:id/accounting`，按 `user_id + parent_run_id` 专用索引有界查询直接子 Workflow Run，分别返回父级自身、子级和合计；不同价格版本标记 `mixed`，运行中、旧记录和截断查询显式降级为 `partial/unavailable`。该视图不改变父子独立准入预算，也不递归统计。
- [x] **P8.4 第四增量：助手用量入口与验证**：每条带权威 Run ID 的助手回复可打开只读用量弹窗。Agent/Gateway 扩大回归、目标 Service/Gateway Race、受影响包 Vet 与 Web 生产构建通过；应用内浏览器在 `1280x800` 和 `390x844` 检查无水平溢出。首次组合 Race 因 Windows 冷构建超过 5 分钟超时，拆分复跑通过；本地无可用模型与完整 Agent API，因此真实 Mongo 父子运行仍待受控环境端到端验收。
- [x] **P8.4 第五增量：Workflow-as-Tool 人工输入挂起/恢复**：Runtime 新增版本化 Tool Continuation 契约，父 Agent 在只读 Workflow Tool 遇到显式 `human_input` Wait 时进入 `awaiting_human`，把子 Run、父 Action、发布 Revision/DSL Hash 和子恢复凭证写入加密 Checkpoint。用户补充信息后恢复同一个子 Run，不重放模型决策、已成功 Action 或新建子 Run；子 Run 已成功但父提交中断时，从权威 Blackboard 幂等读取结果。
- [x] **P8.4 第五增量：调用时复验与编辑器契约**：恢复前重新校验当前 Active 发布、不可变 Workflow Revision/Hash、父子谱系和 Action 绑定；只允许问题非空且有界的 `resume_mode=human_input`，外部回调、用户提供恢复令牌、补偿、Agent 与递归 Workflow Tool 继续 fail-closed。编辑器明确区分“人工输入/外部回调”，不展示 Resume Token；组件库支持点击添加，并在移动端改为抽屉，属性面板改为覆盖层。
- [x] **P8.4 第五增量验证**：Agent 全模块与 `cmd/agent-service` 普通回归、Runtime/Tool/Service 目标 Race、受影响包 Vet 和 Web 两次生产构建通过。应用内浏览器真实添加 Wait 并切换两种等待类型，`1280x800` 与 `390x844` 均无页面级水平溢出；当前本地 Workflow Catalog API 仍返回 500，因此未把浏览器检查描述为真实 Mongo/Agent/子 Workflow 端到端验收。
- [x] **P8.4 第六增量：子 Workflow 审批桥接**：Runtime Tool Continuation 增加 `delegated_tool_approval`；受治理写/风险节点由子 Workflow 独占审批事实、输入摘要、幂等键与一次性 Resume Grant。父 Agent 加密 Checkpoint 只保存子审批引用，审批中心签发子 Grant 后调用父 Resume，由父 Runtime 在原 Tool Action 内恢复同一子 Run，不创建第二条审批或父级令牌。
- [x] **P8.4 第六增量：父子一致性与失败收口**：父 Run 使用 Revision CAS 和委托恢复类型绑定子审批；恢复前复验用户、父子谱系、Action、发布 Revision/DSL Hash、审批状态/摘要/幂等键和 Grant 哈希。子 Run 已成功但父提交中断时从权威 Blackboard 回放，写工具持久幂等结果保证不重复执行；拒绝或过期同步终止父子 Run并清理恢复状态，治理计数正确累加。
- [x] **P8.4 第六增量：发布与审批中心**：写工具只有“逐次审批 + 声明幂等 + 完整恢复桥”同时成立才可发布，风险工具要求逐次审批和完整恢复桥；外部 MCP 继续受当前 Snapshot/Policy 约束。审批收件箱识别 `invocation_source=runtime` 的子 Run，批准后恢复父 Agent；直接 Runtime 审批路径保持不变。
- [x] **P8.4 第六增量验证**：端到端内存测试覆盖父挂起、子审批、一次性授权、原 Action 恢复、写工具仅执行一次、父子完成及令牌重放拒绝；另覆盖非幂等写发布拒绝和子审批拒绝同步终止父 Run。`go test ./internal/module/agent/...`、Runtime/Service 拆分 Race、`go vet ./internal/module/agent/...` 与 Web `vue-tsc + Vite` 生产构建通过。组合 Race 外层超时已拆分复跑，不连接真实模型、Mongo 或第三方 MCP。
- [x] **P8.4 第七增量：Multi-Agent 影子准入**：新增独立 `agent/strategy` 领域包，以确定性复杂度信号匹配版本化研究/草拟角色模板；高复杂度精确路由进入 `multi_agent` 候选，随后逐项校验 Profile Tool Scope、角色/并发上限、显式 Step/Token/成本预算和延迟预算，任一不满足即记录原因并回退。计划不保存原始问题、Prompt、凭据或工具参数。
- [x] **P8.4 第七增量：证据与安全回退**：权威 Agent Run、`RunAgent`/Run 查询 Proto、Gateway 和 Web DTO 追加脱敏 `agent.execution_strategy.v1` 证据，包含稳定原因码、角色 Tool Scope、硬预算、估算延迟与 SHA-256 摘要。`AGENT_MULTI_AGENT_PLANNER_ENABLED` 仅开启影子准入；聚合生命周期执行器尚未安装，复杂任务固定记录 `multi_executor_unavailable` 并执行原单 Agent 路由，旧 `MultiAgentPublishTwitter` 未被复用。
- [x] **P8.4 第七增量验证**：Planner 全边界单测、Service/Repository/gRPC/Gateway 契约测试和 Agent 全模块普通回归通过；Strategy/Service 及 Repository/gRPC/Gateway `go test -race`、Agent/Gateway/Cmd `go vet`、Web `vue-tsc + Vite` 生产构建、Compose 解析、Helm Lint 和开启态环境变量渲染通过。验证使用 Fake/内存计划，不连接真实模型、Mongo、搜索 Provider 或第三方 MCP。
- [x] **P8.4 第八增量：有界多角色聚合执行**：为两个精确研究草拟模板安装顺序 `researcher -> drafter -> reviewer` 执行器；每个角色使用独立 Profile、消息、工具交集和硬预算，研究证据通过有界摘要与结构化 Citation 交接，起草/审校不接收历史会话噪声且无工具权限。执行复用既有 Runtime、Model Router 与 ToolExecutor，不复用旧硬编码 Multi API。
- [x] **P8.4 第八增量：父级治理与失败语义**：共享 Runtime `BudgetTracker` 封顶父级累计 Token/成本，三段 Usage/Step 合并到同一权威 Agent Run；Trace 按父 Run ID 和角色化 Step ID 关联。真实执行由 `AGENT_MULTI_AGENT_EXECUTION_ENABLED` 独立控制并依赖 Planner 与 Recoverable Runs；关闭后立即回到影子准入/单 Agent。任一角色失败、挂起或请求审批都整体失败，不进行消耗后的隐藏回退。
- [x] **P8.4 第八增量验证**：成功路径覆盖角色消息隔离、工具最小权限、结构化交接、父预算、聚合 Run 与单次会话持久化；失败路径覆盖中途终止、禁止隐藏重跑和失败用量归集。Agent 全模块与 `cmd/agent-service` 普通回归、Runtime/Strategy/Service Race、受影响包 Vet、Compose 解析、Helm Lint、合法开启态及两类非法依赖组合均通过。验证使用 Fake Runtime/Tool 和离线配置，不连接真实模型、Mongo、搜索 Provider 或第三方 MCP。
- [x] **P8.4 第九增量：单/多策略对照门禁**：复用 `agent-task-eval` 增加版本化 `agent.strategy-comparison.v1`，在同数据集、Case/模板覆盖、Provider、Model、Pricing Version、环境、Seed 与超时全部一致时，比较单 Agent 和多角色的语义质量、任务/工具回归、平均估算成本与 P95。成本/延迟证据不完整或混入非目标模板时返回 `ineligible`；错误、预算终止、越权写或伪造工具结果直接失败。
- [x] **P8.4 第九增量：固定任务与可回滚 CLI**：新增 20 条只读研究草拟任务，均绑定 `platform.research_draft.v1` 或 `web.research_draft.v1`，并提供单/多录制资源夹具验证评分契约。`--strategy-gate` 只计算，`--enforce-strategy-gate` 才以非零退出码阻断；策略判定进入既有 HMAC 签名与 WORM 归档载荷。新增字段均可选，旧 v2 签名报告保持可验。
- [x] **P8.4 第九增量验证**：领域测试覆盖通过、质量/安全/成本/P95 失败、证据不可比、倍率边界、资源聚合和旧签名形状；CLI 测试覆盖签名策略报告、强制门禁非零退出与缺稳定证据拒绝。Agent 全模块、`agent-task-eval`/`agent-service` 普通回归、Eval/Cmd Race 和 Vet 均通过；完整离线命令同时通过原质量门禁与策略门禁。夹具中的 2000 bps 语义增益、2.3838x 成本和 2.5410x P95 仅为契约自测。
- [x] **P8.4 第十增量：生产/Eval 共用多角色核心**：新增无 Service、Mongo、MCP SDK 依赖的 `internal/module/agent/multirole`，统一顺序角色隔离、受限证据交接、共享父级 Token/成本预算、Usage/Step 聚合与失败即停语义。生产 `multi_agent_execution` 改为薄适配层，继续负责目录、Citation、Trace 与权威 Run 持久化，不再维护第二份角色编排算法。
- [x] **P8.4 第十增量：同配置真实模型策略对照**：`agent-task-eval --strategy-runtime-config` 在一个规范化配置内固定 Provider、Model、Pricing 和两个模板的单 Agent/研究/草拟/审校 Profile Snapshot，自动运行 Multi 候选与 Single 稳定侧并写入相同配置哈希。明文凭据字段严格拒绝，Case Timeout 最少 50 秒；不同配置哈希直接 `ineligible`，Web Case 可在 `allowed_tools` 内按需调用 `page_read`，但不得越出声明工具集合。
- [x] **P8.4 第十增量验证边界**：多角色核心测试覆盖角色/历史/工具隔离、结构化交接、父/角色预算预检与失败不回退；CLI 本地 OpenAI-compatible HTTP 集成覆盖真实 Runner 六次模型请求、签名报告、同配置哈希和双侧通过。Multirole/Eval/CLI Race、Service 关键路径 Race、受影响包 Vet、Agent 全模块和整仓 `go test ./...` 均通过；离线双门禁命令实跑为 `passed/passed`。配置样例只引用环境变量，Search/Page Read 使用确定性结构化证据沙箱。因此本增量证明真实模型调用适配器与编排可运行，不证明任何外部搜索质量、真实 Provider 收益或生产 P95。
- [x] **P8.4 第十一增量：长运行 Live Eval 可恢复护栏**：评测核心新增脱敏 `AgentTaskCaseEvidence`、连续前缀恢复和逐 Case Observer，可仅凭报告安全字段与工具调用计数重建完整聚合指标。Live CLI 在首个未完成 Case 前通过真实 Provider/Model 执行一次无副作用 Chat/`eval_preflight` Tool Call，连接、模型标识或工具能力异常立即失败；Case 执行错误快速终止且不固化失败 Case。
- [x] **P8.4 第十一增量：签名检查点与故障恢复**：`--checkpoint-dir` 按 Candidate/Stable 写入序号连续、逐条 HMAC 签名并链接上一 Payload SHA-256 的追加记录。记录不含输入、回答、Prompt、工具参数或工具结果；恢复严格绑定 Side、数据集/配置 SHA、环境、Seed、Timeout 和执行描述，拒绝篡改、序号缺口、未知文件与配置漂移。端到端测试模拟第二 Case Provider 503，确认首次只保存第一 Case、恢复只重跑第二 Case并生成签名报告。
- [x] **P8.4 第十一增量验证**：Eval/CLI 定向测试、Eval 与 CLI 串行 Race、受影响包 Vet、Agent 全模块串行回归和 20 Case 离线双门禁均通过；离线结果保持 `quality_gate=passed`、`strategy_gate=passed`。真实 LM Studio 冒烟在约 5 秒内于预检阶段明确失败，未创建报告或检查点。首轮组合 Race 的 Windows `compile.exe Access is denied` 已记录为 Issue 122，并以相同 Cache、`-p=1` 串行复跑通过。
- [x] **P8.4 第十二增量：显式 ToolChoice 契约**：真实预检确认 LM Studio 在未声明 `tool_choice` 时会把正确的函数调用 JSON 放入普通文本。Runtime 新增标准化 `ToolChoice` 和仅首轮生效的 `InitialToolChoice`；OpenAI-compatible Adapter 映射 `auto/required/none`，预检、Single Research 和 Multi Researcher 首轮使用 `required`，工具 Observation 后及 Drafter/Reviewer 保持自动选择。Runtime、Model、Multirole 与 Eval CLI 定向测试通过，真实 Adapter/Router 探针均返回标准 `tool_calls`。
- [x] **P8.4 第十二增量：首份真实单/多签名对照**：经用户确认后，使用固定 LM Studio `qwen2.5-3b-instruct`、`controlled-accounting-v1`、相同 Profile Snapshot、20 条 `agent-strategy-cases-v1` 和 90 秒 Case Timeout 完整执行 Multi Candidate 与 Single Stable 共 40 次 Case。两侧任务完成率、工具成功率均为 100%，错误、预算终止、越权写和虚构工具结果均为 0；Multi 工具选择准确率为 90%，高于 Single 的 75%。
- [x] **P8.4 第十二增量：晋级门禁诚实失败**：Multi 语义通过率为 60%，低于 Single 的 90%；平均估算成本为 `3050.2 / 1158.2 = 2.6336x`，P95 为 `12449 / 3322 = 3.7475x`。失败主要来自跨角色交接后遗漏证据中的关键术语及输出超过 800 字，策略门禁以 `candidate_semantic_rate_below_policy`、`semantic_gain_below_policy`、`p95_latency_ratio_exceeded` 拒绝晋级。未放宽阈值，也未把失败报告描述成 Multi-Agent 收益。
- [x] **P8.4 第十二增量：本地签名复验**：脱敏报告写入 `tmp/agent-task-eval/live-strategy-qwen25-20260801-v1.json`，绑定数据集 SHA-256 `ff27daf0...775b`、执行配置 SHA-256 `85e88cfa...f322` 与 Key ID `local-live-eval-20260801-v1`；`--verify-report` 独立复验通过。逐 Case Candidate/Stable HMAC 哈希链检查点完整保留，正文、Prompt、工具载荷和凭据未写入报告。
- [ ] **P8.4 剩余**：当前没有受控 MinIO Object Lock 凭据，失败报告尚未形成 WORM Version ID 回执；Multi-Agent 执行资格继续关闭。下一次资格评测应先定义新的通用 Profile Revision 或由用户启动更强且支持 Tool Call 的固定 Chat 模型，再使用全新配置哈希/检查点运行，不得覆盖本次失败基线或放宽门禁。只有新报告通过、人工复核并 WORM 归档后，才单独验证生产 Search/Page Read 召回与公网 P95，并决定是否开发并行角色和角色级 Checkpoint/Resume。

## 2026-08-01 工作区基线冻结与交付门禁

- [x] **生成产物隔离**：根 `.gitignore` 增加 `/.cache/` 与 `/tmp/`，覆盖仓库内 Go 构建缓存、评测临时报告、渲染产物和开发服务器日志；未删除任何现有文件，也未忽略 `.agents`、`.codex/agents` 或新增源码。
- [x] **Agent 核心门禁**：`multirole/eval/runtime/service` 与 `agent-task-eval` 普通测试通过；四个并发核心包的 Race 结果通过，组合冷构建超时后按 Service 单包复跑闭环；相同目标范围的串行 Vet 通过。
- [x] **Agent 全模块门禁**：Agent 全模块、Agent Service 及 MCP/Memory/RAG/Router/Eval 运维命令使用仓库内缓存和 `-p=1` 串行回归全部通过，不连接真实模型、Mongo、MCP、Brave 或其他外部服务。
- [x] **整仓 Go 门禁**：`go test -p=1 ./... -count=1` 与 `go vet -p=1 ./...` 全部通过；首次编译器退出与组合 Race 超时已按环境抖动记录为 Issue 123，并通过拆分复跑证明没有稳定源码失败。
- [x] **Web 门禁**：`web` 的 `npm.cmd run build` 通过，`vue-tsc` 与 Vite 共转换 880 个模块并生成生产产物；Browserslist 数据陈旧提示不影响构建结果，本轮未执行无关依赖升级。
- [ ] **Mobile 门禁**：`flutter analyze` 在工具链启动阶段 300 秒无输出超时，`flutter --version` 也未在 30 秒内返回；当前不能声称移动端通过，环境阻塞见 Issue 124。
- [ ] **提交边界**：本轮完成可验证工作区基线，但尚未擅自暂存或提交用户工作区；需要按 Agent 核心、Workflow/RAG、P7 Eval、P8 能力生态、跨端/部署/文档拆分可审查提交，避免把数百项历史改动压成单一提交。

## 2026-08-01 P8.4 第十三增量：Profile Set v2 与更强模型资格配置

- [x] **Profile Set 原子灰度**：研究父 Profile 成为唯一 Release Anchor；研究员、草拟者和审校者不能独立灰度。Catalog/AtomicResolver 在单一不可变快照中按父版本精确解析整套 Profile，缺版本、重复成员和非法角色 Release 均失败关闭。
- [x] **Run 级版本固定**：`agent.execution_strategy.v1` 计划追加可选 `profile_set_anchor/profile_set_version` 并纳入 SHA-256 摘要；执行阶段按计划中的精确版本重取整套角色配置，即使 Catalog 在规划与执行之间刷新，本次 Run 也不会漂移。Multi-Role 核心在任何模型调用前再次拒绝混合版本。
- [x] **通用 v2 内容契约**：站内/公网 Single 与 Multi 角色使用 v2 候选 Prompt，优先读取结构化证据、保留精确术语、过滤无关材料；默认直接返回一份完整成稿，不再混入研究摘要、风格判断和适用场景。用户指定长度优先，未指定时中文通常 180-600 字或其他语言相当信息量。
- [x] **受限推理模式控制**：Eval Runtime 增加 `provider_default/disabled/enabled`；当前只为 DashScope 显式映射 `enable_thinking`，仅注入 Chat Completion 且冲突时失败关闭，不开放任意 Provider 请求字段。
- [x] **固定云候选配置**：新增 `agent_strategy_runtime_config.qwen37-v2.example.json`，固定 `qwen3.7-plus-2026-05-26`、Profile Set v2、关闭 Thinking 的 Tool Calling 评测形态、上下文/输出上限和版本化费率；凭据仅引用 `DASHSCOPE_API_KEY`。
- [x] **离线验证**：Profile/Strategy/Multirole/Model/Eval CLI 与 Service 目标测试、Agent 全模块普通回归、并发核心和 Service 目标 Race、受影响包 Vet 均通过；并发替换测试确认 Profile Set 不混合 Catalog 快照。全部使用 Fake/本地 HTTP 夹具，不调用 LM Studio、DashScope、Mongo、MCP 或搜索 Provider。
- [x] **真实资格评测已授权**：用户在本机提供 `DASHSCOPE_API_KEY` 并明确允许固定合成评测数据出站与相关费用；旧 qwen2.5 失败报告继续保留且未覆盖。最终结果与边界见第十四增量。

## 2026-08-01 P8.4 第十四增量：ReAct 收束护栏与 qwen3.7 资格报告

- [x] **最终步收束护栏**：ReAct 在保留最后一步生成最终答案时移除 Tool Catalog 与 `tool_choice`，追加高优先级终止指令；若 Provider 仍返回非终态动作，Runner 在执行工具前以 `max_steps_exceeded` 失败关闭。`InitialToolChoice=required` 且总步数不足 2 时在模型调用前拒绝，避免“必须调用工具”却没有结果生成步的非法配置。
- [x] **可复现评测修复**：首轮 qwen3.7 报告暴露 `agent_strategy_cases.json` 的 10 个 `allowed_tools` 被重复写入首个 Web Case、其余 9 个 Case 缺失字段。加载器现拒绝任意层级重复 JSON Object Key，固定夹具逐 Case 断言 `web_search` 必需且 `page_read` 可选；修复后的契约升级为 `agent-strategy-cases-v2`，旧 v1 报告仅保留为缺陷证据，不作为资格结论。
- [x] **固定云模型完整对照**：使用 DashScope `qwen3.7-plus-2026-05-26`、`enable_thinking=false`、Profile Set v2、Pricing `dashscope-cn-qwen3.7-plus-2026-05-26-cny-2026-08-01` 和 90 秒 Case Timeout 完成 Multi Candidate/Single Stable 各 20 Case。Candidate 任务完成、读工具选择、语义与工具成功率均为 100%，无错误、预算终止、越权写或虚构工具结果；Stable 语义为 95%，其余对应指标为 100%。
- [x] **策略门禁通过**：Candidate 相对 Stable 的语义增益为 500 bps，任务与工具回归均为 0；平均估算成本倍率 `1.0714x`，P95 倍率 `0.8870x`，Candidate P95 `15,707ms`。签名报告位于 `tmp/agent-task-eval/live-strategy-qwen37-20260801-v7.json`，绑定数据集 SHA-256 `55f14d6f...10e46`、执行配置 SHA-256 `429f5e44...63e60` 和 Key ID `local-live-eval-20260801-v3`，独立 HMAC 验签通过。
- [ ] **资格边界与后续**：本报告证明固定模型/Profile 在确定性无副作用 Search/Page Read 证据沙箱中满足两个只读顺序模板的准入门禁，不证明真实 Brave/Page Read 召回、公网 P95、任意模板、并行角色、角色级恢复或写工具协作。下一步先人工抽检 Case 摘要并将报告归档到受控 MinIO Object Lock 获取 Version ID；在此之前生产 Feature Flag 不自动开启。

## 2026-08-01 P8.4 第十五增量：加密人工复核材料

- [x] **正文与报告继续隔离**：`agent-task-eval` 不改变 v2 签名报告、逐 Case 检查点或进度日志契约；它们继续只保存输出 SHA-256、字符数和脱敏评分。仅在 CLI 组合层用 Executor 装饰器短暂捕获本次 Candidate/Stable 的输入与最终输出，生产 Runtime 和 `internal/module/agent/eval` 不新增正文持久化依赖。
- [x] **显式双门禁复核包**：新增 `--review-bundle`。它只允许 Live `--strategy-runtime-config` 单/多对照，要求 `--allow-review-content`、`--enforce-gate`、`--enforce-strategy-gate`、独立的 base64 32 字节 Review Key/Key ID 和全新 `--out`/Bundle 路径；禁止与 Checkpoint、报告归档或既有文件覆盖组合。双门禁未通过时只保留签名报告，不生成复核包。
- [x] **加密与不可替换绑定**：复核包使用 AES-256-GCM，AAD 和加密载荷共同绑定最终签名报告 Payload SHA-256、报告 Key ID、数据集版本/SHA、Candidate/Stable 执行描述及逐 Case 评分/输出哈希。外层只暴露 Schema、算法、Review Key ID 和报告摘要；输入与模型正文不以明文出现在 Bundle、报告、Checkpoint 或日志中，载荷/文件尺寸有硬上限。
- [x] **受控打开**：新增 `--open-review-bundle + --review-report + --review-output`，必须同时提供报告 HMAC Key 和独立 Review Key，先验签报告，再解密并逐 Case 核对正文 SHA-256/字符数和完整结果；明文只写入新的 `0600 + O_EXCL` 本地文件，不输出到终端。该文件只供人工查看，不等于人工签认或发布审批证据。
- [x] **离线验证**：单元测试覆盖密文不含输入/回答、报告摘要篡改拒绝、严格 32 字节 base64 Key、Bundle 不可覆盖和显式内容授权；本地 OpenAI-compatible HTTP 夹具完成两个模板的 Candidate/Stable 双门禁、加密 Bundle、原报告验签及受控打开。`go test ./cmd/agent-task-eval -count=1` 已通过，全程未调用 DashScope、LM Studio、MinIO、Mongo、MCP 或公网搜索。
- [x] **复核材料后续已接续**：历史 `live-strategy-qwen37-20260801-v7.json` 按原隐私设计仍没有正文，无法从哈希逆向恢复，也不会伪造对应 Bundle。经用户重新确认费用与敏感正文捕获后，全新报告/Bundle 的完整重跑和机器辅助正文审阅已在第十六增量完成；外部人工签认与 WORM 仍未完成，且因内容资格被否决暂不进入晋级归档。

## 2026-08-01 P8.4 第十六增量：qwen3.7 全量正文复核与门禁校准

- [x] **全新付费资格运行**：经用户确认固定合成评测数据出站、敏感正文捕获和费用后，使用固定 `qwen3.7-plus-2026-05-26`、Profile Set v2、`agent-strategy-cases-v2`、全新报告/Bundle 路径且无 Checkpoint 完成 Candidate/Stable 各 20 Case；预检与全部 Provider 调用完成，无执行错误。
- [x] **签名与密文链路复验**：报告 HMAC、Payload SHA-256 `f1f96e91...267eb`、AES-256-GCM Bundle 绑定、数据集/执行配置身份及 40 个输出哈希/字符数均独立校验通过。评测和 Review Key 只以当前 Windows 用户 DPAPI 密文保存在忽略的 `tmp` 路径，明文仅进入执行进程环境。
- [x] **自动指标**：Candidate 20/20、Stable 18/20；语义通过率 `100%/90%`，平均 Token `3065.90/3523.95`，估算成本 `0.190004/0.184500 CNY`，P95 `16166/17356ms`。策略门禁记录语义增益 1000 bps、成本倍率 `1.0299x`、P95 倍率 `0.9315x`，总估算费用约 `0.374504 CNY`。Stable 两个 Web Case 因 839/848 字符超过 800 上限失败。
- [x] **40 份正文机器辅助审阅**：逐一对照输入、必需/禁用词和长度契约。未发现明显跨主题混入，但确认工具证据仅是“输入 + 必需关键词”；Candidate 12/20 返回证据不足并夹带评测占位元数据，其余内容也无法由证据验证；Stable 普遍以模型常识补写，存在无来源支撑的归因。该检查不冒充外部人工签认。
- [ ] **生产资格否决**：自动双门禁通过不能覆盖正文审阅结论，本报告固定标记为“内容资格不通过/不可判定”，不启用 Multi Feature Flag，也不作为 WORM 晋级对象。下一轮先开发 v3 实质证据夹具、Claims/Citations、证据不足分支、最终交付可用性、内部元数据泄漏和 groundedness 门禁；离线契约稳定后再申请云模型重跑。
- [ ] **受控环境后续**：本轮没有启动 MinIO、LM Studio、Docker 或其他用户侧软件。外部人工签认、Object Lock Version ID、真实 Brave/Page Read 召回与公网 P95 仍未完成。

## 2026-08-01 P8.4 第十七增量：v3 实质证据与内容资格门禁

- [x] **向后兼容的 Evidence Contract**：`AgentTaskCase` 新增可选 `evidence`，区分 `sufficient/insufficient`，并对 Evidence/Citation/Claim ID、URL、数量、字符预算、重复值、未知引用和 Claim 是否由同一条引用正文共同支撑做严格加载校验。原始证据只参与数据集哈希与内存评分，不进入报告、Checkpoint 或日志；签名报告 schema 继续为 `agent-task-eval-report/v2`，旧 v2 数据集和历史报告保持可验。
- [x] **确定性内容门禁**：充分证据任务要求全部 Claim Terms 出现在最终答案，且精确 `[CitationID]` 位于声明前后 240 字符窗口内；同时识别“证据充分却拒答”、固定无依据声明和内部评测元数据泄漏。证据不足任务必须明确返回“未检索到可靠证据/现有证据不足/没有可核验来源”之一，并受独立长度和无依据短语约束。失败继续写入既有 `semantic_failure_codes`，不扩张报告契约。
- [x] **20 条独立 v3 数据集**：新增 `agent_strategy_cases_v3.json`，包含 8 条站内实质证据、8 条公网实质证据和 4 条空证据任务；原 v2 文件不覆盖。充分证据覆盖并发、可观测性、RAG、MCP、Workflow 恢复、成本预算、HPA、Context、供应链、工具链升级、Secret 轮换和引用治理等可核验事实与权衡。文件 SHA-256 为 `72a3e436...a42557`。
- [x] **Runtime v5 证据投影**：评测沙箱将 v3 数据投影到既有 `platform.tweet_search.v1`、`web.search.v1` 和 `web.page.v1`，没有新增第二套 Tool Schema；`page_read` 只允许读取 Case 白名单 URL。多角色 Handoff 使用数据集精确 Citation ID；空检索以明确 `no-evidence` 控制记录继续进入 Drafter/Reviewer，不伪造成外部来源，也不把合法证据不足误判为执行器故障。Runtime/Strategy Executor 身份升级到 v5，历史 v4 报告不改写。
- [x] **固定 Profile Set v3**：新增 `agent_strategy_runtime_config.qwen37-v3.example.json`，仍固定 `qwen3.7-plus-2026-05-26`、关闭 Thinking、版本化费率与环境凭据引用；Single/Researcher/Drafter/Reviewer 均要求声明旁精确引用，空结果禁止用模型常识补写。文件 SHA-256 为 `0fdb6454...be8c6b`。
- [x] **离线验证**：加载器拒绝未知 Citation、无正文支撑 Claim、非法空证据结构和非只读 Case；评分覆盖缺 Claim、缺引用、引用距离过远、充分证据拒答、无依据声明、元数据泄漏与空证据装作有答案。20 条 Grounded Fake 全部通过；Platform/Web/Page 投影、URL 白名单和 Multi `no-evidence` 通过。定向普通测试、`race`、影响范围五包测试和目标 `go vet` 均通过，全程未调用 DashScope、LM Studio、MinIO、Mongo、MCP 或公网。
- [x] **资格边界与后续**：确定性 Terms/短语/240 字符邻近度只能提高内容质量下限，不能独立证明开放域事实正确性、完整性或写作品质。绑定报告、数据集和规则版本的签认/Judge 契约已在第十八、十九增量完成，v3 云模型运行已在第二十增量完成；外部人工签认和 WORM Version ID 仍未完成，生产 Multi Feature Flag 继续关闭。

## 2026-08-01 P8.4 第十八增量：版本化内容签认闭环

- [x] **独立 Signoff 契约**：新增 `agent-task-content-review-decision/v1`、`agent-task-content-review-signoff/v1` 与 `agent-task-content-review-rules/v1`。签认独立于 `agent-task-eval-report/v2`，绑定报告 Payload/Key ID、数据集版本与哈希、Candidate/Stable 执行配置哈希、Review Bundle Schema/Key ID/文件哈希、决策哈希、审阅时间和逐 Case 输出哈希，不修改历史报告或数据库。
- [x] **完整覆盖与确定结论**：每个 Candidate/Stable Case 都必须给出事实正确性、相关性、证据忠实度和写作质量 `pass/fail`；Case 缺失、重复、错序、非法维度、总结果与维度不一致、未知字段、重复 JSON Key、报告/Bundle 替换和签名篡改均 fail-closed。签认不保存 Input、Output 或自由文本备注。
- [x] **人工/Judge 隔离**：外部人工只保存假名 ID、`asserted_external` 和外部审阅记录 SHA-256；这不是独立身份认证。Judge 必须绑定 Provider/Model/Prompt/Config SHA-256，且永远只作为辅助信号，不能产生 `external_human_approved=true`。外部人工批准仍只是生产资格必要条件，不替代 WORM、真实搜索和发布审批。
- [x] **离线 CLI 闭环**：新增 `--create-review-signoff` 与 `--verify-review-signoff`。两条路径先验报告 HMAC，再解密并逐 Case 核验 Review Bundle，最后使用第三把独立 HMAC Key 创建/验证不可覆盖 Signoff；显式拒绝报告、Bundle 与 Signoff 密钥或 Key ID 复用。验证过程不写第二份明文正文。新增默认全拒绝的 20 Case v3 决策模板。
- [x] **验证与兼容**：领域与 CLI 覆盖创建、严格解码、完整覆盖、结论推导、Judge 降级、报告/Bundle 替换、HMAC 篡改、明文不泄漏和显式内容授权。本轮仅使用 Fake/临时加密材料，不调用 DashScope、LM Studio、MinIO、Mongo、MCP 或公网。
- [x] **受控自动运行后续已执行**：用户已重新确认固定合成数据出站、Review Bundle 正文捕获和 qwen3.7 费用，第二十增量完成全新 v3 Candidate/Stable 20+20 并通过自动门禁。独立外部人工逐 Case 审阅、Signoff、Object Lock 归档和真实 Brave/Page Read 验收仍是后续硬门禁；此前生产 Multi Feature Flag 保持关闭。

## 2026-08-02 P8.4 第十九增量：内容签认进入 WORM 与发布资格链

- [x] **资格证据包契约**：新增 `agent-task-content-qualified-evidence/v1`，把已签名 `agent-task-eval-report/v2` 与 `agent-task-content-review-signoff/v1` 组合为同一不可变对象。构造与解码同时复验报告/Signoff 双 HMAC、报告/Bundle/逐 Case 摘要绑定和外部人工批准；Judge 即使给出 approved 也不能形成生产资格对象。严格 JSON 拒绝未知字段、重复 Key、密钥或 Key ID 复用。
- [x] **归档前完整复验**：`agent-task-eval` 新增 `--archive-content-signoff`。操作员必须同时提供原报告、加密 Review Bundle、Signoff、显式正文授权以及三把独立密钥；CLI 在内存中重新解密并逐 Case 核验 Bundle 后才上传资格对象。`--require-archived-content-signoff` 按精确 MinIO Version ID 回读并复验双 HMAC，无需把 Review AES Key 或正文交给服务进程。
- [x] **发布门禁与兼容回滚**：新增默认关闭的 `AGENT_PROFILE_EVAL_CONTENT_SIGNOFF_REQUIRED`。开启时依赖原 `AGENT_PROFILE_EVAL_EVIDENCE_REQUIRED`，Profile 提交、批准和失败恢复只接受外部人工已批准的资格对象；关闭时继续兼容旧裸报告回执。现有 Proto/Gateway/Web `quality_evidence` 回执字段不变，`report_sha256` 在新模式下覆盖整个资格对象，历史归档不迁移、不改写。
- [x] **部署与验证**：Compose、Helm 和 `.env.example` 增加独立 Signoff Secret/Key ID，Helm 非法开关组合模板期失败。Eval、ObjectStore、CLI 和 Agent Service 四包定向测试通过；Helm 默认模板与 Compose 配置解析通过。全程未调用 DashScope、LM Studio、MinIO、Mongo、MCP 或公网。
- [x] **真实自动证据后续已执行**：第二十增量已获得当前轮次的数据出站、正文捕获和费用授权，并生成通过自动双门禁的 v3 qwen3.7 报告与加密 Bundle。真实外部人工签认、Object Lock Version ID、真实 Search/Page Read 召回与公网 P95 仍未完成；生产 Multi Flag 保持关闭。

## 2026-08-02 P8.4 第二十增量：v3 云评测、失败诊断与 Profile Set v5

- [x] **失败门禁可加密诊断但不可签认**：`agent-task-eval` 新增显式 `--capture-failed-review-bundle`，只允许和 `--review-bundle --allow-review-content` 一起使用。自动门禁失败时可写入绑定签名报告的 AES-256-GCM 诊断 Bundle；领域 Signoff 创建仍强制质量门禁与策略门禁同时通过，失败 Bundle 不能形成外部人工 Signoff 或 WORM/Profile 资格证据。
- [x] **Profile Set v3/v4 诚实收敛**：固定 `qwen3.7-plus-2026-05-26`、同一 20 条 `agent-strategy-cases-v3` 的首轮 v3 为 Candidate `12/20`、Stable `8/20`，第二轮 v4 为 Candidate `16/20`、Stable `15/20`，两轮均只因 Candidate 语义通过率低于 90% 而拒绝晋级。加密诊断确认 v4 已修复 4 条空证据任务的过短回答和错误拒答，剩余问题收敛为跨角色交接遗漏数字、单位、否定结果或治理约束；没有放宽数据集、Terms、Citation 邻近度或策略门槛。
- [x] **不可变 Profile Set v5**：新增 `agent_strategy_runtime_config.qwen37-v5.example.json`。Single 稳定侧的 Prompt 正文、工具权限和预算与 v4 保持一致，只按原子 Profile Set 契约提升版本元数据；Multi Researcher/Drafter/Reviewer 增加按标点拆分的事实 Coverage Unit、精确来源短语、数字+单位、成对数值、零/否定结果和限制条款静默核对。配置文件 SHA-256 为 `026ff998...04615`。
- [x] **第三轮自动双门禁通过**：Candidate `19/20`、Stable `15/20`，两侧任务完成率、读工具选择和工具成功率均为 100%，无执行错误、预算终止、越权写或伪造工具结果。Candidate 语义率 95%，相对 Stable 增益 `2000 bps`；平均估算成本倍率 `1.7911x`，P95 倍率 `1.9362x`，Candidate P95 `15139ms`，均通过既定 `90% / 500bps / 3x / 3.5x / 60s` 门槛。唯一失败 `strategy-v3-web-003` 保留正确 Citation 和全部必需关键词，但遗漏来源精确短语“读写权限”，已留给人工复核。
- [x] **费用与完整性**：第三轮 Candidate/Stable 报告估算费用分别为 `0.176430 / 0.098508 CNY`，合计 `0.274938 CNY`；三轮完整 20+20 报告累计估算 `0.814168 CNY`，另有两次不进入报告的预检调用，因此 Provider 最终账单可能略高但仍预计低于授权的 `1 CNY`。报告 Payload SHA-256 为 `dc2b2500...971fb`，Bundle SHA-256 为 `1f8e5879...684b2`；独立 HMAC 验签、DPAPI Key 解密、Bundle 报告绑定和 40 份正文哈希均通过，正文未输出终端。
- [x] **本地验证与生产边界**：Eval/CLI 定向普通测试、Race、Vet 和 v5 Profile 快照回归通过；回归保证 Stable 除版本元数据外与 v4 一致，并拒绝 Profile Set 混合版本。当前仅取得自动评分资格，不是 `external_human_approved`；下一步必须由独立外部人工逐 Case 审阅 40 份正文并生成 Signoff，随后才能归档到 Object Lock、验证真实 Search/Page Read 和决定是否开启生产 Multi Feature Flag。
- [x] **授权数量边界已最小化处置**：费用仍在授权范围内，但调优过程曾产生 v4、v5 两份各含 40 份正文的加密 Bundle。用户确认后已删除 v4 诊断 Bundle以及 v4/v5 的 `review.opened.json` 明文；当前仅保留最终 v5 签名报告、40 份正文加密 Bundle 和 DPAPI 密钥，所有产物仍位于被忽略的本地 `tmp`。后续 Live Eval 必须同时记录运行次数、Provider 调用数、正文捕获数和费用硬上限。

## 2026-08-02 P8.4 第二十一增量：Live 授权预算与外部人工交付

- [x] **独立签名授权清单**：新增 `agent-task-live-authorization/v1` 与 `--create-live-authorization`。授权严格绑定 Provider/Model、数据集版本/SHA-256、执行配置 SHA-256、签发/过期时间，以及最大运行次数、Provider 调用数、正文捕获数和估算成本；创建命令只读取本地配置和数据集，不构造 Provider 请求。授权 HMAC 与报告 HMAC、Review AES Key 必须独立。
- [x] **调用前预算硬拦截**：所有 Live Runtime/Strategy Eval 必须显式提供 `--live-authorization` 和持久 `--live-authorization-state`。运行启动先预留 Run 与正文数量；每次模型调用根据保守输入 Token 上界、最大输出 Token 和固定 Pricing 在委托 Client 之前预留调用次数与估算成本。授权过期、身份漂移、预算耗尽、记录篡改、序号缺口或未知文件均在出网前 fail-closed，进程崩溃不返还已预留额度。
- [x] **Append-only 本地账本与报告证据**：每个授权 ID 使用不可覆盖事件文件、HMAC 签名和前向 Payload SHA-256 链累计消费，竞争提交通过原子文件创建/链接重试。最终 `agent-task-eval-report/v2` 可选携带授权 ID、授权 Payload 摘要、Key ID、调用实例摘要和批准上限，并受既有报告签名保护；旧离线/历史报告省略该字段，签名形状保持兼容。
- [x] **外部人工 Decision 交付模板**：`--open-review-bundle` 可同时使用 `--review-decision-template` 生成绑定报告与 Bundle 的 `agent-task-content-review-decision/v1` 初稿。模板不复制正文或输出哈希，Candidate/Stable 每个 Case 的事实正确性、相关性、证据忠实度和写作质量均默认失败，总 Verdict 默认拒绝，Reviewer ID、外部记录 SHA-256 和审阅时间必须由真实外部人工补全；原模板不能直接生成批准 Signoff。
- [x] **本地验证与外部边界**：Eval/CLI 定向普通测试和 Race、并发账本 20 次重复测试、完整 `internal/module/agent/...` 串行回归及 Agent/CLI Vet 通过；覆盖授权创建/绑定/过期、跨实例原子预算、预算耗尽、账本篡改、调用前拒绝、授权证据防篡改、旧报告兼容和模板失败关闭。本轮未连接 DashScope、LM Studio、MinIO、Mongo、MCP 或公网，也未产生新费用或正文。
- [ ] **剩余生产资格**：账本当前是同一受控工作区、同一持久 State Root 内的本地护栏，不是跨主机中央配额服务；操作员不得复制授权后切换 State Root。下一步仍必须由独立外部人工完成 40 份正文 Decision/Signoff，再取得 Object Lock Version ID 并验证真实 Search/Page Read；完成前生产 Multi Feature Flag、并行角色、角色级恢复与写工具多角色执行继续关闭。

## 2026-08-02 P8.4 第二十二增量：Live 离线额度计划与模型迁移护栏

- [x] **版本化离线资源计划**：新增 `agent-task-live-plan/v1` 与 `--plan-live-evaluation`。命令只读取数据集和规范化 Runtime 配置，不读取 API Key、不构造 Provider Client；输出精确 Provider/Model、数据集/配置摘要、分侧 Case/Strategy、共享预检、调用最小值与上界、Token/成本硬预算及正文捕获上限。
- [x] **授权覆盖门禁**：Live Authorization 签发不再接受小于完整计划上界的 Provider 调用数或估算费用；Review 正文配置为 `0` 时明确禁止捕获，配置为非零时必须覆盖 Candidate/Stable 全量正文。这样授权不会在已知 `MaxSteps`/Profile 预算内签出一个必然可能中途耗尽的批次。
- [x] **固定 qwen3.7 计划证据**：`qwen3.7-plus-2026-05-26`、20 条 v3 Case 与 Profile Set v5 的本地结果为 `121..241` 次 Provider 调用、`1,240,482` Token 上界、`4,701,348` 微计价单位费用上界和 40 份可选正文。该上界约束授权，不代表实际消费预测；历史 v5 完整运行实际报告费用仍以既有签名报告为准。
- [x] **模型替换隔离**：计划显式声明模型按 `provider + exact_model + execution_config_sha256` 固定。切到 `qwen3.7-plus` 等其他标识会生成不同配置哈希，旧授权无法匹配，且必须新建资格报告；不能只替换名称后沿用 `qwen3.7-plus-2026-05-26` 的结果。
- [x] **本地验证边界**：CLI/Builder 测试覆盖 v5 精确预算、无 Credential 离线计划、不可覆盖写、授权调用/成本/正文不足拒绝、正文零捕获和模型变更哈希；CLI 普通测试与 Race、完整 Agent 模块串行回归和受影响包 Vet 通过。本轮未调用 DashScope 或任何外部服务。生产资格下一步仍是独立外部人工 Signoff，随后才允许 WORM 和真实 Search/Page Read 验收。

## 2026-08-02 P8.4 第二十三增量：Redis 多实例 Live 授权账本

- [x] **可选共享后端且保持本地兼容**：`agent-task-eval` 新增 `file|redis` 授权状态后端；默认 `file` 继续使用既有不可覆盖 HMAC 前向哈希事件文件，Redis 代码只存在 CLI 运维边界，不进入通用 Agent Runtime、Service 或模型适配器。Redis 配置使用严格 `agent-task-live-authorization-redis-config/v1`，凭据只能引用环境变量，并支持显式 TLS Server Name 与有界连接/操作超时。
- [x] **初始化与消费分离**：新增 `--initialize-live-authorization-state`。它只验签已有授权并初始化 Redis，不读取模型 Credential、不构造 Provider Client。Live 运行只消费已存在且身份完全匹配的共享账本，从不自动初始化；未初始化、授权/Key/上限漂移、过期或 Redis 状态不完整均在 Provider 请求前失败关闭。
- [x] **跨连接原子预算与重试幂等**：单个 Lua 事务使用 Redis 服务端时间校验有效期，原子检查并累计 Run、Provider Call、正文捕获和估算成本，同时写入审计 Stream 与 Reservation 去重摘要。不同 Redis 连接并发共享同一额度；同一 Reservation 的网络重试不会重复计费，超限调用不会到达委托模型客户端。
- [x] **状态丢失防恢复与报告证据**：每个授权建立不设 TTL 的持久 marker。State Hash 或 Audit Stream 被逐键删除/提前驱逐时，运行和重复初始化都要求撤销授权，不能重置额度。Redis 报告证据新增可选 `state_backend=redis` 与 Namespace SHA-256；历史离线/file 报告省略字段，原 v2 签名形状保持兼容。Redis 管理员、`FLUSHALL` 和 marker 删除仍属于明确信任边界，生产要求 ACL、TLS、AOF/备份和 `noeviction`。
- [x] **本地验证与外部边界**：CLI/Eval 普通测试和 Race、不同 Redis Client 并发预留、初始化幂等、Reservation 重放、额度越界、State/身份篡改、未初始化调用前阻断、完整 `internal/module/agent/...` 串行回归及目标 Vet 全部通过。测试使用进程内 `miniredis`；本轮未连接用户 Redis、DashScope、LM Studio、MinIO、Mongo、MCP 或公网，未产生模型费用。
- [ ] **剩余生产资格不变**：共享账本消除了受控多实例评测的本地 State Root 分叉，但不替代独立外部人工对 40 份正文的 Decision/Signoff，也不替代 WORM Version ID 和真实 Search/Page Read 验收。完成这些外部证据前，生产 Multi Feature Flag、并行角色、角色级恢复和写工具多角色执行继续关闭。

## 2026-08-02 P8.4 第二十四增量：Redis Live 授权检查与撤销

- [x] **脱敏状态检查**：新增 `--inspect-live-authorization-state`，输出版本化机器可读 JSON，仅包含授权和 Namespace 摘要、状态、四类额度/用量、审计序号及 Redis 服务端时间。命令只验签授权并读取共享账本，不读取模型 Credential、不构造 Provider Client，也不回显 Redis 地址、凭据、Prompt 或正文。
- [x] **原子撤销与不可回退审计**：新增 `--revoke-live-authorization-state`、伪名操作人和固定原因码。正常撤销在单个 Lua 事务内保留累计用量、冻结 marker/state、递增序号并追加 `authorization_revoked` Stream 事件；Redis 只保存操作人 SHA-256。重复撤销不重复写事件，撤销后初始化和预算预留均明确失败。
- [x] **状态丢失事故收口**：State/Stream 不完整时只允许 `state_integrity_incident`，仅把持久 marker 标记为 `marker_only` 撤销并报告 `integrity_status=state_lost`；不会重建零用量 Hash/Stream。该模式能冻结授权，但不能恢复或证明丢失前的完整审计历史。
- [x] **本地验证边界**：CLI/账本普通测试与 Race 覆盖状态快照、用量保持、序号递增、并发预留/撤销串行化、撤销幂等、操作人脱敏、双状态防误恢复、事故原因门禁和状态不重建；并发用例连续 20 次通过。完整 `internal/module/agent/...` 串行回归与目标 Vet 通过。测试使用进程内 `miniredis`；本轮未连接真实 Redis、DashScope、LM Studio、MinIO、Mongo、MCP 或公网，未产生模型费用。
- [ ] **生产资格门保持不变**：这项运维能力不替代 40 份正文的独立外部人工 Decision/Signoff、Object Lock Version ID、真实 Search/Page Read 或公网 P95。资格链完成前继续保持生产 Multi Feature Flag 关闭。

## 2026-08-02 P8.4 第二十五增量：Redis Stream 逐事件重放审计

- [x] **原子审计边界与有界分页**：`inspect` Lua 快照同时捕获用量、序号、事件数和末端 Stream ID；Go 管理路径只分页读取到该游标，快照之后的并发 Reservation 不会混入旧状态。事件数受授权最大 Run/Provider Call 数反向约束，读取使用 512 条分页和 30 秒总审计超时。
- [x] **逐事件语义校验与状态对账**：重放要求精确事件字段集合、从 0 连续序号、严格递增 Stream ID、授权窗口内单调事件时间和唯一可选撤销终态；Run/Provider Call 增量形状、四类累计用量及撤销摘要必须与 Hash/marker 快照一致。成功输出 `replay_status=verified`、`verified_event_count`、`last_stream_id` 与规范化 `stream_sha256`。
- [x] **写入协议双层失败关闭**：Go 入口与 Redis Lua 同时约束 `run_reserved` 只能增加一次 Run（可预留正文），`provider_call_reserved` 只能增加一次 Provider Call（可预留费用），并拒绝非法事件类型、负增量和无效 Subject 摘要，避免语义畸形事件进入共享账本。
- [x] **篡改与并发边界回归**：新增“删除合法事件并补入伪造事件但保持 `XLEN` 不变”的平衡篡改用例，旧长度/序号关系仍成立时，重放会因用量对账失败；另用 521 条事件跨越分页边界，并在快照后追加事件，验证旧游标与摘要稳定。关键并发/分页/篡改用例连续 20 次、Redis 账本 Race、完整 CLI 与 Agent 模块串行回归及目标 Vet 全部通过。
- [ ] **信任边界与生产资格不变**：Stream SHA-256 是可比较的防篡改摘要，不是 HMAC 或 Redis 管理员不可抵赖证明；生产仍需最小 ACL、AOF/备份、`noeviction` 和 Redis 外 WORM 留存。外部人工 Signoff、Object Lock Version ID、真实 Search/Page Read 与公网 P95 未完成，Multi Feature Flag 继续关闭。

## 2026-08-02 P8.5 第一增量：Unified Agent 产品 SLI 基础

- [x] **权威生命周期计数**：新增可注入 `UnifiedAgentProductObserver` 与生产 Prometheus 实现。任务开始只跟随 `AgentExecutionRun` 成功创建，Outcome 及其耗时/用量只跟随 Revision CAS 成功提交；创建失败不计为已接受，提交失败不伪造成完成结果。
- [x] **完成任务成本与失败密度**：新增 `agent_unified_tasks_started_total`、Outcome、Duration、Step、Token、微单位成本和 Tool Result 指标，可按 `outcome=completed` 计算完成率、P95、每完成任务 Token/成本与失败 Tool 数。Tool Name、用户、Run、模型和错误原文不进入 Label。
- [x] **Citation 结构有效率**：对用户可见平台推文和网页 Citation 做固定类型、ID 与资源定位绑定校验，输出 `source_type + validity` 低基数计数。该指标只证明结构契约，不证明事实正确、网页在线或回答 Grounded；语义质量继续由 v3 Eval 和外部人工签认负责。
- [x] **离线与线上职责分离**：固定任务集继续负责工具选择准确率和任务级澄清率；线上 `awaiting_human` 仅作为可重复的澄清转换信号，生产遥测不猜测用户真实意图。指标当前随 `AGENT_RECOVERABLE_RUNS_ENABLED=true` 的权威生命周期启用。
- [x] **离线验证**：低基数/结构有效性/成功提交边界/工具失败投影目标测试、完整 Agent 模块回归、Service Race、`cmd/agent-service` 编译和目标 Vet 全部通过；未启动 Redis、LM Studio、MinIO、Mongo、MCP 或公网服务，也未调用 DashScope。
- [x] **P8.5 后续**：发布采纳率与 Connector 激活/复用率已由跨请求、可重放、幂等产品事件归因完成；不以发布调用次数、进程内缓存或 Session 打开次数冒充业务指标。租户已安装的联合扩展目录已由第三增量完成。

## 2026-08-02 P8.5 第二增量：草稿采纳与 Connector 产品漏斗

- [x] **权威草稿采纳状态**：`AgentExecutionRun` 持久化 `publishable_draft`、首次发布 Tweet ID 与时间。确认发布只接受已完成且可发布的来源 Run，并用 Mongo 原子条件更新实现跨请求幂等；重复确认和 TweetService 幂等重放不会重复计数。
- [x] **只追加产品事件**：新增 `internal/module/agent/product` 领域和 Mongo `agent_product_events` Repository。确定性事件 ID 支持进程重启后的重放与修复，事件不包含 Prompt、正文、Tool 参数/结果、Endpoint 或凭据。
- [x] **Connector 真实激活与复用**：配置成功产生 `configured`；首个已审核工具被显式启用才产生 `activated`；治理后的工具调用成功才产生 `first_used`。同一 Connector 必须在至少两个不同 Agent/Workflow Run 中成功执行才产生一次 `reused`，同 Run 重试和 Session Pool 复用均不算产品复用。
- [x] **低基数指标投影**：新增草稿 `ready/published` 与 Connector `configured/activated/first_used/reused` Counter，只使用执行 Profile、策略、作用域和传输类型等固定枚举 Label。Prometheus 用于趋势，严格 Cohort 分析保留在授权后的 append-only 事件层。
- [x] **离线验证**：完整 `internal/module/agent/...` 回归、Product/Repository/Remote MCP/Agent Service 目标 Race、目标 Vet 与 `cmd/agent-service` 编译全部通过；重放测试覆盖第二个 Run 的使用事实已落库但复用标记尚未落库的中断窗口。未启动 Redis、LM Studio、MinIO、Mongo、MCP 或公网服务，也未调用 DashScope。
- [x] **P8.5 目录后续**：Capability/Skill/MCP 联合目录已由第三增量完成；真实 Mongo 下的事件索引/重放、真实外部 Connector 漏斗和看板展示仍需独立受控验收。

## 2026-08-03 P8.5 第三增量：租户已安装扩展目录

- [x] **统一只读契约**：新增 `internal/module/agent/extension`，定义 `agent.extension.v1`、三类来源、稳定排序、过滤绑定 Cursor、最大 256 项和单页 50 项硬上限；纯领域测试覆盖分页、过滤漂移、重复 ID、非法条目和副本隔离。
- [x] **复用权威来源**：Service 只聚合不可变 Capability Catalog、当前租户精确版本 Skill 和受治理 MCP Tool。MCP 仍由当前成员关系、Active Snapshot 与 Enabled Policy 过滤，响应不携带 Endpoint、Credential、输入 Schema 或 Tool Result。
- [x] **API 与 Web 使用面**：Proto 仅追加 `listAgentExtensions`；Gateway `GET /api/v1/agent/extensions` 只信任 JWT 租户并透传稳定 Cursor。Web 助手工具栏新增扩展目录，Skill 使用前精确解析版本，MCP 仅跳转既有管理/审核界面。
- [x] **独立灰度与回滚**：`.env.example`、Compose 和 Helm 增加 `AGENT_EXTENSION_CATALOG_ENABLED=false` 与页大小配置。关闭只撤销目录读取，不改变 Workflow Publication、Skill 投影、MCP Connection/Snapshot/Policy 或任何执行授权。
- [x] **离线验证**：`extension`、Remote MCP、Agent Service、gRPC、Gateway Handler/Router 六包普通回归通过；Extension/Service/Remote MCP/Gateway 目标 Race 与七包串行 Vet 通过，Agent 组合根编译通过。Proto 重生成稳定，Compose 静态配置、Helm 正常渲染与非法目录上限拒绝、Web `vue-tsc + vite build` 均通过；未启动 Mongo、Redis、RabbitMQ、Temporal、LM Studio、外部 MCP 或公网服务，也未调用模型 Provider。
- [ ] **真实性边界**：第四增量已补发布者身份与包签名的只读市场基础，但本轮目录自身仍不是安装市场。安装审批、依赖解析、恶意扩展扫描、Artifact 分发和版本撤回仍需独立控制面；真实项目跨成员目录和远程 MCP 健康状态还需受控环境验收。

## 2026-08-03 P8.5 第四增量：签名公共扩展市场基础

- [x] **独立供应链领域**：新增 `internal/module/agent/marketplace`，定义发布者、公钥状态、规范 Manifest、不可变 SemVer Release、声明权限、稳定 Cursor 和 Ed25519 签名复验。版本 ID 绑定规范清单；发布者暂停、密钥撤销、签名/摘要篡改和查询条件漂移均 fail-closed。
- [x] **只读运行时边界**：新增两个独立 Mongo Collection；发布者注册和签名版本写入只作为未来离线/控制面 Repository 能力，Service 仅依赖只读 `CatalogStore`，批量读取发布者并对每个结果重新验签。市场不会写入 Capability、Skill、MCP、Workflow 或 Runtime 权限。
- [x] **无密钥 API 与 Web**：Proto 仅追加市场查询 RPC；Gateway `GET /api/v1/agent/marketplace/extensions` 只信任 JWT 并返回 `no-store` 投影。Web 新增搜索、类型过滤和 Cursor 加载更多的只读市场面板，不展示安装按钮，也不返回公钥、原始签名、Artifact URL/字节、Endpoint 或 Credential。
- [x] **独立灰度与回滚**：`.env.example`、Compose 和 Helm 增加 `AGENT_EXTENSION_MARKETPLACE_ENABLED=false` 与 1-50 页大小配置。关闭时不创建市场索引，不删除市场记录，也不影响租户已安装扩展目录。
- [x] **离线验证**：Marketplace/Repository/Service/gRPC/Gateway/Agent 组合根完整受影响包回归通过；Marketplace、Repository、Service、gRPC 与 Gateway 目标 Race 和七包串行 Vet 通过。Proto 重生成前后 SHA-256 一致，Web `vue-tsc + vite build`、Compose 解析、Helm Lint/开启态渲染及 `catalogLimit=51` 负向门禁通过；Vite 预览由 HTTP 200 与监听端口双重确认。测试未连接 Mongo、Redis、RabbitMQ、Temporal、LM Studio、模型 Provider、外部 MCP 或公网。
- [x] **发布治理后续**：第五增量已补发布者归属、独立内部认证、密钥轮换/吊销、签名发布、版本撤回和追加审计；公开目录与管理控制面仍保持独立开关。
- [ ] **完整市场仍未完成**：Artifact 存储与下载、安装审批、依赖解析、恶意包扫描、所有者转移、租户安装状态和真实 Mongo 多副本验收仍需后续增量；当前不能宣称开放可安装市场。

## 2026-08-03 P8.5 第五增量：扩展发布与信任控制面

- [x] **独立认证与所有权**：新增 Marketplace 专属内部令牌和平台管理员配置，不复用 Profile RBAC。平台管理员注册发布者及不可变所有者；后续每次密钥、发布与撤回操作都由 Agent Service 从 Mongo 重新校验 Owner，Gateway 只负责 JWT 操作者和内部令牌注入。
- [x] **公钥生命周期**：系统只接收 Ed25519 公钥。轮换使用 Revision CAS，将旧 Active Key 转为 Retired；Retired Key 只能维持历史验签，不能发布新版本。吊销是终态，并使相关历史签名不再可信。私钥不进入 API、Repository、审计或响应。
- [x] **签名发布与撤回**：发布者提交规范 Manifest 与离线签名，服务端重新规范化、要求 Active Key、生成稳定 Release ID 和发布时间；同 Package/SemVer 不可覆盖。撤回使用 `published -> withdrawn` 终态迁移，保留签名证据和唯一版本记录。
- [x] **CAS 与追加审计**：发布者、版本记录均增加 Revision 和操作者时间戳；所有写操作先后追加 `requested/succeeded/failed` 事件，审计只包含低敏对象 ID、固定 Action/Outcome/Reason/Error Code，不记录签名、Artifact、Credential 或内容。
- [x] **API 与管理页**：Proto 追加独立管理 RPC；Gateway 提供 Access、Publisher、Key、Release 与 Audit REST API并返回 `no-store`。Web 新增 `/agent/marketplace/manage` 独立控制台，公开市场仍只读且不出现安装按钮。
- [x] **独立灰度**：`AGENT_EXTENSION_MARKETPLACE_ADMIN_ENABLED=false` 可在公开目录关闭时单独编排版本；Compose 与 Helm 将同一 Secret 注入 Gateway/Agent，Helm 对 Secret 和管理员列表做模板期门禁。关闭不会删除发布者、版本和审计。
- [x] **离线验证**：Marketplace/Repository/Service/gRPC/Gateway 五层目标测试与拆分 Race、八个受影响包串行 Vet、Agent/Gateway 命令编译、Proto 重生成哈希、Web 生产构建、Compose 解析、Helm Lint/正负向渲染和 Vite 管理路由 HTTP 200 均通过；未启动或访问 Mongo、Redis、RabbitMQ、Temporal、LM Studio、模型 Provider、外部 MCP 或公网。
- [ ] **待受控验收**：真实 Mongo CAS/索引、多副本并发写、Secret 轮换和撤销后大目录可用性仍需受控环境验证。Artifact 分发、扫描、依赖解析、安装审批与租户安装继续明确不在本增量。

## 2026-08-03 P8 产品收口审计：范围冻结

- [x] **进度判定**：Runtime、Tool、Workflow、Approval、Replay、联网、远程 MCP、Workflow-as-Tool、受限 Multi-Agent、Eval 与扩展信任控制面的代码主干已形成；当前主要缺口是统一入口体验、真实环境价值链、可审查交付基线和可重复演示，不是组件数量。
- [x] **市场与学习双目标**：产品定位固定为“面向社交内容研究、创作与运营的可治理 Agent 工作台”；技术学习聚焦 Go Runtime、ReAct、MCP、DAG、恢复、RAG、预算和可观测性。核心价值链固定为取证、带来源草拟、审批发布、自动化和效果追踪。
- [x] **停止扩张**：公共市场安装链路、并行/角色级恢复、多角色写协作、SubWorkflow/MapReduce/Aggregator、事件触发器、代码沙箱、新 Provider 和新中间件均转为需求驱动延期项，不再作为默认下一轮。
- [x] **G0 交付基线执行中**：已将 24,903 条原始状态中的 24,280 条本地 Go Cache 隔离，并把剩余工作区审计为 607 个工程文件和 16 个明确排除项；`docs/agent/DELIVERY_BASELINE.md` 固定九个堆叠审查批次、跨阶段热点文件、验证矩阵和回滚边界。00 Repository Governance 已提交为 `ce9ad55`；01 Platform Reliability 完成 72 项精确暂存、Proto 哈希、普通测试、Race、Vet 与隔离索引快照复验后提交为 `849cb56`。02 Agent Core 将初始 146 项文件名分类收紧为 53 项真实依赖闭包，分别以 Runtime Foundation `79ceb30`、Runtime 持久化 `b23e164` 和安全 Provider HTTP 路由 `d4cd553` 提交；三个索引快照均通过目标测试，涉及并发的包通过 Race，目标 Vet 通过。后续文件已按真实依赖归入 03 至 06 并逐批收口；Mobile、学习文件、课程报告删除和临时脚本均未触碰。
- [x] **G0 03 Workflow/RAG 已提交**：依赖闭包重审后，以确定性 DAG `70d696e`、分层 RAG/Memory `42b889b`、统一 Tool 治理 `febf094` 和持久 Run/Repository `5708150` 四个提交完成 52 项；索引快照发现并修复节点取消与临时错误竞态，另补齐 Agent Run 核算版本依赖。Tool 的只读检索依赖先以 04A Search/Evidence `dc753cd` 提交 14 项。当前纯 `HEAD` 快照对 02-03/04A 的 19 个包联合普通测试、9 个并发敏感包 Race 和 19 个包 Vet 均通过，未启动真实模型、ES、Qdrant、Mongo、Redis、MCP 或公网服务。Workflow Service 与 Cognitive/Session 适配按真实依赖改归 06 集成审查。
- [x] **G0 04 Search/MCP 已提交**：Search/Evidence 前置 `dc753cd` 与项目权限 `1214f65`、远程 MCP Runtime `15f9a63`、Connector 持久化 `f8e52e7`、内部 MCP 安全工具面 `cd32cf0`、签名验收工具 `9d3f547` 合计完成 62 项。纯 `HEAD` 快照对 10 个目标包普通测试和 Vet 全部通过，各索引快照的并发敏感包 Race 通过；同时修复取消 Context 复用 Session、Provider Config 空 Collection、亚毫秒 Redis TTL、发推幂等缺失和验收体积无上限等问题。测试只使用临时回环 Conformance 服务，未启动或访问真实 MCP、Brave、Mongo、Redis、模型或公网。三个 Service 组合适配文件按依赖保留给 06/07，不属于 04 欠账。
- [ ] **完成门禁**：依次完成九批精确暂存、分批验证和提交，把 G0 从 `Prepared` 改为 `Complete`；随后完成统一助手体验收口、联网/MCP/Workflow-as-Tool 三条真实价值链验收，以及十分钟演示/失败演示/启动手册/产品指标看板。四道门禁完成后 P8 进入 Maintenance，不自动开启新阶段。
- [x] **规划同步**：`docs/agent/UNIFIED_AGENT_PRODUCT_PLAN.md` 第 10 节、`docs/AGENT_RUNTIME_STRENGTHENING_PLAN.md`、Agent Runtime Context 和 Technical Debt 已同步范围冻结与真实状态。

## 2026-08-03 P0 稳定性增量：Agent 运行角色与热点播报单一 Owner

- [x] **可测试启动计划**：新增 `internal/module/agent/startup`，严格解析 `AGENT_PROCESS_ROLE=api|worker|all` 与 `AGENT_TRENDING_REPORTER_OWNER=temporal|disabled`；空值使用兼容默认值，未知角色、旧 `local/auto` owner 启动即失败。
- [x] **顶层生命周期隔离**：`api/all` 才启动 gRPC、Consul、内部 MCP、Semantic Anchor 初始化和 API 本地异步实验记录器；`worker/all` 才连接 RabbitMQ 并启动内容归因 Consumer、实验/工具/补偿巡检、Temporal Worker 与影子风控。Profile Catalog 同步保留在所有角色，避免 API 进程配置陈旧。
- [x] **唯一 Reporter Owner**：生产组合根删除进程内 `TrendingReporter.Start` 自动接线，只由固定 ID 的 Temporal `TrendingReporterWorkflow` 调度；Temporal 不可用时明确停播，不隐式切换第二条模型/发推链路。`AGENT_TRENDING_REPORTER_OWNER=disabled` 是无数据迁移的回滚开关，间隔由 `AGENT_TRENDING_REPORTER_INTERVAL` 配置。
- [x] **部署兼容**：`.env.example`、Compose 与 Helm 均默认 `all + temporal`；Helm 对角色与 owner 做模板期枚举校验。当前仍是一个 Deployment，独立 API/Worker Deployment 和组合根物理拆分保留为下一项架构债。
- [x] **离线验证**：启动计划普通测试、Race、Vet 通过；`cmd/agent-service` 在关闭隐式 Vet 后完成编译，`main.go` 定向 Vet 通过。完整包隐式 Vet 扫描 Elasticsearch 大型生成依赖时超时，已按真实边界记录到 `docs/ISSUES.md`。未启动 Mongo、Redis、RabbitMQ、Temporal、LM Studio 或公网服务，也未调用 DashScope。

## 2026-08-03 P0 稳定性增量：Agent 风控退出社交数据面

- [x] **最小只读信号**：Tweet Proto 增加内部 `GetAuthorPostingStats`，只返回窗口内最多两条创建时间戳；5 秒频率阈值仍由 Agent 风控策略判断，Agent 不读取 Tweet 表或正文。
- [x] **幂等治理命令**：内部 `ApplyTweetModeration` 当前仅支持 Shadowban。TweetService 校验 Tweet/Author、幂等更新可见性、严格失效基础缓存，并按 500 条分批用 Lua 原子移除活跃粉丝 Timeline 项和未读计数；重试只在 `ZREM` 实际命中时扣减未读。
- [x] **组合根降权**：`AgentActivities` 使用只包含 Create/Stats/Moderation 的窄客户端接口；`cmd/agent-service` 不再向 Temporal Activity 注入 GORM 或 FollowRepository。热点 Redis、ES、Qdrant 继续作为有界只读投影。
- [x] **兼容与离线验证**：Proto 仅追加 RPC/消息/枚举，并用描述符测试固定方法、字段号和枚举值；现有 Gateway HTTP API 不暴露特权治理命令。受影响包普通测试/Vet/Race、`internal/...`、`pkg/...` 与 `cmd/...` 分组回归全部通过。单体 `go test ./...` 因仓库枚举在 180 秒超时，未冒充全仓命令通过，见 Issue 152。测试使用 `miniredis`，未启动外部服务、模型或公网连接。
- [x] **服务身份认证**：已由下一增量补齐方法级认证、默认失败关闭和共享 Secret 部署契约；不把静态 Bearer 描述成 mTLS。
- [ ] **后续生产增强**：将最多 5000 活跃粉丝的同步清理演进为事务事件驱动、可重放的全量分批任务，并增加分类重试、DLQ 与进度指标。

## 2026-08-03 P0 稳定性增量：特权 Tweet RPC 服务身份认证

- [x] **方法级最小权限**：新增 `pkg/serviceauth`，只保护 `GetAuthorPostingStats` 与 `ApplyTweetModeration`。客户端仅对这两个完整方法名注入身份和 Token，普通 Tweet RPC 不携带内部凭据；服务端拒绝缺失、重复或错误 Header，并使用常量时间比较 Token。
- [x] **默认失败关闭**：未配置 `TWEET_INTERNAL_SERVICE_TOKEN` 时，Tweet 服务继续提供公开业务 RPC，但两个特权 RPC 固定返回 `Unavailable`；Agent 在出网前返回 `FailedPrecondition`。短 Token、空身份或非法身份会在组合根启动时失败，不存在匿名兼容回退。
- [x] **可部署与可回滚**：Compose 由同一环境变量向 Agent/Tweet 注入凭据；Helm 使用 `tweetService.riskControlAuth` 将同一 Kubernetes Secret 挂载到 Agent、Tweet v1 和 v2，启用但未填写 `existingSecret` 时模板渲染失败。关闭 Helm 开关会撤下 Secret 并自动回到“特权 RPC 不可用、普通 RPC 可用”的安全状态。
- [x] **低敏审计与验证**：拒绝日志只包含固定方法名和 `missing/invalid/unconfigured` 结果，不记录凭据或用户。拦截器单元测试覆盖方法隔离、错误/重复凭据、客户端覆盖旧 Header 与 fail-closed；Tweet/Agent 组合根串行编译、默认/启用 Helm 渲染、缺 Secret 负向门禁、Compose 配置解析和差异检查通过。
- [ ] **生产边界**：静态 Bearer 必须运行在受信网络或 mTLS/Service Mesh 上；真实 Kubernetes Secret 轮换、多副本滚动升级、拒绝率告警和网络层身份验收仍需独立受控演练。下一代码增量转向事务 Outbox 驱动的全量影子治理清理。

## 2026-08-03 P0 稳定性增量：事务事件驱动的全量影子治理清理

- [x] **状态与命令原子提交**：`outbox_events` 增加可空唯一 `dedup_key`；TweetService 在同一 UoW 内更新 `visible_type` 并幂等写入无正文的 `TWEET_MODERATED` Event。历史已 Shadowban 但没有治理事件的记录再次调用时也会补发一次，Outbox 写失败会回滚可见性变更。
- [x] **正确的全量游标**：Follow Repository 新增仅供内部任务使用的 `FollowerPageRepository`，按关注关系 `id DESC` 返回独立 `next_cursor`，不再把 follower user ID 误当关系游标，也不改变公开 Follow API。
- [x] **可恢复异步清理**：Canal 新增 `tweet.moderated` 路由；Timeline Consumer 使用 500 人页、Redis 页游标和 30 天完成标记，逐页调用幂等 Lua 同时移除 Timeline 项和递减未读。失败按 1/2/4 秒退避三次后进入 `queue.tweet.moderation.cleanup.dlq`，重复投递和 ACK 不确定窗口不会重复扣减未读。
- [x] **契约与观测**：`ApplyTweetModerationResponse` 追加 `cleanup_queued=3`，保留 `timelines_cleaned=2` 兼容字段；新增事件/页/扫描/移除/耗时低基数指标，Consumer 在 `2116` 暴露并由 Compose/Helm Prometheus 发现，KEDA 同时观察治理队列。
- [x] **本地验证**：治理命令、分页/恢复/重复投递、指标标签、Proto 描述符、Agent Activity、Canal 路由及受影响组合根测试/编译通过；Compose 主文件与 Helm/KEDA 模板解析通过。未启动 MySQL、Redis、RabbitMQ、Canal、模型或公网服务。
- [ ] **真实环境验收**：仍需在受控环境执行数据库回滚、Canal 重启重复投递、Redis 页间故障、三次失败入 DLQ、人工重放、多副本并发和 KEDA 扩缩容演练；完成前技术债保持 `Partial`。

## 2026-08-03 P0 稳定性增量：影子治理 DLQ 运维闭环

- [x] **默认只读检查**：新增 `cmd/timeline-moderation-dlq-replay`，默认以手动 ACK 有界读取治理 DLQ，校验 Routing Key、事件契约、消息大小与累计重放次数，生成报告后统一重新入队；检查模式不启用 Publisher Confirm，也不发布业务消息。
- [x] **显式限量重放**：执行要求 `--execute`、非空操作人、非空原因、1-100 条批量上限和 1-10 次累计上限。非法路由、毒消息、非法 Header 与超限消息继续留在 DLQ；合格消息清除消费重试计数并使用独立人工重放计数。
- [x] **Confirm 后 ACK**：使用专用 RabbitMQ Channel 进入 Publisher Confirm 模式；仅在 `twitter.events/tweet.moderated` 获得 Broker 确认后 ACK 原死信。发布失败重新入队当前及剩余消息；确认后 ACK 失败明确输出 `acknowledgement_uncertain`，不冒充重放成功。
- [x] **最小化审计报告**：JSON 仅包含消息摘要、事件键摘要、次数、固定 Outcome/Error Code 及操作人/原因摘要，不输出 Tweet/作者 ID、事件正文或审计输入原文。新增 Runbook 说明检查、执行、ACK 不确定窗口和生产验收边界。
- [x] **离线验证**：Fake 驱动测试覆盖检查不进入 Confirm、Confirm-before-read、Confirm-before-ACK、毒消息/上限/非法 Header、发布失败、ACK 不确定和报告脱敏；定向普通测试、Race、Vet 与命令编译通过。未连接 RabbitMQ、MySQL、Redis、Canal、模型或公网。
- [ ] **真实环境验收**：仍需按 `docs/timeline_moderation_dlq.md` 执行真实入 DLQ、检查、重放、Confirm/ACK 故障和多副本/KEDA 演练；完成前技术债保持 `Partial`。

## 2026-08-03 P0 稳定性增量：Consumer 分类重试与风控队列归属

- [x] **Timeline 失败转发独立化**：`cmd/consumer` 使用独立 RabbitMQ 连接承担失败发布，创建、删除与治理事件只允许进入固定 Retry/DLQ 路由；JSON 毒消息直接入 DLQ。失败路由取得 Publisher Confirm 后才 ACK 原消息，确认失败时按 1/2/4 秒有界等待后 requeue，日志仅记录路由、次数和正文长度。
- [x] **风控事件单一 Owner**：Agent Worker 直接声明并拥有 `queue.tweet.risk`，从 `twitter.events/tweet.created` 接收原始事件；Timeline Consumer 删除风控队列绑定和异步二次广播。风控重试经独立 `agent.risk.retry -> agent.risk.ingress` 返回，不会重放主事件并再次触发 Timeline。
- [x] **Temporal 去重与故障隔离**：风控使用稳定 `RiskControl-Tweet-{tweet_id}` Workflow ID、`REJECT_DUPLICATE` 和 AlreadyStarted 显式错误语义；重复消息安全 ACK。格式错误、非法 Retry Header 或重试耗尽进入 `queue.agent.risk.dlq.v1`；即使 Temporal 暂不可用，组合根也先声明队列以缓冲事件。
- [x] **Profile 内容互动一致语义**：内容互动消费者启用 Publisher Confirm，1/2/4 秒分类重试与 DLQ 均在 Confirm 后 ACK；失败发布采用有界退避 requeue，ACK 不确定和发布失败进入固定低基数指标。人工重放同时拒绝非法累计重放 Header。
- [x] **离线验证**：Timeline、Agent Consumer/Service 与两个组合根的定向测试和编译通过；覆盖 Confirm-before-ACK、毒消息、非法 Header、重试耗尽、发布失败退避、ACK 不确定、Temporal 重复以及专用拓扑。未启动 RabbitMQ、Temporal、Redis、Mongo、LM Studio 或公网服务，也未调用 DashScope。
- [ ] **真实环境验收**：按 `docs/consumer_retry_dlq.md` 演练真实 Confirm 超时/Channel 中断、三次失败入 DLQ、滚动升级重复事件和多副本恢复；风控受控检查/重放命令已由下一增量补齐，但真实 Broker 证据未完成，因此该技术债保持 `Partial`。

## 2026-08-03 P0 稳定性增量：Agent 风控 DLQ 运维闭环

- [x] **默认只检查**：新增 `cmd/agent-risk-dlq-replay`，默认通过手动 ACK 的 `basic.get` 有界读取 `queue.agent.risk.dlq.v1`；校验固定 Routing Key、1 MiB 正文上限、`TweetCreatedEvent` 最小身份契约和累计人工重放次数，输出报告后将全部消息重新入队。检查模式不启用 Publisher Confirm，也不发布业务消息。
- [x] **专用入口重放**：执行要求 `--execute`、非空操作人/原因、1-100 条批量上限和 1-10 次累计上限。合格事件只发布到 `agent.risk.ingress/tweet.created.agent-risk`，不会重放主 `twitter.events/tweet.created` 或再次触发 Timeline；自动 Retry Header 与 Broker Dead-letter Header 被清除，人工重放次数独立累计。
- [x] **Confirm 后 ACK 与重复吸收**：命令在读取消息前进入 Publisher Confirm 模式，仅在专用 Ingress 发布获确认后 ACK 原 DLQ 消息。发布失败放回当前及剩余批次；Confirm 成功但 ACK 失败输出 `acknowledgement_uncertain`。稳定 `RiskControl-Tweet-{tweet_id}` 与 Temporal `REJECT_DUPLICATE` 吸收该窗口内的重复启动。
- [x] **脱敏审计与取消语义**：报告只保存消息、Workflow 身份、操作人和原因的 SHA-256，以及固定 Outcome/Error Code；不输出 Tweet/作者 ID、推文正文或审计输入原文。Context 取消会把已读取但未处理的整批消息放回 DLQ。
- [x] **离线验证**：Fake 测试覆盖检查模式、Confirm-before-read/ACK、专用目标、Header 清理、毒消息、非法路由、超大消息、非法/超限次数、发布失败、ACK 不确定、取消回收和报告脱敏；未连接 RabbitMQ、Temporal、数据库、模型或公网。
- [ ] **真实环境验收**：仍需按 `docs/agent_risk_dlq.md` 演练真实入 DLQ、检查、限量重放、Confirm/ACK 故障、滚动升级重复和多 Agent Worker 副本；完成前 `Consumer 直接 requeue` 技术债保持 `Partial`。

## 2026-08-03 P0 稳定性增量：Timeline 恢复路由隔离与创建/删除 DLQ 闭环

- [x] **专用恢复拓扑**：新增 `timeline.ingress` 与 `timeline.retry`，创建、删除、治理使用独立 Timeline Routing Key。新失败只进入版本化 `.retry.v2`，TTL 到期只回 Timeline 主队列，不再重放 `twitter.events` 并触发风控或其他订阅者。
- [x] **无冲突滚动迁移**：不原地修改旧 Retry Queue 的 Dead-letter 参数，避免 RabbitMQ `PRECONDITION_FAILED`。旧队列不删除也不再由新路径写入，已有消息可按原拓扑自然排空；回滚时保留新持久 Exchange/Queue/Binding 即可继续交付在途消息。
- [x] **创建/删除受控重放**：新增 `cmd/timeline-event-dlq-replay`，强制选择 `created|deleted`。默认以手动 ACK 有界检查并重新入队；执行要求非空操作人/原因、1-100 条与 1-10 次上限，先幂等声明专用目标拓扑并启用 Confirm，再读取 DLQ。
- [x] **Confirm 后 ACK 与脱敏审计**：合格消息清除自动 Retry/Broker Dead-letter Header，只发布到 `timeline.ingress`；发布失败放回当前及剩余批次，ACK 失败输出 `acknowledgement_uncertain`。报告只保存消息/事件身份/审计输入摘要与固定结果码。
- [x] **治理重放一致化**：`cmd/timeline-moderation-dlq-replay` 同样在执行前声明专用拓扑，改为只发布 `timeline.ingress/tweet.moderated.timeline`，并清除 Broker Dead-letter Header。
- [x] **离线验证**：拓扑、失败路由和重放 Fake 测试覆盖专用目标、旧队列不重声明、默认检查、Topology/Confirm-before-read、Confirm-before-ACK、毒消息、次数上限、发布失败、ACK 不确定、取消回收与报告脱敏；未连接 RabbitMQ、Redis、MySQL、模型或公网。
- [x] **创建事件派生幂等**：已由下一增量补齐趋势原子投影、`sync_es` 唯一任务键和投影完成后 ACK；自动重试、Consumer 重启与 ACK 不确定窗口不再重复计分或重复入队。
- [ ] **剩余边界**：仍需按 `docs/timeline_event_dlq.md` 验收真实 Broker、旧队列排空、多副本 Claim/Lease、Lease 过期恢复及真实 ES/Qdrant 幂等成本，技术债保持 `Partial`。

## 2026-08-03 P0 稳定性增量：Timeline 创建事件幂等派生投影

- [x] **副作用完成后 ACK**：`tweet.created` 依次完成 Redis ZSet 扇出、趋势投影与 `sync_es` Outbox 入队后才 ACK。任一阶段失败进入既有分类 Retry/DLQ；普通作者批次写入和大 V 个人 Timeline 写入错误不再被日志吞掉。全阶段使用 30 秒有界 Context。
- [x] **趋势原子去重**：新增单次 Redis Lua 投影，使用 Tweet ID 的 72 小时 Marker 原子完成主题映射、作者/主题一小时限频与全局 ZSet 加分。主题先规范化、确定性排序并限制为 32 个；重复事件返回 `duplicate`，不会再次消耗限频或加分。当前键布局面向项目现有单实例 Redis，不宣称 Redis Cluster 跨 Slot 兼容。
- [x] **搜索同步唯一任务**：`outbox_tasks` 增加可空唯一 `dedup_key`，`timeline:sync_es:tweet:{id}:v1` 使用冲突忽略写入。Worker 成功后改为 `Success` 收据并保留 72 小时，每分钟最多分十批清理过期收据；毒载荷和未知类型保留为终态失败证据，不再直接删除去重依据。
- [x] **低基数观测**：Consumer 新增 `timeline_tweet_created_stage_total{stage,result}`，Stage 与 Result 均强制映射到固定枚举，覆盖 Fanout、Trend、Outbox、完成、重复、失败、Retry/DLQ 与 ACK 不确定，不使用 Tweet/User/Error 原文 Label。
- [x] **离线验证**：Consumer 完整单测、派生投影/Worker 目标 Race、Consumer/Repository/Consumer Cmd/Tweet Cmd Vet，以及 Repository、Consumer、Tweet Service 受影响包测试/编译通过。测试覆盖两次交付只产生一个 Timeline 成员、一次趋势分值和一份 Outbox，错误 Redis 类型写前失败，以及成功收据有界保留；未启动 RabbitMQ、MySQL、Redis 服务、ES、Qdrant、LM Studio 或公网。
- [x] **下一代码增量**：`outbox_tasks` 数据库原子 Claim/Lease、过期租约回收与 Worker Owner 已由下一增量完成。

## 2026-08-03 P0 稳定性增量：Outbox Worker 多副本 Claim/Lease

- [x] **兼容迁移与查询索引**：`outbox_tasks` 追加 `Processing` 状态、Lease Owner、每次领取重新生成的 Token 和 Lease Until，并为 Retry 扫描与 Lease 过期扫描建立独立复合索引；原状态值和可空唯一 `dedup_key` 保持兼容。
- [x] **MySQL 原子领取与围栏提交**：仓储在事务中使用 `FOR UPDATE SKIP LOCKED` 有界锁定可执行任务，安装 Owner/Token、递增 Attempt 并批量提交。成功或失败结果必须同时匹配 Task、Processing、Owner、Token 且 Lease 未过期；旧协程、过期协程和已被重新领取的协程只能得到 stale，不可覆盖新状态。
- [x] **过期恢复与重试语义**：每轮领取前最多恢复 100 个过期 Lease。未耗尽任务释放为 Failed 并重新进入既有指数退避，耗尽任务封存为终态失败；恢复同样采用 Skip Locked 和事务内条件更新，不与其他副本争抢同一行。
- [x] **并发、超时与收尾**：每批最多领取 10 条并立即并发执行，避免后排任务空耗 Lease；单任务执行限制 60 秒，Lease 为 90 秒，成功/失败回执使用不继承执行取消信号的独立 5 秒收尾 Context。实例 Owner 可由 `TIMELINE_OUTBOX_WORKER_ID` 指定，默认包含主机、PID 与随机后缀。
- [x] **低基数观测**：新增 `timeline_outbox_worker_operations_total{operation,result}`，只允许 Claim、Recover、Execute、Finalize、Cleanup 及固定结果枚举；记录 claimed/retryable/exhausted/stale 等计数，不把 Task、Tweet、Worker 或错误原文写入 Label。
- [x] **离线验证**：Worker Fake 测试覆盖成功收据、陈旧提交拒绝、毒任务终态、并发批次、过期恢复、有界清理和取消后的独立收尾；Repository DryRun 测试固定 Skip Locked、退避、过期扫描和 Owner/Token/Expiry 围栏 SQL。Consumer/Repository 普通测试与 Race、受影响包回归、Consumer/Tweet 组合根编译、目标 Vet 和差异检查通过；未启动 MySQL、Redis、RabbitMQ、ES、Qdrant、LM Studio 或公网服务。
- [ ] **下一代码增量**：把点赞/评论的 ACK-before-async 热度更新改为事件级幂等投影，并补齐失败分类、重复投递和 ACK 不确定窗口测试。

## 2026-08-05 Agent 面试演示阻断修复

- [x] **站内搜索进入统一 Runtime**：`platform.search` 使用受治理的 `runtime.platform_search` Profile，只开放 `hybrid_search_tweets`；上下文追问继承上一轮安全能力，显式新意图仍优先。
- [x] **证据与防幻觉门禁**：平台事实只允许来自结构化工具输出；身份、链接、时间、指标和全文缺失时必须明确不可用，不得补造。结构化 Observation 已可作为成功证据，无证据完成仍失败关闭。
- [x] **Workflow Provider 与错误交互**：新 LLM 节点和 Agent Service 装配默认使用 DashScope/qwen-plus，显式保存的 LM Studio 配置保持不变；连接拒绝、搜索不可用和运行错误改为页面内可关闭提示，不再使用阻塞式 `alert`。
- [x] **对话与能力入口一致性**：成功响应会立即补齐左侧会话摘要；空持久会话显示明确失败说明。扩展目录、市场与管理入口仅在后端探测确认可用后展示，联网搜索配置明确当前只支持 Brave Search。
- [x] **离线验证**：Web `npm run build` 通过；Agent Service 站内搜索、追问连续性、组合研究和无证据拒绝定向测试通过；Runtime 全包与 `cmd/agent-service` 装配测试通过。未调用 DashScope、LM Studio 或公网。
- [x] **运行中环境更新**：经用户确认，仅重建并重启 `frontend` 与 `agent-service`；Frontend `/agent` 返回 HTTP 200，新懒加载 Chunk 包含 DashScope 默认、空会话说明、Web Search 禁用说明和 Workflow 页面内错误提示。Agent 新镜像已运行并在 Consul 注册为 passing。
- [x] **部署构建收口**：修复 1.43 GB 以上 Docker 上下文泄漏并加入持久 Go BuildKit Cache；后续 Agent 单文件镜像重建降到 34.1 秒。原生 `alert` 已从 Workflow Chunk 移除。
- [x] **可观测启动修复**：共享 Trace Resource 不再混用旧 Schema URL；`pkg/trace` 测试与新容器启动日志确认 OTLP Tracer 初始化成功。
- [ ] **剩余运行边界**：Codex 浏览器控制受 Windows ACL 阻断，最终交互路径需用户手工点击；Temporal 服务端健康且 TCP 可达，但 Agent SDK 启动期 Dial 超时后关闭后台 Worker/趋势报告，主 Agent 演示路径不受影响。
