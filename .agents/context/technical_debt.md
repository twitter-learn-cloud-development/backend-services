# Technical Debt Index（当前技术债与已落地护栏）

> 最近核对：2026-08-03
> 详细证据：`architecture_audit_report.md`、`docs/AGENT_RUNTIME_STRENGTHENING_PLAN.md`、`docs/ISSUES.md`

状态定义：`Active` 尚未治理；`Partial` 已有护栏但未闭环；`Resolved` 当前阶段已验证。

## P7 状态覆盖（2026-07-22）

- Prompt/Profile 版本治理为 `Partial`：不可变 Catalog、独立 Mongo 版本/Release 仓储、草稿/发布 CAS、稳定用户分桶、双人审批、动态项目 RBAC、管理 API/UI、跨实例刷新、运行指标实验门禁与自动回滚已完成。
- Assist 内容互动事件已有独立重试/DLQ，专用命令支持默认检查、显式限量重放、事件校验、累计重放上限、Broker Confirm 后 ACK 和脱敏 JSON 报告。当前仅完成 Fake 驱动代码验证，真实 RabbitMQ/Canal/Mongo 演练仍为 `Partial`。
- Agent Eval 已有 52 条固定任务、记录结果适配器、受控 Live Runtime Adapter、行为/语义断言/工具/预算/审批/安全指标、数据集/配置哈希、HMAC 报告签名验签、稳定/候选质量门禁，以及强制 MinIO Versioning/Object Lock COMPLIANCE、不可覆盖写入和指定版本回读验签的归档适配器。Live CLI 额外要求独立签名授权和调用前预算账本：默认 file 后端适合单机受控运行，可选 Redis 后端通过显式初始化、服务端时间、Lua 原子扣减、Reservation 幂等、Stream 审计和无 TTL marker 支持多实例共享额度；独立 inspect/revoke 模式可输出脱敏额度快照并以 append-only 事件冻结授权，状态丢失时只能形成明确的 marker-only 事故撤销。完整状态会固定原子末端 Stream ID，分页重放精确事件字段/序号/时间/增量/撤销终态、对账四类用量并输出稳定 SHA-256；该摘要不是 HMAC，不能防止拥有 Hash/Stream/历史摘要全部改写权的 Redis 管理员。Redis 管理员权限、持久化/备份、`noeviction`、Redis 外 WORM 留存及真实多副本故障演练仍是 `Partial`，通用 Runtime 的共享多实例并发配额也未因此解决。当前只有固定 qwen3.7/Profile v5 的 20+20 策略自动报告通过，52 条通用任务仍不是固定真实 Provider/Profile 的质量基线；真实外部人工 Signoff、Object Lock Bucket/Version ID、真实 Search/Page Read、业务效果信号和外部 IAM 仍未验收。

## P6 状态覆盖（2026-07-19）

- 旧的“每用户一个 Qdrant Episodic Collection”记录仅代表历史风险；当前新写入已使用共享 `agent_episodic_memory`，检索使用服务端 `user_id` Filter，状态降为 `Partial`。
- 剩余风险是受控环境的真实旧集合回填、运行只读迁移验收报告和人工确认删除旧集合，不允许在在线请求中自动迁移或删除。
- RAG 评测已具备离线数据集、统一 Runner、BM25/Vector/RRF/RRF+Rerank 只读命令和 JSON 报告；RAG/Router live 入口均有显式开关、环境变量密钥、Endpoint Policy 和 Provider 失败率记录。Router 已有 34 条离线数据集、固定词典优先级、Stage 元数据、L1/L2/L3 错投基线及受显式 live 开关保护的 Semantic/LLM Provider 对照 Runner。尚未生成真实检索环境基线，也未执行固定 Provider 下的真实 Router 对照。
- Session Memory 已具备显式 Session End API、取消在途 Job、等待租约释放和强制结晶；仍需在真实 Mongo/Qdrant 环境验证回填与跨实例恢复。

## P0：高风险 Active

| 技术债 | 状态 | 代码证据 | 风险/下一步 |
|--------|------|----------|-------------|
| Gateway/Agent 职责泄漏与共享 DB | Partial | Gateway 已是纯 gRPC BFF；风控 Activity 已通过 TweetService 内部 RPC 获取最小时间戳信号和提交治理命令，Worker 构造链移除 GORM/FollowRepository；两个特权 RPC 已使用方法级静态服务身份凭据，未配置时 fail-closed | 拆分 Agent API/Worker Deployment 与 DB 账号；在受控集群执行 Secret 轮换、多副本和 mTLS/服务网格边界验收 |
| 影子治理异步全量清理 | Partial | TweetService 在同一 UoW 提交可见性与幂等 `TWEET_MODERATED` Outbox；Canal 路由到专用队列；Consumer 按关注关系 ID 稳定分页，Redis 保存进度/完成标记，Lua 清理可重放，并有 1/2/4 秒重试、专用 DLQ、低基数指标和 `cmd/timeline-moderation-dlq-replay` 受控人工重放 | 代码与 Fake 驱动测试已闭环；仍需真实 MySQL/Canal/RabbitMQ 故障注入与人工重放演练、多副本/KEDA 验收后才能标记 Resolved |
| Timeline 混合 Fanout | Partial | `fanoutToFollowers` 已按 Redis 大 V集合在普通作者写扩散与大 V个人 Timeline 拉模式间切换；批次/大 V缓存失败现在进入分类重试 | 5000 阈值、活跃粉丝上限和热点作者切换仍需真实压测与动态策略；不能把当前固定阈值描述成完整自适应 Fanout |
| Consumer 直接 requeue | Partial | Timeline 创建/删除/治理、Agent Risk Control 与 Profile 内容互动均已使用固定路由的 1/2/4 秒 Retry、专用 DLQ、Publisher Confirm 后 ACK。Timeline 新重试使用版本化 `.retry.v2` 并只经 `timeline.retry -> timeline.ingress` 回到自有队列；创建/删除/治理、Profile 内容互动和 Agent 风控均有默认检查、显式限量执行、毒消息保留和脱敏报告的人工 DLQ 工具。直接 requeue 仅保留在进程关闭、失败路由发布失败后的有界等待，以及人工 DLQ 检查/保留路径 | 仍需真实 RabbitMQ 演练 Confirm/Channel/ACK 故障、三次失败入 DLQ、旧 Retry Queue 排空、多副本恢复与四类人工重放 |
| Timeline 创建派生投影 | Partial | `tweet.created` 在 ACK 前完成幂等 ZSet、72 小时 Redis Lua Marker 趋势投影和唯一 `dedup_key` 的 `sync_es` 入队；Outbox 已有 MySQL 8 原子 Claim/Lease、Owner/Token 围栏、过期恢复、60 秒执行超时、并发批次和有界低基数指标 | 点赞/评论仍是 ACK-before-async 且热度计数未做事件级去重；外部索引成功但 Outbox 回执丢失仍可能重复 Embedding/网络成本；真实 Redis/MySQL/RabbitMQ/ES/Qdrant 故障与多副本演练前保持 Partial |
| Agent 组合根职责过载 | Partial | `internal/module/agent/startup` 已提供 `api/worker/all` 启动计划；`cmd/agent-service/main.go` 按角色隔离 gRPC/MCP/Consul 与 MQ/Temporal/巡检，但依赖装配仍集中在单文件 | 下一步把 API/Worker 装配函数和 Kubernetes Deployment 物理拆开，继续收紧 Worker 依赖 |
| 热点播报双调度风险 | Resolved | 生产组合根只注册并启动 Temporal `TrendingReporterWorkflow`；`AGENT_TRENDING_REPORTER_OWNER` 只接受 `temporal|disabled`，本地 `TrendingReporter.Start` 不再自动接线 | 滚动升级先替换旧 Worker；长期可删除未注册的 Legacy Reporter 实现 |

## P1：架构演进债务

| 技术债 | 状态 | 代码证据 | 风险/下一步 |
|--------|------|----------|-------------|
| Workflow Event Sourcing 未闭环 | Partial | Run 固定不可变 Revision；StateEvent/Snapshot 幂等持久化并支持恢复、公开只读 Replay 和版本化 Blackboard 检索；独立 Trace、有界事件流、Reducer、节点状态机、持久 Compensation 与跨实例取消已落地 | 补长期保留/归档策略；审批型补偿仍需显式重试，不宣称完整 Event Sourcing 或所有补偿无人值守 |
| Qdrant 每用户一个 Episodic Collection | Partial | 新写入使用共享 `agent_episodic_memory` + 服务端 `user_id` Filter；迁移命令提供 `--verify-only` 与 JSON 报告；旧集合仅有界双读 | 在受控环境执行真实回填和只读验收后人工确认删除旧集合 |
| Agent Trace/Replay 不完整 | Resolved | Run/Step/LLM/Tool 独立 Trace、Metrics、OTel、只读 Replay、有界事件流、Tool Result 引用、Blackboard 检索、模板身份、安全采样与 Grafana 面板已落地 | Legacy 路径随 Runtime v2 灰度补细粒度信号；长期 Trace 留存/归档属于生产部署增强 |
| Messenger Cursor 未完成 | Active | `messenger_service.go` 的 cursor TODO、固定 HasMore | 补稳定 Cursor、索引与分页测试 |
| Auth Logout/注册接线 TODO | Active | `cmd/auth-service/main.go`、`module/auth/grpc/auth.go` | 完成契约与 Token 撤销语义 |

## P2：Agent 强化中的 Partial

| 能力 | 状态 | 已有护栏 | 剩余工作 |
|------|------|----------|----------|
| Runtime | Partial | 统一 Runner、Action、Token/Cost Budget、并发准入、Usage、Rollout、Trace | Runtime v2 全模式灰度、共享式多实例并发配额 |
| Workflow-as-Tool | Partial | 用户显式发布、不可变 Revision/DSL Hash、Revision CAS、租户目录、只读 DAG 双重校验、统一 ToolExecutor、父子 Run 谱系和独立默认关闭开关 | Skill 版本目录、自动模板提取、父子聚合预算/成本视图、挂起/审批型 Workflow 契约及 Multi-Agent Planner |
| Model | Complete | model_kind 生效、Catalog Provider/Fallback 路由、Pricing Version、Endpoint Policy、Provider 低基数指标 | Provider 质量评估与自动路由策略仍需 Eval 数据支撑 |
| Credential | Complete | DSL 拒绝明文 Key、环境 `credential_ref`、用户 Provider Config AES-GCM 加密、轮换/撤销 | KMS/HSM 托管主密钥属于生产部署增强项 |
| Tool Safety | Resolved | ToolSpec/实例 Registry/统一 Executor、写工具 fail-closed、Workflow/Unified Agent 共用 Approval 状态机与一次性恢复授权、精确动作 Checkpoint、调用前重新授权、持久结果回放、TweetService 原生幂等、外部 MCP 风险单次尝试/声明式幂等写入、熔断指标、治理对账、MCP 双重认证、敏感审计脱敏、Tool Trace/Replay，以及显式 Live/Write、脱敏签名报告的 MCP 验收框架 | 生产环境继续补授权异常率告警、长期审计保留，并实际执行真实第三方幂等履约、旧 Token 撤销、Projected Secret 与多副本故障演练；不宣称跨系统严格 exactly-once |
| Session Memory | Partial | Token Budget、阈值/空闲摘要、租约游标、结构化 Payload、共享 `agent_episodic_memory`、Qdrant `user_id` Filter、显式 Session End、51 条检索集与 34 条 Router 集 | 真实租户回填与隔离验收、BM25/Vector/RRF/Rerank 真实报告、Semantic/LLM Router 对照 |
| Agent Eval | Partial | 52 条九类固定任务、存储无关 Executor、受控 Live Runtime Adapter、确定性输出断言、工具/审批/预算/安全指标、数据集/配置哈希、HMAC 签名验签、稳定/候选比较门禁、CI 非零退出，以及 Versioning/Object Lock COMPLIANCE 归档代码闭环 | 配置并验收真实 Object Lock Bucket 与 52 条 Provider/Profile 基线、真实 Resume Token/MCP 验证、归档回执绑定候选晋级审批和业务效果信号 |
| 公共扩展市场 | Partial / Frozen | 已具备独立内部认证、平台管理员与发布者 Owner、Active/Retired/Revoked 公钥生命周期、签名发布、Revision CAS、终态撤回、追加审计，以及逐条重验的无密钥公开目录和独立管理 UI；公开/管理 Feature Flag 均默认关闭 | 当前只补真实 Mongo 多副本、Secret 轮换和撤销后目录可用性验收。Artifact 存储/下载、扫描、依赖解析、安装审批、Owner 转移与租户安装状态不属于当前产品完成条件，只有真实发布者或安装需求后再立项 |

## 已验证 Resolved 护栏

- 推文长度不再全局硬编码为 280；服务和工作流支持配置化字符限制。
- 前端 Chat 模型选择不再展示 Embedding 模型，`model_kind_id` 实际进入请求。
- 自定义 Base URL 已有 SSRF、Redirect、DNS Rebinding 基础防护。
- 新 Workflow DSL/运行输入拒绝明文 API Key，旧 DSL 查询脱敏。
- Agent Runtime 已有 Token 输入/输出/总预算和 Provider Usage/Estimated 区分。
- Workflow 已有共享节点尝试/并发/超时/Token/成本预算；直接 LLM、Runtime Agent 与 Legacy 回退调用均在请求前预留并在 Checkpoint 恢复后继续累计。
- 长回合逐条写 Episodic Memory 已改为 Session Summary 增量结晶。
- Agent 发布稳定键已透传到 TweetService；Tweet/Poll/Outbox/幂等绑定同事务提交，重试不会重复发推。
- Tool 熔断、低基数 Prometheus 指标、审批/执行租约对账和 MCP 回环绑定/双重认证已完成。
- 重复实现拓扑、黑板、路由、重试和审批的 Temporal 用户 DAG bridge 已删除；Temporal 只保留真实注册的风控与热点后台 Workflow。

## 使用规则

- 不得把 `Partial` 描述成“生产闭环”。
- 技术债治理完成后，在本文件改状态并链接验证证据。
- 新发现的编译、测试、运行或部署故障写入 `docs/ISSUES.md`，不要堆在本文件。
- 阶段任务状态写入 `docs/PROJECT_PROGRESS.md`，本文件只保留稳定债务索引。
