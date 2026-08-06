# ADR-001：Agent Runtime 边界与渐进式迁移

- 状态：Accepted
- 日期：2026-07-14
- 决策范围：`internal/module/agent`
- 关联计划：[`AGENT_RUNTIME_STRENGTHENING_PLAN.md`](../AGENT_RUNTIME_STRENGTHENING_PLAN.md)

## 背景

当前 Agent Service 已提供直接对话、RAG 咨询、辅助创作、多智能体创作和自定义工作流五种入口，也已有 MCP、DAG、Checkpoint 与认知 RAG 等能力。但执行逻辑分散在 Service、Workflow Node 和策略工具中，存在三套 ReAct 循环、硬编码多智能体管线、弱工具元数据和不完整运行 Trace。

本次强化不能采用一次性重写。现有 HTTP、gRPC、MongoDB 数据与前端调用都已经被使用，迁移必须支持按入口灰度和立即回滚。

## 决策

### 1. 目标职责

| 模块 | 负责 | 不负责 |
|---|---|---|
| Service | 鉴权后的业务参数校验、选择 Agent/Profile/Workflow、调用 Runtime、映射 API 响应 | ReAct 循环、直接执行工具、拼接所有消息历史 |
| Runtime | Step 循环、Action/Observation、预算、取消、恢复、运行级错误分类 | Mongo/Redis/MCP/OpenAI 具体客户端、HTTP/gRPC 协议 |
| Workflow | DSL 校验与编译、DAG 调度、分支/Join、Checkpoint、状态归并 | 复制一套 Agent 循环、越过 Runtime 直接治理模型成本 |
| Tool | ToolSpec、策略校验、超时/重试/幂等/审批、统一执行结果 | 信任 LLM 提供的用户身份、绕过领域服务写数据库 |
| MCP | 外部工具协议适配与连接复用 | 充当权限源、保存 Agent 全局状态、决定业务审批 |
| RAG | 路由、检索、评分、Token 预算内上下文组装、记忆读写 | 控制 Agent 循环、替代 Tool Policy、同步阻塞主写链路 |
| Repository | Dialogue、Workflow、Run、未来 Trace/Approval 的持久化接口 | 编排业务逻辑、调用模型或工具 |

### 2. 依赖方向

```text
HTTP/gRPC -> Service -> Runtime interfaces
                         |-> ModelClient adapter
                         |-> ToolExecutor adapter -> MCP/domain services
                         |-> RAG adapter
                         |-> TraceRecorder adapter -> Repository

Service -> Workflow compiler/scheduler -> Runtime node adapter
Repository implementations -> MongoDB
```

Runtime 只依赖标准库和自身接口。Runtime 单元测试必须使用 Fake Model、Fake Tool、Fake RAG 与 Fake Recorder，不得启动 MongoDB、Redis、MCP、Temporal 或真实模型。

### 3. 复用现有实现

- 继续演进 `workflow/dsl`、`workflow/engine`、`workflow/tool` 和 `workflow/rag`，不新建第二套工作流或工具框架。
- 保留现有 MCP Server 工具实现，把它们适配到统一 `ToolExecutor`。
- 保留现有 Dialogue、WorkflowDefinition、WorkflowRunRecord 及集合。
- 用户自定义 DAG 不维护 Temporal 双执行后端；重复统一 IR 语义的实验 Bridge 已删除。Temporal 只承载独立后台 Workflow，不宣称已托管用户自定义工作流。

### 4. 灰度开关

环境变量 `AGENT_RUNTIME_V2_MODES` 控制五个入口：

| 值 | 行为 |
|---|---|
| 空字符串或 `none` | 五种模式全部走旧路径，默认值 |
| `consult,assist` | 只有列出的模式允许走 Runtime v2 |
| `all` | 五种模式全部允许走 Runtime v2 |
| 含未知值或空片段 | 启动时记录警告并整体回退旧路径 |

合法模式名固定为 `chat`、`consult`、`assist`、`multi`、`workflow`。配置在进程启动时解析为不可变快照，不在请求中读取环境变量。

P0 只建立迁移接缝，不接入新执行器，因此无论开关值为何，业务行为都与旧路径一致。P1 起每迁移一个入口，才在对应 Service 方法中增加新旧执行器分派。

### 5. 兼容契约

迁移期间遵守以下约束：

- HTTP 路径不改名：`/agent/chat`、`/agent/consult`、`/agent/assist`、`/agent/multi`、`/agent/workflows/:id/run`。
- gRPC 方法全名不改名，现有 Proto 字段号不复用、不重排。
- `DialogueMode` 数值 `1..5` 保持不变。
- MongoDB 集合 `dialogues`、`dialogue_messages`、`agent_workflows`、`agent_workflow_runs` 保持不变。
- 新字段只允许追加，并使用 `omitempty` 或可识别默认值兼容旧数据。
- DSL v2 必须保留原始 DSL；编译失败可以回退 v1。
- Trace、Approval、Prompt/Profile 等新数据使用新集合，不能改变旧记录读取语义。

关键 Proto、DialogueMode、Mongo 集合及 BSON 字段由 Compatibility Contract Test 自动保护。

### 6. 回滚

1. 从 `AGENT_RUNTIME_V2_MODES` 删除出现异常的模式并重启 Agent Service。
2. 若配置无法解析，服务自动回到全旧路径。
3. 不删除新 Runtime 产生的追加式 Trace/Approval 数据，旧路径可以忽略这些集合。
4. 写工具审批策略不允许通过回滚绕过；审批基础设施不可用时必须 fail-closed。

## 后果

正面影响：

- 可以按入口迁移并做新旧对照，不影响其他模式。
- Runtime 能脱离基础设施做确定性单元测试。
- 工具、RAG、模型与存储通过 Adapter 接入，避免具体 SDK 侵入核心循环。
- 现有客户端和历史数据无需同步升级。

代价：

- 迁移期间短期存在新旧双路径，测试矩阵扩大。
- Service 需要暂时承担入口分派，直到全部迁移完成。
- 新字段和新集合必须长期维护向后兼容。

## 被否决方案

- 一次性替换全部入口：回归面过大且无法快速回滚。
- 另建第二套 Tool Registry/Workflow Engine：会形成长期双轨和更多重复语义。
- Runtime 直接依赖 OpenAI/MCP/Mongo SDK：无法离线测试，也会锁死 Provider 与协议。
- 通过请求参数临时切换新旧执行器：扩大外部攻击面，并使相同 API 行为不可预测。
