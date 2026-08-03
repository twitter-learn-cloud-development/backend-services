---
name: agent
description: 在不破坏能力隔离、工具权限和回滚路径的前提下开发可治理 Agent 能力。用于统一 Agent 入口、Runtime、LLM、MCP、Tool、Model Provider、Memory、多智能体或 AI 助手任务。
---

# Agent Skill

## 先读

1. `.agents/context/project_map.md`
2. `.agents/context/agent_runtime_context.md`
3. `docs/AGENT_RUNTIME_STRENGTHENING_PLAN.md` 的当前阶段
4. 与任务对应的 `runtime/strategy/message/model/profile/service/workflow/mcp` 目录

## 推理顺序

1. 优先确认 P8 统一入口所需 Capability；Chat、Consult、Assist、Multi 或 Workflow 只作为兼容执行 Profile 或确定性自动化边界。
2. 确认责任层：gRPC 边界、Service 编排、Runtime、Model Adapter、Tool、Repository 或 UI。
3. 记录当前 Legacy/Runtime v2 行为与 `AGENT_RUNTIME_V2_MODES` 回滚路径。
4. 定义输入、输出、Budget、Tool 权限、错误和取消语义。
5. 用接口/Option 注入 Provider、Tool、Counter、Policy 或 Repository，避免 Runner 依赖基础设施。
6. 先写 Fake 驱动的离线测试，再接真实 Adapter。
7. 更新强化计划、进度；出现失败更新 ISSUES。

## 项目不变量

- `runtime` 不得依赖 Service、Mongo、Redis、MCP SDK 或具体 Provider。
- System/Policy/Current 与 ToolCall/ToolResult 配对规则不可被上下文裁剪破坏。
- Provider Usage 与 Estimated Usage 必须可区分。
- 写工具默认 fail-closed；Assist 不能隐式发推。
- 模型驱动工具调用必须经过实例化 `workflow/tool.Executor`；禁止重新引入 package 全局 Registry 或在 Legacy 路径直接调用写工具。
- 审批恢复必须绑定 User/Run/Step/Tool/输入摘要；持久层只保存 Resume Token 哈希。跨设备恢复只能为已批准且仍挂起的 Run 按 revision 签发短期单次授权；签发必须轮换哈希使旧令牌失效，响应禁止缓存，客户端不得持久化明文授权。
- 写工具的本地结果回放不等于跨服务 exactly-once；下游未提供原生幂等键前必须保留残余窗口说明。
- 模型选择必须使用 Provider 实际 Model ID；Embedding 模型不进入 Chat 选择。
- Capability Hint 仅表达用户偏好，不是权限；实际工具必须取用户连接、Catalog、Profile、Policy、Budget 与审批结果的交集。
- Capability Planner 只可选择不可变 Catalog 中已启用的精确路由；内置组合不得描述为任意动态工具编排，也不得暴露未实现的 Web/MCP 能力。
- Multi-Agent Planner 必须区分 `candidate_strategy` 与实际 `selected_strategy`；角色 Tool Scope 只是准入证据，执行时仍取 Catalog/Profile/Policy/Budget/Approval 交集。聚合执行器缺失时必须单 Agent 回退，不能调用旧硬编码 Multi 路径冒充自动协作。
- Strategy 计划只保存稳定信号、角色标识、预算、原因码和摘要；禁止保存原始问题、Prompt、凭据、工具参数或模型思维过程。
- API Key 不进入 DSL、Mongo、Trace 或日志；仅使用 Credential Reference。
- 可选 Prompt/Completion 预览采样必须默认关闭并通过 Observability 的有界敏感拒绝策略；业务代码不得自行旁路采样器记录正文。
- Runtime Profile 与 Workflow Revision 应把稳定 Prompt Template ID/Version 写入 LLM Trace；该字段只用于复现执行证据，不替代 P7 的 Prompt 生命周期管理。
- Base URL 必须经过 Endpoint Policy；禁止为方便测试放开任意私网。
- 所有 goroutine、HTTP、MCP、LLM、Embedding 调用必须响应 Context 取消。

## 反模式

- 在业务 Service 内复制 ReAct 循环。
- 用 Prompt 文本代替权限校验、预算或审批。
- 模型出错后伪造“正在等待/稍后返回”的后台状态。
- 为某个 Provider 在各业务模式散落 if/else。
- 在 Workflow、Runtime MCP 和 Legacy Agent 中复制 Schema、权限、审批、重试或审计逻辑。
- 把 Multi-Agent 名称包装在单次 LLM 调用上却宣称真实协作。
- 把 Temporal 风控/热点后台 Workflow 描述为用户 DAG 执行器。

## 验证

- 目标包单测 + Runtime/Service/Repository 共享边界测试。
- 并发调度、Timer、租约、缓存或共享状态变更运行 `go test -race`。
- Agent 全包：`go test ./internal/module/agent/... ./cmd/agent-service -count=1`。
- 静态检查：`go vet ./internal/module/agent/... ./cmd/agent-service`。
- 前端模型/工作流变化额外执行 `web/npm run build`。
