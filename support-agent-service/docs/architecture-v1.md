# 用户端智能客服 Agent：V1 架构决策

状态：**Accepted**
日期：2026-09-27
范围：`support-agent-service` 用户端智能客服，不包含管理员控制台、工具审批、自动写操作和通用 Agent 平台。

2026-10-05 角色修订：当前小栖仅作产品使用向导，正式运行时只注册发布指南检索。
站内内容检索、分析、创作和实际执行任务由模型按语义识别，并提示用户自行前往
站内 AI 助手；混合问题保留说明。统一 Chat 接口与 Supervisor/Worker 技术链路不变，
规划采用 `GuideTaskPlan`，纯转交允许零个工具任务，含说明目标必须安排证据任务。
本文件下面的业务调查范围与 Go Query 架构保留为初始设计记录，不再作为当前
小栖能力清单。最新规则、协议与验收见 [`guide-role-v1.md`](guide-role-v1.md)。

## 1. 项目定位

本服务是 Twitter Clone 的用户端智能客服入口。它把用户的自然语言问题加工为可调查的子任务，调用用户有权访问的业务与知识工具并行取证，最后由客服 Supervisor 将结论转换为用户能理解的答复。

核心模式：

```text
Support Supervisor
  -> 理解问题
  -> 生成调查计划
  -> 并行分派 Specialist Workers
  -> 汇总与校验证据
  -> 生成面向用户的答复
```

本项目的价值不是“再做一个聊天机器人”，而是展示一条可解释、可测试、可恢复的 Python + LangChain + LangGraph 业务 Agent 链路。

## 2. 已敲定的边界

### 2.1 V1 要做

- 回答当前登录用户关于账号、帖子、关注关系、Feed 分发、通知和平台规则的问题。
- 根据用户问题动态拆分 1～5 个调查任务。
- 问题类型、任务目标、调查顺序和解决方案由 Agent 根据上下文动态决定，不在代码中维护固定问题分类路由。
- 独立任务并行执行；有依赖的任务顺序执行。
- Worker 通过只读 Tool 获取事实，输出结构化 `WorkerResult`。
- 对证据进行去重、冲突检测、充分性判断和一次受限的补充调查。
- 流式返回客服答复和少量安全的执行状态。
- 保留会话级短期记忆，但不默认建立跨会话用户画像。

### 2.2 V1 不做

- 不做管理员端 Agent、工作流设计器、Agent 市场、MCP 管理或工具审批系统。
- 不允许 Agent 发帖、删帖、改资料、关注/取关、发私信等写操作。
- 不让模型直接访问 MySQL、MongoDB、Redis、MQ、日志或 Trace。
- 不向用户暴露风控规则、内部错误栈、服务拓扑、日志、Trace、缓存键等内部数据。
- 不让 Worker 互相自由对话；所有协作通过 Supervisor 的 fan-out / fan-in 完成。
- 不在 V1 实现任意 DAG 调度器、长期自主运行或无限自我反思。

## 3. 总体架构

```text
Web / Mobile
    |
    | JWT + conversation_id + message
    v
Go Gateway
    | 认证、限流、透传 actor identity
    v
Python FastAPI
    |
    v
LangGraph Support Graph
    |
    +--> Intake / Scope Guard
    +--> Planner (structured output)
    +--> Plan Validator (deterministic)
    +--> LangGraph Send fan-out
            +--> Generic Worker(task A) --+
            +--> Generic Worker(task B) --+--> Evidence Aggregator
            +--> Generic Worker(task C) --+
    +--> Evidence Gate -- insufficient --> one bounded re-plan
    +--> Response Composer
    +--> Output Guard
    v
SSE response to user

Generic Worker
    -> Capability Resolver / Tool Registry
    -> dynamically selected read-only tools
    -> unified Support Query API
    -> existing Go microservices / Qdrant

Conversation checkpoint
    -> existing MongoDB
```

### 3.1 为什么 Python 服务不直接连接业务数据库

权限判断必须由原有 Go 业务边界完成。Python 服务只传递经过 Gateway 验证的登录主体，并调用面向客服场景的只读接口。这样可以避免模型生成任意 SQL、绕过资源归属校验，也避免 Python 侧复制各微服务的业务规则。

Python 侧不感知 `user-service`、`tweet-service` 等具体服务，也不为每类问题设计一套接口。推荐在 Go Gateway 或独立内部 Facade 对 Agent 暴露一个统一的查询协议：

```text
POST /internal/v1/support/query

{
  "operation": "<registered capability name>",
  "arguments": { ... },
  "request_context": {
    "case_id": "...",
    "task_id": "..."
  }
}
```

`operation` 不是任意方法名，而是 Go 侧注册的只读 Capability。Facade 根据认证主体、Capability 元数据和参数 Schema 完成授权、校验与路由，再调用现有微服务。新增业务能力时只注册 Capability，不修改 Agent 主图，也不新增某一“问题类别接口”。

统一返回：

```json
{
  "status": "success",
  "data": {},
  "evidence": [],
  "error": null
}
```

该 DTO 必须稳定、脱敏且面向用户可解释，不返回表结构、内部日志或原始风控策略。

## 4. LangGraph 主链路

```text
START
  -> intake
  -> scope_guard
       | unsupported / unsafe
       +---------------------------> compose_limited_response -> END
       |
       v
     plan
       -> validate_plan
       -> dispatch_workers (Send)
       -> worker_subgraph x N
       -> aggregate
       -> evidence_gate
            | enough ------------------------+
            |                                v
            |                         compose_response
            |                                -> output_guard
            |                                -> END
            |
            +-- insufficient and replan_count < 1
            |       -> replan -> dispatch_workers
            |
            +-- insufficient and limit reached
                    -> compose_partial_response -> output_guard -> END
```

### 4.1 各节点的职责

| 节点 | 类型 | 职责 |
|---|---|---|
| `intake` | 确定性代码 | 清洗输入、长度限制、注入认证主体、生成 request/case id |
| `scope_guard` | 规则优先 | 判断是否为支持场景；拒绝越权查询和危险请求 |
| `plan` | LLM 结构化输出 | 自主理解问题并生成 1～5 个 `TaskSpec`，声明目标、所需证据、依赖和成功标准；必要时决定先澄清 |
| `validate_plan` | 确定性代码 | 只校验任务数、依赖合法性、权限边界和预算，不判断问题类别或业务解法 |
| `dispatch_workers` | LangGraph `Send` | 为无依赖任务动态创建隔离 Worker 状态并并行执行 |
| `worker_subgraph` | 受限 Agent Loop | 根据任务动态发现并选择 Capability，循环调查，最多 4 次模型调用、6 次工具调用 |
| `aggregate` | 代码优先、LLM 辅助 | 证据去重、排序、冲突标注，形成统一结论 |
| `evidence_gate` | 确定性代码 | 根据成功标准判断是否足够回答；最多补充规划一次 |
| `compose_response` | LLM 结构化输出 | 将专业结论变成客服语言，不新增证据 |
| `output_guard` | 确定性代码 | 删除内部字段，校验证据引用和敏感信息，形成 API DTO |

关键原则：**问题分类、任务拆解、Capability 选择和解决方案由 LLM 决定；代码只负责协议、权限、预算、并发和最终安全边界。**

## 5. Worker 设计

V1 只有一种 `GenericWorker`。Planner 每生成一个 `TaskSpec`，Runtime 就动态创建一个 Worker 实例。Worker 没有预设的问题类型，也不和某个微服务绑定。

每个 Worker 收到任务后：

1. 根据任务目标和所需证据查询 Capability Registry。
2. 从检索到的候选 Capability 中自主决定调用哪个、参数是什么、是否继续调查。
3. 达到任务成功标准或预算上限后输出统一 `WorkerResult`。

Capability Registry 中的每项能力提供：

```python
class CapabilitySpec(BaseModel):
    name: str
    description: str
    input_schema: dict
    output_schema: dict
    read_only: bool
    user_data_scope: str
    evidence_types: list[str]
    timeout_ms: int
```

Registry 是“能力目录”，不是“问题分类表”。代码不维护“帖子不可见必须调用 A+B+C”之类的规则；Agent 依据 Capability 的自然语言描述、Schema 和当前证据动态选择。

### 5.1 通用 Worker 子图

```text
worker_start
  -> worker_reason
       | tool_calls -> execute_allowed_tools -> worker_reason
       | no tool call
       v
  format_worker_result
  -> worker_end
```

Worker 输入只包含：当前任务、完成任务所需的最小业务上下文、认证主体引用和当前允许使用的 Capability 集合。这个集合来自统一 Registry 的权限过滤与语义检索，不来自固定 Worker 分类。Worker 不接收完整会话历史，也不接收其他 Worker 的原始内部结果。

失败策略：

- 网络超时和 5xx：代码层指数退避，最多 2 次。
- 4xx/权限失败：不重试，返回结构化失败。
- Worker 达到预算：返回 `partial`，禁止无限循环。
- 单个 Worker 失败不拖垮全部请求；Aggregator 决定能否给出部分答复。

## 6. 核心数据对象

所有 Supervisor / Worker 边界都使用 Pydantic v2 模型，不使用自由文本充当协议。

### 6.1 `SupportRequest`

```python
class SupportRequest(BaseModel):
    conversation_id: UUID | None = None
    message: str
    client_timezone: str = "Asia/Shanghai"
    context: dict[str, str] = Field(default_factory=dict)
```

`actor_user_id` 不允许从请求体读取，只能来自 Gateway 验证后的身份上下文。

### 6.2 `SupportCase`

```python
class SupportCase(BaseModel):
    case_id: UUID
    actor_user_id: str
    original_message: str
    problem_statement: str
    hypotheses: list[str]
    entities: list[CaseEntity]
    time_range: TimeRange | None
    constraints: list[str]
    clarification_question: str | None
```

### 6.3 `TaskSpec` / `TaskPlan`

```python
class TaskSpec(BaseModel):
    task_id: str
    objective: str
    required_evidence: list[str]
    required_context: list[str]
    depends_on: list[str]
    resolves_gap_ids: list[str] = []
    success_criteria: list[str]


class TaskPlan(BaseModel):
    tasks: list[TaskSpec]
    answer_strategy: str
    request_goals: list[RequestGoal] = []
```

`TaskSpec` 不包含固定问题类别或 Worker 类型。Planner 也不能凭空授予权限；Worker 能看到的 Capability 由认证主体、只读策略和 Registry 动态过滤后得到。

2026-10-04 增量：补充任务可关联运行时生成的开放 gap；首轮、未知、已关闭和重复
引用拒绝。关联本身不意味着关闭，必须经过实际结果和独立证据评估。内部账本、
引用校验、模型语义评估限制见 [`gap-resolution-v1.md`](gap-resolution-v1.md)。

回答一致性增量：`request_goals` 是原请求所有必要目标的账本，不是固定业务类别；
最终独立评估必须覆盖它，并从原问题补入遗漏目标。正式运行时强制启用
`ResponseGuard`，逐目标组合可信记录、知识说明和限制；模型异常、超时或不完整
评估进入保守来源回退。内部协议及语义保证边界见
[`response-review-v1.md`](response-review-v1.md)。

### 6.4 `WorkerResult`

```python
class WorkerResult(BaseModel):
    task_id: str
    status: Literal["success", "partial", "failed"]
    conclusion: str
    findings: list[Finding]
    evidence: list[Evidence]
    confidence: float
    gaps: list[str]
    execution_errors: list[ObservationStatus] = []
    missing_inputs: list[CapabilityInputFields] = []
    provided_inputs: list[CapabilityInputFields] = []
```

`Evidence` 至少包含 `source_type`、`source_ref`、`observed_at`、`safe_summary`。原始 Tool payload 不进入最终提示词，只保存在受控 Trace 中并按保留策略清理。

上述调用状态和参数字段名由执行器/Worker 运行时生成，不接受模型 completion
自报证明，不包含参数值，也不暴露到用户 Chat JSON。只有真实 schema 必填缺失
且已有能力可继续执行时才允许澄清，能力不存在与工具故障不属于用户补充信息。

### 6.5 `AggregatedResult` / `SupportResponse`

```python
class AggregatedResult(BaseModel):
    answerable: bool
    conclusion: str
    supporting_evidence: list[Evidence]
    contradictions: list[str]
    unresolved_questions: list[str]
    unresolved_gaps: list[EvidenceGap]
    resolved_gap_ids: list[str]


class SupportResponse(BaseModel):
    case_id: UUID
    answer: str
    suggested_actions: list[str]
    status: Literal["answered", "partial", "clarification_required"]
    evidence_refs: list[str]
    trace_id: str
```

## 7. API 设计

用户端业务入口收敛为一个接口，不按问题类别、处理阶段或 Worker 拆 API：

```text
POST /api/v1/support/chat
GET  /health/live
GET  /health/ready
GET  /metrics
```

`POST /api/v1/support/chat` 同时承载首次提问和多轮追问，通过 `conversation_id` 关联上下文。客户端设置 `Accept: text/event-stream` 时直接返回 SSE；设置 `Accept: application/json` 时返回完整结果。V1 不先上 WebSocket。完整字段与错误协议见 [`api-contract-v1.md`](api-contract-v1.md)。

请求示例：

```json
{
  "conversation_id": "optional-uuid",
  "message": "为什么我昨天发的内容别人看不到？",
  "context": {}
}
```

SSE 只传以下安全事件：

```text
case.started
case.understanding
case.investigating          # 可带“正在检查帖子状态”等用户友好描述
response.delta
response.completed
response.failed
```

不得向前端流式发送 Prompt、Thought、Tool 参数、内部 Worker 名、原始证据或异常栈。

## 8. 认证与数据安全

- 浏览器 JWT 在 Go Gateway 校验；Python 只信任来自内部网络且带服务签名的身份头/令牌。
- `actor_user_id` 由认证上下文注入，不能由 LLM 或请求体指定。
- 每个 Tool 再做一次资源归属校验；不要把“Planner 已判断安全”当成授权。
- Tool 默认只读，使用显式 allowlist；不存在“任意 URL”“任意 SQL”“任意 gRPC 方法”工具。
- Prompt 中的用户文本、帖子内容和检索文档都视为不可信数据，不能覆盖系统策略。
- Checkpoint 和 Trace 可能含用户数据：启用 TTL、脱敏、访问控制；生产环境建议加密 checkpoint serializer。
- 最终答复只允许引用 `safe_summary`，不能引用原始内部 payload。

## 9. 技术栈

| 层 | 选择 | 用途 |
|---|---|---|
| 语言与包管理 | Python 3.12 + `uv` | 稳定的异步与类型生态、快速锁依赖 |
| API | FastAPI + Uvicorn | HTTP、SSE、依赖注入、OpenAPI |
| Agent 编排 | LangGraph 1.x | `StateGraph`、条件边、`Send` 并行、checkpoint |
| 模型与工具 | LangChain 1.x | ChatModel 适配、Tool Calling、Structured Output |
| Schema / Settings | Pydantic v2 + pydantic-settings | 协议校验和环境配置 |
| 服务调用 | HTTPX AsyncClient | 调用 Go Gateway / Support Facade |
| 会话 checkpoint | `langgraph-checkpoint-mongodb` + 现有 MongoDB | 多轮短期记忆、失败恢复，开发测试使用 InMemorySaver |
| 知识检索 | Qdrant Async Client + LangChain Embeddings | 仅索引已发布的用户帮助与规则文档 |
| 缓存与限流 | 现有 Redis | 短 TTL 查询缓存、分布式限流，不充当事实源 |
| 可观测性 | OpenTelemetry + structlog + Prometheus | 对接现有 Jaeger / Prometheus / Grafana |
| 测试 | pytest + pytest-asyncio/AnyIO + HTTPX/RESPX | 单元、图路由、契约和集成测试 |
| 质量 | Ruff + Pyright | 格式、Lint、静态类型检查 |

模型层使用 Provider Adapter，不在业务代码中硬编码厂商。V1 可先接现有 OpenAI-compatible 模型端点；Planner、Worker、Composer 都通过统一 `ModelProvider` 创建。首版先用同一个支持 Tool Calling 和 Structured Output 的模型，稳定后再按节点做强弱模型分层。

不把 LangSmith 设为运行时必需依赖。开发环境可选启用，生产以现有 OpenTelemetry 链路为主，避免用户内容默认流向额外 SaaS。

## 10. 推荐目录

```text
support-agent-service/
├── app/
│   ├── main.py
│   ├── api/
│   │   ├── dependencies.py
│   │   └── routes/
│   │       ├── health.py
│   │       └── support.py
│   ├── core/
│   │   ├── config.py
│   │   ├── errors.py
│   │   ├── logging.py
│   │   └── telemetry.py
│   ├── domain/
│   │   ├── enums.py
│   │   ├── support_case.py
│   │   ├── task_plan.py
│   │   ├── worker_result.py
│   │   └── response.py
│   ├── graph/
│   │   ├── builder.py
│   │   ├── state.py
│   │   ├── routing.py
│   │   ├── nodes/
│   │   │   ├── intake.py
│   │   │   ├── plan.py
│   │   │   ├── aggregate.py
│   │   │   ├── evidence_gate.py
│   │   │   └── compose.py
│   │   └── worker/
│   │       ├── builder.py
│   │       ├── state.py
│   │       └── nodes.py
│   ├── agents/
│   │   ├── planner.py
│   │   ├── worker.py
│   │   └── supervisor.py
│   ├── tools/
│   │   ├── registry.py
│   │   ├── policy.py
│   │   ├── capability.py
│   │   ├── registry.py
│   │   ├── resolver.py
│   │   └── executor.py
│   ├── integrations/
│   │   ├── support_query/
│   │   │   ├── client.py
│   │   │   └── dto.py
│   │   ├── checkpoint.py
│   │   ├── model_provider.py
│   │   └── vector_store.py
│   ├── prompts/
│   │   ├── planner.py
│   │   ├── worker.py
│   │   └── supervisor.py
│   └── services/
│       └── support_runtime.py
├── tests/
│   ├── unit/
│   ├── graph/
│   ├── contract/
│   ├── integration/
│   └── fixtures/
├── evals/
│   ├── datasets/
│   └── run.py
├── docs/
│   └── architecture-v1.md
├── .env.example
├── Dockerfile
├── pyproject.toml
└── README.md
```

与初稿相比，`schemas/` 改为 `domain/`，避免核心对象沦为 API DTO；`graph/` 只放编排，`agents/` 放模型驱动能力，`integrations/` 放外部 I/O。`tools/` 不再按业务类别分文件，而是实现统一 Capability 注册、检索、授权过滤和执行协议。

## 11. 运行时约束与预算

V1 默认预算：

- 每次请求最多 5 个任务。
- 最多 3 个 Worker 同时执行，防止模型和下游服务瞬时过载。
- 每个 Worker 最多 4 次模型调用、6 次 Tool 调用。
- 整个请求最多补充规划 1 次。
- 下游单次调用超时 2 秒；整个调查软超时 12 秒，硬超时 20 秒。
- 达到预算后给出诚实的部分答复，不伪造结论。

这些参数全部配置化，并在 Trace 与指标中记录。

## 12. 可观测性与评测

每个请求贯穿以下关联 ID：

```text
trace_id -> case_id -> conversation_id -> task_id -> tool_call_id
```

核心指标：

- 请求成功率、P50/P95/P99 总延迟。
- Planner 任务数、Worker 并发数、补充规划率。
- Tool 成功率、超时率和各下游延迟。
- 无证据回答率、部分回答率、澄清率。
- 单次请求模型调用数、Token 和估算成本。
- 输出安全规则命中率。

评测集至少覆盖：

- 帖子不可见、Feed 延迟、关注数异常、通知缺失、规则咨询。
- 模糊时间与缺失 tweet id。
- 多意图混合问题。
- 越权查询他人数据。
- Prompt Injection 与要求泄露内部日志。
- 某个 Worker 超时、结论冲突、证据不足。

## 13. MVP 实施顺序

### Phase 1：可运行骨架

- FastAPI、配置、日志、健康检查。
- Pydantic 核心对象。
- LangGraph 主图和 Mock Tools。
- 单轮非流式接口及图路由测试。

验收：用固定 Mock 数据完整跑通“问题加工 -> 规划 -> 并行 Worker -> 聚合 -> 客服答复”。

### Phase 2：接入真实 Twitter 数据

- Go Support Read Facade。
- 统一 Support Query API 和首批只读 Capability。
- 身份透传、资源归属检查、超时重试和契约测试。

验收：登录用户能询问自己的帖子状态与关注关系，且无法查询无权访问的数据。

### Phase 3：会话、流式与知识库

- MongoDB checkpoint、SSE。
- Qdrant 用户文档检索。
- Delivery 脱敏诊断摘要。
- OpenTelemetry 指标、Trace 和离线评测集。

验收：多轮追问可延续上下文，服务重启后可恢复会话，前端能看到安全的流式答复。

## 14. 架构验收标准

以下条件全部满足，V1 架构才算落地：

1. Planner 的输出始终通过 Pydantic 校验和服务端策略校验。
2. 一个复合问题能够 fan-out 至至少两个 Worker 并在 fan-in 后形成统一答复。
3. 任意 Tool 都不能依靠请求体中的 `user_id` 完成授权。
4. Worker 失败、超时或证据不足时，系统能返回明确的部分答复或澄清问题。
5. 最终答复中的事实都能追溯到 `Evidence.source_ref`。
6. 用户无法从 SSE、API 响应或错误中得到 Prompt、Thought、日志、Trace 或内部治理规则。
7. 图节点、Tool 和 Go 接口均有独立测试，核心场景进入固定回归评测集。

## 15. 参考依据

- LangGraph 的 orchestrator-worker 与动态 `Send`：<https://docs.langchain.com/oss/python/langgraph/workflows-agents>
- LangGraph persistence / checkpointer：<https://docs.langchain.com/oss/python/langgraph/persistence>
- LangChain structured output：<https://docs.langchain.com/oss/python/langchain/structured-output>
- FastAPI async：<https://fastapi.tiangolo.com/async/>
- FastAPI dependencies：<https://fastapi.tiangolo.com/tutorial/dependencies/>
