# Twitter Clone 全栈源码细致阅读与架构原理解析计划（21天大师版）

本计划旨在帮助您**细致、无死角地阅读并理解项目的每一行核心代码**。在接下来的 21 天中，您将按照“模块解耦、自底向上”的顺序逐步深入，从最基础的工具类和底层存储基建开始，最终彻底掌握复杂的 AI 智能体编排与 AIOps 混沌自愈体系。

---

## 📅 第一周：底层公共库、基建、以及用户和关系服务（Day 1 - Day 7）

### 🗓️ Day 1: 公共库与基础辅助工具

> 目标：熟悉项目中使用的基础库与辅助服务，理解微服务架构下请求 ID（TraceID）生成和配置自举的原理。

- **核心阅读文件**：
  - [pkg/pkg/snowflake/snowflake.go](file:///e:/GOProject/cloud/twitter-clone/pkg/pkg/snowflake/snowflake.go): 雪花 ID 生成器（与 Redis 结合纠偏，防分布式并发冲突）。
  - [pkg/config/dynamic_config.go](file:///e:/GOProject/cloud/twitter-clone/pkg/config/dynamic_config.go): 基于 Consul 和 `atomic.Pointer` 实现的线程安全、零锁动态参数热重载。
  - [pkg/logger/logger.go](file:///e:/GOProject/cloud/twitter-clone/pkg/logger/logger.go): 基于 Zap 封装的结构化日志（包含 TraceID 自动绑定）。
  - [pkg/trace/trace.go](file:///e:/GOProject/cloud/twitter-clone/pkg/trace/trace.go): OpenTelemetry 基础追踪器初始化与 Jaeger 连接。
- **重点思考**：如何利用 `atomic.Pointer` 实现线程安全的动态配置替换？TraceID 是如何在最底层被绑定到日志上下文中的？

---

### 🗓️ Day 2: 存储基建与持久化框架封装

> 目标：理解微服务是如何建立与 MySQL、Redis、MongoDB 及 RabbitMQ 的底层连接池及超时设置的。

- **核心阅读文件**：
  - [internal/infrastructure/persistence/mysql.go](file:///e:/GOProject/cloud/twitter-clone/internal/infrastructure/persistence/mysql.go): GORM 初始化与数据库连接池细粒度参数（`MaxIdleConns`, `MaxOpenConns`）。
  - [internal/infrastructure/cache/redis.go](file:///e:/GOProject/cloud/twitter-clone/internal/infrastructure/cache/redis.go): Go-Redis 客户端包装、Pub/Sub 事件监听热更（解决脑裂的自举流程）。
  - [internal/infrastructure/mq/rabbitmq.go](file:///e:/GOProject/cloud/twitter-clone/internal/infrastructure/mq/rabbitmq.go): RabbitMQ 信道复用、发布确认模式（Publisher Confirms）、Exchange/Queue 动态参数声明方法。
- **重点思考**：在 `rabbitmq.go` 中，如何安全地进行断线重连并恢复未确认的消息？

---

### 🗓️ Day 3: 工作单元模式与领域契约实体

> 目标：理清项目中的数据模型划分以及领域事件的结构契约。

- **核心阅读文件**：
  - [internal/pkg/database/uow/uow.go](file:///e:/GOProject/cloud/twitter-clone/internal/pkg/database/uow/uow.go): 工作单元模式（Unit of Work）的具体抽象，利用本地事务生命周期保证跨 Repo 操作的原子性。
  - [internal/domain/](file:///e:/GOProject/cloud/twitter-clone/internal/domain) 目录：
    - `user.go` / `tweet.go` / `follow.go` / `outbox.go`: 分别查看各个领域实体的表结构定义与 GORM 映射。
  - [internal/events/tweet_events.go](file:///e:/GOProject/cloud/twitter-clone/internal/events/tweet_events.go): 定义推文发布、点赞、评论、关注等各种事件的 JSON 序列化结构体契约。
- **重点思考**：为什么微服务中要提倡 Unit of Work 模式？它是如何将 GORM 事务上下文包装进 Go 的 `context.Context` 中透传的？

---

### 🗓️ Day 4: User Service 与鉴权安全隔离

> 目标：精读用户微服务的注册、登录、JWT 证书签名以及富媒体对象存储逻辑。

- **核心阅读文件**：
  - [cmd/user-service/main.go](file:///e:/GOProject/cloud/twitter-clone/cmd/user-service/main.go): 用户服务启动、自动迁移与 Consul 注册。
  - [internal/module/user/service/user_service.go](file:///e:/GOProject/cloud/twitter-clone/internal/module/user/service/user_service.go): 密码 Argon2 哈希存储与用户信息获取。
  - [internal/module/auth/service/jwt.go](file:///e:/GOProject/cloud/twitter-clone/internal/module/auth/service/jwt.go): JWT Token 签名、多维角色属性编码。
  - [internal/gateway/handler/upload_handler.go](file:///e:/GOProject/cloud/twitter-clone/internal/gateway/handler/upload_handler.go): 对象存储（MinIO）流式上传文件、跨容器 Endpoint 网络映射转换。
- **重点思考**：网关在处理用户上传文件时，如何适配外网访问 URL 与容器内上传 Endpoint 的差异？

---

### 🗓️ Day 5: Follow Service 社交图谱关系与大V阈值治理

> 目标：精读关注微服务，掌握社交网络关注关系维护和大V动态变更算法。

- **核心阅读文件**：
  - [cmd/follow-service/main.go](file:///e:/GOProject/cloud/twitter-clone/cmd/follow-service/main.go): 关注服务启动引导。
  - [internal/module/follow/service/follow_service_mq.go](file:///e:/GOProject/cloud/twitter-clone/internal/module/follow/service/follow_service_mq.go): 核心关注/取关逻辑、大V状态双阈值判定及晋升/降级异步广播逻辑。
- **重点思考**：大V粉丝数越过阈值（如从 4999 变为 5000）时，代码中是如何异步广播清除/更新受影响用户的 Redis 大V缓存的？

---

### 🗓️ Day 6: Tweet Service 推文极速发布与事务级 Outbox 创建

> 目标：掌握发推、评论、投票与发件箱事务级保存。

- **核心阅读文件**：
  - [cmd/tweet-service/main.go](file:///e:/GOProject/cloud/twitter-clone/cmd/tweet-service/main.go): 包含点赞、转发、书签、推文、投票等多个仓储的初始化与注册。
  - [internal/module/tweet/service/tweet_service_mq.go](file:///e:/GOProject/cloud/twitter-clone/internal/module/tweet/service/tweet_service_mq.go): `CreateTweet` 核心实现（将推文保存、投票创建、发件箱事件注册锁在同一个 `uow.Do` 事务内）。
- **重点思考**：在 `CreateTweet` 中，为什么还要写一份 `OutboxEvent` 事件到 DB，而不是直接通过 GORM 存储推文？

---

### 🗓️ Day 7: 第一周阶段回顾与联调调试

- **阅读内容**：第一周所读源码的回顾与交叉引用。
- **动手实践**：本地拉起 Consul、MySQL、Redis，单独运行并测试 `user-service` 和 `follow-service`，确保 gRPC 调用打通，查看 Consul 注册控制台。

---

## 📅 第二周：高并发 Feed 流缓存、消息队列消费与 CDC 事件流（Day 8 - Day 14）

### 🗓️ Day 8: BFF API Gateway 路由设计与熔断规则热更

> 目标：理解网关层职责，学习基于 Sentinel 的限流熔断防护。

- **核心阅读文件**：
  - [cmd/gateway/main.go](file:///e:/GOProject/cloud/twitter-clone/cmd/gateway/main.go): BFF（网关服务）启动逻辑，Sentinel 熔断参数初始化。
  - [internal/gateway/router/router.go](file:///e:/GOProject/cloud/twitter-clone/internal/gateway/router/router.go): Gin 路由注册、熔断限流防护策略设置。
  - [internal/gateway/middleware/auth.go](file:///e:/GOProject/cloud/twitter-clone/internal/gateway/middleware/auth.go): 请求鉴权中间件。
- **重点思考**：当触发 Sentinel 熔断时，网关会对客户端返回什么响应？熔断规则是如何从动态配置中热加载生效的？

---

### 🗓️ Day 9: 百万并发 Feed 流多级缓存引擎与 Singleflight

> 目标：深入高并发时间线 Feed 流设计，学习多级缓存与防击穿。

- **核心阅读文件**：
  - [tweet_service_mq.go](file:///e:/GOProject/cloud/twitter-clone/internal/module/tweet/service/tweet_service_mq.go) 里的 `GetFeeds` 和 `GetBaseTweetsWithCache`。
  - [internal/module/tweet/cache/timeline_cache.go](file:///e:/GOProject/cloud/twitter-clone/internal/module/tweet/cache/timeline_cache.go): 二级 Redis 时间线 ZSet 读写操作，大V专属 ZSet 重建逻辑。
- **核心概念**：
  1. 一级进程缓存（BigCache）零 GC STW 压力。
  2. Singleflight 拦截瞬间高并发回源（利用 `DoChan` + `select` 超时退出防止假死）。
  3. Redis 缓存防雪崩的随机 TTL。
  4. 翻页预拉取（Cursor Pre-warming）异步预热下一页数据。

---

### 🗓️ Day 10: Canal CDC 事务发件箱事件中继（MySQL CDC）

> 目标：深入 CDC（数据变更捕获）架构，理解如何无感从中继器推送事件到 MQ。

- **核心阅读文件**：
  - [cmd/canal-relay/main.go](file:///e:/GOProject/cloud/twitter-clone/cmd/canal-relay/main.go): Canal 进程启动、Redis 位点保存管理器初始化、Outbox 清理 GC 定时任务启动。
  - [internal/infrastructure/canal/canal.go](file:///e:/GOProject/cloud/twitter-clone/internal/infrastructure/canal/canal.go): 继承 Canal EventHandler 接口，专门监听 `outbox_events` 表的 DDL/DML，将 Payload 解析并利用 RabbitMQ 连接池投递。
- **重点思考**：如何设计 Redis Position Store 记录解析到的 binlog 偏移量？如果在中继投递 MQ 时崩溃，重启后如何保证不丢消息？

---

### 🗓️ Day 11: Timeline Consumer 核心事件消费与写扩散

> 目标：理解消费者服务是如何通过写扩散（Fan-out）维护用户 Timeline 的。

- **核心阅读文件**：
  - [timeline_consumer.go](file:///e:/GOProject/cloud/twitter-clone/internal/mq/consumer/timeline_consumer.go): `NewTimelineConsumer` 队列/绑定关系声明与 `consumeFanout` 推文扇出消费。
  - [internal/mq/consumer/hashtag_batcher.go](file:///e:/GOProject/cloud/twitter-clone/internal/mq/consumer/hashtag_batcher.go): Hashtag 批量限流更新，减少 Redis 频繁读写 CPU 压力。
  - [internal/mq/consumer/trends_processor.go](file:///e:/GOProject/cloud/twitter-clone/internal/mq/consumer/trends_processor.go): 智能分词提取趋势词热度。
- **重点思考**：在 `fanoutToFollowers` 中，对大V发帖是如何处理的？如何通过 `trends_decay` 进行时间窗口的话题评分衰减？

---

### 🗓️ Day 12: MQ 队列指数退避重试与死信隔离

> 目标：吃透大厂级 MQ 可靠性重试与数据隔离治理。

- **核心阅读文件**：
  - [timeline_consumer.go](file:///e:/GOProject/cloud/twitter-clone/internal/mq/consumer/timeline_consumer.go) 里的 `handleFailure` 方法。
- **核心概念**：
  1. `retry.events` 交换机配对不同重试 TTL 的原理。
  2. 利用 RabbitMQ 队列死信转移（x-dead-letter-exchange）回到主消费链路。
  3. 最大重试次数上限归档至死信队列（DLQ），避免无限 requeue 风暴。

---

### 🗓️ Day 13: WebSocket 实时连接池与通知推送

> 目标：学习高并发 WebSocket 维持和基于 Redis Pub/Sub 的消息分发。

- **核心阅读文件**：
  - [websocket_handler.go](file:///e:/GOProject/cloud/twitter-clone/internal/gateway/handler/websocket_handler.go): 处理网关 WebSocket 连接、基于 Redis 管道推送广播。
  - [internal/module/messenger/service/chat.go](file:///e:/GOProject/cloud/twitter-clone/internal/module/messenger/service/chat.go): 私信会话与消息 CRUD。
  - [notification/worker/consumer.go](file:///e:/GOProject/cloud/twitter-clone/internal/module/notification/worker/consumer.go): 监听 `tweet.liked` 等社交交互事件，写入 DB 并分发到 Redis 实时通知信道。
- **重点思考**：当网关采用多实例水平部署时，A 实例下的用户如何接收到 B 实例推送的实时通知？（Redis Pub/Sub 订阅桥接原理）

---

### 🗓️ Day 14: 第二周阶段回顾与消息流整体联调

- **阅读内容**：Canal Relay -> RabbitMQ -> Timeline Consumer -> Redis / Elasticsearch / Qdrant 数据双写链条复盘。
- **动手实践**：在本地发帖，然后查看 Canal Relay 控制台日志、RabbitMQ 后台面板（`localhost:15672`），验证消息流是否正常运转。

---

## 📅 第三周：AI Agent 协同、RAG 向量检索、AIOps 自愈与 Flutter App（Day 15 - Day 21）

### 🗓️ Day 15: Agent Service 智能体基础与 MCP 越权拦截

> 目标：阅读 AI Agent 服务，学习 MCP（Model Context Protocol）工具绑定及鉴权。

- **核心阅读文件**：
  - [cmd/agent-service/main.go](file:///e:/GOProject/cloud/twitter-clone/cmd/agent-service/main.go): 初始化 DashScope 大模型客户端、LM Studio 本地向量化服务器以及连接 Temporal 状态机。
  - [internal/module/agent/service/agent_service.go](file:///e:/GOProject/cloud/twitter-clone/internal/module/agent/service/agent_service.go): 流式聊天会话交互、SSE 长连接管理。
  - `internal/module/agent/mcp/tools/` 目录：
    - [create_tweet.go](file:///e:/GOProject/cloud/twitter-clone/internal/module/agent/mcp/tools/create_tweet.go): 智能体写推文工具。
    - [search_tweets.go](file:///e:/GOProject/cloud/twitter-clone/internal/module/agent/mcp/tools/search_tweets.go): 智能体推文搜索工具。
- **重点思考**：如何实现越权隔离？（拦截器通过 gRPC Context 提取真正的 Token 并重新绑定 `user_id` 的过程）

---

### 🗓️ Day 16: 语义 RAG 混合检索、并发双路召回与 Rerank 降级

> 目标：掌握向量检索与关键词检索的召回以及基于大模型的重排序降级机制。

- **核心阅读文件**：
  - [pkg/qdrant/qdrant.go](file:///e:/GOProject/cloud/twitter-clone/pkg/qdrant/qdrant.go): Qdrant 向量计算、数据点的 Upsert / Search 实现。
  - [pkg/ai/reranker.go](file:///e:/GOProject/cloud/twitter-clone/pkg/ai/reranker.go): 百炼、硅基流动重排客户端工厂，包含基于 Bigram Jaccard 相似度计算的本地 Mock Rerank 降级。
  - [agent_service.go](file:///e:/GOProject/cloud/twitter-clone/internal/module/agent/service/agent_service.go) 里的 `SearchTweetsBySemantic`。
- **重点思考**：双路召回（ES BM25 关键词 + Qdrant 向量相似度）是如何使用 `errgroup` 进行并发调用并统一合并去重的？

---

### 🗓️ Day 17: Temporal Saga 风控自愈与舆情播报工作流

> 目标：掌握基于 Temporal 的分布式 Saga 编排，理解防破产与 Continue-As-New 设计。

- **核心阅读文件**：
  - [workflows.go](file:///e:/GOProject/cloud/twitter-clone/internal/module/agent/service/workflows.go):
    - `TweetRiskControlWorkflow`（风控影子封禁工作流，包含限频、相似度比对、Lua原子洗地）。
    - `TrendingReporterWorkflow`（周期性舆情哨兵工作流）。
  - [activities.go](file:///e:/GOProject/cloud/twitter-clone/internal/module/agent/service/activities.go): 工作流的具体活动步骤实现。
- **核心概念**：
  1. `aiRetryPolicy` 重试次数上限设为 3 限制，防止 AI API 接口抖动刷爆账单（核心防线）。
  2. `infraRetryPolicy` 对基础设施无限自动退避。
  3. `Continue-As-New` 自动刷新事件历史，保证状态机不因内存占用过高崩溃。

---

### 🗓️ Day 18: Pyroscope 性能分析与智能热调优 Agent

> 目标：理解智能性能调优的实现，掌握从 Pyroscope 火焰图到 AI 决策的链路。

- **核心阅读文件**：
  - [profiling_analyzer.go](file:///e:/GOProject/cloud/twitter-clone/internal/module/agent/service/profiling_analyzer.go): 发起对本地 Pyroscope 服务端Collapsed数据抓取、分词统计提取排名前 5 的热点调用栈（去噪）。
  - [agent_service.go](file:///e:/GOProject/cloud/twitter-clone/internal/module/agent/service/agent_service.go) 里的 `TuneCacheConfig` 工具。
  - [dynamic_config.go](file:///e:/GOProject/cloud/twitter-clone/pkg/config/dynamic_config.go): 热更新 TTL 参数。
- **重点思考**：大模型是如何判断 CPU 瓶颈并输出格式化调优自愈配置指令的？缓存调优冷却锁的设计是如何避免参数震荡的？

---

### 🗓️ Day 19: RCA RCA告警环路诊断与网关限流自愈

> 目标：精读Healer逻辑，吃透 AIOps 混沌防御与网关动态 Sentinel 重载。

- **核心阅读文件**：
  - [self_healer.go](file:///e:/GOProject/cloud/twitter-clone/internal/gateway/handler/self_healer.go): 网关接收告警回调、解析 AI 诊断输出并触发 Sentinel 限流和熔断规则热加载的执行引擎。
  - [blackbox_logger.go](file:///e:/GOProject/cloud/twitter-clone/internal/gateway/handler/blackbox_logger.go): 线程安全的 RingBuffer 拦截器，缓存最近 20 条 API 失败日志供智能体诊断。
- **重点思考**：如何利用 `Allowlist` 允许列表限制 AI 的权限，防止因为大模型幻觉或者恶意注入导致 Healer 下发指令摧毁整个集群的防护规则？

---

### 🗓️ Day 20: Flutter 移动端网络架构与全局状态管理

> 目标：了解前端移动端架构与移动设备异构网络的解决思路。

- **核心阅读文件**：
  - [mobile/lib/main.dart](file:///e:/GOProject/cloud/twitter-clone/mobile/lib/main.dart): Flutter 启动与环境配置初始化。
  - [mobile/lib/core/network/dio_client.dart](file:///e:/GOProject/cloud/twitter-clone/mobile/lib/core/network/dio_client.dart) (或者 mobile 网络库): 统一 Dio 网络拦截器、自动携带 Token 并重试。
  - `mobile/lib/features/` 下的 feature 主逻辑：
    - 查看 `auth/` / `tweet/` / `agent/` 的路由和页面绑定逻辑。
- **重点思考**：Flutter 移动端在测试时是如何利用 `adb reverse` 解决宿主机与真机/模拟器之间的 localhost 端口映射难题的？

---

### 🗓️ Day 21: 终极总结与全系统对账测试

- **实践内容**：
  1. 运行根目录下的混沌测试自动化脚本，触发 Redis 延迟故障。
  2. 观察 API 网关环形缓冲区记录的 error、Prometheus 指标变化。
  3. 查看 Pyroscope 中产生的火焰图，跟踪 TraceID 全链路日志。
  4. 观察 Agent Healer 诊断并成功下发限流阈值策略，最终自动解除告警。
- **目标**：对整个系统建立坚实、不可磨灭的全局掌控能力，在大师的视角下迎接任何答辩提问。
