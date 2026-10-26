# 用户端智能客服 Agent：V1 分轮开发计划

状态：**R0～R7 主链路已完成；R8 本机交付收尾中**
日期：2026-09-28
关联文档：[`architecture-v1.md`](architecture-v1.md)、[`development-plan-v1.md`](development-plan-v1.md)、[`api-contract-v1.md`](api-contract-v1.md)

2026-09-29 范围调整：先交付本机联合运行版；原轮次的设计目标保留，未验收的可靠性/生产门禁不因本机交付而视为通过。当前证据见 [`progress-v1.md`](progress-v1.md)，本机交付定义见 [`local-delivery-v1.md`](local-delivery-v1.md)。

## 1. 使用方式

本文件把 V1 拆成九轮开发。每轮必须满足本轮退出条件后再进入下一轮；如果后续轮次发现协议问题，先更新接口文档和契约测试，再修改实现。

开发轮次不等于固定自然周。每轮是一个可独立验证、可演示、可回退的交付单元。

每轮统一执行顺序：

```text
确认本轮输入
  -> 冻结本轮协议
  -> 实现最小闭环
  -> 单元/集成测试
  -> 文档与示例同步
  -> 按退出条件验收
```

## 2. 全局完成标准

每轮提交都必须满足：

- 没有新增问题类别枚举、固定 Worker 类型或固定解决方案路由。
- 新增业务能力通过 Capability 注册进入系统，不修改主图结构。
- 所有模型输出跨节点前经过 Pydantic 校验。
- 所有外部 I/O 都有超时、错误映射和测试替身。
- 用户身份不从请求正文、模型输出或 Tool 参数中获取。
- 新增行为至少包含正常、失败和越权/非法输入测试。
- README、接口文档和示例与实现保持一致。
- Ruff、Pyright、pytest 全部通过。

## 3. 轮次总览

| 轮次 | 对应里程碑 | 本轮主题 | 本轮结束时的能力 |
|---|---|---|---|
| R0 | 计划准备 | 契约冻结与工程约定 | 团队对边界、接口、验收方式达成一致 |
| R1 | M0 + M1 | 工程骨架与核心协议 | 服务可启动，所有核心对象可校验 |
| R2 | M2 | ModelProvider 与 Capability Runtime | 可以离线注册、发现和执行 Mock Capability |
| R3 | M3 | GenericWorker Agent Loop | 单个动态任务可以自主选工具并形成证据结果 |
| R4 | M4 | Supervisor 与并行主图 | 复合问题可拆解、并行调查、汇总并回答 |
| R5 | M5 | 统一 Chat API 与 SSE | 前端可通过一个接口调用完整 Runtime |
| R6 | M6 | Go Support Query 联调 | Agent 可以安全使用真实 Twitter 数据 |
| R7 | M7 | 多轮会话与知识检索 | 可恢复对话并组合业务与知识证据 |
| R8 | M8 | 评测、可观测性与交付 | 可稳定演示、回归和部署 |

## 4. R0：契约冻结与工程约定

### 本轮目标

在写业务代码前冻结 V1 的两个网络接口和四个核心边界，避免 Python 与 Go 分别实现出不同语义。

### 本轮输入

- 已确认的 Supervisor–GenericWorker 架构。
- 用户端只读、无管理员能力、无工具审批的范围。
- 用户侧统一 Chat 接口和内部统一 Query 接口原则。

### 工作项

- 审阅并冻结 [`api-contract-v1.md`](api-contract-v1.md)。
- 审阅并冻结 [`contracts/support-agent-v1.openapi.yaml`](../contracts/support-agent-v1.openapi.yaml)。
- 确认 Python 版本、包管理器、代码规范和测试命令。
- 确认模型配置只从环境变量读取，不写入仓库。
- 确认内部服务身份方案：短期服务 JWT，用户主体从已验证声明传播。
- 确认 `operation` 命名规范、错误码命名规范和 Evidence 最小字段。
- 建立 ADR 变更规则：协议破坏性修改必须提升版本。

### 本轮不做

- 不创建真实 Capability。
- 不调用真实模型、MongoDB、Qdrant 或 Go 服务。
- 不实现 Graph。

### 产物

- 完整接口文档和 OpenAPI 文件。
- 开发、测试、验收命令清单。
- 未决配置项列表及负责人。

### 验收场景

- Python 与 Go 开发者能分别根据同一 OpenAPI 写出 Mock Client/Server。
- 前端能根据文档明确普通 JSON 和 SSE 两种响应方式。
- 文档中不存在按账号、帖子、关注等问题类别拆分的 API。

### 退出条件

- 网络字段、状态码、错误结构和版本路径没有歧义。
- 所有未决项都有默认方案，不阻塞 R1。

## 5. R1：工程骨架与核心协议

### 本轮目标

建立可以持续开发的 Python 服务，并把 Agent Runtime 的数据边界落实为可执行 Schema。

### 工作项

- 初始化 `pyproject.toml`、`uv.lock`、Dockerfile、`.env.example`。
- 配置 Ruff、Pyright、pytest 和 AnyIO。
- 实现 FastAPI app factory、lifespan、结构化日志和统一异常处理。
- 实现 liveness/readiness。
- 实现 `SupportRequest`、`SupportCase`、`TaskSpec`、`TaskPlan`。
- 实现 `CapabilitySpec`、`CapabilityCall`、`CapabilityObservation`。
- 实现 `Evidence`、`WorkerResult`、`AggregatedResult`、`SupportResponse`。
- 实现 `AuthContext`、`RuntimeContext`、`RunBudget`。
- 实现 `SupportState` 和 `WorkerState`，暂不接真实节点。

### 关键设计检查

- `TaskSpec` 没有 `domain`、`intent_type` 或 `worker_type`。
- `SupportRequest` 没有 `actor_user_id`。
- Capability 权限不是 Planner 输出的一部分。
- 可变字段全部使用 `default_factory`。
- 对话 ID、任务 ID 和证据引用均有格式与长度限制。

### 测试目标

- Schema 正反例测试。
- TaskPlan 重复 ID、缺失依赖、循环依赖和超限测试。
- Evidence 缺少来源、confidence 越界测试。
- FastAPI lifespan 与健康检查测试。

### 演示目标

启动服务并展示：健康检查、自动生成的 OpenAPI、合法/非法核心对象校验。

### 退出条件

- 基础质量命令全部通过。
- 后续轮次不需要用裸 `dict` 跨越 Graph 节点或外部接口。

## 6. R2：ModelProvider 与 Capability Runtime

### 本轮目标

建立模型和工具的通用运行基础，但保持完全离线可测。

### 工作项

- 实现 `ModelProvider` 协议和 OpenAI-compatible Adapter。
- 实现 `FakeModelProvider`，支持脚本化返回 structured output 和 tool calls。
- 实现 `CapabilityRegistry.register/list/get`。
- 实现权限过滤：只读、用户数据范围、环境启用状态。
- 实现 `CapabilityResolver`，基于任务文本和 Capability 描述返回候选能力。
- V1 先采用可解释的关键词/描述检索；接口预留向量检索替换点。
- 实现 `CapabilityExecutor`：Schema 校验、RuntimeContext 注入、超时、重试、错误映射。
- 实现至少四个用途不同的 Mock Capability，验证系统没有依赖固定类别路由。

### 接口目标

```python
registry.register(spec, handler)
resolver.resolve(task, auth_context, limit=8)
executor.execute(call, runtime_context)
```

### 测试目标

- 重名注册、未知 Capability、非法参数。
- 只读策略和主体范围过滤。
- 超时、临时失败重试、永久错误不重试。
- 新增 Mock Capability 后无需修改 Resolver/Executor 主逻辑。

### 演示目标

输入两项语义不同的任务，Resolver 返回不同候选集合，并通过统一 Executor 获得规范化 Observation。

### 退出条件

- Capability 的发现、授权与执行解耦。
- 所有测试无需网络和真实模型即可运行。

## 7. R3：GenericWorker Agent Loop

### 本轮目标

实现唯一的通用 Worker，使它可以根据任意 `TaskSpec` 自主发现和组合 Capability。

### 工作项

- 构建 Worker StateGraph。
- 实现 `prepare_task`、`resolve_capabilities`、`reason`、`execute`、`observe`、`format_result`。
- 将候选 Capability 转换为 LangChain Tools。
- 使用 Tool Calling 驱动多轮调查。
- 实现模型调用、工具调用、总时长和候选数量预算。
- 实现 Observation 截断、敏感字段过滤和不可信内容隔离。
- 实现结构化 `WorkerResult` 生成与失败降级。

### 测试目标

- 一轮工具调用后结束。
- 多轮工具组合后结束。
- 模型请求未知 Capability。
- 参数不符合 Schema。
- Tool 超时后换用其他证据或返回 partial。
- 模型持续调用工具时被预算强制终止。

### 演示目标

用同一个 GenericWorker 依次处理两类完全不同的任务，并展示它选择了不同能力组合，而不是切换 Worker 类。

### 退出条件

- Worker 不依赖任何 Twitter 业务类型判断。
- `WorkerResult` 中的每项结论至少关联一个 Evidence 或明确标记 gap。

## 8. R4：Supervisor 与并行主图

### 本轮目标

把问题理解、动态任务规划、并行 Worker、证据合并和客服回答串成完整闭环。

### 工作项

- 实现 `intake` 与只读范围保护。
- 实现 Planner structured output。
- 实现确定性 Plan Validator。
- 使用 LangGraph `Send` 动态 fan-out GenericWorker。
- 实现有依赖任务的分批 dispatch。
- 实现 WorkerResult reducer。
- 实现 Evidence Merger 和冲突检测。
- 实现 Evidence Gate 与一次受限 re-plan。
- 实现 Supervisor Composer 与 Output Guard。
- 增加 Graph recursion limit 与全局 Runtime Budget。

### 测试目标

- 单任务计划。
- 多任务并行计划。
- 父子依赖任务。
- Planner 非法输出被拒绝或修复。
- 一个 Worker 失败、其余成功。
- 证据冲突与证据不足。
- re-plan 不超过一次。

### 演示目标

运行复合问题：显示任务计划、并行执行结果、统一证据和最终用户答复。演示日志可以展示内部节点，用户响应不能展示内部 Thought。

### 退出条件

- Fake Model + Mock Capability 的端到端 Graph 测试稳定通过。
- 最终答复不能包含 AggregatedResult 中不存在的新事实。

## 9. R5：统一 Chat API 与 SSE

### 本轮目标

通过唯一用户 API 暴露 R4 Runtime，同时支持普通响应与流式交互。

### 工作项

- 实现 `POST /api/v1/support/chat`。
- 实现内容协商：JSON 与 `text/event-stream`。
- 实现认证 dependency；本地测试使用显式 Fake Auth。
- 实现 conversation ID 创建、请求 ID 和 case ID 返回。
- 实现 SSE event mapper，不透传原始 Graph event。
- 实现 RFC 7807 Problem Details。
- 实现 `Idempotency-Key`、请求取消、断连处理和总超时。
- 对消息和上下文设置大小限制。

### 测试目标

- 首次对话和已有 conversation。
- JSON 与 SSE 返回同一语义结果。
- 非法 Accept、非法正文、未认证、限流和超时。
- SSE 断连不遗留无限运行任务。
- SSE 不包含 Prompt、Thought、Tool 参数和异常栈。

### 演示目标

用 curl 或前端测试页分别请求 JSON 与 SSE，展示统一接口的两种消费方式。

### 退出条件

- [`api-contract-v1.md`](api-contract-v1.md) 中用户接口的全部示例通过契约测试。

## 10. R6：Go Support Query 联调

### 本轮目标

保持 Python Agent 通用的同时，通过一个内部 Query 协议接入真实 Twitter Clone 数据。

### Python 工作项

- 实现异步 `SupportQueryClient`。
- 将 Registry Capability Adapter 映射为统一 `operation + arguments` 请求。
- 实现内部服务 JWT、trace context 和超时传播。
- 把 Go 返回的结果和 Evidence 转换为 CapabilityObservation。
- 增加 HTTP Mock 契约测试和真实环境 smoke test。

### Go 工作项

- 实现 `POST /internal/v1/support/query`。
- 实现 operation registry、参数 Schema、授权函数和 handler adapter。
- operation 内部调用现有服务，不把微服务拓扑暴露给 Python。
- 实现稳定错误码、脱敏 Evidence 和审计字段。
- 增加防重放、超时和资源归属检查。

### 测试目标

- Python 与 Go 共同通过 OpenAPI 契约测试。
- 未知 operation、参数错误、资源不存在、越权、下游超时。
- 当前用户合法数据查询。
- 模型在 arguments 中伪造 user ID 不能改变认证主体。

### 演示目标

用户询问自己的真实内容状态；GenericWorker 动态选择真实 Capability，最终答复包含可追溯但脱敏的证据。

### 退出条件

- Python 不包含微服务地址、数据库查询或业务分类路由。
- 任意真实 Capability 都经过 Go 侧最终授权。

## 11. R7：多轮会话与知识检索

### 本轮目标

让用户可以自然追问，并让帮助文档检索以普通 Capability 的方式进入同一 Agent Loop。

### 工作项

- 接入官方 `MongoDBSaver`；其异步接口通过 `aget/aput/adelete_thread` 提供（当前官方包不再提供 `AsyncMongoDBSaver` 类）。
- 将 conversation ID 映射为 thread ID，并校验用户归属。
- 设置 TTL、保留策略和会话删除流程。
- 实现历史压缩和上下文选择。
- 建立用户可见文档的 Qdrant collection 和 ingestion 流程。
- 注册知识检索 Capability。
- Evidence 中保留文档 ID、版本和更新时间。

### 测试目标

- 同用户多轮追问。
- 不同用户使用同一 conversation ID。
- 服务重启后的恢复。
- 过期会话。
- 业务 Evidence 与知识 Evidence 联合回答。
- 恶意文档内容不能改变系统策略。

### 演示目标

第一轮询问一条内容状态，第二轮用“它”继续追问规则；系统能解析会话上下文并组合真实数据与规则文档。

### 退出条件

- 多轮上下文不会无限增长。
- 知识检索不需要新增 Graph 分支或 KnowledgeWorker。

## 12. R8：评测、可观测性与交付

### 本轮目标

把功能闭环提升为可回归、可定位、可部署的项目成果。

### 工作项

- 接入 OpenTelemetry、Prometheus 指标和结构化审计日志。
- 建立固定 eval dataset 和可重复运行的评测命令。
- 评测计划质量、Capability 选择、证据忠实度、权限拒绝和延迟预算。
- 完成故障注入：模型失败、MongoDB 失败、Qdrant 失败、Go 下游超时。
- 完成 Docker Compose 配置、资源限制、优雅停机和 readiness。
- 完成开发、联调、演示和故障排查说明。
- 冻结 V1 版本并生成验收记录。

### 上线验收门槛

- 核心场景回归集全部通过。
- 越权测试拒绝率 100%。
- 有事实陈述的回答 Evidence 覆盖率 100%。
- 单次请求严格受模型、工具、并发和总时长预算控制。
- 日志与 Trace 抽检无凭证、原始内部错误和非必要用户敏感数据。
- 任一非关键依赖失败时能够给出安全降级结果。

### 退出条件

- V1 可在目标环境一键启动并完成演示场景。
- 文档、契约、测试和实际行为一致。

## 13. 每轮验收记录模板

每轮结束时在 PR 或开发记录中填写：

```text
轮次：R?
完成日期：
实现范围：
未完成项：
接口变更：无 / 链接
新增配置：
测试命令与结果：
演示场景与结果：
已知限制：
是否满足退出条件：是 / 否
下一轮输入：
```
