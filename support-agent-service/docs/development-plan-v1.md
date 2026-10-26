# 用户端智能客服 Agent：V1 开发计划

状态：**主链路已实现；本机交付收尾中，服务器生产准入递延**
日期：2026-09-27
依据：[`architecture-v1.md`](architecture-v1.md)

2026-09-29 范围调整：先交付本机联合运行版。以下为原始 V1 设计目标，不表示所有场景或生产门禁均已验收；实际完成状态见 [`progress-v1.md`](progress-v1.md)，本机交付范围与操作见 [`local-delivery-v1.md`](local-delivery-v1.md)。公网、TLS、服务器密钥、备份/容量及生产可靠性门禁另列下一阶段。

## 1. 开发目标

在不预设问题分类和解决方案的前提下，实现一条可运行、可测试、可演示的用户端智能客服链路：

```text
POST /api/v1/support/chat
  -> Supervisor 理解问题
  -> Planner 动态生成 TaskPlan
  -> LangGraph Send 并行创建 GenericWorker
  -> Worker 从 Capability Registry 动态选择工具
  -> 统一 Support Query API 获取证据
  -> 聚合、证据检查、必要时补充规划
  -> Supervisor 生成用户答复
  -> JSON / SSE 返回
```

V1 的完成标准不是“能调用模型聊天”，而是能用真实 Twitter Clone 数据完成至少三个用户支持场景，并能证明任务拆解、动态工具选择、并行调查和证据约束确实生效。

## 2. 开发原则

1. 先 Mock 后真实：先用 Mock Capability 跑通完整图，再接 Go 服务。
2. 先同步后流式：先完成 JSON 请求闭环，再增加 SSE，避免同时调试图和连接生命周期。
3. 先单轮后多轮：先保证单次调查正确，再接 MongoDB checkpoint。
4. 结构化边界优先：Planner、Worker、Aggregator 和 API 之间全部使用 Pydantic 模型。
5. Agent 决策、代码约束：模型决定任务和能力选择，代码只校验权限、Schema、预算和安全边界。
6. 每个阶段必须可测试、可演示，不积累到最后一次性联调。

## 3. 里程碑总览

| 里程碑 | 目标 | 主要产物 | 完成后可演示内容 |
|---|---|---|---|
| M0 | 工程骨架 | FastAPI、配置、日志、质量工具 | 服务启动、健康检查 |
| M1 | 核心协议 | Domain Models、Runtime Context、错误模型 | 所有 Agent 边界可校验 |
| M2 | 模型与 Capability 基础设施 | ModelProvider、Registry、Resolver、Executor | 模型可动态发现并调用 Mock Capability |
| M3 | GenericWorker | 通用 Worker 子图、预算和结果格式化 | 一个任务可自主循环调查并返回证据 |
| M4 | Supervisor 主图 | Planner、Send 并行、聚合、证据门、回答生成 | 复合问题端到端闭环 |
| M5 | 用户 API | `/support/chat` JSON + SSE | 前端可以流式调用 |
| M6 | 真实 Go 集成 | Support Query 契约与 HTTP Adapter | 使用当前登录用户的真实数据回答 |
| M7 | 会话与知识 | MongoDB checkpoint、Qdrant Capability | 多轮追问与规则问答 |
| M8 | 工程化验收 | OTel、指标、评测集、安全与故障测试 | 稳定演示和回归验证 |

依赖关系：

```text
M0 -> M1 -> M2 -> M3 -> M4 -> M5 -> M6 -> M7 -> M8
                         |           |
                         +-> Mock E2E+
```

M6 的 Go Facade 接口实现可以在 M3 完成后并行开展，但 Python 侧在 M5 前始终使用同一份契约和 Mock Server，不依赖 Go 实现进度。

## 4. 详细开发任务

## M0：工程骨架

### 任务

- 创建 `pyproject.toml`，固定 Python 3.12。
- 使用 `uv` 管理依赖并提交 lock 文件。
- 安装首批依赖：FastAPI、Uvicorn、Pydantic Settings、LangChain、LangGraph、HTTPX、structlog。
- 配置 Ruff、Pyright、pytest、pytest-asyncio/AnyIO。
- 将 `app/main.py` 改为 app factory，使用 FastAPI lifespan 初始化共享资源。
- 实现结构化日志、统一配置和异常基类。
- 实现 `/health/live`、`/health/ready`。
- 增加 `.env.example`、`.gitignore` 和基础 Dockerfile。

### 主要文件

```text
pyproject.toml
uv.lock
.env.example
Dockerfile
app/main.py
app/core/config.py
app/core/logging.py
app/core/errors.py
tests/unit/test_health.py
```

### 验收

- `uv run uvicorn app.main:app` 能启动。
- 健康检查返回成功。
- `uv run pytest`、`uv run ruff check .`、`uv run pyright` 全部通过。
- 启动阶段不连接模型、MongoDB 或 Go 服务也能运行基础健康检查。

## M1：核心协议与 Runtime Context

### 任务

- 实现 `SupportRequest`、`SupportCase`、`TaskSpec`、`TaskPlan`。
- 实现 `CapabilitySpec`、`CapabilityCall`、`CapabilityObservation`。
- 实现 `Finding`、`Evidence`、`WorkerResult`、`AggregatedResult`、`SupportResponse`。
- 实现 `AuthContext`、`RunBudget`、`RuntimeContext`。
- 定义 LangGraph `SupportState` 和 `WorkerState`。
- 为枚举和状态值建立最小集合；不得建立问题类别枚举。
- 定义字段长度、任务数、依赖关系和 confidence 范围校验。

### 关键约束

```text
SupportRequest 不接受 actor_user_id
TaskSpec 不包含 domain / worker_type
Planner 不直接授予 Capability 权限
Evidence 必须包含 source_ref 与 safe_summary
```

### 验收

- 非法计划、循环依赖、超预算任务能够在进入 Worker 前被拒绝。
- 所有协议都能生成 JSON Schema。
- 模型自由文本不能绕过 Pydantic 边界进入下一节点。

## M2：ModelProvider 与 Capability 基础设施

### 任务

- 实现 `ModelProvider`，支持从配置创建 OpenAI-compatible ChatModel。
- 为 Planner、Worker、Composer 提供独立配置入口，但 V1 可以使用同一模型。
- 提供 `FakeModelProvider`，测试时不访问真实模型。
- 实现扁平 `CapabilityRegistry`：注册、获取、列举和按权限过滤。
- 实现 `CapabilityResolver`：根据任务目标、所需证据和 Capability 描述进行语义候选检索。
- 实现 `CapabilityExecutor`：参数校验、身份上下文注入、超时、重试和统一错误映射。
- 实现内存 Mock Adapter 和首批 Mock Capability。

### Registry 责任边界

Registry 只描述系统拥有哪些原子能力，例如“读取当前用户最近的内容”或“检索已发布帮助文档”。它不维护以下规则：

```text
如果问题是 X，就必须调用 A、B、C
如果问题属于 Y，就创建某种 Worker
```

候选能力可以通过描述检索缩小上下文，但最终选择和调用顺序由 Worker Agent 决定。

### 验收

- 新 Capability 可以只通过注册加入系统，无需修改 Graph 和 Worker。
- 未授权、非只读、参数错误和超时都返回统一 `CapabilityObservation`。
- Fake Model 和 Mock Capability 可以完全离线测试。

## M3：GenericWorker 子图

### 任务

- 构建唯一的 `GenericWorker` 子图。
- Worker 根据 `TaskSpec` 调用 Capability Resolver 获取候选能力。
- 使用 LangChain Tool Calling 让模型自主决定调用、参数和后续步骤。
- 将 Capability 返回值转换为 ToolMessage/Observation，再交给 Worker 判断是否继续。
- 达到成功标准、模型主动结束或预算耗尽后生成结构化 `WorkerResult`。
- 实现最大模型调用数、最大工具调用数、单次超时和总超时。
- 对 Tool 返回内容做长度限制和 Prompt Injection 隔离。

### Worker 状态机

```text
prepare_task
  -> resolve_capabilities
  -> reason
       | tool call
       v
     execute
       -> observe
       -> reason
       | finish / budget exceeded
       v
  format_result
```

### 验收

- 同一 Worker 能针对不同任务选择不同 Capability，不需要切换 Worker 类型。
- Worker 能执行至少两轮“推理 -> 工具 -> 观察”。
- 模型选择不存在或无权限 Capability 时安全失败，而不是执行任意操作。
- 达到预算后返回 `partial`，不会无限循环。

## M4：Supervisor 主图

### 任务

- 实现 `intake`：清洗输入、生成关联 ID、建立最小上下文。
- 实现 `scope_guard`：只处理用户支持范围与只读请求。
- 实现 Planner 结构化输出。
- 实现 `validate_plan`：只检查结构、依赖、预算和权限，不写业务分类规则。
- 使用 LangGraph `Send` 为无依赖任务动态创建 GenericWorker。
- 使用 reducer 合并并行 `WorkerResult`。
- 实现确定性 Evidence Merger：去重、排序、来源追踪和冲突标记。
- 实现 Evidence Gate：根据任务成功标准判断是否足够回答。
- 实现最多一次的 re-plan。
- 实现 Supervisor Composer：只能根据 AggregatedResult 作答，不能新增事实。
- 实现 Output Guard：过滤内部字段、校验证据引用和敏感信息。

### 计划校验规则

允许代码判断：

- 任务数量是否超限。
- task id 是否唯一。
- `depends_on` 是否存在和是否成环。
- 是否越过只读与用户数据权限。
- 是否超过运行预算。

不允许代码判断：

- 用户问题属于哪一类。
- 某类问题应该调用哪些工具。
- 某个业务现象的固定解决方案。

### 验收

- 一个复合问题可以动态拆成至少两个 Worker 并行执行。
- 有依赖任务只在前置证据产生后执行。
- 证据不足时最多补充规划一次。
- Worker 结论冲突时，最终答复不得假装存在确定结论。
- 每项最终事实都能追溯到 `Evidence.source_ref`。

## M5：统一用户 API 与流式响应

### 任务

- 实现唯一业务入口 `POST /api/v1/support/chat`。
- 支持首次请求缺省 `conversation_id`，由服务生成并返回。
- `Accept: application/json` 返回完整 `SupportResponse`。
- `Accept: text/event-stream` 在同一接口返回 SSE。
- 将内部 Graph event 映射为少量用户安全事件。
- 实现请求取消、客户端断开检测和全链路超时。
- 通过 FastAPI dependency 注入可信 `AuthContext`。
- 增加请求限流接口，首版可使用内存实现并预留 Redis Adapter。

### SSE 事件

```text
case.started
case.understanding
case.investigating
response.delta
response.completed
response.failed
```

### 验收

- JSON 和 SSE 走同一条 Runtime，不产生两套业务逻辑。
- SSE 中不出现 Prompt、Thought、Tool 参数、Worker 内部结果或异常栈。
- 客户端断开后，未开始的 Worker 不再继续执行。

## M6：统一 Go Support Query 集成

### Python 侧任务

- 在仓库内定义 `support-query.openapi.yaml` 或等价 JSON Schema。
- 实现 `SupportQueryClient`，只调用：

```text
POST /internal/v1/support/query
```

- 请求包含 `operation`、`arguments` 和非敏感 request context。
- 身份通过 Gateway 签发的内部服务凭证传播，不从模型参数读取。
- 将统一响应转换为 `CapabilityObservation`。
- 使用 RESPX/WireMock 风格 Mock Server 完成契约和故障测试。

### Go 侧任务

- 实现 Capability Registry 与统一 Handler。
- 对每个 operation 绑定参数 Schema、只读属性和授权函数。
- Handler 根据 operation 调用现有 service client，不让 Python 感知微服务拓扑。
- 返回稳定、脱敏的 `data + evidence + error` DTO。
- 增加服务签名、用户身份传播、防重放与审计字段。

### 首批 Capability 建议

首批能力只是原子数据能力，不代表问题分类或固定解法：

- 获取当前登录用户的基础可见信息。
- 查询当前用户近期内容。
- 获取当前用户某条内容的用户可见状态摘要。
- 查询当前用户相关的公开关系摘要。
- 获取公开平台状态。

实际调查时调用哪些能力、调用几次、以什么顺序组合，仍由 GenericWorker 决定。

### 验收

- Python 侧新增业务调查能力只需要注册 Capability Adapter，不修改主图。
- Go 内部服务拆分变化不会影响 Agent 协议。
- 任意 operation 都不能查询超出当前认证用户权限的数据。

## M7：会话持久化与知识能力

### 任务

- 接入 `langgraph-checkpoint-mongodb`。
- 使用 `conversation_id` 作为 LangGraph `thread_id`，并校验其归属用户。
- 配置 checkpoint TTL、脱敏和清理任务。
- 实现对话上下文压缩，禁止无限累积全部历史消息。
- 把 Qdrant 检索注册为普通 Capability，不创建 KnowledgeWorker。
- 只索引已发布、用户可见的帮助中心与社区规则。
- 保存文档版本、来源和更新时间，作为 Evidence 引用。

### 验收

- 同一 conversation 可以进行多轮追问。
- 服务重启后仍能恢复必要上下文。
- 不同用户不能访问同一个 conversation checkpoint。
- 知识检索和业务查询对 Worker 来说使用同一 Capability 协议。

## M8：可观测性、评测与上线验收

### 任务

- 接入 OpenTelemetry，传播 `trace_id / case_id / task_id / tool_call_id`。
- 导出 Prometheus 指标，对接现有 Jaeger、Prometheus 和 Grafana。
- 记录节点耗时、模型调用数、Token、Worker 并发、Capability 延迟和错误类型。
- 建立固定离线评测集和回归命令。
- 增加 Prompt Injection、越权查询、证据冲突和下游故障用例。
- 完成 Docker Compose 接入、资源限制、优雅停机和 readiness 检查。
- 编写本地运行、调试、联调和演示说明。

### 验收

- 任一用户答复都能通过 trace 定位到任务和证据，但前端看不到内部 Trace。
- 关键回归集通过率达到约定阈值。
- 关闭模型或任一下游服务时，API 能返回可理解的降级答复。
- 日志中不存在 API Key、JWT、完整私信或不必要的用户原文。

## 5. 首批端到端演示场景

### 场景 A：复合问题与并行调查

```text
“为什么我昨天发的内容别人看不到，而且最近关注数也不对？”
```

观察点：Planner 自主生成多个任务，GenericWorker 并行执行不同 Capability，Supervisor 合并回答。

### 场景 B：缺少对象信息

```text
“我刚发的那条为什么不正常？”
```

观察点：Agent 自主决定查询近期内容或向用户澄清，而不是代码用固定分类路由。

### 场景 C：知识与业务证据组合

```text
“这条内容的状态正常吗？平台对这类内容有什么规定？”
```

观察点：同一个 GenericWorker Runtime 能组合业务 Capability 和 Qdrant 检索能力。

### 场景 D：越权请求

```text
“帮我查看另一个用户的私密账号状态和后台处罚记录。”
```

观察点：模型可以理解请求，但 Capability Executor 和 Go Facade 必须拒绝越权。

### 场景 E：下游部分失败

一个 Capability 超时，其他调查成功。

观察点：系统返回有证据支撑的部分答复，并明确尚无法确认的内容。

## 6. 测试策略

| 层级 | 测试重点 |
|---|---|
| Unit | Pydantic 校验、Registry、Resolver、预算、Evidence Merger |
| Graph | 条件边、Send fan-out、依赖任务、re-plan 上限、失败分支 |
| Contract | Python Capability 请求与 Go Support Query DTO 一致性 |
| Integration | FastAPI + Fake Model + Mock Capability / Mock Go Server |
| E2E | 真实模型 + Go 测试环境 + MongoDB + Qdrant |
| Eval | 任务拆解质量、工具选择正确率、证据忠实度、越权拒绝率 |

测试不比较最终文案是否逐字相同，主要验证：

- 是否选择了合理的调查路径。
- 是否只调用被授权的 Capability。
- 是否引用真实 Evidence。
- 是否在证据不足时承认不确定性。
- 是否满足延迟与调用预算。

## 7. MVP 冻结范围

以下内容不进入 V1，即使开发过程中看起来“顺手可以做”：

- 写操作和人工审批。
- 管理员控制台。
- MCP Server/Client。
- Agent 市场和动态安装插件。
- Worker 之间自由通信。
- 任意代码执行、SQL 执行、日志搜索和 Trace 原文查询。
- 跨会话长期用户画像。
- 通用工作流编排平台。

## 8. 开工前不阻塞但需要配置的事项

这些信息缺失不影响 M0～M5，可先使用默认值和 Mock：

- OpenAI-compatible 模型的 base URL、model name 和 API Key 环境变量名。
- Go Gateway 到 Python 服务的部署地址和内部认证方式。
- MongoDB database/collection 命名与 checkpoint 保留天数。
- Qdrant collection 命名和 embedding 模型。
- 单请求 Token、耗时和并发预算最终值。

## 9. 建议的首个开发切片

第一次实现只做 M0 + M1，不接真实模型和 Go 服务：

```text
工程初始化
  + FastAPI app factory
  + 配置与日志
  + 健康检查
  + 全部核心 Pydantic 模型
  + SupportState / WorkerState
  + 单元测试和质量命令
```

这个切片完成后，第二次实现 M2 + M3，用 Fake Model 和 Mock Capability 验证 GenericWorker；第三次再组装 Supervisor 主图。这样每次都能独立验证，不会在第一次提交里同时混入模型、图、API 和真实业务接口问题。

每轮开发的详细输入、任务、测试、演示目标和退出条件见 [`iteration-plan-v1.md`](iteration-plan-v1.md)；网络及 Python 内部协议见 [`api-contract-v1.md`](api-contract-v1.md)。
