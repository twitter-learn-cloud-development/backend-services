# Go Agent Runtime 与社交内容智能工作流平台加强计划

> 文档状态：已评审通过；P0-P5 已完成；P6/P7 真实环境验收待执行；P8 统一智能助手主线已启动
> 编写日期：2026-07-14
> 适用范围：`internal/module/agent`、`cmd/agent-service`、Agent Gateway API、工作流前端及相关观测/评测设施
> 执行约束：按 P0-P8 渐进实施，保持现有 API 与业务模式兼容

## 0. 执行摘要

### 0.1 推荐的最终项目定位

面向大厂 Go 后端、AI Infra、Agent 工程化岗位，建议采用下面的目标定位：

> **基于 Go 的可治理 Agent Runtime 与高并发社交内容智能工作流平台**

这个名称比“Twitter Clone + AI”更能体现技术主线，同时保留了项目已有的微服务、Feed、MQ、缓存、检索和云原生能力。社交平台不是背景装饰，而是 Agent Runtime 的真实工具执行环境：Agent 可以在受控权限下检索推文、读取作者内容、编排创作任务，并在人工审批后发布内容。

面试中的目标表达可以是：

> 我在一个 Go 微服务社交系统上构建了可治理的 Agent Runtime。运行时统一负责模型调用、ReAct 决策、MCP 工具执行、上下文组装、预算约束、运行轨迹和安全策略；复杂任务由 DAG 工作流并发编排，并通过 Checkpoint 与 Human Approval 支持中断恢复。RAG 使用 Elasticsearch BM25、Qdrant 向量召回和 RRF/Rerank，业务侧则提供真实的社交内容检索、创作、审核和发布闭环。

这段表述是“目标态”说法。完成 P0-P3 后可以使用“可治理 Agent Runtime”的核心定位；涉及“工作流与 Runtime 已统一、并发状态确定性和双执行后端”的完整表述，必须等 P4 验收后再使用。

### 0.2 当前项目的准确定位

当前更诚实的说法是：

> **基于 Go 微服务的社交内容平台，已具备统一 AgentRunner 最小闭环、可执行 DAG 工作流、受限 ReAct/Plan-Execute、MCP 工具、三级认知 RAG、会话记忆和 Checkpoint 原型。**

目前 `ConsultContent`、`ExecuteWorkflowStrategy`、`AssistPublishTwitter` 均已支持按模式接入统一 `AgentRunner`，Search/Style/Writer/Review 也已收敛为版本化 Agent Profile/Prompt Profile；Token/成本预算、Provider 路由、统一 ToolExecutor、Tool 熔断指标、审批持久化与对账、审批 UI、MCP 内部认证和 TweetService 原生幂等已经落地。Workflow 本地执行器已具备版本化 DSL 编译入口、确定性 IR、只读状态代际、协调器单写并发、内置 Reducer、不可变 Revision、Run 固定版本、周期 Snapshot、恢复时 StateEvent 重放校验、节点状态机、持久补偿、公开只读 Replay、工作流共享预算、过期补偿租约巡检和独立 Journal 人工控制；Temporal 决策门已明确不为用户 DAG 维护双执行后端。Run 查询/控制台、跨实例取消、独立 Run/Step/LLM/Tool Trace、Agent OTel、gRPC/Provider/MCP 传播、有界运行事件流、大型 Tool Result 引用和可检索 Blackboard 已经落地，当前仍缺 Grafana Agent 面板、安全采样/Prompt 模板版本、跨浏览器恢复凭证交接和评测闭环；即使下游幂等已关闭发推重试窗口，也不把整个分布式链路笼统表述为绝对 exactly-once。

### 0.3 本轮审查结论

ChatGPT 给出的主方向正确，但不能原样照搬：

1. 它准确识别了统一 Runtime、Tool Registry、Message Manager、Budget、Trace、Approval、Eval 的价值。
2. 它没有充分识别项目已经实现的 DAG、Checkpoint、MCP 连接复用、RAG 和工作流持久化，若原样新增包会造成第二套引擎和第二套工具注册中心。
3. 它高估了最初完成度。当前 Human Approval 前后端闭环、Token Accounting、Model Router、Tool 级 Prometheus、熔断对账与 TweetService 原生幂等已分阶段补齐；完整 Trace/Replay 和评测仍不是完整闭环。
4. 它建议的“压缩 RAG 文档”与本项目最初的认知架构有冲突。本计划只压缩历史会话和超长工具结果；RAG 召回片段继续按 Token 预算整块装配，不做请求内有损摘要。
5. Temporal 不应成为用户 DAG 的第二套状态机。未注册且重复本地 Scheduler 语义的实验 bridge 已删除；Temporal 仅保留风控和热点后台 Workflow。

## 1. 代码事实基线

### 1.1 已经具备、应当复用的能力

| 能力 | 当前实现位置 | 审查结论 |
|---|---|---|
| JSON DSL 与 DAG 校验 | `workflow/dsl/dsl.go`、`workflow/engine/scheduler.go` | 已支持节点/边、拓扑校验、环检测和未知端点校验，不应重写 |
| 并发 DAG 调度 | `workflow/engine/scheduler.go` | 无依赖节点可并发执行，已有分支跳过和节点超时 |
| Blackboard | `workflow/engine/blackboard.go` | 已有线程安全快照与 Delta 写入，但当前是锁保护的可变状态，不是严格 Immutable/Lock-free |
| 节点 Trace | `workflow/engine/scheduler.go` | 已记录节点状态、起止时间、耗时和错误，但只作为运行输出 JSON 持久化 |
| Checkpoint 与恢复 | `workflow/engine/checkpoint.go`、`service/workflow_service.go` | 已有持久 Checkpoint、一次性 Resume API、审批绑定与 Web 审批收件箱 |
| 工作流/运行持久化 | `repository/dialogue_model.go`、`repository/agent_repo.go` | MongoDB 已保存工作流 DSL、运行、输出和 Checkpoint |
| MCP Server | `mcp/server.go`、`mcp/tools/*` | 当前注册 6 个工具，其中 5 个只读工具已桥接给自定义工作流 |
| MCP 长连接复用 | `service/agent_service.go` | 已有并发安全的 SSE 客户端复用和断线重建 |
| MCP 身份注入 | `service/agent_service.go`、`workflow/guardrails/guardrails.go` | 写工具不直接信任模型提供的 `user_id`，方向正确 |
| 受限 Agent 策略 | `service/workflow_agent_tools.go` | 已支持 ReAct/Plan-Execute，限制 1-8 轮并采用只读工具白名单 |
| 三级认知 RAG | `workflow/rag/router.go`、`workflow/rag/memory.go` | 已有词典/语义/LLM 路由、Persona、Episodic、ES+Qdrant、RRF 和动态评分 |
| 会话持久化 | `repository/agent_repo.go`、`service/agent_service.go` | MongoDB 已保存标准 role/content 消息和多轮历史 |
| 多 Provider LLM 节点 | `workflow/tool/builtin_tools.go` | 工作流节点可配置 LM Studio、DashScope 和 OpenAI-compatible API |
| 单元测试基线 | `internal/module/agent/**/*_test.go` | 2026-07-14 实测 `go test ./internal/module/agent/... ./cmd/agent-service` 通过；Scheduler race 测试通过 |

### 1.2 关键缺口与代码证据

| 缺口 | 代码事实 | 影响 |
|---|---|---|
| Runtime v2 尚未完成生产灰度 | 三个 ReAct 入口均已支持按 Feature Flag 切入统一 Runtime；旧循环仍作为 Legacy 回退且默认配置不切流 | 灰度验证完成前仍需同时维护新旧路径，并比较答案、工具序列、Token、延迟和副作用 |
| `[P3 已修复]` Tool Registry 元数据不足 | 已升级为 `ToolSpec + ToolHandler` 和实例 Registry | 统一表达 read/write/risky、权限、超时、重试、幂等、审批和敏感字段 |
| `[P2 已修复]` Message Manager 缺失 | 已引入统一 Message Builder 与 Token Budget | 保护 ToolCall/ToolResult 配对，并按来源优先级装配上下文 |
| `[P2 已修复]` Token/成本治理缺失 | 已有 Run 级 Token/Cost Budget、Usage 与并发准入 | 完整指标与跨实例共享配额分别归入 P5 和后续生产增强 |
| `[P5 已修复]` Agent Trace 不完整 | 独立 Run/Step/LLM/Tool 记录、租户隔离查询、只读证据回放、模板身份和控制台已落地 | Prompt/Completion 默认只保留哈希与长度；可选预览采样默认关闭并拒绝疑似敏感内容 |
| `[P5 已修复]` Agent Prometheus/OTel 不完整 | Run/Step/LLM/Tool 低基数指标、逻辑 Span、gRPC 与 Provider/MCP HTTP 传播、有界事件流、状态检索和 16 面板 Grafana Dashboard 已落地 | Legacy 路径随 Runtime v2 灰度逐步补齐细粒度信号；长期留存仍属于生产部署增强 |
| `[P3 已修复]` Human Approval 未闭环 | Approval 持久化、Approve/Reject/Resume gRPC/Gateway API 与 Web 审批收件箱已实现 | 已批准审批按最新 Run revision 签发短期轮换授权，不回显旧令牌且支持跨设备恢复 |
| `[P3 已修复]` 审批令牌未校验 | Resume Token 只返回一次，Mongo 保存哈希；恢复校验用户、Run、审批和状态并原子领取 | 并发或重复 Resume 只有一个成功 |
| `[P3 已修复]` 写工具未由策略自动要求审批 | Workflow、Runtime 和 Legacy ReAct 的模型驱动工具调用均进入统一 ToolExecutor；Write/Risky 无审批时返回 `approval_required` | Agent 生成的稳定键已透传到 TweetService 用户级原生幂等契约 |
| `[P2 已修复]` Model Router 不是 Runtime 能力 | Runtime v2 已使用 Catalog Provider/Fallback 路由并记录 Usage/Cost/Pricing Version | Legacy 保留 Feature Flag 回滚；完整路由指标属于 P5 |
| `[P2 已修复]` 模型选择未真正生效 | gRPC `model_kind_id` 已解析并透传到 Chat/Consult/Assist 的 Legacy 与 Runtime 请求 | 未知模型返回 `InvalidArgument`，Embedding 模型不进入 Chat 选择器 |
| 用户 DAG Temporal 伪接入 | 未注册的实验 bridge 已在 P4 第七增量删除 | 不能在面试中宣称自定义工作流由 Temporal 托管 |
| 本地与 Temporal 双引擎重复 | 用户 DAG 只保留本地统一 IR；Temporal 仅承载独立后台 Workflow | 未来若引入 Backend Adapter，必须消费统一 IR，禁止复制状态机 |
| Blackboard 名称与实现不一致 | 当前使用 `sync.RWMutex` 原地更新 map | 不应宣称“Immutable/Lock-free”，应改为单写者事件合并或诚实改名 |
| `[P4 已修复]` Runaway Tracker 未接入 | 未使用且非并发安全的 `WorkflowTracker` 已删除；Scheduler 改用 Runtime `BudgetTracker` | 节点重试、并发模型调用和恢复后的累计额度共享同一线程安全预算快照 |
| 多 Agent 仍是业务硬编码 | `MultiAgentPublishTwitter` 当前按 Search/Style/Reference/Writer 串行执行 | 无法复用 Runtime 策略，也与项目进展文档中的“并发化”描述不一致 |
| Agent 进程职责过载 | `cmd/agent-service/main.go` 同时启动 gRPC、MCP、Temporal Worker、风控 MQ、内存定时任务并直连多种存储 | 任一后台任务故障会影响交互式 Agent，扩缩容和资源隔离困难 |
| 热点播报存在两套调度路径 | 同时启动 Temporal `TrendingReporterWorkflow` 和进程内 `TrendingReporter.Start` | 可能重复生成/发布内容，运行语义和故障恢复方式不一致 |
| Agent 绕过服务边界 | 风控 Activity 直接查询 Tweet 表、Follow Repository 和 Redis 业务 Key，代码日志也标注 `bypass` | Agent 与社交域数据库强耦合，破坏独立演化和最小权限 |
| L2 记忆模型不适合大量用户 | 每个用户一个 `episodic_user_<id>` Qdrant collection | 用户规模扩大后 Collection 管理和资源开销不可控，应使用共享集合 + Payload 过滤 |
| `[P2 已修复]` 记忆结晶粒度偏碎 | 已改为消息阈值 + 空闲 Session 边界的增量摘要，使用版本游标与租约 | 显式 Session End API 和共享 Episodic Collection 仍在后续阶段 |
| `[P2 已修复]` 预算仍按字符而非 Token | Cognitive RAG 与 Message Builder 均接入可注入 TokenCounter | Provider 专用 tokenizer 可继续替换启发式实现 |
| `[P2 已修复]` 用户 API Key 明文进入 DSL | 新 DSL/运行输入拒绝明文 Key；用户级 Provider Config 使用 AES-256-GCM 加密并以 `provider_config_id` 引用 | Legacy `credential_ref` 仅作为环境变量兼容轨道保留，用户凭证不进入 DSL |
| `[P2 已修复]` 自定义 Base URL 缺少网络策略 | 已增加 Scheme/Host/IP、重定向、DNS 复检和 Rebinding 防护 | 本地 Provider 仅对显式本地地址或 allowlist 放行 |
| 工作流选择语义不清 | AI 助手运行自定义工作流时固定选列表第一条 | 用户无法明确知道当前使用哪个工作流，面试演示也不稳定 |

## 2. 对外定位与面试叙事设计

### 2.1 一条主线、两层支撑

主线必须始终是：

> **让 Agent 能被工程化运行、观测、恢复、评估和治理。**

第一层支撑是 Agent Infra：

- AgentRunner / Strategy
- MessageBuilder / Context Budget
- ModelClient / ModelRouter
- ToolRegistry / ToolExecutor / MCP Adapter
- BudgetManager / Guardrail / Approval
- TraceRecorder / Metrics / Replay
- Workflow Engine / Checkpoint
- RAG / Memory / Eval

第二层支撑是现实业务和分布式系统：

- Go + Gin + gRPC 微服务边界
- RabbitMQ 事件驱动和最终一致性
- Redis Feed/缓存/热点治理
- Elasticsearch + Qdrant 混合检索
- MongoDB 会话与运行记录
- Temporal 长任务、Prometheus、Jaeger、Grafana
- Vue 工作流画布与用户操作闭环

面试时不要把所有中间件平铺罗列。先讲一个 Agent Run 如何执行，再讲这些中间件为什么出现在链路里。

### 2.2 三档表达

#### 30 秒版本

> 我做的是一个基于 Go 的可治理 Agent Runtime。它运行在真实社交微服务上，支持 ReAct 和 DAG 两种执行方式，工具通过 MCP 暴露，写操作受权限和人工审批约束。每次运行都有 Token 预算、步骤 Trace 和 Checkpoint，RAG 使用 ES+Qdrant 混合召回。重点不是调用模型，而是控制 Agent 的执行、成本、安全和恢复。

#### 2 分钟版本

1. 业务目标：用户可对话、检索平台内容、生成草稿、编排多 Agent，并在审批后发布。
2. Runtime：LLM 每轮输出 Action，Runner 调模型/工具并写 Observation，受 MaxSteps/Token/Timeout/Cost 约束。
3. Tool：MCP 工具统一注册，Read 自动执行，Write/Risky 必须授权或审批，用户身份由后端强制注入。
4. Workflow：DSL 编译为 DAG，无依赖节点并发，通过 Blackboard Delta 传递结果，节点失败可 Checkpoint 恢复。
5. RAG：Persona 直接注入，Session 结束提炼 Episodic Summary，公共知识走 ES BM25 + Qdrant HNSW + RRF/Rerank。
6. 观测：Run/Step/LLM/Tool 四层 Trace，Prometheus 统计延迟、错误、Token 和预算中止。

#### 深挖版本

准备围绕以下四个真实问题展开：

1. 为什么不能让 LLM 直接传 `user_id`，如何做身份强制覆盖和审批。
2. 为什么历史消息不能全部塞进 Prompt，如何保证 ToolCall/ToolResult 成对并按 Token 分配预算。
3. 为什么 DAG 并发下不能让所有协程直接修改共享状态，如何用单写者合并 Delta。
4. 为什么 RAG 不能只展示“能搜到”，如何用离线数据集和 Recall@K/MRR/NDCG 证明效果。

## 3. 目标架构

### 3.1 运行链路

```text
Gateway Auth Context
        |
        v
Agent Application Service (thin orchestration)
        |
        v
AgentRunner --------------------------------------------------+
  |        |          |          |          |                 |
  v        v          v          v          v                 v
Message  Model      Tool       Budget     Policy          TraceRecorder
Builder  Router     Executor    Manager    Engine          + Metrics
  |        |          |
  |        |          +--> MCP Adapter --> gRPC Services
  |        +--> LM Studio / DashScope / OpenAI-compatible
  +--> Dialogue + Persona + Episodic + RAG + Blackboard
        |
        +--> Action: ToolCall / RAGSearch / AskHuman / FinalAnswer
                         |
                         v
                  Observation -> next step
```

复杂任务由 Workflow Engine 调用同一个 Runtime，而不是再实现一套 Agent：

```text
Workflow DSL -> Validator -> Compiled Graph/IR -> Scheduler
                                                |
                         +----------------------+
                         |                      |
                    Runtime Node            Tool Node
                         |                      |
                         +---- NodeResult/Delta+
                                      |
                               Single-writer Reducer
                                      |
                         State Snapshot + Event Log + Trace
```

### 3.2 模块边界

为了降低迁移风险，不照搬 ChatGPT 建议另建一套 `toolkit` 和 `workflow`，而是在现有目录上演进：

```text
internal/module/agent/
  runtime/                 # 新增：Runner、Action、Observation、Strategy、RunContext
  message/                 # 新增：消息来源、构建、Token 预算、历史压缩
  model/                   # 新增：ModelClient、Usage、Router、Provider Adapter 契约
  trace/                   # 新增：Run/Step/LLM/Tool 记录、Recorder、Metrics
  policy/                  # 新增：预算、权限、审批、Prompt/Tool 安全策略
  eval/                    # 新增：Agent/RAG 数据集、Runner、指标
  workflow/
    dsl/                   # 保留并升级 DSL version、schema、policy
    engine/                # 保留 Scheduler，改造单写者状态与统一节点结果
    tool/                  # 保留并升级为 ToolSpec + ToolExecutor + Adapter
    rag/                   # 保留三级 RAG，升级 Token 预算和共享集合
    temporal/              # 暂时隔离；完成统一 IR 后再接入
  mcp/
    tools/                 # 保留：MCP Server 的业务边界适配
  service/                 # 变薄：参数校验、选择 Profile/Workflow、调用 Runner
  repository/              # 扩展：Trace、Approval、Prompt/Profile、Eval 记录
```

进程部署边界也需要收口。目标不是立刻拆出更多微服务，而是先把同一代码模块按运行角色隔离：

```text
agent-api       # gRPC/HTTP 交互请求、AgentRunner、短工作流
agent-worker    # Temporal Activity、长任务、异步记忆结晶、Eval
agent-mcp       # 内部 MCP Server，可按负载独立扩容
```

三种角色可以先由同一个镜像通过启动参数运行，等负载和故障隔离需求明确后再拆镜像。热点播报只能保留一个调度源，推荐保留 Temporal 版本；Agent 不再以 `bypass` 方式直接操作 Tweet/Follow 数据，而应调用领域服务或消费授权的只读投影。

### 3.3 核心接口草案

接口只约束依赖方向，实施阶段可以调整字段，但不允许让 Runtime 直接依赖具体 OpenAI/MCP/Mongo 客户端。

```go
type AgentRunner interface {
    Run(ctx context.Context, req RunRequest) (RunResult, error)
    Resume(ctx context.Context, req ResumeRequest) (RunResult, error)
}

type Action struct {
    Type      ActionType // tool_call / rag_search / ask_human / final_answer
    Name      string
    Arguments json.RawMessage
    Content   string
}

type ModelClient interface {
    Complete(ctx context.Context, req ModelRequest) (ModelResponse, error)
}

type ModelResponse struct {
    Message Message
    Actions []Action
    Usage   TokenUsage
    Model   string
    Provider string
}

type ToolSpec struct {
    Name           string
    Description    string
    InputSchema    json.RawMessage
    Category       ToolCategory // read / write / risky / internal
    Permission     string
    Timeout        time.Duration
    RetryPolicy    RetryPolicy
    Idempotent     bool
    ApprovalPolicy ApprovalPolicy
}

type ToolExecutor interface {
    Execute(ctx context.Context, call ToolCall) (ToolResult, error)
}

type Budget struct {
    MaxSteps       int
    MaxInputTokens int
    MaxOutputTokens int
    MaxTotalTokens int
    MaxCostMicros  int64
    Deadline       time.Time
}
```

## 4. 分阶段实施计划

### P0：真实性基线与迁移护栏

目标：先固定事实、契约和回滚方式，防止一次性重构破坏现有功能。

预计工作量：1-2 天。

任务：

- [x] 新增 ADR：`docs/adr/ADR-001-agent-runtime-boundary.md`，明确 Runtime、Workflow、MCP、RAG 和 Service 的职责。
- [x] 为 Chat/Consult/Assist/Multi/Workflow 五种入口建立行为清单和冒烟用例。
- [x] 修正 `docs/PROJECT_PROGRESS.md` 中与当前代码不一致的表述，例如多 Agent 并发、Temporal 自定义工作流托管和 Agent 可观测性完成度。
- [x] 增加 Feature Flag：`AGENT_RUNTIME_V2_MODES`，允许按模式逐个切换新旧执行路径。
- [x] 确认旧 API、Proto 和 Mongo 集合保持兼容；新增字段只追加、不重命名、不删除。
- [x] 建立依赖反转测试：Runtime 单元测试不能启动 MongoDB、Redis、MCP、Temporal 或真实模型。

P0 实施记录（2026-07-14）：

- 边界决策见 [`docs/adr/ADR-001-agent-runtime-boundary.md`](./adr/ADR-001-agent-runtime-boundary.md)。
- 五入口基线见 [`docs/agent/AGENT_ENTRY_BEHAVIOR_BASELINE.md`](./agent/AGENT_ENTRY_BEHAVIOR_BASELINE.md)。
- 灰度配置实现于 `internal/module/agent/runtime/rollout.go`，默认所有入口保持旧路径。
- Compatibility Contract Test 已锁定关键 gRPC 方法、Proto 字段号、DialogueMode、Mongo 集合及 BSON 字段。

验收标准：

- 五种现有入口的输入、输出、持久化和错误行为有测试或清单基线。
- 新 Runtime 尚未接业务时，所有现有测试仍通过。
- 任意模式可以通过配置切回旧实现。

面试价值：展示渐进式重构、兼容迁移和风险控制，而不是“推倒重写”。

### P1：统一 AgentRunner 最小闭环

目标：把三套 ReAct 循环收敛为一个可测试、可扩展的 AgentRunner。

预计工作量：3-5 天。

任务：

- [x] 新增 `runtime` 包，定义 `RunRequest`、`RunContext`、`Action`、`Observation`、`Step`、`RunResult` 和错误类型。
- [x] 实现 `ReActRunner`：模型决策 -> Action 校验 -> 工具/RAG 执行 -> Observation 回写 -> 下一轮。
- [x] 支持 `FinalAnswer`、`ToolCall`、`RAGSearch`、`AskHuman` 四类 Action。
- [x] 先实现 `MaxSteps`、Run Timeout 和 Context Cancellation，避免无限循环和协程泄漏。
- [x] 为 OpenAI-compatible Chat Completion 实现 `ModelClient` Adapter，不让 Runtime 直接依赖 `go-openai` 类型。
- [x] 为现有 MCP 长连接实现 `ToolExecutor` Adapter，复用 `getOrInitMCPClient` 和身份注入逻辑。
- [x] 迁移 `ConsultContent` 到新 Runner，并保留按模式回滚的旧路径。
- [x] 迁移 `ExecuteWorkflowStrategy` 和 `AssistPublishTwitter` 到新 Runner。
- [x] 将 Search/Style/Writer/Review 改为 Agent Profile/Prompt Profile，避免四套执行器。

必须覆盖的测试：

- [x] 模型直接返回 FinalAnswer。
- [x] 一次 ToolCall 后返回 FinalAnswer。
- [x] 同一轮多个 ToolCall，结果按 ToolCallID 成对回填。
- [x] 非法 JSON 参数、未知工具、工具错误和空模型响应。
- [x] 超出 MaxSteps、Run Timeout、上游 Context 取消。
- [x] 模型请求写工具时返回 ApprovalRequired，而不是直接执行。
- [x] Fake Model + Fake Tool 完成离线最小闭环。

验收标准：

- [x] `ConsultContent` 公共入口不再持有自己的 ReAct for-loop。
- [x] Runtime 核心测试在 2 秒内完成，不依赖外部中间件。
- [x] `go test -race ./internal/module/agent/runtime/...` 通过。
- [x] 旧 Consult 路径仍可通过 Feature Flag 回滚。
- [x] Workflow Strategy 保留 `model`、`max_tokens`、`max_iterations`、`allowed_tools` 与 `tool_trace` 契约。
- [x] Assist Runtime 不暴露写工具，发布仍通过独立确认接口完成。
- [x] Assist 与 Workflow Strategy 均可按模式切回 Legacy 实现。

P1 第一增量实施记录（2026-07-14）：

- 统一类型、错误模型与执行循环位于 `internal/module/agent/runtime`；Runtime 只依赖本包接口，不导入 Provider、MCP 或存储 SDK。
- OpenAI-compatible 边界适配器位于 `internal/module/agent/model/openai_compatible.go`。
- MCP ToolExecutor、工具风险分类和历史消息转换位于 `internal/module/agent/service/runtime_adapters.go`；未知工具按 risky 处理，写工具默认要求审批。
- `ConsultContent` 通过 `AGENT_RUNTIME_V2_MODES=consult` 切入新 Runner；配置为空时继续执行 `consultContentLegacy`。
- 第一增量结束时 P1 保持进行中，Strategy/Assist/Profile 在第二增量完成。

P1 第二增量实施记录（2026-07-14）：

- `RunRequest`/`ModelRequest` 新增请求级模型提示，OpenAI-compatible Adapter 优先使用该值并回退进程默认模型；这只保留已有工作流节点能力，不提前承担 P2 的 Provider/Credential 路由。
- 新增 `internal/module/agent/profile`，将 Agent Profile 与 Prompt Profile 从 Runtime 解耦；Search/Style/Writer/Review 使用版本化 Profile，Review 作为 Writer 的审校策略组合，不增加额外模型调用。
- 新增可注入 `RuntimeToolCatalog`，Runtime 入口不再直接依赖 MCP 工具发现；默认实现仍复用 MCP 长连接，测试可完全离线。
- `AssistPublishTwitter` 通过 `AGENT_RUNTIME_V2_MODES=assist` 切入 Runtime，只暴露只读 MCP 工具；`ConfirmPublishTwitter` 继续承担显式发布。
- `ExecuteWorkflowStrategy` 通过 `AGENT_RUNTIME_V2_MODES=workflow` 切入 Runtime；只迁移 ReAct/Plan-Execute 策略节点，不替换现有 DAG Scheduler。
- P1 已完成；生产环境仍默认 Legacy，待真实模型/MCP 对照冒烟后再扩大灰度。

面试价值：这是从“AI 功能”升级为“Agent Runtime”的第一证据。

### P2：Message Builder、Token 计量与模型路由

目标：让上下文和成本受统一规则管理，而不是由各业务模式手工拼 Prompt。

预计工作量：4-6 天。

任务：

- [x] 新增 `message` 包，统一接收 System、Developer、当前输入、最近会话、长期画像、Episodic、RAG、Tool Result、Blackboard 和 Policy。
- [x] 定义消息优先级：System/Policy、当前用户输入、ToolCall 对、最近会话、相关记忆、RAG。
- [x] 强制 ToolCall 与 ToolResult 成对保留，禁止裁剪出孤立工具结果。
- [x] 引入 TokenCounter 接口。优先使用 Provider 返回 Usage；请求前使用可替换估算器做预算预检。
- [x] 把字符预算迁移为 Token Budget，分别配置 History、Memory、RAG、Tool Result 和 Output 桶。
- [x] 只压缩历史会话与超长工具结果；RAG Chunk 按最终分数整块装配，放不下则跳过，不做请求内摘要截断。
- [x] 实现 Session Summary：会话超过阈值或结束时结晶一次，替代当前每个长回合都写 Episodic Memory。
- [x] 新增 `model` 包，统一 Provider、Model、Context Window、Pricing Version、Fallback 和 Capability。
- [x] 修复 `model_kind_id` 未实际参与模型选择的问题。
- [x] 把用户自定义 API 改为 Credential Reference；DSL 只保存 `provider_config_id`，不保存明文 Key。
- [x] 自定义 Base URL 增加 Scheme、Host、私网地址和重定向校验，阻止 SSRF。

Budget 规则：

- [x] Run 最大步骤数。
- [x] 单次模型最大输入/输出 Token。
- [x] Run 最大总 Token。
- [x] Run 最大估算成本。
- [x] 用户/工作流并发上限。
- [x] 超预算在调用前终止，返回结构化 `BudgetExceeded`。
- [x] Provider 未返回 Usage 时标记 `estimated=true`，不能把估算值当精确账单。

验收标准：

- 长对话下 System、当前输入和 ToolCall 对永不丢失。
- 任意一次模型调用都能归属到 Run/Step，并产生 Usage 或明确的 estimated 标记。
- 前端所选模型和后端实际请求模型一致。
- MongoDB 中不再出现新增的明文 API Key。
- 预算单元测试覆盖边界值、并发预留和调用失败回滚。

面试价值：可讲 Prompt 工程之外的上下文治理、成本治理和 Provider 抽象。

P2 第一、第二增量实施记录（2026-07-14）：

- 新增 `internal/module/agent/message`：按优先级和独立 Token 桶装配消息，System/Developer/Policy 与当前输入为强制上下文；ToolCall/ToolResult 作为原子单元保留；只允许压缩历史和工具结果，RAG Chunk 放不下时整块跳过。
- Runtime 增加可替换 `TokenCounter`、输入/输出/Run 总 Token 预算和结构化 `budget_exceeded`；调用前为输出预留额度，Provider 缺少 Usage 时回填保守估算并标记 `Estimated`，Provider 精确 Usage 优先。
- 新增只读 Model Catalog 基础类型，包含 Provider、Context Window、Pricing、Fallback 与 Capability；Catalog 尚未接管 Provider Client 路由，因此对应总任务仍保持未完成。
- `model_kind_id` 已在 gRPC 边界解析为具体 Chat 模型，并通过 Context 进入 Chat、Consult、Assist 的 Legacy/Runtime 请求；未知 ID 返回 `InvalidArgument`。
- Workflow DSL 与运行输入拒绝任何非空 `api_key`/`apiKey`，旧 DSL 对外读取时自动脱敏；LLM Tool 支持环境变量兼容用的 `credential_ref`。用户级 `provider_config_id`、管理 API 与加密存储已在 P2 第四增量完成。
- Base URL Policy 校验 Scheme、Host、用户信息、Query、Fragment、私网/链路本地地址和内部域名；HTTP Client 禁止重定向，并在 DNS 解析后复检 IP、固定到已批准地址后连接，降低 DNS Rebinding 风险。LM Studio 的 localhost/host.docker.internal 仅在本地 Provider 或显式 allowlist 下放行。
- Runtime v2 Consult/Assist 已使用 Message Builder；Legacy 仍由 `AGENT_RUNTIME_V2_MODES` 保留回滚能力。成本/并发预算、Catalog Provider 路由和 Provider Config CRUD 已在 P2 第四增量完成。

P2 第三增量实施记录（2026-07-14）：

- `workflow/rag.MemoryManager` 不再使用字符/rune 预算；Persona 与召回 Chunk 统一通过可注入 `TokenCounter` 计量，Persona 可按 Token 截断，RAG/Memory Chunk 仍整块装配，超预算时跳过并只返回实际入选 Chunk。
- 移除直接聊天模式中“每个长回合立即写一次 Episodic Memory”的逻辑；所有会话模式在消息对持久化后进入统一 Session Summary 调度器。
- 长会话达到 12 条未结晶消息时立即生成增量摘要；短会话连续空闲 2 分钟后视为本次 Session 边界并强制检查。服务关闭和删除对话会同时取消尚未触发的 Timer 与在途模型/Embedding Job。
- MongoDB Dialogue 增加 `summary_version`、`summarized_message_count` 和内部租约字段；摘要任务通过原子租约声明 `[from, through)` 消息区间，成功后推进游标，失败释放租约，过期任务可被接管。

P2 第四增量实施记录（2026-07-15）：

- Runtime `Budget` 增加 `MaxEstimatedCostMicros`，价格和累计金额全程使用整数微单位。模型调用前按显式 Fallback 链中最贵候选预留输入/输出成本，完成后记录真实模型的价格版本和 `cost_estimated` 标记。
- 新增可注入 `AdmissionController` 与进程内用户/工作流双维度并发限制；租约支持工作流内部 Runtime 重入，所有成功、模型失败、预算失败和取消路径统一释放。共享 Redis 配额实现可在不修改 Runner 的前提下替换。
- Model Catalog 接管 Runtime v2 的 Provider Client 路由：能力匹配、输出上限和失败回退都只沿 DSL 外部声明的显式 Fallback 图执行；Legacy 路径继续保留为 Feature Flag 回滚轨道。
- 新增用户级 Provider Config CRUD。API Key 使用 AES-256-GCM 与 `user_id/config_id/provider` AAD 加密，MongoDB 只保存 `key_id/nonce/ciphertext`；支持凭据版本轮换、主密钥 Keyring 解密和撤销时清除密文。
- Workflow LLM 节点新增 `provider_config_id`，执行时通过认证上下文强制注入 `user_id` 并按所有权解密；配置引用覆盖节点中的 Provider/Base URL/Model，避免 Prompt 或 DSL 篡改调用目标。Gateway REST 与前端 API/选择器已接入，明文 `api_key` 仍被 DSL/运行输入校验拒绝。
- Qdrant Point ID 由 Dialogue ID 与 Summary Version 稳定生成；重试覆盖同一 Point。Payload 新增 `memory_type`、`facts`、`preferences`、`decisions`、`followups`、`source_dialogue` 与 `summary_version`。
- 摘要模型优先输出结构化 JSON；无法解析时降级保存纯文本摘要，保证旧模型和本地模型仍可使用。

### P3：工具治理与 Human Approval 闭环

目标：所有工具通过同一执行边界，写操作默认受审批、权限、幂等和审计约束。

预计工作量：4-6 天。

任务：

- [x] 扩展现有 `workflow/tool`，不要新增第二套 Registry。
- [x] 把 package 全局单例 Registry 改为依赖注入的实例，避免测试之间污染和隐藏初始化顺序；现有工具名保持兼容。
- [x] 将 `AgentTool` 升级为 `ToolSpec + ToolHandler`，Spec 包含 category、permission、timeout、retry、idempotent、approval policy 和敏感字段规则。
- [x] 新增统一 `ToolExecutor`，集中处理 Schema 校验、身份注入、权限、超时、重试、熔断、审计和错误分类。
- [x] Read 工具可自动执行；Write 默认审批；Risky 必须审批；Internal 只能由 Runtime 调用。
- [x] 发布工具要求幂等键，审批恢复或网络重试不得重复发推；Executor 结果回放与 TweetService 用户级唯一键共同关闭远端成功、本地结果落库前崩溃的重复发推窗口。
- [x] 新增 `ApprovalRequest` 持久化模型：run_id、step_id、tool、脱敏参数、原因、过期时间、状态、审批人和版本号。
- [x] 增加 Proto/Gateway API：审批由 Gate 自动创建，并支持查询、批准、拒绝和恢复 Run。
- [x] 校验 `resume_token`、用户归属、Run 状态和乐观锁版本，保证一次性恢复。
- [x] 前端增加审批收件箱和 Run 挂起详情；拒绝时可填写原因并终止对应 Run。
- [x] 即使画布未放 Wait 节点，Runtime/Workflow 请求写工具也会返回 `ApprovalRequired`，且不会调用真实 Handler。
- [x] MCP Server 只绑定回环地址；SSE/消息入口校验 Bearer Token，服务端工具中间件再次校验认证上下文。

验收标准：

- 未认证、跨用户、过期、重复审批全部拒绝。
- 任何 Agent/Profile/Workflow 都不能绕过审批直接发布。
- 审批后只执行一次写工具；重复 Resume 返回原结果或幂等冲突。
- 工具日志默认脱敏 content 中的密钥、Token、Cookie 和个人敏感数据。

面试价值：这是 Agent 安全治理最强的业务证据，远比只做 Prompt Injection 关键词检测更可信。

P3 第一增量实施记录（2026-07-15）：

- 新增强类型 `ToolSpec + ToolHandler`、实例化 `ToolRegistry` 和统一 `Executor`；JSON Schema 在注册时编译，动态 MCP Schema 在 Executor 内并发缓存。
- Executor 统一处理认证身份覆盖、权限、节点超时、保守重试、错误分类、审批 Gate、幂等键要求和敏感参数审计；查询、Prompt、正文、Token、Cookie 与密钥默认脱敏。
- Workflow ToolNode、Runtime MCP、Legacy Consult/Assist ReAct、Multi-Agent 只读检索和 Legacy Workflow Strategy 均接入该边界；`ConfirmPublishTwitter` 作为用户显式确认接口保留，不属于模型自主工具调用。
- 全局 `GetRegistry` 已移除，生产 Composition Root 显式构造 Registry/Executor；静态 DSL 保存校验会拒绝未注册工具。
- `PublishTweet` 与 MCP `create_tweet` 无审批时 fail-closed；工作流即使不放 Wait 节点也不会调用 TweetService。持久结果领取/回放和一次性 Approval Resume 已在第二增量完成。

P3 第二增量实施记录（2026-07-15）：

- 新增 Mongo `agent_tool_approvals` 与 `agent_tool_executions`，分别保存审批状态机和幂等执行领取/结果；唯一索引约束调用身份，审批列表只返回脱敏参数。
- `PersistentApprovalGate` 使用 `pending -> approved/rejected/expired -> executing -> consumed` 状态机和执行租约；审批决策使用 `revision` 乐观锁，失败执行会释放批准状态。
- Write/Risky 工具首次命中 Gate 时自动把 DAG Run 挂起；Checkpoint 使用 `retry_current_node` 区分 Wait 完成与批准后重试工具，修复了旧恢复逻辑会跳过写节点的问题。
- 每次挂起生成 256-bit 随机恢复令牌，Mongo 只保存 SHA-256 哈希；恢复同时校验用户、Run、审批、批准状态并原子领取，重复/并发恢复只有一个成功。
- 新增审批列表、批准/拒绝和恢复 Run 的 gRPC/Gateway API，以及 Web API 封装；审批收件箱在第三增量完成。
- Executor 已接入持久化幂等领取和成功结果回放，可阻断并发重复执行与已落库结果的网络重放；跨 TweetService 的崩溃窗口在第三增量通过下游原生幂等契约关闭。

P3 第三增量实施记录（2026-07-15）：

- Tweet `CreateTweetRequest` 追加兼容字段 `idempotency_key = 7`；Agent Workflow、Legacy MCP 和 Temporal 发布 Activity 将受信任的执行键透传到 TweetService，模型输入不能覆盖该键。
- MySQL 新增用户级唯一幂等记录；Tweet、Poll、Outbox Event 与幂等绑定共享同一 Unit of Work。相同键/相同请求回放原 Tweet，相同键/不同请求拒绝，兼容旧调用方不传键的行为。
- 修复 Tweet/Poll Repository 未使用事务 Context，以及 TweetService 事务失败仍返回成功的旧缺陷；补充事务失败、回放、摘要冲突和 Proto 字段号契约测试。
- Web AI 助手新增审批收件箱、状态筛选、脱敏参数、挂起 Run 详情、批准/拒绝/继续执行和待处理数量。2026-07-19 已改为批准后签发短期、单次、可轮换 Grant 并立即恢复，不再把明文恢复凭证写入浏览器存储。
- P3 第四增量已补齐 Tool 熔断器、低基数指标、审批/执行对账任务及 MCP 内部认证；完整 Trace/Replay 按计划留在 P5。

P3 第四增量实施记录（2026-07-15）：

- 新增按工具隔离的 `closed/open/half_open` 熔断状态机；幂等结果回放绕过熔断，新执行在领取后检查，失败会释放审批和执行租约。调用方主动取消不计为下游故障。
- 新增 Tool 执行次数、耗时、处理器尝试次数、熔断状态和治理对账 Prometheus 指标；标签只使用工具、类别、来源、决策、错误码、状态及固定动作，不包含用户、Run、输入或错误文本。
- Mongo 对账任务周期处理过期审批、失效审批执行租约和失效幂等执行租约；审批过期会终止对应挂起 Run 并清除恢复凭证，避免永久悬挂。

P3 第五增量安全收口（2026-07-19）：

- 新增独立 `issueWorkflowResumeGrant` RPC 与 `POST /api/v1/agent/tool-approvals/:id/resume-grant`：仅审批所有者可为已批准、未过期且仍挂起的绑定 Run 按 revision 签发授权。
- 授权默认 5 分钟并受审批过期时间封顶；每次签发原子轮换 Run 中的 SHA-256 哈希，旧挂起令牌和旧 Grant 立即失效，恢复仍通过条件更新单次领取。
- Web 在内存中签发后立即恢复，删除无读取方的 `sessionStorage` 明文写入；已批准列表支持网络失败后重新签发。所有含令牌响应使用 `no-store`，兼容旧 Run 中没有 Grant 过期字段的恢复记录。
- MCP 默认绑定 `127.0.0.1`，拒绝非回环地址；进程内生成或读取 256-bit 级共享令牌，客户端自动携带 Bearer Header，HTTP 边界和 Tool Middleware 双重校验。
- 全仓 `go test ./...`、受影响 Agent 包 `go test -race`、Agent/Cmd `go vet` 和定向 `git diff --check` 通过；全仓 `go vet ./...` 仍命中既有 Auth 不可达代码，记录于 `docs/ISSUES.md`。

### P4：Workflow Engine 语义收口与可靠执行

目标：让工作流真正复用 Runtime，收口本地 Scheduler、Blackboard、持久恢复和补偿语义，并通过 Temporal 决策门消除双引擎分叉。

预计工作量：5-8 天。

任务：

- [x] DSL 升级为 versioned schema：`dsl_version`、workflow_version、node input/output schema、retry、policy、profile_ref、provider_ref。
- [x] 保存工作流时创建不可变 Revision；Run 固定引用 revision，后续编辑不影响历史运行。
- [x] 新增 Compile 阶段并让本地 Scheduler 消费确定性 IR；Temporal 决策门最终选择删除用户 DAG 实验 bridge。
- [ ] LLM/Agent 节点统一调用 AgentRunner，Tool 节点统一调用 ToolExecutor。
- [x] 将 Blackboard 改为“节点拿只读快照，执行返回 Delta，协调器单写合并”。协程不得直接写共享状态。
- [x] 在 Run 完成或挂起边界幂等保存 append-only StateEvent；Checkpoint 与 Run 记录 event sequence/state_version。
- [x] 增加执行中周期 Snapshot 与事件重放/校验入口，缩短长流程恢复时间。
- [x] 本地执行器按 DSL 声明顺序确定性合并并发波次；同路径并发写必须声明一致 Reducer，并由协调器确定性归约。
- [x] 节点支持 retry/backoff/deterministic-jitter、timeout、cancel、branch-skip、suspend 的明确状态机；Trace 记录尝试次数。
- [x] 增加 durable compensation journal、反向执行顺序、Tool Policy/Approval、独立幂等键与恢复语义；禁止进程内临时回调冒充可靠补偿。
- [x] Scheduler 发生节点错误后等待已启动协程退出，不能直接返回并遗留后台执行。
- [x] 接入实际 Runaway/Budget 检查，删除未使用的 `WorkflowTracker` 或让其成为 Runtime Budget 的一部分。
- [ ] 增加 SubWorkflow 和 Aggregator 前，先完成 IR、Revision 和 Reducer；不要只堆前端组件。
- [x] 自定义工作流前端允许用户选择具体 Workflow/Revision，不再默认取第一条。
- [x] 交互 API、MCP Server 和后台 Worker 已通过 `api/worker/all` 运行角色隔离；默认 `all` 保持单进程部署兼容。
- [ ] 将 `cmd/agent-service/main.go` 的 API/Worker 依赖装配继续拆成窄 Composition Root，并落实为独立 Kubernetes Deployment。
- [x] 删除生产组合根的双热点播报路径，只保留 Temporal 调度源；显式 `disabled` 可停播，Temporal 不可用时不回退到进程内 Ticker。
- [x] 风控和舆情任务通过 Tweet gRPC 与 ES/Qdrant/Redis 授权只读投影访问领域数据，不再直连 Tweet 表、Follow Repository 或 Timeline 业务 Key。

Temporal 决策门：

- [x] P4 前半段不启用当前 bridge。
- [x] 统一 IR、ToolExecutor 和持久补偿完成后评估：用户交互式 DAG 暂不增加 Temporal Backend，避免维护第二套状态机。
- [ ] 若保留 Temporal：注册真实 Workflow/Activities，认证信息通过显式签名载荷传播，审批使用 Signal/Update，禁止复制一套拓扑逻辑。
- [x] 暂不保留用户 DAG Temporal Backend：删除重复拓扑、路由、黑板和重试语义的实验 bridge；Temporal 只保留当前真实注册的风控和舆情定时 Workflow。

验收标准：

- 同一 DSL 在本地执行器上的分支、Join、失败和恢复结果确定一致。
- 并发节点写同一字段时必须在编译期拒绝或通过 reducer 合并。
- Checkpoint 恢复不重跑已成功的非幂等节点。
- `go test -race`、goroutine leak test、取消/超时测试通过。
- Workflow Revision 可以重放和审计。

面试价值：可讲 Go 并发、DAG 调度、状态机、确定性、幂等和恢复，不再只是“前端画线”。

P4 第一增量实施记录（2026-07-15）：

- DSL 新增 `dsl_version/workflow_version` 及节点 Schema、Retry、Policy、Profile/Provider Reference 字段；旧 DSL 缺省按 v1 编译，新保存内容自动补齐版本且保留 `ui` 扩展，未知版本 fail-closed。
- 新增独立 `workflow/ir` Compile 阶段，确定性校验节点、边、循环、变量依赖路径和并行全局写冲突；本地 Scheduler 已消费 IR，Temporal bridge 尚未迁移，因此“统一双后端 IR”仍未完成。
- Blackboard 使用 copy-on-write 状态代际并追加内存 `StateEvent`；WorkflowNode 只接收 `StateView`，结果 Delta 由协调器按声明顺序单写合并，`writes.path/source` 由协调器实际映射，Checkpoint 记录 `state_version`。
- Scheduler 改为确定性并行波次；并发节点完成顺序不影响 StateEvent 顺序，失败/挂起/取消会等待本波次所有已启动节点退出。第一增量结束时 StateEvent 尚未持久化，也未实现周期 Snapshot/Reducer 执行。

P4 第二增量实施记录（2026-07-15）：

- 新增 `agent_workflow_revisions` 不可变集合；`agent_workflows` 保留当前视图并以 `current_revision_id/current_revision_number` 指向当前版本。旧定义在首次运行或更新时懒迁移为 Revision 1。
- Workflow 更新使用当前 Revision 条件更新；并发写入冲突返回 `ErrWorkflowRevisionConflict`，失败候选 Revision 由补偿删除清理。
- Run 新增 `workflow_revision_id/workflow_revision_number`。启动时固定当前 Revision，挂起恢复按 Run 引用读取 DSL，后续编辑不再改变历史运行语义。
- 新增 `agent_workflow_state_events`，按 `(run_id, sequence)` 唯一幂等追加；相同序号但内容哈希不同会 fail-closed。Run 保存 `state_version`，恢复后的事件从 Checkpoint 版本继续编号。
- 当前事件在 Run 完成或挂起边界批量落库，尚未实现波次级周期 Snapshot、公开 Revision 列表/指定版本运行 API、Reducer 执行和持久事件重放，因此仍不宣称完整 Event Sourcing。
- Agent 全包、IR/Engine/Tool/Service `go test -race` 与 Agent/Cmd `go vet` 通过；首次窄测的语法错误已修复并记录于 `docs/ISSUES.md`。

P4 第三增量实施记录（2026-07-16）：

- Revision 能力贯通 Proto、gRPC、Gateway、Web 与 AI 助手：支持分页列出不可变版本、读取指定版本及按 `workflow_revision_id` 启动 Run；画布与自定义工作流模式均可显式选择版本。
- Engine 新增只读 `StateCommit` 与持久化回调，每个确定性并行波次合并完成后可向外发出防御性快照，不让 Repository 反向进入 Scheduler。
- 新增 `agent_workflow_state_snapshots`，按 `(run_id, state_version)` 唯一幂等保存；默认每 16 个新事件落周期快照并推进 Run 游标，完成/挂起边界强制保存最终快照，间隔可通过 `AGENT_WORKFLOW_SNAPSHOT_EVENT_INTERVAL` 调整。
- Resume 使用最近 Snapshot 加后续 StateEvent 重建 Blackboard，校验 Snapshot/Event 哈希、事件连续性、Checkpoint 版本与状态摘要；事件缺失、乱序或内容篡改均 fail-closed。
- 这里的 Replay 仅用于确定性状态恢复校验，不会重新调用 LLM 或写工具；P5 的公开只读运行回放、Trace 时间线和控制台仍未完成。

P4 第四增量实施记录（2026-07-16）：

- DSL `writes.reducer` 支持 `append/sum/min/max/merge/first/last` 七种内置策略；编译器拒绝未知 Reducer、节点内重复路径、同一路径混合策略，以及没有 Reducer 的并发写冲突。
- Scheduler 只在协调器中执行 Reducer，并按 IR 节点声明顺序合并并发结果；Goroutine 实际完成先后不会改变状态。Reducer 类型错误发生时，本波次对应节点输出和全局路径均不产生部分提交。
- Workflow Editor 增加全局状态写入配置，并将其转换为 DSL 顶层 `writes`；加载/保存 Revision 时同时保留 `input_schema/output_schema/retry/policy/profile_ref/provider_ref` 等节点执行元数据，避免编辑器往返造成语义丢失。
- 当前 Reducer 只处理显式全局状态路径；普通节点输出仍保存在节点命名空间。节点级 retry/skip/compensate 状态机、公开只读 Replay 与 Temporal IR 适配继续留在后续增量。
- Agent/Cmd 全包测试、IR/Engine/Service 竞态检测、Agent/Cmd `go vet`、Web 生产构建与本轮定向 `git diff --check` 均通过。

P4 第五增量实施记录（2026-07-16）：

- Engine 将节点状态收敛为显式转换表：`pending -> running -> retrying/success/failed/suspended/canceled/timed_out`，分支未激活节点进入 `skipped`；非法终态重启会被拒绝。
- Scheduler 开始消费 DSL `retry`：总尝试次数最多 10 次，退避受节点总 Timeout 与 Context 取消约束，Jitter 基于节点 ID/尝试次数确定性计算，不引入随机 Replay 漂移。
- 只有 `IsRetryable()` 明确标记的错误或临时网络错误会重试；普通业务错误、审批/Wait 挂起、取消和 Deadline 均 fail-closed。Tool `ExecutionError` 将已有治理分类暴露给 Engine，写工具重试仍必须经过 ToolExecutor、审批与幂等结果回放。
- Node Trace 新增当前/最大尝试次数；Workflow Editor 可通过开关和数值控件配置顶层 Retry，加载/保存继续与 Tool Properties 隔离。
- 自动补偿暂不开放：可靠补偿必须先有持久 Compensation Journal、反向顺序、审批恢复与独立幂等键，不能在失败返回前做一次易丢失的内存回调。
- Agent/Cmd 全包测试、IR/Engine/Tool/Service 竞态检测、Agent/Cmd `go vet`、Web 生产构建与本轮定向 `git diff --check` 均通过。

P4 第六增量实施记录（2026-07-16）：

- Tool 节点支持顶层 `compensation` 契约，包含补偿工具、结构化输入映射、总超时与独立 Retry；IR 编译器限制补偿引用只能读取源节点自身或其上游结果，并校验补偿工具已注册。
- Engine 仅根据已成功节点生成确定性的反向拓扑补偿计划；主 DAG 失败后先持久化原失败 Run 和 `agent_workflow_compensations` Journal，再逐条领取补偿任务，禁止重放主流程副作用。
- Journal 使用 `(run_id, sequence)` 唯一约束、输入/计划哈希、领取租约、Attempt ID 和稳定独立幂等键；计划漂移、并发重复领取和旧 Attempt 回写均 fail-closed。
- 补偿调用复用统一 ToolExecutor，因此继续受 Schema、Tool Policy、审批、熔断、超时和持久幂等结果约束。写补偿可挂起并使用一次性 Resume Token 恢复；拒绝会终止 Journal，失败补偿可由用户显式重试且不会重跑已成功主节点。
- Run 增加 `compensating/compensated/compensation_failed` 状态；原始工作流错误始终保留。当前恢复由审批回调或显式重试触发，后台过期租约自动扫描器、公开 Compensation Journal API 和 Temporal IR 适配仍未完成。
- IR/Engine/Repository/Service/Gateway 定向测试与竞态检测、Agent/Cmd `go vet`、Web 生产构建及本轮定向 `git diff --check` 均通过。

P4 第七增量实施记录（2026-07-16）：

- 完成 Temporal 决策门审查：实验 bridge 未注册到 `agent-service`，却独立实现 Kahn 拓扑、可变 Blackboard、Router、固定 Retry 和 Signal 审批，无法复用当前 IR、Revision、Reducer、StateEvent、Tool Approval 与 Compensation Journal。
- 删除 `workflow/temporal/bridge.go`，不再保留一套不可达且语义漂移的用户 DAG 执行器。用户工作流继续由确定性本地 IR 与持久状态机承载；Temporal 只运行启动链路真实注册的 `TweetRiskControlWorkflow` 和 `TrendingReporterWorkflow`。
- 若未来出现必须由 Temporal 承载的跨天长任务，将新增基于统一 IR 的 Backend Adapter ADR，并要求 Activities 仅调用现有 Node/Tool 执行边界；不得复制拓扑编译、状态归约和补偿逻辑。
- 2026-08-03 边界增量：TweetService 新增内部 `GetAuthorPostingStats` 与 `ApplyTweetModeration` RPC。频率 Activity 只读取最多两条时间戳信号，治理 Activity 只提交命令；TweetService 负责作者校验、幂等可见性更新、严格缓存失效和 Timeline/未读原子清理。Agent Worker 构造链已移除 GORM 与 FollowRepository。当前同步清理仍只覆盖最多 5000 个活跃粉丝，服务间强身份认证与事件化全量清理保留为后续生产增强。
- Agent/Cmd 全包测试、`go vet` 与本轮定向 `git diff --check` 通过；测试输出不再包含已删除的 `workflow/temporal` package。

P4 第八增量实施记录（2026-07-16）：

- 新增用户隔离的 `GetWorkflowRunReplay` gRPC 与 `GET /api/v1/agent/workflow-runs/:id/replay` Gateway 契约，装配 Run、固定 Revision 摘要、StateEvent、最近 Snapshot 元数据和脱敏 Compensation Journal。
- Replay 服务逐条校验事件所有权、连续序号、Event Hash、Snapshot Hash 和最终 `state_version`；任一证据缺失或篡改均 fail-closed。接口只读，不调用 Scheduler、LLM、MCP 或 Tool。
- 补偿回放不返回原始输入/输出、幂等键和审批敏感参数，只暴露状态、尝试次数、输入/计划哈希与时间证据。Mongo 回放查询限制 10,001 条探测，超过 10,000 个事件明确拒绝，不静默截断。
- Workflow Editor 增加只读运行回放对话框，展示完整性、Revision/Snapshot、状态事件时间线和补偿步骤；后端、Gateway 离线测试与 Web 生产构建通过。

P4 第九增量实施记录（2026-07-16）：

- DSL 顶层新增 `budget`，支持最大节点尝试、最大并发节点、工作流总超时、总 Token 和估算成本微单位；IR 拒绝负值和超过服务硬上限的定义，Workflow Editor 提供独立运行预算配置并在 Replay 中显示最终快照。
- 删除未接入且非线程安全的旧 `WorkflowTracker`，新增 Runtime `BudgetTracker`。Scheduler 在每次实际尝试前计数，使用信号量限制节点处理器并发，并把预算总账注入所有节点 Context；跳过分支不计数，Retry 会再次计数。
- 直接 LLM Tool、Runtime ReAct/Plan-Execute 和 Legacy 策略回退均在模型网络调用前预留输入、最大输出及可选成本。并发分支会计入在途预留；Provider Usage 优先，缺失时显式估算；已完成调用即使超限也保留真实消耗后终止。
- Run 成功、失败、补偿、挂起和恢复路径均携带预算快照；Checkpoint 保存已消费节点与 Usage，恢复继续累计。成本预算为 0 时关闭金额拦截，启用后未知模型定价 fail-closed。
- Agent/Gateway/Cmd 全包测试、Runtime/Engine/Tool/Service 竞态检测、Agent/Gateway/Cmd `go vet`、Web 生产构建与定向格式检查通过。

P4 第十增量实施记录（2026-07-16）：

- 新增补偿恢复 Repository 端口和 Mongo 聚合查询：先按 Run/Sequence 选出严格首个未成功 Journal，再只返回 `executing` 且 `lease_until <= now` 的候选，避免后续计划跨越失败、挂起或有效租约步骤。
- `WorkflowCompensationReconciler` 随 Agent Service 生命周期启动，扫描超时、周期和批量大小均有界；多实例发现相同候选后仍由现有 `ClaimWorkflowCompensation` 原子竞争，旧 Attempt 无权提交结果。
- 后台恢复复用原 ToolExecutor、Retry、幂等键和 Run 状态机。无需审批的任务自动继续；审批型或未注册工具先原子重领再标记失败，把 Run 转为 `compensation_failed`，由用户显式重试获取可交付的一次性 Resume Token，后台不绕过审批也不丢失令牌。
- 离线测试覆盖安全工具自动恢复、审批工具零调用降级、多 Reconciler 并发仅一次副作用；Repository/Service 定向测试与竞态检测、Agent 全包测试、Agent/Cmd `go vet` 和定向格式检查均通过。

P4 第十一增量实施记录（2026-07-16）：

- 新增 `GetWorkflowCompensationJournal` 与 `RetryWorkflowCompensation` RPC/HTTP 契约；查询先通过 `(run_id, user_id)` 获取 Run，再按相同租户读取 Journal，跨用户访问失败。
- Journal 响应使用独立运行摘要和条目 DTO，只暴露状态、哈希、租约、尝试次数、审批请求 ID 与错误，不返回工作流/补偿原始输入输出、幂等键、Attempt ID 或审批参数。
- 人工重试只允许严格首个未完成记录为 `planned`、`failed` 或租约已过期的 `executing`；有效租约和 `suspended` 记录不可抢占。专用端点继续复用原子 Claim、ToolExecutor、审批和一次性 Resume Token，不重放主 DAG。
- Workflow Editor 增加独立补偿日志视图与下一步骤重试入口；Agent/Gateway 全包测试、Service/Repository/gRPC/Gateway Handler 竞态检测、Agent/Gateway/Cmd `go vet` 和 Web 生产构建通过。

P5 第一增量实施记录（2026-07-16）：

- 新增用户隔离的 Run 分页查询 RPC/HTTP API，支持 Workflow/Status 过滤和稳定倒序分页；列表使用轻量摘要，不携带输入、输出或 Trace 大字段，详情仍按 Run 单独读取。
- Workflow Editor 新增运行记录控制台，可查看历史状态、错误、预算和节点 Trace，并把历史 Run 切换为当前回放/补偿上下文。
- 新增持久 `canceling/canceled` 状态、取消原因/时间证据和跨实例取消端点。执行实例有界轮询 Mongo 控制状态并用 `context.CancelCause` 传播；正常完成/挂起与取消使用 `status + revision` 条件提交。周期 Snapshot 通过专用原子仓储接口只推进 `state_version + revision`，失败恢复本地游标后可幂等重试，取消请求不会被旧运行快照覆盖。
- 取消只接受 `running`，有效审批挂起不被绕过；Scheduler 继续等待已启动节点退出，已成功节点仍按原持久 Journal 补偿。离线测试覆盖阻塞节点跨控制请求取消、租户隔离、原因长度、分页/过滤和 Gateway 契约；Agent/Gateway 全包测试、Service/Repository/gRPC/Gateway Handler 竞态检测、Agent/Gateway/Cmd `go vet` 和 Web 生产构建通过。

P5 第二增量实施记录（2026-07-16）：

- 新增独立 `observability` 契约与 Mongo/InMemory Recorder，Run、Step、LLMCall、ToolCall 分集合持久化；记录键显式包含 `user_id`，读取必须使用 `(user_id, run_id)`。
- Runtime 和 Workflow 共用同一追踪数据模型。LLM 保存最终 Model/Provider、Usage、成本、耗时和错误分类；ToolExecutor 保存治理决策、尝试次数及输入/输出摘要。Prompt、Completion、工具参数和结果正文默认不进入 Trace。
- 新增 `GetWorkflowRunTrace` gRPC 与 `GET /api/v1/agent/workflow-runs/:id/traces`；查询先验证 Run 所有权。Workflow Editor 优先消费独立 Trace，并对历史 `output_json.traces` 保持只读兼容。
- Observability/Runtime/Repository/Tool/Service/gRPC/Gateway Handler 竞态检测、Agent/Gateway 全包测试、静态检查和 Web 生产构建通过；隐私测试验证原始 Prompt、Completion 与 Tool 输入输出不会出现在 Trace 响应。

P5 第三增量实施记录（2026-07-16）：

- `cmd/agent-service` 初始化 ParentBased OTLP gRPC Provider，采样率由 `AGENT_TRACE_SAMPLE_RATIO` 控制；Agent gRPC Server、Tweet/User gRPC Client 接入 `otelgrpc`，退出时限时 Flush。
- Recorder 扇出到 Mongo、Prometheus 与 OTel。OTel 产生逻辑 Run/Step/LLM/Tool Span；Prometheus 增加 Run/Step/LLM 请求、延迟、Token 与微单位成本指标，Tool 指标继续由 ToolExecutor 单点产生。
- Metric Label 只使用有限 source/strategy/status/step_type/provider/direction/estimated 枚举，刻意不使用模型名和任何租户身份。内存 gRPC 联合测试验证 Trace ID 传播，Span/Metric 测试验证正文和高基数值不会泄露。

P5 第四增量实施记录（2026-07-16）：

- 新增可组合 HTTP Transport/Server Middleware：客户端从请求 Context 创建 Span 并注入 W3C `traceparent`，MCP HTTP 服务端提取父上下文；默认模型、LM Studio、用户级 Provider、Reranker 与 MCP SSE/Tool 请求均接入。
- Transport 只记录固定 Span 名、HTTP 方法、协议、目标主机/端口和状态码，不记录 URL Path、Query、Header、Authorization、Prompt、Tool 参数或请求/响应正文；原始网络错误也不写入 Span，避免错误文本夹带完整 URL。
- 包装器复制 `http.Client` 并保留调用方的 Transport、Redirect、Timeout 与 Cookie 策略。离线联合测试验证 Provider Client -> MCP Server 父子 Trace、敏感数据隔离、重定向禁止和 Context 取消；相关包竞态检测通过。

P5 第五增量实施记录（2026-07-17）：

- 新增 Redis Stream 执行事件 Store，与 Mongo Recorder 通过 Fanout 并行写入。Key 按 `user_id + run_id` 哈希隔离，Stream ID 作为稳定游标，长度和 TTL 均有上限；投递失败只产生遥测告警，不改变工作流业务结果，完整查询仍以 Mongo Trace 为准。
- 追加 `WatchWorkflowRunEvents` 服务端流式 RPC 与 Gateway SSE。Service 在订阅前校验 Run 所有权，提供空闲心跳、最长连接窗口、终态关闭、陈旧/超前游标重置和 Context 取消；Gateway 不把 Bearer Token 放入 URL，也不向 SSE 暴露后端原始错误。
- Workflow Editor 使用带 Authorization 的 `fetch` 增量解析 SSE，按 `record_id` 幂等更新 Run/Step/LLM/Tool Trace；切换 Run、关闭控制台和卸载页面都会取消旧连接，断线或窗口结束后从最后游标恢复，重置/终态重新读取 Mongo 快照。
- 相关并发包竞态检测、Agent/Gateway/Cmd 全量测试、`go vet` 与 Web 生产构建通过。本地页面服务返回 200；受 Codex 宿主 Playwright 权限/依赖限制，自动化浏览器截图未计入验收证据。

P5 第六增量实施记录（2026-07-17）：

- ToolExecutor 对所有 MCP、Runtime 与 Workflow 工具结果统一执行 JSON 编码和 1 MiB 硬上限；超过 64 KiB 的结果通过存储无关的归档端口写入独立私有 MinIO Bucket，`workflow/tool` 不依赖 MinIO 或 Mongo。
- 幂等执行记录采用“内联结果或对象引用”二选一持久化；引用回放强制校验长度、SHA-256、Bucket、Key 前缀与 Content-Type。对象上传成功但 Mongo 条件提交失败时回收未提交对象，避免产生确定性孤儿。
- Tool Trace、gRPC/SSE 和 Workflow 控制台只新增 storage、无凭证 `minio://` 引用与 Content-Type，不记录 Tool Result 正文。对象归档开关可独立关闭；关闭后超过内联阈值的结果 fail-closed，不退回 Mongo 大文档。
- 相关并发包竞态检测、Agent/Gateway/Cmd 全量测试、`go vet`、Web 生产构建与 Docker Compose 配置校验通过；对象存储测试全程使用离线 Fake。

P5 第七增量实施记录（2026-07-18）：

- 新增租户隔离的 Blackboard 检索 Service，从目标版本之前最近的已验证快照加载基线，再通过 Mongo 现有 `(user_id, run_id, sequence)` 索引读取 `(base, target]` 有界事件并校验连续性、事件哈希和最终状态哈希；查询不执行 Scheduler、模型或工具。
- 新增版本化游标分页、路径前缀和关键词过滤。游标绑定首次查询的状态版本与过滤条件，运行中的状态继续增长时后续页面保持稳定；页大小、查询长度、字段总数、事件总数和单值预览均设硬上限。
- 新增 gRPC/HTTP 契约与 Workflow Editor 状态检索面板。API Key、Authorization、Cookie、Password、Secret 和 Access/Refresh/Resume Token 等敏感键递归脱敏；超出预览上限的值只返回类型、长度和 SHA-256。
- Repository/Service/gRPC/Gateway 目标测试与竞态检测、Agent/Gateway/Cmd 完整测试、完整 `go vet`、Web 生产构建和定向格式检查通过。安全回归测试额外验证顶层敏感字段不会被关键词命中。

P5 第八增量实施记录（2026-07-18）：

- 新增独立于 OTel Trace 采样的 Prompt/Completion 安全预览采样器：默认关闭、按稳定键确定性采样、预览最多 4 KiB、扫描上限默认 64 KiB；疑似密钥、Authorization/Cookie、邮箱、手机号、身份证和带 Query/UserInfo 的 URL 直接拒绝，不尝试局部脱敏后落库。
- Runtime 与 Workflow LLM Trace 记录 Prompt Template ID/Version。内置 Profile 使用版本化 Profile 身份，Workflow 节点使用 `workflow_id + node_id` 和不可变 Revision 身份；这只是执行证据，不等同于 P7 的 Prompt 发布/灰度管理。
- 安全预览只写入租户隔离的 Mongo Trace，并通过既有所有权校验 API 展示；OTel Attribute、Prometheus Label 和日志继续禁止 Prompt/Completion 正文。采样状态显式区分 `disabled/not_selected/sensitive/oversized/captured`，便于审计为何没有样本。
- 新增 Agent Runtime Grafana Dashboard，覆盖 Run 吞吐/成功率/P95、Step 延迟、LLM 请求/Token/成本、Tool 治理决策/延迟、熔断与对账失败；Docker Prometheus 增加 Agent `9191` 抓取，Helm/Docker Grafana 使用固定 `prometheus` datasource UID，查询不使用租户或运行级高基数 Label。

### P5：Trace、Metrics、Replay 与运行控制台

目标：形成 Agent Infra 的可见证据，让一次失败可定位、一次运行可解释、一次工具调用可审计。

预计工作量：4-6 天。

任务：

- [x] 为 `cmd/agent-service` 初始化可 Flush 的 OTel TracerProvider，并为 Agent gRPC Server 与 Tweet/User gRPC Client 配置传播器。
- [x] 为外部 HTTP/MCP 传输配置 Trace Header 注入与对端传播验证。
- [x] 定义 `RunRecord`、`StepRecord`、`LLMCallRecord`、`ToolCallRecord`，避免把所有信息塞进 `output_json`。
- [x] TraceRecorder 支持 Mongo 持久化、OTel Span、Prometheus 指标和测试 InMemory Recorder 扇出。
- [x] 记录 run_id、step_id、strategy、model/provider、tool、latency、status、error_class、usage、budget snapshot；数据模型为 parent_step_id 保留稳定字段。
- [x] Prompt/Completion 默认不全量记录，只保存 SHA-256、长度和使用量；可选安全预览默认关闭、确定性有界采样并拒绝疑似敏感内容，同时记录 Prompt Template ID/Version。
- [x] MCP Tool Result 设置大小上限，大结果放对象存储并在 Trace 中保存引用。
- [x] 增加 Run API 的列表、详情和跨实例取消；前端展示运行记录、预算和节点 Trace。
- [x] 增加独立 Run/Step/LLM/Tool Trace 与租户隔离查询，控制台不再长期依赖 `output_json.traces`。
- [x] 增加租户隔离、有界、可恢复的运行事件流。
- [x] 增加可检索黑板快照。
- [x] 增加只读证据 Replay：校验并返回历史 Event/Snapshot/Compensation，不调用 Scheduler、模型或工具，写工具永远不自动重放。

Prometheus 指标建议：

```text
agent_runs_total{strategy,status}
agent_run_duration_seconds{strategy,status}
agent_steps_total{type,status}
agent_step_duration_seconds{type,status}
agent_llm_requests_total{source,provider,status}
agent_llm_tokens_total{source,provider,direction,estimated}
agent_llm_estimated_cost_micros_total{source,provider,estimated}
agent_tool_calls_total{tool,category,status}
agent_tool_duration_seconds{tool,status}
agent_budget_exhaustions_total{dimension}
agent_approval_pending
workflow_node_duration_seconds{node_type,status}
rag_retrieval_duration_seconds{route,status}
```

指标约束：

- 禁止把 user_id、run_id、prompt、URL 或错误原文放入 Label，避免基数爆炸。
- 计数器使用有限枚举 Label；详细身份和参数进入 Trace/Audit。
- Token 和成本使用整数累计，金额使用微单位，避免浮点误差。

验收标准：

- 任意 Run 可从 Gateway Trace 追到 Agent、LLM、MCP 和下游 gRPC。
- 工具失败能区分参数错误、权限拒绝、超时、下游错误和预算终止。
- 运行控制台能展示 Step 顺序、耗时、Token、工具和审批状态。
- 关闭 Prompt 采样时，日志和 Trace 不泄露用户 API Key 与完整敏感内容。

面试价值：Trace 是证明 Runtime 存在的关键，而不是辅助功能。

### P6：三级 RAG/Memory 工程化与评测闭环

目标：把现有 RAG 从“实现了检索”升级为“可评估、可迁移、可控制噪声”。

预计工作量：5-8 天。

任务：

- [ ] L1 Persona 从 Agent Service 直连数据库迁移到清晰的 Profile Repository/Service Boundary，避免新的共享数据库职责泄漏。
- [x] L2 改为共享 `agent_episodic_memory` Collection，通过 `user_id` Payload Filter 隔离用户。
- [x] 为旧 `episodic_user_<id>` Collection 提供有界双读兼容、显式迁移命令和 `--verify-only` JSON 验收；旧集合删除仍需真实验收成功后的人工确认。
- [x] Session End 事件触发结构化摘要结晶，包含 memory_type、facts、preferences、decisions、followups、source_dialogue、summary_version；HTTP/gRPC 入口在保留对话的同时取消并等待旧摘要任务，再同步强制结晶。
- [x] Memory 写入使用稳定 Point ID Upsert，重复 Session Summary 事件覆盖同一点，避免重复记忆。
- [x] L3 保留 ES BM25 + Qdrant HNSW 双路召回；RRF 输出排序增加稳定 ID/Content tie-breaker。
- [x] Composite Score 参数可注入，并记录每条 Chunk 的 sim/time/freq/final 分解，便于评测。
- [ ] Token Budget 按完整 Chunk 装配；低于阈值直接丢弃，不对 Chunk 做中途截断。
- [ ] Embedding Collection 保存 model、dimension、version；模型升级采用新 Collection 双写、回填、灰度读和回滚。
- [ ] 词典路由只有在基准测试证明收益后再替换为 Aho-Corasick；不要为了名词堆砌增加复杂度。
- [ ] ONNX Semantic Router 作为可选优化，先建立延迟/准确率基线，再决定是否内嵌模型。

RAG Eval：

- [x] 建立 51 条带相关文档标注的数据集，覆盖中文、英文、混合语种、错别字、无答案、时态记忆和用户画像。
- [x] 新增 Recall@K、MRR、NDCG@K、空召回率、噪声率和 P50/P95 的统一 Runner；逐 Case Provider/存储错误进入报告，不伪装成成功。
- [x] 新增 BM25-only、Vector-only、RRF、RRF+Rerank 的真实适配命令与稳定 RRF/重排 tie-break。
- [x] 使用 34 条独立数据集评估 Router 意图准确率和 L1/L2/L3 错投率；离线词典/默认层基线为 91.18%。四模式 Provider 对照 Runner、live 开关、Endpoint Policy、Token/成本与降级错误报告已落地，Semantic/LLM Fallback 真实对照仍需固定 Provider 后执行。
- [x] 评测报告保存环境、模型版本、数据集版本、随机种子、逐 Case 排名和错误，保证相同输入可复现。

Agent Eval：

- [x] 建立至少 50 条任务集：当前 52 条覆盖普通问答、平台检索、写作、需澄清、工具失败、提示注入、越权发布、审批恢复和预算终止。
- [x] 指标包括任务完成率、工具选择准确率、工具成功率、平均 Step、Token、预算终止率、审批通过率和虚构工具结果率；输出正文不进入报告。
- [x] 第一版门禁：只读工具选择准确率 >= 90%，越权写操作成功数 = 0，虚构“工具已执行”数 = 0；稳定/候选使用同一数据集版本并限制任务、工具和语义断言回归。
- [ ] Task Completion 先测基线再定目标，禁止为了漂亮数字修改评分口径。

验收标准：

- RAG 方案选择有数据对比，不再依赖主观截图。
- Embedding 模型升级可灰度、可回滚。
- 记忆检索始终带 user_id 过滤，跨用户泄露测试为 0。
- Eval 可以在 CI 中使用 Mock/固定模型结果稳定执行。

面试价值：评测能把“我做了 RAG”升级为“我知道如何证明 RAG 有效”。

### P7：产品化与高级能力（面试核心闭环后再做）

目标：增强演示体验和长期扩展，不阻塞前述核心架构。

任务候选：

- [x] Prompt/Profile 版本管理与灰度发布（已完成不可变 Catalog、Mongo 版本/Release 仓储、草稿/发布状态、乐观并发、双人审批、管理 API/UI、跨实例热更新、动态项目 RBAC，以及运行错误率/P95/成本与固定业务效果门禁和 CAS 自动回滚；真实产品事件源验收归入 Eval/A-B 任务）。
- [ ] 工作流模板市场、Revision Diff、发布/草稿状态。
- [ ] Agent Trace 可视化和运行对比。
- [ ] Eval 自动回归与 Prompt A/B Test（运行错误率/P95/成本和固定业务效果信号 A/B 安全门禁与自动回滚、52 条 Agent Task、受控 Live Runtime Adapter、报告 HMAC、MinIO Versioning/Object Lock COMPLIANCE 归档和归档回执绑定发布审批均已完成；真实稳定基线、Object Lock Bucket 环境验收和 Tweet/Agent 产品事件源接入仍待完成）。
- [ ] 多租户额度、项目级并发控制和计费报表。
- [ ] SubWorkflow、Map/Reduce、Aggregator 和事件触发器。
- [ ] 外部 MCP Server 产品化完善（个人/项目级注册、真实成员校验、实时撤权、能力同步、租户隔离、Egress Allowlist、受治理执行、连接池、主动健康巡检和部署托管项目凭据已完成；真实第三方、Kubernetes Secret 轮换、多副本和远端幂等履约验收待完成）。
- [ ] 当且仅当支持用户代码/脚本时，引入容器或微虚拟机沙箱；当前服务边界工具不需要先造通用代码执行平台。

这些能力可以作为面试中的演进方向，不能在未实现时当作当前能力陈述。

P7 第一增量实施记录（2026-07-19）：

- `profile` 包新增不可变 Catalog、严格 Release 校验和基于用户身份的 SHA-256 基点分桶；返回值深拷贝，部分灰度缺少稳定身份时 fail-closed。
- 内置 `assist.draft` 注册 v1/v2，默认 Release 固定 v1；Assist 与 Workflow Runtime 统一经 Resolver 选择 Profile，Prompt 变量只在选择后的副本中渲染。
- `cmd/agent-service` 从 `AGENT_PROFILE_RELEASES` 构造启动快照，未知字段、版本、重复发布和非法比例直接拒绝启动。实际 Prompt 版本进入 LLM Trace，Assist 消息元数据记录 Profile/Prompt 版本。

P7 第二增量实施记录（2026-07-20）：

- 新增独立 `MongoProfileRepository`，使用不可变版本集合与可变 Release 指针集合；草稿发布和 Release 更新均采用 revision CAS，快照携带 Schema 与 SHA-256 完整性校验。
- 新增 `ProfileCatalogManager` 与 `AtomicResolver`。草稿不进入运行目录，发布和 Release 变更先构造完整下一代 Catalog，失败保留旧目录，运行时不查询 Mongo。
- Agent Service 默认启动加载持久化目录；环境 Release 保持最高优先级应急覆盖，`AGENT_PROFILE_STORE_ENABLED=false` 可回退到内置目录。
- Profile/Repository/Service 竞态检测、全仓测试、全仓 Vet、Compose 解析和 Helm 渲染通过；生命周期测试全部使用离线 Fake。
- 第二增量结束时尚无管理 API、发布审批和跨实例通知；这些能力已在后续增量补齐，Eval/A-B 自动判定仍未完成。

P7 第三、第四增量实施记录（2026-07-20）：

- 新增受保护管理 API、append-only 脱敏审计、Redis 失效通知与周期 Mongo 反熵；完整构建和校验成功后才原子替换运行目录。
- 新增绑定草稿 revision/hash 的双人发布审批、执行租约与失败恢复，申请人不能审批自己的发布申请；直接发布默认关闭，仅保留显式 break-glass。
- Web 新增 `/agent/profiles` 管理台，覆盖草稿、发布申请、审批/恢复、Release CAS 与审计。

P7 第五增量实施记录（2026-07-20）：

- 新增 `agent_profile_role_bindings` 与 `agent_profile_role_audit_events`，动态项目角色使用用户唯一索引、revision CAS 和 append-only 审计。
- Agent Service 合并环境变量 break-glass 与 Mongo 动态角色，并在每个 Profile 管理 RPC 内执行 viewer/editor/approver/admin 校验；Gateway 只传递 JWT 身份并在滚动升级期间兼容旧服务的静态角色。
- 只有环境变量根管理员可授予或撤销动态 admin；动态管理员可管理普通角色。`AGENT_PROFILE_DYNAMIC_RBAC_ENABLED=false` 可回滚到纯静态角色。
- 管理台新增成员权限与角色审计栏目。当前仍是项目级授权，尚未接入外部组织目录或统一 IAM。

P7 第六增量实施记录（2026-07-20）：

- 新增绑定 Release revision 的 Profile 实验状态机、独立 Mongo 实验/观测集合、每 Profile 单运行实验约束、Run ID 幂等观测和有界扫描；不保存用户、Prompt、Completion 或工具正文。
- Runtime v2 记录实际 Profile ID/version，旁路 Recorder 采集成功率、耗时和估算成本；按每组最小样本比较错误率、P95 延迟和平均成本，回归时通过 Release CAS 自动把候选流量降为 0。
- Release 被外部修改时实验进入 `superseded`；达到目标样本只标记 `passed`，不会自动提升候选。环境 Release 覆盖禁止实验，功能开关默认关闭并可立即停用协调器。
- 管理 gRPC/Gateway/API/UI 支持启动、查询、立即评估和停止；Prometheus 仅使用分组、结果和终态低基数标签。语义质量、答案正确性和业务转化仍需后续 Eval/信号接口。
- 本增量目标包竞态测试、全仓测试/Vet、Web 生产构建、Compose 解析、Helm 默认/启用/缺失管理员失败模式均通过；所有测试使用离线 Fake，未连接真实模型或执行线上流量实验。
- 定向测试与竞态检测、全仓测试/Vet、Web 生产构建、Compose 解析及 Helm 正反配置渲染均通过；未连接真实组织目录或外部 IAM。

P7 第七增量实施记录（2026-07-22）：

- `internal/module/agent/eval` 新增存储无关 Agent Task 契约、52 条九类固定任务、录制执行结果 Adapter、确定性输出断言和行为/工具/审批/预算/安全指标；报告只保存输出 SHA-256、字符数和轨迹摘要，不保存回答正文。
- 新增 `cmd/agent-task-eval`，可离线生成候选报告、比较同数据集稳定报告并通过非零退出码执行质量门禁；默认绝不连接模型、MCP、Mongo 或外部服务。
- 门禁硬性拒绝越权写成功和虚构工具结果，要求只读工具选择准确率至少 90%，并限制任务完成、工具选择和确定性语义断言的相对回归。录制夹具的 52/52 通过仅证明评分契约可复现，不代表真实模型质量，也不会自动晋级 Profile。

P7 第八增量实施记录（2026-07-22）：

- `agent-task-eval` 新增必须显式 `--allow-live` 的受控 Runtime Adapter：严格固定 Provider/Model/Profile Version，复用 Endpoint Policy、Provider Router、Profile Catalog 与 ReActRunner；Credential 只按环境变量引用读取，配置中的明文 Key 会被严格 JSON 解码拒绝。
- Live 工具使用 CLI 私有无副作用沙箱，平台检索/Web 搜索返回确定性测试证据，发布工具永不连接 TweetService。未审批写操作继续由 Runtime fail-closed；审批恢复仅模拟可信服务已消费一次性授权后的继续执行，不替代真实 Resume Token 集成验证。
- 报告新增数据集 SHA-256、脱敏执行配置 SHA-256 与 HMAC-SHA256 完整性证明；支持独立验签和从已验签历史报告加载稳定基线，且在候选 Provider 调用前先验签。HMAC 文件尚不是 WORM/版本化对象归档，也不提供公钥签名的不可否认性。

P7 第九增量实施记录（2026-07-22）：

- `eval` 新增存储无关的不可变报告归档请求/回执契约；MinIO 适配器位于 `objectstore`，评测核心不依赖 MinIO SDK。专用归档 Bucket 必须启用 Versioning 与 Object Lock，且不能带 Bucket Policy；既有 Bucket 不满足条件时 fail-closed。
- 报告对象使用 `COMPLIANCE` 保留模式和 `If-None-Match: *` 不可覆盖写入，对象键绑定数据集版本哈希、数据集/执行配置哈希、签名日期与完整报告 SHA-256；重复归档相同内容只允许验证并返回既有版本，不创建覆盖版本。适配器不提供删除接口。
- CLI 支持生成报告时归档、归档已有签名报告和按回执指定 `version_id` 复验。流程为本地 HMAC 验签、上传、指定版本回读、长度/SHA-256/保留策略核对、再次 HMAC 验签；归档配置只允许环境变量凭据引用，本地回执以 `O_EXCL` 追加式创建。
- 离线 Fake 已覆盖缺失 Versioning/Object Lock、存在 Bucket Policy、GOVERNANCE 降级、回读篡改和重复归档；受影响包普通/竞态测试、Agent 全模块、整仓测试与整仓 Vet 通过。本机 LM Studio 与固定模型可达，但 HMAC/MinIO 凭据未配置且 MinIO 未启动，因此未伪造真实 52 条 WORM 基线，真实环境验收仍待执行。

P7 第十增量实施记录（2026-07-22）：

- Eval 报告输出/签名契约从 CLI 抽入 `eval` 纯领域包；新增 `QualityEvidenceVerifier` 窄接口和 MinIO Adapter，按精确 Version ID 校验 HMAC、数据集/执行配置/报告哈希、`runtime_live` Profile 身份、通过 Gate、安全指标与有效 COMPLIANCE 保留期。
- Profile 发布申请可携带归档回执；审批记录只持久化对象定位和质量摘要。申请、批准与失败重试均重新验真，运行时请求热路径不访问 MinIO。`AGENT_PROFILE_EVAL_EVIDENCE_REQUIRED=false` 保持向后兼容并作为回滚开关。
- Proto/gRPC/Gateway/Web 已同步增量契约，管理台支持粘贴 CLI archive receipt、查看 Gate/样本数/关键准确率；Compose、Helm 与 `.env.example` 只通过环境变量/Secret 注入归档凭据和 HMAC 密钥。
- 离线测试覆盖回执验证、错误 Profile 拒绝、强制证据和批准前复验；受影响包竞态检测、整仓测试/Vet、Web 构建、Compose 解析及 Helm 正反配置验证通过。真实 WORM 基线仍受本机 MinIO/Object Lock 和独立密钥缺失阻塞，未将 Fake 结果声明为生产证据。

P7 第十一增量实施记录（2026-07-22）：

- Profile 实验策略新增可选固定业务结果门禁，支持回答被采纳、草稿被发布和内容获互动；旧策略零值保持原运行指标判定，不需要数据迁移或双写。
- 受保护结果入口只允许 admin 服务账号按已有 Run/Event ID 回填正/负结果；同值请求幂等重放、冲突值拒绝、异步运行观测尚未落库时失败关闭。Mongo、响应和指标均不保存用户、Prompt、Completion 或工具正文。
- 领域判定在运行错误率/P95/成本护栏之后检查每组业务结果最小样本与结果率下降阈值；回归继续复用 Release CAS 自动止损，达到目标仍只标记 `passed`，不自动提升候选。
- Proto/gRPC/Gateway/API/Web 管理台已同步策略与聚合统计；真实产品事件源接入和线上样本验收仍待执行，不能把测试回填声明为业务提升证据。
- 领域、Repository、Service、gRPC、Gateway 定向测试与受影响包竞态检测、全仓测试/Vet、Web 生产构建及定向格式检查均通过。

P7 第十二增量实施记录（2026-07-22）：

- Assist Runtime 结果、对话元数据、Proto/gRPC/Gateway 和 Web 历史消息统一携带最小化 `run_id` 与可发布标记；普通对话不获得发布入口，旧客户端仍可省略来源字段兼容运行。
- Confirm 发布不再借用模型可调用 MCP 路径：应用层通过窄 Port 校验当前用户拥有的已完成 Assist Run，再以 `run_id + 正文摘要` 稳定幂等调用 TweetService。该入口代表用户显式操作，不绕过模型驱动写工具的 Policy/Approval 约束。
- TweetService 成功后按 Run 的 Profile 版本与实验窗口记录 `draft_published=true`；重复事件幂等，归因失败不撤销已成功推文。正文、用户 ID 和动态错误不进入实验观测或 Prometheus Label。
- Web 支持结构化候选优先、历史草稿恢复、编辑和最终确认；未发布不自动构造负样本。
- Service/Repository/gRPC/Gateway/Cmd 定向测试、受影响包竞态检测、全仓测试/Vet、Web 生产构建和定向差异检查通过；未连接真实模型、Mongo 或 TweetService，线上 stable/candidate 样本和回滚演练仍待受控环境验收。

P7 第十三增量实施记录（2026-07-22）：

- 点赞、评论由直接 MQ best-effort 改为业务记录与 Outbox 同事务提交，Canal 路由追加 `TWEET_LIKED`/`COMMENT_CREATED`，事件载荷 append-only 增加发生时间；既有 Timeline 消费契约保持兼容。
- Assist 确认发布成功后写入独立、短期 TTL 的内容归因集合，只保存 Tweet、作者、来源 Run 和窗口。默认 7 天，关闭 Profile 实验会同时关闭写入和消费。
- Agent 独立队列消费点赞/评论事件；外部首个有效互动按来源 Run 幂等记录 `content_engaged=true`，自赞、过期、普通推文与重复消息不增加样本。处理失败指数退避三次后进入专用 DLQ，正文和互动用户不进入实验观测或指标标签。
- 本阶段仍不把“未互动”推断为负样本，也未将取消点赞、删除评论或转发解释为业务结果。后续需要明确拒绝动作、受控 stable/candidate 流量验收及 DLQ 运维演练。
- 定向与全仓普通测试、受影响包竞态检测、全仓 Vet、Compose 解析、Helm 渲染和差异检查均通过；测试未启动真实 MySQL/Canal/RabbitMQ/Mongo，因此运行环境验收仍需单独执行。

P7 第十四增量实施记录（2026-07-23）：

- 新增 `cmd/agent-profile-dlq-replay` 和通过窄 Broker 接口隔离的重放服务。默认只检查有界批次；真实执行必须显式提供操作人和原因，单批/累计重放次数均有硬上限，命令不会自动声明缺失队列。
- 每条消息先由 DLQ Routing Key 恢复原事件类型，再重新解析和校验；毒消息、超大消息、未知路由和达到重放上限的消息保留在 DLQ。报告只保存消息 SHA-256、固定类型/错误码和原因 SHA-256，不输出事件或评论正文。
- 重放保留 Trace/Correlation Header、清除消费重试次数并增加独立重放计数；独立 RabbitMQ Channel 强制 Publisher Confirm，Broker ACK 后才 ACK 原消息。ACK 失败显式标记结果不确定，依赖既有 Run/事件幂等消化可能重复。
- Fake 测试覆盖只读检查、正常重放、毒消息、重放上限、Publisher Confirm 初始化、发布失败和 ACK 不确定窗口。当前 Docker Compose 无运行服务，尚未执行真实 DLQ 演练，不能把代码闭环描述为环境验收完成。

### P8：统一智能助手与能力生态

详细产品、架构、迁移与回滚计划见 [`docs/agent/UNIFIED_AGENT_PRODUCT_PLAN.md`](./agent/UNIFIED_AGENT_PRODUCT_PLAN.md)。

当前决策：

- [x] 统一连续对话成为长期主入口；五个模式保留为兼容 API 和内部 Execution Profile，不再继续扩展同级模式按钮。
- [x] Tool/Connector 表达动态能力，Workflow 表达可重复的确定性自动化，Multi-Agent 表达受预算约束的内部执行策略。
- [x] 复用 Runtime v2、ToolExecutor、Policy、Approval、Budget、Trace 和 Workflow Engine，不新建第二套 Agent 基础设施。
- [x] P8.0：新增统一 `RunAgent` 契约和可替换的保守 Capability Planner。
- [x] P8.1：统一 Chat/Consult/Assist 的会话与运行路径，前端迁移为统一输入、能力面板和独立自动化入口。
- [ ] P8.2：真实 Web Search/Page Read 与 Citation、安全和成本闭环。前三增量已完成 Brave Adapter、`web.search`、`page.read`、公网 Citation、出站安全、来源缓存、用户/Run 准入预算及用户级加密搜索 Provider Config；真实 Brave 受控验收仍待完成。
- [ ] P8.3：个人/项目级远程 MCP Connection、真实成员校验与实时撤权、Tool Snapshot、受治理 Agent 执行、Workflow 高风险/声明式幂等写入审批、有界 Session Pool、主动健康巡检、Unified Agent 权威 Run 生命周期、`ask_human`/工具审批的加密 Checkpoint/Resume、部署托管项目凭据，以及显式 Live/Write、脱敏签名报告的验收框架已落地；真实第三方与生产轮换/故障验收待执行。
- [ ] P8.4-P8.5：Workflow-as-Tool、版本化 Skill 契约/目录、成功任务显式模板化、父子聚合预算/成本视图、两类 Tool Continuation、两个只读顺序多角色模板、生产/Eval 共用核心、v3 Evidence/Claim/Citation 门禁、签名评测授权、产品 SLI、租户联合目录，以及公共市场的发布者 Owner、公钥生命周期、签名发布、版本撤回和追加审计均已完成；固定 qwen3.7/Profile v5 云自动资格运行已通过。当前只把真实外部人工签认、WORM Version ID、真实 Brave/MCP/Workflow-as-Tool 价值链验收、统一入口体验与可审查交付基线作为收口条件。并行/角色级恢复和公共市场安装链路转入需求驱动延期项。

范围冻结（2026-08-03）：P8 后续不再以新增节点、Provider、角色、Marketplace 安装能力或中间件为默认进度。完成标准改为统一助手体验、三条真实价值链、失败路径证据、产品指标、演示手册和可回滚提交；具体门禁以 `docs/agent/UNIFIED_AGENT_PRODUCT_PLAN.md` 第 10 节为准。

真实性约束：首批保守 Planner 只是向后兼容迁移层；Web Search 未启用或未完成 live 验收时、内部回环 MCP 或硬编码模式分发不得被描述为通用联网助手或开放 MCP 生态。

P8.0 实施记录（2026-07-23）：

- Proto/gRPC/Gateway/Web API 新增统一 `RunAgent` 契约，响应返回实际 `execution_profile`、`capability_ids`、Run、站内检索结果和可发布草稿标记；旧五入口保持兼容。
- Service 新增可注入保守 Capability Planner，自动或按单个 Hint 选择当前真实存在的 Chat/Consult/Assist 路径；未知能力和“搜索并写作”等尚未实现的复合执行明确失败，不伪造动态能力。
- Capability Hint 不参与授权；既有 Runtime Profile、ToolExecutor、Policy、Budget、Approval 和 Dialogue 归属校验继续作为执行边界。
- Agent/Gateway 全模块普通测试、Service/gRPC/Handler/Router 竞态检测、目标 Vet 与 Web 生产构建通过。

P8.1 第三增量实施记录（2026-07-24）：

- `conversation.reply` 已从兼容 Chat 路由迁移到显式 `runtime.chat`；新增无工具 `conversation.reply@v1` Profile，统一入口复用 Message Builder、认知上下文、Model Catalog、Token/成本/并发预算、Trace 和 Session Summary。
- 普通 Chat 不列举 MCP Catalog，也不向模型暴露搜索或写工具；其他能力必须由 Capability Catalog 的精确路由显式选择，Profile/Policy 继续 fail-closed。
- Runtime Chat 保存实际 Run/Profile/Prompt/Capability/Execution Profile 与上下文预算元数据；消息对落库失败时不返回未持久化答案。
- 旧 `/chat` API 与响应保持兼容，`AGENT_RUNTIME_V2_MODES=chat` 可切换 Runtime v2，关闭后回退原 Legacy 直连路径；统一入口仍可通过停止前端切流回到旧 API。
- Agent 全模块与 `cmd/agent-service` 普通测试、Service 竞态测试和 Agent 全模块 Vet 通过；测试使用 Fake Runner/Repository，未连接真实模型、Mongo、MCP 或公网。

P8.1 第四增量实施记录（2026-07-24）：

- `content.draft` 已迁移到显式 `runtime.draft`，统一入口保证可发布草稿拥有持久化 Run；旧 `/assist` 与 `compat.assist` 继续作为迁移回滚路径。
- `RunAgent` 追加类型化 Artifact、`run_status` 与脱敏 `approval_state`。Artifact 绑定 `source_run_id` 并要求显式确认；普通响应不包含工具参数、审批输入、凭据或一次性恢复令牌。
- 当前 Capability Catalog 没有写能力，成功响应的审批态为 `not_required`。真实待审批状态必须在后续写 Capability 复用现有 Approval Repository/API 后验收，不能以新增 DTO 冒充完成。
- Runtime 草稿消息落库失败改为整体失败；Proto/gRPC/Gateway/Web DTO 同步并通过 Agent/Gateway 回归、Race、Vet 与 Web Build。

P8.1 第五增量实施记录（2026-07-25）：

- Web AI 助手移除五模式主导航和页面内硬编码执行分支，统一调用 `RunAgent`；能力下拉只表达 Hint，实际权限继续由服务端治理交集决定。
- Workflow 保持独立自动化入口，旧 API 不删除。前端回滚不需要迁移 Dialogue、Message、Run 或 Workflow Revision 数据。
- 助手消息展示实际 Capability、Run Status、脱敏 Tool Activity、站内 Citation 和可发布 Draft Artifact；发布必须由用户检查并携带 `source_run_id` 显式确认。
- 会话详情请求增加版本隔离，慢请求不能覆盖用户随后选择的 Dialogue；统一请求成功后刷新服务端会话列表，能力变化复用原 Dialogue；独立工作流审批恢复不再向当前聊天注入无归属结果。
- Web 生产构建及 1440x900/390x844 交互验证通过。P8.2 将接真实 Web Search/Page Read；当前站内 Citation、回环 MCP 和 `not_required` 审批态不得扩张描述。

P8.2 第二增量实施记录（2026-07-25）：

- 新增受限 `page_read`/`PageRead`：只接受公网 HTTP(S)，拒绝凭据、Fragment、私网/本地地址、重定向、DNS Rebinding 和非文本响应，并对 URL、响应体、正文字符、并发与超时设置硬上限。
- HTML 提取仅保留可见文本，移除脚本、样式、表单、导航及隐藏节点；常见中英文 Prompt Injection 信号进入结构化 Safety 元数据，模型侧只收到明确标记的不可信且去指令化文本。
- Search/Page Read 共享 Redis 来源缓存和原子准入脚本。服务端从受信任执行上下文注入 `user_id + run_id`，按用户固定窗口、Run 请求数和 Run 估算成本 fail-closed；用户 DSL/MCP 参数不能伪造计费身份。
- Runtime 联网 Profile 可在搜索后按需读取高价值来源；`web.page.v1` 仅由受信任 `page_read` Observation 投影 Citation，同 URL 的正文摘录替换搜索短摘要。普通 Chat/Assist 不因此获得公网工具。
- Workflow Editor 新增独立 WebSearch/PageRead 组件，ReAct/Plan-Execute 可显式选择对应只读 MCP 工具。部署配置支持独立关闭 Page Read，并配置缓存、并发、体积、速率和成本预算。
- 离线 Fake/HTTP Server/Redis 测试与后端定向测试已通过；真实 Brave 密钥、真实公网质量与生产 Redis 容量仍未验收。

P8.2 第三增量实施记录（2026-07-26）：

- Provider Config 增加 `llm/web_search` 类型隔离；旧记录缺少 `kind` 时按 `llm` 读取。Brave 用户凭据复用既有 AES-256-GCM、AAD、Keyring、Credential Version、Revision 与撤销链路，API Key 不返回前端，也不进入 DSL 或模型可见 Tool Schema。
- 新增租户搜索 Provider Resolver。AI 助手通过可信 `RunAgent.web_search_provider_config_id` 注入内部 MCP 参数；Workflow WebSearch 可保存配置引用，并在执行时按认证用户校验所有权。非搜索配置、跨租户配置、已撤销配置和无可信用户身份均 fail-closed。
- 动态 Brave Adapter 只允许租户选择 Base URL 和密钥；超时、响应体、结果数、Endpoint Policy、并发与成本预算仍由部署控制。所有动态 Adapter 与平台兜底共享同一并发闸门、Redis Cache 和 Governor，缓存键包含用户、配置 Revision 与 Credential Version。
- Web 新增个人联网 API 管理/选择；Workflow 的 LLM 和 WebSearch 下拉按 `kind` 隔离。Proto/gRPC/Gateway 只追加兼容字段；Compose/Helm 增加 Provider Config 密钥 Secret，平台 Brave Key 改为可选兜底。
- 后端定向测试、共享并发闸门测试、Web 生产构建、Compose 解析和 Helm“仅用户配置/平台兜底/Keyring”模式渲染通过。尚未持有真实 Brave 凭据，因此公网质量、真实费用与生产 Redis 容量仍是 P8.2 验收项。

P8.3 第二增量实施记录（2026-07-27）：

- Active Snapshot 增加工具级策略，策略通过 Connection Revision 做 CAS 并绑定 Snapshot ID；只有远端明确声明 `readOnlyHint=true` 且非 destructive 的工具可启用。Schema 重新发现会暂停执行，新 Snapshot 审核后清空旧策略，禁止授权漂移。
- Repository 通过固定两次 Mongo 查询装配用户执行目录，避免按 Connection 逐条读取 Snapshot；Runtime Profile 每次 Run 注入当前用户精确工具名，不把 Capability Hint 当权限，也不把远端任意结构投影为可信 Citation。
- 外部调用经现有 `ToolExecutor.ExecuteAdHoc` 统一执行 JSON Schema、身份、超时、保守重试、熔断、结果硬上限/私有归档、脱敏审计和 Trace。Manager 在实际调用前再次读取 Connection/Snapshot/Policy，并只在该边界解密凭据；远端错误对模型返回通用分类。
- `connector.mcp` 默认保持 `planned`，仅在 `AGENT_EXTERNAL_MCP_ENABLED=true` 且 Manager 可用时注册 `runtime.external_mcp` 精确路由。Run 必须包含当前允许工具的成功 Observation 才能返回并持久化答案。
- Web 增加外部 MCP 管理面，支持用户连接新增/编辑/撤销、发现、Schema 审核和只读工具启停；写、风险或缺失只读声明的工具没有启用入口。API、Service 和 Runtime 离线测试不连接公网第三方 Server。
- P8.3 尚未完成写/高风险 Approval、连接池/健康巡检、管理员托管凭据、项目成员校验和真实第三方 MCP 受控验收，不能描述为开放式 MCP 市场。

P8.3 第三增量实施记录（2026-07-27）：

- Active Snapshot 中未声明只读的工具可由用户显式配置为 `risky`；`write` 仍因缺少可信远端幂等契约而 fail-closed。风险工具不进入统一 Agent Runtime 目录，只能由具有持久 Checkpoint 的 Workflow 动态 Tool 节点引用。
- 动态节点复用 `ToolExecutor.ExecuteAdHoc`、`PersistentApprovalGate`、审批收件箱、一次性 Resume Grant 和当前 Workflow Run/Step 身份。未批准时不调用远端；批准恢复后重新读取 Connection、Active Snapshot、Schema 和 Policy，控制面变更立即生效。平台身份保留在治理通道，第三方 MCP 请求只发送远端 Schema 已校验的显式参数，不隐式携带平台 `user_id`。
- 高风险工具的 Executor 尝试固定为 1，节点错误实现 `IsRetryable=false`，即使 DSL 配置重试也不自动重放未知结果；外部 MCP 补偿继续拒绝，避免在没有补偿幂等契约时制造第二个副作用入口。
- Workflow Editor 新增外部 MCP 工具组件，按当前用户加载已审核、已启用的 `read/risky` 工具，通用 JSON 参数在执行前剥离 DSL 控制字段并按远端 Schema 校验。MCP 管理面明确显示“高风险·逐次审批”并在启用时二次确认。
- 本增量不为普通 AI 对话伪造恢复能力。后续需先新增持久 Agent Run 状态机，再考虑在统一 Agent 中开放高风险工具；真实第三方 MCP、连接健康和远端未知结果运营流程仍需受控验收。
- 离线验证覆盖审批前零调用、批准后单次调用、策略恢复时重新授权，以及 DSL 声明三次重试但高风险远端仍只调用一次；Agent/Gateway/Cmd 扩大回归、目标竞态检测/Vet、Web 生产构建和定向格式检查通过。浏览器只验证了组件卡片与连接管理空态，未把缺少真实连接的策略列表声明为 live 证据。

P8.3 第四增量实施记录（2026-07-27）：

- 外部 Tool Snapshot 追加标准 `idempotentHint`、项目扩展 `_meta["io.twitter-clone/idempotency-key-argument"]` 与经验证的参数名。只有参数在顶层 Input Schema 中为必填字符串时才形成写入契约；旧 Snapshot 字段零值、损坏 Schema 或不完整声明均 fail-closed。
- `write` 策略仍绑定当前 Active Snapshot 并通过 Connection Revision CAS 启用。它不进入统一 Agent Catalog，只允许 Workflow 动态节点使用持久 Approval、Checkpoint 和一次性 Resume Grant；执行和远端调用前继续重新校验租户、策略与 Snapshot。
- 平台按 Run/Step/Tool 生成稳定本地执行键，并使用域隔离 SHA-256 生成固定长度远端键。DSL、模型或用户提供的同名参数在校验前和真实调用前均被覆盖；远端看不到内部 Run/Step ID。ToolExecutor 要求持久幂等，并允许同一逻辑执行使用相同远端键做最多两次临时错误重试。
- Proto/gRPC/Gateway/Web 只追加契约字段；管理面和 Workflow 属性面板区分只读、高风险与幂等写入，并声明键由平台注入。外部 MCP 补偿、普通 Agent 高风险/写入执行和任意服务端自称的 exactly-once 仍不开放。
- 离线测试覆盖不完整元数据拒绝、运行时损坏快照拒绝、攻击者键覆盖、内部身份不泄漏、审批前零调用和重试键稳定；Fresh 定向测试、Remote/Service 竞态检测、整仓测试/Vet、Web 生产构建与页面空态检查通过。真实第三方 Server 是否正确履约仍需受控集成验收；该契约只能描述为声明式重放安全，不能描述为分布式严格 exactly-once。

P8.3 第五增量实施记录（2026-07-27）：

- `SDKDiscoverer` 增加可关闭的有界 Session Pool，池身份绑定 Connection、Transport、Endpoint 和 Credential Version；同一 Session 单租约使用，单连接可按部署上限横向增加少量 Session。全局容量、单连接容量、获取等待与空闲 TTL 均有界，饱和时显式背压，不创建无界 Goroutine 或 Client。
- Connection 更新凭据/端点及撤销成功后使旧池身份失效；空闲 Session 立即关闭，在途 Session 释放后销毁。`AGENT_EXTERNAL_MCP_POOL_ENABLED=false` 保留逐次初始化/关闭的兼容回滚路径，Endpoint Policy、受限 HTTP Client、Trace Transport 和 Context 取消语义不变。
- 主动健康巡检通过 Mongo 独立租约跨实例领取连接，健康写入不递增 Connection Revision，因此不会与用户保存、审核或策略 CAS 冲突。检查使用 MCP `ping`、超时、批次、并发上限、稳定抖动和指数退避；单次失败为 `degraded`，达到阈值为 `unhealthy`，成功后恢复。池饱和只推迟探测，不增加失败计数。
- 健康状态严格为诊断信息，不修改 Discovery、Active Snapshot 或 Tool Policy，也不绕过真实调用前的授权与连接校验。新增指标只使用 Transport、固定结果/错误码、池事件和池状态，不使用 User、Connection、Endpoint 或远端错误原文。
- Proto/Gateway/Web 与 Compose/Helm/.env 追加兼容字段和独立回滚开关；旧 Mongo 记录缺少健康字段时按 `unknown` 且到期可领取，不需要停机迁移。真实第三方 Server、长连接代理/负载均衡空闲超时和生产 Prometheus 告警阈值仍需受控环境验收。
- 收口验证（2026-07-28）已通过 Remote/Repository/Service 竞态检测、整仓测试与串行 Vet、Web 生产构建、Compose 解析和 Helm Lint/渲染。浏览器同时检查桌面与 390px 移动视口，MCP 管理弹窗增加标准 Dialog 语义并压缩移动端空工具区；本地 Agent Service 未运行时可显示明确降级提示。该检查不替代真实第三方 MCP、证书、代理和生产网络验收。

P8.3 第六增量实施记录（2026-07-28）：

- 新增独立 `agent_execution_runs` 权威生命周期集合，明确隔离 Trace 证据与 DAG Workflow Run。Run 在模型执行前创建，之后使用租户、Run ID、`running` 状态和 Revision CAS 原子提交 `completed/failed/canceled/awaiting_human/approval_required`；同一 Run ID 贯穿 Runtime、Trace、对话元数据和状态记录。
- 状态只持久化 Capability/Execution Profile、模型、Profile/Prompt 版本、Token/成本、时间和按 Run ID 域隔离的正文 SHA-256 摘要，不保存 API Key、Credential、Tool 原始参数、用户输入、模型回复或一次性恢复令牌。模型成功后的对话落库失败会把 Run 记为失败；请求取消后使用脱离请求取消信号但受 3 秒上限约束的提交上下文，降低永久 `running` 记录。
- `AGENT_RECOVERABLE_RUNS_ENABLED` 默认关闭并保留旧路径回滚；启用时状态创建失败不调用模型，状态提交失败不返回成功。该基础增量的记录先显式 `resume_supported=false`：挂起态可审计，但在版本化、限长且可加密的 Checkpoint、Resume Claim 租约和重新授权完成前不提供恢复 RPC，也不开放 Unified Agent 高风险/写工具。
- Repository/Service/Cmd 定向测试、Repository/Service Race、整仓普通测试、串行整仓 Vet、Compose 解析、Helm Lint 与 Agent Deployment 渲染通过。首次测试因受限 Windows 并发派生 `compile.exe` 被拒绝而中断，改用 `-p=1` 后通过；该环境问题记录于 `docs/ISSUES.md`。

P8.3 第七增量实施记录（2026-07-28）：

- Runtime 新增独立 `react.v1` Checkpoint/Resume 契约，快照完整保留 Context、模型消息、已完成 Step、累计 Usage 和待处理 `ask_human` 动作，但刻意排除 Tool Definition。恢复从下一 Step 继续并沿用原预算账本，不重放已成功 Tool 或模型步骤。
- `agent_execution_runs` 增加加密 Checkpoint 与 Resume Claim 字段。明文序列化有 256 KiB 默认硬上限，使用独立于 Provider/MCP Credential 的 AES-256-GCM Keyring、`user_id + run_id` AAD、摘要和版本校验；疑似凭据参数、未知版本、损坏或不完整密文均 fail-closed。旧文档无字段时保持不可恢复，不需要停机迁移。
- 人工回答通过 `status + revision + lease + attempt_id` CAS 原子领取；过期租约可重新领取，旧 Attempt 无法提交。恢复前重新解析当前 Profile/Prompt，并只注入当前用户仍获授权的只读 Tool；Checkpoint 不固化授权。Profile 版本漂移会释放 Claim 回到等待态，避免悄然改变原 Run 语义。
- Proto/gRPC/Gateway 新增租户隔离 Run 查询与人工回答恢复，HTTP 响应设置 `no-store` 且不返回密文、密钥标识、租约、Attempt 或回答。Web 从对话事实源装配消息后读取权威 Run，下一条输入继续挂起执行；失败时撤销乐观消息、恢复输入并重新同步权威状态，响应丢失但 Run 已完成时重载持久化对话。
- 本增量只恢复 `ask_human`。`approval_required`、Unified Agent 高风险/写工具、项目级连接、托管凭据和真实第三方 MCP 仍未开放；恢复开关默认关闭，生产启用时缺少独立 Checkpoint 密钥会启动失败，关闭开关即可回到旧不可恢复路径。
- 收口验证通过 Runtime/Credential/Repository/Service/gRPC/Gateway 目标竞态检测、整仓普通测试、串行整仓 Vet、Web 生产构建、Compose 解析、Helm Lint 以及恢复关闭/启用 Keyring 两种部署渲染。测试只使用 Fake Runner/Tool 和内存仓储，不把离线恢复测试描述为真实模型、密钥轮换或多副本故障演练。

P8.3 第八增量实施记录（2026-07-29）：

- `AGENT_UNIFIED_APPROVAL_RECOVERY_ENABLED` 新增为默认关闭的独立灰度开关，并要求同时启用可恢复 Run 与外部 MCP。开启后，`runtime.external_mcp` 可从当前用户、Connection、Active Snapshot 与 Tool Policy 的交集中装配 `read/risky/write` 目录；旧只读目录接口保持不变，关闭任一开关即可回退到只读 Agent 路径。
- Runtime 将待审批 `tool_call` 保存为类型化 `react.v1` Checkpoint，记录动作 ID、工具、参数摘要、审批 ID 与已累计 Usage，但不固化 Tool Definition。批准后只恢复原 Step 内的精确待处理动作，不再次调用产生该动作的模型，也不重放此前成功的模型或工具步骤。
- `agent_execution_runs` 升级为状态版本 2，持久化审批绑定、输入摘要、稳定幂等键、审批时限和加密 Checkpoint；一次性 Resume Grant 只保存 SHA-256，按当前 Run Revision 原子签发并轮换旧令牌，恢复通过状态、Revision、审批、令牌、租约和 Attempt ID 联合 CAS 领取。令牌只在 `no-store` 响应中出现，Web 审批收件箱在内存中立即消费，不写入浏览器存储。
- 签发授权、领取恢复和真实远端调用前均重新校验当前 Profile/Prompt、租户、Connection、Credential Version、Active Snapshot、Schema、Tool Policy、审批状态、动作/输入摘要与幂等键。`risky` 工具最多一次远端尝试；`write` 工具必须具备已审核的声明式幂等参数，并在同一逻辑执行的有限重试中使用同一平台稳定键。
- 拒绝或审批过期会终止对应 Agent Run，清除 Checkpoint、授权哈希和租约；批准恢复只持久化 assistant 结果，不伪造第二条用户消息。Proto/gRPC/Gateway/Web 共用既有 Tool Approval 事实源，审批收件箱按 `source=workflow/runtime` 选择各自授权与恢复端点，没有引入第二套审批系统。
- 离线测试覆盖审批前零远端调用、错误令牌、一次性消费、重放拒绝、调用前授权漂移、风险单次尝试、写入稳定键、拒绝与过期终止，以及 Gateway `no-store`/不回显令牌；目标竞态、整仓测试、串行 Vet、Compose、Helm 与 Web 生产构建通过。应用内浏览器检查了桌面与 390x844 审批抽屉空态/后端不可用状态，未把本地 Agent API 返回 500 的页面检查计为审批 API 端到端验收。真实第三方 MCP、托管密钥、多副本故障注入和远端幂等履约仍需受控环境验收；截至第八增量只能声称“受 Feature Flag 控制的用户级治理与审批恢复”，不能声称开放 MCP 市场或跨系统严格 exactly-once。

P8.3 第九增量实施记录（2026-07-30）：

- 新增独立、存储无关的 `agent/project` 领域与 `agent_projects` Mongo 集合。项目成员嵌入同一文档并与 Revision 共享 CAS 边界；`owner/editor/viewer` 分别对应成员治理、连接治理和能力使用，Owner 不可移除或降级，单项目成员与单用户可访问项目数均有硬上限。
- 成员新增/更新通过窄 `UserDirectory` Port 调用 User Service `GetBatchUsers` 做精确存在性校验；目标不存在、目录不可用或超时均 fail-closed。Gateway 只从 JWT 注入操作者，目标成员来自路径，用户 ID 在 HTTP JSON 中以字符串返回。未来企业目录可替换 Adapter，不把 User Service Proto 或 Mongo 耦合进 MCP。
- 外部 MCP Connection 新增不可变 `scope=user|project` 与 `project_id`。`AGENT_EXTERNAL_MCP_PROJECT_SCOPE_ENABLED` 默认关闭并要求外部 MCP 主开关开启；旧缺少作用域的记录继续按个人连接读取。项目 Owner/Editor 可治理连接，Viewer 可查看并使用已审核能力；列表、按 Connection ID/Server ID 解析、运行目录和真实调用均实时重查成员权限，移除成员后无需等待缓存失效。
- 项目连接暂时继续使用创建者提供的用户凭据，Connection/Snapshot/加密 AAD 均绑定创建者用户 ID；共享成员只通过 ToolExecutor 调用边界使用能力，响应和目录不暴露凭据。管理员托管凭据仍属于下一增量，不能把当前 `credential_source=user` 描述为企业密钥托管。
- Proto/gRPC/Gateway/Web 增量增加项目、成员、作用域和角色感知管理面。离线测试覆盖 Owner 不变量、真实用户校验、Revision 冲突、Editor 管理、Viewer 使用、成员撤销后的目录/调用拒绝、JWT/路径身份和大整数 JSON；目标包 Race、整仓测试、串行 Vet、Web 构建、Compose 与 Helm 默认/开启/非法组合均通过，桌面与 390x844 弹窗布局无溢出。本地 Agent API 仍返回 500，真实 Mongo/User Service/第三方 MCP 的项目链路须在受控环境补验。

P8.3 第十增量实施记录（2026-07-30）：

- 新增 `ManagedCredentialResolver` Port 与文件型部署 Adapter。严格 Registry 只接受引用、项目、Endpoint、认证方式、Secret 文件键和正版本；未知字段、重复引用、明文 Token、非法路径、软链接越界、项目/Endpoint/Auth 绑定不一致全部 fail-closed。真实 Token 位于专用只读目录，Connection 仅保存引用与最近验证版本。
- 托管凭据只允许项目级 Bearer 连接，并受 `AGENT_EXTERNAL_MCP_MANAGED_CREDENTIALS_ENABLED` 独立开关控制；该开关要求外部 MCP 与项目作用域同时开启。用户凭据与托管引用可显式通过 Connection Revision CAS 迁移，切换来源、引用、版本、Endpoint 或认证方式会清空 Snapshot/Policy，避免旧审核授权漂移。
- Discovery、主动健康探测和 Tool Call 都在调用边界重新解析 Secret。Session Pool 身份升级为绑定 Connection、Transport、Endpoint、Registry Version 与当前 Token 单向摘要；同版本 Secret 文件轮换后新调用自然进入新身份，Connection 更新/撤销会使该 Connection 的所有历史凭据身份失效。Registry Version 与 Connection 最近验证值不一致时运行时 fail-closed，Owner/Editor 必须以 Revision CAS 重新保存连接，采纳新版本并清空 Snapshot/Policy；池与状态中均不保存明文 Token。
- Proto/gRPC/Gateway/Web 仅追加 `credential_source`、`managed_credential_ref` 与脱敏版本字段；Web 只让项目 Bearer 连接选择部署托管引用，不提供 Secret 录入或回显。Helm 使用现有 Kubernetes Secret 只读挂载专用目录，并对非法开关组合、空 Registry 和缺失 Secret 做模板期拒绝；Compose 保持显式目录挂载责任。
- 离线测试覆盖精确绑定、严格 Registry、Secret 文件轮换身份、Registry Version 漂移拒绝与 CAS 采纳、Connection 无密文、用户/托管迁移、项目/Feature Flag 限制、Session Pool 全身份失效及 Gateway 不泄密。目标包普通测试与 Race、整仓普通测试/Vet、Web 生产构建、Compose 解析、Helm Lint、合法只读 Secret 渲染和非法配置 Guard 均通过。代码完成不等于生产密钥平台：运行时管理员 CRUD、KMS/Vault Adapter、真实 Projected Secret 轮换、多副本故障和第三方 Server 行为仍需后续受控验收。

P8.3 第十一增量实施记录（2026-07-30）：

- 新增隔离于请求热路径的 `internal/module/agent/mcp/acceptance`、`cmd/agent-mcp-acceptance` 与回环 `cmd/agent-mcp-conformance`。验收命令必须显式 `--allow-live`，写探针还必须显式 `--allow-write`；严格 JSON 配置只允许环境变量或文件凭据引用，并拒绝参数中的凭据型字段。
- Runner 复用生产 Endpoint Policy、受限 HTTP Client、MCP SDK Adapter 与有界 Session Pool，依次验证 Ping、Discovery、只读调用、可选同键写入重放/只读状态计数和文件凭据轮换后的新 Session Identity。Conformance Server 提供确定性只读、声明式幂等写入、状态查询与有界延迟/错误工具，只绑定回环地址并执行 Bearer 双重认证。
- 报告只保存 Config/Endpoint/Catalog/Schema/结果摘要、固定错误码和证据等级，不保存 URL、Token、工具原始结果或错误正文；支持独立 HMAC-SHA256 Key ID 签名和离线验签。响应一致或状态计数都不等于跨系统严格 exactly-once，凭据轮换成功也不等于旧 Token 已被第三方撤销。
- Agent 镜像同时构建两个运维二进制；Helm 增加默认关闭的一次性验收 Job，使用 `backoffLimit: 0`、非 root、只读根文件系统、关闭 ServiceAccount Token，并整卷挂载 Secret 以观察 Projected Secret 更新。模板对空配置、缺少签名 Secret 和非法轮换配置 fail-closed。
- 本地真实 MCP SDK HTTP 集成与 Fake 测试已覆盖协议、Bearer、Schema/结果脱敏、显式写权限、可观察幂等状态、轮换新身份、超时固定错误码、严格配置、签名篡改和 CLI Guard。目标包与 `mcp/remote` Race、整仓串行测试/Vet、Compose 解析、Helm 默认/合法渲染及三类非法 Guard 均通过；命令级本地烟测生成并验签 `passed` 报告（5 通过、0 跳过、0 失败）。完整操作与受控故障矩阵见 `docs/agent_mcp_acceptance.md`。截至本增量仍未连接真实第三方或 Kubernetes 集群，P8.3 继续为 `Partial`。

P8.4 第一至第六增量实施记录（2026-07-31）：

- Workflow-as-Tool 使用独立发布记录和 Revision CAS，把用户明确选择的不可变 Workflow Revision/DSL Hash 发布为动态 Tool。发布和真实调用都重新编译并校验 DAG；持续拒绝补偿、Agent、递归 Workflow Tool、未知工具与外部回调，只读工具不得审批，写工具必须审批且幂等，风险工具必须审批。调用复用统一 ToolExecutor，子 Run 保存父 Run/Action 谱系。
- 新增 `agent/skill` 领域契约，但不建立第二套 Skill 持久化事实源。Skill Catalog 从当前用户 Active 发布记录确定性投影，版本指纹覆盖发布 Revision、Workflow Revision/DSL Hash、Profile/Prompt、预算、指令、单一允许工具和输出 Schema；更新或停用发布会让旧版本立即失效，历史 Run 只保留审计信息。
- Unified Agent 只接受显式、完整的 `skill_id + skill_version`，并固定进入 `skill.run -> runtime.skill`；不按自然语言或关键词自动挑选 Skill，也不接受 `latest`。规划前、Runtime 前和真实工具调用前均重新解析与校验绑定，执行目录只有一个 Workflow Tool，完成态必须同时具备成功 Tool Observation 和符合 JSON Schema 的结构化输出。
- Proto/gRPC/Gateway/Web 增加租户隔离的 Skill 列表、精确版本查询、执行选择和 Run 审计字段。`AGENT_SKILL_CATALOG_ENABLED` 默认关闭且要求 Workflow-as-Tool 已启用；关闭后立即撤销目录和路由，不修改发布记录或普通会话数据。
- 新增独立 `agent_task_templates` 事实源，但它不是第二套 Workflow。用户只能从自己的 `completed` 权威 Agent Run 显式创建，并自行提供含唯一 `{{input}}` 的指令；服务端不读取源对话或输出正文，只保存源 Run Revision、结果摘要、执行 Profile、Capability、Skill/Profile/Prompt 版本等证据。
- 模板内容创建后不可变，创建支持租户级幂等键，归档通过 Revision CAS。执行时重新核验模板状态、源 Run 完成证据和当前 Catalog 路由，再进入统一 RunAgent；模型和用户级 Provider Config 由当次请求选择，模板不能固化密钥或授予新 Tool 权限。新 Run 保存模板 ID/Revision 审计。
- 第一、二增量的目标普通测试、Skill/Service Race、受影响包 Vet、Web 生产构建、Compose、Helm Lint、开启态渲染及非法开关组合拒绝均通过。第三增量的 Repository/Service/gRPC/Gateway/Router/Cmd 测试、Repository/Service/Gateway 目标 Race、独立串行 Vet、Web 生产构建和部署配置验证通过；应用内浏览器使用一次性契约夹具检查任务模板选择与归档入口，桌面和 `390x844` 视口均无水平溢出。本地完整 Agent API 依赖未启动，因此 Skill/模板的真实 Mongo、模型和子 Workflow 调用仍需受控集成环境补充端到端证据。
- 第四增量把 Agent 与 Workflow 的预算上限、Step/Node、精确或估算 Token/成本和 Pricing Version 直接写入各自权威 Run，并以 `execution.accounting.v1` 区分新旧证据。新增窄仓储按 `user_id + parent_run_id` 专用索引有界查询直接子 Workflow Run；服务聚合父级自身、子级和总计，价格版本冲突标记 `mixed`，旧记录、运行中与截断数据显式返回 `partial/unavailable`。
- 聚合接口和 AI 助手“用量”弹窗均为只读观测，不解析业务 `output_json` 或 Trace，不递归，也不把父子独立准入预算改造成共享硬上限。Agent/Gateway 扩大回归、目标 Race、独立 Vet、Web 生产构建及 `1280x800`/`390x844` 布局检查通过；本地无可用模型和完整 Agent API，真实 Mongo 父子运行仍待受控集成验收。
- 第五增量为 Runtime 增加通用、版本化 Tool Continuation，父 Agent 以加密 Checkpoint 保存子 Workflow Resume Token、父 Action 和发布版本绑定。只读 Workflow 的显式人工输入 Wait 可挂起父 Run并恢复同一子 Run；恢复前重查当前 Active 发布、Revision/DSL Hash、谱系与 Action，子 Run 已完成时从权威 Blackboard 幂等回放，不重放模型或已成功 Action。
- Workflow Editor 的 Wait 明确区分人工输入与外部回调，删除用户 Resume Token 配置；移动端组件库改为抽屉、节点属性改为覆盖层，并支持点击添加。Agent 全模块回归、目标 Race/Vet、Web Build 与桌面/移动浏览器交互验证通过；本地 Workflow Catalog API 500 仍阻断真实依赖链端到端验收。
- 第六增量增加 `delegated_tool_approval` Continuation。子 Workflow 独占审批事实、输入摘要、幂等键和一次性 Grant；父 Agent 只在加密 Checkpoint 保存子审批引用。审批中心批准子请求后以子 Grant 恢复父 Agent，父 Runtime 在原 Tool Action 内恢复同一子 Run，不产生第二条审批或父级授权。
- 发布准入只对具备完整恢复桥的审批型 Workflow 开放：写工具还必须声明幂等，风险工具固定逐次审批，外部 MCP 继续重查当前 Snapshot/Policy。恢复复验父子谱系、Action、发布版本/Hash、审批绑定和 Grant；子成功/父提交中断从 Blackboard 回放，拒绝或过期同步终止父子 Run。
- 第六增量通过 Agent 全模块测试、Runtime/Service 拆分 Race、Agent Vet 与 Web 生产构建；集成测试验证审批前零写入、批准后单次写入、父子完成及令牌重放拒绝，并覆盖非幂等写发布拒绝和子审批拒绝级联。验证使用内存/Fake，不冒充真实 Mongo、模型或第三方 MCP live 证据。
- 第七增量新增独立 `agent/strategy` 纯领域包和 `agent.execution_strategy.v1` 脱敏证据。Planner 只在精确研究/草拟 Capability 路由上评估有界复杂度信号；高复杂度任务进入 Multi-Agent 候选后继续验证角色/并发、Profile Tool Scope、Step/Token/成本和延迟，任一不满足即按稳定原因码回退，计划本身不授予工具。
- 权威 Agent Run、Proto、Gateway 与 Web DTO 持久化/返回候选、实际选择、稳定原因码、角色预算和摘要。`AGENT_MULTI_AGENT_PLANNER_ENABLED` 控制准入，`AGENT_MULTI_AGENT_EXECUTION_ENABLED` 独立控制真实聚合执行；执行开关关闭时仍以 `multi_executor_unavailable` 回退原单 Agent 路由，不调用旧硬编码 Multi 流水线。
- 第七增量的 Planner/跨层契约/Agent 全模块普通测试、受影响包 Race/Vet、Web Build、Compose 与 Helm 配置验证通过；验证不连接真实模型、Mongo、搜索 Provider 或第三方 MCP，也不把影子候选冒充多角色执行证据。
- 第八增量安装首个有界聚合执行器：仅对 `platform.research_draft.v1` 与 `web.research_draft.v1` 运行顺序 `researcher -> drafter -> reviewer`。三个角色使用独立不可变 Profile、消息视图、Tool Scope、Token/成本/超时预算；研究角色在父 Profile、当前 Catalog、角色 Profile 和计划 Tool Scope 的交集内调用只读工具，起草与审校角色无工具。交接物仅包含有界研究摘要和受信任 Structured Content 投影出的 Citation，完整历史只进入研究角色。
- 三个角色复用现有 Runtime、Model Router 与 ToolExecutor，并通过共享 `BudgetTracker` 受父级累计 Token/成本约束。权威 Agent Run只保存聚合 Usage、Step 数、预算和计划摘要；LLM/Tool/Step Trace 使用父 Run ID 与角色前缀关联。角色失败、挂起或请求审批时父 Run整体失败，不在已经消耗预算后自动重跑单 Agent；准入阶段不满足条件仍安全回退。
- 第八增量的成功/失败执行测试、Agent 全模块与 Cmd 普通回归、Runtime/Strategy/Service Race、受影响包 Vet、Compose 与 Helm 默认/合法/非法组合验证均通过。测试使用 Fake Runtime/Tool 和离线配置，不构成真实模型、Mongo、搜索 Provider、第三方 MCP 或生产 P95 证据。
- 第九增量在既有 `agent-task-eval` 内增加 `agent.strategy-comparison.v1`：只有数据集、Case/模板覆盖、Provider、Model、Pricing Version、环境、Seed 与 Case Timeout 一致且成本/P95 证据完整时才计算倍率。默认要求 20 条任务、多角色语义通过率不低于 90% 且至少提升 500 bps、任务/工具零回归、平均成本不超过 3x、P95 不超过 3.5x/60 秒，并继续拒绝错误、预算终止、越权写与工具结果伪造。
- 新增 20 条固定研究草拟任务和单/多录制夹具，只覆盖当前两个只读顺序模板。策略门禁复用报告 HMAC 与 Object Lock 归档；CLI 分离“计算”和“强制阻断”，新增报告字段缺省不编码，避免破坏旧 v2 报告的规范化验签。录制夹具的质量增益、成本和 P95 只证明评分契约，不是模型或生产性能证据。
- 第九增量通过 Agent 全模块与两个命令入口普通回归、Eval/Cmd Race、Vet、签名兼容测试和完整离线双门禁命令。录制结果为 2000 bps 语义增益、2.3838x 平均估算成本和 2.5410x P95；这些刻意构造的数值只用于验证计算、阈值和非零退出，不能进入产品或面试中的真实性能声明。
- 第十增量抽出 `agent/multirole` 存储无关核心，生产 Service 与 Eval 复用同一顺序角色隔离、结构化证据交接、共享父预算、聚合 Usage/Step 和失败即停语义。Service 保留目录、Citation、Trace 与权威 Run 持久化责任，避免评测重新实现一套“看起来相似”的编排器。
- `agent-task-eval --strategy-runtime-config` 在同一规范化 Provider、Model、Pricing 和 Profile Snapshot 配置内自动执行 Multi 候选与 Single 稳定侧；完整配置与 Executor Version 生成统一 SHA-256，不同哈希直接不可比较。配置拒绝明文凭据，工具结果为无副作用结构化证据，Web Case 允许在声明集合内按需 `page_read`。
- 第十增量的领域测试和本地 OpenAI-compatible HTTP 集成已验证角色隔离、父/角色预算预检、失败不回退、六次真实模型协议调用、双侧报告、配置哈希与 HMAC 验签；Multirole/Eval/CLI 与 Service 关键路径 Race、受影响包 Vet、Agent 全模块、整仓测试和离线双门禁命令均通过。它仍未使用真实外部搜索或 20 Case Provider 运行，不能表述为生产质量、召回或 P95 证据。
- 第十一增量为长时间 Live Eval 增加真实 Chat/`eval_preflight` Tool Call 探针、执行错误快速终止、逐 Case 脱敏进度和连续前缀恢复。Candidate/Stable 检查点使用隔离目录，记录逐条 HMAC 签名并链接上一 Payload SHA-256；恢复严格绑定数据集、配置、环境、Seed、Timeout 与执行描述，不保存输入、模型正文或工具载荷。Provider 在 Case 中途失败时当前 Case 不固化，修复后同命令从最后签入位置继续。
- 第十一增量的核心恢复测试、签名/篡改/身份漂移测试与本地 OpenAI-compatible 端到端故障恢复通过；故障用例确认第二 Case 503 后只保留第一条证据，重跑仅执行第二 Case并生成签名报告。Eval/CLI 串行 Race、受影响包 Vet、Agent 全模块串行回归和 20 Case 离线双门禁均通过；首轮组合 Race 的 Windows 编译器派生权限抖动已记录并串行复跑闭环。上一轮真实冒烟在约 5 秒内预检失败且未产生半成品，后续确认原因为用户主动关闭 LM Studio，不属于项目故障；用户随后报告已加载固定 Chat 模型，Live 命令仍须在再次确认后执行，因此尚未生成真实 20 Case 报告。
- 第十二增量在真实 LM Studio 预检中发现协议边界：未声明 `tool_choice` 时，`qwen2.5-3b-instruct` 会把正确的函数调用 JSON 作为普通正文返回。Runtime 新增 `ToolChoice` 与仅首轮生效的 `InitialToolChoice`，OpenAI-compatible Adapter 映射标准字符串；预检、Single Research 和 Multi Researcher 首轮要求工具调用，后续轮次及普通对话不受影响。Runtime/Model/Multirole/Eval CLI 定向回归通过，真实 Adapter 与 Catalog Router 探针返回标准 `tool_calls`。
- 经用户确认后，固定 LM Studio `qwen2.5-3b-instruct`、`controlled-accounting-v1`、两个模板的完整 Profile Snapshot、20 条数据集和 90 秒 Case Timeout，完成 Multi Candidate 与 Single Stable 共 40 次真实模型 Case。两侧任务完成率和工具成功率均为 100%，Multi 工具选择准确率 `90%` 高于 Single 的 `75%`；但 Multi 语义通过率 `60%` 低于 Single 的 `90%`，平均成本 `2.6336x`，P95 `3.7475x`，因此策略门禁以语义和延迟原因拒绝晋级。失败报告保留数据集/配置哈希、逐 Case HMAC 链并通过 `--verify-report`，没有通过调阈值或重写历史证据伪造收益。
- 本阶段仍不是开放技能或任意 Multi-Agent 市场：只读顺序模板之外的角色拓扑、并行角色、角色级 Checkpoint/Resume、写工具与审批仍未开放。首份真实报告还缺 Object Lock Version ID；下一次资格评测必须采用新的通用 Profile Revision 或更强固定 Chat 模型与全新配置哈希，通过、人工复核并 WORM 归档后，再单独验证生产 Search/Page Read，随后才能决定扩大模板或并发范围。
- 第十三增量增加原子 Profile Set：研究父 Profile 是唯一 Release Anchor，研究员/草拟者/审校者按父版本从同一个不可变 Catalog 快照精确解析，角色独立 Release 被拒绝。父策略选中的 Anchor/Version 写入执行计划并参与摘要，执行阶段按固定版本重取整套配置；Catalog 在规划与执行之间刷新也不能改变本次 Run，Multirole 在模型调用前再次拒绝混合版本。
- 通用 Profile v2 消除“默认三候选”与“只输出一份”的冲突，默认给一份完整成稿，只有用户明确要求才附研究摘要、多候选或适用场景；结构化证据、精确术语、相关性和默认内容量成为 Single/Multi 共用契约。Eval 增加受限 `reasoning_mode` 映射，并提供固定 `qwen3.7-plus-2026-05-26`、DashScope `enable_thinking=false`、Profile Set v2 和版本化费率的云候选配置。离线代码已验证；真实调用等待用户设置环境变量并确认费用，旧 qwen2.5 失败证据不覆盖。

P8.4 第十四增量实施记录（2026-08-01）：

- 真实 qwen3.7 运行暴露模型在多轮工具观察后会持续请求工具。Runtime 现把最后一步保留为显式终态边界：移除 Tool Catalog/Tool Choice，追加高优先级收束 System 消息；Provider 若仍返回非终态动作，Runner 在执行工具前失败关闭。`InitialToolChoice=required` 同时要求至少两个总步骤，避免没有最终答案步的非法预算。
- 首轮云报告进一步暴露固定数据集的 10 个 `allowed_tools` 被重复写入第一个 Web Case，导致其余 Case 将合法 `page_read` 误判为工具回归。数据集加载器现递归拒绝重复 JSON Object Key，夹具测试逐 Case 校验 `web_search` 必需、`page_read` 可选；修复契约升级为 `agent-strategy-cases-v2`，旧 v1 云报告保留为缺陷证据而不参与资格判定。
- 经用户明确授权固定合成数据出站和费用后，使用 DashScope `qwen3.7-plus-2026-05-26`、`enable_thinking=false`、Profile Set v2、版本化 CNY 费率和 90 秒 Case Timeout 完成 Multi/Single 各 20 Case。Candidate 的任务完成、读工具选择、语义和工具成功率均为 100%；Stable 语义为 95%，其余对应指标为 100%。Candidate 相对 Stable 语义增益 500 bps、平均估算成本 `1.0714x`、P95 `0.8870x`，策略门禁通过。
- 报告 `tmp/agent-task-eval/live-strategy-qwen37-20260801-v7.json` 绑定数据集 SHA-256 `55f14d6f...10e46`、执行配置 SHA-256 `429f5e44...63e60`、执行器 `agent-strategy-runtime/v4` 与 Key ID `local-live-eval-20260801-v3`，独立 HMAC 验签通过。一次 Stable Case 的 Provider 请求超时通过同身份签名检查点只恢复未完成后缀，没有重跑/挑选已完成 Case。
- 该资格只覆盖无副作用确定性证据沙箱中的两个只读顺序模板。人工抽检与 MinIO Object Lock Version ID 尚未完成，生产 Feature Flag 不自动开启；真实 Web Search/Page Read、任意模板、并行角色、角色级 Checkpoint/Resume 和写工具多角色协作仍需后续独立阶段。

P8.4 第十五增量实施记录（2026-08-01）：

- 评测报告和逐 Case Checkpoint 继续只保存输出摘要、字符数与评分，避免把模型正文扩散到长期证据或恢复链。人工复核正文由 `cmd/agent-task-eval/review_bundle.go` 在 CLI 组合层用 Executor 装饰器短暂采集，不修改生产 Runtime、Eval 领域报告或 Object Store 契约。
- `--review-bundle` 只允许无 Checkpoint 的 Live 单/多策略对照，要求显式 `--allow-review-content`、质量与策略双门禁强制模式、独立 base64 32 字节 Review Key/Key ID 和不可覆盖的新报告/Bundle 路径；禁止与归档同跑。只有最终签名报告双门禁通过后才生成 AES-256-GCM Bundle。
- Bundle 的 AAD/密文共同绑定最终报告 Payload SHA-256、报告 Key ID、数据集版本/哈希、两侧执行描述及逐 Case 完整评分和正文哈希；外层不含输入或输出。`--open-review-bundle` 必须同时提供原签名报告、报告 HMAC Key 和 Review Key，先验签再解密并逐 Case 核对，明文只写新建的本地敏感文件，不进入终端或普通日志。
- 该能力解决“脱敏报告通过后无法人工阅读真实回答”的流程缺口，但不把打开 Bundle 等同于人工签认、审批或发布。现有 qwen3.7 v7 报告没有正文 Bundle，哈希不可逆，必须在用户重新确认费用与敏感内容捕获后使用全新路径完整重跑；人工结论与 WORM Object Lock Version ID 仍未完成。
- 单元测试和本地 OpenAI-compatible HTTP 夹具覆盖密文不泄露、报告绑定篡改拒绝、Key 格式、文件不可覆盖、显式授权、双模板 Candidate/Stable 双门禁及受控打开；目标测试通过，未连接真实模型、MinIO 或公网。

P8.4 第十六增量实施记录（2026-08-01）：

- 经用户确认固定合成评测数据出站、敏感正文捕获和 qwen3.7 费用后，以全新路径、无 Checkpoint、质量/策略双强制门禁完成 Candidate/Stable 各 20 Case。签名报告与 AES-256-GCM Bundle 分别绑定数据集 `55f14d6f...10e46`、执行配置 `429f5e44...63e60` 和报告 Payload `f1f96e91...267eb`；独立 HMAC 验签、Bundle 解密、报告摘要绑定及逐 Case 正文哈希校验全部通过。
- 自动指标为 Candidate 20/20、Stable 18/20；Candidate/Stable 语义通过率 `100%/90%`，平均 Token `3065.90/3523.95`，估算成本 `0.190004/0.184500 CNY`，P95 `16166/17356ms`。策略门禁记录语义增益 1000 bps、平均成本倍率 `1.0299x`、P95 倍率 `0.9315x`，总估算费用约 `0.374504 CNY`。
- 对全部 40 份正文执行机器辅助内容审阅，未发现跨主题混入，但发现评测证据本身只由 Case 输入与必需关键词拼接。Candidate 的 12/20 因识别到证据空洞而返回“证据不足”并夹带 Tweet ID、`example.com` 或 `controlled-eval` 等占位元数据；其余 8/20 虽更可读，核心事实也无法由夹具验证。Stable 大量以模型常识补写，出现未受检索结果支撑的机构/事件归因，另有 2/20 超过 800 字符。
- 因而本轮结论是“自动门禁通过、内容资格不通过/不可判定”，不是 Multi-Agent 生产晋级证据，也不是外部人工签认。签名报告保留为评测设计缺陷证据，暂不作为 WORM 晋级对象；生产 Feature Flag 保持关闭。
- 下一增量先将数据集升级为有实质内容的结构化证据，显式定义可验证 Claim/Citation、证据不足的合法结果、最终交付可用性、内部评测元数据泄漏和 groundedness 门禁，并以离线 Fake/录制结果验证。只有该契约稳定后才向用户申请下一次云模型费用和敏感内容复核；通过外部人工签认后再做 Object Lock 归档与真实 Brave/Page Read 验收。

P8.4 第十七增量实施记录（2026-08-01）：

- 在 `AgentTaskCase` 上增加可选且仅属于数据集的 Evidence Contract，分为 `sufficient/insufficient`。加载器对数量/字符预算、ID、URL、重复值、未知 Citation 以及 Claim Terms 是否由同一条已引用正文共同支撑做严格校验；旧 Case 不提供该字段时行为不变。
- 内容评分在原关键词/长度之外增加 Claim Coverage、精确方括号 Citation、声明与引用 240 字符邻近度、充分证据拒答、证据不足通知、固定无依据声明和内部元数据泄漏规则。新增判断继续复用 `semantic_failure_codes`；报告 schema 保持 `agent-task-eval-report/v2`，Evidence 原文不进入报告或 Checkpoint，历史签名不失效。
- 新增独立 `agent_strategy_cases_v3.json`：16 条有实质结构化事实的站内/公网任务与 4 条合法空证据任务；原 v2 数据集继续保留。新增固定 qwen3.7 Profile Set v3，四个角色都要求事实声明旁保留精确 Citation，空结果不得从模型先验补写。
- Eval Runtime/Strategy Executor 升级为 v5，把 Case Evidence 投影到现有 Platform Search、Web Search 和 Page Read Structured Content；Page Read 精确匹配白名单 URL。Multi Handoff 使用契约 Citation ID，空结果用明确 `no-evidence` 控制记录满足现有非空 Handoff 不变量，但不冒充来源。
- 20 条 Grounded Fake、错误契约/语义失败矩阵、Tool 投影、URL 白名单、空结果 Multi Handoff、定向普通测试与 Race 全部通过；`eval/evidence/multirole/runtime/agent-task-eval` 五包测试和目标 Vet 通过。本轮未调用任何模型或外部服务。全量 Agent Vet 在 184 秒硬超时前无诊断，按影响范围拆分后通过，不能把超时写成整包 Vet 通过。
- 迁移只需选择新的 v3 数据集与 Profile 配置；回滚选择原 v2 文件即可，不迁移数据库、不修改生产 Tool Schema、不删除历史报告。确定性门禁不是事实 Judge；下一轮先实现与报告 Payload/DataSet/Rule Version 绑定的外部人工/Judge 签认，再经用户确认费用执行全新 v3 云模型 20+20，未通过前保持生产 Feature Flag 和 WORM 晋级关闭。

P8.4 第十八增量实施记录（2026-08-01）：

- 新增独立 Decision/Signoff/Rule v1。Signoff 绑定报告 Payload 与 Key ID、数据集版本/哈希、两侧执行配置哈希、Review Bundle Schema/Key ID/文件哈希、决策哈希、审阅时间和逐 Case 输出哈希；报告 v2 与历史 HMAC 不变。
- 每个 Candidate/Stable Case 强制记录事实正确性、相关性、证据忠实度和写作质量二值结论。缺失、重复、错序、非法状态、聚合结论不一致、未知/重复 JSON Key、报告或 Bundle 替换都失败关闭；产物不保存正文或自由文本。
- `external_human` 只保存假名 ID、声明式身份保证和外部记录摘要；`judge` 必须绑定 Provider/Model/Prompt/Config SHA-256 且只作为辅助信号。人工批准仍只是生产资格必要条件，不能替代自动门禁、身份治理、WORM 与真实搜索验收。
- CLI 新增离线创建/验签模式：重新验报告 HMAC、在内存中解密并逐 Case 核验 Bundle，再使用第三把独立 HMAC Key；拒绝密钥和 Key ID 复用，写入使用不可覆盖路径。默认决策模板把 20 条 v3 Case 全部标为拒绝，避免示例默认放行。
- 迁移为纯旁路新文件，无数据库、HTTP/gRPC 或报告 schema 迁移；回滚只需停止创建/消费 Signoff。五包普通测试、Eval/CLI Race 和五包 Vet 通过；本轮未调用模型或外部服务。下一步须经用户重新授权费用和正文捕获后执行全新 v3 20+20，再由真实外部人工产生签认。

P8.4 第十九增量实施记录（2026-08-02）：

- 新增 `agent-task-content-qualified-evidence/v1`，将签名报告与外部人工已批准 Signoff 封装为一个 WORM 对象；双 HMAC、数据集/执行配置、Review Bundle 文件摘要、逐 Case 输出摘要和四维结论在归档与读取时重新验证。Judge Signoff 与旧裸报告不能通过严格内容资格模式。
- CLI 归档资格对象前必须重新解密加密 Review Bundle 并逐 Case 核验，且报告 HMAC、Review AES 与 Signoff HMAC 的 Key/Key ID 必须两两独立。WORM 回读不持有正文解密 Key，只验证归档对象摘要和双 HMAC。现有回执 schema 不变，`report_sha256` 在新模式下代表整个资格对象摘要。
- Profile 发布新增默认关闭的 `AGENT_PROFILE_EVAL_CONTENT_SIGNOFF_REQUIRED`，依赖原证据开关；开启后提交、批准和失败恢复只接受外部人工批准资格对象。回滚新开关即可恢复旧报告门禁，无数据库/Proto/Gateway/Web 迁移，也不改写 Object Lock 历史。
- Compose、Helm、环境样例、Eval 文档和 Profile 运维文档已同步。定向普通测试、Helm 默认渲染和 Compose 解析通过；第十九增量本身未启动模型、MinIO 或其他外部服务。真实 v3 20+20 已在第二十增量执行；外部人工记录和 WORM Version ID 仍待完成。

P8.4 第二十增量实施记录（2026-08-02）：

- 为失败评测增加显式加密诊断路径：`--capture-failed-review-bundle` 只能与 Review Bundle 和正文授权组合，失败报告仍可绑定 AES-256-GCM Bundle 供受控排障；Signoff 领域继续强制自动质量门禁和策略门禁同时通过，因此诊断材料不能被包装成生产资格。
- 使用固定 `qwen3.7-plus-2026-05-26`、同一 20 条 v3 Evidence 数据集和不可覆盖路径完成三次 Candidate/Stable 20+20。Profile v3 为 `12/20` 对 `8/20`，Profile v4 为 `16/20` 对 `15/20`，均诚实失败于 Candidate 绝对语义率；加密诊断把问题从空证据过短/错误拒答收敛为个别数字、单位、否定结论和治理约束遗漏，未降低 90% 门槛或弱化 Claim/Citation 评分。
- 新增不可变 Profile Set v5：Single 稳定侧保留 v4 Prompt 正文、工具权限和预算，仅按原子集合契约同步版本元数据；Multi 三角色增加事实 Coverage Unit 和输出前静默核对，精确保留来源中的标识符、数字+单位、成对数值、零/否定结果、限制与 guardrail。首次启动在正式 Case 前因混合 Profile 版本失败关闭，只产生预检调用；修正后测试保证整套版本一致且 Stable 其余字段不变。
- v5 第三轮 Candidate `19/20`、Stable `15/20`，语义率 `95%/75%`、增益 `2000 bps`，平均成本倍率 `1.7911x`、P95 倍率 `1.9362x`、Candidate P95 `15139ms`；任务、工具选择和工具成功率均为 100%，质量与策略门禁同时通过。唯一 Candidate 失败只遗漏证据中的精确短语“读写权限”，Citation 与必需关键词均保留。
- 第三轮报告估算费用 `0.274938 CNY`，三次完整报告累计 `0.814168 CNY`；两次额外预检不进入报告，最终账单可能略高但预计仍在用户授权的 `1 CNY` 内。报告 HMAC、Payload `dc2b2500...971fb`、Bundle `1f8e5879...684b2`、DPAPI 密钥和 40 份正文哈希均独立复验通过，正文未打印到终端。
- 自动门禁通过只把任务推进到独立外部人工复核，不等于 `external_human_approved`、WORM 晋级或生产发布。下一步由独立人工逐 Case 完成四维 Decision/Signoff，再用严格资格对象归档 Object Lock 并验证真实 Search/Page Read；在此之前继续关闭生产 Multi Feature Flag、并行角色、角色级恢复和写工具多角色执行。

P8.4 第二十一增量实施记录（2026-08-02）：

- 新增 `agent-task-live-authorization/v1`。独立 CLI 模式只读取并哈希数据集与 Runtime 配置，签名绑定 Provider/Model、数据集版本/SHA、执行配置 SHA、有效期，以及最大 Run、Provider 调用、正文捕获和估算成本；创建授权不构造 Provider Client，也不发起网络请求。
- 所有 Live Eval 现在必须同时提供授权文件、受保护的持久 State Root 和独立授权 HMAC Key。每次运行先预留 Run/正文数量，每次模型调用按保守 Token 上界和固定 Pricing 在委托 Client 前预留调用/估算成本；事件形成不可覆盖文件、HMAC 签名和前向哈希链。进程崩溃不退款，授权过期、身份漂移、篡改、序号缺口或预算耗尽均在出网前失败关闭。
- `agent-task-eval-report/v2` 以可选字段携带授权 ID、授权 Payload 摘要、Key ID、调用实例摘要和批准上限，并纳入既有报告 HMAC；旧离线报告省略该字段，签名形状和验签保持兼容。授权密钥与报告/Review 密钥复用会被拒绝。
- 打开加密 Review Bundle 时可用 `--review-decision-template` 生成绑定报告和 Bundle 的外部人工 Decision 初稿。模板不复制正文或输出哈希，所有四维结论和总 Verdict 默认失败，Reviewer ID、外部记录摘要与审阅时间留空，因此不能未经真实人工处理直接创建 Signoff。
- Eval/CLI 定向普通测试与竞态检测、跨 Ledger 实例并发消费 20 次重复测试、完整 Agent 模块串行回归及 Agent/CLI Vet 通过；测试覆盖授权身份/过期、预算耗尽、账本篡改、Provider 调用前拦截、旧报告兼容、报告授权证据防篡改和 Decision 模板失败关闭。本轮没有连接 DashScope、LM Studio、MinIO、Mongo、MCP 或公网。当前账本只保证同一受控 State Root 内的本地原子消费，不冒充跨主机中央配额服务。

P8.4 第二十二增量实施记录（2026-08-02）：

- 新增纯离线 `agent-task-live-plan/v1` 与 `--plan-live-evaluation`。计划只读取并规范化固定数据集和 Runtime 配置，不读取 Credential、不构造 Provider Client；输出 Provider/精确 Model、数据集/配置 SHA、Candidate/Stable 分侧预算、共享预检、调用最小值/上界、Token 硬预算、费用授权上界和 Review 正文上限。
- 调用最小值按可完成闭环计算：只读工具任务至少包含 Tool Call 与最终回答；多角色候选还包含独立 Drafter/Reviewer。调用上界按每个 Case 实际模板的 Profile `MaxSteps` 汇总，Token 与费用按 `MaxTotalTokens`、`MaxEstimatedCostMicros` 和固定 Pricing 汇总。授权签发现在要求完整覆盖调用与费用上界；正文数量为 `0` 表示禁止捕获，非零则必须覆盖完整 Review Bundle，避免签出注定中途耗尽的批次。
- 固定 `qwen3.7-plus-2026-05-26`、20 条 `agent-strategy-cases-v3` 与 Profile Set v5 的离线计划为最少 `121`、最多 `241` 次 Provider 调用，Token 硬预算上界 `1,240,482`，估算费用上界 `4,701,348` 微计价单位，Review 上限 40 份正文。该数字是跑满预算的授权天花板，不是实际预计账单；计划产物位于忽略目录 `tmp/agent-task-eval/qwen37-v5-20260802.live-plan.json`。
- 模型身份明确绑定 `provider + exact_model + execution_config_sha256`。从固定快照切换到滚动别名会改变配置哈希，必须重新生成计划、授权和资格报告；历史报告保持原模型作用域，不允许只改名字后继续消费旧授权或混入旧稳定基线。CLI 普通测试与 Race、完整 Agent 模块串行回归及受影响包 Vet 通过，且未访问 DashScope、LM Studio、MinIO、Mongo、MCP 或公网；独立外部人工 Signoff、WORM Version ID 和真实 Search/Page Read 仍是后续生产资格门槛。

P8.4 第二十三增量实施记录（2026-08-02）：

- 在不修改默认 file 账本和通用 Runtime 依赖方向的前提下，为 `agent-task-eval` 增加可选 Redis 共享预算后端。严格配置只允许非秘密连接元数据与 Username/Password 环境变量引用，TLS 必须显式 Server Name，所有连接和命令都有硬超时；示例配置独立放在 Eval testdata，不把部署凭据写入仓库。
- 新增独立 `--initialize-live-authorization-state`：只验签既有授权并创建中央状态，不读取模型 API Key、不构造 Provider Client。Redis Live 执行不具备隐式初始化能力，必须命中已存在且授权 Payload、Key ID、有效期和四类上限完全一致的状态；未初始化或身份漂移在 Provider 调用前失败。
- 预算消费由 Redis Lua 原子事务完成：使用服务端时间校验授权窗口，在同一事务检查 Run/Provider Call/正文/估算成本上限，累计 Hash、追加 Stream 审计并记录 Reservation 摘要。不同连接并发共享一份额度，客户端重试同一 Reservation 时幂等返回，冲突重放和超限均失败关闭。
- 为每个授权保留无 TTL marker。State 或 Stream 被提前删除/驱逐时，消费和再次初始化都会返回“撤销授权”，不会把旧授权恢复为零用量。成功报告可选绑定 `state_backend=redis` 和 Namespace SHA-256；旧报告因字段 `omitempty` 继续按原 v2 Payload 验签。Redis 管理员及数据库级清空属于信任边界，部署要求 ACL、TLS、AOF/备份、`noeviction` 和受控 marker 清理。
- CLI/Eval 定向普通测试与 Race、不同 Redis Client 并发、重试幂等、状态丢失/身份篡改、未初始化调用前拦截、完整 Agent 模块串行回归和目标 Vet 通过。全部使用 `miniredis`，未访问用户 Redis、DashScope、LM Studio、MinIO、Mongo、MCP 或公网。该增量只加强费用授权基础设施，不绕过独立外部人工 Signoff、WORM 与真实搜索资格门。

P8.4 第二十四增量实施记录（2026-08-02）：

- 为 Redis Live 授权账本增加与运行路径隔离的 `inspect`/`revoke` 管理模式。检查输出使用版本化 JSON，只暴露授权/Namespace 摘要、状态、额度、累计用量、审计序号和 Redis 服务端时间，不输出 Endpoint、凭据、Prompt 或模型正文；两个模式都不读取模型 Credential、不构造 Provider Client。
- 正常撤销在 Lua 原子事务内把 marker 和状态置为 `revoked`，保留累计用量、递增序号并追加 `authorization_revoked` Stream 事件。操作人只保存 SHA-256，原因使用固定枚举；重复撤销返回原始撤销事实且不重复写事件，撤销后的初始化和所有预算预留在 Provider 前失败。
- State/Stream 部分或全部丢失时只允许 `state_integrity_incident`。命令仅把无 TTL marker 冻结为 `marker_only` 并报告 `state_lost`，不重建 Hash/Stream、不把未知历史消费重置为零，也不把 marker-only 结果表述成完整审计链。
- CLI/账本普通测试与 Race 覆盖额度快照、用量保持、并发预留/撤销串行化、撤销幂等、操作人脱敏、双状态防误恢复、状态丢失原因门禁和不重建状态；并发用例连续 20 次通过，完整 Agent 模块串行回归与目标 Vet 通过。验证使用进程内 `miniredis`，未连接真实 Redis、模型 Provider、MinIO、Mongo、MCP 或公网。外部人工 Signoff、WORM Version ID 和真实搜索资格门保持不变。

P8.4 第二十五增量实施记录（2026-08-02）：

- Redis 管理检查不再只信任 `XLEN == sequence + 1`。Lua 在同一快照原子返回 Hash 用量、事件数和末端 Stream ID，Go 以 512 条为一页、30 秒总超时重放到固定游标；检查期间更晚写入的 Reservation 不会改变该快照的判断或摘要。
- 重放器验证精确字段集合、连续业务序号、严格递增 Stream ID、授权窗口内单调时间、合法 Run/Provider Call 增量、四类累计用量和唯一可选撤销终态，并与 Hash/marker 撤销事实逐项对账。通过后管理 JSON 增加 `replay_status=verified`、`verified_event_count`、`last_stream_id` 和包含 Stream ID 的规范化 `stream_sha256`。
- 写路径在 Go 与 Lua 两层固定事件形状，拒绝混合 Run/Provider Call 增量和无效 Subject 摘要。测试覆盖保持总长度不变的删/补篡改、跨 512 条分页、快照后并发追加隔离和稳定摘要；关键用例 20 次重复、Redis Race、完整 CLI/Agent 回归和目标 Vet 均通过。这些检查提高损坏发现能力，但 SHA-256 不是 HMAC，不能替代 Redis 最小权限、外部 WORM 或管理员不可抵赖审计。
- 本增量仅使用 `miniredis` 和离线测试，不连接用户 Redis、DashScope、LM Studio、MinIO、Mongo、MCP 或公网，也不产生模型费用。40 份正文外部人工 Signoff、Object Lock Version ID、真实 Search/Page Read 和公网 P95 仍是生产资格硬门槛。

P8.5 第一至第五增量实施记录（2026-08-03）：

- 第一、二增量完成统一任务 SLI、草稿采纳与 Connector `configured/activated/first_used/reused` 的跨请求、可重放产品事实；Prometheus 只做低基数投影，不替代权威 Run/Connection/Event。
- 第三增量完成租户已安装的 Capability/精确版本 Skill/受治理 MCP Tool 联合目录，目录复用现有权限事实源且不新增执行路径。
- 第四增量新增独立发布者身份、规范 Manifest、确定性 Release ID、Ed25519 签名和不可变版本存储。Runtime 只依赖只读 Store，并在每次列举时重新校验发布者、Key、ID 与签名；公开 API/UI 不返回密钥、原始签名、Artifact URL/字节、Endpoint、Credential 或安装授权。
- 第五增量补齐 Marketplace 专属内部认证、平台管理员与 Publisher Owner、公钥轮换/吊销、Active Key 签名发布、Revision CAS、终态撤回和追加审计，并同步独立管理 API/UI 与部署门禁。公开/管理开关仍默认关闭，离线回归、拆分 Race、Vet、Proto、Web、Compose 和 Helm 验证通过。
- Artifact 分发、安装审批、依赖解析、恶意包扫描、Owner 转移和租户安装状态不再作为当前 P8 完成条件；这些能力按范围冻结进入需求驱动延期项。当前只保留真实 Mongo 多副本、Secret 轮换和撤销后目录可用性的受控验收。

## 5. 推荐开发顺序与里程碑

### 5.1 面试最小闭环

建议优先完成 P0-P3，形成 10-15 个有效开发日的第一里程碑：

1. 一个通用 AgentRunner。
2. 一个统一 ToolExecutor。
3. 一个 MessageBuilder + Token Budget。
4. 一套 Run/Step/LLM/Tool Trace 数据模型。
5. 一个真正端到端的 Human Approval 发布流程。
6. 一组离线测试和 20-50 条 Agent Eval 样例。

达到这一里程碑后，就可以在面试中稳定使用“可治理 Agent Runtime”定位。

### 5.2 第二里程碑

完成 P4-P6：

1. Workflow Engine 统一复用 Runtime。
2. 单写者 Blackboard、Revision、确定性恢复。
3. Agent Prometheus/OTel/Replay 控制台。
4. 共享 Episodic Collection 与 RAG Eval。

达到这一里程碑后，可以应对 Agent Infra、Go 并发、工作流引擎、RAG 工程化和分布式可观测性的深入追问。

## 6. 迁移计划

### 6.1 兼容策略

- 保留现有 gRPC/HTTP 接口，新增字段必须 optional 或具备默认值。
- P8 新增统一入口；现有五种前端模式只在迁移期保留，内部逐个切换到 Runtime，前端最终改为能力面板和自动化入口。
- MongoDB 旧集合不直接改名，新增 Trace/Approval/Revision 集合。
- Workflow DSL 旧版本通过 Migrator 转成 IR，不要求用户手工重画。
- MCP Server 工具名保持兼容，通过 Adapter 接入新 Registry/Executor。

### 6.2 切流顺序

1. `consult`：最适合验证 ReAct + Read Tool。
2. `assist`：验证 Draft 与 Write Approval 分离。
3. `workflow agent node`：验证 DAG 节点复用 Runtime。
4. `multi`：把硬编码 Search/Style/Writer/Review 改为 Profile + Workflow。
5. `chat`：最后接入 MessageBuilder/Memory，避免先影响最常用路径。

### 6.3 双轨验证

- 新旧路径使用相同输入构造离线 Golden Case。
- 线上开发环境可做 Shadow Run，但 Shadow Run 禁止执行写工具。
- 对比最终答案、工具序列、Token、延迟和错误分类。
- 达到验收阈值后再扩大 Feature Flag 范围。

## 7. 回滚计划

- 每种业务模式都能通过 `AGENT_RUNTIME_V2_MODES` 单独关闭新 Runner。
- DSL v2 保存时保留原始 DSL；Compiler 失败可退回 v1 执行器。
- Trace/Approval 新集合是追加式，不影响旧 Dialogue/Workflow Run 读取。
- RAG Collection 迁移使用双写/双读，切换失败可立即退回旧 Collection。
- Model Router 失败时回退请求中显式模型，再回退服务默认模型。
- Write Tool 新审批策略不可因降级被绕过；审批系统故障时采用 fail-closed。

## 8. 风险分析

| 风险 | 触发方式 | 控制措施 |
|---|---|---|
| 重构范围膨胀 | 同时改五种模式、Workflow、RAG 和前端 | 按入口切流，P1 只迁 Consult |
| 形成第二套基础设施 | 直接新增 toolkit/workflow 而不迁旧代码 | 演进现有 `workflow/tool` 与 `workflow/engine` |
| Trace 数据暴涨 | 全量保存 Prompt、Completion、Tool Result | 默认脱敏、采样、大小限制和对象存储引用 |
| Prometheus 基数爆炸 | user_id/run_id/error 原文作为 Label | 固定枚举 Label，身份进入 Trace |
| Token 估算不准确 | 不同 Provider tokenizer 不一致 | Provider Usage 优先，估算值显式标记 |
| 审批重复执行 | 网络重试、重复点击、Checkpoint 恢复 | 乐观锁 + 幂等键 + 一次性 Token |
| Workflow 非确定性 | 并发节点覆盖同一黑板字段 | 单写者 Reducer + 编译期冲突检查 |
| SSRF/密钥泄露 | 用户保存自定义 Base URL/API Key | Credential Reference + URL Policy + 日志脱敏 |
| 记忆污染 | 每回合总结、跨主题召回、重复写入 | Session 事件、幂等、阈值和来源元数据 |
| Temporal 过度设计 | 为短请求全部引入外部编排器 | 只用于长任务/审批/跨服务可靠执行，先统一 IR |

## 9. 统一验收门禁

每个阶段完成都必须满足：

- [ ] `go test ./internal/module/agent/... ./cmd/agent-service` 通过。
- [ ] 并发核心包 `go test -race` 通过。
- [ ] 前端 `npm run build` 通过。
- [ ] `git diff --check` 通过。
- [ ] 新增接口同步更新 `docs/API_REFERENCE.md`。
- [ ] 任务状态同步更新 `docs/PROJECT_PROGRESS.md`。
- [ ] 测试或运行失败同步记录 `docs/ISSUES.md` 并在修复后闭环。
- [ ] 新增指标、日志和 Trace 已验证，不存在敏感信息泄露。
- [ ] 迁移和回滚路径至少完成一次本地演练。

Runtime 性能目标不包含外部模型和工具耗时：

- Runner 状态转换 P95 < 5ms。
- MessageBuilder 在 200 条消息输入下 P95 < 10ms。
- 取消后所有本 Run goroutine 在 1 秒内退出。
- Tool Timeout 后无后台继续写入。
- Checkpoint 恢复不会重复执行已成功的非幂等 Step。

## 10. 面试材料交付清单

代码完成之外，还要留下可展示的工程证据：

- [ ] `docs/agent_infra_design.md`：目标架构、执行链路和模块职责。
- [ ] `docs/agent_runtime_sequence.md`：Chat/ReAct/Approval/Resume 四张时序图。
- [x] `docs/agent_eval.md`：数据集、指标、固定契约夹具基线、回归门禁与真实 Profile 基线待办。
- [ ] `docs/rag_eval.md`：BM25/Vector/RRF/Rerank 对比。
- [ ] `docs/agent_security.md`：身份注入、Tool Policy、Approval、SSRF 和密钥管理。
- [x] `docs/agent_observability.md`：Trace 数据模型、Metrics、安全采样和 Grafana 面板。
- [ ] 一份 10 分钟演示脚本：检索 -> Planner -> ReAct -> 审批 -> 发布 -> Trace 查看。
- [ ] 一份失败演示：工具超时或模型 429 -> Fallback/预算终止 -> Run Trace 定位。

## 11. 推荐代码阅读顺序

### 当前代码

1. `internal/module/agent/workflow/engine/scheduler.go`
2. `internal/module/agent/workflow/engine/blackboard.go`
3. `internal/module/agent/workflow/engine/checkpoint.go`
4. `internal/module/agent/workflow/tool/registry.go`
5. `internal/module/agent/service/workflow_agent_tools.go`
6. `internal/module/agent/service/workflow_service.go`
7. `internal/module/agent/service/agent_service.go`
8. `internal/module/agent/workflow/rag/router.go`
9. `internal/module/agent/workflow/rag/memory.go`
10. `cmd/agent-service/main.go`

### 目标代码

1. `runtime/runner.go`
2. `runtime/action.go`
3. `message/builder.go`
4. `model/client.go`
5. `workflow/tool/executor.go`
6. `policy/budget.go`
7. `policy/approval.go`
8. `trace/recorder.go`
9. `workflow/engine/scheduler.go`
10. `service/agent_service.go`

要求不是背文件，而是能回答：一次 Run 的身份、消息、预算、模型、工具、状态、Trace 和恢复分别由谁负责。

## 12. 评审决策项

开始开发前需要确认以下架构选择：

1. 是否接受“可治理 Agent Runtime + 高并发社交内容平台”作为最终定位。
2. 是否同意先迁 `ConsultContent`，而不是一次性重写所有模式。
3. 是否同意扩展现有 `workflow/tool`，避免新建重复 Toolkit。
4. 是否同意 RAG 只做预算装配，不对召回 Chunk 做请求内有损压缩。
5. 是否同意写工具默认审批，审批系统不可用时 fail-closed。
6. 是否同意 Temporal 延后到统一 IR 后再决定，不把当前未接入的 bridge 当成已完成功能。
7. 是否同意用户 API Key 改为 Credential Reference，并补 SSRF 防护。
8. 是否接受 P0-P3 为第一开发批次，P4-P6 为第二批次。

评审通过后，第一项开发任务应当是 P0 的 ADR、行为基线和 Feature Flag，不直接从大规模移动代码开始。
