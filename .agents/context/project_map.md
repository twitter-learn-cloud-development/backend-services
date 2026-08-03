# Project Map（Agent 必读项目导航）

> 状态：当前仓库导航的唯一规范入口（Canonical Navigation Context）
> 最近核对：2026-08-03
> 模块：`twitter-clone`
> Go：`1.25.5`

## 1. 使用规则

1. 每个新用户请求开始分析前，先读本文件，再按“任务定位表”读取相关文件。
2. 本文件只描述稳定结构、依赖方向和入口，不复制具体业务实现。
3. 事实优先级：实际代码/配置 > `go.mod` 与 Proto > 本文件 > 其他设计或进度文档。
4. 如果代码与本文件冲突，以代码为准，并在本次改动中同步更新本文件。
5. `architecture_audit_report.md` 是历史审计快照，不等于当前实现；仅在架构评审、阶段规划和技术债任务中读取。

## 2. 一屏拓扑

```text
Web (Vue 3) / Mobile (Flutter)
             |
             v
Gateway (HTTP/WebSocket, Consul discovery)
             |
             +--> Auth/User/Tweet/Follow/Messenger/Notification (gRPC)
             |
             +--> Agent Service (gRPC :9100, loopback MCP :9200, metrics :9191)
                        |
                        +--> Agent Runtime / Strategy / Message / Model / Profile
                        +--> Workflow DSL / Engine / Tool / RAG
                        +--> MongoDB (dialogue/workflow/run/project/approval/tool result)
                        +--> MinIO (private large Tool Result objects + versioned/WORM Agent Eval reports)
                        +--> ES (BM25) + Qdrant (vector)
                        +--> Public Web Search Provider (optional, governed egress)
                        +--> LLM/Embedding Provider

Tweet/Follow/... --> MySQL + Redis + RabbitMQ --> Consumer/Canal Relay
Agent AIOps jobs --------------------------------> Temporal (optional)
```

## 3. 顶层目录

| 路径 | 责任 | 进入条件 |
|------|------|----------|
| `cmd/` | 所有可执行程序的装配入口 | 服务启动、依赖注入、端口、生命周期 |
| `api/<domain>/v1/` | Proto 与生成的 gRPC 契约 | API/RPC 字段或方法变化 |
| `internal/domain/` | 社交领域实体与仓储接口 | User/Tweet/Follow/Message 等领域语义 |
| `internal/module/` | 各业务模块实现 | 服务、仓储、gRPC、缓存逻辑 |
| `internal/gateway/` | HTTP/WebSocket 入口、鉴权、gRPC Client | Web API 与路由问题 |
| `internal/infrastructure/` | MySQL、Mongo、Redis、MQ、Canal 等适配 | 基础设施连接和实现 |
| `internal/mq/` | RabbitMQ producer/consumer | Timeline、通知、异步事件 |
| `pkg/` | 可跨模块复用的基础库 | AI、ES、Qdrant、日志、指标、注册、Tracing |
| `web/` | Vue 3 + Vite + Pinia 前端 | 桌面 Web UI |
| `mobile/` | Flutter + Riverpod 客户端 | 移动端 UI |
| `deploy/`、`docker-compose*.yaml` | 容器、监控与本地基础设施 | 部署、端口、依赖服务 |
| `docs/` | 计划、进度、API 与问题记录 | 交付记录和阶段治理 |
| `.agents/` | 项目上下文、治理规则与原生仓库级 Skills | 开发前治理与按需 Skill 触发 |
| `.codex/agents/` | Codex 项目级自定义 Subagent 配置 | 测试、审查等专用子任务 |

## 4. 可执行服务地图

| 程序 | 默认端口/形态 | 核心代码 | 主要依赖 |
|------|---------------|----------|----------|
| `cmd/gateway` | HTTP `8080`；`.env.example` 使用 `9638` | `internal/gateway/` | Consul、Redis、各 gRPC 服务、MinIO |
| `cmd/auth-service` | gRPC `9097` | `internal/module/auth/` | User gRPC、Redis |
| `cmd/user-service` | gRPC `9091` | `internal/module/user/` | MySQL、Redis |
| `cmd/tweet-service` | gRPC `9092` | `internal/module/tweet/` | MySQL、Redis、RabbitMQ、Follow 数据 |
| `cmd/follow-service` | gRPC `9093` | `internal/module/follow/` | MySQL、Redis、RabbitMQ、Tweet 数据 |
| `cmd/messenger-service` | gRPC `9094` | `internal/module/messenger/` | MySQL（GORM）、Redis Pub/Sub |
| `cmd/notification-service` | gRPC `9095` | `internal/module/notification/` | MySQL、Redis、RabbitMQ |
| `cmd/agent-service` | `AGENT_PROCESS_ROLE=api|worker|all`；API 暴露 gRPC `9100` + loopback MCP `9200`，所有角色暴露 Prometheus `9191` | `internal/module/agent/` | Mongo、MySQL、Redis、MQ、ES、Qdrant、LLM、私有 MinIO、可选 Temporal；Worker 直接拥有风控队列及其 Retry/DLQ，默认 `all` 兼容单进程部署 |
| `cmd/agent-mcp-acceptance` | 显式 Live 的远程 MCP 一次性验收命令 | `internal/module/agent/mcp/acceptance/`、`internal/module/agent/mcp/remote/` | 必须传 `--allow-live`；写探针额外要求 `--allow-write`，报告只保存摘要/固定错误码并可 HMAC 签名 |
| `cmd/agent-mcp-conformance` | 回环长驻 MCP 协议夹具，默认 `127.0.0.1:9320` | `internal/module/agent/mcp/acceptance/`、MCP SDK Server | 只绑定回环地址；用于本地协议、幂等、故障和验收器测试，不代表真实第三方证据 |
| `cmd/agent-rag-eval` | 显式 live 的只读 RAG 对比命令 | `internal/module/agent/eval/`、`pkg/es`、`pkg/qdrant`、`pkg/ai`、`internal/module/agent/model` | 必须传 `--allow-live`；`--strategies` 按依赖闭包懒初始化 Provider；出站连接受 Endpoint Policy/受限 HTTP Client 保护；输出策略与 Provider 失败率 JSON 报告 |
| `cmd/agent-router-eval` | Cascade Router 基线与 Provider 对照命令 | `internal/module/agent/eval/`、`internal/module/agent/workflow/rag/`、`internal/module/agent/model/endpoint_policy.go` | 默认 lexical 且不连接外部服务；semantic/llm/full 必须显式 `--allow-live`，输出错投、Stage、Provider 错误、Token/成本 JSON 报告 |
| `cmd/agent-task-eval` | Agent 行为/安全评测、单/多策略对照、签名报告、加密人工复核/失败诊断材料、版本化归档与质量门禁命令 | `internal/module/agent/eval/`、`internal/module/agent/multirole/`、`internal/module/agent/objectstore/`、固定任务集、录制结果或受控 Runtime Adapter | 默认离线；Live 必须显式使用 `--allow-live`、一个 Runtime 配置、`--live-authorization` 和持久授权账本。单机默认使用 `--live-authorization-state` 的 file 账本；多实例可显式选 Redis 后端并先独立初始化，inspect/revoke 管理模式只读模型无关状态或原子冻结授权；完整状态检查固定原子末端 Stream ID，分页重放事件并对账用量后输出稳定摘要。签发授权前可用 `--plan-live-evaluation` 在不读取 Credential、不构造 Provider Client 的前提下计算调用最小值/上界、Profile Token/费用和正文上限；授权必须覆盖完整调用与费用计划。授权由独立命令离线签发，绑定 Provider/精确 Model、数据集/配置哈希、有效期及运行、Provider 调用、正文捕获和估算成本上限；每次模型调用在出网前向选定账本原子预留，过期、撤销、篡改或超预算立即失败。策略模式在同一规范化 Provider/Model/Pricing/Profile Snapshot 和配置哈希内执行真实模型单/多对照；启动先做真实 Chat/Tool Call 预检，执行错误快速终止，`--checkpoint-dir` 用逐 Case HMAC 哈希链在同身份连续前缀上恢复。固定任务 JSON 拒绝重复 Object Key，当前策略契约为 `agent-strategy-cases-v3`。工具仍为无副作用结构化证据沙箱。策略门禁只比较两个已开放的只读研究草拟模板，报告可 HMAC 签名验签并归档到私有 MinIO Object Lock；无 Checkpoint 的 Review Bundle 用独立密钥加密输入/正文并绑定最终报告哈希。打开 Bundle 可同时生成默认全拒绝且待人工补全的外部审阅 Decision 模板。显式 `--capture-failed-review-bundle` 可保存失败诊断密文，但 Signoff 仍拒绝失败报告。模型名变化会生成新配置哈希，旧授权和资格报告不得混用；门禁不自动发布 Profile 或扩大 Multi-Agent 范围 |
| `cmd/agent-memory-migrate` | Episodic 迁移与只读验收命令 | `pkg/qdrant`、`internal/module/agent/workflow/rag/` | 仅接受显式用户 ID；支持 dry-run、原始 Point ID 迁移、`--verify-only` JSON 验收和显式删除旧集合 |
| `cmd/agent-profile-dlq-replay` | Profile 内容互动 DLQ 检查与限量人工重放命令 | `internal/module/agent/consumer/`、`internal/infrastructure/mq/` | 默认只检查并重新入队；执行要求显式操作人/原因，校验事件与累计重放上限，Broker Confirm 成功后才 ACK；报告不输出事件正文 |
| `cmd/agent-risk-dlq-replay` | Agent 风控 DLQ 检查与限量人工重放命令 | `internal/module/agent/service/risk_control_replay.go`、`internal/infrastructure/mq/` | 默认只检查并重新入队且不启用发布模式；执行要求显式操作人/原因，校验风控事件与累计重放上限，只回专用 `agent.risk.ingress`，Broker Confirm 成功后才 ACK；报告不输出 Tweet/作者 ID 或正文 |
| `cmd/timeline-event-dlq-replay` | Timeline 创建/删除 DLQ 检查与限量人工重放命令 | `internal/mq/consumer/timeline_event_replay.go`、`timeline_recovery_topology.go`、`internal/infrastructure/mq/` | 默认只检查并重新入队；执行前幂等声明 Timeline 专用恢复拓扑，校验事件/累计次数并只回 `timeline.ingress`，Broker Confirm 成功后才 ACK；报告不输出 Tweet/作者 ID 或正文 |
| `cmd/timeline-moderation-dlq-replay` | Timeline 影子治理清理 DLQ 检查与限量人工重放命令 | `internal/mq/consumer/moderation_replay.go`、`internal/events/`、`internal/infrastructure/mq/` | 默认只检查并重新入队且不启用发布模式；执行要求显式操作人/原因，校验路由、事件和累计重放上限，只回 `timeline.ingress`，Broker Confirm 成功后才 ACK；报告只输出摘要与固定结果码 |
| `cmd/consumer` | RabbitMQ consumer；Prometheus `2116` | `internal/mq/consumer/` | RabbitMQ、MySQL、Redis、ES、Qdrant；Timeline 失败转发使用独立 Confirm Publisher，新重试通过 `timeline.retry` 与版本化 `.retry.v2` 只回 `timeline.ingress`；`tweet.created` 在 ACK 前完成幂等 ZSet 扇出、Redis Lua 趋势投影和唯一键 `sync_es` Outbox 入队 |
| `cmd/canal-relay` | CDC relay | `internal/infrastructure/canal/` | MySQL Binlog/Canal、RabbitMQ |

服务发现与公共基础能力：`pkg/registry`、`pkg/config`、`pkg/logger`、`pkg/metric`、`pkg/trace`、`pkg/profiler`、`pkg/serviceauth`。`pkg/serviceauth` 只提供方法级内部 gRPC 身份凭据与 fail-closed 守卫，不承载业务授权。

## 5. Agent 模块地图

```text
cmd/agent-service/main.go                 # 组合根：gRPC/内部 MCP/指标/存储/Temporal/后台任务
  -> startup/                             # 纯配置启动计划：api/worker/all 与唯一 Trending Reporter owner
  -> grpc/                                # Proto 边界；鉴权后的 user/model/workflow 参数
  -> service/                             # 用例编排；P8 统一 RunAgent、兼容模式与工作流服务
       -> runtime/                        # Runner、Action、Step、Budget、Usage；不依赖 Service
       -> strategy/                       # 无 I/O Multi-Agent 准入、模板与版本化计划证据
       -> multirole/                      # 存储无关的顺序角色聚合核心；生产 Service 与 Eval 共用
       -> message/                        # Token-aware 上下文装配；依赖 Runtime 消息契约
       -> model/                          # ModelClient、Catalog、Endpoint Policy、Provider Adapter
       -> profile/                        # 不可变 Agent/Prompt Catalog、AtomicResolver、原子 Profile Set、Release 校验与确定性分桶
       -> project/                        # Agent 项目、成员角色、User Directory 与 MCP AccessResolver
       -> observability/                  # Run/Step/LLM/Tool TraceRecord、Recorder/Reader 契约
       -> objectstore/                    # 私有 MinIO Tool Result Adapter + Agent Eval Object Lock Archive；不进入 eval/workflow 核心
       -> repository/                     # Mongo dialogue/message/workflow run/publication/Agent execution run/project/trace/summary/tool governance/Profile versions/RBAC/experiments
       -> workflow/
            dsl/                          # 版本化 JSON DSL 强类型契约
            ir/                           # 确定性 Compile Plan、依赖与写冲突校验
            engine/                       # 并行波次调度、单写者状态、挂起/恢复
            tool/                         # ToolSpec/Executor、内置工具、审批、幂等、熔断与指标
            rag/                          # Cascade Router、Persona、Episodic、ES+Qdrant 检索
       -> mcp/                            # 回环 MCP、远程 Adapter/治理，以及隔离的生产验收 Runner/Conformance
```

关键约束：

- `runtime/` 不得反向依赖 `service/`、Mongo、Redis 或 MCP SDK。
- `AGENT_PROCESS_ROLE=api|worker|all` 控制顶层生命周期：`api/all` 启动 gRPC、Consul 与内部 MCP，`worker/all` 启动 MQ Consumer、治理巡检与 Temporal Worker。热点播报只允许 `temporal|disabled`，生产组合根不得自动启动进程内 Ticker。
- Temporal 风控 Activity 不得持有 GORM、FollowRepository 或直接修改 Timeline Key；近期发帖信号与治理命令通过 TweetService 内部 gRPC。TweetService 在同一 UoW 提交可见性和幂等 `TWEET_MODERATED` Outbox，Canal 投递 `tweet.moderated`，Timeline Consumer 按关注关系 ID 稳定分页并用 Redis 游标/完成标记重放全量清理。`GetAuthorPostingStats` 与 `ApplyTweetModeration` 必须经过 `pkg/serviceauth` 的精确方法白名单认证；未配置凭据时只关闭这两个特权 RPC，不得退化为匿名调用。ES/Qdrant/Redis 热点读取属于有界只读投影。
- 风控队列归 Agent Worker 所有，直接以 `queue.tweet.risk` 订阅原始 `twitter.events/tweet.created`；Timeline Consumer 不得二次广播风控事件。失败重试只能经 `agent.risk.retry -> agent.risk.ingress` 返回风控队列，不能重放到主事件交换机；人工 DLQ 重放同样只允许回专用 Ingress。Temporal Workflow ID 固定为 `RiskControl-Tweet-{tweet_id}` 并使用 `REJECT_DUPLICATE`，用于覆盖滚动升级、ACK 不确定和人工重放造成的重复投递。
- Timeline、Agent 风控和 Profile 内容互动消费者在正常失败路径中均先向固定 Retry/DLQ 路由取得 Publisher Confirm，再 ACK 原消息。Timeline 自动重试和人工重放只能经 `timeline.retry -> timeline.ingress` 回到自有队列，禁止重放 `twitter.events`。只有失败路由基础设施不可用时才允许有界等待后 requeue；进程关闭时允许直接 requeue 未提交消息。不得记录消息正文或把用户、Tweet、Run ID 放入指标标签。
- `tweet.created` 的派生处理顺序固定为 Timeline ZSet -> Redis 趋势 Lua -> `sync_es` Outbox -> ACK。趋势使用 Tweet ID 的 72 小时 Marker，主题规范化后最多 32 个；Outbox 使用唯一 `dedup_key` 并保留 72 小时 Success 收据。Outbox Worker 依赖 MySQL 8 `FOR UPDATE SKIP LOCKED` 原子领取，使用 Owner + 每批新 Token + 90 秒 Lease 防止多副本同时执行；过期 Attempt 被有界恢复，旧 Token 无权提交。单任务执行限制 60 秒并发启动，数据库收尾另有 5 秒上限。该护栏覆盖自动 Retry、重启和 ACK 不确定，不代表跨 Redis/MySQL/RabbitMQ 或 ES/Qdrant 的严格 exactly-once；外部写成功但回执提交失败时，固定文档/Point ID 可收敛状态，Embedding 与网络调用成本仍可能重复。
- Provider 差异放在 `model/` Adapter；业务模式只传模型/预算/Profile。
- `message/` 管上下文优先级和 Token 桶；RAG Chunk 不做请求内有损截断。
- `repository/` 只做持久化，不调用模型或工具。
- 旧 Workflow DSL 缺省按 v1 编译，新保存定义补齐版本；未知版本拒绝。Scheduler 只消费 `workflow/ir` 计划，节点只读 `StateView` 并返回 Delta，由协调器按声明顺序单写合并。
- Workflow 已使用独立不可变 Revision，Run 可固定指定 `workflow_revision_id`；Blackboard StateEvent 与周期 Snapshot 按 `state_version` 幂等持久化，周期游标使用专用原子更新且不覆盖运行控制字段，Resume 通过 Snapshot + Event 重建并校验 Checkpoint。显式全局状态写入支持七种内置 Reducer；Retry/Timeout/Cancel/Skip/Suspend 使用显式状态转换和确定性退避。DSL 顶层共享预算统一限制节点尝试、并发、总超时、Token 和成本，直接 LLM、Runtime Agent 与 Legacy 回退调用均在请求前预留并在恢复后继续累计。Tool 补偿通过 `agent_workflow_compensations` Journal 按反向拓扑持久执行，复用 Tool Policy/Approval/幂等并支持显式恢复；后台 Reconciler 安全回收普通工具的过期执行租约，独立脱敏 Journal API/控制台允许人工推进 `planned/failed/expired executing`，审批挂起和有效租约不可抢占。Run API 支持用户隔离分页、详情、持久跨实例取消及独立 Trace 查询，禁止用进程内 Cancel Map 冒充可靠控制。`GET /agent/workflow-runs/:id/traces` 返回脱敏 Run/Step/LLM/Tool 记录；`GET /agent/workflow-runs/:id/events` 使用租户隔离、TTL/长度有界的 Redis Stream 投递脱敏增量事件并支持游标恢复，Mongo Trace 仍是完整查询事实源；`GET /agent/workflow-runs/:id/replay` 提供用户隔离、哈希校验、无副作用的只读事件/快照/补偿证据；不得宣称完整 Event Sourcing 或所有补偿无人值守执行。
- `GET /agent/workflow-runs/:id/blackboard` 按目标 `state_version` 从最近快照与有界事件区间重建、校验并分页检索 Blackboard；游标绑定版本和过滤条件，值默认递归脱敏并限制预览大小，查询不执行 Scheduler/LLM/Tool。
- 所有工具通过 `workflow/tool` 的 `ToolSpec + ToolHandler` 注册，并由实例化 `ToolRegistry/Executor` 统一执行；JSON Schema 使用 `jsonschema/v6` 编译校验，Write/Risky 默认进入持久化 Approval Gate。
- Tool Result 由统一 Executor 执行 JSON 体积校验；超过硬上限直接失败，超过内联阈值经 Service Port 写入独立私有 MinIO 桶。Mongo 幂等记录只保存内联结果或对象引用，Trace 只保存哈希、长度和无凭证引用；`workflow/tool` 不依赖 MinIO/Mongo。
- 审批与工具执行结果位于 Agent Mongo Repository；Service 提供持久化 Adapter，`workflow/tool` 不依赖 Mongo。恢复令牌数据库只保存哈希；已批准审批可按 Run revision 签发 5 分钟内有效的一次性新授权，每次签发原子轮换并使旧令牌失效。
- 工具熔断按工具名维护 Closed/Open/Half-Open 状态；幂等结果回放必须先于熔断判断，不能因下游暂时熔断破坏已提交结果的可回放性。
- Tool Prometheus Label 只允许有限枚举（工具、类别、来源、决策、错误码、熔断状态），禁止放入 user/run/input/error text 等高基数字段。
- Agent 执行 Recorder 扇出到 Mongo、Prometheus 与 OTel；Mongo 是查询事实源。Agent gRPC Server 和 Tweet/User Client 使用 `otelgrpc`，Provider/MCP HTTP 使用保留原 Client 安全策略的 Transport 包装与 W3C Trace Context；指标禁止 user/run/step/model/error 原文 Label，HTTP Path/Query/Header 与 Prompt/Completion/Tool 正文不得进入 Span。Prompt/Completion 预览采样独立配置、默认关闭、确定性有界并拒绝疑似敏感内容，只允许进入租户鉴权后的 Mongo Trace。Agent 指标由 `:9191` 暴露，Docker/Helm 的 Grafana 面板固定引用 `prometheus` datasource UID。
- 后台治理对账负责回收过期审批和陈旧执行租约；审批过期时必须终止对应挂起 Run，并清理恢复字段，避免永久悬挂。
- 内置 MCP 只允许回环地址，HTTP Bearer Token 与 Tool Middleware 认证缺一不可；令牌不得写日志，未显式配置时仅使用进程内随机令牌。
- 可重试发布工具把稳定执行键传给 TweetService；Tweet/Poll/Outbox 与用户级幂等绑定在 MySQL 同事务提交。
- Runtime v2 由 `AGENT_RUNTIME_V2_MODES` 按模式灰度，Legacy 暂时保留回滚。
- P8 `RunAgent` 使用可注入不可变 Capability Catalog 与保守 Planner。`conversation.reply` 使用无工具 Runtime Profile；`content.draft` 只有消息与 Run 持久化成功后才返回绑定 `source_run_id` 的 Artifact；`platform.search -> content.draft` 通过只读 `hybrid_search_tweets` 完成站内研究草拟。Web `Agent.vue` 统一调用 `RunAgent`，旧 API 与 Feature Flag 保留回滚。
- P8.2 的 `internal/module/agent/websearch` 提供 Brave Adapter、受治理 Page Reader、Redis 来源缓存、用户/Run 准入预算和 `kind=web_search` 用户级加密 Provider Config。动态 Provider 与平台兜底共享并发闸门、Cache 和 Governor；未选择用户配置且没有平台 Key 时 fail-closed。站内搜索、Web Search 和 Page Read 输出版本化 Structured Content，Citation 只从受信任 Action/Observation 生成；公网请求受 SSRF、禁重定向、DNS Rebinding、Prompt Injection、超时、并发、数量、字符和响应体边界保护。
- P8.3 的 `internal/module/agent/mcp/remote` 提供个人/项目级加密 Connection、部署托管项目凭据 Resolver、独立 Egress/Feature Flag、Streamable HTTP/SSE Discovery、`server_id.tool_name` 命名空间、不可变 Schema Snapshot、Snapshot-bound Tool Policy、有界 Session Pool 和跨实例主动健康巡检。`internal/module/agent/project` 维护 `owner/editor/viewer`、Revision CAS 成员关系和 User Service 真实用户校验；MCP 只依赖 `AccessResolver`，列表、按 ID/Server 解析、目录与真实调用均重查当前成员关系。用户凭据加密保存；项目 Bearer 连接可改用严格绑定项目/Endpoint/Auth 的托管引用，Token 从专用只读 Secret 目录按调用解析，Connection 不保存 Token/密文，共享成员不能读取密钥。同版本 Secret 文件轮换只切换 Session Identity；Registry Version 漂移会拒绝执行，必须 CAS 重存并清空 Snapshot/Policy。Workflow 可逐次审批 `risky` 与契约完整的声明式幂等 `write`；Unified Agent 在 `AGENT_RECOVERABLE_RUNS_ENABLED`、`AGENT_EXTERNAL_MCP_ENABLED` 和 `AGENT_UNIFIED_APPROVAL_RECOVERY_ENABLED` 同时启用时，也从当前用户治理交集装配 `read/risky/write`。独立 `agent_execution_runs` 使用 Revision CAS 保存权威生命周期；`ask_human/tool_call` 共享加密 `react.v1` Checkpoint、Resume Claim 租约和租户隔离恢复 RPC。工具授权签发、恢复及真实调用前重新校验 Profile/Prompt、Connection、Credential、Snapshot、Schema、Policy、审批和动作/输入/幂等绑定；一次性令牌只保存哈希并轮换，恢复精确动作不重放模型或已成功 Step。`risky` 最多一次远端尝试，`write` 仅以平台稳定键做有限同键重试；拒绝或过期终止 Agent Run。`mcp/acceptance` 通过独立显式 Live/Write 命令复用生产网络与 SDK 边界，生成不含 URL/凭据/原始结果的可签名报告；回环 Conformance 只用于框架自测。项目作用域、托管凭据、审批恢复和验收 Job 均默认关闭；真实 Brave/第三方 MCP、旧 Token 撤销、Kubernetes Secret 轮换和多副本受控验收尚未完成，第三方幂等声明不是严格 exactly-once 证明。
- P8.4 Workflow-as-Tool 位于 `repository/workflow_tool_publication.go`、`service/workflow_as_tool.go` 与 Unified Agent `runtime.workflow` 路径。发布记录按用户隔离并用 Revision CAS 绑定不可变 Workflow Revision/DSL Hash；继续保存草稿不改变已发布行为。`AGENT_WORKFLOW_AS_TOOL_ENABLED` 默认关闭，关闭时拒绝新发布和 Runtime 发现/调用，但允许查询与停用。可发布 DAG 必须无补偿、无 `agent`、无递归 Workflow Tool；只读工具不得声明审批，写工具必须同时声明逐次审批与幂等，风险工具必须逐次审批，审批型发布还要求 Recoverable Run、统一审批恢复、Run Store 与 Checkpoint Cipher 全部可用。外部 MCP 节点按当前 Snapshot/Policy 做同等校验。`wait` 仅允许显式 `resume_mode=human_input` 和非空有界问题；外部回调、用户恢复令牌及未知工具继续 fail-closed。动态工具复用统一 ToolExecutor；子 Workflow Run 保存 `parent_run_id/parent_action_id`，父子准入预算独立且父工具超时约束子运行。第二增量在 `internal/module/agent/skill/` 与 `service/workflow_skill_catalog.go` 增加版本化 Skill 契约：Skill 由当前用户 Active 发布记录确定性投影，精确版本绑定发布/Workflow Revision、DSL Hash、Profile/Prompt、预算、指令、单一工具和输出 Schema；`skill.run -> runtime.skill` 只接受显式 `skill_id + version`，并在规划、Runtime 和真实工具调用前重校验绑定。`AGENT_SKILL_CATALOG_ENABLED` 默认关闭且依赖 Workflow-as-Tool；关闭只撤销目录和路由，不改发布元数据。
- P8.5 第三增量在 `internal/module/agent/extension/` 与 `service/agent_extension_catalog.go` 增加 `agent.extension.v1` 租户已安装目录。它只聚合不可变 Capability、精确版本 Skill 和 `ListGovernedTools` 已过滤的 MCP Tool，使用有界过滤、确定性排序与过滤条件绑定 Cursor；不返回 Endpoint、Credential、输入 Schema 或 Tool Result，也不提供第二条执行路径。`GET /agent/extensions` 只信任 Gateway JWT 租户；Web `AgentExtensionCatalogDialog.vue` 对 Skill 做精确版本解析，对 MCP 只跳转既有管理面。`AGENT_EXTENSION_CATALOG_ENABLED` 默认关闭且不改任何来源数据。
- P8.5 第四、五增量在 `internal/module/agent/marketplace/`、`repository/extension_marketplace.go`、`service/agent_extension_marketplace.go` 与 `service/extension_marketplace_manager.go` 建立签名公共目录及独立发布控制面。公开读取仍经只读 Store 逐条复验发布者、密钥、Manifest、Release ID 和 Ed25519 签名；管理写入由专属内部令牌保护，JWT Actor 在 Agent Service 端按平台管理员或 Mongo 中不可变 Owner 重新授权。系统只保存公钥；Active Key 可发布，轮换将旧 Key 置为 Retired，Revoked Key 使历史签名失效。发布者与版本使用 Revision CAS，版本只能从 Published 终态撤回，所有写操作追加 requested/succeeded/failed 低敏审计。公开目录与管理面分别由 `AGENT_EXTENSION_MARKETPLACE_ENABLED`、`AGENT_EXTENSION_MARKETPLACE_ADMIN_ENABLED` 默认关闭；`GET /agent/marketplace/extensions` 仍无密钥/Artifact URL/安装授权，`/agent/marketplace/manage/*` 与 `ExtensionMarketplaceAdmin.vue` 只治理元数据和离线签名。Artifact 分发、扫描、依赖解析、安装审批、Owner 转移和租户安装尚未实现，不能称为可安装开放市场。
- P8.4 第五、六增量的 Tool Continuation 位于 `runtime/types.go`、`runtime/checkpoint.go`、`runtime/runner.go`、`service/agent_execution_*`、`service/workflow_as_tool.go` 与 `web/src/components/agent/ApprovalInbox.vue`。父 Agent 将子 Run、父 Action、发布 Revision/DSL Hash 和版本化 Continuation 放入加密 Checkpoint；人工输入恢复携带子恢复凭证，审批恢复只保存子审批引用。子 Workflow 独占审批事实与一次性 Grant，审批中心将子 Grant 交给父 Agent Resume，父 Runtime 在同一 Tool Action 内恢复子 Run，不创建第二条审批或父级令牌。恢复重新校验租户、谱系、Action、Revision/Hash、审批摘要/幂等键与 Grant；子 Run 已成功而父提交中断时从权威 Blackboard 回放确定性输出。拒绝或过期同步终止子 Run 与委托父 Run。
- P8.4 第七至十七、二十增量的 Multi-Agent 准入、执行与评测位于 `internal/module/agent/strategy/`、`internal/module/agent/multirole/`、`service/multi_agent_strategy.go`、`service/multi_agent_execution.go`、`eval/agent_strategy_gate.go`、`eval/agent_task_evidence.go` 和 `cmd/agent-task-eval/{strategy_runtime_executor,runtime_executor,live_run,checkpoint,review_bundle}.go`。Strategy 是无 I/O 准入领域层；Multirole 是不依赖 Service、Mongo 或 MCP SDK 的顺序聚合核心，生产 Service 与 Eval 共用同一角色隔离、交接、父预算和失败语义。研究父 Profile 是原子 Profile Set 的唯一灰度 Anchor，计划固定 Anchor/Version，执行从单个 Catalog 快照解析同版角色并在模型调用前拒绝混合版本。`AGENT_MULTI_AGENT_PLANNER_ENABLED` 与 `AGENT_MULTI_AGENT_EXECUTION_ENABLED` 分别控制准入和真实执行。首个执行器只支持两个只读研究草拟模板及顺序 `researcher -> drafter -> reviewer`；执行关闭或准入不满足时回退原单 Agent，角色执行后失败不隐藏重跑。ReAct 最后一步移除工具目录并追加高优先级收束指令，Provider 若仍返回非终态动作会在工具执行前失败关闭。`agent-task-eval --strategy-runtime-config` 在同一 Provider/Model/Pricing/Profile Snapshot 和配置哈希中运行真实模型单/多对照，配置哈希不同直接 `ineligible`；Live 预检真实 Chat/Tool Call，逐 Case 检查点仅保存脱敏指标重建证据并用 HMAC 与前向哈希链防篡改，Provider 错误不会固化失败 Case。复核材料由 CLI 层独立 AES-256-GCM Bundle 承载，绑定签名报告并禁止与 Checkpoint/归档同跑，不改变 Eval 核心脱敏报告；显式失败诊断 Bundle 不能创建 Signoff。第十七增量为 `AgentTaskCase` 增加可选、仅属于数据集的 v3 Evidence Contract；报告 schema 保持 `agent-task-eval-report/v2`。独立 `agent_strategy_cases_v3.json` 提供 16 条实质证据与 4 条合法证据不足任务，声明 Claim/Terms/Evidence ID、精确方括号 Citation、拒答/无依据声明/内部元数据规则；沙箱按现有 `platform.tweet_search.v1`、`web.search.v1`、`web.page.v1` 投影，不新增生产 Tool Schema，执行身份升级为 v5。第二十增量的固定 qwen3.7/Profile v5 云对照为 Candidate `19/20`、Stable `15/20`，自动双门禁和报告/Bundle 复验通过；确定性短语/邻近度仍不是完整事实裁判，外部人工 Signoff 与 WORM 资格未完成，因此 Feature Flag 继续关闭。并行角色、角色挂起/审批恢复、写工具和任意拓扑仍未开放，旧 Multi API 不参与新路径。
- P8.4 第十八、十九增量的内容资格链位于 `eval/agent_task_content_review.go`、`eval/agent_task_content_qualified_evidence.go`、`cmd/agent-task-eval/{content_review_signoff,report_archive}.go` 与 `objectstore/agent_task_quality_evidence.go`。Decision/Signoff 逐 Case 绑定报告、Review Bundle 和四维人工结论；`agent-task-content-qualified-evidence/v1` 将已验签报告与外部人工批准 Signoff 作为同一个 Object Lock 对象。归档前 CLI 必须用三把独立密钥重新验报告、解密 Bundle 和验 Signoff；Profile 严格模式仅持有报告/Signoff HMAC Key，不解密正文。`AGENT_PROFILE_EVAL_CONTENT_SIGNOFF_REQUIRED` 默认关闭且依赖旧证据开关，关闭可兼容裸报告回执；现有回执/API 不迁移。v3 云自动报告与加密 Bundle 已存在并通过双门禁，但仍无真实外部人工记录或 WORM Version ID。
- P8.4 第二十一至二十五增量在 `cmd/agent-task-eval/{live_plan,live_authorization,live_authorization_redis,live_authorization_redis_admin,live_authorization_redis_audit,review_decision_template}.go` 与 `eval/agent_task_output.go` 增加 Live 离线资源计划和授权预算闭环。计划固定 Provider/精确 Model、数据集/配置哈希并汇总调用、Token、费用与正文上界；签名授权、报告签名和 Review 加密密钥必须独立，授权必须覆盖完整调用/费用计划。默认 file 后端在运行和每次 Provider 调用前消耗本地 append-only HMAC 账本；可选 Redis 后端要求先用独立命令显式初始化，再由 Lua 按 Redis 服务端时间原子校验和累计四类额度、Stream 审计及 Reservation 去重，不同连接共享同一预算。无 TTL marker 发现 State/Stream 丢失后要求撤销授权，运行路径不会重新初始化；管理检查固定原子末端 Stream ID，分页重放精确事件、对账状态并输出可外部留存的稳定 SHA-256。签名报告可选携带授权 ID/摘要/预算证据；Redis 额外绑定后端和 Namespace SHA，历史报告形状不变。Stream 摘要不是 HMAC；Redis ACL/TLS/AOF/备份、`noeviction`、外部 WORM 和管理员权限仍属于部署信任边界。
- Profile 版本选择独立于 Runtime/Provider Rollout：内置、Mongo 已发布版本和 `AGENT_PROFILE_RELEASES` 分层构造不可变 Catalog，使用用户稳定分桶；`AtomicResolver` 只在完整校验后替换快照。协作角色通过父 Profile Release 原子选版，角色 Profile 不接受独立 Release；计划摘要固定 Profile Set Anchor/Version，运行时从单一快照精确解析，避免跨刷新混版。管理面已有项目级 RBAC、双人发布审批、管理 UI、append-only 脱敏审计和跨实例刷新。可选 Profile 实验控制器绑定 Release revision，采集脱敏 Run 可靠性指标与三类固定业务结果；Assist 显式确认发布通过窄应用 Port 校验来源 Run，以稳定幂等键调用 TweetService，并短期保存不含正文的 Tweet/Run 映射。真实发布可记录 `draft_published=true`，归因窗口内首个外部点赞/评论可经事务 Outbox、Canal 和 Agent 独立重试/DLQ 队列记录 `content_engaged=true`；自赞、超时和未操作不推断负样本。专用运维命令可限量检查/重放内容互动 DLQ，只有 Broker Confirm 后才 ACK，毒消息和超限消息继续保留。运行与业务护栏回归时按 CAS 自动回滚，但不会自动提升候选。`agent-task-eval` 已提供 52 条固定任务、受控 Live Runtime Adapter、签名质量门禁和 Object Lock COMPLIANCE 私有归档，Profile 发布会按精确对象版本复验回执；运行热路径不访问对象存储。录制夹具不等于真实模型/MCP 基线。Agent Service 是授权和来源运行校验事实源，Gateway 只传递 JWT 身份并做契约转换。外部组织目录、真实 Profile Eval 基线、明确拒绝动作和受控线上样本仍未完成。
- Temporal 当前只注册风险控制与热点播报；重复实现用户 DAG 语义的实验 bridge 已删除，不要宣称用户自定义 DAG 由 Temporal 托管。

- P8.4 第三增量的显式任务模板位于 `repository/agent_task_template.go`、`service/agent_task_template.go` 和统一 `RunAgent` 路径。`agent_task_templates` 是独立于 Workflow DAG 的不可变单输入预设：用户只能从自己的 completed 权威 Run 创建，自行提供含唯一 `{{input}}` 的指令；服务端不复制会话或输出正文，只保存源 Run Revision/结果摘要、Profile、Capability 和 Skill/Profile/Prompt 版本证据。执行前重新校验模板、源 Run 和当前路由，新 Run 记录模板 ID/Revision；模型和用户 Provider Config 保持当次选择。`AGENT_TASK_TEMPLATES_ENABLED` 默认关闭且依赖 Recoverable Runs，关闭执行仍允许列表与 CAS 归档。
- P8.4 第四增量的父子核算位于 `repository/agent_run_accounting.go`、`service/agent_run_accounting.go`、权威 Agent/Workflow Run 模型及 `GET /agent/runs/:id/accounting`。父 Run 与直接子 Workflow Run 写入 `execution.accounting.v1` 的预算上限、Step/Node、Token、估算成本和 Pricing Version；旧记录不解析业务 JSON 或 Trace。查询按 `user_id + parent_run_id + started_at + _id` 索引有界读取，返回父级、子级和总计并用 `complete/partial/unavailable` 暴露证据完整性。该投影不递归、不保存正文，也不改变父子独立准入预算。

## 6. 数据与中间件归属

| 组件 | 当前用途 | 主要访问方 |
|------|----------|------------|
| MySQL | 社交领域主数据、Tweet 发布幂等绑定，以及 Agent 自有 Persona/配置数据；风控 Activity 不直连社交表 | User/Tweet/Follow/Messenger/Notification、Agent Repository |
| MongoDB | Agent Dialogue、Message、Workflow Revision/DAG Run/StateEvent/Snapshot、Workflow Tool Publication、Unified Agent Execution Run、Agent Project/成员、Run/Step/LLM/Tool Trace、Tool Approval/Execution Result、Summary Cursor/Lease、Profile 版本/审批/项目角色绑定/实验运行与业务结果观测/审计、外部 MCP Connection/Schema Snapshot | Agent Repository |
| Redis | 缓存、Timeline ZSet、实时 Pub/Sub、幂等/配置、Agent Run 有界事件投递 | Gateway 与多数服务、Agent |
| RabbitMQ | 推文、Timeline、通知、风险控制等异步事件；Timeline 用 `timeline.retry/timeline.ingress` 隔离恢复投递，Agent 风控与 Profile 内容互动使用自有 Retry/DLQ，均采用 Confirm-before-ACK | Producer、Consumer、Agent |
| Elasticsearch | 推文 BM25/全文检索 | Consumer、Agent RAG/MCP |
| Qdrant | 推文向量与共享 `agent_episodic_memory` Episodic Memory；共享集合必须使用 `user_id` Payload Filter | Consumer、Agent RAG、`cmd/agent-memory-migrate` |
| Consul | 服务注册发现与部分动态配置 | Gateway、gRPC 服务 |
| Temporal | Agent 风控和热点播报的可选持久工作流 | Agent Service |
| MinIO | 公共媒体、私有大型 Tool Result、私有 Agent Eval Object Lock 报告使用不同 Bucket/访问/保留语义 | Gateway 上传链路、Agent Service、`agent-task-eval` 运维命令 |

## 7. 前端地图

### Web

- 技术栈：Vue 3、TypeScript、Vite、Pinia、Axios、Vue Flow。
- 路由：`web/src/router/index.ts`。
- API 封装：`web/src/api/`；状态：`web/src/stores/`；共享组件：`web/src/components/`。
- 业务页面：`web/src/views/`。
- AI 助手：`web/src/views/Agent.vue`。
- 外部连接与项目成员：`web/src/components/agent/ExternalMCPDialog.vue`、`AgentProjectDialog.vue`；项目作用域关闭或 API 不可用时个人连接表单保持可用，托管凭据只输入部署下发的引用，不录入或回显 Secret。
- 工具审批收件箱：`web/src/components/agent/ApprovalInbox.vue`；批准后查询最新 Run、签发短期一次性授权并立即恢复，不持久化明文授权，支持跨浏览器/跨设备处理。
- 工作流画布：`web/src/views/agent/WorkflowEditor.vue`，节点属性在 `web/src/components/agent/NodePropertiesDrawer.vue`；已保存 Workflow 可显式发布/停用不可变只读 Revision 供 Unified Agent 调用。
- 工作流运行追踪：`internal/module/agent/observability/` 定义存储无关契约、安全预览采样与模板身份字段，`repository/execution_trace.go` 持久化完整查询记录，`repository/execution_event_stream.go` 通过 Redis Stream 有界投递增量事件；Workflow Editor 分别展示节点、模型和工具调用，并按游标断线恢复。
- 工作流状态检索：`internal/module/agent/service/workflow_blackboard.go` 从持久快照和范围事件构建版本稳定、脱敏的字段页；Workflow Editor 运行控制台支持版本、路径、关键词和游标查询。
- 工作流只读回放：`internal/module/agent/service/workflow_replay.go` 装配并校验证据，Workflow Editor 的运行回放对话框消费 Gateway 结构化响应。
- 工作流 DSL 必须与 `internal/module/agent/workflow/dsl/` 和 gRPC/API 契约同步。

### Mobile

- 技术栈：Flutter、Riverpod、Dio、go_router。
- 功能代码：`mobile/lib/features/`；共享能力：`mobile/lib/core/`。
- Web 与 Mobile 并非自动同步；改用户可见行为时分别确认影响范围。

## 8. 任务定位表

| 任务 | 首读文件/目录 |
|------|---------------|
| 新增/修改 HTTP API | `internal/gateway/router/`、`handler/`、`client/`、对应 `api/*/v1/*.proto` |
| 修改 gRPC 契约 | 对应 `.proto`、生成脚本/Makefile、模块 `grpc/`、Gateway client |
| Tweet/Timeline | `internal/module/tweet/`、`internal/mq/`、`cmd/consumer/` |
| 登录/JWT | `internal/module/auth/`、`internal/gateway/middleware/` |
| Agent 统一入口/兼容模式 | `internal/module/agent/grpc/`、`internal/module/agent/service/unified_agent.go`、`internal/module/agent/service/agent_service.go`、`internal/module/agent/service/agent_profiles.go` |
| Agent Runtime | `.agents/context/agent_runtime_context.md`、`internal/module/agent/runtime/`、`model/`、`message/`、`profile/` |
| Multi-Agent 准入/执行/对照评测 | `internal/module/agent/strategy/`、`internal/module/agent/multirole/`、`internal/module/agent/service/multi_agent_strategy.go`、`service/multi_agent_execution.go`、`service/unified_agent.go`、`internal/module/agent/eval/agent_strategy_gate.go`、`internal/module/agent/eval/agent_task_content_review.go`、`cmd/agent-task-eval/strategy_runtime_executor.go`、`cmd/agent-task-eval/content_review_signoff.go` |
| Unified Agent Run 状态/恢复 | `internal/module/agent/repository/agent_execution_run.go`、`internal/module/agent/service/agent_execution_run.go`、`service/unified_agent.go`、`docs/adr/ADR-004-authoritative-agent-run-lifecycle.md` |
| 自定义工作流 | `internal/module/agent/workflow/dsl/`、`ir/`、`engine/`、`tool/`、`internal/module/agent/service/workflow_service.go`、Workflow Editor |
| Workflow-as-Tool | `internal/module/agent/repository/workflow_tool_publication.go`、`internal/module/agent/service/workflow_as_tool.go`、`service/unified_agent*.go`、`api/aiAgent/v1/aiAgent.proto`、Workflow Editor |
| 版本化 Skill 目录/执行 | `internal/module/agent/skill/`、`internal/module/agent/service/workflow_skill_catalog.go`、`service/unified_agent*.go`、`repository/agent_execution_run.go`、`api/aiAgent/v1/aiAgent.proto`、`internal/gateway/handler/agent_handler.go`、`web/src/views/Agent.vue` |
| 租户已安装扩展目录 | `internal/module/agent/extension/`、`internal/module/agent/service/agent_extension_catalog.go`、`internal/module/agent/mcp/remote/`、`api/aiAgent/v1/aiAgent.proto`、`internal/module/agent/grpc/agent_server.go`、`internal/gateway/handler/agent_handler.go`、`web/src/components/agent/AgentExtensionCatalogDialog.vue` |
| 签名公共扩展目录 | `internal/module/agent/marketplace/`、`internal/module/agent/repository/extension_marketplace.go`、`internal/module/agent/service/agent_extension_marketplace.go`、`api/aiAgent/v1/aiAgent.proto`、`internal/module/agent/grpc/agent_server.go`、`internal/gateway/handler/agent_handler.go`、`web/src/components/agent/AgentExtensionMarketplaceDialog.vue` |
| 显式任务模板 | `internal/module/agent/repository/agent_task_template.go`、`service/agent_task_template.go`、`service/unified_agent.go`、`api/aiAgent/v1/aiAgent.proto`、`internal/gateway/handler/agent_handler.go`、`web/src/api/agent.ts`、`web/src/views/Agent.vue` |
| 父子 Run 预算/成本核算 | `internal/module/agent/repository/agent_run_accounting.go`、`repository/agent_execution_run.go`、`repository/dialogue_model.go`、`service/agent_run_accounting.go`、`service/workflow_budget.go`、`api/aiAgent/v1/aiAgent.proto`、`internal/gateway/handler/agent_handler.go`、`web/src/views/Agent.vue` |
| Unified Agent 产品漏斗/归因 | `internal/module/agent/product/`、`internal/module/agent/repository/product_event.go`、`repository/agent_execution_run.go`、`service/product_events.go`、`service/unified_agent_product_metrics.go`、`internal/module/agent/mcp/remote/metrics.go`、`cmd/agent-service` |
| RAG/Memory | `internal/module/agent/workflow/rag/`、`internal/module/agent/service/cognitive_context.go`、`session_summary.go`、`internal/module/agent/eval/`、`pkg/es`、`pkg/qdrant`、`cmd/agent-memory-migrate`、`cmd/agent-rag-eval`、`cmd/agent-router-eval` |
| MCP 工具/外部连接/项目共享/托管凭据/生产验收 | `internal/module/agent/mcp/`、`internal/module/agent/mcp/remote/`、`internal/module/agent/mcp/acceptance/`、`internal/module/agent/project/`、`internal/module/agent/repository/agent_project.go`、`internal/module/agent/workflow/tool/`、`cmd/agent-service`、`cmd/agent-mcp-*` |
| 公网搜索/Page Read | `internal/module/agent/websearch/`、`internal/module/agent/evidence/`、`internal/module/agent/model/endpoint_policy.go`、MCP/Workflow 注册与 Capability Catalog |
| 模型/自定义 API | `internal/module/agent/model/`、`internal/module/agent/service/model_selection.go`、`internal/module/agent/workflow/tool/credentials.go` |
| Web UI | `web/src/views/` -> `components/` -> `stores/` -> `api/` |
| 部署/端口/观测 | `docker-compose*.yaml`、`deploy/`、各 `cmd/*/main.go`、`.env.example` |
| 架构评审/技术债 | 本文件、`architecture_audit_report.md`、`.agents/context/technical_debt.md`、强化计划 |

## 9. 生成文件与噪声边界

- `api/*/v1/*.pb.go`、`*_grpc.pb.go` 是生成文件；修改 `.proto` 后重新生成，禁止手工维护生成内容。
- 根目录 `*.exe`、`bin/`、`web/dist/`、Go build cache、上传文件不是架构事实，分析时默认忽略。
- 工作区允许存在用户未提交修改；禁止为了“干净”而回滚不属于当前任务的文件。
- 搜索优先使用 `rg`/`rg --files`，先按任务定位表限定目录，避免每次全仓扫描。

## 10. 验证入口

```powershell
# Go Agent（受限环境使用工作区缓存）
$env:GOCACHE='E:\GOProject\cloud\twitter-clone\tmp\go-build-cache'
go test ./... -count=1
go test -race ./internal/module/agent/eval ./pkg/qdrant ./internal/module/agent/service ./internal/module/agent/grpc ./internal/gateway/handler ./internal/gateway/router -count=1
go vet ./...
go run ./cmd/agent-router-eval --out tmp/router-eval/report.json # 纯离线词典/默认层基线，不连接模型
go run ./cmd/agent-task-eval --stable-results internal/module/agent/eval/testdata/agent_task_recorded_results.json --enforce-gate --out tmp/agent-task-eval/report.json # 固定录制结果的评分/门禁契约验证，不代表真实模型质量
go run ./cmd/agent-task-eval --dataset internal/module/agent/eval/testdata/agent_strategy_cases.json --dataset-version agent-strategy-cases-v2 --results internal/module/agent/eval/testdata/agent_strategy_multi_results.json --stable-results internal/module/agent/eval/testdata/agent_strategy_single_results.json --min-cases 20 --enforce-gate --strategy-gate --enforce-strategy-gate --out tmp/agent-task-eval/strategy-comparison.json # 单/多质量成本P95门禁契约自测，不代表生产性能
go run ./cmd/agent-task-eval --plan-live-evaluation tmp/agent-task-eval/qwen37-v5.live-plan.json --dataset internal/module/agent/eval/testdata/agent_strategy_cases_v3.json --dataset-version agent-strategy-cases-v3 --strategy-runtime-config internal/module/agent/eval/testdata/agent_strategy_runtime_config.qwen37-v5.example.json # 纯离线计划；不读取 API Key、不调用 Provider，模型或 Profile 改变后必须重算
go run ./cmd/agent-task-eval --initialize-live-authorization-state --live-authorization tmp/agent-task-eval/live-strategy.authorization.json --live-authorization-state-backend redis --live-authorization-redis-config internal/module/agent/eval/testdata/agent_task_live_redis_config.example.json --live-authorization-key-id live-authorization-v1 # Redis 多实例账本显式初始化；只验签授权并连接 Redis，不构造模型 Provider
go run ./cmd/agent-task-eval --inspect-live-authorization-state --live-authorization tmp/agent-task-eval/live-strategy.authorization.json --live-authorization-state-backend redis --live-authorization-redis-config internal/module/agent/eval/testdata/agent_task_live_redis_config.example.json --live-authorization-key-id live-authorization-v1 # 脱敏检查共享额度并逐事件重放到原子 Stream 边界；不读取模型 Credential
go run ./cmd/agent-task-eval --allow-live --runtime-config internal/module/agent/eval/testdata/agent_task_runtime_config.example.json --live-authorization tmp/agent-task-eval/live-baseline.authorization.json --live-authorization-state tmp/agent-task-eval/live-authorization-state --out tmp/agent-task-eval/live-baseline.json # 真实模型调用还必须先离线签发匹配授权并配置独立授权/报告 HMAC；工具仍为无副作用沙箱
go run ./cmd/agent-task-eval --dataset internal/module/agent/eval/testdata/agent_strategy_cases.json --dataset-version agent-strategy-cases-v2 --allow-live --strategy-runtime-config internal/module/agent/eval/testdata/agent_strategy_runtime_config.example.json --live-authorization tmp/agent-task-eval/live-strategy.authorization.json --live-authorization-state tmp/agent-task-eval/live-authorization-state --preflight-timeout 20s --case-timeout 90s --timeout 45m --checkpoint-dir tmp/agent-task-eval/live-strategy-checkpoint --progress --strategy-gate --enforce-strategy-gate --out tmp/agent-task-eval/live-strategy-comparison.json # 同配置真实模型单/多对照；授权须为该数据集/配置新签且 State Root 不得切换
go run ./cmd/agent-task-eval --dataset internal/module/agent/eval/testdata/agent_strategy_cases.json --dataset-version agent-strategy-cases-v2 --allow-live --strategy-runtime-config internal/module/agent/eval/testdata/agent_strategy_runtime_config.example.json --live-authorization tmp/agent-task-eval/live-strategy-review.authorization.json --live-authorization-state tmp/agent-task-eval/live-authorization-state --case-timeout 90s --timeout 45m --enforce-gate --enforce-strategy-gate --allow-review-content --review-bundle tmp/agent-task-eval/live-strategy.review.enc.json --review-key-id review-key-v1 --out tmp/agent-task-eval/live-strategy.review-report.json # 付费重跑且捕获正文，授权必须预留完整正文数量；禁止 checkpoint/archive
go run ./cmd/agent-task-eval --verify-review-signoff tmp/agent-task-eval/v3.content-signoff.json --review-report tmp/agent-task-eval/v3.report.json --review-bundle-input tmp/agent-task-eval/v3.review.enc.json --allow-review-content --review-signoff-key-id content-signoff-v1 # 离线复验报告、Bundle 与签认；三类密钥均只由环境变量注入，Judge 不能冒充人工批准
go run ./cmd/agent-task-eval --archive-report tmp/agent-task-eval/live-baseline.json --archive-config internal/module/agent/eval/testdata/agent_task_archive_config.example.json --archive-receipt tmp/agent-task-eval/live-baseline.archive-receipt.json # 要求专用私有 MinIO Bucket 启用 Versioning 与 Object Lock COMPLIANCE
# 将 archive receipt 粘贴到 /agent/profiles 发布申请；AGENT_PROFILE_EVAL_EVIDENCE_REQUIRED=true 时申请/批准/重试都会按精确对象版本复验
go run ./cmd/agent-rag-eval --allow-live --dataset internal/module/agent/eval/testdata/rag_cases.json --out tmp/rag-eval/report.json # 仅在依赖服务已显式启动且用户明确允许 live 时运行
# 终端一常驻运行本地回环夹具；终端二显式执行一次验收。真实第三方需使用独立无凭据配置和受控 Secret。
go run ./cmd/agent-mcp-conformance
go run ./cmd/agent-mcp-acceptance --allow-live --allow-write --config internal/module/agent/mcp/acceptance/testdata/conformance_config.example.json --out tmp/agent-mcp-acceptance/report.json

# Web
Set-Location web
npm run build

# Flutter
Set-Location mobile
flutter analyze
flutter test
```

测试范围应按改动扩展：窄改动先跑目标包，共享契约/Runner/Repository 改动再跑 Agent 全包与 `-race`。

## 11. 维护触发器

出现以下任一变化时，同一提交必须更新本文件：

- 新增、删除或重命名 `cmd/` 服务、核心模块、存储或消息通道。
- API、Agent Runtime、Workflow、RAG 的依赖方向发生变化。
- 端口、主执行路径、灰度开关或持久化归属改变。
- “任务定位表”指向的入口失效。

不要把每日进度、具体 Bug、完整文件清单写进本文件；这些内容分别进入 `docs/PROJECT_PROGRESS.md` 和 `docs/ISSUES.md`。
