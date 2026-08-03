# Project Overview（项目定位与运行拓扑）

> 最近核对：2026-07-15
> 导航入口：`.agents/context/project_map.md`

## 1. 项目定位

这是一个 Go Monorepo，包含两条相互连接但边界不同的产品主线：

1. Twitter 风格社交平台：用户、推文、关注、私信、通知、Timeline、搜索与媒体上传。
2. Agent Runtime 与社交内容智能工作流平台：多模式 AI 助手、MCP 工具、RAG/Memory、可视化 DAG 工作流和可选 Temporal 后台任务。

面试定位可表述为：

> 基于 Go 的可治理 Agent Runtime 与高并发社交内容智能工作流平台。

不能把项目描述成“所有链路均生产就绪”：Agent Runtime 正在按 P0-P7 增量强化，Timeline、服务边界、审批、可观测性和多租户配额仍有明确技术债。

## 2. 技术栈事实

| 层 | 当前实现 |
|----|----------|
| Backend | Go 1.25.5、gRPC、Gin/HTTP、WebSocket |
| Web | Vue 3、TypeScript、Vite、Pinia、Vue Flow |
| Mobile | Flutter、Riverpod、Dio、go_router |
| Primary DB | MySQL + GORM（社交领域、Messenger、Persona） |
| Agent DB | MongoDB（Dialogue、Message、Workflow、Run、Summary Lease） |
| Cache/Realtime | Redis（缓存、Timeline ZSet、Pub/Sub、幂等） |
| Async | RabbitMQ、Outbox/Canal Relay；Agent 部分任务使用 Temporal |
| Search | Elasticsearch 负责 BM25/全文检索 |
| Vector | Qdrant 负责推文向量与 Episodic Memory |
| AI | OpenAI-compatible Chat/Embedding；DashScope 与 LM Studio 是当前默认适配目标 |
| Governance | Consul、OpenTelemetry/Jaeger、Prometheus/Grafana、Loki、Pyroscope、Sentinel |

## 3. 主运行路径

### 社交请求

```text
Web/Mobile -> Gateway -> gRPC Service -> Repository/Cache
                                      -> RabbitMQ/Outbox -> Consumer
```

- Gateway 承担 HTTP/WebSocket、JWT 和 gRPC Client；新增业务逻辑不应继续下沉到 Gateway。
- User/Tweet/Follow/Messenger/Notification 主数据当前落 MySQL。
- Redis 提供 Timeline、缓存和实时通知，不是永久事实源。

### 推文与 Timeline

```text
CreateTweet -> MySQL/Outbox or MQ -> Consumer -> Redis Timeline
                                      |        -> Elasticsearch BM25
                                      +-------> Qdrant vector
```

当前 Timeline 仍以写扩散为主；大 V 推拉结合、重试/DLQ 和数据对账仍是演进项。

### Agent 请求

```text
Gateway -> Agent gRPC -> Service
                       -> Runtime/Message/Model/Profile
                       -> MCP/Workflow/RAG
                       -> Mongo + ES + Qdrant + LLM
```

- Runtime v2 通过 `AGENT_RUNTIME_V2_MODES` 按模式灰度。
- 用户自定义 DAG 由本地统一 IR 与持久状态机执行；重复语义的 Temporal bridge 已删除，Temporal 仅承载风控和热点后台 Workflow。
- Temporal 在组合根中主要承载风险控制与热点播报。

## 4. 核心边界

- Proto 是跨进程契约；Gateway 和服务端必须同时适配。
- `internal/domain` 是社交领域模型；Agent 的会话/工作流模型位于 `internal/module/agent/repository`。
- Agent `runtime` 不依赖 Service、数据库或 MCP SDK。
- Elasticsearch 与 Qdrant 职责分离，禁止把当前架构笼统描述为“ES 向量检索”。
- Agent 工具必须经过 Tool Registry/Executor；不能从 Prompt 直接越权访问数据库。
- 用户工作流不得保存明文 API Key；只保存 Credential Reference。

## 5. 事实来源

- 结构与任务入口：`.agents/context/project_map.md`
- 领域实体：`.agents/context/domain_model.md`
- 环境与端口：`.agents/context/environment_context.md`
- 当前技术债：`.agents/context/technical_debt.md`
- Agent Runtime：`.agents/context/agent_runtime_context.md`
- 阶段进度：`docs/PROJECT_PROGRESS.md`
- Agent 强化计划：`docs/AGENT_RUNTIME_STRENGTHENING_PLAN.md`

当这些文档与代码冲突时，以代码为准并同步修订文档。
