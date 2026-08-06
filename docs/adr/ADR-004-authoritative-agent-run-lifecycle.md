# ADR-004: Unified Agent 使用独立权威 Run 生命周期

- 状态：Accepted
- 日期：2026-07-29
- 关联：P8.3 可恢复 Agent Run

## 背景

Unified Agent 已有 `agent_run_traces` 记录执行证据，Workflow Engine 也有
`agent_workflow_runs`、Checkpoint、审批和恢复授权。但两者都不能直接充当
模型驱动 Agent 的权威运行状态：Trace 允许异步、采样和幂等覆盖，不承担状态
转换；Workflow Run 又绑定 DAG Revision、Node、Blackboard 和补偿语义。

若直接复用其中任意一种，会让观测数据成为业务真相，或让普通 Agent 被迫依赖
Workflow 的节点模型。高风险/写 MCP 工具也就无法建立可靠的挂起、审批和恢复边界。

## 决策

1. 新增独立 Mongo 集合 `agent_execution_runs`，以 Runtime Run ID 为 `_id`。
2. 状态记录只保存租户、路由、Profile/Prompt 版本、Token/成本、状态时间和以
   Run ID 域隔离的内容摘要；不保存 API Key、Credential、Tool 原始参数、回复正文
   或恢复令牌。
3. 新 Run 必须从 `running@revision=1` 创建；完成、失败、取消、等待人工和等待审批
   均通过 `user_id + run_id + status + revision` CAS 原子提交。
4. 同一个 Run ID 贯穿 Runtime、Trace、对话元数据和权威状态，不再生成平行身份。
5. 模型调用前若无法创建状态，停止执行；执行结束后若无法提交状态，不返回成功
   响应。对话持久化等后处理失败时，Run 记录为 `failed`，不能误报 `completed`。
6. `agent_run_traces` 继续作为证据，`agent_workflow_runs` 继续作为 DAG 状态；三者
   集合和职责保持隔离。
7. `ask_human` 与待审批 `tool_call` 挂起均使用版本化、限长且 AES-GCM 加密的 Runtime
   Checkpoint；Checkpoint 不保存 Tool Definition。人工回答恢复时重新装配当前只读 Tool；
   工具审批恢复时重新装配当前受治理目录，并精确校验待处理 Action、审批与工具契约。
8. 人工回答恢复使用 `status + revision + lease + attempt_id` 原子领取；过期租约可回收，
   旧 Attempt 不能提交。查询 API 只返回脱敏生命周期投影，不返回密文、Nonce、Key ID、
   Attempt ID、租约或人工回答。
9. `resume_supported=true` 只适用于已经完整保存并验证的 `ask_human` 或工具审批 Checkpoint。
   它不授予工具权限；工具恢复还要求既有 Tool Approval 已批准且未过期，并通过 User、Run、
   Revision、Action ID、Tool、Category、输入摘要、稳定幂等键和一次性 Resume Token 联合校验。
10. 工具审批签发、领取恢复和真实远端调用前都重新读取当前 Profile/Prompt、Connection、
    Credential Version、Active Snapshot、Schema 与 Tool Policy。`risky` 最多一次远端尝试；
    `write` 必须具备已审核的声明式幂等参数，并在有限重试中复用同一平台稳定键。
11. Tool Approval 继续作为 Workflow 与 Unified Agent 的共同事实源。拒绝或审批过期会终止
    对应 Agent Run 并清除 Checkpoint、授权哈希和租约；不新建平行审批状态机。

## 迁移与回滚

- 迁移只新增集合和索引，不修改旧 Run、Trace、Workflow 或对话文档。
- `AGENT_RECOVERABLE_RUNS_ENABLED=false` 为默认值，关闭时保持既有执行路径和不可恢复语义。
- `AGENT_UNIFIED_APPROVAL_RECOVERY_ENABLED=false` 为独立默认值；只有它、可恢复 Run 和外部 MCP
  三者同时启用时，Unified Agent 才装配需要审批的外部 `risky/write` 工具。关闭任一开关可
  回退到只读 Agent Catalog，Workflow 的既有审批路径不受影响。
- 开启时若状态仓储或索引不可用，服务失败闭合；回滚只需关闭开关，保留文档供
  审计和后续迁移，不删除集合。
- 开启恢复的环境必须配置独立 `AGENT_RUN_CHECKPOINT_MASTER_KEY` 或 Keyring；它与
  Provider Config、MCP Credential 密钥域隔离，缺失时生产启动失败。
- 开启该开关的环境必须同时确保对应 Capability 进入 Runtime 路径；Legacy 路径
  不会伪造可恢复状态。

## 影响

### 正向

- 服务重启后仍可查询 Run 的权威终态或挂起态，并可继续满足条件的 `ask_human` 或工具审批 Run。
- Trace 丢失、延迟或采样不会改变业务状态。
- Checkpoint、Resume Claim 与重新授权建立在稳定 CAS 边界上，且不会重放已成功旧 Step。
- 工具审批恢复只执行原 Step 内精确待处理动作，不再次调用产生动作的模型；一次性令牌只保存
  哈希并在签发时轮换，Web 不持久化明文令牌。
- 用户输入和输出正文不因生命周期治理而额外复制到状态集合。

### 代价与风险

- Mongo 状态提交成为启用环境的成功响应前置条件。
- 当前没有请求级幂等键；状态提交失败后，客户端盲目重试仍可能重复生成内容。
- Checkpoint 包含模型消息与待处理动作，属于敏感运行数据；必须使用独立密钥、租户与
  Run 绑定的 AAD、大小上限和摘要校验，并纳入密钥轮换与保留期治理。
- Profile/Prompt 版本漂移时当前实现释放领取并继续等待人工，不自动升级运行语义；
  运维需要修复版本可用性或明确终止该 Run。
- 外部 MCP 的幂等保证依赖第三方履行其 Schema Annotation 与参数契约，只能声明有限重放安全，
  不能声明跨系统严格 exactly-once。
- 真实第三方 MCP、Credential 轮换、多副本故障注入和审批过期运营流程仍需受控环境验收。

## 后续

补项目级连接与真实成员校验、管理员托管凭据和真实第三方 MCP 受控验收；随后增加
Credential 轮换、多副本故障注入、授权异常告警和远端幂等履约证据。在这些验证完成前，
不得把当前能力描述成开放 MCP 市场或严格 exactly-once。
