# P8 统一智能助手与能力生态计划

> 状态：Approved / Consolidation and acceptance
> 启动日期：2026-07-23
> 产品定位：面向社交内容创作与运营的可扩展 Agent 工作台
> 技术原则：统一入口、能力优先、工作流确定性、工具治理复用、旧接口可回滚

## 1. 决策摘要

当前 Chat、Consult、Assist、Multi、Workflow 五种模式是历史兼容入口，不再作为长期产品信息架构。目标体验是一个连续对话入口：用户描述目标，服务端在当前用户可用能力集合内选择模型、只读工具、受审批写工具或确定性工作流。

本阶段不删除旧 API，不复制 Runtime、ToolExecutor 或 Workflow Engine。统一入口先作为兼容编排层接入，旧入口继续可用；真实工具能力成熟后，Web 再从模式切换器迁移为能力面板。

## 2. 产品模型

### 2.1 用户可见对象

| 对象 | 用户语义 | 当前/目标状态 |
|------|----------|---------------|
| 对话 | 连续表达目标、补充信息和接收结果 | 当前已有，P8 统一为主入口 |
| 能力 | 搜索、读取、创作、分析、发布等可调用能力 | 站内、联网及受治理 MCP 已入目录；外部风险/写能力必须逐次审批并受独立开关控制 |
| 应用连接 | 用户或管理员连接的外部 MCP/API | 用户级、项目级和部署托管 Secret 引用已实现；运行时管理员/KMS 管理面待建设 |
| 自动化 | 可重复、确定性的工作流 | 当前已有 DAG，保留高级入口 |
| 深度任务 | 系统按复杂度选择的 Plan/ReAct/Multi-Agent 策略 | 当前以显式 Multi 模式存在，目标转内部策略 |

### 2.2 不再暴露的实现细节

- 不要求用户先判断“查询模式”还是“辅助发推模式”。
- Multi-Agent 是执行策略，不是默认产品栏目。
- Workflow 是可保存的确定性自动化，不等同于智能对话本身。
- Capability Hint 仅表达用户偏好，不能提升 Tool 权限或绕过审批。

## 3. 目标链路

```text
Unified Chat Request
        |
        v
Capability Planner
  - 对话上下文
  - 用户能力/连接
  - Tool Catalog Snapshot
  - Budget/Policy
        |
        v
Agent Runtime v2
  - Model
  - ReAct / Plan-Execute
  - ToolExecutor
  - Workflow-as-Tool
        |
        +--> Read Tools: platform.search / web.search / page.read
        +--> Write Tools: content.publish -> Approval
        +--> External MCP: server.tool -> Policy / Audit
        |
        v
Answer + Citations + Artifacts + Approval/Run State
```

首个增量仅实现统一契约和可替换的保守 Planner，通过 Chat、Consult、Assist 兼容路径运行。它不宣称动态组合多个工具，也不宣称拥有真实公网搜索。

## 4. 能力命名与契约

能力 ID 使用稳定命名空间，不使用前端文案作为协议：

| Capability ID | 语义 | 当前状态 |
|---------------|------|----------|
| `conversation.reply` | 普通连续对话 | Implemented |
| `platform.search` | 站内推文/用户检索 | Implemented |
| `content.draft` | 社交内容草稿生成 | Implemented |
| `content.publish` | 显式确认后发布 | Implemented / Approval required |
| `workflow.run` | 运行指定不可变工作流版本 | Implemented / 显式发布的只读工作流已接入统一入口 |
| `web.search` | 真实公网搜索 | Implemented / Feature Flag 与有效 Provider Config 必需 |
| `page.read` | 安全读取公开网页 | Implemented / 仅受治理联网 Profile 可用 |
| `connector.mcp` | 用户/项目级外部 MCP 工具 | Implemented / Feature Flag、成员权限、Active Snapshot、Tool Policy；风险/写入还要求审批恢复 |

统一请求中的 `preferred_capability_ids`：

- 可为空；为空表示自动选择。
- 只是 Planner Hint，不是授权声明。
- 未知或尚未启用的能力 fail-closed。
- 实际执行能力必须由服务端 Catalog、用户连接、Profile、Policy 与 Budget 的交集决定。

## 5. 分阶段实施

### P8.0 产品与技术契约

- [x] 确认统一对话为主入口，五模式降级为兼容执行 Profile。
- [x] 定义 Capability、Application Connection、Automation、Deep Task 的产品边界。
- [x] 定义渐进迁移、回滚和真实性约束。
- [x] 建立统一 RPC/HTTP 契约及保守 Planner。

验收：旧五入口行为不变；统一入口可以自动或按 Hint 选择现有安全路径，并返回实际执行 Profile/Capability。

实施记录（2026-07-23）：

- 新增 `RunAgent` gRPC/HTTP 契约、Web API Client 和向后兼容路由；旧五入口未删除或改义。
- 新增可注入 `AgentCapabilityPlanner`，默认仅选择 `conversation.reply`、`platform.search`、`content.draft` 三个已实现能力，并返回 `compat.chat/consult/assist` 执行证据。
- 未知能力与未实现的复合能力 fail-closed；`preferred_capability_ids` 不进入 Tool 授权判断。
- Agent/Gateway 全模块测试、受影响包竞态检测、目标 Vet 与 Web 生产构建通过。

### P8.1 统一 Agent 入口

- [x] Web 主入口的 Chat/Consult/Assist 统一进入 `RunAgent`，旧 API 保持兼容回滚。
- [x] 同一 Dialogue 内允许能力变化，不隐式创建新会话。
- [x] 建立不可变 Capability Catalog，并以精确能力集合解析有序执行路由。
- [x] Chat 切换到 Runtime v2，并使用 Catalog/Policy 选择工具。
- [x] 返回脱敏 Tool Activity 与站内结构化 Citation。
- [x] 返回可追溯 Artifact 与脱敏 Run/Approval State。
- [ ] 扩展公网 Citation（转入 P8.2，不作为 P8.1 站内闭环的完成条件）。
- [x] Web 移除五模式主导航，改为统一输入框、能力面板和自动化入口。

验收：用户无需切换模式即可完成站内查询、内容草拟和连续追问；执行记录能解释使用了哪些能力，但不暴露模型思维链。

实施记录（2026-07-24，第一增量）：

- 新增不可变 `AgentCapabilityCatalog`，校验能力版本、状态、依赖环、精确路由与返回副本；Catalog 路由不替代 Profile、Tool Policy、Budget 或 Approval。
- 新增 `platform.search -> content.draft` 精确组合路由和 `unified.research_draft@v1` Profile；一次 Runtime v2 Run 只暴露只读 `hybrid_search_tweets`，没有成功 Tool Observation 时 fail-closed。
- 组合任务复用请求中的 Dialogue，最终只持久化一组 user/assistant 消息，并记录 Capability IDs、Execution Profile、Profile/Prompt Version 与 Runtime Run ID。
- 本增量没有开放任意能力组合、公网 `web.search`、外部 MCP 或写工具，也没有完成 Web 五模式迁移。

实施记录（2026-07-24，第二增量）：

- MCP 站内搜索工具新增 `platform.tweet_search.v1` 结构化结果，同时保留供模型消费的文本 fallback；Runtime `ToolResult/Observation` 透传独立结构字段，不把该字段额外注入 Prompt。
- `RunAgent` 追加脱敏 `tool_activities` 与 `citations`。Tool Activity 不返回参数、原始输出或错误正文；站内 Citation 只接受受信任搜索工具的版本化结构、校验数字 Tweet ID、由服务端派生站内 URL，并执行去重、数量和摘要长度上限。
- 单能力兼容 Consult 可把既有 `TweetResult` 映射为站内 Citation；旧响应字段和旧五入口保持不变。统一 Web UI、公网 Citation 与外部 MCP 仍未完成。

实施记录（2026-07-24，第三增量）：

- `conversation.reply` 从 `compat.chat` 切换为显式 `runtime.chat` 路由；统一 `RunAgent` 固定进入 Runtime v2，复用 Model Catalog、Message Builder、Token/成本/并发预算、版本化 Profile、Trace 和 Session Summary。
- 新增 `conversation.reply@v1` Agent Profile。普通对话不枚举 MCP Catalog、`AllowedTools` 为空且 Runtime 请求不携带工具；搜索、草拟和写入能力必须由 Capability Catalog 显式选择，不能从普通聊天中隐式获得。
- Runtime Chat 继续装配同一 Dialogue 的历史消息和认知上下文，保存实际 Profile/Prompt/Capability/Execution Profile/Run/Token 元数据；消息对持久化失败时不返回未落库答案。
- 旧 `/chat` 契约不删除，仍可通过 `AGENT_RUNTIME_V2_MODES=chat` 灰度进入同一 Runtime 实现；关闭该模式即回到原直连 Legacy 路径。统一入口若需紧急回滚，可停止前端切流并恢复旧 `/chat`。

实施记录（2026-07-24，第四增量）：

- `content.draft` 从 `compat.assist` 迁移为显式 `runtime.draft`。统一入口不受旧 Assist 灰度开关影响，仍复用同一 Runtime、Profile、预算、Trace 与只读 Tool Policy；`compat.assist` 仅保留为可注入回滚路由。
- 只有已持久化且具有 Run ID 的草稿才生成 `content.draft` Artifact；Artifact 以 `source_run_id` 绑定确认发布来源，并显式标记 `requires_confirmation=true`。正文持久化失败时不会返回 Artifact 或答案。
- `RunAgent` 追加 `run_status`、`artifacts` 与脱敏 `approval_state`。当前 Catalog 没有写 Capability，成功响应如实返回 `not_required`；真正的 pending/approved/rejected 状态仍以既有 Approval Repository/API 为事实源，普通响应不复制审批输入或一次性恢复令牌。

实施记录（2026-07-25，第五增量）：

- Web AI 助手删除五模式按钮和页面内分支，所有普通输入统一调用 `RunAgent`。能力选择器只传递 `preferred_capability_ids` Hint；服务端 Catalog、连接、Policy 与 Budget 仍决定实际执行能力。
- Workflow 不再作为聊天输入模式，保留为独立“自动化”入口；旧 Chat/Consult/Assist/Multi/Workflow API 未删除，紧急回滚只需恢复前端旧调用。
- 同一 Dialogue 内切换能力不会新建会话。历史消息继续以服务端 Dialogue/Message 为事实源；前端对并发详情请求使用递增序号，忽略已经过期的慢响应。独立工作流审批恢复只刷新会话事实源，不把结果临时追加到当前 Dialogue。
- 新响应展示实际 Capability、Run Status、脱敏 Tool Activity、站内 Citation 和可发布 Draft Artifact；发布继续要求用户检查正文并携带 Artifact `source_run_id` 调用确认接口。
- Web 生产构建通过，并在桌面/移动视口验证历史加载、统一发送、结构化证据、确认发布和快速会话切换。公网来源、真实写 Capability 和外部 MCP 仍未实现。

### P8.2 真实联网搜索

- [x] 用 Provider-neutral 接口和 Brave Adapter 替换 Mock `WebSearch`；未启用，或既无平台 Key 又未选择有效用户配置时明确失败，不再返回伪结果。
- [x] 新增 `web.search`、`web.search.v1`、`page.read`、`web.page.v1` 结构化证据和 `web_page` Citation。
- [x] 复用 Endpoint Policy 与受限 HTTP Client，覆盖 URL、SSRF、重定向和 DNS Rebinding 防护。
- [x] Redis 来源缓存、用户速率和每 Run 请求/估算成本预算已实现；Provider/Page Reader 继续执行并发、响应体、结果数/字符和超时硬上限。
- [x] 搜索摘要与网页正文进入模型前均标记为不可信数据；Page Reader 移除隐藏/脚本内容、隔离常见 Prompt Injection 指令，并保留结构化 Safety 信号。
- [x] 用户级 Brave Provider Config 使用 AES-256-GCM 加密、租户过滤、Revision/凭据版本与撤销治理；AI 助手和 Workflow 可显式选择，模型不能提交或覆盖配置 ID。

实施记录（2026-07-25，第一增量）：

- `internal/module/agent/websearch` 定义窄 Provider 接口，Brave 协议、认证头和响应映射只存在于 Adapter；Workflow、MCP、Runtime 和前端不依赖 Brave DTO。
- 已保存 Workflow 继续使用 `WebSearch` 组件名，但无 Provider 时返回 `ErrUnavailable`。内部 MCP 新增只读 `web_search`，与 Workflow 共享同一 Provider 实例和 ToolExecutor 治理。
- `web.search` 默认在 Capability Catalog 中为 `planned`；只有 `AGENT_WEB_SEARCH_ENABLED=true` 且配置通过校验时才变为 `available`，并开放 `runtime.web_search` 与 `web.search + content.draft` 精确路由。
- Runtime Profile 只暴露 `web_search`，要求真实成功 Observation；外部标题、摘要和 URL 只能通过版本化 Structured Content 生成 Citation，回答文本不能伪造来源。
- 首版仅提供服务级 Brave 密钥。用户级搜索 Provider Config、来源缓存、配额/成本账本、`page.read` 与远程 MCP 不在本增量完成范围内。
- 离线测试覆盖 Provider 响应归一、查询/响应体/并发/超时硬上限、重定向不转发凭据、Provider 错误脱敏、Workflow/MCP 结构化结果和 Citation 信任边界；部署检查覆盖 Secret 正常注入与缺失时失败关闭。尚未使用真实 Brave 凭据做公网验收。

实施记录（2026-07-25，第二增量）：

- `HTTPPageReader` 复用 Endpoint Policy 和受限 HTTP Client，拒绝私网/本地/凭据 URL、Fragment、重定向、DNS Rebinding、非文本 MIME 与越界响应；HTML 只提取可见文本并移除指令型行。
- Search/Page Read 共享 Redis TTL 缓存与 Lua 原子准入。服务端从 ToolExecutor 执行上下文注入用户和 Run 身份，用户/模型参数无法覆盖；Redis 准入故障时 fail-closed，缓存故障仅降级为直读。
- Runtime 联网 Profile 仍要求成功 `web_search`，可按需 `page_read` 高价值来源；Page Read Structured Content 可生成或丰富同 URL Citation，回答正文中的链接仍不受信任。
- Workflow Editor 暴露独立 WebSearch/PageRead 组件；普通对话与普通草拟不会自动获得公网工具。Compose/Helm 支持独立关闭 Page Read 与配置缓存、请求数和成本预算。
- 第二增量当时仍只有服务级 Brave Key；该限制已由第三增量解除。

实施记录（2026-07-26，第三增量）：

- Provider Config 以 `kind=llm|web_search` 隔离，旧记录兼容为 `llm`。用户 Brave API Key 复用既有 AES-GCM/Keyring 生命周期，响应只暴露 `has_secret`、Credential Version 和 Revision。
- AI 助手通过可信 Run 上下文选择个人搜索配置；MCP Schema 不公开内部配置字段。Workflow WebSearch 保存配置引用并在执行时按用户校验，跨租户、错误类型和撤销引用都 fail-closed。
- 动态 Adapter 与平台兜底共享部署级并发闸门、Redis 来源缓存及用户/Run Governor；用户不能覆盖超时、结果数、响应体、Endpoint Policy 或成本预算。
- 前端增加个人联网 API 管理与选择，并将 LLM/WebSearch 配置下拉按类型隔离。平台 Brave Key 改为可选兜底，Compose/Helm 支持 Provider Config 主密钥和轮换 Keyring。
- 离线后端、Web Build 与部署渲染已验证；真实 Brave 搜索质量、真实费用和生产 Redis 容量仍未验收。

验收：搜索结果包含可验证来源；Provider 故障明确返回失败或降级，不伪造“正在获取”。

### P8.3 外部 MCP 与连接目录

- [x] 建立用户级与项目级 MCP Server Config/Connection；项目成员使用 `owner/editor/viewer`、User Service 精确存在性校验和实时撤权。
- [x] 支持远程 Streamable HTTP/SSE；多租户服务端不开放任意 stdio。
- [x] Tool Discovery 生成不可变 Schema Snapshot，更新需重新审核。
- [x] 工具名使用 `server_id.tool_name` 命名空间，避免冲突。
- [x] 用户 Bearer 凭据复用 AES-GCM Keyring；项目连接可引用部署托管只读 Secret。运行时管理员 CRUD、KMS/Vault Adapter 仍待独立管理面。
- [x] Discovery 已有独立 Feature Flag、Egress Allowlist、DNS/IP 限制、租户隔离和健康状态。
- [x] 将已审核且显式启用的只读工具按 Snapshot 接入动态 Tool Catalog，并复用现有 ToolExecutor 的 Schema、身份、超时、重试、熔断、结果治理、审计和 Trace。
- [x] 提供工具级策略 API 与 Web 管理面；Schema 变化暂停执行，新 Snapshot 审核后清空旧策略。
- [x] Workflow 与灰度启用的 Unified Agent 可对外部 `risky`/声明式幂等 `write` 工具逐次 Approval；批准恢复与真实调用前重新校验 Connection、Credential、Snapshot、Schema 和 Tool Policy。
- [x] SDK Adapter 使用可关闭的有界 Session Pool，并通过 Mongo 跨实例租约执行主动健康巡检；健康状态只用于诊断，不授予执行权限。
- [x] Unified Agent 增加独立 `agent_execution_runs` 权威生命周期和 Revision CAS，准确保存完成、失败、取消、等待人工与等待审批。
- [x] 增加版本化/限长/独立密钥加密 Checkpoint、租户隔离 Run 查询、Resume Claim 租约和恢复时重新授权；支持 `ask_human` 与审批 Tool Call，Checkpoint 不固化 Tool Definition。
- [x] 审批恢复重新校验一次性授权、Tool Policy、Connection、Snapshot、Credential 与副作用幂等绑定；高风险/写工具仍由独立 Feature Flag 默认关闭。
- [x] 增加本地 Conformance Server、显式 Live 验收 Runner、故障/幂等/轮换探针、脱敏 HMAC 报告和默认关闭的 Helm 一次性 Job。
- [ ] 使用真实第三方 MCP、Kubernetes Projected Secret、多副本故障和项目成员跨服务链路完成受控验收。

验收：外部只读 MCP、Workflow/Unified Agent 审批恢复、声明式幂等写入、Session Pool、健康巡检、权威 Run 与加密恢复均已有离线安全/竞态测试；新增验收框架可生成脱敏签名证据，但当前只执行过本地回环协议夹具。真实第三方、旧 Token 撤销、代理空闲策略、Projected Secret、多副本与跨服务成员链路仍未执行，因此 P8.3 保持 `Partial`。

### P8.4 Skill 与 Workflow-as-Tool

- [x] Skill 版本包含指令、允许工具、知识/Profile、预算和输出契约。
- [x] Workflow 可以作为受治理 Tool 被 Agent 调用。
- [x] 成功任务可显式保存为自动化模板，不自动把一次对话变成流程。
- [x] 父 Agent Run 与直接子 Workflow Run 提供版本化预算/成本只读聚合。
- [x] 只读 Workflow/Skill 的显式人工输入 Wait 可加密挂起并恢复同一子 Run。
- [x] 受治理写入/风险 Workflow Tool 由子 Run 独占审批与一次性授权，并委托父 Agent 恢复原 Action。
- [x] Multi-Agent 候选只在复杂度、角色 Tool Scope、预算和延迟允许时由 Planner 影子准入，并记录单 Agent 回退原因。
- [x] 两个只读研究草拟模板已有顺序聚合生命周期执行器；Planner 仅在准入与执行开关、预算和 Tool Scope 全部满足时把实际 `selected_strategy` 切换为 `multi_agent`。

实施记录（2026-08-01，第一至第十增量）：

- Workflow Editor 增加显式“发布给 Agent”控制；发布记录按用户隔离并绑定不可变 Revision、DSL Hash、稳定工具名、Input Schema 与 Revision CAS。继续编辑草稿不会静默改变已发布行为。
- `workflow.run` 通过独立 Feature Flag 和 `runtime.workflow` Profile 接入 Unified Agent。每次请求只装配当前用户仍有效的发布目录，模型不能指定其他用户、Workflow Revision 或父 Run 身份。
- 可发布 DAG 持续拒绝补偿、Agent 节点、递归 Workflow Tool、未知工具和外部回调；只读工具不得审批，写工具必须逐次审批且声明幂等，风险工具必须逐次审批，审批恢复基础设施不可用时发布 fail-closed。外部 MCP 按当前 Snapshot/Policy 做同等校验。
- 动态 Workflow Tool 复用统一 ToolExecutor 的 Schema、超时、幂等结果、熔断、结果体积、审计与 Trace，并把父 Agent Run/Action 记录到子 Workflow Run。父子准入预算保持独立，父 Tool Timeout 约束子运行。
- Skill Catalog 从当前用户 Active Workflow 发布确定性投影，不另建可漂移事实源；精确版本固定指令、Tool、知识/Profile、预算和输出契约，只允许显式 `skill_id + version`。
- 新增独立 `agent_task_templates`：用户只能从自己的 completed 权威 Agent Run 主动保存模板，并自行编写含唯一 `{{input}}` 的指令；不读取或复制整段对话，也不自动生成 Workflow。
- 模板固化源 Run 的 Profile、Capability、精确 Skill 版本与结果摘要证据，执行前重新校验源证据和当前路由；模型及用户级 Provider Config 仍由当次请求选择。内容创建幂等，归档使用 Revision CAS。
- 第三增量已通过跨层定向测试、目标 Race、独立 Vet、Web 生产构建与部署配置验证；浏览器用一次性契约夹具检查桌面和 `390x844` 的模板选择/归档布局。该证据不代表真实 Mongo、模型或 Workflow Tool 已完成 live 验收。
- 父 Agent Run 与 Workflow Run 直接保存 `execution.accounting.v1` 预算/用量快照；专用租户索引有界查询直接子 Run，分别返回父级、子级和合计，并显式标注旧记录、运行中和截断数据。该视图不解析业务 JSON/Trace，不递归，也不改变准入预算。
- 第四增量已通过 Agent/Gateway 扩大回归、目标 Race、独立 Vet、Web 生产构建及桌面/移动布局检查；真实 Mongo 父子运行仍需受控环境验收。
- 第五增量允许发布中显式 `resume_mode=human_input` 的 Wait；父 Agent 将 Tool Continuation 写入加密 Checkpoint，恢复时重校验 Active 发布、不可变 Revision/Hash、父子谱系与 Action，并恢复同一子 Run。外部回调和用户提供 Resume Token 继续拒绝；审批型工具由第六增量的委托恢复处理。
- 编辑器不再向用户暴露 Resume Token；移动端组件库使用抽屉、属性面板使用覆盖层，并支持点击添加节点。Agent 回归、目标 Race/Vet、Web Build 和桌面/移动真实交互检查通过；本地 Agent API 500 仍阻断 live Workflow 调用。
- 第六增量为 Runtime 增加委托审批恢复类型。子 Workflow 独占审批事实、幂等绑定与一次性 Grant，父 Agent Checkpoint 只保存子审批引用；审批中心使用子 Grant 恢复父 Agent，由父 Runtime 继续原 Workflow Tool Action，不创建第二条审批或父级令牌。
- 恢复前重校验用户、父子谱系、Action、发布 Revision/DSL Hash、审批状态/摘要/幂等键与 Grant。子 Run 已成功但父提交中断时从权威 Blackboard 回放；拒绝或过期终止父子 Run。离线闭环验证批准后写工具只执行一次且旧令牌不能重放。
- 第七增量新增独立、无 I/O 的 `agent/strategy` Planner。复杂度只使用有界稳定信号，角色模板固定为研究、草拟和审查；研究角色仅获得对应搜索 Tool Scope，其余角色无工具。Step、Token、估算成本、延迟、角色数和并发任一不满足都返回稳定回退原因。
- `agent.execution_strategy.v1` 计划证据写入权威 Agent Run并通过统一响应/查询返回；计划摘要覆盖候选、决策、原因、复杂度信号、角色范围和预算，不包含原始问题、Prompt、凭据或参数。开关仅启用影子准入，聚合执行器缺失时强制选择单 Agent，旧硬编码 Multi API 保持兼容但不进入新路径。
- 第七增量已通过 Planner/跨层契约/Agent 全模块普通测试、受影响包 Race 与 Vet、Web Build、Compose 和 Helm 配置验证；均为离线 Fake/内存证据，不代表多角色模型调用已经发生。
- 第八增量仅为 `platform.research_draft.v1` 与 `web.research_draft.v1` 安装顺序 `researcher -> drafter -> reviewer`。研究角色获得搜索工具，草拟/审校无工具；共享父预算累计 Token/成本，角色失败后整体失败且不隐藏重跑单 Agent。
- 第九增量新增固定 20 Case 的 `agent.strategy-comparison.v1`，以同数据集、模板、Provider/Model/Pricing、环境和超时比较质量、成本和 P95；录制夹具只验证门禁契约。
- 第十增量把聚合算法抽为生产 Service 与 Eval 共用的 `agent/multirole`，并新增 `--strategy-runtime-config`。CLI 在同一规范化配置哈希内自动执行真实模型 Multi 候选和 Single 稳定侧，配置不同直接不可比较；本地协议集成已通过，但尚未运行真实 20 Case Provider 报告。
- 本阶段只有两个只读顺序模板，不支持任意角色拓扑、并行角色、角色级恢复或写工具协作；Search/Page Read 评测仍使用无副作用结构化证据沙箱。不能把 P8.4 描述成开放技能生态、生产搜索质量或模型可无约束自主组队。

### P8.5 评测与扩展目录

- [x] 增加统一入口任务完成率，并复用固定任务集的工具选择准确率和澄清结果指标。
- [x] 增加用户可见 Citation 的结构有效率；事实正确性和 Groundedness 继续由版本化 Eval/人工签认判断。
- [x] 增加具备跨请求幂等归因的发布采纳率、Connector 激活/复用率。
- [x] 增加每个完成任务的延迟、Token、成本和 Tool Failure 指标。
- [x] 建设租户已安装的 Capability/Skill/MCP 联合扩展目录，复用现有授权事实源并保持执行治理不变。
- [x] 完成公共扩展市场的信任与发布控制面：发布者身份、不可变清单、Ed25519 复验、公钥轮换/吊销、签名发布、版本撤回和追加审计。

Artifact 分发、恶意包扫描、依赖解析、安装审批、租户安装状态和 Owner 转移从当前产品完成条件中移除，进入需求驱动的 Deferred Backlog；没有真实发布者与安装需求证据前不继续扩建。

实施记录（2026-08-02，第一增量）：

- 新增 `UnifiedAgentProductObserver`，生产使用低基数 Prometheus 实现；Observer 只接收执行 Profile、单/多策略、固定生命周期状态、Usage 和结构化 Tool/Citation 投影，不接收用户、Run、模型、工具名、URL 或正文 Label。
- `agent_unified_tasks_started_total` 只在权威 `AgentExecutionRun` 创建成功后增加；Outcome、端到端耗时、Step、Token、成本、Tool Result 和 Citation 只在 Revision CAS 提交成功后增加。提交失败不会伪造成完成任务。
- 完成率以 `completed outcome / accepted task` 计算；`awaiting_human` 是线上澄清转换信号，固定任务级澄清率和工具选择准确率继续由 `agent-task-eval` 的标注数据计算，生产遥测不猜测用户意图。
- Tool 指标不使用动态 Tool Name Label，可按 `outcome=completed,result=failed` 计算每个完成任务的失败工具数。Citation 仅校验平台推文 ID/路径或 Web URL/稳定摘要 ID 的结构绑定；它不验证网页仍在线，也不证明回答中的事实成立。
- 当前指标依赖 `AGENT_RECOVERABLE_RUNS_ENABLED=true` 的权威生命周期。发布采纳和 Connector 激活必须先建立可重放、跨请求幂等的产品事件归因；本增量没有用发布请求次数、进程内缓存或连接池打开次数冒充真实采用率/激活率。

实施记录（2026-08-02，第二增量）：

- `AgentExecutionRun` 增加权威 `publishable_draft`、`published_tweet_id` 和 `draft_published_at`。只有已完成且明确产出可发布草稿的 Run 才能确认发布；发布后以原子条件更新记录首次采纳，同一确认请求、TweetService 幂等重放和进程重启均不会重复计数。
- 新增通用 append-only `agent_product_events`，事件 ID 由稳定业务事实确定性生成。事件只保存租户、主体、关联主体、固定维度和发生时间，不保存 Prompt、草稿、Tool 输入输出、Endpoint 或凭据；重复写入会读取并校验既有事实。
- Connector `configured` 绑定已持久化 Connection，`activated` 只在首个已审核工具被显式启用后产生；仅发现 Schema 或审核 Snapshot 不视为激活。`first_used` 要求治理后的工具调用成功，`reused` 要求同一 Connector 在至少两个不同 Agent/Workflow Run 中成功执行；连接池 Session 重用不属于产品复用。
- 新增 `agent_unified_draft_events_total{execution_profile,strategy,event}` 与 `agent_external_mcp_product_events_total{scope,transport,event}`。发布采纳率按 `published / ready`，Connector 激活率按 `activated / configured`，首用率按 `first_used / activated`，跨 Run 复用率按 `reused / first_used` 计算；所有 Label 都是固定枚举。

实施记录（2026-08-03，第三增量）：

- 新增纯领域 `agent.extension.v1` 目录契约，按 Capability、Skill、MCP Tool 做有界过滤、确定性排序和绑定过滤条件的稳定 Cursor；目录最多聚合 256 项，单页最多 50 项。
- Service 只聚合既有不可变 Capability Catalog、当前租户 Active Skill 投影和 `ListGovernedTools`。MCP 来源继续重查个人/项目权限、Active Snapshot 与 Tool Policy；目录不包含 Endpoint、Credential、输入 Schema 或 Tool Result。
- Skill 条目只携带精确 `skill_id + version`，选择后仍由 Skill 解析器重新校验；MCP 条目只提供连接管理引用，真实调用仍经过 Runtime、ToolExecutor、Policy、Budget 与 Approval。目录不是新的执行路径。
- 新增 `GET /api/v1/agent/extensions`、gRPC 契约与 Web 扩展面板，支持搜索、类型过滤和 Cursor 加载更多。`AGENT_EXTENSION_CATALOG_ENABLED=false` 为独立回滚开关，关闭不修改 Capability、Workflow 发布、Skill 或 MCP 连接数据。
- 本增量只完成租户已安装目录。公共发布者信任、包签名、安装审批、依赖解析、恶意扩展扫描和版本撤回尚未实现，因此不能描述为开放 Skill/MCP 市场。
- 产品事件写入采用可重放的尽力修复：业务发布或 Connector 调用成功后，不因指标存储短暂失败撤销已完成的外部副作用；后续幂等请求会补写缺失事件。权威 Run/Connection 状态仍是业务事实源，Prometheus 只是低基数投影。

实施记录（2026-08-03，第四增量）：

- 新增独立 `agent.extension_marketplace.v1` 领域，不复用租户已安装目录作为信任源。发布者只保存公开身份和 Ed25519 公钥；Manifest 固定包 ID、类型、SemVer、发布者、Artifact SHA-256、Capability 与声明权限，并使用规范序列化签名。
- 版本 ID 由规范 Manifest 确定性生成，Mongo 以 `package_id + version` 唯一键做不可变插入。读取时仍重新校验发布者状态、密钥状态、版本 ID 与签名；暂停发布者、撤销密钥、缺失发布者或篡改记录全部 fail-closed。
- 新增 `GET /api/v1/agent/marketplace/extensions`、追加式 gRPC 契约和只读 Web 市场面板。公开投影不返回公钥、原始签名、Artifact URL/字节、Endpoint、Credential 或安装授权，也不提供伪安装按钮。
- `AGENT_EXTENSION_MARKETPLACE_ENABLED=false` 独立默认关闭；关闭时不创建市场索引，不影响现有 Capability、Skill、MCP 和租户扩展目录。第四增量完成时，发布者控制面、版本撤回与密钥轮换仍未实现；其中信任控制面已由第五增量补齐，安装链路则按范围冻结转入延期项。

实施记录（2026-08-03，第五增量）：

- 新增 Marketplace 专属内部认证、平台管理员和持久化 Publisher Owner；Gateway 只注入 JWT 操作者与内部令牌，Agent Service 每次写操作都重新校验所有权。
- 公钥支持 Active、Retired、Revoked 生命周期和 Revision CAS。Retired Key 只验证历史版本，不能发布新版本；Revoked 为终态并使相关签名失效，私钥从不进入 API、Repository、审计或响应。
- 发布使用规范 Manifest、离线 Ed25519 签名和不可变 Package/SemVer；撤回使用 `published -> withdrawn` 终态迁移。所有控制操作写入低敏、追加式 requested/succeeded/failed 审计。
- Proto、gRPC、Gateway、独立管理页、Compose/Helm 灰度与 Secret 门禁已同步并通过离线测试、Race、Vet、Web Build 和部署静态验证；真实 Mongo 多副本与 Secret 轮换仍属受控环境验收。
- 本增量只完成信任控制面，不提供 Artifact 上传/下载、扫描、依赖解析或安装按钮。公共市场能力在此冻结，不再作为默认下一轮。

## 6. 迁移计划

1. 新增 `RunAgent`，旧 Chat/Consult/Assist/Multi/Workflow API 保持兼容。
2. 第一阶段 Planner 仅选择已实现的 Chat/Consult/Assist 路径。
3. 通过不可变 Catalog 逐个注册并验收精确组合路由；未注册组合继续 fail-closed，不能据此宣称任意动态编排。
4. Web 先接统一 API，再将旧模式入口收进“高级/兼容”区域。
5. 观察统一入口质量后，旧 API 进入 Deprecated；删除需要单独版本决策。

数据迁移不修改 Dialogue ID。能力变化必须复用同一 Dialogue，并在消息/Run 元数据中记录实际执行 Profile 和能力 ID。

## 7. 回滚计划

- `RunAgent` 是新增入口；关闭前端切流即可回到旧 API。
- 保守 Planner 通过依赖注入替换，不改变 Runtime、Repository 或 ToolExecutor。
- 外部 MCP、真实 Web Search 和 Workflow-as-Tool 分别使用独立 Feature Flag。
- 写工具审批策略不参与降级；治理组件不可用时继续 fail-closed。
- 旧 Dialogue、Message、Workflow Revision 和 Run 数据不做破坏性迁移。

## 8. 主要风险

| 风险 | 控制 |
|------|------|
| 工具过多导致选择错误和上下文膨胀 | 权限过滤后再做候选召回，只向模型暴露 Top-K Schema |
| 统一入口退化成隐藏硬编码模式 | 保守 Planner 只作为迁移层，P8.1 必须接入 Catalog 驱动执行 |
| 外部 MCP 数据泄露或越权 | Egress Policy、Schema Snapshot、用户授权、Tool Policy、Approval 和审计 |
| 网页 Prompt Injection | 外部内容标记为不可信数据，禁止覆盖 System/Policy |
| Multi-Agent 成本和延迟失控 | 默认单 Agent，复杂任务才启用，受 Run Budget 和最大 Step 约束 |
| 自动化与智能对话语义混淆 | Workflow 保持确定性；Agent 仅通过显式 Tool 契约调用 |
| 做成无差异 ChatGPT 克隆 | 聚焦社交内容研究、创作、发布、效果归因和自动化闭环 |

## 9. 首批实现范围

首批只包含：

1. [x] `RunAgent` RPC/HTTP 契约。
2. [x] 可注入的保守 Capability Planner。
3. [x] `conversation.reply`、`platform.search`、`content.draft` 三个已实现能力。
4. [x] 实际 Execution Profile 与 Capability IDs 返回。
5. [x] Service、gRPC、Gateway 契约测试和 API 文档。

P8.0 首批当时明确不包含真实公网搜索、外部 MCP、前端五模式删除或 Multi-Agent 自动选择；后续增量的当前状态以第 5 节实施记录和第 10 节收口门禁为准。

## 10. 2026-08-03 产品收口与范围冻结

### 10.1 核心产品承诺

本项目不继续扩张为通用 Agent 商店。当前产品定位固定为：面向社交内容研究、创作与运营的可治理 Agent 工作台。核心闭环只有一条：

```text
自然语言目标 -> 站内/公网/已授权 MCP 取证 -> 带 Citation 的草稿
             -> 用户审批发布 -> 保存为可重复自动化 -> Run/成本/效果可追踪
```

技术学习重点固定为 Go Agent Runtime、ReAct/Tool Calling、MCP 治理、Workflow DAG、Checkpoint/Resume、RAG、预算与可观测性；不再用新增同类组件数量衡量学习成果。

### 10.2 当前成熟度

| 范围 | 状态 | 收口判断 |
|------|------|----------|
| Runtime、Tool、Workflow、Approval、Replay | Implemented | 代码与离线测试已形成完整技术主线，不再扩展新执行模型 |
| 统一助手、联网、MCP、Workflow-as-Tool | Partial | 功能已实现，但默认开关、自动路由、真实环境验收和用户体验尚未形成一个稳定成品 |
| Multi-Agent | Partial / Optional | 仅保留两个有评测证据的顺序只读模板；不开发任意拓扑、并行角色或写工具协作 |
| 公共扩展市场 | Frozen | 信任与发布控制面完成；安装生态不是当前产品完成条件 |
| 交付基线 | Partial | 工作区改动规模过大，尚需形成可审查提交、启动手册和端到端演示证据 |

### 10.3 剩余四道完成门禁

1. **可审查交付基线**：按 Agent Core、Workflow/RAG、P7 Eval、P8 产品、部署/文档拆分工作区，建立可回滚提交和最小启动配置；不混入无关 Mobile/社交业务改动。
2. **统一助手体验收口**：自动模式应在已授权、低风险工具集合内表现为一个 Agent，而不是依赖用户猜模式；显式 Skill/Workflow/写操作继续保持精确选择与审批。隐藏未启用能力，收拢高级入口，修复空态、错误态和连续对话一致性。
3. **三条真实价值链验收**：固定模型与配置下验证“联网研究并草拟”“真实 MCP 读取并继续对话”“发布 Workflow-as-Tool 并挂起/恢复”；每条同时保存成功、超时/撤权/预算失败证据，不用 Fake 冒充 Live。
4. **可交付演示与指标**：完成十分钟演示脚本、失败演示、最小部署手册和产品指标看板；记录延迟、Token、成本、Citation 有效率、草稿采纳和 Connector 首用/复用，不把代码行数或组件数当产品成果。

四道门禁完成后，P8 进入 Maintenance，不再自动开启 P9。

### 10.4 明确停止项

- 不继续开发 Artifact 市场、包扫描、依赖解析、租户安装和 Owner 转移，除非出现真实发布者或安装需求。
- 不继续开发并行 Multi-Agent、角色级恢复或更多角色模板，除非同模型评测证明质量收益足以覆盖成本和延迟。
- 不新增 SubWorkflow、Map/Reduce、Aggregator、事件触发器、代码执行沙箱或新中间件，除非三条核心价值链被当前能力明确阻塞。
- 不新增第二个搜索 Provider、更多 MCP 协议变体或更多模型适配器；先把 Brave、一个真实 MCP 和一个主力 Chat 模型验收到位。
- Mobile Agent 体验不作为当前 Web 产品收口阻塞项；是否补齐由独立产品优先级决定。

任何停止项重新进入计划前，至少需要一种证据：目标用户访谈、真实使用失败、产品指标瓶颈、受控验收缺口或面试叙事中无法由现有能力回答的核心问题。
