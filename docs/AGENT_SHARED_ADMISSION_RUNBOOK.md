# M7-R2 Agent角色依赖与共享准入

日期：2026-10-13。当前为代码与离线验证范围；共享开关默认关闭。真实Redis初始化、MySQL最小权限、Provider限流/费用、进程崩溃与部署旅程未Accepted。

## 1. 功能模块与轮次

| 子项 | 代码边界 | 验证与后续 |
|---|---|---|
| M7-R2-A Persona依赖 | domain.AgentPersonaStore；repository.SQLPersonaStore；MemoryManager不持有GORM。仅API/all读取独立MySQL凭据、检查agent_personas并装配Persona/认知引擎；worker不打开Persona数据库。 | Fake Port、SQL驱动、角色和组合根AST检查；真实账号与不可越权仍待验收。 |
| M7-R2-B 共享Run准入 | admission.Shared实现Runtime接口，Redis TIME/原子多scope租约，global/user/workflow配额，同执行链借用父scope、工作流独立新增scope。 | 双client竞争、嵌套/释放、崩溃过期、续租/失效/取消；真实多进程/故障/容量待验收。 |
| M7-R2-C Provider准入 | Runtime Model Adapter/默认Router、Workflow LLM节点、Agent兼容及Workflow策略SDK调用均在出网前预留共享并发、估算输入+输出上限与可选费用。 | Fake SDK/受控HTTP、拒绝前零调用、输出上限/释放/取消、服务器分钟/费用预算；其他调用清单见下方。 |
| M7-R2-D 管理与发布接线 | 显式管理CLI、配置摘要、低基数指标/告警、三份Compose透传与生产角色预检。API/all启用共享时必须所有Runtime v2模式开放，检查在业务依赖初始化前。 | 默认离线/审批摘要/只读与初始化/有界连接、YAML/角色AST/PS语法；没有运行Compose、构建镜像、初始化真实Redis或部署。 |

Runtime核心不导入Redis/GORM/Service。Mongo/Redis/模型及公共检索等其他装配仍按现有服务需要保留，Worker不是彻底移除所有API对象的最小依赖进程。API不启动MQ/Temporal Worker；Worker不启动gRPC/MCP服务或语义锚点初始化。生产模板仍只有一个agent-service，真实API/Worker拆副本、发现和抓取配置需单独验收。

## 2. 配置与覆盖范围

| 配置 | 默认 | 含义 |
|---|---|---|
| AGENT_SHARED_ADMISSION_ENABLED | false | 关闭时保留原进程内Run准入且不加共享Provider策略；开启后Redis失败不降级为本地。 |
| AGENT_SHARED_ADMISSION_NAMESPACE | agent-v1 | 所有遵守同一策略的实例必须使用相同namespace。 |
| AGENT_SHARED_ADMISSION_LEASE | 30s | 6—60秒；每1/3租期续租，scope TTL为3倍租期。 |
| AGENT_SHARED_MAX_RUNS | 64 | namespace内顶层Run上限。 |
| AGENT_SHARED_MAX_USER_RUNS / AGENT_SHARED_MAX_WORKFLOW_RUNS | 2 / 2 | 对应用户/工作流scope上限，支持嵌套借用同一执行链已持有的scope。 |
| AGENT_SHARED_MAX_PROVIDER_CALLS / AGENT_SHARED_MAX_PER_PROVIDER | 8 / 8 | 所有受保护调用/规范化Provider标识的并发上限。 |
| AGENT_SHARED_OUTPUT_TOKEN_LIMIT | 4096 | 共享策略将受保护请求的输出上限限制到该值；默认/非正值也设置有限上限。 |
| AGENT_SHARED_TOKEN_BUDGET_PER_MINUTE | 250000 | Redis服务器固定分钟的估算输入+最大输出预留上限，包含工具schema/历史调用；并非真实Tokenizer或账单。 |
| AGENT_SHARED_COST_BUDGET_PER_MINUTE | 0 | 0关闭费用约束；开启必须显式设置上界单价。 |
| AGENT_SHARED_COST_MICROS_PER_TOKEN | 0 | 统一货币微单位/token，必须覆盖当前所有受保护Provider/model的输入与输出上界价格；不自动下载价格。 |
| AGENT_RUNTIME_V2_MODES | 原有配置 | API/all共享启用必须为all或等价完整五模式列表；不完整/非法配置拒绝共享启动。Worker不强制此模式门禁。 |

配额1—1024；输出1—32768；Token预算1—10^9；费用预算0—10^12；单价0—10^6。非法配置即使开关关闭也拒绝启动，避免随后启用时隐藏坏策略。策略摘要覆盖所有配置字段，实例配置不一致拒绝Ready/Initialize。

Provider配额按有限长度Provider标识哈希，不按具体模型/账号/端点细分；两个Provider名称可对应同一上游，global Provider配额仍覆盖这些调用。Router每次主/备用尝试分别预留；共享额度/预算错误不属于可自动回退的Provider错误。单次预算和共享分钟预算同时约束Workflow，失败/取消/响应不确定不退还分钟预留，只释放并发槽；预算Hash TTL为120秒，固定分钟边界可能在短窗口消耗两个分钟的额度。

覆盖：Runtime.OpenAICompatibleClient、默认DashScope/LMStudio Router、LLMChatTool、Agent兼容SDK调用及Workflow策略SDK调用。未覆盖：pkg/ai中的摘要、语义Router、RAG/Embedding、Reranker、Temporal/AIOps直接调用和独立Eval命令。它们有各自既有控制，不能由该开关推断全项目费用封顶。当前文本兼容guard拒绝多模态输入；Workflow共享模式拒绝没有显式可限制输出端点的旧pkg/ai后备路径。启用前必须审查上述额外负载/价格/上游真实额度，估算预留不代替账单核对。

## 3. Redis语义与恢复边界

key前缀为agent:admission:{SHA256(namespace)}，scope身份也哈希；Lua所有key位于同一hash slot。当前客户端为普通redis.Client，没有提供Redis Cluster、Sentinel、ACL USER或TLS专用接线；不能由hash tag推断这些部署形态已支持。

- ready为每namespace一个无TTL策略摘要哨兵，独立Initialize建立；热路径发现缺失/漂移失败，不自动重建。Hash/zset类型、scope基数（≤2048）等在任何写之前检查。
- Run/Provider同时占所需scope，服务器TIME检查有效期；并发竞争只有完整获得所有scope者成功。续租要求原随机token在每个scope仍有效，失效后不复活；旧释放不能删除替代token。
- 每个Redis操作最多2秒，续租失败取消传给Runner/SDK的Context。正常用户取消不记录lease_lost；显式release仅执行一次，独立最多2秒清理，失败记录release_uncertain并等待过期。崩溃/漏释放由服务器时间及TTL恢复容量。
- Redis响应不确定可能已登记租约/预算，调用返回失败但保留保守占用。旧父Context共享scope不表示新增独立顶层Run也被计数；调用者必须用正确的执行Context/用户身份，禁止跨用户搬用父Context。
- Redis持久化/复制丢失、淘汰scope或budget、时钟跳变可能破坏容量保证；需要专用容量/noeviction和权限治理、真实故障测试。取消Context不是外部Provider侧围栏，不保证已送达的模型调用立刻停止计费或副作用回滚。租约过期不代表远端调用已经结束。

该门禁控制正在执行的受保护Run，不提供持久等待队列、自动重试或恢复已失败运行。已挂起/等待人工审批的Run仍按既有Checkpoint/Resume规则恢复；租约不能替代其用户、revision、单次授权或工具审批。

## 4. 管理命令与启用顺序

`go run ./cmd/agent-admission-admin` 默认仅输出策略配置与SHA256，不读取Redis凭据、不构造client。管理操作最多5秒（连接和动作共用Context）；输出固定报告/摘要，不保存Prompt、用户或Provider凭据。现有REDIS_HOST/PORT/PASSWORD/DB仅在显式Live读取。

```powershell
# 离线生成待审核策略，建议使用已构建的精确版本程序。
go run ./cmd/agent-admission-admin
# 明确只读：仅核对当前namespace策略是否Ready。
go run ./cmd/agent-admission-admin --allow-live
# 实际写入需要已审查策略摘要及操作人/原因。
go run ./cmd/agent-admission-admin --allow-live --initialize --approved-policy=<reviewed-sha256> --operator=<operator> --reason=<reason>
```

本轮未执行上述任何Live命令。CLI的声明/摘要是操作门禁，不是持久RBAC、审计授权账本或组织审批系统。重复初始化匹配策略是幂等的，不清slots/budget；漂移拒绝，命令没有reset/flush/更改现有策略模式。

1. 先按M7-R1确认API所需agent_personas既有Schema/最小权限；worker无需MySQL Persona凭据。生产预检仍要求其他七角色独立账号，API/all要求Agent独立成对凭据，Worker跳过该要求；Worker健康探测使用9191，API/all仍探测9100。
2. 审查未覆盖Provider调用、真实tokenizer/价格上界、Redis持久化/权限/容量和取消行为。确认Redis已经就绪后另行授权Live，保留离线策略摘要/精确版本与操作证据。
3. 停止/排空全部旧或本地准入执行实例，确认远端在途调用情况。初始化同一namespace策略；所有API/all使用完整Runtime模式与相同共享策略后启用。不能灰度混跑受不同准入后端控制的执行者后仍宣称全局限额成立。
4. 验证跨实例同用户/工作流与不同Provider竞争、TTL/续租、Redis断连/回执丢失/进程崩溃、Provider实际usage与预算拒绝。查询agent_shared_admission_total的kind=run|provider和固定result；确认告警送达、全部实例/Worker抓取配置正确。
5. 新策略需另审窗口与摘要。保留旧namespace状态，不通过换namespace/删键规避当前预算。迁移须停止全部旧owner并确认在途调用，不能仅等租约超时就认定远端完成。

回滚：停止/排空共享实例后统一恢复false及原Runtime rollout，保留Redis控制状态/预算与所有SQL结构、Mongo运行证据。回到本地准入会失去跨副本上限；不得自动在Redis故障时切本地继续执行。Persona Port兼容原agent_personas，无新DDL，也不恢复运行AutoMigrate。Persona主体删除写围栏仍待专项治理，生产数据删除保持关闭。

## 5. 离线验证

Fake/SQL驱动、miniredis双client和httptest仅验证实现/接线；不证明真实MySQL权限、Redis故障持久化、多进程竞争、Provider费用、staging容量或告警投递。扩大Agent测试、Vet、相关Race结果和问题记录以ARCHITECTURE_REMEDIATION_PLAN/PROJECT_PROGRESS/ISSUES243的本轮收口为准。
