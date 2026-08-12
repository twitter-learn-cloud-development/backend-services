# Agent Runtime Context（当前实现与演进边界）

> 最近核对：2026-08-09
> 当前计划：`docs/AGENT_CORE_REFOCUS_PLAN.md`
> 历史强化计划：`docs/AGENT_RUNTIME_STRENGTHENING_PLAN.md`
> 进度：`docs/PROJECT_PROGRESS.md`

## 1. 阶段状态

| 阶段 | 状态 | 当前证据 |
|------|------|----------|
| G0 Goal Runtime 基线 | Complete | 已冻结外围扩张并建立 20 个端到端任务矩阵；`runtime/goal.go` 已新增 TaskSpec、Environment、EvidenceLedger 与 Verifier，未接入生产 RunAgent |
| G1 VerifiedRunner | Complete | opt-in Adapter 已复用 AgentRunner，支持工具交集、前后快照、结构化观察证据、完成验证、共享预算有界修复和 Verified Checkpoint/Resume；恢复时保留 Task/Evidence/Before/修复次数并重新授权当前工具。站内搜索严格 Schema Collector/Verifier 已通过两个默认关闭开关接入只观测 Shadow，复用既有 RunResult，不重复模型/工具调用，也不替换生产响应 |
| G2 Environment Packs | Complete | Twitter/Web Read、Workflow-as-Tool、External MCP 与 Tweet Write Pack 已闭环当前 Catalog/Task/策略交集和低敏快照；写操作由 Timeline 前后状态与领域 Collector/Verifier 验证，Environment 仍不执行工具。生产 Goal 执行继续关闭，真实审批/写入集成验收归 G4 |
| G3 Unified Planning | Complete | 一至三步短计划、确定性 Admission、一次受限恢复、显式能力默认 Planner 与低敏 TaskOutcome 已具备 E2E-02/11/18 离线证据 |
| G4 Task Migration | Complete | E2E-05 至 E2E-20 固定迁移矩阵已具备离线或受控进程内结果证据；E2E-20 固定 Provider 错误分类、显式允许回退和 denied/exhausted blocked 终态，不产生伪造答案。Legacy Runtime 仍是唯一生产执行所有者，生产 Goal execution 与部署态依赖继续待验收 |
| G5 Cleanup | In Progress | 首个增量删除无生产调用者的关键词能力 Planner、命名兼容构造器及专属测试；显式能力 Planner/Catalog 保持唯一默认路由。历史 Compat Profile 因旧会话恢复与指标仍可达而保留；Marketplace/Profile 继续冻结 |
| P0 真实性/迁移护栏 | Complete | ADR、兼容测试、`AGENT_RUNTIME_V2_MODES`、回滚路径 |
| P1 AgentRunner | Complete | Runtime 类型、ReActRunner、Action、Adapter、Profile、灰度入口 |
| P2 Message/Token/Model | Complete | Message/Token/Cost Budget、Catalog 路由、并发准入、Provider Config 加密管理与 Session Summary 已落地 |
| P3 Tool Policy/Approval | Complete | ToolExecutor、审批/API/UI、一次性恢复、持久幂等、TweetService 原生幂等、熔断指标、治理对账与 MCP 双重认证已完成 |
| P4 Workflow Engine | Complete | 版本化 DSL/IR、StateView/单写者 Delta、Reducer、Revision、Snapshot/Replay、节点状态机、持久补偿、共享预算、补偿巡检/人工控制与 Temporal 决策门已闭环 |
| P5 Observability/Control | Complete | Run 控制、独立 Trace、脱敏查询/控制台、跨协议 OTel、低基数指标、有界事件流、对象引用、可检索 Blackboard、模板身份、安全预览采样和 Agent Grafana 面板已完成 |
| P6 | In progress | 已切换共享 Episodic Collection、Qdrant `user_id` Filter、迁移命令、只读迁移验收报告、可复现 BM25/Vector/RRF/RRF+Rerank Runner、四模式 Router Provider 对照 Runner、RAG/Router live 护栏、错投率离线基线、显式 Session End，以及 52 条 Agent 行为/安全任务集、固定录制结果 Runner 和受控 Live Runtime Adapter；真实集合回填、真实检索报告、真实 Semantic/LLM Router 和真实 Profile 任务基线仍待完成 |
| P7 | In progress | 已完成不可变 Profile Catalog、Mongo 版本/Release 仓储、双人发布审批、受保护管理 API/UI、项目 RBAC、跨实例刷新、运行指标与业务结果 A/B 门禁、CAS 自动回滚，以及 Assist 显式确认发布到 `draft_published=true`、外部点赞/评论到 `content_engaged=true` 的可信正向事件链路；内容互动专用 DLQ 已有默认检查、显式限量重放、Broker Confirm 与脱敏报告命令；Agent Eval 已有签名、Object Lock 归档和发布审批复验。真实 Bucket/52 条基线、真实 DLQ 演练、明确拒绝动作、受控线上样本和外部组织目录仍待完成 |
| P8 | In progress | P8.1 核心已完成；P8.2 已具备受治理 Web Search/Page Read 与用户级加密搜索配置；P8.3 已完成个人/项目级远程 MCP Connection、真实成员校验与实时撤权、不可变 Schema Snapshot、Workflow 与 Unified Agent 的高风险/声明式幂等写入审批恢复、有界 Session Pool、主动健康巡检、权威 Agent Run 与 `ask_human/tool_call` 加密 Checkpoint/Resume、部署托管项目凭据，以及显式 Live/Write 的脱敏签名验收框架；P8.4 已完成 Workflow-as-Tool、精确版本 Skill、任务模板、父子预算成本视图、两类 Tool Continuation、Multi-Agent 版本化准入证据、两个只读研究草拟模板的顺序三角色聚合执行、生产/Eval 共用聚合核心、显式 ToolChoice、最终步收束护栏、加密复核/失败诊断材料通道、v3 实质证据/Claim/Citation/证据不足确定性门禁、独立版本化内容签认、Signoff 强制 WORM/Profile 发布资格链，以及 Live Eval 离线计划、签名授权、file/Redis 调用前预算账本和外部人工 Decision 交付模板；P8.5 已完成产品 SLI、草稿/Connector 可重放产品事件、租户已安装联合目录、签名公共目录，以及发布者 Owner、专属内部认证、公钥轮换/吊销、签名发布、CAS 撤回和追加审计控制面。固定 qwen3.7 + Profile v5 的 v3 云报告已通过自动双门禁并完成 HMAC/Bundle 复验；真实外部人工签认、WORM、真实搜索验收、公共市场 Artifact/扫描/安装、并行角色和角色级恢复仍待完成 |

P8.4 状态覆盖（2026-08-02）：第一至第六增量的 Workflow/Skill/模板/核算/Continuation 能力保持不变；第七至第十一增量完成 Planner、顺序三角色执行、生产/Eval 共用核心、同配置策略门禁和 HMAC 检查点恢复。第十二增量补齐 Runtime `ToolChoice` 并保留 qwen2.5 的真实失败基线；第十三至十九增量依次完成原子 Profile Set、ReAct 收束、加密 Review Bundle、v3 实质 Evidence、外部人工/Judge Signoff 契约和 Signoff 强制 WORM/Profile 资格链。第二十增量已用固定 `qwen3.7-plus-2026-05-26`、Profile Set v5 与 20 条 v3 Case 完成 Candidate/Stable 各 20 Case，自动质量与策略门禁通过且本地 HMAC/Bundle 复验成功。第二十一增量补齐 Live Eval 的独立签名授权、每次调用前预算预留和默认全拒绝的外部人工 Decision 模板；第二十二增量新增不读取 Credential 的离线 Live 资源计划，并要求授权完整覆盖固定 Profile 的调用与费用上界；第二十三增量提供显式初始化、Redis 原子消费、Reservation 幂等和状态丢失 marker 的多实例共享账本，默认 file 路径保持兼容。后续 Live 命令缺少匹配计划、绑定授权和已初始化持久账本会在 Provider 调用前失败。报告仍使用受控结构化证据沙箱，不代表生产搜索召回或公网 P95；尚缺独立外部人工 Signoff 和 Object Lock Version ID，生产开关不自动开启。并行角色、角色级恢复、写工具多角色执行和模板市场仍未完成，因此 P8.4 保持 `In progress`。

P8.5 第三增量（2026-08-03）新增 `agent.extension.v1` 租户目录：纯领域层负责有界过滤、确定性排序和稳定 Cursor；Service 仅适配 Capability Catalog、当前租户精确 Skill 版本和受治理 MCP Tool。Proto/Gateway/Web 只暴露无密钥目录投影，Skill 使用与 MCP 管理继续回到各自权威入口。目录由独立默认关闭开关控制，关闭不修改来源数据。

P8.5 第四增量（2026-08-03）新增独立 `agent.extension_marketplace.v1` 只读公共目录基础：发布者身份只保存公开 Ed25519 Key，规范 Manifest 与确定性 Release ID 固定包、版本、Artifact SHA-256、Capability 和声明权限；Mongo 不可变插入后，Service 仍逐条重验发布者、Key、ID 和签名。Proto/Gateway/Web 只返回无公钥、无原始签名、无 Artifact URL/字节和无安装授权的 `stable` 投影，独立开关默认关闭。发布者自助控制面、安装审批、依赖解析、扫描、Artifact 分发、版本撤回和真实 Mongo 多副本验收尚未完成。

第十五增量补齐人工复核前置材料：CLI 仅在无 Checkpoint 的 Live 单/多对照同时强制通过质量/策略门禁，并显式允许敏感内容时，短暂捕获两侧输入与最终正文；正文使用独立 Review Key 做 AES-256-GCM 加密，绑定最终签名报告 Payload SHA-256，并以新文件写入。打开时必须同时验签原报告、解密并逐 Case 复核正文哈希，明文不输出终端。该 Bundle 只是人工检查材料，不是人工签认或发布证据；既有 v7 报告无法从哈希补造 Bundle，后续付费重跑、人工签认和 WORM Version ID 仍未完成。

第十六增量完成全新受控复核运行：固定 qwen3.7、Profile Set v2 和 `agent-strategy-cases-v2` 的 Candidate/Stable 各 20 Case 已生成新签名报告与 AES-256-GCM Bundle，独立 HMAC 验签、报告摘要绑定、解密和逐 Case 正文哈希均通过。自动门禁显示 Candidate 20/20、Stable 18/20，语义增益 1000 bps、平均成本倍率 `1.0299x`、P95 倍率 `0.9315x`；总估算费用约 `0.374504 CNY`。但 40 份正文机器辅助审阅发现，沙箱证据仅为“输入 + 必需关键词”：Candidate 有 12/20 退化为证据不足声明并暴露评测占位元数据，其余内容也无法由证据验证；Stable 则普遍以模型常识补写，且 2/20 超长。故自动通过不等于内容资格通过，生产 Multi Feature Flag、WORM 晋级归档和“人工已通过”声明继续禁止；下一步先交付有实质证据、可验证 Claims/Citations、可用性与 groundedness 门禁的 v3 数据集，再申请付费重跑。

第十七增量完成 v3 离线内容资格契约：`AgentTaskCase.evidence` 为可选数据集字段，不进入签名报告，旧 `agent-task-eval-report/v2`、v2 数据集和历史 HMAC 验证保持兼容。新 `agent_strategy_cases_v3.json` 含 16 条实质结构化证据和 4 条空证据任务；每条充分证据显式绑定 Claim Terms 与 Evidence ID，并要求最终正文在声明附近保留精确 `[CitationID]`。门禁同时识别充分证据拒答、证据不足未声明、无依据固定声明和内部元数据泄漏。Runtime v5 把同一契约投影到既有 Platform/Web/Page Structured Content；多角色空结果使用明确 `no-evidence` 控制 handoff，不伪造外部来源。固定 qwen3.7 Profile Set v3 配置已快照化，20 条 Grounded Fake、投影、URL 白名单、Race 和目标 Vet 通过；本轮未访问 DashScope、LM Studio、MinIO、Mongo 或公网。确定性 Terms/Citation 邻近度只能建立质量下限，仍需版本化 Judge/外部人工签认和全新付费运行。

第十八增量完成版本化内容签认闭环：独立 Decision/Signoff/Rule v1 不修改报告 v2，逐 Case 绑定 Candidate/Stable 输出哈希及事实正确性、相关性、证据忠实度、写作质量结论，并绑定报告 Payload、数据集、执行配置和 AES Review Bundle 文件身份。CLI 创建/验签会重新验报告 HMAC、在内存中解密并逐 Case 核验 Bundle，再使用第三把独立 HMAC Key；签认不保存正文或自由文本。`external_human` 只记录假名和外部记录摘要，属于声明式身份而非独立身份认证；配置绑定 Judge 只能形成辅助信号。该增量当时只有离线 Fake/临时密钥测试，v3 付费运行已在第二十增量完成，真实外部人工签认仍未执行。

第十九增量把内容签认接入真实资格消费链：`agent-task-content-qualified-evidence/v1` 将签名报告与外部人工已批准 Signoff 作为同一 WORM 对象；CLI 归档前继续解密并逐 Case 核验 Bundle，Profile 服务只持有报告/Signoff 两把独立 HMAC Key。`AGENT_PROFILE_EVAL_CONTENT_SIGNOFF_REQUIRED` 默认关闭且依赖旧质量证据开关，开启后旧裸报告和 Judge Signoff 均 fail-closed；回滚只需关闭新增开关，不改 API、数据库或历史对象。当前只完成离线代码/配置验证，未产生新的 v3 付费报告、真实人工记录或 MinIO Version ID。

第二十增量完成 v3 云自动资格运行和失败诊断闭环：`--capture-failed-review-bundle` 可在显式授权下为自动门禁失败报告保存加密诊断正文，但 Signoff 领域继续拒绝任何未通过双门禁的报告。Profile v3、v4 两轮分别为 Candidate/Stable `12/20 vs 8/20`、`16/20 vs 15/20`，均未降低 90% 门槛；Profile v5 保持 Stable Prompt 正文/权限/预算不变，只升级 Multi 三角色的事实 Coverage Unit 和精确条款核对。最终 Candidate/Stable 为 `19/20 vs 15/20`，语义增益 `2000 bps`、成本倍率 `1.7911x`、P95 倍率 `1.9362x`，质量/策略门禁通过。报告 Payload `dc2b2500...971fb`、Bundle `1f8e5879...684b2` 和 40 份正文哈希已独立复验；三次完整报告累计估算 `0.814168 CNY`。唯一 Candidate 失败漏掉“读写权限”精确短语但保留 Citation。下一步仅允许独立外部人工逐 Case Decision/Signoff，再进入 WORM 与真实 Search/Page Read；Codex 自审不能冒充外部人工。

第二十一增量完成 Live 评测授权与外部人工交付护栏：`--create-live-authorization` 只读取数据集和 Runtime 配置并离线签发 `agent-task-live-authorization/v1`，不会构造 Provider 请求。授权绑定 Provider/Model、数据集版本/SHA、执行配置 SHA、签发/过期时间、最大 Run、Provider 调用、正文捕获和估算成本；Live 执行必须提供独立 HMAC Key、授权文件和持久 State Root。运行与每次模型调用都先追加签名预算事件，过期、篡改、序号缺口或超限在出网前 fail-closed，崩溃不返还预留。最终签名报告可选携带授权摘要和预算上限，旧离线/历史 v2 报告保持原签名形状。打开 Review Bundle 可同时输出绑定报告/Bundle 的外部人工 Decision 模板；模板不包含正文哈希、默认所有四维均失败且缺少人工身份/时间，不能被直接签认为通过。本轮仅做本地 HTTP/Fake 与文件系统验证，没有连接 DashScope、LM Studio、MinIO、Mongo、MCP 或公网。账本仍是受控工作区内的本地状态，不是跨主机中央配额服务。

第二十二增量完成 Live 评测离线额度计划与模型迁移护栏：`--plan-live-evaluation` 生成 `agent-task-live-plan/v1`，只读取固定数据集和规范化 Runtime 配置，不读取 API Key 或构造 Provider Client。计划按 Case/模板汇总工具闭环最少调用、各 Profile `MaxSteps` 调用上界、`MaxTotalTokens`、`MaxEstimatedCostMicros`、共享预检和可选正文数量；授权签发必须覆盖完整调用/费用上界，正文为零则禁止捕获。固定 `qwen3.7-plus-2026-05-26`、v3 20 Case 和 Profile Set v5 的结果为 `121..241` 次调用、`1,240,482` Token、`4,701,348` 微计价单位和 40 份可选正文。模型按 Provider、精确 Model 和配置 SHA 固定，改用滚动别名必须新建计划、授权和资格报告。CLI 普通/Race、完整 Agent 模块串行回归和受影响 Vet 通过；该增量未调用 DashScope 或其他外部服务。

第二十三增量完成 Live Eval 的可选 Redis 共享授权账本：默认 file 后端及 HMAC 文件链不变，Redis 依赖只位于 CLI 运维边界。`--initialize-live-authorization-state` 只验签授权并创建中央状态，不读取模型 Credential；运行阶段从不隐式初始化。Lua 使用 Redis 服务端时间，在同一事务校验授权身份/有效期/四类上限并累计 Hash、写 Stream 与 Reservation 去重摘要，不同连接共享额度且网络重试不重复扣减。无 TTL marker 在 State/Stream 被提前删除时禁止消费和重新初始化；Redis 报告可选记录后端与 Namespace SHA，旧报告保持可验。生产部署仍须把 Redis ACL/TLS/AOF/备份/`noeviction` 和管理员权限纳入信任边界。本增量只使用 `miniredis` 验证，没有连接用户 Redis 或模型 Provider；它不替代外部人工 Signoff、WORM 和真实搜索验收。

## 2. 兼容产品模式与 P8 目标

Chat、Consult、Assist、Multi、Workflow 是当前兼容入口，不再作为长期产品信息架构。P8 目标是统一连续对话入口，由 Capability Planner 在用户可用 Catalog、Policy、Budget 与连接范围内选择能力；Workflow 保持确定性自动化，Multi-Agent 转为复杂任务的内部策略。

| Mode | Service 入口 | Runtime v2 状态 |
|------|--------------|-----------------|
| Chat | `RunAgent` / `CallApiOfAi` | `RunAgent` 的 `conversation.reply` 固定使用无工具 `runtime.chat`；旧 `/chat` 由 rollout 在 Runtime v2 与 Legacy 间切换 |
| Consult | `ConsultContent` | 可通过 rollout 使用 Runtime v2 |
| Assist | `AssistPublishTwitter` | 可通过 rollout 使用 Runtime v2；仅生成草稿，发布需携带用户所属已完成 Assist Run 显式确认；成功发布可旁路归因 `draft_published=true` |
| Multi | `MultiAgentPublishTwitter` | 业务编排 + 版本化 Profile，未完全迁移通用 Runtime |
| Workflow | `ExecuteWorkflowStrategy` / Workflow Service | 策略节点可进 Runtime；DAG Scheduler 仍是本地 Engine |

Rollout 环境变量：`AGENT_RUNTIME_V2_MODES=chat,consult,assist,multi,workflow`；空值保留 Legacy。

## 3. 包职责与依赖

```text
service -> runtime <- model adapters
        -> message -> runtime.Message
        -> profile -> runtime Budget/Tool filter
        -> strategy -> versioned admission plan
        -> multirole -> runtime Runner/Profile/Strategy contracts
        -> repository (Mongo)
        -> workflow/* and mcp/*
```

- `runtime`：Runner、Action、Observation、Step、Budget、TokenUsage、错误模型。
- `message`：System/Developer/Policy/Current/History/Tool/Persona/Memory/RAG/Blackboard 装配。
- `model`：OpenAI-compatible Adapter、Catalog 基础、Endpoint Policy。
- `profile`：不可变 Agent/Prompt Profile Catalog、AtomicResolver、Release 校验与确定性用户分桶；不保存运行状态、用户数据或可变草稿。
- `strategy`：无 I/O 的 Multi-Agent 准入、固定模板和脱敏计划证据。
- `multirole`：无 Service/存储/MCP SDK 依赖的顺序角色聚合；生产 Service 与 Eval 复用同一角色隔离、交接、共享父预算和失败语义。
- `project`：Agent 项目、成员角色、权限 Port 与 User Directory Adapter；Mongo 实现在 `repository`，MCP 只依赖窄 `AccessResolver`。
- `service`：选择模式/Profile/Model、持久化对话、调用 Runner。

禁止让 `runtime` 依赖 Service、Mongo、Redis、MCP SDK 或具体 Provider。

## 4. Runtime 不变量

- ToolCall/ToolResult 必须成对保留；孤立工具结果丢弃。
- System/Developer/Policy 和当前输入是强制上下文。
- History/Tool Result 可按 Token 压缩；RAG Chunk 整块入桶，放不下则跳过。
- 调模型前预留输出 Token；超过单次输入、输出或 Run 总预算返回结构化 `budget_exceeded`。
- Provider Usage 优先；缺失时保守估算并标记 `Estimated=true`。
- Workflow 的节点尝试、并发、总 Token、总超时与成本使用共享 `BudgetTracker`；并发模型调用必须先预留，实际已消费额度在失败/挂起恢复后不得清零。
- Workflow-as-Tool 必须由用户显式发布并绑定不可变 Revision/DSL Hash；草稿更新不能隐式漂移。发布继续拒绝补偿、Agent/递归节点、外部回调和未知工具；只读工具不得审批，写工具必须审批且声明幂等，风险工具必须审批，审批型 DAG 还要求可恢复 Agent Run、统一审批恢复、Run Store 与 Checkpoint Cipher 全部可用。运行时只装配当前用户 Active 发布记录，Input Schema 不允许 `$ref` 或平台身份字段；动态调用复用统一 ToolExecutor，并记录父 Agent Run/Action。父子准入预算仍独立，父 Tool Timeout 同时约束子 Workflow Run；`execution.accounting.v1` 只把各自权威快照按直接子级聚合展示，不得把观测总计误当共享硬预算或递归账本。
- Skill Catalog 只从当前用户 Active Workflow-as-Tool 发布记录只读投影，不另存可漂移副本。Skill Version 必须覆盖发布/Workflow Revision、DSL Hash、Profile/Prompt、Budget、Instructions、单一 Tool 与 Output Schema；调用必须显式指定精确 ID/Version，不允许 `latest`、关键词自动选择或隐式回退。规划前、Runtime 前和真实 Tool 调用前都要重校验当前绑定；Feature Flag 关闭后目录与执行 fail-closed，但不得改写发布元数据。
- 任务模板与 Workflow/Skill 分层：`agent_task_templates` 只保存用户主动编写的单输入指令、不可变源 Run 证据和固定能力路由，不读取源会话正文，不保存凭据。创建要求 completed 权威 Run 和精确 Revision，内容创建幂等、归档 CAS；执行重校验源 Run/路由并写入新 Run 的模板 ID/Revision。模板开关默认关闭且依赖 Recoverable Runs；关闭执行保留列表与归档。
- 写工具默认 fail-closed；只读工具白名单由 Profile/Tool Catalog 控制。Workflow 始终可按当前策略逐次审批外部 MCP `risky/write`。Unified Agent 只有在 `AGENT_RECOVERABLE_RUNS_ENABLED`、`AGENT_EXTERNAL_MCP_ENABLED` 与 `AGENT_UNIFIED_APPROVAL_RECOVERY_ENABLED` 同时启用时，才从当前用户治理交集中装配这两类工具；关闭任一开关立即回退只读目录。`risky` 最多一次远端尝试；`write` 还必须具备审核过的声明式幂等键契约。
- Unified Agent 权威状态位于独立 `agent_execution_runs`，不能以 `agent_run_traces` 或 `agent_workflow_runs` 代替。启用可恢复 Run 后，模型调用前必须先创建 `running` 记录，终态/挂起态使用 Revision CAS 提交；创建或提交失败均 fail-closed。同一 Run ID 贯穿 Runtime、Trace、对话元数据和状态集合。`ask_human` 与待审批 `tool_call` 使用 `react.v1` 限长加密 Checkpoint 和 `status + revision + lease + attempt_id` Claim；Checkpoint 不保存 Tool Definition。审批恢复还精确绑定 Action、审批、输入摘要与稳定幂等键，签发/恢复/真实调用前重新解析当前 Profile/Prompt、Connection、Credential、Snapshot、Schema 和 Policy。一次性令牌只保存哈希并轮换；恢复工具动作不重放模型或此前成功 Step。
- 公网 `web_search/page_read` 必须由服务端执行上下文注入用户与 Run 身份，经 Redis 原子准入后调用；网页正文始终作为不可信证据，Citation 只从受信任版本化 Structured Content 投影。
- Profile Release 与 Runtime/Provider Rollout 相互独立；同一 `profile_id + salt + user_id` 必须稳定命中，同一 Run 使用选版副本且不得中途漂移。Multi-Agent 研究父 Profile 是 Profile Set 唯一 Release Anchor，角色 Profile 禁止独立灰度；计划摘要固定 `profile_set_anchor/version`，执行按该精确版本从一个 Catalog 快照解析全部成员，缺失或混合版本在模型调用前 fail-closed。默认 Release 固定 v1，非法发布配置在启动阶段 fail-closed。
- Assist 确认发布必须先校验来源 Run 的租户、模式和完成状态，再用来源 Run 与最终正文摘要构造稳定 TweetService 幂等键；只有真实发布成功可记录 `draft_published=true`，不得用超时或未操作推断负样本。
- 所有 timeout/cancel 必须沿 `context.Context` 传播，后台 goroutine 必须可退出。

## 5. Model 与 Credential

- `model_kind_id` 在 gRPC 边界解析为 Chat Model ID；未知模型返回 `InvalidArgument`。
- Chat 模型选择器不得包含 Embedding Model。
- Model Catalog 已接管 Runtime v2 Provider Client 路由；Legacy 继续作为 Feature Flag 回滚路径。
- Workflow LLM 节点优先使用加密的用户级 `provider_config_id`；`credential_ref` 仅保留环境变量兼容映射。
- DSL、运行输入、查询输出均不得出现明文 `api_key`。
- 自定义 Base URL 需经过 Endpoint Policy 和受限 HTTP Client；本地地址仅对本地 Provider/Allowlist 放行。
- Eval 的 `reasoning_mode` 是受限 Provider 能力映射，不是任意请求扩展：当前只有 DashScope 可将 `disabled/enabled` 映射为 Chat Completion 的 `enable_thinking`，冲突或未映射 Provider fail-closed；生产普通请求保持 Provider 默认值。

## 6. Session Memory

- 所有模式在 user/assistant 消息对持久化后进入统一 Summary Scheduler。
- 12 条未结晶消息触发增量摘要；空闲 2 分钟视为短 Session 边界。
- Mongo 租约声明 `[from, through)` 消息区间；成功推进版本/游标，失败释放。
- Qdrant Point ID 由 Dialogue + Summary Version 稳定生成，重试覆盖同一点。
- 删除 Dialogue 或关闭服务会取消 Timer 与在途 LLM/Embedding Job。
- 新摘要写入共享 `agent_episodic_memory` Collection，Payload 必须带字符串 `user_id`、`embedding_model`、`embedding_dimension`、`embedding_version`；检索必须使用 Qdrant 服务端 `user_id` Filter。
- `MemoryManager` 默认保留旧 `episodic_user_<id>` 的有界双读兼容路径；迁移使用 `cmd/agent-memory-migrate --user-ids ...`，禁止在在线请求中枚举或删除旧集合。
- Qdrant 共享集合迁移不重新生成 Embedding，使用 Scroll + 原始 point ID Upsert；删除旧集合只能在显式迁移成功后执行。
- `cmd/agent-memory-migrate --verify-only --user-ids ...` 提供只读迁移验收：按用户比较旧集合有效 Point ID 与共享集合服务端 Filter 结果，并检查 `user_id` 与 `shared_user_payload_v1`；可用 `--report` 输出 JSON，失败时不执行删除。
- RAG 评测位于 `internal/module/agent/eval/`：纯函数、Runner、稳定 RRF 和 JSON 报告不依赖具体存储；`cmd/agent-rag-eval` 是必须显式 `--allow-live` 的只读比较入口，`--strategies` 按依赖闭包懒初始化 Provider，出站连接经过 Endpoint Policy/受限 HTTP Client，当前 smoke dataset 为 51 条，并记录 Provider 请求失败率。
- `cmd/agent-task-eval` 默认消费固定任务和录制执行结果；通用集为 52 条，单/多策略集为绑定两个只读研究草拟模板的 20 条。v2 只验证关键词/长度；独立 v3 集增加不进入报告的实质 Evidence Contract、Claim/Citation 邻近度、证据不足合法分支和元数据/无依据短语门禁。`--allow-live --runtime-config` 固定单 Agent Provider/Model/Profile；`--allow-live --strategy-runtime-config` 在同一规范化 Provider/Model/Pricing、四角色 Profile Snapshot 和配置哈希内自动执行真实模型 Multi 候选与 Single 稳定侧。Live 首先用真实模型完成无副作用 Chat/Tool Call 预检，执行错误立即停止；`--checkpoint-dir` 按 Candidate/Stable 保存逐 Case HMAC 签名哈希链，只含脱敏评分证据，重跑时从同身份连续前缀恢复。两种模式均走 Endpoint Policy、Provider Router、Profile Catalog/Runtime，并只使用无副作用沙箱工具；策略沙箱能验证模型决策、角色交接与预算，不代表生产搜索召回或公网延迟。策略门禁额外要求两侧 `execution_config_sha256` 完全一致。报告不保存正文/Base URL/Credential/Evidence，绑定数据集/配置 SHA-256 并可 HMAC 签名验签；需要人工查看时只能在无 Checkpoint 的全新 Live 对照中显式启用独立 AES-256-GCM Review Bundle。默认只为双门禁通过结果写 Bundle；显式 `--capture-failed-review-bundle` 可为失败报告写诊断密文，但该 Bundle 不能创建 Signoff 或资格对象。Bundle 打开时先验签报告再逐 Case 对正文哈希，只是复核材料；独立 Decision/Signoff v1 再绑定报告、Bundle、规则、Reviewer 类型和逐 Case 四维结论，Judge 不得冒充外部人工批准。可选归档要求私有 MinIO Versioning + Object Lock COMPLIANCE 并按精确 Version 回读验签。`AGENT_PROFILE_EVAL_EVIDENCE_REQUIRED=true` 时发布管理路径复验回执，运行热路径不访问 MinIO。
- Cascade Router 通过 `RouteWithMetadata` 暴露 Lexical/Semantic/LLM Fallback/Default 决策阶段；词典使用固定优先级，避免 Go map 遍历导致同一查询漂移。`cmd/agent-router-eval` 使用 34 条离线数据集记录意图准确率和 L1/L2/L3 错投率，默认不连接模型；Semantic/LLM Fallback 的真实对照必须固定 Provider/模型版本后显式执行。
- Cascade Router 的 Embedding/Chat 依赖已拆为窄接口并支持显式 Semantic 阈值；决策元数据保留 Semantic/LLM 降级错误。`agent-router-eval` 的 semantic/llm/full 模式必须使用 `--allow-live`，API Key 仅从环境读取，并记录 Provider、模型、请求失败、Token、估算成本和 Pricing Version。
- `POST /api/v1/agent/dialogues/:id/end` 是显式 Session End：保留对话，取消并等待旧摘要 Job 释放租约，再同步强制结晶；Mongo 租约/游标保证并发请求至多提交一个新版本。

## 7. Workflow 边界

- 前端 Vue Flow 生成 JSON DSL；后端 `workflow/dsl` 定义版本契约，`workflow/ir` 编译确定性计划，`workflow/engine` 调度。
- 缺失 `dsl_version` 的旧定义按 v1 编译；新保存定义补齐 v1 和 workflow version，未知 DSL 版本拒绝执行。
- 本地 Scheduler 以确定性并行波次执行；节点只读取同一 `StateView` 代际并返回 Delta，协调器按 DSL 声明顺序单写合并，不能由节点直接修改 Blackboard。
- Blackboard 已有 copy-on-write 代际；Scheduler 通过不可变 `StateCommit` 回调按事件阈值持久化 StateEvent/Snapshot，并以专用原子接口只推进 Run 的 `state_version + revision`，禁止用旧 Run 对象覆盖运行控制字段。完成/挂起边界强制保存最终快照；Resume 从最近 Snapshot 重放后续事件并核对哈希、连续序号、Checkpoint 版本与状态摘要。
- v1 的 `writes.path/source` 由协调器映射到全局两段状态路径；并发同路径写入必须声明一致的 `append/sum/min/max/merge/first/last` Reducer，由协调器按 IR 声明顺序归约。无 Reducer 或策略不一致时编译失败；节点 ID 命名空间内的普通输出仍按声明顺序确定性落地。
- 节点失败、挂起或取消时，本地 Scheduler 会等待本波次已启动 Goroutine 退出后再返回。
- 节点 Retry 使用确定性退避；只有显式 `IsRetryable()` 或临时网络错误可重试。普通业务错误、挂起、取消与 Deadline 不重试，节点 Trace 保存当前/最大尝试次数。
- Tool 节点可声明顶层 `compensation`；Engine 仅为已成功节点生成反向拓扑计划。主 Run 失败后必须先持久化 Compensation Journal，再按序领取执行，补偿不得依赖重放主 DAG。
- 补偿工具必须通过统一 ToolExecutor，继续受 Policy/Approval/Breaker/Timeout 和稳定独立幂等键约束。审批挂起通过一次性 Resume Token 恢复；`compensation_failed` 可显式重试，已成功补偿不会重跑。
- 后台 Reconciler 只扫描每个 Run 严格首个未完成且租约过期的 `executing` 补偿；普通工具复用原子 Claim/Attempt/幂等自动恢复，审批型或目录缺失工具转 `compensation_failed` 并保留显式重试。不得描述成所有补偿都可绕过人工无人值守执行。
- 独立 Compensation Journal API 只返回脱敏运行摘要、状态、哈希、租约和错误；人工 Retry 仅推进严格首个 `planned`、`failed` 或过期 `executing`，有效租约与 `suspended` 不可抢占，且绝不重放主 DAG。
- 公开 Replay 只读装配 Run/Revision/Event/Snapshot/Compensation 证据并校验哈希、连续序号和最终版本；最多 10,000 个事件，不调用 Scheduler、LLM 或 Tool，也不返回补偿原始输入/输出。
- Run 列表只返回租户隔离轻量摘要；业务详情保留 `output_json` 兼容字段，独立 `GET /workflow-runs/:id/traces` 先校验 Run 所有权，再读取 Run/Step/LLM/Tool 记录。`GET /workflow-runs/:id/events` 通过 Redis Stream 投递同一批脱敏 DTO，使用服务端有序游标、长度/TTL 边界、心跳和连接窗口支持水平扩容与断线恢复；Redis 失败不改变工作流业务结果，Mongo Trace 始终是完整查询事实源。Prompt、Completion 默认只保存摘要、长度、Usage、模板身份与错误分类；可选预览采样独立于 OTel 采样，默认关闭、确定性有界并拒绝疑似密钥/凭证/直接身份标识。样本只进入租户隔离 Mongo Trace，禁止进入 OTel、Prometheus 或日志。持久取消只允许 `running -> canceling -> canceled`，执行实例轮询状态并取消 Context，终态用 `status + revision` 提交；周期状态游标不得改写 `status/cancel_*`，不得退化为进程内 `cancelFunc` 注册表。
- Blackboard 查询通过目标版本之前最近的完整快照和 `(after, target]` 有界事件范围重建，校验快照/事件哈希、连续序号与最终版本。路径页按字典序稳定排序，游标绑定版本和过滤条件；敏感键递归脱敏，单值预览、页大小、查询长度、字段数和事件数均有硬上限。该读取路径不调用 Scheduler、模型或工具。
- DSL 顶层 `budget` 由 IR 校验并在 Scheduler 执行：节点 Retry 计入最大尝试数，跳过分支不计数，并发处理器受信号量限制；直接 LLM、Runtime ReAct/Plan-Execute 与 Legacy 回退共享 Token/成本账本。成本上限为 0 时关闭金额拦截，启用后未知定价 fail-closed。
- 重复实现用户 DAG 语义的 `workflow/temporal` 实验 bridge 已删除；Temporal 只承载独立的风控和热点后台 Workflow，不是用户 DAG 执行后端。
- Agent Service 顶层生命周期由 `internal/module/agent/startup` 的纯配置计划控制：`api/all` 启动 gRPC、Consul、内部 MCP 和 API 本地异步记录器，`worker/all` 启动 MQ Consumer、治理巡检和 Temporal Worker；Profile Catalog 跨实例同步在所有角色保持运行。热点播报的唯一生产 owner 是 Temporal，显式 `disabled` 可回滚，Temporal 不可用时停播且不自动回退到本地 Ticker。
- LLM Chat 与 LLM Writer 是不同组件；默认 Chat 不能隐式发布推文。
- PublishTweet 是写工具，最终目标必须经过统一 Policy/Human Approval/Idempotency。
- Workflow、Runtime MCP 与 Legacy 模型驱动工具调用共享实例化 Registry/Executor；未知工具按 risky/fail-closed 处理。
- Workflow Run 支持 Checkpoint/WaitingNode；审批挂起使用服务端随机 Resume Token，Mongo 只保存哈希，批准后通过条件更新单次领取并重试当前 Tool 节点。
- Workflow 外部 MCP 动态节点只识别 `server_id.tool_name` 命名空间；保存 DSL 时不读取远端连接，执行与审批恢复时必须重新读取租户 Connection、Active Snapshot、Schema 和 Policy。`risky` 调用固定单次尝试并禁止节点自动重试。`write` 必须同时具备 `idempotentHint=true`、`_meta["io.twitter-clone/idempotency-key-argument"]` 和 Input Schema 必填字符串参数；平台覆盖注入域隔离稳定键，同一逻辑执行只允许有限同键重试。外部补偿仍拒绝。
- Workflow-as-Tool 的发布元数据存入独立 `agent_workflow_tool_publications` 集合，不扩张通用 Agent Repository 接口；唯一键为 `(user_id, workflow_id)` 与 `(user_id, tool_name)`。调用和两类恢复都必须重新验证 Active 发布、绑定 Revision、DSL Hash、父子谱系与可发布性，不能把发布时校验当作永久授权。父 Agent 的 Tool Continuation 进入独立密钥加密 Checkpoint；人工输入保存子恢复凭证，委托审批只保存子审批引用。子 Workflow 独占审批事实与一次性 Grant，父 Agent 不签发第二枚令牌；普通 Run/API 不回显任何恢复凭证。
- 审批与工具执行结果分别持久化到 Mongo；参数只保存脱敏副本。Agent 生成的稳定执行键经受信任 Context 透传到 TweetService，Tweet/Poll/Outbox/幂等绑定同事务提交，关闭远端成功/Agent 结果落库前崩溃造成的重复发推窗口。
- Web 审批收件箱支持状态筛选、挂起 Run 详情、批准/拒绝/恢复；直接 Runtime 审批签发父 Run 授权，子 Workflow 审批签发子 Run 授权后调用父 Agent Resume，由父 Runtime 恢复同一子 Tool Action。两条路径都不持久化明文授权，跨浏览器/跨设备可继续同一 Run。
- ToolExecutor 使用按工具隔离的熔断器；持久结果回放不受熔断影响，调用方主动取消不计为下游故障。
- 所有 Tool Result 先经过统一 JSON 体积硬上限；超过内联阈值时只允许写入独立私有对象桶并在 Mongo/Trace 保存哈希引用。对象上传成功但幂等提交失败时必须回收对象；关闭对象存储开关后大结果 fail-closed，禁止退回 Mongo 大文档。
- Tool 指标只允许工具、类别、来源、决策、错误码、熔断状态和固定对账动作等低基数标签；禁止 user/run/input/error text 标签。
- 治理对账周期回收审批和执行租约；审批过期会终止对应挂起 Run 并清除恢复凭证，子 Workflow 委托审批还必须同步终止绑定的父 Agent Run，计数按两类 Run 累加而不是覆盖。
- 内置 MCP 只绑定回环地址；HTTP Bearer 与 Tool Middleware 双重认证，令牌缺省由进程安全随机生成。
- 外部 MCP 只允许 `streamable_http`/`sse`，由独立 Feature Flag 和 Egress Policy 约束；连接可属于个人或 Agent 项目，旧缺少作用域的记录按个人兼容。项目 Owner/Editor 可治理，Viewer 可使用；成员由 User Service 精确校验，列表、按 ID/Server 解析、Workflow/Unified Agent 目录及真实调用均重查当前成员关系，撤权立即生效。用户凭据 Bearer 加密保存并只在调用边界解密；项目 Bearer 连接还可选择部署托管引用，Registry 精确绑定项目/Endpoint/Auth，Token 位于专用只读目录并在 Discovery、健康和调用时解析。Connection 只保存引用/版本，共享成员不能读取用户或托管凭据。Discovery 生成待审核不可变 Schema Snapshot；Active Snapshot 中明确声明且用户启用的只读工具可进入统一 Agent 动态 Catalog。`risky` 和契约完整的 `write` 可进入 Workflow 动态节点；在审批恢复灰度同时启用时也可进入 Unified Agent 的当前治理目录。两条路径复用同一持久 Approval、Checkpoint、一次性 Resume Grant 和 ToolExecutor，并在授权签发、批准恢复和真实调用前重新校验策略。风险调用不自动重试；写调用仅能使用平台覆盖注入的稳定键进行有限重试。外部 MCP 补偿仍 fail-closed，第三方声明不得表述为严格 exactly-once。
- 外部 MCP SDK Adapter 使用部署可关闭的有界 Session Pool；池身份绑定 Connection/Transport/Endpoint/单向 Credential Identity，托管身份额外包含 Registry Version 与当前 Secret 摘要。单 Session 单租约使用，容量、单连接并发、等待和空闲 TTL 均有界。同版本 Secret 文件轮换后新调用不复用旧身份；Registry Version 漂移则 fail-closed，需 CAS 重新保存连接并重审；连接更新或撤销会失效该 Connection 的全部身份。主动健康巡检通过 Mongo 独立租约跨实例领取连接，使用 `ping`、批次、并发上限、超时、抖动和退避；健康字段不递增用户 Revision，也不修改 Discovery、Snapshot、Policy 或执行权限。
- 外部 MCP 生产验收位于 `mcp/acceptance` 与 `cmd/agent-mcp-*`，不进入 Agent Service 热路径。网络调用必须显式 `--allow-live`，写探针额外要求 `--allow-write`；配置只引用环境变量/文件凭据，报告只保存摘要与固定错误码并可 HMAC 签名。回环 Conformance Server 仅证明验收器和协议夹具；真实第三方、旧 Token 撤销、Projected Secret、多副本和代理故障必须单独执行并保留签名证据。

## 8. 当前优先级

1. **P8 产品收口**：停止新增 Agent 执行模型、节点、Provider、角色和 Marketplace 安装能力。优先把统一入口从关键词式迁移路由收口为真实可理解的单 Agent 体验，在权限过滤后的低风险工具集合内完成联网、MCP 和 Workflow-as-Tool 三条用户价值链；显式 Skill、Workflow 与写操作继续精确选择和审批。
2. **真实验收与质量证据**：固定一个主力 Chat 模型、Brave、一个真实第三方 MCP 和现有 Workflow 版本，保存成功以及超时、撤权、预算终止、审批恢复证据。外部人工 Signoff 与 MinIO Object Lock 只服务于已有多角色模板资格，不触发新的并行或角色级恢复开发。
3. **交付与体验**：先拆分当前大规模未提交工作区，形成可审查、可回滚提交；隐藏未启用功能、收拢高级入口，补十分钟演示、失败演示、最小启动手册和产品指标看板。Agent Core、Workflow/RAG、Eval 与 P8 产品边界不得继续混成单一提交。
4. **延期项**：P6 真实 Qdrant 回填、P7 真实 DLQ/对象锁、生产 API/Worker 物理拆分继续保留为受控环境或部署增强；公共市场 Artifact/扫描/依赖/安装、并行 Multi-Agent、更多 Workflow 节点和代码沙箱只有真实需求证据后才重新立项。

具体停止线与四道完成门禁见 `docs/agent/UNIFIED_AGENT_PRODUCT_PLAN.md` 第 10 节。四道门禁完成后 P8 进入 Maintenance，不自动开启下一阶段。

## 9. 验证矩阵

```powershell
$env:GOCACHE='E:\GOProject\cloud\twitter-clone\tmp\go-build-cache'
go test ./... -count=1
go test -race ./internal/module/agent/multirole ./internal/module/agent/eval ./internal/module/agent/workflow/rag ./pkg/qdrant ./internal/module/agent/service ./internal/module/agent/grpc ./internal/gateway/handler ./internal/gateway/router -count=1
go vet ./...
go run ./cmd/agent-router-eval --out tmp/router-eval/baseline.json
go run ./cmd/agent-task-eval --stable-results internal/module/agent/eval/testdata/agent_task_recorded_results.json --enforce-gate --out tmp/agent-task-eval/report.json
go run ./cmd/agent-task-eval --dataset internal/module/agent/eval/testdata/agent_strategy_cases.json --dataset-version agent-strategy-cases-v2 --results internal/module/agent/eval/testdata/agent_strategy_multi_results.json --stable-results internal/module/agent/eval/testdata/agent_strategy_single_results.json --min-cases 20 --enforce-gate --strategy-gate --enforce-strategy-gate --out tmp/agent-task-eval/strategy-comparison.json
go run ./cmd/agent-task-eval --allow-live --runtime-config internal/module/agent/eval/testdata/agent_task_runtime_config.example.json --out tmp/agent-task-eval/live-baseline.json
go run ./cmd/agent-task-eval --dataset internal/module/agent/eval/testdata/agent_strategy_cases_v3.json --dataset-version agent-strategy-cases-v3 --allow-live --strategy-runtime-config internal/module/agent/eval/testdata/agent_strategy_runtime_config.qwen37-v5.example.json --preflight-timeout 45s --case-timeout 90s --timeout 20m --min-cases 20 --enforce-gate --progress --strategy-gate --enforce-strategy-gate --review-bundle tmp/agent-task-eval/v5.review.enc.json --capture-failed-review-bundle --allow-review-content --out tmp/agent-task-eval/v5.report.json
go run ./cmd/agent-task-eval --archive-report tmp/agent-task-eval/live-baseline.json --archive-config internal/module/agent/eval/testdata/agent_task_archive_config.example.json --archive-receipt tmp/agent-task-eval/live-baseline.archive-receipt.json
go test ./internal/module/agent/mcp/acceptance ./cmd/agent-mcp-acceptance ./cmd/agent-mcp-conformance -count=1
# 本地协议验收需分别启动 agent-mcp-conformance，再显式运行 agent-mcp-acceptance；真实环境步骤见 docs/agent_mcp_acceptance.md。
```

涉及 Workflow Editor 时额外执行 `web/npm run build`。共享 Runtime/Repository/调度器变更必须有离线 Fake 测试，不连接真实模型、MCP、Mongo 或 Qdrant。

## 10. Goal Core Refocus Update（2026-08-10）

- G3 已完成：短计划、确定性 Admission、严格模型 Adapter、一次 Admission/执行/验证恢复、opt-in VerifiedRunner 计划消费和低敏 `TaskOutcome` 均具备离线证据。默认 Service Planner 只解析显式能力，无选择时回到对话；G5 已删除无生产调用者的关键词 Router 及其兼容构造器。E2E-02/11/18 固定夹具验证澄清挂起、研究草拟 Artifact 和单次失败恢复。生产 Goal execution 仍默认关闭。
- G4 E2E-05 已完成离线迁移契约：平台搜索 Legacy Runner 仍只调用一次；Goal shadow 复用同一结构化结果，记录 `observed_execution` TaskOutcome 与 Tweet Evidence Reference，文本型伪证据返回 blocked。两个生产开关继续默认关闭，未替换 Legacy 响应或持久化路径。
- G4 E2E-06 已完成离线迁移契约：上一轮结构化 Tweet Citation 以可信引用持久化；追问只允许选择同一对话内引用，并把工具收窄为 `get_tweets_by_ids`。Action 参数、`platform.tweet_detail.v1` Observation、Legacy Citation 与 Goal prior/detail Evidence 必须指向同一 ID；歧义、伪造、多 ID 和文本伪证据均 fail-closed，执行仍只有一次。
- G4 E2E-07 已完成离线迁移契约：`web_search` 必须先返回带 Provider/Query 的 `web.search.v1` 公网来源，后续 `page_read` Action 与 `web.page.v1` 必须绑定其中同一规范化 URL 和非空支持类型正文。Goal shadow 复用 Legacy 单次执行并输出低敏 Search/Page Evidence；仅搜索、跨来源页面、伪造 Ledger 与文本伪证据均 blocked。独立 Web shadow 与全局开关继续默认关闭，受控真实 Provider 集成仍待执行。

- G4 E2E-08 已完成离线迁移契约：空搜索、Provider/Page 错误、私网或畸形引用统一生成带稳定原因码的 blocked TaskOutcome；诊断 Evidence 只保存固定原因码摘要和结构元数据。Legacy Web 搜索/草拟完成护栏要求至少一个结构化公网引用后才持久化回答，禁止无证据的“稍后返回”，但普通搜索不强制 `page_read`，也不重复任何执行。
- G4 E2E-09 已完成离线迁移契约：站内/Web 草拟复用同一 Legacy `RunResult`，来源 Evidence 只接受可信结构化 Tweet 或公网 URL；草稿 Artifact 必须包含与邻近主张绑定的精确 `[/tweets/{id}]` 或 `[public URL]` 标记，Verifier 将 Artifact 与对应来源 Evidence 交叉绑定。缺失、伪造、跨来源和脱离正文的标记均 blocked；独立 Shadow 与全局开关默认关闭，不重复模型/工具、不改变响应。现有不可变 Profile 未原地改写，真实引用格式符合率和后续 Profile 晋级仍需受控集成。
- G4 E2E-10 已完成纯离线契约：`RewriteConstraintSpec` 通过规范化 Task 同时绑定语言、输出结构和 Unicode 字符上下限；Collector 只保留约束与正文摘要，Verifier 使用 70% 主导文字脚本、严格 JSON/Markdown 列表/纯文本结构和字符范围确定性判断 `content.rewrite` Artifact。错配、越界、空正文、Task 漂移、工具权限和伪造摘要均 fail-closed。该任务没有新增 Service Shadow、API 字段、Profile 或生产开关，显式结构化请求契约形成前不得通过关键词解析接入生产。
- G4 E2E-11 已完成受控离线迁移契约：G3 计划夹具证明 research→respond 准入顺序，独立 Research Draft Shadow 则在站内/Web 单次 Legacy `RunResult` 上要求可信研究 Observation 早于匹配正文的终止 `final_answer` Action，并复用 E2E-09 Citation/Artifact 真值。顺序倒置、缺少研究、缺少终止动作和伪造顺序 Evidence 均 blocked；全局与专用开关默认关闭，不修改 Profile 或生产响应。
- G4 E2E-12 已完成纯离线契约：可信只读 Tool 必须返回版本化 `claim_id/value/reference`，Collector 只保存摘要与公网引用；同一 Claim 至少两个不同 canonical value 且引用不同才构成冲突。领域 Verifier 通过可选 `SuspendedRunVerifier` 在 `ask_human` 挂起时运行，只有精确配置的问题位于全部冲突证据之后才生成已验证 checkpoint/TaskOutcome；同值、静默 FinalAnswer、提前或无关问题、未配对 Observation 和伪造 Ledger 均 fail-closed。未新增 Service Shadow、API、Profile、Provider 或生产开关。
- `TweetWriteEnvironment` 通过只读 Timeline Adapter 捕获作者推文引用的前后状态，不保存正文、用户 ID 或 Credential。
- `create_tweet` 返回 `platform.tweet_publish.v1` Structured Content；ToolExecutor 幂等回放会恢复该结构，不再次执行写工具。
- `TweetPublishGoalVerifier` 只接受成对 Runtime Action/Observation，并把结构化 `tweet_id`、After Snapshot 与 Evidence Ledger 交叉验证；新发布必须仅新增目标引用，幂等回放必须保持状态集合不变。
- E2E-13/14 已完成受控进程内集成：ReAct 审批前零写入，checkpoint 恢复后真实 `create_tweet` MCP Handler 只执行一次，Timeline After 证明目标新增；同键完整重跑恢复 Structured Content 且状态不变。该夹具使用内存 Approval/Idempotency Store 与 TweetService Fake，部署态 Mongo/TweetService 验收仍未完成，不得描述为生产闭环。
- E2E-15/16 已完成受控进程内集成：External MCP Snapshot 绑定 Actor Digest、Connection Revision 与 Binding Digest；只读任务由 Collector/Verifier 复算成功 Observation，跨租户目录为空；写任务审批前远程调用为零，审批后撤权使 Resume 在当前目录校验阶段 blocked，远程调用仍为零。真实外部 MCP、Mongo 持久授权与进程重启恢复仍未验收。
- E2E-17 已完成受控进程内集成：Workflow Snapshot 绑定 Actor 与不可变 Publication Binding；真实 Scheduler/ToolExecutor 仅执行发布时固定的 Revision，结构化结果绑定父 Run/Action、child Run、DSL Hash、响应摘要和权威 OutputJSON 摘要；Collector/Verifier 重新读取用户隔离 child Run 后才通过。子节点失败会持久化 failed child 并向父级返回固定 Tool 错误，不产生完成证据。真实 Mongo、进程重启与生产 Goal 路由仍未验收。
- E2E-19 已完成受控进程内集成：VerifiedCheckpoint Revision 按 1→2 单调推进，两次 JSON 往返模拟持久化边界；审批恢复只执行一次真实 create_tweet，后续 ask_human 挂起前把成功写入 Observation 与中间 After Snapshot 结晶为 Evidence，人工恢复保持同一 Run 并从下一 Step 继续。最终仅一条 Tweet 状态证据、两条摘要化 Resume Evidence，实际写入和幂等完成计数均为 1。真实加密 Mongo Checkpoint、多副本 Claim/租约和进程重启仍未在 Goal Runtime 路径验收。
- E2E-20 已完成受控进程内集成：ProviderRouter 只沿 Catalog 显式 Fallback 图尝试能力兼容模型；暂时性故障可进入允许的后备模型，认证/永久请求错误立即 `fallback_denied` 且后备调用为零，所有允许路线不可用时为 `fallback_exhausted`。Runtime 保留一个失败模型 Step 和低敏路由轨迹，VerifiedRunner 将有效终止轨迹投影为 blocked Provider Routing Evidence；最终没有 Assistant Message、FinalAnswer 或 Artifact。真实 DashScope/LM Studio 故障演练和生产 Goal 路由仍未验收。
