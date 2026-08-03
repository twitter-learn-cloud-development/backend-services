---
name: workflow
description: 保持前端 DSL、后端校验、DAG 调度、Tool Policy 和持久化运行状态一致。用于 Workflow Editor、JSON DSL、DAG、节点、连线、Router、Wait/Approve、Checkpoint、Resume 或 Tool Registry 任务。
---

# Workflow Skill

## 先读

- `.agents/context/agent_runtime_context.md`
- `internal/module/agent/workflow/dsl/`
- `internal/module/agent/workflow/engine/`
- `internal/module/agent/workflow/tool/`
- `internal/module/agent/service/workflow_service.go`
- `web/src/views/agent/WorkflowEditor.vue`
- `web/src/components/agent/NodePropertiesDrawer.vue`

## 端到端链路

```text
Vue Flow Nodes/Edges -> JSON DSL -> gRPC/Service validation
                     -> DAG Engine -> Tool/LLM/Router/Wait
                     -> Run/Checkpoint/Resume -> UI result
```

## 执行步骤

1. 明确是画布交互、DSL Contract、Scheduler、Node Executor、Tool 还是恢复语义。
2. 先修改强类型 DSL/校验，再适配前端序列化；禁止只改 UI 属性。
3. 校验唯一 Node ID、Edge 端点、无环、Start/End、变量引用和节点输入 Schema。
4. 定义 Timeout、Retry、Idempotency、Output Schema 和失败传播。
5. 工具通过 `ToolSpec + ToolHandler` 注册到注入式 Registry；写工具标记 Category/Approval，密钥只传 Credential Reference。
6. 对并行分支验证确定性 Join、取消传播和状态合并。
7. 保存后重新加载必须还原 Nodes、Edges、Position 和 Properties。

## 项目不变量

- LLM Chat 与 LLM Writer 是两个明确组件；默认 LLM 不隐式写推文。
- Edge 从 Handle 边缘连接；选中 Edge 可删除且同步 DSL。
- Runtime Strategy 节点不替换 DAG Scheduler。
- Workflow ToolNode 必须调用统一 Executor；不得绕过审批 Gate 直接执行 Write/Risky Handler。
- Registry 由 Composition Root 创建并注入；禁止恢复 package 全局单例。
- 用户 DAG 不使用 Temporal 双执行后端；Temporal 只承载独立后台 Workflow。未来若重新引入 Backend Adapter，必须消费统一 IR，禁止复制拓扑、Reducer、审批或补偿语义。
- Run 挂起必须持久化 Checkpoint/WaitingNode；Resume Token 只保存哈希并通过状态条件更新单次领取，不能占用 goroutine 等待人工。
- Wait 恢复将当前节点标记完成；工具审批恢复必须设置 `retry_current_node` 并重新执行当前 Tool，禁止跳过副作用节点。
- 已批准审批的跨设备恢复使用独立短期 Grant 端点：校验租户、Run/Approval 绑定、状态和 revision 后轮换令牌哈希；禁止查询回显旧令牌或依赖浏览器持久化明文凭证。
- 本地 Blackboard 使用 copy-on-write 状态代际；WorkflowNode 只接收 `StateView`，Delta 只能由 Scheduler 协调器单写合并。
- StateEvent 与周期 Snapshot 已按 `state_version` 幂等持久化；周期 Run 游标只能通过专用原子接口推进 `state_version + revision`，禁止用旧 Run 对象整包更新状态或取消字段。Resume 必须通过 Snapshot + Event 重建校验 Checkpoint。公开 Replay 只能返回用户隔离、哈希校验的只读证据，不得调用 Scheduler/LLM/Tool 或暴露补偿原始输入；仍不宣称完整 Event Sourcing。用户 DAG 不再保留 Temporal 双后端。
- Blackboard 检索必须从目标版本之前最近快照加有界事件范围重建，校验所有权、哈希、序号和最终版本；分页游标绑定版本与过滤条件。值必须递归脱敏并限制预览，禁止为了查询重新执行 DAG、模型或工具。
- v1 的 `writes.path/source` 必须由协调器实际映射；并行节点声明同一全局写路径时必须使用一致且非空的内置 Reducer。Reducer 只能由协调器按 IR 声明顺序执行，禁止节点协程直接写 Blackboard。
- 节点 Retry 只接受显式可重试错误或临时网络错误；退避/Jitter 必须确定性且响应 Context。挂起、取消、Deadline 和业务错误不得盲目重跑，Trace 必须保留尝试次数。
- Compensation 只补偿已成功节点并按确定性反向拓扑执行；主 Run 失败与 Journal 必须先持久化，补偿工具必须复用 ToolExecutor、审批与独立幂等键，禁止通过重放主 DAG 模拟补偿。
- Journal 领取必须使用严格序号、租约和 Attempt ID；挂起恢复或失败重试只继续首个未成功补偿。后台只能接管首个过期 `executing` 租约；审批型/未知工具必须转显式重试，禁止为了无人值守绕过 Approval 或生成无法交付的 Resume Token。
- Journal 查询必须按 Run 所有者隔离并使用脱敏 DTO；人工 Retry 只能推进严格首个 `planned`、`failed` 或租约过期的 `executing`，不得抢占有效租约、绕过 `suspended` 审批或重放主 DAG。
- Run 取消必须先持久化 `canceling`，由执行实例观察后取消 Context，并以状态+Revision 原子提交终态；禁止使用仅在单进程有效的 `map[runID]cancelFunc`。取消后仍需等待已启动节点退出，并保留既有补偿语义。
- Run/Step/LLM/Tool Trace 必须写入独立 Recorder，并以 `(user_id, run_id)` 查询；`output_json.traces` 只用于历史兼容。默认只记录 Prompt/Completion/Tool 输入输出的摘要与长度，禁止把正文复制到 Trace。
- DSL 顶层运行预算必须由 IR、Scheduler、模型调用和 Checkpoint 端到端共同执行；节点 Retry 计入尝试次数，并发模型调用先预留 Token/成本，恢复后继续累计。禁止只在前端展示预算或只在 Run 结束后统计。

## 验证

- DSL cycle/missing node/invalid reference/credential leak 单测。
- Engine 并发、Join、timeout、cancel、suspend/resume `-race`。
- Workflow 保存-刷新-加载、Edge 创建/删除、节点属性回显。
- `npm run build` + Workflow Engine/Service Go 测试。
