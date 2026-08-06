# Agent 五入口行为基线

状态：P0 已冻结迁移前基线，并追加 P1 灰度迁移说明；不代表最终目标架构。

## 1. 公共契约

| 项目 | 当前行为 |
|---|---|
| 鉴权 | HTTP Gateway 从 JWT 上下文获取 `user_id`，客户端传入的用户身份不作为权限源 |
| 会话键 | 优先使用 24 位十六进制 `dialogue_key`；旧 `uint64 dialogue_id` 继续兼容 |
| 历史上下文 | 最多读取最近 20 条消息，当前按消息条数而非 Token 预算裁剪 |
| 消息持久化 | 成功响应通常批量写入一条 user 与一条 assistant 消息，再更新 Dialogue 时间 |
| 模型选择 | HTTP/Proto 接受 `model_kind_id`，当前 Service 仍使用进程级 `chatModel`，尚未按该字段路由 |
| 错误映射 | 参数错误通常为 HTTP 400；下游 gRPC/模型/工具错误通常由 Gateway 映射为 HTTP 500 |
| Runtime 灰度 | `AGENT_RUNTIME_V2_MODES` 默认空；可独立启用 `consult`、`assist`、`workflow`。其中 `workflow` 仅迁移 ReAct/Plan-Execute 策略节点，不替换 DAG Scheduler |

## 2. 五种入口

### 2.1 Chat

- HTTP：`POST /api/v1/agent/chat`，Gateway 路由组内为 `/agent/chat`。
- gRPC：`/aiAgent.v1.AiAgentService/callApiOfAi`。
- 输入：`content` 必填；`dialogue_id`、`dialogue_key`、`model_kind_id` 可选。
- 超时：Gateway 60 秒。
- 执行：认知 Router/Memory 可降级注入 System Prompt，然后单次调用 Chat Completion。
- 输出：`response`、`dialogue_key`。
- 持久化：Dialogue mode=`1`；保存 user/assistant；成功后异步结晶本回合记忆。
- 失败：模型失败不保存消息，但 Dialogue 可能已创建；上下文读取失败仅告警并继续；消息保存失败仅记录日志，仍返回模型结果。

### 2.2 Consult

- HTTP：`POST /api/v1/agent/consult`。
- gRPC：`/aiAgent.v1.AiAgentService/consultContent`。
- 输入：与 Chat 相同。
- 超时：Gateway 60 秒。
- 执行：默认沿用旧 ReAct；设置 `AGENT_RUNTIME_V2_MODES=consult` 后，由统一 `ReActRunner` 执行最多 5 步，MCP 长连接仍由适配器复用并注入当前用户身份。
- 输出：`response`、`tweet_list`、`dialogue_key`；当前主要检索结果通过自然语言 `response` 返回，`tweet_list` 通常为空。
- 持久化：Dialogue mode=`2`；仅获得最终回答时保存 user/assistant。
- 失败：MCP 初始化、模型、参数 JSON、工具调用或超过 5 轮/步均返回错误；工具连接异常会重置 MCP Client；失败前可能留下空 Dialogue。Runtime v2 对未知工具采用 risky/fail-closed，对写工具返回 `ApprovalRequired` 而不直接执行。

### 2.3 Assist

- HTTP：`POST /api/v1/agent/assist`；确认发布为独立 `POST /api/v1/agent/confirm`。
- gRPC：`/aiAgent.v1.AiAgentService/assistPublishTwitter`。
- 输入：与 Chat 相同。
- 超时：生成 60 秒；确认发布 30 秒。
- 执行：默认沿用旧 ReAct；设置 `AGENT_RUNTIME_V2_MODES=assist` 后由统一 `ReActRunner` 执行最多 5 步，只暴露只读检索工具。生成草稿时不发布，确认后由独立接口调用 `create_tweet`。
- 输出：`response`、`tweet_list`、`dialogue_key`；当前候选主要位于自然语言 `response`。
- 持久化：Dialogue mode=`3`；获得最终回答时保存 user/assistant。确认发布接口本身不追加对话消息。
- 失败：Legacy 与 Consult 类似；Runtime v2 对未知工具 fail-closed，且 Profile 在工具目录阶段排除写工具。当前“需确认后写入”已有 Runtime 工具白名单和独立确认接口双重边界，但通用 Human Approval 闭环仍属于 P3。

### 2.4 Multi

- HTTP：`POST /api/v1/agent/multi`。
- gRPC：`/aiAgent.v1.AiAgentService/multiAgentPublishTwitter`。
- 输入：`domain`、`author_user_id`、`style_ratio`、`content` 必填；参考推文与 `dialogue_key` 可选。
- 超时：Gateway 120 秒。
- 执行：当前依次执行领域检索、作者风格检索、可选参考推文读取，再由 Writer 单次生成；Search/Style/Writer/Review 已用版本化 Profile 描述，其中 Review 作为 Writer Prompt 的审校策略组合，不产生额外模型调用。当前仍不是通用 Multi-Agent Runtime，也不是并发调度。
- 输出：`response`、`dialogue_key`。
- 持久化：Dialogue mode=`4`；保存 user/assistant 和 domain、author、style、reference、agent_profiles metadata。
- 失败：主要检索或 Writer 失败则整体失败；参考推文读取失败会降级为空；失败前可能留下空 Dialogue。

### 2.5 Workflow

- HTTP：`POST /api/v1/agent/workflows/:id/run`。
- gRPC：`/aiAgent.v1.AiAgentService/runWorkflow`。
- 输入：`workflow_id` 路径参数与 JSON 输入；AI 助手场景可在输入中传 `persist_dialogue=true`、`dialogue_key`、`user_input`。
- 超时：Gateway 120 秒。
- 执行：加载用户隔离的 DSL，解析输入，创建 Run，编译 DAG，注入用户上下文并执行本地 Scheduler。设置 `AGENT_RUNTIME_V2_MODES=workflow` 时，仅其中 ReAct/Plan-Execute 策略节点使用统一 Runner，并继续保留节点级 model/max_tokens/max_iterations/allowed_tools。
- 输出：`run`、可选 `dialogue_key`、可选 `response`；Run 输出包含 blackboard 与 NodeTrace。
- 持久化：始终写 `agent_workflow_runs`；只有 `persist_dialogue=true` 才创建/复用 mode=`5` Dialogue 并保存 user/assistant。
- 失败：DSL/输入在 Run 创建前失败时不会产生 Run；调度阶段失败会写 failed Run；Wait 节点会写 suspended Run 与 Checkpoint。
- 当前边界：`ResumeWorkflowRun` 已贯通 Proto/Gateway/UI，并使用一次性令牌恢复挂起运行；用户 DAG 的 Temporal 实验 Bridge 已删除，Temporal 只承载独立后台 Workflow。

## 3. 存储兼容基线

| 数据 | 固定值 |
|---|---|
| Dialogue modes | Chat=1, Consult=2, Assist=3, Multi=4, Workflow=5 |
| Dialogue collection | `dialogues` |
| Message collection | `dialogue_messages` |
| Workflow collection | `agent_workflows` |
| Workflow run collection | `agent_workflow_runs` |

新增字段必须追加且兼容旧记录；禁止复用 Proto 字段号或重命名 BSON 字段。

## 4. 冒烟用例

### 4.1 离线门禁

```powershell
$env:GOCACHE="$PWD\tmp\go-build-cache"
go test ./internal/module/agent/runtime ./internal/module/agent/repository ./internal/module/agent/grpc
go test ./internal/module/agent/service ./internal/module/agent/workflow/engine
go test -race ./internal/module/agent/runtime ./internal/module/agent/workflow/engine
```

这些测试不得连接 MongoDB、Redis、MCP、Temporal 或真实模型。

### 4.2 集成冒烟清单

- [ ] Chat 新建会话后返回非空 `dialogue_key`，再次携带该 key 能看到上下文连续性。
- [ ] Consult 搜索已知关键词时返回真实工具结果；工具失败时明确报错，不声称“正在等待”。
- [ ] Assist 请求草稿时不发布；确认接口只以当前 JWT 用户身份发布。
- [ ] Multi 返回与当前主题相关的完整候选，输入无关历史推文不会污染正文。
- [ ] Workflow 编辑器测试默认不写 Dialogue；AI 助手运行同一工作流时写入同一 `dialogue_key`。
- [ ] Workflow Wait 节点产生 suspended Run、Checkpoint、waiting node 和 resume token。
- [ ] 将 `AGENT_RUNTIME_V2_MODES` 设为空后重启，五种入口全部保持旧路径。

集成冒烟依赖本地模型和中间件，不属于纯 Runtime 单元测试。每迁移一种模式，必须在同一输入下对比新旧最终答案、工具序列、Token、延迟、错误类型和持久化副作用。
