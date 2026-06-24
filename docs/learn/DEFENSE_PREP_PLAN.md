# Twitter Clone 全栈源码深度阅读与毕业/项目答辩冲刺计划

本计划专为**毕业答辩/技术分享/架构评审**定制，覆盖整个 Monorepo 仓库的所有微服务、前端移动端、AI 智能体网格以及云原生可观测性自愈系统。旨在帮助您在极短时间内对项目建立全景式的认识，并掌握答辩时面对评委提问的核心术语和回答话术。

---

## 🗺️ 第一天：系统整体架构、网关接入与核心社交基础（Gateway & User, Follow, Tweet）
> **目标**：理解从用户 HTTP 请求网关开始，到登录注册、关注关系、发布推文的完整同步链路与数据流向。

### 1. 核心源码阅读指南
- **统一网关入口（Gin HTTP BFF 网关）**：
  - [gateway/main.go](file:///e:/GOProject/cloud/twitter-clone/cmd/gateway/main.go): 查看网关初始化，包含动态限流、Sentinel 熔断防卫、Snowflake ID 生成器初始化，以及下游 gRPC 客户端长连接预热（Eager Connection）。
  - [router/router.go](file:///e:/GOProject/cloud/twitter-clone/internal/gateway/router/router.go): 网关路由注册、JWT 统一鉴权隔离中间件。
- **User Service（用户服务）**：
  - [user-service/main.go](file:///e:/GOProject/cloud/twitter-clone/cmd/user-service/main.go): 数据库自动迁移，Consul 服务注册。
  - [user_service.go](file:///e:/GOProject/cloud/twitter-clone/internal/module/user/service/user_service.go): 用户注册与登录逻辑，雪花 ID 精度纠偏。
- **Follow Service（关注服务）**：
  - [follow-service/main.go](file:///e:/GOProject/cloud/twitter-clone/cmd/follow-service/main.go): 关注业务启动流程。
  - [follow_service_mq.go](file:///e:/GOProject/cloud/twitter-clone/internal/module/follow/service/follow_service_mq.go): 核心关注/取关逻辑、大V（Celebrity）状态双阈值防抖升级/降级判定。
- **Tweet Service（推文服务）**：
  - [tweet-service/main.go](file:///e:/GOProject/cloud/twitter-clone/cmd/tweet-service/main.go): 包含推文、点赞、二级评论、投票、书签等核心实体的自动迁移。
  - [tweet_service_mq.go](file:///e:/GOProject/cloud/twitter-clone/internal/module/tweet/service/tweet_service_mq.go): `CreateTweet` 核心逻辑（本地事务与 Outbox 记录）。

### 2. 答辩核心知识点与高频提问
- **Q：为什么要在 gRPC 客户端启动时做长连接预热（Eager Connection）？**
  - **A**：gRPC 基于 HTTP/2 协议，默认在首次调用时才建立物理连接（Lazy Dial）。若在高并发瞬间涌入，首次解析 DNS、TCP 握手和 HTTP/2 握手会带来明显的请求时延（Cold Start），甚至触发网关超时。我们在网关和 Auth 服务启动时通过 `conn.Connect()` 强制拉起底层连接，消除了这笔耗时。
- **Q：关注系统的双阈值防抖（Hysteresis Band）机制是如何工作的？**
  - **A**：设定了非对称的阈值判定：大V晋升线为 5000 粉丝，降级线为 4500 粉丝。当粉丝数在 5000 边缘上下波动时（例如频繁取关/重新关注），该带宽（500 粉丝的缓冲区）可以有效防止用户在“普通博主”和“大V”两种模式状态下频繁抖动切换，避免高频更新全局 Redis 缓存和广播。

---

## 📅 第二天：高并发 Feed 流、实时通知与即时通讯（WS 聊天、双域 ZSet 合并）
> **目标**：理清系统是如何支撑高并发 Feed 流读写、利用 WebSocket 实现即时聊天与实时交互通知的。

### 1. 核心源码阅读指南
- **混合 Feed 流（Hybrid Push/Pull）**：
  - [tweet_service_mq.go](file:///e:/GOProject/cloud/twitter-clone/internal/module/tweet/service/tweet_service_mq.go) 里的 `GetFeeds` 和 `getCelebrityTimelineWithRebuild` 方法。
  - 重点阅读其多级缓存设计：L1 本地内存缓存（BigCache） + L2 二级缓存（Redis ZSet，带防雪崩随机 TTL），以及高并发防穿透的 Singleflight 归并机制。
- **消息消费与异步写扩散（Timeline Consumer）**：
  - [timeline_consumer.go](file:///e:/GOProject/cloud/twitter-clone/internal/mq/consumer/timeline_consumer.go): 重点查看 `fanoutToFollowers`（大V旁路拉模式，普通博主写扩散推模式）。
- **WebSocket 实时推送与单聊（Messenger & Notification）**：
  - [websocket_handler.go](file:///e:/GOProject/cloud/twitter-clone/internal/gateway/handler/websocket_handler.go): Websocket 连接池与 Redis Pub/Sub 广播实现。
  - [messenger-service/main.go](file:///e:/GOProject/cloud/twitter-clone/cmd/messenger-service/main.go): 私信系统（MongoDB 离线消息拉取）。
  - [notification/worker/consumer.go](file:///e:/GOProject/cloud/twitter-clone/internal/module/notification/worker/consumer.go): 消费 RabbitMQ 点赞、评论等事件，通过 Redis Pub/Sub 广播到网关 WebSocket。

### 2. 答辩核心知识点与高频提问
- **Q：介绍一下你们的 Feed 流架构（Push/Pull 混合模式）？**
  - **A**：采用推拉结合模型。对于粉丝少于 5000 的普通用户，发推时使用 **Push（写扩散）** 异步将推文 ID 写入粉丝的 Redis Timeline ZSet；对于超过 5000 粉丝的大V，发推时**不写扩散**，而是仅写入其个人 Inbox 缓存。用户拉取 Feed 流时，使用 Redis Pipeline 批量并发读取普通 Timeline ZSet 以及所关注大V的缓存进行**内存 Merge Sort** 分页截取。这解决了纯 Push 模式下“名人发布推文瞬间带来百万次并发写”的写扩散雪崩瓶颈。
- **Q：二级缓存是如何保证数据一致性与抗击穿的？**
  - **A**：使用 `singleflight.Group` 拦截高并发回源请求，保证对同一个 Key 的瞬间热点查询在进程内仅有一个协程前往二级 Redis 缓存或 MySQL 数据库，其余协程挂起等待。当数据发生修改/物理删除时，由推文服务和消费端通过 Redis Pub/Sub 广播清除各网关节点的 L1 本地缓存（BigCache），确保多副本最终一致性。

---

## 📅 第三天：事务一致性保障与异步搜索同步（Outbox & MQ 韧性治理）
> **目标**：吃透分布式系统下的事务可靠性，理解项目中的最终一致性保障及 MQ 治理设计。

### 1. 核心源码阅读指南
- **Canal CDC Outbox 事件中继器**：
  - [cmd/canal-relay/main.go](file:///e:/GOProject/cloud/twitter-clone/cmd/canal-relay/main.go): Canal 服务主进程，使用 Redis Position Store 持久化 binlog 位点。
  - [canal.go](file:///e:/GOProject/cloud/twitter-clone/internal/infrastructure/canal/canal.go): Canal `EventHandler` 实现。监听 `outbox_events` 表的 binlog 变化，将 Payload 转换后精准投递至 RabbitMQ 的 Exchange。
- **消费失败指数退避与死信队列（MQ 健壮性）**：
  - [timeline_consumer.go](file:///e:/GOProject/cloud/twitter-clone/internal/mq/consumer/timeline_consumer.go) 里的 `handleFailure` 方法。
  - 重点阅读：业务消费端发生故障时，如何声明 `retry.events.exchange` 并设置 Expiration TTL，并在最大 3 次失败后发布到死信队列（DLQ），坚决不用 `msg.Nack(false, true)` 进行死循环重试。
- **ES 异步向量化双写发件箱**：
  - [timeline_consumer.go](file:///e:/GOProject/cloud/twitter-clone/internal/mq/consumer/timeline_consumer.go) 里的 `StartOutboxWorker` 和 `executeESIndex`。

### 2. 答辩核心知识点与高频提问
- **Q：什么是事务发件箱模式（Transactional Outbox Pattern）？为什么这么设计？**
  - **A**：若在数据库事务内同步发送网络 MQ 消息，一旦网络抖动会导致长事务挂起，打满 DB 连接池；若先提交事务再发送消息，一旦发送失败会导致数据与事件不一致。我们采用发件箱模式：将“发推”与“Outbox 事件记录”放在同一个 MySQL 本地事务内保存。由外部独立 CDC 中继服务（Canal）解析 binlog，捕获到 `outbox_events` 新增记录后可靠地将其投递到 RabbitMQ，从而保证了数据库变更与消息发送的**原子性与最终一致性**。
- **Q：如何防御 MQ 的“重试风暴”？**
  - **A**：传统的 `Nack(requeue=true)` 会把消息放回队列头部并被消费者立刻重新拉取，在下游服务宕机时产生死循环风暴。我们设计了**基于死信 TTL + Exchange 的指数退避重试**：消费失败后将消息确认（Ack），同时附带递增的重试次数头（Header）发布至重试 Exchange，由重试队列持有一段过期时间（TTL，如 1s, 2s, 4s...），过期后消息通过死信路由自动退回到正常业务队列中重新消费。超过 3 次重试仍失败的消息则被分流至死信队列（DLQ），避免阻塞主通道。

---

## 📅 第四天：AI Agent、多 Agent 协作与语义 RAG（Agent, MCP, Vector DB）
> **目标**：熟悉项目中智能体的设计模式、向量库接入以及基于流程控制的高弹性多智能体交互架构。

### 1. 核心源码阅读指南
- **Agent gRPC 服务与多模式交互**：
  - [agent_service.go](file:///e:/GOProject/cloud/twitter-clone/internal/module/agent/service/agent_service.go): 提供直接对话、RAG 混合语义检索、智能发帖以及多 Agent 协作写贴等 4 种交互模式。
- **MCP Server & Tools 鉴权隔离**：
  - [tools](file:///e:/GOProject/cloud/twitter-clone/internal/module/agent/mcp/tools): 定义智能体可以调用的底层工具（包含发推、搜索、批量获取）。
  - 查看 MCP 工具的鉴权防越权安全拦截器（废除大模型自动填充 `user_id` 的漏洞，改为从认证 Context 中提取 JWT 强行注入）。
- **RAG 双路召回与 Rerank 降级**：
  - [reranker.go](file:///e:/GOProject/cloud/twitter-clone/pkg/ai/reranker.go): 百炼/硅基流动重排工厂类，支持基于 Jaccard 相似度的本地 Mock 重排作为 1.5s 时延断路降级。
- **Temporal Saga 编排状态机**：
  - [workflows.go](file:///e:/GOProject/cloud/twitter-clone/internal/module/agent/service/workflows.go) 与 [activities.go](file:///e:/GOProject/cloud/twitter-clone/internal/module/agent/service/activities.go): `TweetRiskControlWorkflow`（风控影子封禁工作流，包含大模型限频、Qdrant 向量查重、Lua 脚本洗地）与 `TrendingReporterWorkflow`（舆情哨兵自动发帖，包含 `Continue-As-New` 定时清理历史）。

### 2. 答辩核心知识点与高频提问
- **Q：多 Agent 协作写推文（模式四）中，如果某个 Agent 超时挂起，系统会如何应对？**
  - **A**：为防止并发网络 IO 协程泄漏，整个多智能体协作流（Search -> Style -> Reviewer）被包裹在带 3s 强制超时的 `context.WithTimeout` 中。我们使用 `errgroup` 对不互相依赖的数据获取步骤进行高并发并行调用，并将数据生成放入无锁内存隔离的切片中防止数据竞争。一旦 Reviewer 或 Style 步骤发生网络超时或 429 报错，系统会捕获错误并触发 Lock-free 降级，平滑返回前几个步骤已生成的安全草稿，保证主链路不发生假死挂起。
- **Q：什么是 Temporal 的 Continue-As-New 机制？为什么要引入它？**
  - **A**：在我们的周期性舆情监控哨兵（TrendingReporter）中，工作流会无限循环地监控热度并自动总结发帖。如果一直循环下去，Temporal 数据库内记录的该 Workflow 执行历史事件（History Events）会无限堆积，一旦超过 50,000 条上限，Workflow 就会因占用内存过高崩溃。我们在循环每完成一轮时调用 `workflow.NewContinueAsNewError(...)`，在清空当前事件历史的同时自举启动一个同参数的新 Workflow 实例，保持系统无限期稳定运转。

---

## 📅 第五天：混沌测试、可观测性与 AIOps 自愈（OTel, PLG, Pyroscope, Self-Healer）
> **目标**：展示项目在大厂级高可用、AIOps 混沌防御与自愈方面的深度实践，这是体现技术壁垒的关键。

### 1. 核心源码阅读指南
- **全链路 Trace 与跨协程上下文透传**：
  - [follow_service_mq.go:pullRecentTweetsToTimeline](file:///e:/GOProject/cloud/twitter-clone/internal/module/follow/service/follow_service_mq.go#L96-L119): 在异步启动的 Goroutine 中透传 OTEL trace Context 的具体用法（使用 `context.WithoutCancel` 防幽灵断裂）。
- **Pyroscope 持续 Profiling 火焰图解析**：
  - [profiling_analyzer.go](file:///e:/GOProject/cloud/twitter-clone/internal/module/agent/service/profiling_analyzer.go): 动态获取 Pyroscope 的火焰图性能堆栈坍塌文本，提取 Top 5 热点 CPU 方法给大模型作为智能诊断 RCA 上下文。
- **AIOps 智能 RCA 诊断与网关限流熔断自愈**：
  - [self_healer.go](file:///e:/GOProject/cloud/twitter-clone/internal/gateway/handler/self_healer.go): 网关侧动态熔断规则重载、Allowlist 安全名单防御及 Sentinel 规则合并逻辑。
  - [dynamic_config.go](file:///e:/GOProject/cloud/twitter-clone/pkg/config/dynamic_config.go): 使用 `atomic.Pointer` 结构体指针实现线程安全的零锁动态参数热重载（防脑裂）。

### 2. 答辩核心知识点与高频提问
- **Q：你们的可观测性系统是如何实现的？怎么实现跨异步链路的 TraceID 穿透？**
  - **A**：我们接入了标准的 OpenTelemetry 体系，通过 gRPC Unary/Stream 拦截器、HTTP Gin 中间件自动注入并向后透传 TraceID。当发起异步协程时，使用 `trace.ContextWithSpan(context.Background(), span)` 创建一个带有主调用链上下文的 async Context 以延续 Trace 追踪，保证在 Grafana + Jaeger 中查看拓扑时，异步任务和后台消费链条不会形成“可观测性盲区”。
- **Q：介绍一下你们的 AIOps 智能诊断与自愈闭环是如何对抗混沌网络延时的？**
  - **A**：我们在网关集成了 Pyroscope Pyro-SDK，并部署了 Chaos Mesh 在 Kubernetes 内部注入混沌时延（例如对 Redis 注入 5s 网络延迟导致网关疯狂报错）。此时网关限流告警 Webhook 触发并向 `agent-service` 发起诊断请求。智能分析器（`ProfilingAnalyzer`）拉取 Pyroscope 此时的 CPU 火焰图堆栈与黑匣子日志，通过大模型进行 RCA（根因分析），自动识别出是 Redis 连接超时，输出报告的同时返回包含 `VirtualService` 动态切流规则和 Sentinel 限流降级规则的结构化 JSON。网关 Healer 验证 Allowlist 安全后动态加载规则生效，实现了**“故障触发 -> 持续监控 -> 智能 RCA 诊断 -> 决策输出 -> 自动 Healer 热加载生效”**的智能自愈环路。

---

## 💡 答辩通用抢答与防御话术

1. **“该项目并不是简单的 CRUD，而是涵盖了微服务治理的完整生命周期。”**
   - *话术套路*：在介绍推文发布时，千万不要只提 GORM 的 Insert。要主动把话题引向“Transactional Outbox + Canal 异步双写 ES/Qdrant + RabbitMQ 指数退避及死信隔离”。这能体现您有处理分布式事务和最终一致性难题的生产级思维。
2. **“我们在设计 AI Agent 时，不仅考虑了‘对话’，更注重了‘工程落地与安全性’。”**
   - *话术套路*：提到智能体时，重点讲“MCP Server 连接池保活心跳”和“Tool 拦截器强制上下文 JWT 注入防 LLM 越权”。告诉评委，市面上的 Demo 容易被 Prompt 注入，而我们实现了严格的权限隔离。
3. **“高并发下，我们奉行‘多级缓存解决大部分流量，Singleflight 拦截瞬间击穿’的治理策略。”**
   - *话术套路*：讲到 Feed 流和 Trending 趋势榜，重点突出“BigCache 零 GC 进程缓存、ZSet 指数时间衰减算法及 HashtagBatcher 本地并发缓冲”。这说明您不仅知道缓存，还对 GC STW、Redis 单点热 Key 冲突有深度的优化实践。
