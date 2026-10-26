# 用户端智能客服 Agent：V1 接口文档

状态：**Draft for freeze**
版本：`v1`
日期：2026-09-28
机器可读契约：[`contracts/support-agent-v1.openapi.yaml`](../contracts/support-agent-v1.openapi.yaml)

## 1. 接口设计原则

- 用户侧只有一个业务入口：`POST /api/v1/support/chat`。
- Python 调用 Go 只有一个业务入口：`POST /internal/v1/support/query`。
- 不按问题类型、Worker、微服务或处理阶段拆分接口。
- 首次提问和多轮追问使用同一 Chat 接口。
- 普通 JSON 与 SSE 使用同一路径，通过 `Accept` 协商。
- 用户身份来自认证令牌，不允许在请求正文中指定。
- Agent 决定调用哪个 Capability；服务端策略决定该调用是否被允许执行。
- 接口仅传递结果、证据和安全进度，不传递 Chain of Thought。

2026-10-05 产品范围更新：正式小栖仅做使用向导，知识检索用于解释操作方法；
站内内容检索、分析或创作等任务转到站内 AI 助手继续尝试。纯转交仍使用
`unsupported`，混合请求使用 `partial`，运行时生成
`suggested_actions: ["open_site_ai_assistant"]`。前端将该固定标识映射 `/agent`，
由用户自行点击；不自动传送问题或凭据，不承诺另一助手具备全部能力。
无转交目标或向导故障时不生成此 action。规则见
[`guide-role-v1.md`](guide-role-v1.md)，旧业务调查示例为历史协议说明。

## 2. 通用约定

### 2.1 Content Type

请求：

```http
Content-Type: application/json
```

普通响应：

```http
Accept: application/json
```

流式响应：

```http
Accept: text/event-stream
```

错误响应：

```http
Content-Type: application/problem+json
```

### 2.2 ID

| 字段 | 生成方 | 含义 |
|---|---|---|
| `request_id` | API 层 | 一次 HTTP 请求 |
| `conversation_id` | 客户端复用、服务端首次生成 | 一段多轮会话 |
| `case_id` | Runtime | 一次用户问题调查 |
| `task_id` | Planner | 一个动态调查任务 |
| `tool_call_id` | Worker Runtime | 一次 Capability 调用 |
| `trace_id` | OpenTelemetry | 跨服务追踪 |

对外 ID 使用 UUID 字符串；`trace_id` 遵循 W3C Trace Context。

### 2.3 时间

- 使用 RFC 3339 UTC 字符串，例如 `2026-09-28T06:30:00Z`。
- 客户端时区只用于理解“昨天”等自然语言，不改变服务端存储时间。

### 2.4 幂等

客户端应为 Chat 请求发送：

```http
Idempotency-Key: <uuid>
```

同一认证主体、同一路径和相同 key 在有效期内重复请求，服务端返回同一 case 的结果或当前执行状态。相同 key 对应不同正文返回 `409 idempotency_conflict`。

### 2.5 错误格式

所有非 SSE 错误遵循 RFC 7807：

```json
{
  "type": "https://errors.example.com/support/invalid-request",
  "title": "Invalid request",
  "status": 422,
  "code": "invalid_request",
  "detail": "message must not be empty",
  "request_id": "019...",
  "trace_id": "4bf92f...",
  "errors": [
    {"field": "message", "reason": "required"}
  ]
}
```

不得在 `detail` 中返回 Prompt、模型原始输出、异常栈、内部地址或服务凭证。

## 3. 用户接口：Chat

### 3.1 请求

```http
POST /api/v1/support/chat
Authorization: Bearer <user-access-token>
Idempotency-Key: 5c3d34a3-c7a4-4aef-bac4-e395a7b1fdb0
Content-Type: application/json
Accept: application/json
```

```json
{
  "conversation_id": null,
  "message": "为什么我昨天发的内容别人看不到？",
  "client_timezone": "Asia/Shanghai",
  "context": {
    "current_page": "/home",
    "selected_resource_id": "optional-opaque-id"
  }
}
```

字段：

| 字段 | 类型 | 必填 | 约束 | 说明 |
|---|---|---:|---|---|
| `conversation_id` | UUID/null | 否 | 已存在且属于当前用户 | 首次请求为空，后续复用响应中的值 |
| `message` | string | 是 | 1～8000 字符 | 用户自然语言问题 |
| `client_timezone` | string | 否 | IANA 时区，默认 UTC | 用于解释相对时间 |
| `context` | object | 否 | 序列化后不超过 4 KiB | 客户端提示信息，仅作线索，不能作为授权依据 |

`context` 保持通用，服务端不得根据其中字段建立固定问题路由。任何资源 ID 都必须由 Tool/Go 服务重新校验归属和可见性。

### 3.2 JSON 成功响应

HTTP `200 OK`

```json
{
  "request_id": "0199a440-b575-7000-a7d2-4b4509ca89c3",
  "conversation_id": "0199a440-cb30-7000-9cc9-f8496bac72cc",
  "case_id": "0199a440-dc2e-7000-80a8-4c3c008f7851",
  "status": "answered",
  "answer": "这条内容已经正常发布。目前没有发现可见性限制……",
  "suggested_actions": [
    "让关注你的用户刷新首页后再次查看"
  ],
  "evidence_refs": [
    "ev_01J...",
    "ev_01K..."
  ],
  "created_at": "2026-09-28T06:30:00Z",
  "trace_id": "4bf92f3577b34da6a3ce929d0e0e4736"
}
```

`status`：

| 值 | 含义 |
|---|---|
| `answered` | 原请求全部必要目标有证据支持，聚合门通过且无未处理执行失败 |
| `partial` | 请求尚未完整确认；保留可靠事实/来源，并明确未解决部分 |
| `clarification_required` | 已有授权能力实际缺少必填输入，用户补充后可继续执行；不是能力缺失或故障 |
| `unsupported` | 没有可回答部分，全部目标均在当前授权能力范围外，且无未处理技术失败 |

`evidence_refs` 是安全引用，不是内部日志链接。V1 不向用户返回原始 Evidence payload。

`citations` 是向后兼容的可选数组，默认 `[]`。真实内容查询可返回
`{"id":"tweet:2103856181104619520","kind":"tweet","label":"推文 2103856181104619520","path":"/tweets/2103856181104619520","evidence_ref":"ev_01J..."}`。
引用只由已核验的业务记录确定性生成，不接受模型提供的 URL；ID 必须与路径一致，证据必须属于本轮 `evidence_refs`。前端跳转站内推文详情，权限和内容存在性由详情接口再次检查。演示编号不生成引用。SSE 的 `response.completed` 使用同一响应结构。

### 3.3 SSE 成功响应

请求路径与正文不变，仅设置：

```http
Accept: text/event-stream
```

响应头：

```http
Content-Type: text/event-stream
Cache-Control: no-cache
Connection: keep-alive
X-Accel-Buffering: no
```

每个事件使用统一 envelope：

```text
id: 01J...
event: case.started
data: {"sequence":1,"timestamp":"2026-09-28T06:30:00Z","request_id":"...","conversation_id":"...","case_id":"...","payload":{}}

```

允许的事件：

| event | payload | 说明 |
|---|---|---|
| `case.started` | `{}` | case 已创建 |
| `case.understanding` | `{"message": string}` | 正在理解问题的用户友好状态 |
| `case.investigating` | `{"message": string, "completed": int, "total": int}` | 安全调查进度，不暴露 Worker/Tool |
| `response.delta` | `{"text": string}` | 最终回答增量 |
| `response.completed` | 完整 `SupportResponse` | 最终成功或部分成功 |
| `response.failed` | 安全 Problem 子集 | 流已经建立后的失败 |

示例：

```text
event: case.started
data: {"sequence":1,"timestamp":"2026-09-28T06:30:00Z","request_id":"req-1","conversation_id":"conv-1","case_id":"case-1","payload":{}}

event: case.investigating
data: {"sequence":2,"timestamp":"2026-09-28T06:30:01Z","request_id":"req-1","conversation_id":"conv-1","case_id":"case-1","payload":{"message":"正在核对相关信息","completed":1,"total":2}}

event: response.delta
data: {"sequence":3,"timestamp":"2026-09-28T06:30:02Z","request_id":"req-1","conversation_id":"conv-1","case_id":"case-1","payload":{"text":"经检查，"}}

event: response.completed
data: {"sequence":4,"timestamp":"2026-09-28T06:30:03Z","request_id":"req-1","conversation_id":"conv-1","case_id":"case-1","payload":{"status":"answered","answer":"经检查……","suggested_actions":[],"evidence_refs":["ev_1"],"trace_id":"...","created_at":"2026-09-28T06:30:03Z"}}

```

禁止事件内容：

- 模型 Thought / Chain of Thought。
- System Prompt 或完整上下文。
- Worker 名称、Prompt 和原始结果。
- Capability 参数、原始业务 payload。
- 日志、Trace、异常栈、内部服务名。

### 3.4 Chat 状态码

| HTTP | code | 场景 |
|---:|---|---|
| 200 | — | 完整 JSON 成功或 SSE 建连成功 |
| 400 | `invalid_accept` | 不支持的响应类型 |
| 401 | `unauthorized` | 缺少或无效用户凭证 |
| 403 | `conversation_forbidden` | conversation 不属于当前用户 |
| 409 | `idempotency_conflict` | 相同幂等键对应不同请求 |
| 413 | `request_too_large` | 消息或 context 超限 |
| 422 | `invalid_request` | Schema 校验失败 |
| 429 | `rate_limited` | 用户或服务限流 |
| 503 | `temporarily_unavailable` | Runtime 或关键依赖不可用 |
| 504 | `runtime_timeout` | 超出整次请求硬超时 |

Agent 调查得到 `partial`、`clarification_required` 或 `unsupported` 属于有效业务结果，使用 HTTP 200，而不是技术错误状态码。

原请求目标、逐目标证据评估和执行输入证明仅为内部协议，不新增业务接口或公开
内部推理。复合请求的部分成功不会覆盖知识说明或未支持部分；最终评估失败时可
返回 `partial` 的来源回退。细则见 [`response-review-v1.md`](response-review-v1.md)。
这不改变初始模型/运行时不可用时原有 503/504 错误契约。

## 4. 内部接口：Support Query

该接口仅供 Python support-agent-service 调用，不能暴露给浏览器或公网。

### 4.1 认证与身份

```http
POST /internal/v1/support/query
Authorization: Bearer <short-lived-service-jwt>
traceparent: 00-<trace-id>-<span-id>-01
Content-Type: application/json
```

服务 JWT 至少包含：

```json
{
  "iss": "support-agent-service",
  "aud": "twitter-support-query",
  "sub": "support-agent-service",
  "act": {"user_id": "authenticated-user-id"},
  "iat": 178...,
  "exp": 178...,
  "jti": "unique-token-id"
}
```

- `act.user_id` 由 Python API 的可信认证上下文注入。
- `arguments` 中即使出现 `user_id` 也不能覆盖 `act.user_id`。
- Go 侧必须独立验证 service identity、audience、有效期、jti 和资源权限。

### 4.2 请求

```json
{
  "operation": "content.list_recent",
  "arguments": {
    "time_from": "2026-09-27T00:00:00Z",
    "limit": 5
  },
  "request_context": {
    "case_id": "0199a440-dc2e-7000-80a8-4c3c008f7851",
    "task_id": "task_01J...",
    "tool_call_id": "call_01J..."
  }
}
```

字段：

| 字段 | 类型 | 必填 | 说明 |
|---|---|---:|---|
| `operation` | string | 是 | 注册的原子只读 Capability 名称 |
| `arguments` | object | 是 | 由该 operation 的 JSON Schema 校验 |
| `request_context.case_id` | string | 是 | 调查关联 ID |
| `request_context.task_id` | string | 是 | 动态任务 ID |
| `request_context.tool_call_id` | string | 是 | 本次调用 ID，用于审计和幂等 |

`operation` 命名建议使用稳定的 `<namespace>.<verb_noun>`，但 namespace 只用于组织能力，不代表问题分类或 Worker 路由。

### 4.3 成功响应

HTTP `200 OK`

```json
{
  "request_id": "0199a441-1038-7000-91c1-52e909448afd",
  "operation": "content.list_recent",
  "status": "success",
  "data": {
    "items": []
  },
  "evidence": [
    {
      "evidence_id": "ev_01J...",
      "source_type": "business_api",
      "source_ref": "tweet:opaque-resource-ref",
      "observed_at": "2026-09-28T06:30:01Z",
      "safe_summary": "当前用户在指定时间范围内发布了 1 条内容",
      "attributes": {
        "count": 1
      }
    }
  ],
  "meta": {
    "duration_ms": 24,
    "data_freshness": "live"
  }
}
```

要求：

- `data` 只包含当前 Capability 对 Agent 可见的数据。
- `safe_summary` 可以进入模型上下文和最终证据合并。
- `attributes` 只能包含已批准的结构化字段，不得混入日志或内部错误。
- Evidence 不返回数据库表名、内部服务地址、风控规则、缓存键或 Trace 原文。

### 4.4 内部接口错误

错误同样使用 Problem Details：

```json
{
  "type": "https://errors.example.com/support/operation-forbidden",
  "title": "Operation forbidden",
  "status": 403,
  "code": "operation_forbidden",
  "detail": "The authenticated actor cannot access the requested resource.",
  "request_id": "019...",
  "trace_id": "4bf..."
}
```

| HTTP | code | 是否重试 | 场景 |
|---:|---|---:|---|
| 400 | `invalid_arguments` | 否 | operation 参数不符合 Schema |
| 401 | `invalid_service_identity` | 否 | 内部服务凭证无效 |
| 403 | `operation_forbidden` | 否 | operation 未授权或资源越权 |
| 404 | `operation_not_found` | 否 | 未注册的 operation |
| 404 | `resource_not_found` | 否 | 资源不存在或对当前用户不可见 |
| 409 | `tool_call_conflict` | 否 | tool_call_id 重用但请求不同 |
| 422 | `operation_unprocessable` | 否 | 参数合法但业务上无法处理 |
| 429 | `query_rate_limited` | 是 | 内部查询限流，遵守 Retry-After |
| 502 | `upstream_invalid_response` | 可选 | 下游返回不符合契约 |
| 503 | `query_temporarily_unavailable` | 是 | 下游临时不可用 |
| 504 | `query_timeout` | 是 | 下游超时 |

为了避免枚举攻击，资源不存在与用户不可见时可以统一返回 `resource_not_found`。

### 4.5 重试与幂等

- Python 只对 429、503、504 和明确的网络失败重试。
- 默认最多重试 2 次，并受 Worker 总预算约束。
- `tool_call_id` 在重试时保持不变，Go 侧据此保证幂等审计。
- 4xx 永不由 Executor 自动重试。

## 5. Python 内部 Capability 接口

这不是网络 API，而是 Agent Runtime 的稳定编程边界。

### 5.1 CapabilitySpec

```python
class CapabilitySpec(BaseModel):
    name: str
    description: str
    input_schema: dict[str, Any]
    output_schema: dict[str, Any]
    read_only: bool = True
    user_data_scope: str
    evidence_types: list[str]
    timeout_ms: int = 2000
    enabled: bool = True
```

### 5.2 CapabilityCall

```python
class CapabilityCall(BaseModel):
    name: str
    arguments: dict[str, Any]
    task_id: str
    tool_call_id: str
```

`CapabilityCall` 不含 `actor_user_id`；Executor 从不可被模型修改的 `RuntimeContext` 注入主体。

### 5.3 CapabilityObservation

```python
class CapabilityObservation(BaseModel):
    tool_call_id: str
    capability_name: str
    status: Literal["success", "not_found", "forbidden", "invalid", "temporary_error"]
    data: dict[str, Any] | None
    evidence: list[Evidence]
    error_code: str | None
    retryable: bool = False
```

### 5.4 Registry / Resolver / Executor

```python
class CapabilityRegistry(Protocol):
    def register(self, spec: CapabilitySpec, handler: CapabilityHandler) -> None: ...
    def get(self, name: str) -> RegisteredCapability: ...
    def list_allowed(self, auth: AuthContext) -> list[CapabilitySpec]: ...


class CapabilityResolver(Protocol):
    async def resolve(
        self,
        task: TaskSpec,
        auth: AuthContext,
        limit: int,
    ) -> list[CapabilitySpec]: ...


class CapabilityExecutor(Protocol):
    async def execute(
        self,
        call: CapabilityCall,
        runtime: RuntimeContext,
    ) -> CapabilityObservation: ...
```

职责划分：

- Registry 回答“系统有哪些能力”。
- Resolver 回答“哪些能力可能与当前任务有关”，但不替 Agent 决定最终调用顺序。
- Worker Agent 决定“现在调用哪个能力以及是否继续”。
- Executor 回答“这次调用是否被允许，并安全执行”。

## 6. Evidence 接口

```python
class Evidence(BaseModel):
    evidence_id: str
    source_type: Literal["business_api", "knowledge", "platform_status"]
    source_ref: str
    observed_at: datetime
    safe_summary: str
    attributes: dict[str, JsonValue] = Field(default_factory=dict)
```

规则：

- `safe_summary` 是唯一默认可进入最终 Composer 的自然语言证据。
- `source_ref` 使用不泄露内部结构的稳定引用。
- `attributes` 必须经过字段 allowlist，不允许任意下游 payload 透传。
- 相互冲突的 Evidence 不覆盖，Aggregator 必须保留并标记 contradiction。
- 用户响应默认只返回 `evidence_id`，不返回内部 Evidence 明细。

## 7. 版本兼容规则

2026-10-04 内部增量：`TaskSpec.resolves_gap_ids`（默认空）用于补充任务绑定开放缺口，
`AggregatedResult` 增加 `unresolved_gaps`、`resolved_gap_ids`；详见
[`gap-resolution-v1.md`](gap-resolution-v1.md)。这些字段不进入 Chat JSON/SSE 用户
响应，不允许请求正文指定，用户 HTTP/OpenAPI 契约不变。

以下修改兼容 v1：

- 新增可选字段。
- 新增 operation。
- 新增不会改变现有语义的错误码。

以下修改需要 v2：

- 删除或重命名字段。
- 改变字段类型、必填性或已有枚举含义。
- 改变认证主体来源。
- 改变 Chat/SSE 或 Query 的基本响应语义。

Capability 的 `output_schema` 自身必须有版本；同名 operation 不允许静默改变既有字段语义。

## 8. 安全检查清单

- 用户接口只信任认证中间件提供的主体。
- 内部接口只接受内部网络和有效 service JWT。
- 请求正文中的资源提示永远不构成授权。
- Planner 和 Worker 只能看到 Registry 过滤后的 Capability。
- Executor 和 Go Handler 都执行权限检查，形成纵深防御。
- 所有 Capability V1 均为只读。
- SSE 与错误响应均经过安全映射。
- 原始 Tool payload 不直接进入用户响应。
- 所有 ID、文本、context 和 arguments 都有大小上限。

## 9. 待环境确认项

这些值不改变接口形状，通过配置确定：

- 用户 JWT 的签发方和 audience。
- 内部 service JWT 的签发与密钥轮换方式。
- 幂等记录 TTL。
- SSE heartbeat 间隔。
- Chat 请求硬超时。
- 每用户限流阈值。
- 对外错误文档的最终域名。
