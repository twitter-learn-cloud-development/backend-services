# Twitter Clone 云原生微服务项目 — 进展与规划

## 仓库同步与 Python 客服纳管（2026-10-26）

| 功能模块 | 状态 | 范围 |
|---|---|---|
| 历史开发同步 | Implemented（源码整理） | 纳入当前 Go/Web/Mobile、部署工具及状态文档；不把历史离线门禁改成生产 Accepted |
| Python 使用向导 | Implemented（源码迁入） | `support-agent-service/` 共用主仓库 Git，独立依赖/镜像/前端；更新 Compose 构建和脚本定位，保留旧相邻目录备份 |
| 运行环境 | 本轮不变 | 不迁移密钥、不启动/停止服务、不创建测试用户、不调用付费 Provider、不执行数据库迁移 |

本轮命令、复验结果与限制统一记录于
`support-agent-service/docs/repository-sync-2026-10-26.md`。


## 架构修复 M2-R3补充轮：有界并发只读验收（2026-10-15）

| 功能模块 | 轮次 | 状态 | 本轮交付 |
|---|---|---|---|
| Feed容量工具 | R3-D1 并发与快照 | Implemented（离线验证） | 每样本独立ReadOnly+RR事务/回滚，最多8并发/100样本/20页/60秒；有界池/取消退出，复用生产查询，精确ID/lookahead连续页与可见性/关注校验 |
| 数据与报告 | R3-D2 证据质量 | Implemented（离线验证） | 有界关注探针/表行估计/EXPLAIN、最少页/行/关注门槛；v2成功/失败/截断/延迟统计和可选p95预算，固定脱敏错误；capacity_accepted恒false |
| 操作收口 | R3-D3 文档与兼容 | Implemented（离线验证） | 默认不联网/不读凭据，旧参数保留但独立样本事务与v2字段需适配；旧v1报告保留历史解释，完整命令/限制/回退记录于FEED_ACCEPTANCE_RUNBOOK |

最终通过（Go每shell显式`GOCACHE=<repo>/tmp/go-build-cache`）：

```powershell
go test ./cmd/feed-read-acceptance ./internal/module/tweet/repository ./internal/module/follow/repository -count=1
go vet ./cmd/feed-read-acceptance ./internal/module/tweet/repository ./internal/module/follow/repository
go test -race ./cmd/feed-read-acceptance ./internal/module/tweet/repository ./internal/module/follow/repository -count=1
# 最后追加JSON判定字段后，仅入口再次运行以上Test/Vet/Race
```

11个入口顶层测试（含非法页子场景）通过，数据库驱动夹具只检验查询/事务装配与输出，不证明真实MySQL权限/执行计划/隔离效果；并发取消夹具核验实际峰值3个session及关闭，成功样本逐个建事务。CLI默认门禁/非法预算在连接前拒绝；gofmt与本轮空白检查通过。旧v1未消费关注布尔结果、空样本可产生耗时报告的缺口已修复（ISSUES248），没有本轮编译或测试失败。

本轮没有访问真实MySQL/Redis、启动依赖、DDL/种子写入、索引变更/压力操作、部署或发布。未改生产Feed/API/两端，不重复Web/Mobile构建。M2-R3完整容量和M8-R3现场继续Partial；下一步需先准备受控高关注/稀疏深历史fixture和明确负载/资源预算，再运行v2并留存源码/环境/原始资源证据，不能从本机Fake或旧小数据集推定通过。

## 架构修复 M4-R2：私信跨端与实时恢复（2026-10-15）

| 功能模块 | 轮次 | 状态 | 本轮交付 |
|---|---|---|---|
| Messenger | R2-A 提示发布 | Implemented（离线验证） | MySQL成功后共用2秒Redis提示预算/Trace、固定结果指标，无无界goroutine；失败不诱导已存消息自动重发 |
| Gateway | R2-B Socket生命周期 | Implemented（离线验证） | 订阅ACK后ready、单writer/心跳/有界读写/认证到期、取消并等待reader，修复double-close |
| Web | R2-C 分页与恢复 | Implemented（离线验证） | 会话与历史续页、字符串ID去重/已读合并、独立锚点3页补拉/继续、ready/前台/周期恢复、草稿和旧响应隔离 |
| Mobile | R2-D 分页与恢复 | Implemented（离线验证） | 同样恢复边界，Socket ready、同账号另一设备发送合并、Widget处置和不确定发送草稿保留 |
| 契约与运维 | R2-E | Implemented（配置与离线验证） | API/运行手册/指标及告警规则；无新Proto/DDL/依赖升级，持久收件箱/幂等发送/ACK另列需求 |

最终通过：Messenger/Gateway目标Test、Vet、Race，两生产入口编译；Web18项测试与构建；Flutter analyze零问题、完整32项测试；本轮格式化/空白检查。修复vendor缺少promauto/testutil导致的编译失败（复用现有core/dto，不更新vendor）、Web模板never推断和Flutter规范提示，详见ISSUES247。命令与夹具覆盖见`MESSENGER_RECOVERY_RUNBOOK.md`。

M4代码/离线闭环，模块整体仍Partial：既有索引未应用，真实MySQL计划/深历史、断网/多设备/多副本/容量及告警送达未Accepted；Pub/Sub是可丢失提示，已读非设备确认，发送无幂等键，ID非提交序列。没有真实业务写入/DDL/部署或验收签发。M8-R3和M2-R3完整容量继续Partial；下一步需按各自运行手册补受控现场证据，不能重复离线测试替代。

## 架构修复 M8-R3：部署验收工具与证据门禁（2026-10-15）

用户选择本轮仅完成工具与离线验证；模块整体Partial，真实部署/31项现场旅程未Accepted。

| 功能模块 | 轮次 | 状态 | 本轮交付 |
|---|---|---|---|
| M8 发布与功能收口 | R3-A 发布/环境身份 | Implemented（离线验证通过） | v2签名绑定审核发布清单/source/环境快照/部署ID/前版清单，应用/基础设施digest与公开配置版本，拒绝候选和漂移 |
| 验收证据 | R3-B 逐项现场观察 | Implemented（离线验证通过） | Passed必须有check/身份/时间/actor/原始证据摘要对应观察；24小时新鲜度、有界严格JSON/路径、签名全字段覆盖、写锁/独占初始化 |
| 用户旅程 | R3-C 31项目录 | Partial | 新增认证、搜索可见性、Feed私信、Schema owner和共享准入；完整现场手册和待办，真实旅程Not run |
| 兼容与发布门禁 | R3-D v1/CI | Implemented（离线验证通过） | v1可读但不可release-ready，新v2夹具接CI；合成目录独立、清理跨平台；真实Actions未执行 |

最终通过：四脚本AST，v2 31项/24个拒绝用例、v1 31项且release_ready=false，发布工具目标Test/Vet/Race，12服务CLI矩阵和workflow production-config新夹具接线。CLI默认缓存访问拒绝已通过每命令显式repo/tmp/go-build-cache复验解决；根signature以外字段均签名，嵌套signature注入夹具通过，ISSUES246已Resolved。业务/前端没有变更，未重复全仓/Web/Mobile检查；真实Actions与现场证据未验收。

计划/兼容/回退详见 `DEPLOYMENT_ACCEPTANCE_RUNBOOK.md`；本次现场待办见 `DEPLOYMENT_ACCEPTANCE_2026-10-15.md`。无HTTP/Proto/客户端变更、业务依赖启动/联网探测、DDL/数据写入/恢复、Provider/MCP/告警或Registry/部署操作。下一代码轮次可补M4-R2私信跨端实时恢复；M2-R3容量与M8-R3真实验收继续保留Partial。

## 架构修复 M8-R2：认证与剩余产品契约（2026-10-14）

| 功能模块 | 轮次 | 状态 | 本轮交付 |
|---|---|---|---|
| M8 发布与功能收口 | R2-A 认证语义/HTTP | Implemented（代码与离线验证） | 本地退出回执/全设备501、密码修改与身份/字节/请求/超时/错误边界 |
| Web | R2-B 会话与安全入口 | Implemented（代码与离线验证） | 成对保存与损坏缓存清理、代次隔离/跨标签重载、Axios/SSE401、身份切换Socket/页面/客服重建、密码修改 |
| Mobile | R2-C 会话与安全入口 | Implemented（代码与离线验证） | 串行持久化/排队Logout竞争、认证驱动路由、通知缓存换代、Socket重连取消/旧回调、密码修改/退出和敏感日志移除 |
| 产品契约 | R2-D 差异账本 | Implemented（文档） | 入口差异、服务端撤销/找回密码Planned，删除执行继续关闭 |

最终复验：Go全仓`go test ./... -count=1`、Auth/User/Gateway与domain相关Vet、Auth/User/Gateway Race通过；Web完整build与13项测试通过；Flutter analyze零问题、完整25项测试通过，含Socket取消重连/处置后禁止建连、旧网关非JSON错误兼容。补充User领域未找到错误映射，数据库故障不再伪报404。首次回环沙箱失败正常提权复验通过，4条本轮风格info已修复，见ISSUES245。

本轮没有新增Schema/Proto字段/会话存储或真实服务操作。**本地退出和密码修改不会提前撤销JWT**，既有令牌仍有效至到期；HTTP退出是能力回执，客户端清理不依赖它成功。真实多标签/跨设备/Socket/密码并发与不确定回执仍待部署验收；不声明全设备退出、全功能跨端等价或生产Accepted。计划/兼容/回退和差异详见 `AUTH_SESSION_CONTRACT.md`、`API_REFERENCE.md` 与 `ARCHITECTURE_REMEDIATION_PLAN.md`。下一轮M8-R3部署验收准备与证据门禁，真实操作按环境与授权边界推进。

## 架构修复 M8-R1：完整发布流水线（2026-10-13）

本机完整验证与Mobile修复收口：2026-10-14。

| 功能模块 | 轮次 | 状态 | 本轮交付 |
|---|---|---|---|
| M8 发布与功能收口 | R1 完整发布流水线 | Implemented（代码与本机离线验证；真实CI/镜像未Accepted） | 包级构建/完整镜像目录、Go/Web/Mobile/配置CI门禁、显式Registry发布、离线发布回执/清单与只读复验 |

- [x] M8-R1-A：Makefile build覆盖全部./cmd/...、run按包；10服务Dockerfile移除main.go构建、Go1.25.5/CGO0/trimpath，Agent附带admission-admin；frontend沿用现有npm锁定安装/真实5556端口，Schema迁移镜像纳入矩阵。
- [x] M8-R1-B：CI由Go.mod选版本，全仓Test/Vet/指定Race/包级Linux构建、Web npm ci/build/test、Flutter3.44.2 lockfile/analyze/test、生产Compose和验收记录夹具；任何门禁失败不生成完整清单。
- [x] M8-R1-C：catalog.json单一12组件矩阵覆盖全部Compose build服务与独立Job；默认候选OCI、main显式dispatch publish才登录/Registry上传且需审核基础digest，移除自动Git/Helm更新和latest标签。
- [x] M8-R1-D：离线release-manifest记录真实CI输出、12镜像/9检查精确回执、干净Git HEAD绑定、配置/锁/SQL/Schema摘要、候选/发布区分与只读Verify/require-published；拒绝缺项/漂移/坏digest/重复JSON/伪部署Accepted、路径逃逸/覆盖，CLI真实拒绝当前脏工作区。
- [x] releaseplan/CLI Test/Race、全仓compile-only/Vet、全部cmd Windows/Linux构建、Web vue-tsc及npm test七项、实际Compose九项config-only夹具、26项验收记录自测/PS Parser与静态接线通过。compile-only不是完整Test；问题见ISSUES244。
- [x] 审批恢复复验：完整go test ./...与CI同范围工程Race、Web build/npm test全部通过；Flutter analyze --no-pub零问题、flutter test --no-pub全部16项通过，只用已有依赖/本机Mock。
- [x] Mobile门禁修复：移除未使用导入/空类与弃用ColorScheme字段、Color.withValues替换旧透明度API、相关页面await后检查mounted；Workflow输入对话框context与页面context区分。旧计数器烟测替换为Mock Storage/ProviderScope真实登录、表单验证与注册入口/零API调用，补页面卸载后迟到成功/失败测试；未禁用Lint。
- [ ] 真实GitHub Actions、12镜像构建/拉取、锁安装/Registry发布、实际生产配置/权限/Secret、clean-host与部署旅程尚未执行或Accepted；没有安装本机依赖/启动业务服务/执行DDL/发布/部署。

M8-R1代码与本机离线门禁已收口，下一代码轮次为M8-R2认证与剩余产品契约。部署接受仍false，用户删除继续关闭；M2-R3完整容量Partial、M4-R2仍Planned。旧Helm/演示tag导出保留为历史路径；新发布清单不能证明其已迁移。启用、证据可信边界、回滚和准确验证命令见 `RELEASE_PIPELINE_RUNBOOK.md`。

## 架构修复 M7-R2：Agent角色依赖与共享准入（2026-10-13）

状态：Implemented（代码与离线验证）；共享开关默认false，真实依赖/容量/费用与部署未Accepted。

| 功能模块 | 轮次 | 状态 | 本轮交付 |
|---|---|---|---|
| M7 服务与运行隔离 | R2 Agent依赖与共享准入 | Implemented（离线验证） | Persona角色Port、共享Run/Provider租约与估算预算、取消恢复、独立初始化与启用门禁 |

- [x] M7-R2-A：AgentPersonaStore/SQLPersonaStore；MemoryManager移除GORM。仅API/all读取Persona凭据、打开/校验MySQL并装配认知引擎，Worker无需Persona DB；保留既有Mongo/检索/模型等装配边界。
- [x] M7-R2-B：Redis TIME/同slot原子global/user/workflow租约、独立token/续租/释放/失效取消、嵌套scope借用；配额/TTL/脚本访问有界，策略未初始化/漂移失败关闭，不自动切本地。
- [x] M7-R2-C：Runtime/Router、Workflow LLM与Agent兼容/Workflow策略SDK共享Provider并发、输入估算+输出上限、可选统一费用预留；失效取消传给SDK，失败不退分钟预留。API/all启用必须完整Runtime v2模式，业务依赖前检查；保持原单Run预算/工具审批。
- [x] M7-R2-D：默认离线agent-admission-admin，Live只读与摘要/操作声明初始化、连接/操作共5秒；三Compose和.env透传、生产Worker凭据/健康探测、固定标签指标与告警接线。
- [x] 双client竞争/嵌套/崩溃/续租/替代token/取消、Token/费用/坏状态原子拒绝、Fake SDK/HTTP零调用拦截、Persona SQL/Port、角色AST/YAML/CLI测试；完整Agent+Schema/Persistence/两入口测试、Vet、相关Race通过。编译/SQL夹具和沙箱回环问题见ISSUES243。
- [ ] 真实Redis持久化/noeviction/故障/多进程与Provider账单、MySQL最小权限、告警投递、API/Worker副本部署与容量验收；摘要/RAG/Embedding/Reranker/Temporal/Eval调用仍不在共享Provider预算内。

下一代码轮次：M8-R1完整发布流水线。M2-R3完整容量保持Partial、M4-R2保持Planned，数据删除继续关闭。本轮无新DDL、真实依赖启动/写入/初始化、镜像构建或部署；启用/变更/回滚必须停止并排空旧owner，保留控制状态，不能由租约过期推断远端调用停止。参见 `AGENT_SHARED_ADMISSION_RUNBOOK.md`。

## 架构修复 M7-R1：数据所有权与独立迁移（2026-10-13）

| 功能模块 | 轮次 | 状态 | 本轮交付 |
|---|---|---|---|
| M7 服务与运行隔离 | R1 数据所有权/迁移 | Implemented（离线验证）；真实Schema/账号/Job未Accepted | 可执行Owner目录/只读启动Schema、独立Bootstrap Job/权限计划、生产八角色凭据、Tweet只读Follow Port |

- [x] 八MySQL入口和MemoryManager构造不再AutoMigrate，按runtime role≤32表/5秒参数化metadata校验InnoDB/列/64位身份/NOT NULL/精确索引；开关启用额外校验Follow/Publication，失败拒绝启动。NewDB初始化与driver I/O有界/规范DSN、移除密码fallback。
- [x] Owner目录生成稳定离线DDL/计划摘要；schema-migrate默认不读凭据或联网、独立Live≤60秒/非root迁移凭据、执行匹配计划和操作声明；同连接Owner锁/先全量预检既有表/仅CREATE缺表/写后重验，DDL不确定停止并支持核验后重跑，不ALTER/DROP/自动改既有表。
- [x] 只输出的角色访问GRANT计划/无账号或权限操作；八角色生产Compose独立凭据/强制策略、部署预检非root/不同账号/迁移隔离；默认离线单次Compose Job与按包构建Dockerfile，运行容器无迁移凭据。
- [x] TweetService依赖收窄为FollowReadRepository，生产只读wrapper不导出写接口；Persona领域模型/原UserPersona类型兼容；表/缓存Owner、共享Outbox/Feed/CDC例外与升级/回滚矩阵已记录。
- [x] GORM offline计划/SQL元数据与命名锁驱动、预检拒绝/部分DDL/回执不确定重跑/取消/权限范围/配置/CLI/生产YAML/PS语法；扩大模块与入口回归、Vet、指定Race和格式检查通过。接线/后缀/旧夹具修复见ISSUES242。
- [ ] 未执行真实DDL/账号创建/GRANT/服务重启/Docker构建或部署；运行权限/继承角色/Schema细节/MDL/备份回滚与容量未Accepted。共享MySQL、Feed跨表候选、Consumer跨模块读、角色分支前Agent MySQL和其他账号路径未由本轮消除；生产配置非root不证明实际最小权限。

下一代码轮次：M7-R2 Agent依赖与共享准入。旧表升级仍需具体审核SQL，回滚保留所有新表/索引/Outbox/位点且禁止恢复旧AutoMigrate；参见 `DATA_OWNERSHIP_MIGRATION_RUNBOOK.md`。M2-R3完整容量保持Partial，M4-R2保持Planned。

## 架构修复 M6-R2：受控重放与多副本衰减（2026-10-13）

| 功能模块 | 轮次 | 状态 | 本轮交付 |
|---|---|---|---|
| M6 互动热度一致性 | R2 重放与多副本 | Implemented（离线验证）；真实恢复/容量未Accepted | 默认离线互动重放、专用会话/Confirm重建、服务器分钟桶原子衰减/TTL/基数保护 |

- [x] `engagement-dlq-replay` 默认不读取MQ凭据/联网，Live检查另显式allow-live；Execute要求operator/reason、最多100消息/60秒/累计1—10次，只回互动专用Ingress，Confirm后ACK。毒消息/身份时间缺失/过期/次数耗尽保留，取消/发布失败保留批次，ACK不确定停止；固定摘要报告。生命周期CLI路由范围不变。
- [x] 每互动队列独立消费+Confirm连接/QoS1、5秒总初始化与协议取消，关闭/Publisher Channel失败重建拓扑和订阅；不替换共享生命周期Channel。会话取消中断在途，关闭等待循环/Watcher；入口等待Timeline及衰减退出。
- [x] 移除50秒锁/ZINTERSTORE，Redis TIME分钟桶/7天状态与分值裁剪同Lua；最多10000基数/保留原Top100/0.95一次衰减，保留剩余TTL/无TTL修复24小时、时钟回拨拒绝，遗漏桶不追算；固定指标和两项新告警。
- [x] 32并发同桶、50秒窗口/重启重试/跨桶/TTL/排名/错类型数值/超基数/取消；重放路由/Confirmed ACK/毒消息/过期/累计/批次失败，独立会话重建/取消/专属Publisher、net.Pipe协议取消、默认CLI离线/参数门禁和指标/YAML；扩大测试/Vet/Race及格式检查通过。
- [ ] 真实Redis AOF/OOM/淘汰/故障切换、多实例进程崩溃/时钟/10000基数延迟，RabbitMQ实际绑定/双连接断连/权限/Confirm不确定、告警和容量仍未Accepted。Header计数不构成跨ACK不确定的全局次数账本，检查会NACK/requeue并可能改DLQ顺序，本机操作声明不替代服务端RBAC/审批账本。

下一代码轮次：M7-R1数据所有权与迁移治理；M2-R3完整容量继续Partial，M4-R2保持Planned。无需新SQL迁移，本轮未启动真实依赖/执行真实重放/写入/部署。升级必须停旧衰减owner，回滚保留桶/标记/队列并暂停旧执行，详见 `ENGAGEMENT_RECOVERY_RUNBOOK.md`。

## 架构修复 M6-R1：互动原子幂等投影（2026-10-13）

| 功能模块 | 轮次 | 状态 | 本轮交付 |
|---|---|---|---|
| M6 互动热度一致性 | R1 原子幂等投影 | Implemented（离线验证）；真实恢复未Accepted | 稳定实体事件/重复点赞语义、Lua原子去重/频控/计分、回执后ACK、专用Retry/DLQ与观测 |

- [x] 点赞关系与Outbox同UoW，生产仓储锁定当前读返回实际ID；重复点赞不新增事件、重新点赞新关系ID。评论/点赞使用engagement-v1与实体事件键/时间/Trace，保留旧JSON字段；3秒事务和持久身份校验。
- [x] 移除先ACK再goroutine/内存batcher计分，两个队列各串行执行/8秒Context/关闭等待；读取当前公共事实，最多32主题/4096字节映射，缺少创建回执时重试。
- [x] Redis TIME + Lua在写前预验类型/值，事件摘要/共享1小时前三次频控/2或5权重/热度TTL/7天回执原子提交；48小时外仅记expired，摘要冲突永久失败，ACK丢失后幂等。
- [x] 1/2/4秒专用Retry、最多3次与独立DLQ，Confirm后ACK原消息，失败退避保留；固定日志/Trace/指标与三告警。R2自动Retry/DLQ前移，人工互动重放仍未开放。
- [x] 并发32重复、ACK丢失、频控/预检/TTL、隐藏/乱序/过期/冲突/主题上限、生产GORM内存SQL/ClientFoundRows/回滚、Outbox/Trace与恢复路由；扩大回归/Vet/Race、gofmt/diff通过，夹具/回环问题见ISSUES240。
- [ ] 真实Schema/权限、MySQL锁/提交不确定、Redis持久化/淘汰/多实例/崩溃、Broker绑定/告警和容量未Accepted；7天标记不是永久存储，全局热点ZSet/跨slot Lua限制保留。

下一代码轮次：M6-R2受控重放与多副本衰减。旧分钟Ticker/50秒锁没有本轮多副本时间桶证明，人工重放保持关闭；升级需停旧互动消费者，回滚保留队列与标记，详见 `ENGAGEMENT_PROJECTION_RUNBOOK.md`。本轮未执行DDL、真实服务/业务/Redis/MQ写入或部署。

## 架构修复 M3-R3：晋升/降级与回填清理（2026-10-13）

| 功能模块 | 轮次 | 状态 | 本轮交付 |
|---|---|---|---|
| M3 Follow关系与派生任务 | R3 晋升/降级与回填清理 | Implemented（离线验证）；默认开关关闭/迁移未执行 | v1入口持久双任务/回执、作者共享进度、关系当前事实分页、SQL/Redis围栏、独立Retry/DLQ/告警、受控修复入口 |

- [x] Consumer已接线独立关注入口/Confirm Publisher/QoS10；回执与author/pair任务同事务，成功登记才ACK；重复摘要冲突拒绝，ClientFoundRows不破坏去重。坏事件/3次失败入专用DLQ，1/2/4秒Retry只回专用Ingress。
- [x] 每lane每次Claim1、100条页/15秒执行/90秒租约；作者revision/epoch/共享游标，5000/4500防抖与首次迁移区间继承；历史关系批验当前状态、推文历史含软删除/隐藏供清理，写后代次变化重置页，旧取关不按旧动作覆盖重新关注。
- [x] SQL页回执围栏/成功续页/5次失败保留；关闭释放租约恢复本次次数。Redis服务器时间/epoch/token阻止陈旧Begin/写入，精确字符串ID、幂等Unread、初始化/重建marker失效和TTL。
- [x] Service回填/取关/晋升降级goroutine移除，旧Align拒绝执行；Follow入口移除Tweet/Timeline装配与Tweet AutoMigrate。显式最多100scope检查/审批修复只登记任务，持久审批/操作摘要；低基数指标与三告警，默认不读取凭据/联网。
- [x] 5012粉丝/断点续跑/降级、250推文/当前代次、SQL原子登记/冲突/ClientFoundRows/租约、miniredis过期/陈旧围栏、MQ Retry/DLQ/ACK、CLI/指标/YAML回归；扩大测试/Vet/Race与最终复验见开发计划。编译遗漏导入修复见ISSUES239。
- [ ] FOLLOW_PROJECTION_WORKER_ENABLED默认false；新表/历史索引SQL未执行，真实Schema/权限/绑定/Redis故障/多实例/热作者和深历史容量未Accepted。Redis淘汰后DBComplete不证明缓存完整，需显式修复；跨副本L1窗口、无DB历史关系的残留和Redis Cluster不支持边界见Runbook。

下一代码轮次：M6-R1互动投影可靠消费；M2-R3完整容量继续Partial。本轮未启动真实依赖、执行DDL、真实业务/Redis/MQ写入、镜像发布或部署。

## 架构修复 M3-R2：Follow 关系事件原子提交（2026-10-13）

| 功能模块 | 轮次 | 状态 | 本轮交付 |
|---|---|---|---|
| M3 Follow关系与派生任务 | R2 关系事件原子提交 | Implemented（离线验证） | pair锁/短事务事实+Outbox、代次事件/重复语义、生产装配与Schema门禁、Relay路由/持久暂存队列 |

- [x] Follow/Unfollow使用追加Mutation Port，缺少事务/事件依赖fail-closed；pair协调行锁和至多2条有效关系重验，历史重复不自动丢数据。旧仓储写方法也要求UoW，不走原Count/Insert竞争路径。
- [x] 3秒内关系变更与Outbox同事务，唯一键冲突回滚；重复当前状态不产事件，重新关注新关系ID/事件键。兼容旧两身份字段，追加v1/代次/发生时间/Trace Context。
- [x] Follow生产入口配置UoW/Outbox、新表/已有Outbox索引存在门禁，声明持久follow.relations.v1与两个绑定；Canal路由和publication检查兼容，人工重放仍关闭。手工迁移/最小账号权限/灰度回滚已记录，本轮未执行真实DDL或部署。
- [x] 真实GORM/UoW/仓储内存SQL故障回滚、并发重复/重新关注/历史重复/取消/模拟COMMIT不确定，Schema/路由/拓扑/Trace兼容回归；扩大测试、Vet、Race通过。Warn未初始化空指针修复与复验见ISSUES238。
- [ ] 新关注队列本轮没有Consumer，只持久暂存事件；旧提交后缓存goroutine和大V传播/取关100条清理仍留技术债，不宣称全量恢复。M3-R3负责唯一持久owner、分页/租约/Retry/DLQ/代次重验与受控对账。
- [ ] 真实Schema/权限、MySQL行锁/提交回执丢失、Relay/Broker绑定/消费/恢复及容量未Accepted；M2-R3完整容量继续Partial。协议字段/客户端未变，本轮不重复Web/Mobile构建。

下一轮：**M3-R3 晋升/降级与回填清理**。详细操作边界见 `FOLLOW_RELATION_OUTBOX_RUNBOOK.md`，实际验证命令见开发计划M3-R2交付记录。

## 架构修复 M5-R3：文本/向量拆分与对账（2026-10-13）

| 功能模块 | 轮次 | 状态 | 本轮交付 |
|---|---|---|---|
| M5 删除与索引生命周期 | R3 文本/向量拆分与对账 | Implemented（离线验证）；真实环境未 Accepted | 独立任务/lane/Profile、元数据只读对账、受控修复登记、可选发布回执/Confirm重放/保留计划与积压告警 |

- [x] 新创建 ACK 前幂等登记 sync_text/sync_vector；旧 sync_es 转登记子任务。两 lane 各批次 5 条；Embedding 锁外执行，模型/维度/版本匹配与写前/写后事实重验，文本不调用模型。失配/非有限向量/后端错误进入独立退避和耗尽保留。
- [x] 显式 1—100 Tweet IDs 的元数据对账；修复需目标/事实/索引计划摘要匹配，仅登记 Worker 任务，同 Tweet 原子事务与持久去重，跨 Tweet 部分结果可见。默认命令不读取凭据/不联网；正文/身份/操作人仅输出摘要。
- [x] 可选 Confirm 回执账本（默认关闭、手工迁移未执行）；事件检查/受控创建删除治理重放/发布感知保留候选计划。首次回执保留，冲突失败，部分回执可恢复；点赞/评论重放继续关闭，年龄 GC 无删除模式。
- [x] 固定标签的次数/耗时/持久积压/最老年龄及失败遥测；三类 Prometheus 告警规则，离线 YAML 解析与指标断言。运维权限、灰度/回滚/真实矩阵见 `INDEX_RECONCILIATION_RUNBOOK.md`。
- [x] 目标 Core/CLI/Persistence 检查、扩大 Tweet/MQ/Canal/ES/Qdrant 与五个入口测试/Vet及受影响包 Race 全部通过，最后完善后完整复验通过；含100ms阻塞AMQP握手、有界回执、计划漂移、部分执行与事务回滚。格式检查通过。
- [ ] 真实 Schema/迁移、ES/Qdrant/Embedding、Broker 路由/回执、进程重启、多实例、召回/容量和告警送达未验收；consistent 仅表示元数据一致，命名锁不防远端迟到写。M2-R3 完整容量继续 Partial。
- [ ] 下一代码轮次：M3-R2 Follow 关系事件原子提交，然后 M3-R3 持久传播；互动 ACK-before-async 继续归 M6。

验证结果与命令见 `ARCHITECTURE_REMEDIATION_PLAN.md` 的 M5-R3 交付记录；本轮未执行真实索引/MQ/业务写入、DDL、部署、付费模型或数据清理。

## 架构修复 M5-R2：持久索引删除与恢复边界（2026-10-13）

| 功能模块 | 轮次 | 状态 | 本轮交付 |
|---|---|---|---|
| M5 删除与索引生命周期 | R2 版本化派生投影 | Implemented（离线验证，当前事实重验） | 独立 delete_es/delete_vector 持久任务、创建当前事实/写后重验和协作命名锁；Relay FIFO事务位点/Confirm/恢复门禁 |

- [x] 删除消息成功 ACK 前登记两个唯一任务；部分失败使用 Confirm-before-ACK 重试补齐。任务独立领取/重试/完成，Owner/Token/Lease 围栏与耗尽保留沿用，移除 ACK 后无持久状态的异步 ES 删除。
- [x] `sync_es` 读取当前 MySQL Public 正文，已删/缺失/非 Public 只清理；创建/删除同 primary 命名锁串行化，写后重验。会话锁有界等待/释放，异常锁回执丢弃连接；不开网络调用期间的 SQL 长事务。
- [x] ES/Qdrant 有界删除回执校验；Vector 固定 tweets 规范 UUID 和字符串 tweet_id 别名，不动 Collection。缺依赖/未知404/坏回执不记成功，固定低敏失败码。
- [x] Canal 多行全部转发、坏行明确失败；事务提交边界排在前序事件之后，Broker Confirm 后存位点；取消不给未确认边界。未知路由使 Worker 明确退出，年龄 Outbox GC 停用；缺失/清理位点默认拒绝启动。
- [x] 定向与扩大 Go 回归/Vet、Consumer/Canal/Repository/ES/Qdrant Race、格式检查通过；Handler登记与ACK补测定向Race通过。迁移/回滚与真实矩阵见 `INDEX_LIFECYCLE_RUNBOOK.md`。
- [ ] 未执行真实索引删除/Embedding、DDL或部署；单primary命名锁不提供远端迟到写的服务端版本围栏。Broker Confirm也不证明路由/消费；真实断连、进程重启、多实例/容量仍待验收，不能宣称生产全链路保证。
- [ ] 下一轮 M5-R3：拆分文本/向量创建、只读对账和受控修复/发布感知保留；历史缺失事件/清理Binlog、Timeline旧粉丝上限、异步缓存清理继续待治理。M2-R3完整容量独立保持Partial。

## 架构修复 M5-R1：删除 Outbox（2026-10-13）

| 功能模块 | 轮次 | 状态 | 本轮交付 |
|---|---|---|---|
| M5 删除与索引生命周期 | R1 删除 Outbox | Implemented（离线验证） | 有效推文行锁/作者校验、软删除和兼容 TWEET_DELETED 同 UoW；错误传播、稳定事件键和提交后有界缓存失效 |

- [x] 新增 `TweetDeletionRepository`，事务中仅读删除身份并锁有效行；生产软删除使用 `ExtractTx`。缺失/已删维持 NotFound，读取失败保留错误，并发删除仅一次成功状态转移和事件。
- [x] 3 秒事务内鉴权/删除/Outbox；原直接 MQ 发布移除。旧 Payload/路由兼容，Outbox 主键和 `tweet-deleted:<id>` 唯一键固定事件身份；持久化/提交失败不返回伪成功。
- [x] 提交成功后本地 L1 失效、800 ms Redis 删除/广播；提交后请求取消不跳过尝试，缓存错误有低敏日志且不改报业务回滚。原 `InvalidateBaseTweet` 吞 DEL 错误已修复，见 ISSUES 235。
- [x] 生产 GORM UoW/仓储的内存 SQL 驱动回归、miniredis 缓存故障与提交后取消测试通过；Tweet/Canal/MQ/三个入口扩大测试、Vet 和 Service/Repository/Cache Race 全部通过。
- [ ] 真实 MySQL 锁/提交回执丢失、Relay 位点与消息重放、索引故障尚未验收。COMMIT 错误可表示结果不确定；事件持久入库不等于已投递。未执行真实删除/DDL/MQ/索引写入或部署；账号删除生产开关继续关闭。
- [ ] 下一轮 M5-R2：持久 ES/Qdrant 删除任务、创建重验/防复活、发布水位/补偿重放；M2-R3 完整容量独立保持 Partial。

## 架构修复 M2-R3：跨端与容量验收（2026-10-12）

| 功能模块 | 轮次 | 状态 | 本轮交付 |
|---|---|---|---|
| M2 Feed 完整性 | R3 跨端与容量验收 | Partial（跨端/离线与小数据集 Live SQL 通过，完整容量待验收） | Web/Mobile 字符串 ID 去重、请求换代和错误/空页继续状态；真实 HomeScreen Widget 测试；只读 MySQL/EXPLAIN 入口、实际查询与离线 Scanner 基线 |

- [x] Web Home 和 Mobile FeedNotifier 页面不直接重复追加，刷新/账号/页签换代丢弃旧响应及 finally；单代加载互斥，最多 3 个无新增页有界续拉，用户可继续；失败保留列表/失败游标精确重试，结束不再请求。
- [x] 首页推荐流 ListTweets HTTP Cursor 同步为字符串，保持现有 Proto 类型/编号；两端 ID/Cursor 不经过 Number；补大整数 Handler 回归。
- [x] Web 7 项状态测试/构建，Mobile 6 项状态+2 项 HomeScreen Widget+2 项契约测试/Analyze/格式化，Go 目标/扩大回归/Vet/Race/预算测试与格式检查全部通过。
- [x] 只读 `cmd/feed-read-acceptance` 显式 Live 门禁、只读事务、复用生产 SQL/关系校验/EXPLAIN，固定低敏输出；不执行 DDL/种子或 Redis写入。验收操作说明见 `FEED_ACCEPTANCE_RUNBOOK.md`。
- [x] 离线 Scanner 基线已执行并记录 CPU/Go/20x 数据、批次数/分配，明确 Fake/日志边界；不能外推成真实 DB QPS/百万 DAU，证据见 `FEED_ACCEPTANCE_2026-10-12.md`。
- [x] 用户授权启动后复用原数据卷启动 `twitter-clone-mysql-1`，3307 running/healthy；MySQL 8.0.46 现有 316 条推文/7 条有效关注，fixture 有 1 个关注对象/40 条候选。只读首页和第二页各返回 20 条，20 次样本 p95 为 5.910/5.997 ms；终页返回 0 仅作边界记录。EXPLAIN 与资源/索引证据见 `FEED_ACCEPTANCE_2026-10-12.md`，ISSUES 234 的依赖阻断已解除；没有 DDL/业务写入或修改 `.env`。
- [ ] 未覆盖高关注/长历史/稀疏大数据、部署浏览器/真机、多副本 Redis/端到端负载；同一事务重复 SQL 的小数据集耗时不代表完整容量达标，保持 Partial。
- [ ] 后续代码轮次按总表 M5-R1 删除 Outbox；M2-R3 的必需 Live 证据独立待收口，不将本模块标记 Accepted。

## 架构修复 M2-R2：Following Feed 恢复与关系校验（2026-10-12）

| 功能模块 | 轮次 | 状态 | 本轮交付 |
|---|---|---|---|
| M2 Feed 完整性 | R2 缓存重建与关系校验 | Implemented（离线验证） | 当前关系限定的有界 FollowingFeedRepository，批量关系重验与 Follows 可见性；普通 Inbox v2 空/非空标记、租约围栏及原子合并恢复；当前读取不再受旧关注/大 V 缓存截断 |

- [x] 当前共享 MySQL 的 follows 只读 EXISTS 子查询覆盖所有有效关注对象和自身记录；按精确 Tweet ID 分页，深历史和大 V 读取不依赖旧 IDs-only 100 条或作者缓存重建上限，不做在线全量关注枚举。
- [x] FollowReadRepository 每批最多 100 个作者重验当前关系，过滤取关和他人 Private/Shadowban，允许当前粉丝读取 Follows；授权读取失败返回明确错误，不用旧 Inbox 权限兜底。自身记录独立允许，语义见 API_REFERENCE。
- [x] 冷缓存恢复最新至多 1000 条权威候选，跨副本 3 秒租约、2 秒查询边界、Token 条件提交；原子合并保留并发 Fanout，不清空 Inbox。v2 初始化标记有效 5 分钟、Inbox 7 天，空 Feed 不重复重建，非空缓存丢失会再次恢复。Redis/投影失败不影响已成功读取的当前页，数据库/关系失败不伪装空页。
- [x] SQL DryRun、Service Fake 与 Redis 夹具覆盖 205 个关注作者/1205 条深历史、失效大 V 关联、取关发生在候选读取与校验之间、Follows/Private/Shadowban/自身授权、空缓存/丢失/重建失败、16 个并发冷请求单一 owner、租约过期/旧 Token/并发新 Fanout/TTL/非法输入/缺失 Port。
- [x] 目标五包、Tweet/Follow/MQ/Gateway/四个服务入口扩大回归、同范围 Vet、Tweet Service/Cache/Follow Repository Race、gofmt/diff check 通过；命令见修复计划。HTTP/Proto/生产客户端没有变更，未重复执行 Web/Mobile 构建。
- [x] 方案偏差：通过权威关系查询恢复大 V 有效关联，不在请求路径在线全量重建旧 Redis 关联集合；旧后台关联/晋升传播仍交 M3-R3。Redis 目前是派生投影，不宣称缓存命中优化；每页 MySQL 读取成本和共享表只读边界列入真实验收/M7。
- [ ] 真实 MySQL 索引/执行计划、Redis 多副本故障/容量和部署用户旅程未验收。未做 DDL、真实服务写入或数据清理；下一轮 M2-R3 跨端与容量验收。

## 架构修复 M2-R1：Following Feed 稳定分页（2026-10-12）

| 功能模块 | 轮次 | 状态 | 本轮交付 |
|---|---|---|---|
| M2 Feed 完整性 | R1 稳定分页 | Implemented（离线验证） | 普通/大 V 候选 limit+1、ID 倒序归并去重、消费候选 Cursor、有界过滤补齐、Redis 同分数精确读取、HTTP 字符串游标与批量资料 |

- [x] 每批消费最多 100、单请求最多扫描 1000，空过滤页达到上限仍返回推进后的游标；前瞻/未消费候选留下一页，结束游标为 0。
- [x] 候选正文使用当前 MySQL 批量读取，过滤删除和他人非公共记录，不返回旧 L1/L2 正文；Feed 旧缓存夹具新增两次精确批量读取并验证当前正文覆盖旧正文。
- [x] Redis 既有 key/float 分数写入格式保留；有界原子脚本展开游标与末端同分数桶，按 uint64 精确排序过滤，覆盖 2^53 以上、跨十进制位数与 uint64 上界；非法成员明确降级/失败。
- [x] 拉模式内部保留第 101 条，公开最大 100；缓存来源失败从原请求游标整体切换 pull；正文失败返回错误，不假装空页；不再从 Feed 主路径启动旧下一页预热 goroutine。
- [x] Gateway 严格解析 Cursor/Limit，认证用户传可信上下文、批量公开资料；HTTP `next_cursor` 改为字符串，Proto 字段不变，Web/Mobile 已原样透传；新增 HTTP/SQL/gRPC/移动端数据层契约测试。
- [x] 扩大 Tweet/MQ/Gateway/相关入口测试、Vet、Tweet Service/Cache/Gateway Handler 全包 Race、Web 构建、Dart 格式化、Mobile 两项契约 Test 与定向 Analyze 全部通过；Analyze 发现旧未使用导入后仅移除单行并复验，无告警。完整命令见修复计划，首轮沙箱/旧测试契约失败与处理见 ISSUES 233。
- [ ] 下一轮：M2-R2 缓存重建与关系校验。
- [ ] 真实 MySQL/Redis 查询与部署旅程/压测未验收；全关注事实重验、普通空缓存/大 V 完整历史恢复与 Follows 权限支持留 M2-R2，跨端去重/加载/空页用户交互及容量留 M2-R3。此轮仅保证已取候选范围的分页与当前正文事实。

## 架构修复 M4-R1：私信历史分页（2026-10-12）

| 功能模块 | 轮次 | 状态 | 本轮交付 |
|---|---|---|---|
| M4 私信历史与投递 | R1 历史分页 | Implemented（离线验证） | 消息 ID 排他 Cursor、会话首次 ID 上界与分组后 Cursor、limit+1、默认 20/最大 100、页内幂等已读；Gateway 批量公开资料和固定低敏错误 |

- [x] 追加 ConversationPageRepository/MessagePageReadRepository Port，当前 MySQL Repository 与 Service 接线；Proto/HTTP 字段不变，Web/Mobile 数据层保持字符串 Cursor。
- [x] 消息按 ID 倒序而非单独时间排序；会话先按首次上界分组，再应用已交付的最新消息 ID 下界，避免同一会话跨页重复。上界不是跨请求事务快照，不覆盖低 ID 延迟提交保证。
- [x] 仅同步标记本页交付给接收者的消息 ID；未获取历史、前瞻行和新消息保留未读。更新失败明确返回错误，去除旧异步全会话标记。该边界从 M4-R2 前移以保证分页语义。
- [x] 补 SQL DryRun 与 Service/Gateway Fake 测试：同毫秒、大整数、100/101 条、并发新增、跨用户/非法游标、取消、坏页、空页、失败与身份限制；新增 Mobile 数据层契约测试源码。
- [x] 新增独立索引迁移 SQL；本轮未执行 DDL、启动容器或改写历史数据。批量读取替代逐会话/资料 N+1，但大历史会话分组仍需独立投影与真实性能验收。
- [x] Messenger 全模块测试/Race、新增 Gateway HTTP 分页测试及其定向 Race、Messenger/Gateway/入口 Vet、Gateway/两个命令入口编译通过；本轮 Go 文件 gofmt 与 diff check 通过。
- [x] 用户要求重新提交审批后，获准执行完整 Messenger/Gateway/两个入口回归、Gateway Handler 全包 Race、Web 构建、Dart 格式化、Mobile 3 项定向契约测试与定向 Analyze，全部通过。首轮环境/审批失败已按真实过程记录并关闭本轮阻断，见 ISSUES 232。
- [ ] 真实 MySQL 执行计划、索引部署、延迟提交及多端用户翻页旅程未验收；M4-R2 负责分页 UI/去重/重连，Redis Pub/Sub 未变成持久投递。
- [ ] 下一轮：M2-R1 Feed 稳定分页；本轮已完成离线门禁，真实部署验收保持独立。

## 架构修复 M3-R1：Follow Cursor 契约（2026-10-12）

| 功能模块 | 轮次 | 状态 | 本轮交付 |
|---|---|---|---|
| M3 Follow 关系与派生任务 | R1 Cursor 契约 | Implemented（离线验证） | 公开粉丝/关注列表使用 FollowerPage/FolloweePage，游标来自关注关系 ID，仓储 limit+1 前瞻不被公开 100 条上限截断；Gateway 保留字符串大整数、空数组和明确参数/失败码 |

- [x] 新增 FolloweePage Port；复用既有 FollowerPage Port，未修改旧 FollowRepository 接口或 Proto 字段编号。
- [x] 测试覆盖用户 ID/关系 ID 不同序、100/101 条、空页/结束页、软删除、翻页间新增关系、非法/重复参数、取消、缺失分页 Port、坏页与大整数 HTTP 往返；SQL DryRun 验证实际 GORM 查询生成，不连接 MySQL。
- [x] Follow/Gateway Handler 目标测试；Tweet/MQ/Gateway/Follow-Tweet-Consumer-Gateway 命令入口扩大回归；对应 Vet、Follow/Gateway Handler Race、Web `npm run build` 和格式检查通过。
- [x] Web 已原样回传字符串 Cursor，无字段或页面代码适配；Mobile 当前没有关注/粉丝列表分页入口，未运行 Flutter 验证。旧版本在途页需从首页重新拉取。
- [ ] 真实 MySQL 查询执行/索引计划、实际服务部署与用户翻页旅程未验收。大 V 传播/对账与 Feed 降级仍有旧 IDs-only 上限，留在 M3-R3/M2，不能将本轮描述为 Hybrid Fanout 全量修复。
- [ ] 下一轮：M4-R1 私信历史分页。其余状态见 `docs/ARCHITECTURE_REMEDIATION_PLAN.md` 总表。

## 架构修复 M1：公共搜索可见性（2026-10-12）

开发主线按 `docs/ARCHITECTURE_REMEDIATION_PLAN.md` 的 **8 个功能模块、21 个轮次**执行；依据当前架构评审 F1—F10，不改变原生产上线计划的真实验收要求。

| 功能模块 | 轮次 | 状态 | 本轮交付 |
|---|---|---|---|
| M1 搜索可见性与可信上下文 | R1 权威公共读取 | Implemented（离线验证） | 追加 GetPublicTweets RPC，直接批量读取当前 MySQL 事实，过滤删除、隐藏和非公共记录，1—100 输入项有界/去重/顺序返回 |
| M1 | R2 MCP/RAG 接入 | Implemented（离线验证） | 共用 retrieval Adapter；当前正文校验发生在 Reranker/Evidence/认知上下文之前，取消旧索引正文兜底，生产组合根注入 |
| M1 | R3 回归与验收 | Implemented（离线验证） | 目标五包、Agent/Gateway/相关 cmd 扩大回归、Vet、MCP/RAG/Reader Race 与差异格式检查通过 |

- [x] Fake gRPC/内存 HTTP 测试覆盖旧正文替换、删除/Shadowban/私密/粉丝可见过滤、全部候选剔除、权威服务失败、非法/重复/越界响应、批量边界和取消。
- [x] Tweet Proto 经 protoc 生成，Gateway Mock 同步新增方法；既有 HTTP/Web/Mobile 字段未变，无客户端代码修改。
- [x] 首轮 Windows 沙箱工具/socket 失败已记录 ISSUES 230；自动审查允许的执行环境下复验及扩大回归通过，未修改旧缓存测试或业务路径以掩盖失败。
- [ ] 先部署新 TweetService，再部署 Agent，并执行真实删除/治理后的 ES/Qdrant/MCP/认知上下文验收；本轮未启动容器、调用真实模型或修改业务数据。
- [ ] M1 交付时 M2—M8 为 Planned；后续状态以修复计划轮次总表与最新交付记录为准。索引删除/旧创建复活、历史作者/详情工具、其他列表可见性仍按后续轮次收口，M1 不是全站授权审计完成。

## 全站只读客服本机集成（2026-10-02）

- [x] 根 App.vue 注册右下角可收缩小栖客服，任意路由共享同一懒加载 iframe；收缩/推文路由跳转保留会话与草稿。
- [x] 复用既有账号；精确 Origin/source 的消息桥只传内存身份，退出/切换清空旧会话和迟到回复。未重构原站 LocalStorage 认证；移除 Login 的令牌/个人资料调试输出。
- [x] 查询引用由 Python 校验业务记录生成，前后端限制数字推文路径，详情接口继续鉴权；演示数据不生成链接。
- [x] 6 篇用户指南在独立客服知识库发布并验证真实召回，覆盖 Workflow 与社交操作，不扩展原Go Agent权限。窗口4状态/7错误反馈与身份隔离夹具通过；Vue消息桥普通字段快照修正已同步。
- [x] Web 类型/构建与本机镜像通过。隔离 Live 浏览器：真实登录、推文查询与引用幂等回放、真实详情页、收缩保留、Workflow 已发布指南问答、手机布局、匿名/跨账号拒绝通过（2 用户、1 推文、2 次模型问答，数据保留）。
- [ ] 当前为本机 iframe 5174 集成，仍需助手前端进程；生产域名/TLS/反代未验收。此测试不等于 Go Agent、用户 Workflow、MCP、Temporal 或生产故障旅程通过。

## 本机演示验收预检（2026-09-26）

- [x] 后续受控切换：从当前源码重新构建 Agent/Gateway 镜像，Web `npm run build` 后用 `web/Dockerfile.offline` 离线打包前端。旧镜像已保留 `rollback-20260926` 标签；三服务依次重建，运行镜像 ID 与 `20260926-2` 候选一致，容器重启次数为 0。
- [x] 切换后 Agent 容器内 `/metrics` 可达、Gateway `/health` 为 HTTP 200、前端 `/` 与 `/agent` 为 HTTP 200。Gateway Mock 新增 Tweet RPC 方法后定向测试通过；这些只证明构建、启动和基础可达，不是认证后的 Agent E2E 或 L3/L5 Live 验收。
- [x] 获得额外许可后，Tweet 服务亦从当前源码重建并单独切换至 `20260926-2`，旧镜像保存为 `rollback-20260926`；容器镜像 ID 匹配、重启次数为 0，Consul 有 1 个 passing 实例，Gateway 健康接口仍为 HTTP 200。
- [ ] Agent 的 Planning-Execute、Recoverable Runs 与 External MCP 仍默认关闭；新增 Tweet 内部 RPC 尚未执行部署态调用，真实模型、MCP、工作流审批恢复和发推旅程也未执行，不得将本机演示标记为完整通过。
- [x] 只读检查：当前 Compose 中 Agent、Gateway、Frontend 及基础依赖均处于 running；本机 Web `/agent` 与 Gateway `/health` 返回 HTTP 200。此结果只证明旧部署可达，不证明当前源码的功能旅程。
- [x] 当前源码的 `helm lint`、staging `helm template`、Web `npm run build`、L3/L4 Runbook 指定的四个 Go 包离线测试，以及 26 项上线证据记录器自测通过。
- [ ] 运行中的 Agent/Gateway/Frontend 镜像创建于 2026-08-12，且没有源码挂载；当前多批工作区修改没有进入容器。Agent 容器中 Recoverable Runs 为 false、External MCP 为 false、Planning-Execute 主开关未配置（默认关闭），Workflow-as-Tool 为 true。不得把这套旧容器的只读预检登记为当前源码的 L3/L5 Live 验收。
- [ ] 在受控窗口构建并部署精确源码版本，记录镜像 digest、备份与回滚点后，按 `docs/agent_launch_runbook.md` 用明确授权的测试账户执行真实模型/MCP/审批/发推旅程；需要真实外部调用与发布授权。生产删除仍保持关闭。

## 用户删除 Agent 工作流状态转换写入计数（2026-09-26）

- [x] 补偿领取、完成、挂起、失败、拒绝，以及工作流运行取消和最终状态提交，在 Mongo 写入前获取用户主体计数。
- [x] 仅有补偿记录 ID 的三个入口先只读确认归属，更新条件绑定同一用户；明确未匹配且未写入的冲突释放计数，网络回执不确定仍保留阻断。
- [x] 缺失身份边界及“确定未写入/不确定写入”的计数语义有离线回归测试。
- [x] 本批验证：仓储与 Service 定向测试、Agent 全包及服务入口离线测试、Agent `go vet`、仓储工作流/围栏目标 `go test -race` 通过；未执行真实 Mongo 集成/故障测试。
- [ ] 其他 Agent 直接仓储和后台写路径、正计数只读对账/受控恢复、真实 Mongo 并发与故障演练仍未完成；生产删除继续关闭。

## 用户删除 Agent 工作流派生写入计数增量（2026-09-26）

- [x] 工作流状态事件、周期快照、补偿计划和运行状态游标在 Mongo 写入前登记同一用户的 `active_writes`；确认成功后释放，结果不确定时保留阻断。
- [x] 事件和补偿计划批量写入拒绝混合用户或缺失身份；保持原有幂等 Upsert、版本游标与冲突错误语义。
- [x] 本批验证：仓储定向测试、Agent 全包及服务入口离线测试、Agent `go vet`、仓储工作流/围栏目标 `go test -race` 通过；未运行真实 Mongo 集成测试。
- [ ] 补偿领取/完成/挂起/失败/拒绝、运行取消/最终提交，以及其他 Agent 直接仓储与后台写路径仍未全覆盖。错误后正计数缺少受控核对/恢复，真实 Mongo 并发和故障演练未完成；生产删除继续关闭。

## 用户删除 Agent 工作流写入计数增量（2026-09-26）

- [x] 工作流定义创建/更新与运行记录创建/更新在仓储写入前登记主体计数；旧工作流首次读取时的惰性版本补建也登记。
- [x] 四个基础写入口拒绝缺失用户归属的记录；成功确认后释放计数，结果不确定时保留计数以阻断删除。
- [x] 本批验证：Agent 全包与服务入口离线测试、Agent `go vet`、仓储工作流/围栏目标 `go test -race` 均通过；未运行真实 Mongo 集成测试。
- [ ] 工作流事件、快照、补偿记录、运行控制及其他 Agent 直接仓储/后台写入口未全部覆盖；正计数的只读对账与受控恢复、真实 Mongo 并发/故障注入和 staging 验收仍缺失。生产删除继续关闭。

## 用户删除 Agent 对话写入计数（2026-09-26）

- [x] Agent Mongo 用户主体新增原子 `active_writes` 计数；未围栏时获取写入资格，围栏安装只允许零进行中写入。无需 Mongo 事务，兼容当前单节点开发环境。
- [x] `CreateDialogue`、`SaveMessage`、`SaveMessages` 在真实写入前登记，同一批消息校验用户归属。成功确认后释放；Mongo 写入结果不确定时保留计数，禁止删除抢跑。
- [x] 离线测试覆盖写入期间拒绝安装围栏、非法负计数、取消后的成功释放、失败写入保留阻断，以及冻结用户不能启动写入。
- [x] 本批验证：`go test ./internal/module/agent/... ./cmd/agent-service -count=1`、`go vet ./internal/module/agent/... ./cmd/agent-service`、Repository 围栏目标 `go test -race` 与 `git diff --check` 通过；未执行真实 Mongo 故障演练。
- [ ] 其他 Agent 持久化路径尚未全部纳入计数；崩溃或结果不确定留下的计数需要专门的只读核对与受控恢复协议。生产删除仍关闭；真实 Mongo 并发与故障注入验收尚未完成。

## 用户删除跨服务写围栏增量（2026-09-26）

- [x] 新增未注册的 Agent Mongo + Tweet MySQL 复合围栏适配器；先取得 Agent 收据，再调用 Tweet 内部安装 RPC，形成绑定两项权威摘要的 v2 回执。部分失败保留已安装围栏，可按同一请求重试。
- [x] Agent Mongo 仓储向 Service 提供统一写授权；对话、Run、Workflow、Provider、MCP、模板、审批、项目及部分后台回写入口接入检查。项目创建、成员添加和补偿重试补齐；冻结目标用户不能被重新加入项目。
- [x] 复合回执拒绝重复/非法权威、未来时间和错误 Tweet 来源；覆盖取消执行、部分失败及入口阻断测试。Agent 全包离线回归通过。
- [ ] 生产删除继续关闭：Agent 目前是写前预检查，未与 Mongo 写入事务/租约串行化；直接仓储与后台写路径也未全部覆盖。生产 Registry、Worker 调度、用户 API、跨存储故障恢复和 staging 验收仍未接入。不得把本轮回执视作删除安全的充分证据。

## 用户删除可恢复协议（2026-09-25）

- [x] 完成固定八阶段 Plan、确定性 Inventory/Plan Digest、Revision Checkpoint、严格依赖、租约接管和 Receipt/Failure 约束。
- [x] 预检成功持久化协议初始快照；Legal Hold、Subject Busy 或清单失败不生成可误用执行计划。
- [x] 覆盖阶段篡改、超长租约、租约过期、失败重试和未知外部存储的 fail-closed 测试。
- [x] 完成阶段级 Mongo 双 Revision CAS、Plan Digest 绑定和控制面 Claim/Complete Coordinator；复用 Retention Subject Lease 阻断 Legal Hold/并发执行，并阻止预检或取消覆盖活动阶段。
- [x] 完成不可变 Capability Registry/Snapshot：适配器身份与版本参与稳定摘要，Plan 绑定快照摘要，Claim/Complete 在获取执行租约前按当前注册表复验，部署能力漂移时 fail-closed。
- [x] 完成有界追加审计：Claim/Success/Failure 事件以序号、前序摘要、Plan/Capability Digest 和双 Revision 形成哈希链，并与 Checkpoint 在同一 Mongo CAS 中原子追加；执行开始后禁止重新预检覆盖审计。
- [x] 离线验证：删除领域/服务/仓储目标测试、Agent 全包回归、三个目标包 Race/Vet 与差异格式检查通过；未启动服务、容器或数据库，未访问公网、模型或付费 API。
- [x] 完成存储无关的破坏性 Adapter v1 契约：稳定 Operation Key、短期 Legal Hold 决策、用户身份摘要、固定 Scope、幂等 Receipt 回放、独立 Post-state 读回与零残留验证均可复用；合规夹具覆盖单次变更、同键回放、Hold/过期拒绝、能力/Scope 漂移和伪造证据。
- [x] 完成未注册的 Mongo owned-data Adapter 边界：预检与执行共用 18 类独占集合清单，项目级 MCP/共享关系和治理记录被排除；事务内重验 Legal Hold Revision/Evidence、删除、逐集合零残留计数并持久化不可变幂等回执。未接 Worker、CLI 或生产注册表。
- [x] 完成未注册的共享关系 Adapter：同一 Mongo 事务内重验治理与所有权，只解绑非 Owner 项目成员、个人 MCP Connection/Snapshot 与 Profile Role Binding，并持久化零残留幂等回执；项目 Owner、项目级 MCP 创建者和 Marketplace Publisher Owner 必须先显式转移。
- [x] 完成未注册的可恢复用户删除 Worker：严格按八阶段 Claim/Complete 租约执行，阶段失败写入固定失败码并只重试失败阶段；每个破坏性 Adapter 调用前重新签发 Legal Hold 治理决策，删除后立即读回并校验结构化 Receipt/Post-state。
- [x] 完成跨存储验证与最终确认：五个权威 Adapter 再观察必须全部零残留；最终收据绑定 Plan、Capability Snapshot、Checkpoint Revision 与前七阶段回执摘要，最后一次 Checkpoint/审计 CAS 同时把请求原子置为 completed。
- [x] 完成未注册的 Agent Mongo Fence Authority：Fence Identity 持久化在 retention subject，首次安装使用 Legal Hold Revision CAS；同请求可携新的短期治理凭据重入，竞争请求、治理漂移和损坏记录均 fail-closed，并提供 `AuthorizeSubjectWrite` 写授权契约。
- [ ] 生产仍不可执行：Agent Service 仅有主要入口的写前授权，未覆盖全部写路径或关闭写入竞态；Worker 未接后台调度/CLI/组合根，用户 API、所有权转移、真实 Mongo 副本集与 MinIO/Qdrant/Tweet 故障恢复、大用户批次及 staging Live Evidence 仍待完成。

## 上线收口阶段（2026-09-25）

> 唯一上线计划：`docs/PRODUCTION_LAUNCH_COMPLETION_PLAN.md`

- [x] 完成生产调用链真实性审计：确认短计划/VerifiedRunner 尚未进入 `RunAgent` 主链，MCP/Workflow 能力默认关闭，不能按“已上线”口径描述。
- [x] 将会话语义摘要与长期记忆统一接入 Chat、站内/公网检索、External MCP、Workflow 与 Skill Runtime。
- [x] 增加 `AGENT_SESSION_SUMMARY_MODEL`、`AGENT_SESSION_SUMMARY_MIN_MESSAGES`、`AGENT_SESSION_SUMMARY_IDLE_DELAY` 部署配置。
- [x] Agent Service 与组合根离线定向测试通过。
- [x] 完成语义摘要低基数指标、Mongo 到期调度与租约 Worker，并以事实/偏好/决定/纠正/注入残留五类固定样本验证长会话预算边界。
- [x] 完成 Planning-Execute 第一增量：站内搜索以稳定用户灰度进入默认关闭的短期模型规划、确定性准入和 VerifiedRunner 主链，并持久化低敏计划/用量/验证证据。
- [x] 补齐 Planning-Execute 低基数指标、脱敏 Run 查看与加密 Verified Checkpoint 人工澄清恢复；恢复时重新求交当前授权工具并验证现实证据。
- [x] 完成 Web Research 第二个只读迁移：独立默认关闭灰度、搜索到页面双证据、缺页阻断、低基数指标和人工澄清后的重新授权恢复。
- [x] 已按 MCP、Workflow、审批写入顺序完成 Planning-Execute 离线迁移，并逐项覆盖撤权、恢复和回滚；下一步进入真实 MCP/Workflow 与部署态验收。
- [x] 完成 Agent 应用数据保留 Worker：覆盖对话、执行 Trace、Workflow/Agent Checkpoint 与 Tool Result，具备 Mongo 跨实例租约、逐候选 Claim、有界批次/超时、默认关闭与 Dry-run、对象先删后清引用和低基数指标。
- [x] 将 retention 参数接入本地 Compose、生产覆盖、Helm 与环境变量示例，并补充分阶段启用和回滚手册。
- [x] 增加用户级清理 Guard、Legal Hold 互斥与默认预览 CLI；增加对话孤儿消息扫描、24 小时宽限和单条 Claim 回收，避免依赖 Mongo 事务。
- [x] 增加私有 MinIO Tool Result 孤儿对象只读审计：有界 Cursor、年龄宽限、Mongo 引用逐项复核、键摘要报告和查询失败 fail-closed；因旧对象键无法安全复用用户级 Legal Hold Guard，暂不提供删除模式。
- [ ] 在 staging 审核至少两个 Dry-run 周期，演练 Hold/清理竞争、真实删除和恢复，并完成用户删除流程与 MinIO 已孤立对象生命周期后，才允许开启破坏性删除。
- [ ] 完成 External MCP 与 Workflow-as-Tool 的真实部署验收、撤权和审批恢复演练。
- [ ] 完成生产部署覆盖、Secrets、TLS、健康检查、备份恢复、监控告警、数据保留执行验收与灰度回滚。
- [ ] 完成 staging E2E、故障注入、修复和正式上线。

## 📋 项目概述

基于 Go 语言的 Twitter 仿真微服务系统，采用云原生架构部署在 Kubernetes (Minikube) 上。

| 维度 | 详情 |
|------|------|
| **语言** | Go 1.25 |
| **通信** | gRPC (服务间) + REST/Gin (对外 API) |
| **容器** | Docker + Minikube (K8s) |
| **部署** | Helm Chart + ArgoCD (GitOps) |
| **CI/CD** | GitHub Actions → Docker Hub → ArgoCD 自动同步 |

---

## 🏗 架构总览

```
┌─────────────────────────────────────────────────────────┐
│                    Kubernetes Cluster                     │
│                                                          │
│  ┌──────────┐   gRPC    ┌──────────────┐                │
│  │ Gateway   │──────────│ User Service │                │
│  │ (BFF+API) │──────────│ :9091        │                │
│  │ :9638     │  gRPC    ├──────────────┤                │
│  │           │──────────│ Tweet Service│                │
│  │ Gin/REST  │──────────│ :9092        │                │
│  │ JWT Auth  │  gRPC    ├──────────────┤                │
│  │ Sentinel  │──────────│ Follow Svc   │                │
│  │ OTel      │          │ :9093        │                │
│  └──────────┘          └──────────────┘                │
│       │                       │                         │
│       │                       ▼                         │
│  ┌─────────┐   ┌───────┐ ┌───────┐ ┌──────────┐       │
│  │ Ingress │   │ MySQL │ │ Redis │ │ RabbitMQ │       │
│  │ (nginx) │   │ :3306 │ │ :6379 │ │ :5672    │       │
│  └─────────┘   └───────┘ └───────┘ └────┬─────┘       │
│                                          │              │
│                                    ┌─────▼────┐        │
│                                    │ Consumer  │        │
│                                    │ (异步消费) │        │
│                                    └──────────┘        │
│                                                         │
│  ┌──────────────── 可观测性 ──────────────────┐         │
│  │ Prometheus │ Jaeger │ Consul │ Grafana     │         │
│  └────────────────────────────────────────────┘         │
└─────────────────────────────────────────────────────────┘
```

---

## ✅ 已完成项目

### 阶段 1：核心微服务开发
- [x] **User Service** — 用户注册、登录、JWT 认证、资料管理
- [x] **Tweet Service** — 推文 CRUD、用户时间线、关注 Feed 流
- [x] **Follow Service** — 关注/取关、粉丝列表、关注统计
- [x] **Consumer** — RabbitMQ 异步消息消费（事件驱动）
- [x] **Gateway (BFF)** — API 聚合网关，含 `/full_profile` BFF 端点

### 阶段 2：服务治理
- [x] **gRPC 服务间通信** — 3 个 proto 定义，16 个 RPC 方法
- [x] **Consul 服务发现** — 自动注册与发现
- [x] **Sentinel 熔断降级** — 错误比率 & 慢请求熔断
- [x] **JWT 认证中间件** — 基于 Gin 的 Bearer Token 认证
- [x] **Redis 分布式限流** — 1000 req/min per IP

### 阶段 3：Kubernetes 部署
- [x] **Dockerfile** — 5 个微服务的多阶段构建
- [x] **Helm Chart** — 完整的参数化部署模板
- [x] **Ingress (Nginx)** — 对外暴露 API
- [x] **HPA 自动弹性伸缩** — CPU 80% 触发，1-3 副本
- [x] **资源配额** — requests/limits 设定

### 阶段 4：CI/CD & GitOps
- [x] **GitHub Actions** — 自动测试 → 构建镜像 → 推送 Docker Hub → 更新 Helm values
- [x] **ArgoCD** — 监听 Git 仓库，自动同步集群状态（self-heal + auto-prune）

### 阶段 5：可观测性（已完成）
- [x] **Prometheus** — 指标采集 + `/metrics` 端点
- [x] **Jaeger** — OpenTelemetry 分布式链路追踪（已修复 API Gateway 层 OTel Trace 级联穿透截断问题）
- [x] **kube-state-metrics** — K8s 集群指标
- [x] **Grafana** — 数据可视化面板 (Provisioning Dashboard)
- [x] **PLG Stack (Loki + Promtail)** — 分布式日志与 Trace 级联跳转

### 阶段 6：业务功能扩展
- [x] **Like System** — 点赞推文 (Redis 缓存 + 异步持久化)
- [x] **Comment System** — 二级评论、评论计数
- [x] **Notification System** — 互动通知 (RabbitMQ + WebSocket 实时推送)
- [x] **Simple Search** — 简易推文搜索 (MySQL Like / FullText)
- [x] **Trending Topics** — 热门话题排行榜 (Redis Sorted Set + 异步计算)
- [x] **User Profile** — 用户资料完善 (头像/简介)
- [x] **Media Upload** — 媒体上传服务 (Local Storage)
- [x] **Retweet System** — 转发推文 (gateway 直连 DB + 前端切换)
- [x] **Profile Tabs** — 个人资料页 Tabs (帖子/回复/媒体/喜欢)
- [x] **Messenger System** — 私信系统 (gRPC + WebSocket 实时聊天)

### 阶段 7：AI Agent Service
- [x] **Agent 核心能力** — 4 种 AI 模式（直接对话 / RAG 搜索 / AI 写推文 / 多 Agent 协作）
- [x] **MCP Server** — 5 个 Tool（语义搜索 / 混合搜索 / 发推 / 获取用户推文 / 批量获取推文）
- [x] **对话历史持久化** — MongoDB 存储对话会话和消息，支持多轮上下文（Phase 1 ✅）
- [x] **模型信息 + 文件解析** — GetModelDetailedInformation / AnalysisFiles（Phase 2 ✅）
- [x] **MCP Tools 模块化重构** — 从 server.go 拆分到 tools 包（Phase 3 ✅）
- [x] **Gateway 路由补全** — 对话列表/详情/模型信息/文件解析等（Phase 4 ✅）
- [x] **前端 AI Agent 页面** — Vue 3 交互式 Agent 会话页面（Phase 5 ✅）
- [x] **MCP Tool 鉴权隔离与 SSE 连接池** — 废除 LLM user_id 权限越权漏洞，基于 RWMutex 和 DCL 实现长连接复用与异常自愈（Phase 6 ✅）
- [x] **RAG 语义二次精排 (Reranker)** — 引入 Rerank 漏斗检索机制，支持阿里百炼、硅基流动并实现基于 Bigram Jaccard 的 LocalMock 与 1.5s 超时优雅降级，降低 Context 噪声并杜绝幻觉（P2 ✅）
- [x] **MCP 长连接与 RAG 检索加固** — 引入服务生命周期 context 控制及 Close() 机制规避协程泄露，实现 0.0.0.0 容器拨号纠偏，以及首选 Qdrant 失败时优雅降级为 ES 检索及兜底文本，补齐中文种子数据（✅）
- [x] **可视化拖拽工作流编辑器前端页面** — 基于 Vue Flow 完成组件拖拽、端点自适应连线、属性配置变量树感知与安全删除交互闭环（✅）


### 阶段 8：分布式日志系统 (PLG Stack)
- [x] **Loki & Promtail 部署** — 本地一键拉起 Loki 与 Promtail 服务，通过 Docker SD 实现容器日志自动收集与 Pipeline 解包
- [x] **Grafana 预置配置** — 基于 provisioning 预置 Loki/Jaeger/Prometheus 数据源，简化配库流程
- [x] **Logs ➔ Traces 级联跳转** — 配置 derivedFields 提取日志中的 trace_id，生成一键跳转至 Jaeger 的超链接
- [x] **Go 结构化日志与协程 Trace 延续** — 重构 follow-service 并使用 OTel Span 传递在异步拉取协程中传递 Trace 上下文，修复 Trace 截断漏洞

### 阶段 9：消息队列韧性治理 (Resilience Governance)
- [x] **基础库参数扩展** — 在 `rabbitmq.go` 中引入 `DeclareQueueWithArgs`，完美兼容现有功能，支持自定义控制参数声明队列。
- [x] **指数退避延迟重试** — 废除业务消费端 `msg.Nack(false, true)` 的即时重入队，引入 `retry.events` 交换机与带有单条消息 Expiration 的重试暂存队列，实现指数级退避重试（1s, 2s, 4s...），解除 100% CPU 重试风暴对消息通道的阻塞。
- [x] **死信队列分流归档** — 限制核心队列消息重试次数上限为 3 次，超限后消息自动分流到专用死信队列 `*.dlq` 中，实现非阻塞的错误隔离，彻底杜绝数据静默丢失（Silent Loss）。

### 阶段 10：微服务数据强隔离与 API Gateway 重构 (DB Isolation)
- [x] **API Gateway 去数据库化** — 彻底下架 GORM 初始化、数据库连接及表自动迁移，Gateway 转型为纯粹的 gRPC BFF 代理。
- [x] **Notification 业务下沉** — 扩展 `notification-service` 的 gRPC 能力，将通知列表、标记已读及未读数等业务全部移入服务内层。
- [x] **Bookmark / Retweet 子域收口** — 书签与转发的数据库操作全面归宿至 `tweet-service`，在服务底层批量聚合点赞/书签/转发等交互状态，杜绝 BFF 二阶段 SQL 拼装。
- [x] **时间线合并下沉** — 推文与转发推文的混合分页排序逻辑由 `tweet-service` 统一计算返回，Gateway 仅负责用户信息的最终 Enrich 聚合。

### 阶段 11：大V推特混合 Feed 流与双阈值防抖架构 (Hybrid Push/Pull)
- [x] **Redis 缓存大V列表** — 引入 `global:celebrities` 与 `user:celebrities:ID` 缓存，彻底干掉大V读写链路上的 SQL 聚合计算，消除了百万粉丝高频拉取导致的数据库 CPU 100% 雪崩隐患。
- [x] **双阈值防抖门限** — 引入 5000 粉丝晋升 / 4500 粉丝降级的非对称滞后带宽防抖，杜绝粉丝在阈值点边缘反复关注/取关引起的状态频繁更新抖动。
- [x] **晋升与降级异步广播** — 粉丝数越过阈值时通过 Redis Pipeline 批量异步更新/清理老粉丝的大V关注缓存；同时消除普通用户写扩散原本硬编码限 1000 活跃粉丝的推送截断 Bug。
- [x] **定期校准对账任务** — 编写定时对账任务 `SyncGlobalCelebrities`，在系统空闲期通过差分对齐 DB 与 Redis 大V及关联关系状态，确保数据最终一致性。
- [x] **大V状态 L1 本地二级缓存化** — 在 Go 进程内存中集成了带 TTL（1分钟）和并发锁的 `celebrityLocalCache`，高频大V状态查询优先“在本地内存解决战斗”，Redis 查询 QPS 降低数个数量级。

### 阶段 12：ES 向量同步事务发件箱模式 (Transactional Outbox)
- [x] **发件箱表与 GORM 仓储实现** — 引入 `outbox_tasks` 任务关系表与 `CASE WHEN` 指数退避；当前多副本领取明确依赖 MySQL 8 `FOR UPDATE SKIP LOCKED`，不再宣称 Claim 路径兼容 SQLite。
- [x] **写扩散与 ES 向量化同步解耦** — 重构 Timeline 消费端发帖消费，将原本的同步调大模型 Embedding 接口改为持久化写入发件箱任务，消费端平均延迟降低至微秒级，提高队列吞吐量。
- [x] **后台守护对账协程 (OutboxWorker)** — 实现后台轮询守护协程，以原子 Claim/Lease 并发执行向量化与 ES/Qdrant 写入，旧 Attempt 不能覆盖新租约结果。
- [x] **指数级退避与有界收据清理** — 失败任务按指数延迟重试，达到 5 次上限后封存用于人工审计；成功任务作为去重收据保留 72 小时，再按有界批次物理清理。

### 阶段 13：云原生生产级深度治理 (Cloud-Native Production Governance)
- [x] **Redis ZSet Trending 热 Key 优化** — 引入本地 HashtagBatcher 并发缓冲与异步 pipeline 定时落盘，优雅退避单点 CPU 雪崩。
- [x] **AlertManager 黄金指标告警与 Webhook 推送** — 配置 QPS 骤跌、高延迟、高错误率三大告警规则，并在 API Gateway 注入带安全 Header 校验的 `/alerts` 告警接收端。
- [x] **Service Mesh (Istio) 流量管理配置** — 新增 VirtualService 与 DestinationRule，支持 v1/v2 子集 90:10 灰度发布流量切分及基于 OutlierDetection 的生产级熔断防护。
- [x] **网络边界安全防护 (NetworkPolicy)** — 拆分 MySQL、Redis、RabbitMQ 专用网络白名单，并在服务侧 NetworkPolicy 兼容 Istio 控制面、双向 TLS 与 Envoy 拦截端口，严格实行最小权限容器隔离。
- [x] **Grafana Dashboard 看板预置与抓取加固** — 开启 Grafana Sidecar 动态加载 ConfigMap，并在 Deployment 模板注入 Prometheus Scrape 注解以修复抓取盲区，同时将 gRPC 核心服务指标（QPS, Latency, Error Rate）纳入全站看板。

### 阶段 14：ES 向量检索迁移至独立向量库 Qdrant (Qdrant Vector DB Migration)
- [x] **Qdrant 向量库接入与预建集合** — 在 Helm Chart 中集成生产级 Qdrant 并限制 limits 防止 OOM，在服务启动时幂等创建 `tweets` 向量集合（1024维 Cosine 相似度）。
- [x] **发件箱事务双写重构** — 重构 Timeline 消费端 OutboxWorker，将 ES 的 `ContentVector` 设为 `nil` 释放 HNSW 与 JVM 堆内存压力，数据双写刷至 Qdrant，并通过 Snowflake UUID 映射方法 `ConvertSnowflakeToQdrantID` 规避 64位无符号精度截断溢出隐患。
- [x] **MCP 智能体双路并发召回** — 重构 MCP 检索工具 `search_tweets_by_semantic` 与 `hybrid_search_tweets`，利用 `errgroup` 对 ES 倒排（BM25）与 Qdrant 向量（HNSW）进行 1.5s 短超时并发双路召回，去重后接入 Reranker 二次精排与 gRPC 延迟回表。

### 阶段 15：多 Agent 协同写推文原型（真实性校准）
- [x] **Search/Style/Reference/Writer 管线原型** — 模式四已能收集领域、作者和指定参考内容，再交给 Writer 生成候选。
- [ ] **Review Agent 独立执行** — 当前主链路没有独立 Review Agent；需在统一 Runtime/Profile 后补齐。
- [ ] **数据拉取阶段并发化** — 当前主链路仍按 Search → Style → Reference 串行调用，不宣称已完成并发优化。
- [ ] **Lock-free 状态隔离** — 当前 Workflow Blackboard 使用锁保护可变状态；单写者不可变状态将在强化计划 P4 实现。

### 阶段 16：舆情播报与影子风控双轨并行治理 (Trending Reporter & Anti-Spam Shadowban)
- [x] **visible_type 4 适配** — 修改 `tweet.proto` 并增加影子封禁状态可见性过滤。
- [x] **GetFeeds 读时防线与写时洗地** — 在 `GetFeeds`/`GetUserTimeline`/`GetTweet` 中实施 map 一致性检测和影子封禁逻辑，拦截对垃圾推文的透出；当前由 Agent Worker 的 `queue.tweet.risk` 直接订阅原始 `tweet.created`，Timeline Consumer 不再二次广播风控事件。
- [x] **Redis Lua+Pipeline 原子清洗** — 编写原子 ZREM 和 DECR 未读数 Lua 脚本，使用 Pipeline 批次（500规模，10ms间隔限流）异步对粉丝的 Timeline 进行清洗，规避集群 CROSSSLOT 瓶颈。
- [x] **分布式排他锁防护** — 基于 Redis `SetNX` 排他锁对舆情追踪哨兵进行分布式锁控制，防止 Ticker 定时器多实例重复发推雪崩。
- [x] **切片隔离与 Mock 压测挡板** — 实现 `MOCK_EMBEDDING=true` 压测挡板，拦截真实 Embeddings API 以支持零费用高并发压测，并在内部对推文切片进行显式重新声明与深拷贝以绝 race。
- [x] **单元测试与 K6 剧本** — 使用 miniredis 分布式锁、洗地 Lua 脚本、Race 检测测试通过，并编写了 K6 压测剧本。

### 阶段 17：百万并发 Feed 流优化与请求归并多级缓存 (Million-Concurrency Feed Optimization)
- [x] **BigCache L1 进程内存缓存** — 引入纯 Go 实现的零 GC BigCache 作为一级内存缓存，大幅降低 GC STW 扫描压力。
- [x] **Redis Base Tweet L2 缓存与随机 TTL** — 引入 `tweet:base:<ID>` 二级 Redis 缓存，并通过 24小时 + 0~30分钟随机抖动 TTL 防御缓存雪崩。
- [x] **Singleflight 并发归并与短超时熔断** — 引入 `golang.org/x/sync/singleflight`，将读操作与 `DoChan` 结合 `select` 设置短超时（800ms/1500ms），秒级熔断上游防止协程雪崩。
- [x] **Redis Pub/Sub 广播失效与 L1 一致性** — 引入 `tweet_invalidations` Redis 订阅广播，删除/隐藏时双链路（DeleteTweet API 与 Consumer 消息清洗）异步清除全局 L1 本地缓存，达成最终一致性。
- [x] **大V个人时间线 ZSet 缓存与防穿透占位** — 引入 `user_timeline:<ID>` ZSet 缓存大V最新的 1000 条推文，并配合 `:initialized` 标志区分“冷数据/未初始化”和“大V推文为空”的场景，杜绝缓存穿透。
- [x] **Redis Pipeline 批量 Pull 与内存归并排序** — 放弃拉取关注流（GetFeeds）时对 MySQL 的依赖，改用 Redis Pipeline 批量执行多个大V ZSet `ZRevRangeByScore`，在内存中进行 Merge Sort 并截取，实现全缓存化混合 Feed 流。
- [x] **无感秒开异步分页预热 (Cursor Pre-warming)** — 引入异步预热，检测到 `hasMore` 时在后台启动独立的、最大 2s 超时防挂起的预热协程，根据 `nextCursor` 提前拉取下一页推文 IDs 并写入 L1/L2，实现极速翻页。

### 阶段 18：Serverless Agentic Mesh (智能体云原生网格化)
- [x] **Temporal 基础设施部署** — 引入 `temporalio/dev:1.24.0` 开发版容器，提供内置 SQLite 和可视化 Web UI 控制台，注入 `TEMPORAL_HOST` 环境变量。
- [x] **开销感知与容灾路由** — 实现 `GetChatCompletionWithRouting` 双底层连接，在 Cheap 模型网络超时/429/500错误时自动 failover 至 Premium 性能机，并支持 Stream 进度心跳 RecordHeartbeat 回调防僵死误杀。
- [x] **自愈状态机与防崩溃禁忌** — 严格规避非确定性原生协程与时延函数，在舆情监控哨兵采用 `Continue-As-New` 定时清空历史事件（50,000限制）开启新轮回。
- [x] **MQ 去重去死锁与生死线 Ack** — 绑定 Workflow ID `RiskControl-Tweet-{TweetID}` 实现防消息重投，只在状态机持久化确认后调用 Ack，筑牢可靠接力线。

### 阶段 19：AIOps 与可观测性实验 (Chaos Engineering & AIOps)
- [x] **Continuous Profiling 持续监控** — 在核心组件集成 `pyroscope` SDK，在容器配置中拉起 Pyroscope 仪表盘（映射 4040:4040），实现对 CPU 与 Memory 分配的实时火焰图分析，保障零开销高性能。
- [x] **网关级线程安全 ERROR 日志环** — 实现并发安全的预分配固定容量 20 元素的 RingBuffer 中间件，自动截留 5xx 状态的 API Gateway ERROR 日志，绕过 Loki 查询的高额网络开销。
- [x] **告警去重防抖与 firing 过滤** — 在网关 `/alerts` webhook 路由注册了带 `sync.Map` 的 5分钟去重缓存及 status 状态拦截过滤器，彻底阻断了告警风暴对 LLM API 造成的 DDoS 限流封禁风险。
- [x] **大模型智能 RCA 诊断与持久化** — 在 `agent-service` 中实现 gRPC 接口 `AnalyzeAlert`，结合大模型降级与心跳路由机制对告警与黑匣子日志关联诊断，生成 Markdown 格式的 RCA 报告并实时追加保存到 `scratch/alert_rca_reports.md`。
- [x] **Chaos Mesh 混沌机制与跨平台验证脚本** — 编写了 Redis pod 网络延迟 5s 的混沌测试 `network-delay.yaml`，并提供配备 `minikube` 上下文安全验证护栏与 Windows 平台 GBK 编码兼容性的自动化测试脚本 `run_chaos_test.py`。

### 阶段 20：系统级 Chaos 压力测试与 AIOps 智能自愈闭环 (Phase 20)
- [x] **K6 高并发压力测试及环境隔离** — 编写了 K6 压测脚本 `stress_feeds.js` 模拟高并发刷新，并在 JWT 中间件引入了基于环境变量 `APP_ENV=chaos_testing` 强隔离保护的安全压测 Token。
- [x] **大模型结构化指令输出** — 升级 `agent_service.go` 的 `AnalyzeAlert` 角色与 System Prompt，实现文本报告与自愈 JSON 指令（`[STRUCT_START]/[STRUCT_END]`）的双轨输出和安全正则表达式解析提取。
- [x] **网关安全自愈与保底规则合并** — 构建了 `self_healer.go`，内置 `Allowlist` 允许列表防范大模型幻觉全站自杀。并在动态重载时将 AI 规则合并入 Sentinel 基础防线，防止规则全量覆写造成漏洞。
- [x] **混沌自愈闭环演练与限流规避重构** — 进行了 K6 压测与 Pod-Kill 持续混沌故障注入联合演练，在 `ratelimit` 中间件实现安全压测 Token 旁路绕过，完美把并发压力传导至 Sentinel 层，验证了故障发生时 99.8% 的高频熔断拦截与服务上线后的完全闭环自愈。

### 阶段 21：Evolution V4.0 - 云原生自治与多维度自愈防御系统
- [x] **OTel 长生命周期 Span 延续** — 引入 `context.WithoutCancel` 延续异步诊断协程的 Trace 上下文，彻底解决了 HTTP 响应返回时 HTTP Context 取消引起的 OTel Span 幽灵截断和数据丢失问题。
- [x] **K8s 动态客户端自愈防撞** — 在网关自愈引擎集成 `k8s.io/client-go` 的 `dynamic.Interface`，使用无类型 `unstructured` 解析直接操作 K8s VirtualService，并使用 `RetryOnConflict` 乐观锁并发重试保护，规避了高并发更新下的 409 Conflict 故障，实现了高可用的灰度切流自愈。
- [x] **Qdrant 3节点分布式高可用集群重构** — 在 Helm 部署中将 Qdrant 重构为 3 节点 StatefulSet，在启动命令行配合 Downward API `POD_NAME` 实现条件自引导（qdrant-0 独立启动，qdrant-1/2 引导加入），规避了冷启动死锁；同时设置 Collection 默认为 2 副本冗余，完美验证了节点强杀混沌下的无损搜索。
- [x] **服务环境与基础设施高可用降级** — 升级 `agent-service` 连接 MongoDB、ES 和 Temporal Server 的启动和运行容灾逻辑，支持外部存储离线/降级平滑启动，并将 `agent-service` 完美移入 Kubernetes 网络。

### 阶段 22：Evolution V5.0 - 自适应性能压测与 Pyroscope 缓存调优智能体
- [x] **线程安全动态配置热更新** — 实现了 `pkg/config/dynamic_config.go`，使用 `atomic.Pointer` 结构体指针实现一键无锁原子替换，建立边界安全护栏 (Guardrails)。
- [x] **防配置脑裂双保险订阅** — 设计了 `config_receiver.go`，通过 Redis KV 获取自举初始化 + Redis PubSub 监听动态更新，规避 K8s Pod 重启/扩容时的脑裂问题。
- [x] **多级缓存动态热重载** — 重构了 `timeline_cache.go`，将 L1 状态缓存与 L2 推文基本信息缓存的 TTL 以及翻页预加载深度全部改用动态原子配置，实现零停机热重载。
- [x] **Pyroscope 持续剖析分析器** — 编写了 `profiling_analyzer.go`，通过 Pyroscope Collapsed 格式抓取并提取 CPU 采样前五名热点业务堆栈，为 AI 提供精准的实时性能上下文。
- [x] **调优智能体工具与防震荡冷却锁** — 在 `agent_service.go` 中挂载了 AI 缓存调优工具 `TuneCacheConfig` 并注入 Redis 分布式冷却锁（3分钟 Cooldown 观察期），杜绝参数剧烈抖动与系统震荡。
- [x] **可观测追踪与联合测试验证** — 完成了 OTel 调优子 Span 的深度切入，并编写了一键自动化演练脚本 `test_adaptive_tuning.py`，打通了“性能过载-火焰图抓取-AI调优-配置热重载-OTel拓扑追踪”的完整闭环。
- [x] **发总结的 AI 助手定时任务激活** — 实例化并启动了 `TrendingReporter` 定时舆情监视哨兵，实现了大V粉丝热度推文并发双路召回（ES+Qdrant）、内存去重、AI 评论快报自动生成及官方发帖（BotUserID=100）的完整业务闭环。

### 阶段 23：云原生对象存储适配与数据流重构 (MinIO Object Storage)
- [x] **MinIO 对象存储自举集成** — 网关 (gateway) 集成官方 MinIO SDK，启动时自动检测并幂等创建 `twitter-media` 存储桶，自动下发 JSON 公共只读 (Public Read-Only) 访问策略。
- [x] **流式上传与透明网络适配** — 重构网关 `/api/v1/upload` 接口，废除本地磁盘落盘逻辑，将文件流流式上传至 MinIO，并妥善解决网关容器内写入 Endpoint (`minio:9000`) 与宿主机公开访问 Base URL (`http://localhost:9000`) 异构网络访问问题。

### 阶段 24：大厂级动态加权防刷热搜与时间衰减模型 (Dynamic Trending & Anti-Spam Decay)
- [x] **GSE 纯 Go 中文分词与实体提取** — 引入纯 Go 实现的轻量级分词器 `go-ego/gse`，避免 CGO 对 Docker 容器交叉编译的干扰，支持开箱即用的分词与 NER。
- [x] **48小时 TTL 预映射缓存** — 发推时提取实体词并写入 `tweet_tags:{tweet_id}` (48h TTL)，将点赞/评论的二级加分完全收拢在内存层，实现数据库 0 压力。
- [x] **1小时滑窗防刷 (W&A)** — 引入用户维度的 `lock:user_tag_count:{uid}:{tag}` 进行 1 小时限频，同一个 UID 对同一个词超过 3 次的交互不再计分，有效抵御水军刷榜。
- [x] **多副本防雪崩分布式锁衰减** — 引入 Redis 分布式锁 `lock:trends_decay`，在多副本部署下每分钟仅允许单实例抢锁并对 ZSet 进行 0.95 衰减，同时即时裁剪前 100 名之外的长尾热词。

### 阶段 25：Flutter 移动端 App 开发 (V6.0) (已完成)
- [x] **基础设施与环境适配** — 采用 `adb reverse` 统一真机与模拟器调试时的本地网关端口映射，规避多端网络拓扑冲突；在客户端实现相对媒体路径补全，彻底解决 MinIO 绑定痛点；基于 `--dart-define-from-file` 配合 json 配置实现编译期环境隔离。
- [x] **登录与鉴权** — 编写 Auth 响应模型和本地 Token 持久化存储，利用 Riverpod v3 Notifier 机制自动引导登录态并自愈登出。
- [x] **首页信息流与发帖上传** — 封装 TweetCard 极佳适配 Twitter 图片布局逻辑，实现触底 Cursor 自动分页加载；集成 image_picker，调用后端 `/api/v1/upload` 流式上传至 MinIO。
- [x] **互动、详情与个人主页** — 支持点赞、转发、书签、评论等交互按钮的即时 UI 乐观更新；支持 `TweetDetailScreen` 层级评论展现；构建 `ProfileScreen` 并配合 Notifier.family 提供发帖、媒体、喜欢三 Tab 各自 Cursor 分页。
- [x] **热搜与 AI 智能体** — 支持 Trends 趋势榜与文字检索；编写 `AgentChatScreen` 支持 RAG 向量卡片、对话以及 AI 创作草稿一键确认发布。
- [x] **实时通知与私信聊天室** — 接入 WebSocket 服务端推送，实时广播私信（`message`）事件并让 `ChatRoomScreen` 对话气泡动态流式追加；实现实时通知（`like`/`comment`/`follow`）红点徽标提示及 `NotificationScreen` 分类列表跳转。

### 阶段 26：微服务通信与连接性能深度治理 (gRPC Connection Optimization & Eager Dial)
- [x] **Consul 发现 gRPC 重构** — 将 `auth-service` 对 `user-service` 的连接由原本硬编码的容器内直连升级为通过 Consul 注册中心自动发现与 `round_robin` 负载均衡。
- [x] **连接预热 (Eager Connection)** — 在 API Gateway 和 `auth-service` 的 gRPC 客户端引入 `conn.Connect()` 主动长连接预热，将 DNS 首次解析超时及 TCP/HTTP2 建连成本消灭在系统初始化阶段，彻底扫除首笔登录请求超时。
- [x] **链路追踪断裂修复** — 在 `auth-service` 连接 `user-service` 时补齐 OTEL Client stats handler 拦截器，完美在 Jaeger 中连通 `user-service` Span，完成闭环分布式追踪。

---

## 🚧 当前阶段：已全部完成


### 目标
完成了微服务高并发多级缓存、分布式 Saga 自愈编排、K6/PodKill 混沌压测、大模型闭环熔断自愈治理以及基础设施控制面的主动降级与网格自愈，并实现基于 Pyroscope 火焰图的 AI 自适应性能调优智能体。同时，全面实现了具备离线持久化、BFF 多层缓存对接、WebSocket 实时聊天与分类推送等特性的生产级 Flutter 移动端手机 App。


---

## 🔮 未来规划

| 方向 | 描述 | 优先级 |
|------|------|--------|
| **多因子自愈恢复** | 基于 AIOps 进行更复杂的灰度流量切分与跨集群动态漂移 | ⭐⭐⭐ |
| **安全意图防火墙** | 建立防 Prompt 注入与 AI 幻觉的自愈 Guardrails 独立哨兵 | ⭐⭐ |

---

## 🛠 技术栈一览

| 类别 | 技术 |
|------|------|
| 语言 | Go 1.25 |
| Web 框架 | Gin |
| RPC | gRPC + Protobuf |
| 数据库 | MySQL 8.0 (GORM) |
| 缓存 | Redis |
| 消息队列 | RabbitMQ 3.13 |
| 服务发现 | Consul |
| 熔断降级 | Sentinel-Go |
| 链路追踪 | OpenTelemetry + Jaeger 1.66 |
| 指标监控 | Prometheus + kube-state-metrics |
| 容器化 | Docker + Minikube |
| 编排 | Kubernetes (Helm Chart) |
| CI/CD | GitHub Actions |
| GitOps | ArgoCD |
| Ingress | Nginx Ingress Controller |

---

## 2026-06-25 P1 工作流 DSL 闭环开发

- [x] **DSL 持久化接口**：新增 Agent Workflow 的 proto、gRPC、Service、Repository 与 Gateway HTTP 接口，支持创建、更新、列表、详情查询，并通过 MongoDB `agent_workflows` 集合按 `user_id` 隔离存储。
- [x] **DAG 后端执行入口**：新增运行记录模型与 `agent_workflow_runs` 集合，`RunWorkflow` 会加载 DSL、编译 DAG、注入用户上下文 Guardrail、执行节点并回写运行状态、输出快照与错误信息。
- [x] **前端画布接入后端**：`WorkflowEditor.vue` 的保存按钮已调用 `/agent/workflows`，运行按钮会自动保存未持久化 DSL 后调用 `/agent/workflows/:id/run`，并将 `tool_name` 写入工具节点属性，保证后端可稳定解析工具类型。
- [x] **Go 验证补测（2026-07-14）**：当前工具链已可用，`go test ./internal/module/agent/... ./cmd/agent-service` 通过。

## 2026-06-25 P2 认知 RAG 主链路接入

- [x] **Cascade Router 工程化重构**：重写 `workflow/rag/router.go`，恢复清晰的中英文词典路由，并保留语义锚点与 LLM JSON fallback，路由失败时自动降级为全局知识检索。
- [x] **三层记忆管理器可降级化**：重写 `workflow/rag/memory.go`，支持 L1 用户画像、L2 Qdrant 私有 episodic memory、L3 ES + Qdrant 混合召回；任一依赖缺失时跳过对应路径，不阻断对话主流程。
- [x] **Chat 认知上下文注入**：新增 `cognitive_context.go`，通过 `SetCognitiveEngine` 将 Router/Memory 以可插拔方式挂入 `AgentService`，普通 Chat 会在 system prompt 中注入画像、记忆与知识上下文，并记录路由元数据。
- [x] **摘要结晶化写入**：对话回复持久化后异步提炼当前回合为 L2 episodic memory，写入用户隔离的 Qdrant collection；失败只记日志，不影响用户请求。
- [x] **Go 验证补测（2026-07-14）**：Agent Service 与认知 RAG 已纳入 Agent 全包测试并通过。

## 2026-06-25 P3 工作流运行可观测性与节点级 SLA Trace

- [x] **Scheduler 节点级 Trace**：调度器新增 `NodeTrace`，记录节点 `pending/running/success/failed/skipped`、开始/结束时间、耗时和错误信息，支持路由分支跳过节点可视化。
- [x] **运行记录输出增强**：`RunWorkflow` 的 `output_json` 同时保留原 blackboard 顶层字段，并新增 `blackboard` 与 `traces` 字段，前端和排障工具可以直接消费节点运行轨迹。
- [x] **前端节点状态回填**：工作流画布运行后会读取 `run.output.traces`，逐节点更新状态，不再只按整体成功/失败粗粒度染色。
- [x] **Trace 单元测试补充**：新增调度器 trace 测试，覆盖成功节点与 router 未激活分支的 skipped 状态。
- [x] **Go 验证补测（2026-07-14）**：Scheduler 测试通过，`go test -race ./internal/module/agent/workflow/engine` 通过。
> 范围校准：本阶段只完成 Workflow NodeTrace，不等于 Agent Run/Step/LLM/Tool 全链路 Trace；统一 Agent 可观测性仍属于强化计划 P5。
## 2026-06-25 P4 工作流挂起恢复与 Checkpoint

- [x] **Engine Checkpoint 抽象**：新增 `WorkflowCheckpoint` 与 `SuspensionError`，调度器支持 `suspended` 节点状态、黑板快照导出以及 `ExecuteFromCheckpoint` 恢复执行，恢复时不会重跑已成功或已跳过的上游节点。
- [x] **运行记录挂起状态持久化**：`agent_workflow_runs` 增加 `checkpoint_json`、`waiting_node_id`、`resume_token`、`suspended_at` 字段，并补充按 `user_id/status/suspended_at` 查询的 MongoDB 索引。
- [x] **Wait 节点与内部恢复入口**：DSL 新增 `wait` 节点类型，用于人工审批、外部 MCP 回调和长阻塞任务；`AgentService.ResumeWorkflowRun` 已支持从 checkpoint 水化并继续执行下游。
- [x] **前端画布接入**：将原先不被后端支持的 `approve` 节点统一为 `wait` 节点，补充 reason/resume_token 属性配置，并支持 `suspended` 节点状态展示。
- [x] **Temporal 自定义工作流决策门**：删除未注册且重复实现拓扑/黑板/路由/重试/审批语义的实验 Bridge。用户 DAG 明确使用本地统一 IR 与持久状态机；Temporal 只保留真实注册的风控和热点长任务。
- [x] **验证**：前端历史构建验证通过；2026-07-14 补跑 Agent 全包 Go 测试与 Workflow Engine race 测试通过。
## 2026-06-25 P4 Follow-up: LLM Provider Customization

- [x] **LM Studio 默认模型修正**：移除 `qwen3.6-plus` / `deepseek-chat` 旧默认值，本地工作流默认改为 `qwen2.5-3b-instruct`，DashScope 对话默认改为 `qwen-plus` / `DASHSCOPE_MODEL_CHAT`。
- [x] **工作流 LLM 节点可定制 API**：`LLMChatTool` 支持从节点属性读取 `provider`、`base_url`、`credential_ref`、`model`、`system_prompt`、`max_tokens`，兼容 LM Studio、DashScope 和 OpenAI-compatible 自定义服务；明文 `api_key` 已在 P2 被禁用。
- [x] **前端属性面板补齐**：Workflow Editor 的 LLM 节点提供 Provider、Base URL、Credential Ref、Max Tokens 配置项，用户可在单节点维度切换模型服务且不会把密钥写入 DSL。
- [x] **验证**：前端历史构建验证通过；2026-07-14 补跑 Agent 全包 Go 测试通过。

## 2026-06-26 课程设计报告书写与 Word 自动化导出 (Course Design Report)

- [x] **详细 Markdown 报告撰写**：在 `docs/COURSE_DESIGN_REPORT.md` 中为项目编写了完整的 9 章节课程设计报告。突出论述了系统的微服务模块边界、多级缓存机制、JWKS 零共享密钥验签、以及 Canal CDC 事务发件箱模式（对传统对称 JWT 和同步双写消息队列的设计优势），内容严谨学术，代码注释行级解析。
- [x] **Word DOCX 导出自动化**：编写了 `scripts/generate_report_docx.py` 转换脚本，基于 `python-docx` 库实现了对 Markdown 中标题级联、无序/有序列表、阴影边框引用块、细灰网格表格以及 Consolas 等宽字体代码容器的精美排版解析。
- [x] **深度扩写与百页级架构剖析**：响应用户对报告字数和深度的要求，重新读取了核心中间件（JWKS）、MQ 消费者（Timeline 推拉结合防雪崩）、事务发件箱（Outbox）以及 AI 智能体编排（Temporal + Kahn 拓扑排序）的核心源码，生成了附带真实源码注释和极深原理解读的万字级报告。
- [x] **生成与双端验证**：在项目根目录下成功输出了 Word 文档 `（6）课程设计报告（含任务书）_生成版.docx`，并通过 Python 代码级物理校验确认其完全生成。

## 2026-06-27 Agent 会话一致性与工作流策略扩展

- [x] **历史消息协议统一**：对话详情新增标准 `role/content` 字段并保留 `question/response` 兼容字段，前端统一归一化渲染；重复点击当前会话可重新拉取，避免列表选中但消息区为空。
- [x] **自定义工作流会话持久化**：AI 助手运行工作流时显式传入 `persist_dialogue` 和 `dialogue_key`，运行结果写入同一 MongoDB 对话仓储；编辑器测试默认不落聊天历史。
- [x] **模型目录职责修正**：AI 助手模型接口只返回 Chat Completion 模型，Embedding 模型从用户选择器移除。
- [x] **MCP 工作流桥接**：将语义推文检索、混合推文检索、用户搜索、用户推文和按 ID 取推文接入统一 `AgentTool` 注册边界。
- [x] **策略组件补齐**：新增 Planner、ReAct、Plan Executor 组件；策略节点采用只读 MCP 白名单、1-8 次迭代上限和节点超时，写操作仍由独立 PublishTweet 节点承载。
- [x] **验证**：`npm run build` 通过；`go test ./internal/module/agent/... ./internal/gateway/handler ./cmd/agent-service` 通过。

## 2026-07-14 Agent Runtime 面试强化开发

详细方案见 [`docs/AGENT_RUNTIME_STRENGTHENING_PLAN.md`](./AGENT_RUNTIME_STRENGTHENING_PLAN.md)。评审已通过，当前完成 P0-P5；P6-P7 仍以实际代码和验收结果为准。

- [x] **P0 真实性基线与迁移护栏**：完成 ADR、五入口行为/冒烟基线、`AGENT_RUNTIME_V2_MODES`、Proto/Mongo 兼容契约测试与回滚说明；默认执行路径未改变。
- [x] **P1 统一 AgentRunner**：完成 Runtime 类型/错误模型、ReActRunner、四类 Action、预算取消边界、请求级模型提示、OpenAI-compatible/MCP Adapter、可注入 Tool Catalog、Consult/Workflow Strategy/Assist 灰度迁移，以及 Search/Style/Writer/Review Profile 收敛。
- [x] **P2 Message/Token/Model 治理**：统一消息构建、Token/成本预算、Provider 路由与凭证引用。
- [x] **P3 Tool Policy 与 Human Approval**：统一 ToolExecutor、审批持久化、批准/拒绝状态机、一次性恢复、审批 UI、持久幂等结果回放、TweetService 原生幂等、Tool 熔断指标、治理对账和 MCP 二次认证已完成。
- [x] **P4 Workflow Engine 语义收口**：已完成版本化 DSL、确定性本地 IR、单写者 Blackboard、内置 Reducer、不可变 Revision、指定版本运行、周期 Snapshot、StateEvent Replay 校验、Retry/Timeout/Cancel/Skip/Suspend 状态机、持久补偿、Temporal 决策门、公开只读回放、共享运行预算、后台补偿巡检和 Journal 人工控制。
- [x] **P5 Agent 可观测性**：已完成 Run 控制、独立 Run/Step/LLM/Tool Trace、脱敏查询、控制台、Agent gRPC 与 Provider/MCP HTTP OTel 传播、低基数指标、有界运行事件流、大型 Tool Result 引用、Blackboard 快照检索、Prompt Template 执行版本、安全预览采样和 Agent Grafana 面板。
- [ ] **P6 RAG/Memory Eval**：共享 Episodic Collection、Session 结晶、Embedding 迁移与评测闭环。
- [ ] **P7 产品化增强**：Profile/Prompt 版本、审批、项目 RBAC、运行指标与可选业务结果 A/B 自动回滚、归档 Eval 质量证据发布门禁已完成；工作流模板、外部 MCP、多租户额度、真实 WORM 基线、真实产品事件源验收和外部 IAM 待完成。

P0 验证：

- `go test ./internal/module/agent/... ./cmd/agent-service` 通过。
- `go test -race ./internal/module/agent/runtime ./internal/module/agent/workflow/engine` 通过。
- `go vet ./internal/module/agent/... ./cmd/agent-service` 通过。
- 本次变更文件定向 `git diff --check` 通过；全仓检查仍会命中两处本轮未修改的 Flutter 尾随空格。

P1 第一增量验证：

- `go test ./internal/module/agent/... ./cmd/agent-service -count=1` 通过。
- `go test -race ./internal/module/agent/runtime ./internal/module/agent/model ./internal/module/agent/service -count=1` 分包通过；Service 首次统一命令受 120 秒编译时限影响，单独提高时限后通过。
- `go vet ./internal/module/agent/... ./cmd/agent-service` 通过。
- Runtime 离线测试覆盖 FinalAnswer、单/多 ToolCall 配对、RAGSearch、AskHuman、非法动作、工具错误、MaxSteps、Timeout、Cancellation 与写工具 fail-closed。

P1 第二增量验证：

- `go test ./internal/module/agent/... ./cmd/agent-service -count=1` 通过。
- `go test -race ./internal/module/agent/runtime ./internal/module/agent/model ./internal/module/agent/profile ./internal/module/agent/service -count=1` 通过。
- `go vet ./internal/module/agent/... ./cmd/agent-service` 通过。
- 迁移测试确认 Assist 只获得只读工具；Workflow Strategy 保留节点模型、Token、迭代数、工具白名单和 `tool_trace`；所有测试使用 Fake Runner/Tool Catalog，不连接真实 MCP 或模型。
- Multi 的 Writer/Review Profile 组合保持原 System Prompt 文本，不增加模型调用次数；消息 metadata 追加记录本次使用的 Profile ID。

P2 第一、第二增量验证：

- [x] **Message Builder 与 Token Budget**：新增 System/Developer/Policy、当前输入、History、Persona/Memory、RAG、Tool Result、Blackboard 的统一装配边界；工具调用对原子保留，历史/工具结果可压缩，RAG Chunk 整块跳过。
- [x] **Runtime Usage 与预算预检**：新增单次输入/输出、Run 总 Token 和 `budget_exceeded`；调用前预留输出额度，Provider 无 Usage 时显式记录 `Estimated=true`，失败后的已发生调用仍保留 Step Usage。
- [x] **模型选择闭环**：`model_kind_id` 不再在 gRPC 层被丢弃，Chat/Consult/Assist 的 Legacy 与 Runtime 请求都使用前端所选 Chat 模型；Embedding 模型仍不进入用户选择器。
- [x] **Credential 与 SSRF 基础护栏**：DSL/运行输入拒绝明文 `api_key`，旧 DSL 查询自动脱敏，LLM Tool 与前端统一改为 `credential_ref`；Base URL 增加私网地址、DNS 解析结果和重定向防护。
- [x] **验证**：`go test ./internal/module/agent/... ./cmd/agent-service -count=1`、P2 五包 `go test -race`、`go vet ./internal/module/agent/... ./cmd/agent-service` 与 `web/npm run build` 通过。
- [x] **认知链路 Token 化**：`MemoryManager.BuildContextBlock` 接入统一 TokenCounter；Persona 按 Token 截断，RAG/Memory Chunk 保持整块装配，并返回实际入选 Chunk。
- [x] **Session Summary 结晶**：所有会话模式统一在消息持久化后调度；达到 12 条未摘要消息立即结晶，空闲 2 分钟触发短会话结晶，使用 Mongo 租约、版本游标和稳定 Qdrant Point ID 保证幂等重试；删除对话或服务关闭会取消 Timer 与在途摘要 Job。
- [x] **结构化 Episodic Payload**：摘要保存 `memory_type/facts/preferences/decisions/followups/source_dialogue/summary_version`，JSON 解析失败可降级纯文本。
- [x] **P2 第三增量验证**：Agent 全包测试、Repository/RAG/Service `go test -race` 与 `go vet` 通过。
- [x] **P2 第四增量**：Run 成本/并发预算、Catalog Provider/Fallback 路由、用户级 Provider Config 加密 CRUD、Key 轮换/撤销与工作流 `provider_config_id` 解析已完成。
- [x] **P2 第四增量验证**：Agent/Gateway/Cmd 全包测试与前端构建通过；Runtime、Model、Credential、Repository、Service、Workflow Tool 竞态检测通过；Agent/Gateway/Cmd `go vet` 通过；本轮文件定向 `git diff --check` 通过。全仓检查仅命中两个本轮未修改的 Flutter 文件尾随空格。
- [x] **P3 第一增量：统一工具治理基础**：完成 `ToolSpec + ToolHandler`、实例 Registry、统一 Executor、JSON Schema 校验、认证身份覆盖、权限、超时、保守重试、错误分类、审批 Gate、幂等键要求和敏感参数审计；Workflow、Runtime MCP 与 Legacy 模型驱动工具路径已统一接入。
- [x] **P3 第一增量安全验证**：无 Wait 节点的 Workflow `PublishTweet` 和 Legacy ReAct `create_tweet` 均在网络调用前返回 `approval_required`；Agent 全包测试、Tool/Runtime/Service race 和 `go vet` 通过。
- [x] **P3 第二增量：审批持久化与安全恢复**：新增 Approval/Execution Mongo 模型及索引、带租约审批状态机、脱敏审批参数、乐观锁决策、随机令牌哈希、Run 原子领取、审批后重试当前工具节点，以及查询/决策/恢复 gRPC 与 Gateway API。
- [x] **P3 第二增量并发与安全覆盖**：离线 Fake 测试覆盖跨用户审批、重复决策、错误令牌、并发双 Resume、拒绝终止和幂等结果回放；并发恢复仅一个成功且 TweetService 调用一次。
- [x] **P3 第二增量验证**：Agent/Gateway/Cmd 全包测试、Tool/Engine/Service `go test -race`、Agent/Gateway/Cmd `go vet`、`web/npm run build`、Go 格式检查及本轮文件定向 `git diff --check` 全部通过。
- [x] **P3 第三增量：审批工作台与下游原生幂等**：Web 新增审批收件箱、挂起 Run 详情和批准/拒绝/恢复交互；TweetService 新增用户级幂等键、唯一记录与同事务 Tweet/Poll/Outbox 提交，Agent 和 Temporal 发布链路透传稳定执行键。
- [x] **P3 第三增量缺陷修复**：Tweet/Poll Repository 接入 Unit of Work 事务 Context，并修复 TweetService 事务失败后仍返回成功的旧错误路径；相同键不同输入返回冲突，旧客户端不传键继续兼容。
- [x] **P3 第三增量验证**：Tweet/Agent/Gateway/Cmd 受影响模块全包测试、Tweet/Tool/Service `go test -race`、完整 `go vet`、Web 生产构建、Go 格式与定向 `git diff --check` 通过；审批抽屉完成 1280px 与 390px 实际页面验收。
- [x] **P3 第四增量：熔断、指标与治理对账**：新增工具级 Closed/Open/Half-Open 熔断状态机和低基数 Prometheus 指标；新增周期性 Mongo 对账，回收失效审批/执行租约并终止因审批过期而挂起的 Run。
- [x] **P3 第四增量：MCP 内部认证**：MCP 默认只绑定回环地址，SSE/Message HTTP 边界要求 Bearer Token，Tool Middleware 再校验认证上下文；进程内客户端自动携带同一令牌并支持优雅停机。
- [x] **P3 第四增量验证**：`go test ./...` 通过；Tool/MCP/Repository/Service `go test -race` 通过；`go vet ./internal/module/agent/... ./cmd/agent-service` 和本轮文件定向 `git diff --check` 通过。全仓 `go vet ./...` 仅命中既有 `internal/module/auth/grpc/auth.go:44` 不可达代码，已登记问题单。
- [x] **P4 第一增量：版本化 DSL 与 Compile IR**：DSL 增加版本、节点输入/输出 Schema、Retry、Policy、Profile/Provider Reference；旧 JSON 兼容为 v1，新保存时补齐版本并保留前端 `ui`。新增独立 IR 编译器，确定性校验拓扑、变量依赖与并行写冲突。
- [x] **P4 第一增量：单写者状态与确定性并发**：Blackboard 改为 copy-on-write 代际和 append-only 内存 StateEvent；节点只获得 `StateView`，Scheduler 以并行波次执行并由协调器按声明顺序合并 Delta，Checkpoint 记录状态版本。
- [x] **P4 第一增量：取消与失败收敛**：节点失败、挂起或外部取消后，Scheduler 等待本波次所有已启动 Goroutine 退出再返回；离线测试覆盖反向完成顺序、同代只读、内部失败等待、外部取消等待和恢复不重跑。
- [x] **P4 第一增量验证**：`go test ./internal/module/agent/... ./cmd/agent-service -count=1`、IR/Engine/Tool/Service `go test -race` 与 `go vet ./internal/module/agent/... ./cmd/agent-service` 通过。
- [x] **P4 第二增量：不可变 Workflow Revision**：新增 Revision 集合与当前版本指针，旧 DSL 懒迁移；保存使用条件更新防止并发覆盖，Run 固定 Revision，Resume 不再读取后续编辑的 DSL。
- [x] **P4 第二增量：持久 StateEvent**：新增 append-only 事件集合和 `(run_id, sequence)` 唯一约束，事件内容哈希冲突 fail-closed；Run/Checkpoint 以 `state_version` 对齐恢复序号。
- [x] **P4 第二增量验证**：Agent/Gateway/Agent Service 全包测试通过；Repository/Service/Engine/gRPC/Gateway Race 检测通过；Agent/Gateway/Agent Service `go vet` 与本轮定向 `git diff --check` 通过。
- [x] **P4 第三增量：Revision 查询与指定版本运行**：不可变版本的列表/详情接口贯通 gRPC、Gateway、Web 与 AI 助手，画布和自定义工作流模式不再隐式选择第一条或当前可变定义。
- [x] **P4 第三增量：周期 Snapshot 与恢复校验**：Engine 通过不可变 `StateCommit` 回调解耦存储；Mongo 新增快照集合和唯一版本索引，长流程按事件阈值落快照并推进 Run 游标，完成/挂起强制保存最终快照。
- [x] **P4 第三增量：StateEvent Replay**：Resume 从最近快照重放后续事件，校验事件序号、事件/快照哈希、Checkpoint 版本及状态摘要；持久状态不完整或被篡改时拒绝恢复，且不会重放 LLM/写工具副作用。
- [x] **P4 第三增量验证**：`go test ./internal/module/agent/... ./cmd/agent-service -count=1`、Engine/Repository/Service `go test -race`、`go vet ./internal/module/agent/... ./cmd/agent-service` 与本轮定向 `git diff --check` 通过；Revision 前后端增量的 Gateway 定向测试与 Web 生产构建同时通过。
- [x] **P4 第四增量：确定性 Reducer**：DSL/IR/Engine 支持 `append/sum/min/max/merge/first/last`；同路径并发写必须使用一致且非空的 Reducer，协调器按节点声明顺序归约，失败时不留下节点或全局状态的部分提交。
- [x] **P4 第四增量：编辑器契约保真**：属性抽屉可配置全局状态路径、输出来源和 Reducer；Workflow/Revision 加载再保存会保留 `writes` 及 Schema、Retry、Policy、Profile/Provider Reference 等节点顶层执行元数据。
- [x] **P4 第四增量验证**：`go test ./internal/module/agent/... ./cmd/agent-service -count=1`、IR/Engine/Service `go test -race`、`go vet ./internal/module/agent/... ./cmd/agent-service`、`web/npm run build` 与本轮定向 `git diff --check` 全部通过。
- [x] **P4 第五增量：节点执行状态机**：新增合法状态转换表、`retrying/timed_out`、Trace 尝试次数和确定性退避；仅重试明确可重试/临时网络错误，挂起、业务错误、取消与 Deadline 不重试，退避可被 Context 立即中断。
- [x] **P4 第五增量：Retry 配置闭环**：Workflow Editor 使用开关和数值控件编辑 DSL 顶层 `retry`，总尝试次数、初始/最大退避、倍数和确定性 Jitter 均可配置，且不混入 Tool Properties。
- [x] **P4 第五增量验证**：`go test ./internal/module/agent/... ./cmd/agent-service -count=1`、IR/Engine/Tool/Service `go test -race`、`go vet ./internal/module/agent/... ./cmd/agent-service`、`web/npm run build` 与本轮定向 `git diff --check` 全部通过。
- [x] **P4 第六增量：持久补偿契约与反向计划**：Tool 节点支持独立补偿工具、输入映射、Timeout/Retry；IR 拒绝非法引用或未注册工具，Engine 只对已成功节点生成确定性反向拓扑计划。
- [x] **P4 第六增量：Journal 与恢复闭环**：主 Run 失败先持久化 Compensation Journal，再按严格序号和租约领取；补偿复用 ToolExecutor、Policy、Approval、Breaker 和稳定独立幂等键，支持审批挂起、拒绝终止及失败后显式重试，且不会重放主 DAG。
- [x] **P4 第六增量验证**：IR/Engine/Repository/Service/Gateway 定向测试与竞态检测、Agent/Cmd `go vet`、Web 生产构建和本轮定向 `git diff --check` 全部通过。
- [x] **P4 持久补偿运维闭环**：审批回调、显式重试、过期执行租约后台扫描、独立脱敏 Journal API 和控制台已完成。无需审批的过期补偿可无人值守恢复，审批型补偿仍安全转人工，不宣称所有补偿都自动执行。
- [x] **P4 第七增量：Temporal 决策门**：实验证明旧 Bridge 与统一 IR/Reducer/Event/Approval/Compensation 语义冲突且从未注册；现已删除。用户 DAG 不再规划双执行后端，后台风控与热点任务继续独立使用 Temporal。
- [x] **P4 第七增量验证**：删除实验 Bridge 后 Agent/Cmd 全包测试、`go vet` 和本轮定向 `git diff --check` 通过，启动链路仍只注册风控与热点 Temporal Workflow。
- [x] **P4 第八增量：公开只读回放**：新增用户隔离 Replay RPC/HTTP API，返回固定 Revision、哈希校验 StateEvent、Snapshot 元数据与脱敏 Compensation Journal；篡改、缺序或超过 10,000 事件时 fail-closed，且绝不重放模型和工具副作用。
- [x] **P4 第八增量：回放 UI**：Workflow Editor 可查看完整性状态、Revision/Snapshot、状态事件时间线和补偿步骤；原始补偿输入/输出、幂等键与审批敏感参数不进入响应。
- [x] **P4 第九增量：工作流共享预算**：DSL/IR/Editor 增加节点尝试、并发、总超时、总 Token 和估算成本预算；Scheduler 使用线程安全共享账本限制重试与并发波次，Checkpoint/Run 输出持久化预算快照，恢复后继续累计。
- [x] **P4 第九增量：模型调用统一记账**：直接 LLM Tool、Runtime ReAct/Plan-Execute 和 Legacy 回退策略均在网络调用前预留额度并按 Provider Usage 或显式估算提交；并发分支不能绕过总额，实际调用超限仍保留已发生消耗。
- [x] **P4 第九增量验证**：Agent/Gateway/Cmd 全包测试、Runtime/Engine/Tool/Service 竞态检测、Agent/Gateway/Cmd `go vet`、Web 生产构建和定向格式检查通过。
- [x] **P4 第十增量：过期补偿租约扫描**：Mongo 只返回每个 Run 严格首个未完成且租约过期的执行记录；后台 Reconciler 复用原子 Claim、Attempt ID、ToolExecutor 和幂等键，多实例竞争不会重复副作用。
- [x] **P4 第十增量：审批安全降级**：无需审批的过期补偿自动继续；Write/Risky 或目录缺失工具不会由后台执行，而是转为 `compensation_failed`，保留用户显式重试和一次性 Resume Token 路径。
- [x] **P4 第十增量验证**：Repository/Service 定向测试与竞态检测、Agent 全包测试、Agent/Cmd `go vet` 和定向格式检查通过。
- [x] **P4 第十一增量：补偿 Journal 控制面**：新增用户隔离的 Journal RPC/HTTP 查询和专用重试端点，只返回运行摘要、状态、哈希、租约与错误；工作流/补偿输入输出、幂等键、Attempt ID 和审批参数均不进入响应。
- [x] **P4 第十一增量：人工恢复窗口**：`planned`、`failed` 与租约过期的 `executing` 可人工推进；有效租约与 `suspended` 禁止抢占。Workflow Editor 新增补偿日志视图，重试仍复用 ToolExecutor、原子 Claim、审批和一次性 Resume Token。
- [x] **P4 第十一增量验证**：Agent/Gateway 全包测试、Service/Repository/gRPC/Gateway Handler 竞态检测、Agent/Gateway/Cmd `go vet`、Web 生产构建和定向格式检查通过。
- [x] **P5 第一增量：Run 查询与 Trace 控制台**：新增用户隔离、Workflow/Status 过滤、稳定倒序分页的 Run 摘要 API；Workflow Editor 可查看历史运行、错误、预算和节点 Trace，并切换当前回放/补偿上下文。
- [x] **P5 第一增量：跨实例取消**：新增 `canceling/canceled`、取消原因与时间证据；Mongo 原子请求、执行侧有界轮询和 `status + revision` 终态提交避免取消被旧快照覆盖。周期 Snapshot 通过专用原子仓储接口只推进 `state_version + revision`，不再用旧 Run 对象覆盖状态与取消字段；游标提交失败会恢复本地版本并允许幂等重试。只允许取消 `running`，审批挂起与有效补偿租约不被绕过。
- [x] **P5 第一增量验证**：Agent/Gateway 全包测试、Service/Repository/gRPC/Gateway Handler 竞态检测、Agent/Gateway/Cmd `go vet`、Web 生产构建和定向格式检查通过。
- [x] **P5 第二增量：独立执行追踪模型**：新增与业务 `output_json` 解耦的 `RunRecord/StepRecord/LLMCallRecord/ToolCallRecord`、Mongo 四集合 Repository 与并发安全 InMemory Recorder；唯一键包含租户身份，避免相同 Run/Record ID 跨租户覆盖。
- [x] **P5 第二增量：Runtime/Workflow 统一采集**：Runtime ReAct 和 Workflow DAG 均记录 Run/Step；模型调用记录最终 Model/Provider、精确或估算 Token/成本、耗时与错误分类；ToolExecutor 只向 Trace 发送参数/输出摘要和长度，不保存原始 Prompt、Completion、工具参数或结果。
- [x] **P5 第二增量：租户隔离查询与控制台**：新增 `GET /api/v1/agent/workflow-runs/:id/traces`，读取前先按 Run 所有者鉴权；Workflow Editor 并行读取业务详情与独立 Trace，分别展示节点、模型和工具调用，旧 Run 继续回退到 `output_json.traces`。
- [x] **P5 第二增量验证**：Observability/Runtime/Repository/Tool/Service/gRPC/Gateway Handler 竞态检测、Agent/Gateway 全包测试、Agent/Gateway/Cmd `go vet` 与 Web 生产构建通过；离线测试覆盖跨租户同 ID、LLM/Tool 正文不落 Trace、Workflow 所有权校验和 Gateway 契约。
- [x] **P5 第三增量：OTel 与低基数指标**：Agent Service 初始化可 Flush 的 ParentBased OTLP gRPC Provider，Tweet/User Client 与 Agent Server 接入 `otelgrpc`；Mongo Recorder 扇出到 OTel Span 和 Prometheus，Run/Step/LLM 指标不使用用户、Run、Step、模型或错误原文 Label。
- [x] **P5 第三增量：传播与隐私验证**：内存 gRPC 联合测试验证客户端父 Trace ID 可传播到服务端；Span 测试验证 Prompt/Completion 摘要不会成为 Attribute，Metric 测试验证租户自定义值被归一为有限枚举。
- [x] **P5 第四增量：Provider/MCP HTTP 传播**：新增可组合 HTTP Client Transport 与 Server Middleware；默认/用户级 Provider、Reranker 和 MCP SSE/Tool 请求注入 `traceparent`，MCP 服务端提取父上下文，保留原 Endpoint Policy、Redirect、Timeout 与取消语义。
- [x] **P5 第四增量：安全与竞态验证**：离线 Client/Server 联合测试验证父子 Trace；Span 不包含 URL Path/Query、Authorization、Prompt 或正文，原始网络错误不进入遥测。`pkg/trace`、AI、Model、MCP、Tool 与 Service 竞态检测通过。
- [x] **P5 第五增量：有界运行事件流**：新增 Redis Stream Recorder/EventReader，按 `user_id + run_id` 哈希隔离 Key，使用服务端 Stream ID、约 2000 条长度上限和 24h TTL；Agent Service 提供心跳、连接窗口、终态关闭、游标重置和 Context 取消语义，Mongo Trace 继续作为完整查询事实源。
- [x] **P5 第五增量：gRPC/SSE 与控制台恢复**：追加服务端流式 RPC 和 `GET /api/v1/agent/workflow-runs/:id/events`；Gateway 保持 Bearer 鉴权并即时 Flush，Web 使用可取消 `fetch` 解析 SSE，按 `record_id` 幂等合并，切换 Run/关闭控制台时终止旧连接，窗口结束或网络中断后从最后游标恢复。
- [x] **P5 第五增量验证**：Observability/Repository/Service/gRPC/Gateway Handler/Router 竞态检测、Agent/Gateway/Cmd 全量测试与 `go vet`、Web 生产构建通过。本地 Vite 页面返回 200；Codex 内置 Playwright 依赖因宿主权限阻塞，未把自动化截图冒烟计为通过。
- [x] **P5 第六增量：大型 Tool Result 治理**：统一 ToolExecutor 对 MCP/ReAct/DAG 结果执行 JSON 体积硬上限；超过 64 KiB 的结果通过 Service Port 写入独立私有 MinIO 桶，Mongo 幂等记录保存内联结果或对象引用，回放时校验长度与 SHA-256。对象上传后若 Mongo 提交失败会回收未提交对象。
- [x] **P5 第六增量：脱敏引用 Trace**：Tool Trace、gRPC、Gateway SSE 与 Workflow 控制台追加 storage/reference/content-type 元数据，只暴露无凭证 `minio://` 哈希路径，不返回 Tool Result 正文；Docker 显式启用对象归档，独立开关提供 fail-closed 回滚。
- [x] **P5 第六增量验证**：Tool/ObjectStore/Repository/Service/Observability/gRPC/Gateway Handler 竞态检测、Agent/Gateway/Cmd 全量测试、`go vet`、Web 生产构建和 Compose 配置校验通过；所有对象存储测试使用离线 Fake，不连接真实 MinIO。
- [x] **P5 第七增量：可检索 Blackboard 快照**：新增目标版本之前最近快照 + `(after, target]` 有界事件重建链路，严格校验用户/Run 所有权、快照与事件哈希、连续序号和最终状态版本；支持路径前缀、关键词、页大小及绑定版本/过滤条件的稳定游标。
- [x] **P5 第七增量：脱敏查询与控制台**：新增 `GET /api/v1/agent/workflow-runs/:id/blackboard` 和对应 gRPC 契约；敏感键递归脱敏，单值预览、查询、字段数和重放事件数均有硬上限。Workflow Editor 运行控制台支持版本、路径、关键词和前后页检索。
- [x] **P5 第七增量验证**：Repository/Service/gRPC/Gateway Handler/Router 目标测试与竞态检测、Agent/Gateway/Cmd 完整测试、完整 `go vet`、Web 生产构建和本轮定向 `git diff --check` 通过；测试覆盖跨租户拒绝、篡改快照拒绝、运行中游标版本固定、敏感原值不可搜索和超大值正文省略。
- [x] **P5 第八增量：安全内容采样与模板身份**：Prompt/Completion 默认继续只存哈希和长度；可选采样按稳定键确定性执行，限制预览/扫描大小并拒绝疑似密钥、凭证、邮箱、手机号、身份证和带敏感结构 URL。Runtime Profile 与 Workflow Revision 的模板身份进入租户隔离 LLM Trace，但不进入 OTel/Metric/日志正文。
- [x] **P5 第八增量：Agent Runtime Dashboard**：新增 Docker/Helm 双份 16 面板 Grafana 看板，覆盖 Run、Step、LLM、Token/成本、Tool Policy、熔断和治理对账；Prometheus 补 Agent `9191` 抓取，Grafana datasource 使用固定 UID，面板查询不引入 `user_id/run_id/prompt` 高基数标签。
- [x] **P5 第八增量验证**：Observability/Service/Tool/gRPC/Gateway Handler 目标测试、相关竞态检测、Agent/Gateway/Cmd 全包、`go vet`、Web 生产构建、Docker Compose、Helm 模板、双份 Dashboard JSON/UID/面板数和高基数查询扫描通过。竞态首轮 gRPC 编译器进程瞬时退出，单包重跑通过并记录于 Issue 86。

## Codex 项目治理原生化（2026-07-16）

- [x] **仓库级 Skill 迁移**：将旧单数目录下的平铺 Skill 文档迁移为 `.agents/skills/<name>/SKILL.md`，并将架构审计工作流纳入 `architecture-audit` Skill。
- [x] **Skill 元数据规范化**：11 个 Skill 仅保留 `name/description` frontmatter，触发条件合并到 `description`，支持 Codex 渐进式发现与按需加载。
- [x] **Agent 文件职责收口**：项目 Context/Rule 统一迁入 `.agents/context`、`.agents/rules`；`AGENTS.md` 保持仓库级持久指令入口；`.codex/agents/test-runner.toml` 保持项目级自定义 Subagent 配置。
- [x] **迁移验证**：官方 `quick_validate.py` 在 `PYTHONUTF8=1` 下验证 11 个 Skill 全部通过；旧单数目录路径引用与遗留文件扫描为零。
## 2026-07-19 P3 审批跨设备恢复安全收口

- [x] **短期轮换恢复授权**：新增 `issueWorkflowResumeGrant` gRPC 与 Gateway API，只允许审批所有者为已批准、未过期且仍挂起的绑定 Run 按乐观锁 revision 签发；授权默认 5 分钟并受审批过期时间封顶。
- [x] **单次领取与旧令牌失效**：签发使用 Mongo 条件更新原子轮换 SHA-256 哈希并推进 revision，原始挂起令牌与此前 Grant 立即失效；恢复检查可选 Grant 过期时间，成功后清空授权元数据，旧 Run 无该字段时保持兼容。
- [x] **跨浏览器 Web 闭环**：审批收件箱先读取最新 Run，再签发授权并立即恢复；仅对 revision `409` 重新读取一次，已批准列表支持失败后再次继续。删除初始和后续令牌写入 `sessionStorage` 的死状态，只保留旧数据清理。
- [x] **敏感响应禁缓存**：工作流运行、补偿重试、恢复和 Grant 签发响应统一返回 `Cache-Control: no-store, max-age=0` 与 `Pragma: no-cache`。
- [x] **定向验证**：服务测试覆盖旧令牌失效、重复签发轮换、过期拒绝和最终副作用仅一次；Repository BSON、Proto 方法/字段与 Gateway 租户/revision/响应头契约测试通过。

## 2026-07-19 P6 共享 Episodic Collection 与 RAG Eval 基线

- [x] **共享 Episodic 写入契约**：新摘要写入 `agent_episodic_memory`，Payload 增加 `user_id`、Collection Schema、Embedding 模型/维度/版本；摘要重试继续覆盖稳定 Point ID。
- [x] **服务端租户隔离**：Episodic 检索通过 Qdrant `SearchWithFilter` 在存储侧注入 `user_id` 条件；旧 `episodic_user_<id>` 仅保留有界双读兼容，不在请求中枚举用户集合。
- [x] **离线迁移工具**：新增 `cmd/agent-memory-migrate`，按显式用户 ID 使用 Scroll + 原始 Point ID Upsert，支持批大小、dry-run 和迁移成功后的显式删除旧集合；不重新生成 Embedding。
- [x] **RAG Eval 基线**：新增 51 条覆盖中文、英文、混合语种、错别字、无答案、时态记忆和画像的离线数据集，以及 Recall@K、MRR、NDCG@K、空召回率和噪声率纯函数。
- [x] **P6 第二增量：可复现检索 Runner**：新增存储无关 `Retriever` 接口、逐 Case 错误、Recall/MRR/NDCG/Empty/Noise、P50/P95、稳定 RRF 融合与 JSON 报告；`cmd/agent-rag-eval` 可显式连接 ES/Qdrant/Embedding/Reranker，按固定顺序比较 BM25、Vector、RRF 和 RRF+Rerank，并记录环境、模型、数据集版本与随机种子。
- [x] **P6 第二增量：隔离与 Session End**：Fake Qdrant 共享集合覆盖双用户服务端 Filter 契约；新增 `POST /api/v1/agent/dialogues/:id/end`，取消并等待旧摘要 Job 后同步强制结晶，Mongo 租约/游标保证并发 End 最多写一份新摘要。
- [x] **P6 第三增量：Router 可评测化**：Cascade Router 改为固定词典优先级并通过 `RouteWithMetadata` 暴露 Lexical/Semantic/LLM Fallback/Default Stage；新增 34 条独立数据集、通用 Router Runner 与 `cmd/agent-router-eval`。离线词典/默认层基线为 31/34、Accuracy 91.18%、MisrouteRate 8.82%，L1/L2/L3 错投率分别为 22.22%/11.11%/0%。
- [x] **P6 第四增量：迁移只读验收**：`cmd/agent-memory-migrate --verify-only` 按显式用户 ID 使用共享集合服务端 `user_id` Filter，比较旧集合有效 Point ID 与目标集合，检查租户 Payload、共享 Schema、缺失/意外点，并可输出 JSON 报告；验证路径不写入、不删除数据。
- [x] **P6 第五增量：Router Provider 对照能力**：Cascade Router 改为 Embedding/Chat 窄接口与显式阈值配置，保留旧构造器兼容；`agent-router-eval` 支持 lexical/semantic/llm/full 四种模式，live 模式必须显式 `--allow-live`，API Key 只从环境读取。报告记录 Provider/Endpoint/模型、请求失败、Token、估算成本、Pricing Version、单 Case 超时和 Semantic/LLM 降级错误。
- [x] **P6 第六增量：RAG Live 评测护栏**：`cmd/agent-rag-eval` 现在必须显式传入 `--allow-live`；ES、Qdrant、Embedding、Reranker 使用受限 HTTP Client 和 Endpoint Policy，API Key 只从环境读取，Runner 支持单 Case Timeout，报告记录 Provider/Endpoint/模型/请求数/失败数/失败率。新增的客户端注入点保留旧构造器兼容，本轮未连接真实服务。
- [x] **P6 第七增量：策略选择与依赖懒初始化**：`agent-rag-eval` 支持 `--strategies` 子集执行并按依赖闭包初始化 Provider；BM25、Vector、RRF、RRF+Rerank 的报告顺序稳定，避免只验证单路召回时仍触发无关服务。
- [ ] **P6 未闭环项**：真实环境旧集合回填与跨用户隔离验收、保存真实 BM25/Vector/RRF/RRF+Rerank 基线报告，以及固定 Provider 下的 Semantic/LLM Router 对照；真实数据迁移与删除仍需人工确认。
- [x] **P6 第一增量验证**：`go test ./... -count=1`、RAG/Eval/Qdrant/迁移命令 `go test -race`、Agent/Qdrant/迁移命令 `go vet` 和本轮文件 `git diff --check` 通过；测试不连接真实 Qdrant、ES 或模型服务。
- [x] **P6 第二增量验证**：Eval/Qdrant/Session/gRPC/Gateway 目标测试、目标包 race、全仓 `go test ./... -count=1`、全仓 `go vet ./...` 和本轮文件 `git diff --check` 通过；默认 vendor 已同步缺失的 OTel `tracetest` 测试包。未运行真实 RAG 评测或迁移命令。
- [x] **P6 第四增量验证**：迁移命令与 Qdrant 目标测试、race、vet 及全仓 `go test ./... -count=1`、全仓 `go vet ./...` 通过；Fake Qdrant 覆盖服务端租户 Filter、Point ID 对账、错误租户/Schema 和 JSON 报告目录创建。本轮仍未连接真实 Qdrant 或执行真实迁移。
- [x] **P6 第五增量验证**：Router/RAG Eval/命令目标测试覆盖 Semantic 失败后 LLM 接管、无效 LLM JSON、ProviderErrorRate、单 Case Timeout、Fake OpenAI-compatible Embedding/Chat Usage 与成本统计、Endpoint Policy 和 live 显式开关；离线基线仍为 31/34、Accuracy 91.18%，本轮未调用真实 Provider。
- [x] **P6 第六增量验证**：Eval、AI、ES、Qdrant、RAG 命令目标测试通过；live 缺少 `--allow-live` 时在任何 Provider 初始化前拒绝；未调用真实 ES、Qdrant、Embedding 或 Reranker。
- [x] **P6 第七增量验证**：命令测试覆盖策略去重/稳定顺序、Vector-only 与 RRF+Rerank 依赖闭包；本轮未调用真实 ES、Qdrant、Embedding 或 Reranker。

## 2026-07-19 P7 Prompt/Profile 版本治理

- [x] **P7 第一增量：不可变 Profile Catalog**：新增 `profile_id + version` 唯一快照、Profile/Prompt/Tool 配置校验、返回副本深拷贝和无 Release 多版本拒绝策略；Catalog 不保存运行状态或用户数据。
- [x] **P7 第一增量：确定性灰度发布**：Release 使用 `0..10000` 基点和 `SHA-256(profile_id + salt + user_id)` 稳定分桶；部分灰度缺少身份或 salt 时 fail-closed，0/10000 支持全量回滚/切换。
- [x] **P7 第一增量：Runtime 与组合根接入**：内置 `assist.draft@v1/v2`，默认固定 v1；Assist/Workflow Runtime 统一通过 Resolver 选版，Agent Service 从严格 JSON 环境变量 `AGENT_PROFILE_RELEASES` 构造启动快照，非法配置拒绝启动。
- [x] **P7 第一增量：版本证据**：实际命中的 Prompt ID/Version 继续进入 LLM Trace；Assist 对话元数据新增 `agent_profile_version`，Prompt 中的用户变量只在选版后的副本渲染。
- [x] **P7 第一增量验证**：Profile/Service/Agent Service 目标测试、Profile/Service `go test -race`、全仓 `go test ./... -count=1` 与全仓 `go vet -p=1 ./...` 通过；测试只使用离线 Catalog/Runner，不调用真实模型、MCP、ES 或 Qdrant。
- [ ] **P7 Prompt 管理未闭环项**：运行错误率/P95/成本门禁、固定数据集 Eval 契约和归档回执候选审批已在第六至十增量补齐；真实 WORM 基线、业务效果信号和外部 IAM 仍待后续增量。

## 2026-07-20 P7 Profile/Prompt 持久化内核

- [x] **独立版本仓储**：新增 `agent_profile_versions` 与 `agent_profile_releases`，不扩大对话仓储接口；版本快照不可变并带 Schema/SHA-256，发布与 Release 更新使用 revision CAS。
- [x] **草稿与发布状态**：草稿创建不会进入运行目录；只有已发布版本参与 Catalog 构建，坏快照、身份不一致和非法 Release 均 fail-closed。
- [x] **原子目录替换**：请求通过 `AtomicResolver` 无锁读取；管理器完整构建、校验下一代目录后一次替换，失败继续服务旧目录。
- [x] **启动加载与回滚**：Agent Service 默认从 Mongo 加载；内置 < 持久化 < 环境覆盖，`AGENT_PROFILE_STORE_ENABLED=false` 可回退到内置目录。
- [x] **离线验证**：定向测试、Profile/Repository/Service 竞态检测、全仓测试、全仓 Vet、Compose 解析和 Helm 渲染通过；生命周期 Fake 不连接真实 Mongo、模型、MCP、ES 或 Qdrant。

## 2026-07-20 P7 Profile/Prompt 管理面与跨实例刷新

- [x] **受保护管理 API**：Proto/gRPC/Gateway 提供草稿创建、版本查询、发布 CAS、Release 查询/更新和审计查询；Gateway 使用 JWT 用户白名单，Agent Service 独立校验内部令牌，客户端无法提交或读取服务间凭据。
- [x] **Append-only 脱敏审计**：写操作在持久化变更前必须先记录 `requested`；随后记录 `succeeded/failed/activation_failed/propagation_failed`，仅保存操作元数据、revision 和 SHA-256，不保存 Prompt 正文或 Provider 凭据。
- [x] **跨实例最终一致刷新**：本地完整校验并原子激活后发布 Redis 失效提示；其他实例重新从 Mongo 构建 Catalog，默认每 30 秒周期反熵，通知丢失不会永久分叉。
- [x] **配置与部署契约**：Compose、`.env.example` 和 Helm 支持管理令牌、管理员 ID、通知频道与同步周期；Helm 令牌只允许引用已有 Secret。
- [x] **本轮验证**：管理相关定向测试、Profile/Repository/Service/gRPC/Gateway race、全仓 `go test ./... -count=1`、全仓 `go vet -p=1 ./...`、Compose 解析、Helm 默认及启用 Secret 双模式渲染均通过；事件总线测试仅使用 miniredis。
- [ ] **P7 后续**：运行指标自动回滚、归档 Eval 证据审批和业务效果信号契约已在后续增量补齐；组织目录 RBAC、真实 WORM 基线与真实产品事件源验收仍未完成。

## 2026-07-20 P7 Profile 双人发布审批与管理 UI

- [x] **快照绑定审批状态机**：新增 `agent_profile_publish_approvals`，申请绑定 Profile/version、草稿 revision 与 SHA-256；申请者不能审批自己的申请，重复提交和并发决策使用唯一键/revision CAS 拒绝。
- [x] **可恢复发布执行**：审批通过先进入带租约的 `applying`，成功进入 `applied`，失败进入 `apply_failed`；恢复时先检查目标版本是否已发布，覆盖“发布成功但审批收尾前崩溃”的窗口。
- [x] **项目级角色授权**：Gateway 分离 viewer/editor/approver/admin，admin 继承全部角色；直接发布默认关闭，仅由显式 `AGENT_PROFILE_DIRECT_PUBLISH_ENABLED` 开启为 break-glass。Agent Service 继续独立校验服务令牌。
- [x] **真实管理界面**：新增 `/agent/profiles`，接入草稿、发布申请、审批/拒绝/恢复、Release CAS 与审计接口；能力显隐读取后端 access API，不用前端状态替代授权。
- [x] **本轮验证**：审批状态机、Repository、Service、gRPC、Gateway 定向测试与竞态检测、全仓 `go test ./... -count=1`、全仓 `go vet -p=1 ./...`、Web 生产构建、Compose 解析、Helm 默认及角色 Secret 模式渲染均通过；`/agent/profiles` 已在 1280x720 浏览器视口完成实际渲染验收。
- [ ] **P7 后续**：运行指标自动回滚、归档 Eval 证据审批和业务效果信号契约已在后续增量补齐；外部组织目录/统一 IAM、真实 WORM 基线与真实产品事件源验收仍未完成。

## 2026-07-20 P7 动态项目 RBAC

- [x] **持久化角色绑定**：新增 `agent_profile_role_bindings`，按用户唯一索引保存 viewer/editor/approver/admin 动态角色，创建/更新/删除均使用 revision CAS；环境变量角色保持独立且不能被 API 覆盖或删除。
- [x] **服务端授权事实源**：Agent Service 合并静态 break-glass 与 Mongo 动态角色，并在所有 Profile 管理 RPC 内再次校验；Gateway 使用 JWT 用户查询权限，滚动升级遇到旧服务 `Unimplemented` 时仅回退到原静态角色。
- [x] **根管理员与审计护栏**：只有 `AGENT_PROFILE_ADMIN_USER_IDS` 中的根管理员可授予或撤销动态 admin；角色变更写入独立 append-only `agent_profile_role_audit_events`，不保存 JWT、内部令牌或请求正文。
- [x] **成员权限管理台**：`/agent/profiles` 新增成员权限、角色来源和角色审计视图；`AGENT_PROFILE_DYNAMIC_RBAC_ENABLED=false` 可回滚到纯静态角色模式。
- [x] **本轮验证**：Profile/Repository/Service/gRPC/Gateway 定向测试与竞态检测、全仓 `go test ./... -count=1`、全仓 `go vet -p=1 ./...`、Web 生产构建、Compose 解析、Helm 默认/根管理员模式渲染和缺少根管理员的失败模板均通过；测试不连接真实组织目录或外部 IAM。
- [ ] **P7 后续**：当前仍是项目级 RBAC；运行指标自动回滚、归档 Eval 证据审批和业务效果信号契约已补齐，外部 IAM、真实 WORM 基线与真实产品事件源验收仍未完成。

## 2026-07-20 P7 Profile 运行指标实验门禁

- [x] **Release 绑定实验状态机**：新增 `agent_profile_experiments` 与独立观测集合，实验快照绑定 stable/candidate、流量、salt 和 Release revision；每个 Profile 只允许一个 running 实验，状态更新使用 revision CAS。
- [x] **隐私最小化观测**：Runtime v2 Run 记录实际 Profile ID/version；旁路 Recorder 仅按 Run ID 幂等保存分组、成功标记、耗时和估算成本，不保存用户 ID、Prompt、Completion 或工具正文，记录失败不改变用户响应。
- [x] **自动止损**：达到每组最小样本后比较错误率、P95 延迟和平均成本；任一回归则通过 Release CAS 把候选基点设为 0。外部 Release 变更进入 superseded，目标样本通过只标记 passed，不自动提升候选。
- [x] **管理与观测入口**：Proto/gRPC/Gateway/Web 支持启动、分页查询、立即评估和停止；Prometheus 指标不使用 Profile/实验/用户等高基数标签。功能默认关闭，启用要求 Profile Store、管理令牌和静态管理员。
- [x] **本轮验证**：Profile/Repository/Service/gRPC/Gateway 目标竞态测试、全仓 `go test ./... -count=1`、全仓 `go vet -p=1 ./...`、Web 生产构建、Compose 解析及 Helm 默认/启用/缺失管理员失败模式通过；测试使用离线 Fake，未调用真实模型或运行线上实验。
- [ ] **仍未闭环**：运行指标门禁不代表答案语义质量；固定数据集 Eval 契约和归档回执候选审批已补齐，真实 WORM 基线、业务转化信号和外部 IAM 仍待后续增量。

## 2026-07-22 P6/P7 Agent Task Eval 质量证据层

- [x] **52 条固定任务集**：覆盖普通问答、平台检索、写作、澄清、工具失败、提示注入、越权发布、审批恢复和预算终止；数据集 ID 与版本固定，Loader 拒绝重复 ID、非法状态和不完整审批约束。
- [x] **存储无关 Runner 与隐私最小报告**：Executor 仅返回结构化 Outcome、工具调用、Step、Token 和内存输出；报告不保存回答正文，只保存 SHA-256、字符数、断言结果与聚合指标。
- [x] **行为与安全指标**：统计任务结果、工具选择/成功率、只读工具选择准确率、确定性语义断言、平均 Step/Token、预算终止、审批处理、越权写成功和虚构工具结果。
- [x] **稳定/候选质量门禁**：同数据集版本比较任务、工具和语义断言回归；只读工具选择准确率默认至少 90%，审批用例必须 100% 正确处理，越权写成功和虚构工具结果必须为 0。CLI 可在 CI 中通过非零退出码阻止失败候选，但不会自动发布 Profile。
- [x] **固定夹具验证**：`agent-task-eval` 对 52 条录制契约夹具生成 52/52 通过报告，14 个只读工具用例与 10 个审批用例通过，越权写/虚构工具结果为 0。该结果仅验证评分契约，不是实际 Provider/Profile 质量基线。
- [x] **本轮验证**：`go test` 定向与 Agent 全模块通过，Eval/CLI 竞态检测通过，`go test ./... -count=1` 与 `go vet -p=1 ./...` 整仓通过；实际 CLI 稳定/候选比较返回 `passed`。
- [x] **受控 Live Runtime Adapter**：显式 `--allow-live --runtime-config` 固定 Provider/Model/Profile，复用 Endpoint Policy、Provider Router、Profile Catalog 和 ReActRunner；配置严格拒绝明文 `api_key`，Credential 仅按环境变量引用读取，工具全部为 CLI 私有无副作用沙箱。
- [x] **报告完整性**：报告绑定数据集与脱敏执行配置 SHA-256，支持 HMAC-SHA256 签名、独立验签及已签名稳定报告加载；质量门禁拒绝空/不一致数据集哈希和未绑定执行配置的报告。
- [x] **Live HTTP 集成验证**：本地 OpenAI-compatible 测试服务完整走过受限 HTTP Client、固定模型/Profile、Runtime 和签名报告；同时修复本地 Provider 使用 `127.0.0.1` 时预检与拨号策略不一致的问题。
- [x] **本批验证**：Eval/Model/CLI 定向测试与竞态检测通过，Agent 全模块通过，`go test ./... -count=1` 和 `go vet -p=1 ./...` 整仓通过；实际 `v2` 签名报告 52/52、门禁 `passed`、独立验签成功且无 Input/Output 正文字段。
- [ ] **仍未闭环**：受控环境真实 52 条 Provider/Profile 基线、真实 Object Lock Bucket 验收、真实 Resume Token/MCP 集成验证及业务效果信号；归档回执候选审批已在第十增量补齐。

## 2026-07-22 P7 Agent Eval 版本化/WORM 归档代码闭环

- [x] **存储无关契约**：`eval` 只定义归档请求、版本回执和验证规则；MinIO SDK 隔离在 `objectstore` 适配器，评测 Runner/Profile 逻辑不依赖对象存储。
- [x] **强制不可变存储护栏**：专用 Bucket 必须启用 Versioning 与 Object Lock 且无 Bucket Policy；对象使用 `COMPLIANCE` 保留模式和 `If-None-Match: *` 写入，返回非空 `version_id`，适配器不提供删除能力，不满足条件时 fail-closed。
- [x] **端到端证据复验**：CLI 支持运行后归档、归档已有签名报告和按回执复验；上传后按精确版本回读，核对长度、SHA-256、保留策略与报告身份，再重新执行 HMAC 验签。本地回执使用 `O_EXCL` 创建，不能覆盖已有证据。
- [x] **密钥与隐私边界**：归档配置只保存 Access/Secret Key 的环境变量名，严格 JSON Loader 拒绝未知明文字段；对象键/回执只含哈希、版本、保留元数据与签名 Key ID，不保存 Prompt、回答、Base URL 或凭据。
- [x] **离线验证**：受影响包普通测试与 `go test -race`、Agent 全模块、`go test ./... -count=1` 和 `go vet -p=1 ./...` 整仓验证通过；Fake 覆盖缺 Versioning/Object Lock、Bucket Policy、GOVERNANCE 降级、回读篡改、重复归档和本地回执不可覆盖。
- [ ] **真实环境验收**：本机 LM Studio `127.0.0.1:1234` 可达且已加载 `qwen2.5-3b-instruct`；但独立 HMAC Key/Key ID、MinIO 凭据均未配置，`127.0.0.1:9000` 不可达，因此本轮未执行真实 52 条基线或声称已经完成真实 WORM 归档。

## 2026-07-22 P7 Eval 证据绑定 Profile 发布审批

- [x] **领域与基础设施解耦**：Eval 报告输出/签名成为可复用纯契约；Profile 只依赖 `QualityEvidenceVerifier`，MinIO 读取与 HMAC 验真留在 `objectstore`，Runtime 热路径不访问对象存储。
- [x] **发布门禁**：发布申请可绑定精确 Bucket/Key/Version ID/报告哈希；申请、批准和失败重试均校验有效 COMPLIANCE 保留期、`runtime_live` Profile 身份、passed Gate、至少 50 Case、零错误/越权写/虚构工具结果和审批用例全通过。
- [x] **持久化与 API 最小披露**：Mongo/Proto/Gateway 只保存和返回回执定位、Gate 与聚合指标，不保存数据集正文、Prompt、Completion、Base URL 或 Credential；请求唯一键包含证据对象身份。
- [x] **管理 UI 与部署**：`/agent/profiles` 支持粘贴归档回执并展示证据摘要；Compose/Helm/`.env.example` 新增默认关闭的 `AGENT_PROFILE_EVAL_EVIDENCE_REQUIRED` 及 Secret 配置，关闭开关可回滚到原双人审批策略。
- [x] **本轮验证**：Profile/ObjectStore/Repository/Service/gRPC/Gateway 定向测试、受影响包竞态检测、`go test ./... -count=1`、`go vet -p=1 ./...`、Web 生产构建和 Compose 解析通过；Helm 默认/启用 Secret 模式渲染及缺少管理员或质量证据 Secret 的失败关闭检查均通过。
- [ ] **真实环境验收**：仍需受控 MinIO Object Lock Bucket 和独立 HMAC Secret 生成、人工复核并提交第一份真实 52 条 LM Studio 基线。
- [ ] **后续闭环**：启动并验收专用 Object Lock Bucket，执行、人工复核并归档真实 52 条基线；补真实 Resume Token/MCP 与 Tweet/Agent 产品事件源接入验收。

## 2026-07-22 P7 Profile 实验业务效果信号

- [x] **向后兼容策略**：实验策略可选三类固定业务结果、每组最小结果样本和最大结果率下降阈值；旧记录零值继续只按错误率、P95 与成本评估。
- [x] **可信幂等回填**：新增 admin 保护的结果入口，只能更新已有 Run/Event 观测；同值重试返回幂等回放，冲突值、未知信号、未配置策略和终态实验失败关闭。
- [x] **自动止损与隐私边界**：业务结果样本不足时继续收集，候选结果率回归时复用 Release CAS 回滚；Mongo/API/Prometheus 不保存或标记用户、Prompt、Completion、工具正文及动态信号名。
- [x] **跨层契约与管理台**：Proto/gRPC/Gateway/Web 支持策略配置和结果率/样本展示；业务结果写入 API 不暴露在模型 Tool Catalog。
- [x] **本轮验证**：领域、Repository、Service、gRPC、Gateway 定向测试与受影响包竞态检测、全仓 `go test ./... -count=1`、全仓 `go vet -p=1 ./...`、Web 生产构建及定向 `git diff --check` 均通过。
- [ ] **真实事件验收**：Assist 草稿确认发布与外部点赞/评论正向归因均已接入；仍需受控环境 stable/candidate 样本、明确拒绝动作、DLQ 和回滚演练证据。

## 2026-07-22 P7 Assist 草稿发布产品结果归因

- [x] **来源运行契约**：Assist 响应和历史 assistant 消息暴露最小化 Run ID 与可发布标记；Confirm 请求追加可选 `source_run_id`，旧客户端可继续发布但不会被自动归因。
- [x] **可信确认发布**：Agent Service 校验来源 Run 属于当前用户、模式为 Assist 且状态已完成，再通过窄 `ConfirmedDraftPublisher` 直连 TweetService；幂等键绑定来源 Run 与最终编辑正文，返回真实 Tweet ID。
- [x] **真实正向信号**：TweetService 成功后，旁路记录 `draft_published=true`；只归入与 Run Profile 版本、实验窗口和策略信号匹配的运行中实验，同一 Run 重试幂等，不保存草稿正文或用户到实验观测。
- [x] **前端显式操作**：Web 优先使用结构化草稿候选，历史消息按常见草稿格式恢复；用户可选择、编辑并确认发布，研究摘要、风格分析和适用场景不会在结构化候选存在时被整体发出。
- [x] **失败隔离与可观测性**：来源非法返回 422；产品结果落库失败只记录脱敏日志和固定结果指标，不回滚已经发布的推文。未发布不自动写负样本，避免把用户暂未操作误判为拒绝。
- [x] **本轮验证**：Service/Repository/gRPC/Gateway/Cmd 定向测试、受影响包竞态检测、全仓 `go test ./... -count=1`、全仓 `go vet -p=1 ./...`、Web `vue-tsc + Vite` 生产构建和定向差异检查通过；测试使用 Fake，不调用真实模型、Mongo 或 TweetService。内置浏览器因本地网络隔离未完成视觉验收，见 Issue 93。
- [ ] **后续闭环**：定义明确拒绝动作后再接负样本；在受控流量中保存 stable/candidate、DLQ 重放与自动回滚演练证据。

## 2026-07-22 P7 内容互动产品结果归因

- [x] **可靠事件源**：点赞、评论业务记录与 `TWEET_LIKED`/`COMMENT_CREATED` Outbox 在 MySQL 同事务提交，Canal 统一发布到既有 Topic Exchange；载荷只 append-only 增加发生时间，Timeline 兼容消费。
- [x] **短期最小映射**：Assist 确认发布后保存带 TTL 的 Tweet、作者、来源 Run 和归因窗口，不复制正文；默认窗口 7 天，通过 `AGENT_PROFILE_CONTENT_ATTRIBUTION_WINDOW` 调整。
- [x] **正向语义**：窗口内首个非作者点赞或评论写入 `content_engaged=true`；普通推文、自赞、过期事件与 MQ 重投不增加实验样本，未互动不推断负值。
- [x] **消费护栏**：Agent 使用独立持久队列、16 条 Prefetch、1/2/4 秒退避和三次后 DLQ；Repository/Trace/Outcome 任一暂态失败均可重试，错误标签保持低基数。
- [x] **部署与验证**：Compose、Helm 和 `.env.example` 已同步归因窗口，RabbitMQ NetworkPolicy 已放行 Agent Service；Agent Processor/Consumer 和 Tweet Outbox 定向测试覆盖首次归因、重放、自赞、过期、格式错误、重试与 Outbox 失败。
- [x] **本轮验证**：定向测试、全仓 `go test ./... -count=1`、受影响包 `go test -race`、全仓 `go vet -p=1 ./...`、Compose 解析、Helm 渲染和定向 `git diff --check` 全部通过。
- [ ] **真实环境验收**：仍需启动 MySQL/Canal/RabbitMQ/Mongo 全链路，验证外部互动、DLQ 重放、stable/candidate 样本和自动回滚证据；转发与明确拒绝动作未纳入。

## 2026-07-23 P7 内容互动 DLQ 运维闭环

- [x] **安全检查与显式执行**：新增 `agent-profile-dlq-replay`；默认只检查并重新入队，执行要求操作人/原因，批次最多 100 条，累计重放次数最多 10 且默认 1。
- [x] **毒消息隔离**：只允许两个已知内容互动路由，发布前重新解析并校验事件；未知、格式错误、超大和达到重放上限的消息继续留在 DLQ，不会因单条毒消息删除后续消息。
- [x] **确认后 ACK**：重放使用独立 Publisher Confirm，Broker 确认接收后才 ACK 原 DLQ 消息；发布失败会保留当前及未处理消息，ACK 失败显式报告不确定窗口并依赖既有事件/Run 幂等。
- [x] **脱敏审计输出**：JSON 仅返回消息哈希、固定事件类型/错误码、操作人和原因哈希，保留 Trace/Correlation Header，不输出评论、推文、Prompt 或用户正文。
- [x] **代码验证**：定向普通测试与 `go test -race`、全仓 `go test ./... -count=1`、全仓 `go vet -p=1 ./...` 和定向差异检查通过；测试全部使用 Fake，不连接真实 RabbitMQ。
- [ ] **真实演练**：Docker Compose 当前无运行服务；真实 RabbitMQ/Canal/Mongo 重放与 stable/candidate 自动回滚证据仍待受控环境验收。

## 2026-07-23 P8 统一智能助手与能力生态

- [x] **产品与架构决策**：统一连续对话成为长期主入口；Chat/Consult/Assist/Multi/Workflow 五模式降级为兼容 API 或内部执行策略，不再继续新增同级模式。
- [x] **能力边界**：动态 Tool/Connector、确定性 Workflow、版本化 Skill 和受预算约束的 Multi-Agent 分层建模；Capability Hint 不授予权限，写工具仍由 Tool Policy/Approval fail-closed。
- [x] **迁移与回滚计划**：新增入口、旧接口保留、Dialogue ID 不迁移；前端切流可独立回滚，真实 Web Search、外部 MCP 与 Workflow-as-Tool 分 Feature Flag 演进。
- [x] **详细计划**：新增 `docs/agent/UNIFIED_AGENT_PRODUCT_PLAN.md`，明确 P8.0-P8.5、风险和验收门禁。
- [x] **P8.0 首批代码**：新增统一 `RunAgent` RPC/HTTP/Web Client 契约、可注入保守 Capability Planner、三种已实现能力映射、实际 Execution Profile/Capability 回传和跨层契约测试；旧五入口保持兼容。
- [x] **P8.0 验证**：Agent/Gateway 全模块普通测试通过；Service/gRPC/Handler/Router `go test -race`、目标 `go vet` 和 Web `npm run build` 通过。测试使用离线 Planner/Fake/现有单测，不调用真实模型、MCP 或公网搜索。

## 2026-07-24 P8.1 Capability Catalog 与首个组合任务

- [x] **不可变 Capability Catalog**：新增能力版本、状态、依赖与精确路由快照；重复能力、未知依赖、依赖环、不可用能力和未注册组合均 fail-closed，返回 Definition/Plan 使用副本隔离。
- [x] **站内研究后草拟**：`platform.search + content.draft` 解析为有序 `runtime.research_draft` 路由，复用现有 Runtime v2 和受治理 MCP Executor；Profile 只暴露 `hybrid_search_tweets`，不包含发布或其他写工具。
- [x] **证据与对话一致性门禁**：组合 Run 必须包含成功的站内检索 Tool Observation 才返回草稿；同一请求只运行一次 Runtime，并在最终阶段写入一组 user/assistant 消息，metadata 记录实际 Capability、Execution Profile 和版本证据。
- [x] **意图路由修复**：恢复统一 Planner 的中文搜索/草拟词典，中文“搜索资料并写推文”可自动进入组合路由；显式 Hint 的输入顺序不会改变 Catalog 规定的执行顺序。
- [x] **P8.1 第一增量验证**：Capability/Planner/组合路径目标测试、Agent Service 全包、Agent 全模块与 `cmd/agent-service` 普通测试、Service `go test -race`、Agent 全模块 `go vet` 均通过；全部使用 Fake Runner/Catalog，不连接真实模型、MCP 或公网。
- [x] **P8.1 第二增量：结构化执行证据**：站内搜索 MCP 工具输出版本化 `platform.tweet_search.v1` 结构，Runtime 独立透传 Structured Content；`RunAgent` 返回脱敏 Tool Activity 与站内 Citation，不解析最终回答、不暴露工具参数/原始结果/内部错误，并对来源工具、Tweet ID、数量和摘要长度执行硬校验。
- [x] **P8.1 第二增量契约与验证**：Proto 仅追加字段并重新生成；gRPC、Gateway 与 Web Client 同步。Agent/Gateway 全模块测试、受影响包 `go test -race`、Agent/Gateway `go vet` 和 Web 生产构建通过。
- [x] **P8.1 第三增量：Chat Runtime v2**：`conversation.reply` 使用显式 `runtime.chat` 路由和无工具 `conversation.reply@v1` Profile；统一入口固定复用 Runtime Provider Router、Message Builder、预算、Trace、认知上下文和 Session Summary。普通对话不读取 MCP Catalog、不获得搜索或写权限。
- [x] **P8.1 第三增量兼容与验证**：旧 `/chat` 保持原契约，并由 `AGENT_RUNTIME_V2_MODES=chat` 在 Runtime 与 Legacy 间切换；Runtime 消息持久化失败时不返回未保存答案。Chat/Profile/Planner 定向测试、Agent 全模块与 `cmd/agent-service` 普通测试、Service `go test -race` 和 Agent 全模块 `go vet` 均通过，测试未连接真实模型、Mongo、MCP 或公网。
- [x] **P8.1 第四增量：Artifact 与状态投影**：`content.draft` 从受灰度影响的兼容 Assist 路由迁移到显式 `runtime.draft`；只有已持久化且具有 `source_run_id` 的草稿才返回可发布标记和类型化 `content.draft` Artifact。`RunAgent` 同步返回 `run_status` 与脱敏 `approval_state`，普通响应不包含审批输入、凭据或一次性恢复令牌。
- [x] **P8.1 第四增量契约与验证**：Proto 仅追加 Artifact/Approval/Run Status 字段并重新生成，gRPC、Gateway 和 Web Client DTO 同步；草稿落库失败会整体失败。Agent/Gateway 全模块测试、受影响包 `go test -race`、目标 `go vet` 与 Web 生产构建通过。
- [x] **P8.1 第五增量：Web 统一入口**：AI 助手主界面删除 Chat/Consult/Assist/Multi/Workflow 五模式分支，所有自然语言请求统一调用 `RunAgent`；能力面板只发送可选 Hint，不授予 Tool 权限。Workflow 保持顶部独立“自动化”入口，旧五 API 继续保留为回滚兼容路径。
- [x] **P8.1 Web 执行证据与会话一致性**：助手消息展示实际 Capability、Run Status、脱敏 Tool Activity、站内 Citation 与可发布 Draft Artifact；发布仍携带 `source_run_id` 走显式确认。会话详情请求增加序号隔离，快速切换时旧响应不能覆盖当前 Dialogue；能力变化不会隐式新建会话；工作流审批恢复只刷新服务端会话，不再把无归属结果拼入当前聊天。
- [x] **P8.1 Web 验证**：`vue-tsc + vite build` 生产构建通过；使用本地临时 Mock Gateway 在 1440x900 与 390x844 视口验证历史会话、统一发送、结构化证据、草稿确认发布和 400ms 慢请求后的会话竞态，临时服务与测试文件已清理。
- [x] **P8.1 核心完成**：统一连续对话主入口、现有三能力及精确研究草拟组合、执行证据和 Artifact 展示已闭环。当前 Catalog 仍没有写 Capability，因此真实 `pending` 审批态需等受治理写能力接入后验收；公网 Citation/搜索与外部 MCP 分别进入 P8.2/P8.3。
- [x] **P8.2 第一增量：真实 Web Search Adapter**：新增 Provider-neutral `websearch.Provider` 和 Brave Web Search Adapter；旧 Workflow `WebSearch` 不再返回 Mock，未配置时 fail-closed。请求具备查询/结果/响应体/并发/超时硬上限，Provider HTTP 错误不回传响应正文。
- [x] **P8.2 出站安全与证据**：复用 Endpoint Policy 和受限 HTTP Client，拒绝私网/本地/凭据 URL、HTTP 重定向与 DNS Rebinding；新增 `web.search.v1` Structured Content、`web_page` Citation 和来源 URL/Schema/工具名校验，不从回答正文解析引用。
- [x] **P8.2 统一能力路由**：`web.search` 默认为 `planned`，仅在 Provider 启用且启动校验成功后变为 `available`；新增 `runtime.web_search` 与 `web.search + content.draft` 精确组合，Profile 只获得只读 `web_search`，没有发布权限。
- [x] **P8.2 Web/部署入口**：AI 助手增加“联网搜索/联网研究并草拟”能力 Hint；`.env.example`、Docker Compose 和 Helm 同步 Feature Flag、Provider、密钥引用和资源上限。默认关闭；平台 Brave Key 是可选兜底，未选用户配置且没有平台 Key 时调用 fail-closed。
- [x] **P8.2 第一增量验证**：Web Search/Workflow/MCP/Runtime/Service/Agent Service 普通测试、受影响包 `go test -race`、目标 `go vet`、Web 生产构建、Compose 解析及 Helm 模式渲染均通过；测试使用离线 Fake/HTTP Server，未调用真实 Brave API。
- [x] **P8.2 第二增量：受治理 Page Read**：新增 `web.page.v1`、`page_read` MCP 和 `PageRead` Workflow 组件；只读取公网 HTTP(S) 可见文本，拒绝凭据/Fragment、私网、本地地址、重定向、DNS Rebinding 与非文本响应，并限制 URL、响应体、字符、并发和超时。
- [x] **P8.2 Prompt Injection 与 Citation 边界**：移除脚本、样式、表单、导航及隐藏 HTML；常见注入信号写入 Safety 元数据，模型只接收去指令化且明确标记为不可信的文本。公网 Citation 只接受受信任工具的版本化 Structured Content，同 URL 的 Page Read 摘录可替换搜索摘要。
- [x] **P8.2 来源缓存与准入预算**：Search/Page Read 共享 Redis TTL 缓存和原子准入脚本；服务端从执行上下文注入用户/Run 身份，按用户窗口、Run 请求数及估算成本 fail-closed，DSL/MCP 输入无法伪造计费身份。
- [x] **P8.2 Workflow/部署接线**：Runtime 联网 Profile 可按需调用 `page_read`，普通 Assist 保持站内只读；Workflow Editor 增加 WebSearch/PageRead 组件，Compose/Helm/.env 同步缓存、Page Read 与治理配置。
- [x] **P8.2 第三增量：用户级搜索 Provider Config**：复用 AES-256-GCM、Keyring、Revision、撤销和租户所有权校验，新增 `kind=web_search` 的 Brave 配置；API Key 不进入 DSL、模型 Tool Schema、响应或日志，旧无 `kind` 配置按 `llm` 兼容。
- [x] **P8.2 租户路由与治理复用**：AI 助手和 Workflow WebSearch 可引用 `provider_config_id`；Runtime 仅从可信请求上下文向 MCP 注入配置 ID，模型不能伪造。动态 Provider 与平台兜底共享并发闸门、Redis Cache 和用户/Run Governor，缓存按用户、配置 Revision 和凭据版本隔离。
- [x] **P8.2 用户配置 UI/部署契约**：AI 助手新增个人联网 API 管理与选择，Workflow WebSearch 只列 `web_search` 配置，LLM 节点只列 `llm` 配置。Compose/Helm 支持 Provider Config 主密钥与 Keyring Secret；启用联网不再强制配置平台 Brave Secret。
- [ ] **P8.2 剩余**：在真实 Brave 凭据与受控公网环境完成质量、配额、缓存和生产 Redis 容量验收后，才能声明具体部署环境具备公网搜索。
- [x] **P8.3 第一增量：远程 MCP 连接控制面**：新增用户级 Connection、Mongo CRUD/CAS、独立 Feature Flag 和 HTTP API；仅接受 Streamable HTTP/SSE，明确拒绝 stdio 与未授权项目级作用域。Bearer Token 使用现有 AES-256-GCM Keyring，响应、Schema、日志与 DSL 均不返回凭据。
- [x] **P8.3 第一增量：发现与审核边界**：远程请求复用受限 HTTP Client，禁止重定向、私网/回环/DNS Rebinding，并支持独立 Egress Allowlist。Tool Discovery 对数量、名称、描述和 Schema 体积设硬上限，按名称规范化后生成 SHA-256 不可变 Snapshot；工具使用 `server_id.tool_name` 命名空间，Schema 变化保留旧 Active Snapshot 并要求重新审核。
- [x] **P8.3 第一增量：跨层契约与验证**：追加 gRPC/HTTP/Web API、Mongo 索引、Compose/Helm/.env 配置；离线测试覆盖租户隔离、密钥加密、Feature Flag、禁止 stdio/私网、真实 Streamable HTTP Discovery、稳定 Snapshot、重复发现复用和变更重新审核。Agent/Gateway 相关包完整测试、受影响包竞态检测、独立 `go vet`、Web 生产构建、Compose 解析及 Helm 渲染均通过。
- [x] **P8.3 第二增量：Snapshot Tool Policy**：Connection 保存绑定 Active Snapshot 的工具策略并使用 Revision CAS；只有服务端确认远端 `readOnlyHint=true` 且非 destructive 的工具可启用。Schema 变化暂停执行，新 Snapshot 审核后清空旧策略；写、风险和未知工具继续 fail-closed。
- [x] **P8.3 第二增量：动态目录与统一执行**：Repository 固定两次查询装配用户执行目录；`connector.mcp` 只在 Feature Flag 与 Manager 可用时路由到 `runtime.external_mcp`。Profile 每次 Run 只获得当前策略允许的精确工具名，远端调用复用 ToolExecutor 的 Schema、身份、超时、重试、熔断、结果治理、脱敏审计和 Trace。
- [x] **P8.3 第二增量：跨层 API 与 Web**：追加工具目录/策略 gRPC 与 HTTP 契约，Gateway 使用 JWT 用户身份；AI 助手增加外部 MCP 能力 Hint 和连接管理面，支持新增/编辑/撤销、发现、Schema 审核与只读启停，不回显凭据。
- [x] **P8.3 第二增量：离线验证**：真实本地 Streamable HTTP SDK Server 验证 Discover/Call/Bearer；Fake 覆盖租户隔离、Snapshot 绑定、只读声明、凭据仅调用时解密、策略失效、ToolExecutor Schema/审计和统一入口 Evidence。Web `vue-tsc + Vite` 生产构建通过；Agent 全模块与 `cmd/agent-service` 普通测试、Repository/Service/gRPC/Gateway/remote 竞态检测及目标 Vet 均通过。
- [x] **P8.3 第三增量：Workflow 高风险工具审批**：已审核 Active Snapshot 中未声明只读的工具可由用户显式标记为 `risky`；此类工具不进入统一 Agent 目录，只能作为 Workflow 动态 Tool 节点执行，并复用持久 Approval、Checkpoint、一次性 Resume Grant、租户身份和统一 ToolExecutor。
- [x] **P8.3 第三增量：恢复时重新授权**：审批前远端调用为 0；批准恢复后重新读取 Connection、Active Snapshot、Schema 和 Tool Policy，策略撤销或 Schema 变化均 fail-closed。高风险工具内部尝试固定为 1，节点错误显式标记不可重试，避免未知远端结果被 DSL 自动重放；外部 MCP 补偿仍禁止。平台 `user_id` 只参与租户授权、审批和审计，第三方请求仅携带远端 Schema 已校验的显式参数。
- [x] **P8.3 第三增量：工作流体验与验证**：组件库新增外部 MCP 工具，属性面板只列当前用户已审核且启用的 `read/risky` 工具，并支持通用 JSON 参数；管理面区分只读与“高风险·逐次审批”。Remote/Service 定向测试、Agent/Gateway/Cmd 扩大回归、Remote/Service `go test -race`、目标 `go vet`、定向 `git diff --check` 和 Web `vue-tsc + Vite` 生产构建通过；浏览器已验证组件卡片与连接管理弹窗，真实策略列表仍待可用第三方 MCP 连接验收。
- [x] **P8.3 第四增量：外部 MCP 幂等写入契约**：Discovery 同时记录标准 `idempotentHint` 与项目命名空间幂等参数元数据，并验证该参数是 Input Schema 的必填字符串；只有完整、已审核契约可启用 `write`，旧 Snapshot 零值继续 fail-closed。
- [x] **P8.3 第四增量：可信键注入与安全重试**：Workflow 为每个 Run/Step/Tool 生成稳定执行键，远端只收到域隔离 SHA-256 键；调用前再次解析租户、Snapshot 与 Policy 并覆盖 DSL/模型同名参数。写工具逐次审批、要求 ToolExecutor 持久幂等且允许同键有限重试；`risky` 仍严格单次，外部补偿仍禁止。
- [x] **P8.3 第四增量：跨层体验与离线验证**：Proto/gRPC/Gateway/Web 增量暴露声明、参数和支持状态；MCP 管理面与 Workflow Editor 区分只读、高风险和幂等写入。Fake 覆盖不完整契约拒绝、攻击者键覆盖、身份不泄漏、审批前零调用和两次重试键一致；Fresh 定向测试、Remote/Service `go test -race`、整仓 `go test ./...`、串行整仓 Vet、Web `vue-tsc + Vite` 构建和页面空态检查通过。
- [x] **P8.3 第五增量：有界 MCP Session Pool**：SDK Adapter 按 Connection、Transport、Endpoint 与 Credential Version 隔离复用 Session；全局容量、单连接容量、获取等待和空闲 TTL 均有硬上限。端点/凭据轮换及撤销后旧身份立即失效，池关闭后在途 Session 释放即销毁；独立开关可回退逐次建连。
- [x] **P8.3 第五增量：主动健康巡检**：Mongo 使用独立健康租约跨实例领取到期连接，不递增用户控制面 Revision；巡检具备批次、并发上限、超时、稳定抖动、指数退避和连续失败阈值。健康状态仅为 `unknown/healthy/degraded/unhealthy` 诊断信号，不自动改变 Snapshot、Tool Policy 或真实执行权限。
- [x] **P8.3 第五增量：观测与跨层体验**：新增无租户/连接高基数标签的健康与连接池 Prometheus 指标；Proto/Gateway/Web 显示固定健康分类、连续失败与检测时间，旧记录零值按 `unknown`。离线测试覆盖 Session 复用、容量背压、空闲回收、身份轮换失效、多实例租约、降级/恢复与标签收敛。
- [x] **P8.3 第五增量验证**：Remote/Repository/Service `go test -race`、整仓 `go test -vet=off ./...`、串行整仓 `go vet -p=1 ./...`、Web `vue-tsc + Vite` 生产构建、Compose 解析和 Helm Lint/渲染均通过。浏览器在桌面与 390px 移动视口验证 MCP 弹窗空态、服务不可用提示、滚动与表单布局；因本地 Agent Service/真实第三方 MCP 未启动，本轮未把真实连接健康状态和长连接代理行为计为通过。
- [x] **P8.3 第六增量：Unified Agent 权威 Run 生命周期基础**：新增独立 `agent_execution_runs`，不再把 Trace 或 DAG Workflow Run 当作模型驱动 Agent 的业务状态。Run 在模型执行前创建 `running@revision=1`，由租户、Run ID、状态和 Revision CAS 提交完成、失败、取消、等待人工或等待审批；同一 Run ID 贯穿 Runtime、Trace、对话元数据和状态记录。状态集合只保存路由、版本、Token/成本及按 Run ID 域隔离的正文摘要，不保存凭据、Tool 原始参数、输入/回复正文或恢复令牌。
- [x] **P8.3 第六增量：失败闭合与灰度**：`AGENT_RECOVERABLE_RUNS_ENABLED` 默认关闭；启用后创建状态失败时不调用模型，提交状态失败时不返回成功，模型完成后的对话落库失败会记录为 `failed`。请求取消时使用独立 3 秒提交窗口尝试落终态。该基础增量先固定 `resume_supported=false`，只准确记录挂起态，不提前伪装恢复能力。
- [x] **P8.3 第六增量验证**：Repository/Service/Cmd 定向测试、Repository/Service `go test -race -p=1`、整仓 `go test -vet=off -p=1 ./...`、串行整仓 `go vet -p=1 ./...`、Compose 解析、Helm Lint 与 Agent Deployment 环境变量渲染均通过。首次并行编译被 Windows 工具链派生权限拦截，改用串行测试后通过并记录于 `docs/ISSUES.md`。
- [x] **P8.3 第七增量：版本化加密 Checkpoint**：Runtime 新增 `react.v1` Checkpoint/Resume 契约，完整保留消息、步骤、Usage 与待处理动作但不保存 Tool Definition；状态密文使用独立 AES-256-GCM Keyring、租户与 Run 绑定 AAD、SHA-256 摘要及 256 KiB 默认硬上限。Checkpoint 缺失、损坏、超限、版本未知或待处理参数疑似包含凭据时 fail-closed。
- [x] **P8.3 第七增量：原子恢复与重新授权**：Mongo 通过 `status + revision + lease + attempt_id` CAS 领取 `ask_human` Run，过期租约可回收且旧 Attempt 不能提交。恢复继续累计原 Step/Token/成本，不重放已成功 Step；每次恢复重新解析 Profile/Prompt 并装配当前用户仍获授权的只读 Tool，版本漂移或授权撤销不会沿用 Checkpoint 中的旧能力。
- [x] **P8.3 第七增量：查询、恢复与 Web 连续对话**：新增租户隔离 `GET /agent/runs/:id` 与 `POST /agent/runs/:id/resume`，响应不暴露 Checkpoint/租约/Attempt。AI 助手重载历史后查询权威 Run，用户下一条消息可继续当前 `ask_human` Run；恢复失败时撤销乐观消息、恢复输入并重新读取服务端状态，若响应丢失但权威 Run 已完成则重载已持久化对话。该增量当时仍关闭 `approval_required` 与 Unified Agent 高风险/写工具，已由第八增量补齐。
- [x] **P8.3 第七增量验证**：Runtime/Credential/Repository/Service/gRPC/Gateway 目标 `go test -race -p=1`、整仓 `go test -vet=off -p=1 ./...`、串行整仓 `go vet -p=1 ./...` 和 Web `vue-tsc + Vite` 生产构建通过；Compose 解析、Helm Lint 及恢复关闭/启用 Keyring 两种 Agent Deployment 渲染通过。测试使用内存仓储、Fake Runner/Tool，不连接真实模型或第三方 MCP。
- [x] **P8.3 第八增量：Unified Agent 工具审批恢复**：新增默认关闭的 `AGENT_UNIFIED_APPROVAL_RECOVERY_ENABLED`，并要求可恢复 Run 与外部 MCP 同时启用。受治理 Catalog 可向 `runtime.external_mcp` 提供当前用户已授权的 `read/risky/write` 工具；Runtime 对待审批 `tool_call` 保存类型化 Checkpoint，批准后恢复同一步内的精确动作，不重跑产生动作的模型或此前成功步骤。
- [x] **P8.3 第八增量：权威绑定与一次性授权**：Agent Run 状态版本升级为 2，绑定 Pending Action、审批 ID、输入摘要、稳定幂等键与审批时限；Resume Grant 按 Run Revision 原子签发、只保存哈希并轮换旧令牌，恢复通过状态/Revision/审批/令牌/租约/Attempt 联合 CAS。拒绝或过期会终止 Run 并清除 Checkpoint、授权和租约。
- [x] **P8.3 第八增量：调用前重新授权与副作用边界**：授权签发、领取恢复和真实调用前重新读取当前 Profile/Prompt、Connection、Credential Version、Active Snapshot、Schema、Tool Policy 与审批绑定。`risky` 固定一次远端尝试；`write` 必须具备已审核的声明式幂等参数并在有限重试中复用平台稳定键。恢复只新增 assistant 消息，不伪造用户消息。
- [x] **P8.3 第八增量：跨层审批体验与离线验证**：Proto/gRPC/Gateway/Web 复用既有 Tool Approval 事实源；审批收件箱按 Workflow/Runtime 来源签发对应授权并立即在内存中消费，Agent 响应与授权端点设置 `no-store` 且不回显已消费令牌。离线测试覆盖错误令牌、单次消费、重放拒绝、授权漂移、风险重试、写入稳定键、拒绝/过期终止和 Gateway 契约；目标竞态、整仓测试、串行 Vet、Compose、Helm 与 Web 生产构建通过。应用内浏览器验证桌面与 390x844 审批抽屉空态/服务不可用状态；本地 Agent API 返回 500，因此未计为审批 API 端到端验收。
- [x] **P8.3 第九增量：持久 Agent 项目与真实成员校验**：新增独立 `agent/project` 领域、`agent_projects` Mongo 集合和 Revision CAS 成员表；角色固定为 `owner/editor/viewer`，Owner 不可移除或降级。新增/更新成员通过 User Service 精确校验目标用户，目录故障时 fail-closed；Gateway 只使用 JWT 操作者和路径目标用户，HTTP 用户 ID 保持字符串。
- [x] **P8.3 第九增量：项目级 MCP 共享与实时撤权**：`AGENT_EXTERNAL_MCP_PROJECT_SCOPE_ENABLED` 默认关闭并要求外部 MCP 同时启用。Owner/Editor 可治理项目连接，Viewer 可查看并使用；列表、按 ID/Server 解析、Workflow/Unified Agent 目录和真实调用均重新读取当前成员关系。移除成员后调用立即被拒绝，Connection/Snapshot 继续以创建者用户 ID 绑定加密 AAD，其他成员永不读取凭据。
- [x] **P8.3 第九增量：跨层体验与离线验证**：Proto/gRPC/Gateway/Web 增加项目、成员及连接作用域契约；MCP 管理面支持个人/项目创建、角色感知只读和项目成员管理。Manager/Fake 覆盖真实用户存在性、Owner 不变量、CAS、Editor 管理、Viewer 使用和撤权后目录/调用拒绝；Gateway 覆盖 JWT 身份、路径成员、CAS 与大整数 JSON。目标包 Race、整仓测试、串行 Vet、Web 生产构建、Compose 解析、Helm Lint、默认/开启渲染和非法开关 Guard 均通过；浏览器在 1440x900 和 390x844 验证 MCP 弹窗无横向溢出或控件越界。本地 Agent API 仍返回 500，因此未把真实 Mongo/User Service 项目创建计为端到端验收。
- [x] **P8.3 第十增量：部署托管项目凭据**：新增严格 JSON Registry 与只读文件 Resolver，只允许项目级 Bearer 连接以 `credential_source=managed + managed_credential_ref` 精确绑定项目、Endpoint 和认证方式。Connection 仅保存引用/版本，不保存托管 Token 或密文；未知字段、重复引用、路径越界、绑定不一致和缺失 Secret 均 fail-closed。
- [x] **P8.3 第十增量：轮换与会话隔离**：Discovery、健康巡检和真实调用均按次解析托管 Secret；Session Identity 绑定 Connection、Registry Version 与当前 Token 的单向摘要，同版本文件轮换后新调用不会复用旧 Session，更新/撤销会失效该 Connection 的全部身份。Registry Version 漂移会 fail-closed，Owner/Editor 通过 CAS 重新保存后才采纳新版本并清空 Snapshot/Policy；用户与托管来源也可显式 CAS 迁移。
- [x] **P8.3 第十增量：跨层与部署契约**：Proto/gRPC/Gateway/Web 追加兼容字段，项目连接管理面可选择用户或部署托管引用；`.env`、Compose 和 Helm 增加默认关闭开关，Helm 只读挂载现有 Secret 并拒绝非法开关组合、空 Registry 或缺失 Secret。离线测试覆盖 Registry 严格校验、项目/Endpoint 绑定、Secret 轮换身份、存储无密文、来源迁移、池全身份失效和 HTTP 不泄密。
- [x] **P8.3 第十增量验证**：Remote/Repository/Service/gRPC/Gateway 目标测试与 `go test -race -p=1`、整仓 `go test -vet=off -p=1 ./...`、串行整仓 `go vet -p=1 ./...`、Web `vue-tsc + Vite` 生产构建、Compose 解析、Helm Lint、合法只读 Secret 渲染和非法配置 Guard 均通过。验证只使用临时文件、内存/Fake 和模板渲染，不冒充真实 Kubernetes Secret 轮换或第三方 MCP live 证据。
- [x] **P8.3 第十一增量：生产验收 Runner**：新增显式 `--allow-live` 的独立命令，复用生产 Endpoint Policy、MCP SDK 与有界 Session Pool，验证 Ping、Discovery、只读调用、可选声明式幂等写入/状态计数及文件凭据轮换新身份；写探针额外要求 `--allow-write`，配置不接收明文凭据。
- [x] **P8.3 第十一增量：Conformance、证据与部署**：新增只绑定回环地址的 MCP Conformance Server，提供确定性只读、幂等写入、状态查询和故障工具；报告只保存摘要/固定错误码并支持 HMAC 签名验签。Agent 镜像增加运维二进制，Helm 增加默认关闭、`backoffLimit: 0`、整卷 Secret 挂载和非 root 的一次性 Job。
- [x] **P8.3 第十一增量：离线验证**：本地真实 MCP SDK HTTP 集成与 Fake 覆盖 Bearer、协议、目录、只读调用、写权限、状态计数、轮换新身份、超时脱敏、严格配置、签名篡改和 CLI Guard；目标包与 `mcp/remote` Race、整仓 `go test -vet=off -p=1 ./...`、整仓 `go vet -p=1 ./...`、Compose 解析、Helm 默认/合法渲染及空配置/缺签名 Secret/轮换缺凭据 Secret Guard 均通过。命令级烟测生成并验签 `passed` 报告（5 通过、0 跳过、0 失败）；操作手册及故障矩阵见 `docs/agent_mcp_acceptance.md`。该证据验证框架，不冒充真实第三方或 Kubernetes 演练。
- [ ] **P8.3 剩余**：使用新增框架在受控环境完成真实第三方 MCP、Kubernetes Projected Secret 轮换与旧 Token 撤销、多副本/代理故障、远端幂等履约和项目成员跨服务链路验收；如需控制台动态维护 Registry，再单独设计管理员 RBAC、审计与 KMS/Vault Adapter，不能把部署配置冒充运行时密钥管理平台。
- [ ] **真实性边界**：公网搜索与外部 MCP 已具备代码和离线安全测试，但当前未使用真实 Brave 密钥或公网第三方 MCP Server 做 live 验收；外部 MCP 可描述为“受 Feature Flag 控制的个人/项目级治理、部署托管 Secret 引用、实时成员撤权、Workflow 与 Unified Agent 逐次审批恢复、声明式幂等写入”，远端声明不是服务端行为证明，不能描述成已开放 MCP 市场、完整 KMS/Vault 管理、严格 exactly-once 或完整执行生态。

## 2026-07-30 至 2026-08-01 P8.4 Workflow-as-Tool、Skill 与 Multi-Agent 增量

- [x] **显式发布与不可变绑定**：新增独立 `agent_workflow_tool_publications` Mongo 集合、租户唯一键与 Revision CAS；发布记录固定 Workflow Revision、DSL Hash、稳定 Tool Name、描述和 Input Schema。继续保存草稿不会隐式改变已发布行为，停用只撤出 Runtime Catalog，不删除 Workflow 或历史 Revision。
- [x] **可发布性与调用时复验**：发布和真实调用均重新编译 DSL/IR 并检查节点。持续拒绝补偿、`agent`、递归 Workflow Tool、未知工具和外部回调；只读工具不得审批，写工具必须逐次审批且声明幂等，风险工具必须逐次审批，审批型发布还要求完整恢复基础设施。外部 MCP 按当前 Snapshot/Policy 做同等校验。Input Schema 拒绝 `$ref` 和平台身份字段。
- [x] **Unified Agent 与治理复用**：新增 `workflow.run` Capability、`runtime.workflow` Profile 和动态用户目录；调用复用统一 ToolExecutor 的 Schema、超时、幂等结果、熔断、结果体积、审计与 Trace。子 Workflow Run 保存父 Agent Run/Action 谱系；父子预算独立记账，父 Tool Timeout 同时约束子运行。
- [x] **跨层发布控制**：Proto/gRPC/Gateway/Web 增加发布、查询和停用契约；Workflow Editor 显式展示“发布给 Agent”与绑定版本。`AGENT_WORKFLOW_AS_TOOL_ENABLED` 默认关闭，关闭时新发布、目录发现和调用 fail-closed，但保留查询与停用路径；Compose、Helm 与 `.env.example` 已同步 Catalog 数量和 Tool Timeout。
- [x] **离线验证**：Repository/Service/gRPC/Gateway/Cmd 普通回归通过；Repository race、Workflow-as-Tool Service 目标 race、gRPC/Gateway 全包 race 和受影响包 Vet 通过。Web `vue-tsc + Vite` 生产构建、Compose 解析、Helm Lint 与开启态环境变量渲染通过；应用内浏览器验证桌面与 390px 视口，发布按钮可见且无水平溢出。测试使用内存仓储/Fake Runner，不连接真实模型、Mongo 或第三方 MCP。
- [x] **P8.4 第二增量：版本化 Skill 契约与只读目录**：新增独立 `agent/skill` 领域契约，由当前用户 Active Workflow-as-Tool 发布记录确定性投影 Skill，不新增可漂移的 Skill 数据库。版本指纹固定绑定发布 Revision、Workflow Revision/DSL Hash、单一允许工具、Profile/Prompt、预算、指令和输出 Schema；目录只支持精确 `skill_id + version`，没有 `latest` 或关键词自动选择。
- [x] **P8.4 第二增量：受治理执行与审计**：新增 `skill.run -> runtime.skill` 精确路由。规划前、Runtime 前和工具真实调用前均重新解析 Active 发布并校验版本绑定；Runtime 只获得 Skill 固定的单一 Workflow Tool，完成态还要求成功 Tool Observation 与结构化输出通过 Skill Schema。权威 Agent Run、gRPC/Gateway/Web 响应均记录实际 Skill ID/Version。
- [x] **P8.4 第二增量：跨层目录与回滚**：新增租户隔离 `GET /agent/skills` 和精确版本查询 API，AI 助手提供显式 Skill 选择器；Proto 只追加兼容字段。`AGENT_SKILL_CATALOG_ENABLED` 默认关闭且依赖 Workflow-as-Tool 开关，关闭后目录和执行路由立即 fail-closed，不修改发布元数据，也不影响普通对话路径。
- [x] **P8.4 第二增量验证**：Skill/Repository/Service/gRPC/Gateway/Router/Cmd 定向普通回归、Skill/Service 目标 Race 和受影响包 Vet 通过；Web `vue-tsc + Vite` 生产构建、Compose 解析、Helm Lint、合法开启态渲染及非法开关组合模板期拒绝通过。应用内浏览器检查 `1280px` 与 `390px` 降级页面均无水平溢出；本地 Agent API 返回 500，因此未把 Skill 列表与真实 Workflow 调用计为端到端证据。
- [x] **P8.4 第三增量：成功任务显式模板化**：新增独立 `agent_task_templates` 集合和 `agent.task_template.v1` 契约。用户只能从自己的 `completed` 权威 Agent Run 主动创建模板，并自行提供含唯一 `{{input}}` 的指令；模板不复制对话或模型输出，只保存源 Run Revision、结果摘要、执行 Profile、Capability 及 Skill/Profile/Prompt 版本证据。
- [x] **P8.4 第三增量：不可变复用与审计**：模板创建支持租户级幂等键，内容不可变，归档使用 Revision CAS。执行前重新校验模板状态、源 Run 完成证据和当前 Catalog 路由，再进入统一 RunAgent；模型和用户 Provider Config 仍由当次请求选择。新 Run、gRPC/Gateway/Web 响应均记录实际模板 ID/Revision。
- [x] **P8.4 第三增量：跨层控制与 UI**：新增创建、列出、归档和执行 API，AI 助手可从任意带 Run ID 的历史完成消息读取权威状态后保存模板，并在输入区选择或归档模板。`AGENT_TASK_TEMPLATES_ENABLED` 默认关闭且依赖 Recoverable Runs；关闭执行时保留只读列表与归档，不删除模板。
- [x] **P8.4 第三增量验证**：Repository/Service/gRPC/Gateway/Router/Cmd 定向测试和受影响包串行 Vet 通过；Repository/Service 目标 Race 通过，Gateway Race 在关闭隐式 Vet 后通过且独立 Vet 已补齐。Web `vue-tsc + Vite` 生产构建、Compose 解析、Helm Lint、合法开启态渲染及非法开关组合拒绝均通过。应用内浏览器使用一次性契约夹具验证任务模板选择与归档入口，`1280px` 和 `390x844` 均无水平溢出；本地完整 Agent 依赖未启动，因此未把真实 Mongo、模型调用或模板执行计为端到端证据。
- [x] **P8.4 第四增量：父子 Run 版本化核算**：权威 Agent Run 与 Workflow Run 直接持久化预算上限、Step/Node、精确或估算 Token/成本、Pricing Version 和 `execution.accounting.v1`。Workflow 挂起/恢复继续使用累计 BudgetSnapshot；旧记录不从 `output_json` 或 Trace 猜测回填。
- [x] **P8.4 第四增量：租户隔离只读聚合**：新增 `GET /agent/runs/:id/accounting`，按 `user_id + parent_run_id` 专用索引有界查询直接子 Workflow Run，分别返回父级自身、子级和合计；不同价格版本标记 `mixed`，运行中、旧记录和截断查询显式降级为 `partial/unavailable`。该视图不改变父子独立准入预算，也不递归统计。
- [x] **P8.4 第四增量：助手用量入口与验证**：每条带权威 Run ID 的助手回复可打开只读用量弹窗。Agent/Gateway 扩大回归、目标 Service/Gateway Race、受影响包 Vet 与 Web 生产构建通过；应用内浏览器在 `1280x800` 和 `390x844` 检查无水平溢出。首次组合 Race 因 Windows 冷构建超过 5 分钟超时，拆分复跑通过；本地无可用模型与完整 Agent API，因此真实 Mongo 父子运行仍待受控环境端到端验收。
- [x] **P8.4 第五增量：Workflow-as-Tool 人工输入挂起/恢复**：Runtime 新增版本化 Tool Continuation 契约，父 Agent 在只读 Workflow Tool 遇到显式 `human_input` Wait 时进入 `awaiting_human`，把子 Run、父 Action、发布 Revision/DSL Hash 和子恢复凭证写入加密 Checkpoint。用户补充信息后恢复同一个子 Run，不重放模型决策、已成功 Action 或新建子 Run；子 Run 已成功但父提交中断时，从权威 Blackboard 幂等读取结果。
- [x] **P8.4 第五增量：调用时复验与编辑器契约**：恢复前重新校验当前 Active 发布、不可变 Workflow Revision/Hash、父子谱系和 Action 绑定；只允许问题非空且有界的 `resume_mode=human_input`，外部回调、用户提供恢复令牌、补偿、Agent 与递归 Workflow Tool 继续 fail-closed。编辑器明确区分“人工输入/外部回调”，不展示 Resume Token；组件库支持点击添加，并在移动端改为抽屉，属性面板改为覆盖层。
- [x] **P8.4 第五增量验证**：Agent 全模块与 `cmd/agent-service` 普通回归、Runtime/Tool/Service 目标 Race、受影响包 Vet 和 Web 两次生产构建通过。应用内浏览器真实添加 Wait 并切换两种等待类型，`1280x800` 与 `390x844` 均无页面级水平溢出；当前本地 Workflow Catalog API 仍返回 500，因此未把浏览器检查描述为真实 Mongo/Agent/子 Workflow 端到端验收。
- [x] **P8.4 第六增量：子 Workflow 审批桥接**：Runtime Tool Continuation 增加 `delegated_tool_approval`；受治理写/风险节点由子 Workflow 独占审批事实、输入摘要、幂等键与一次性 Resume Grant。父 Agent 加密 Checkpoint 只保存子审批引用，审批中心签发子 Grant 后调用父 Resume，由父 Runtime 在原 Tool Action 内恢复同一子 Run，不创建第二条审批或父级令牌。
- [x] **P8.4 第六增量：父子一致性与失败收口**：父 Run 使用 Revision CAS 和委托恢复类型绑定子审批；恢复前复验用户、父子谱系、Action、发布 Revision/DSL Hash、审批状态/摘要/幂等键和 Grant 哈希。子 Run 已成功但父提交中断时从权威 Blackboard 回放，写工具持久幂等结果保证不重复执行；拒绝或过期同步终止父子 Run并清理恢复状态，治理计数正确累加。
- [x] **P8.4 第六增量：发布与审批中心**：写工具只有“逐次审批 + 声明幂等 + 完整恢复桥”同时成立才可发布，风险工具要求逐次审批和完整恢复桥；外部 MCP 继续受当前 Snapshot/Policy 约束。审批收件箱识别 `invocation_source=runtime` 的子 Run，批准后恢复父 Agent；直接 Runtime 审批路径保持不变。
- [x] **P8.4 第六增量验证**：端到端内存测试覆盖父挂起、子审批、一次性授权、原 Action 恢复、写工具仅执行一次、父子完成及令牌重放拒绝；另覆盖非幂等写发布拒绝和子审批拒绝同步终止父 Run。`go test ./internal/module/agent/...`、Runtime/Service 拆分 Race、`go vet ./internal/module/agent/...` 与 Web `vue-tsc + Vite` 生产构建通过。组合 Race 外层超时已拆分复跑，不连接真实模型、Mongo 或第三方 MCP。
- [x] **P8.4 第七增量：Multi-Agent 影子准入**：新增独立 `agent/strategy` 领域包，以确定性复杂度信号匹配版本化研究/草拟角色模板；高复杂度精确路由进入 `multi_agent` 候选，随后逐项校验 Profile Tool Scope、角色/并发上限、显式 Step/Token/成本预算和延迟预算，任一不满足即记录原因并回退。计划不保存原始问题、Prompt、凭据或工具参数。
- [x] **P8.4 第七增量：证据与安全回退**：权威 Agent Run、`RunAgent`/Run 查询 Proto、Gateway 和 Web DTO 追加脱敏 `agent.execution_strategy.v1` 证据，包含稳定原因码、角色 Tool Scope、硬预算、估算延迟与 SHA-256 摘要。`AGENT_MULTI_AGENT_PLANNER_ENABLED` 仅开启影子准入；聚合生命周期执行器尚未安装，复杂任务固定记录 `multi_executor_unavailable` 并执行原单 Agent 路由，旧 `MultiAgentPublishTwitter` 未被复用。
- [x] **P8.4 第七增量验证**：Planner 全边界单测、Service/Repository/gRPC/Gateway 契约测试和 Agent 全模块普通回归通过；Strategy/Service 及 Repository/gRPC/Gateway `go test -race`、Agent/Gateway/Cmd `go vet`、Web `vue-tsc + Vite` 生产构建、Compose 解析、Helm Lint 和开启态环境变量渲染通过。验证使用 Fake/内存计划，不连接真实模型、Mongo、搜索 Provider 或第三方 MCP。
- [x] **P8.4 第八增量：有界多角色聚合执行**：为两个精确研究草拟模板安装顺序 `researcher -> drafter -> reviewer` 执行器；每个角色使用独立 Profile、消息、工具交集和硬预算，研究证据通过有界摘要与结构化 Citation 交接，起草/审校不接收历史会话噪声且无工具权限。执行复用既有 Runtime、Model Router 与 ToolExecutor，不复用旧硬编码 Multi API。
- [x] **P8.4 第八增量：父级治理与失败语义**：共享 Runtime `BudgetTracker` 封顶父级累计 Token/成本，三段 Usage/Step 合并到同一权威 Agent Run；Trace 按父 Run ID 和角色化 Step ID 关联。真实执行由 `AGENT_MULTI_AGENT_EXECUTION_ENABLED` 独立控制并依赖 Planner 与 Recoverable Runs；关闭后立即回到影子准入/单 Agent。任一角色失败、挂起或请求审批都整体失败，不进行消耗后的隐藏回退。
- [x] **P8.4 第八增量验证**：成功路径覆盖角色消息隔离、工具最小权限、结构化交接、父预算、聚合 Run 与单次会话持久化；失败路径覆盖中途终止、禁止隐藏重跑和失败用量归集。Agent 全模块与 `cmd/agent-service` 普通回归、Runtime/Strategy/Service Race、受影响包 Vet、Compose 解析、Helm Lint、合法开启态及两类非法依赖组合均通过。验证使用 Fake Runtime/Tool 和离线配置，不连接真实模型、Mongo、搜索 Provider 或第三方 MCP。
- [x] **P8.4 第九增量：单/多策略对照门禁**：复用 `agent-task-eval` 增加版本化 `agent.strategy-comparison.v1`，在同数据集、Case/模板覆盖、Provider、Model、Pricing Version、环境、Seed 与超时全部一致时，比较单 Agent 和多角色的语义质量、任务/工具回归、平均估算成本与 P95。成本/延迟证据不完整或混入非目标模板时返回 `ineligible`；错误、预算终止、越权写或伪造工具结果直接失败。
- [x] **P8.4 第九增量：固定任务与可回滚 CLI**：新增 20 条只读研究草拟任务，均绑定 `platform.research_draft.v1` 或 `web.research_draft.v1`，并提供单/多录制资源夹具验证评分契约。`--strategy-gate` 只计算，`--enforce-strategy-gate` 才以非零退出码阻断；策略判定进入既有 HMAC 签名与 WORM 归档载荷。新增字段均可选，旧 v2 签名报告保持可验。
- [x] **P8.4 第九增量验证**：领域测试覆盖通过、质量/安全/成本/P95 失败、证据不可比、倍率边界、资源聚合和旧签名形状；CLI 测试覆盖签名策略报告、强制门禁非零退出与缺稳定证据拒绝。Agent 全模块、`agent-task-eval`/`agent-service` 普通回归、Eval/Cmd Race 和 Vet 均通过；完整离线命令同时通过原质量门禁与策略门禁。夹具中的 2000 bps 语义增益、2.3838x 成本和 2.5410x P95 仅为契约自测。
- [x] **P8.4 第十增量：生产/Eval 共用多角色核心**：新增无 Service、Mongo、MCP SDK 依赖的 `internal/module/agent/multirole`，统一顺序角色隔离、受限证据交接、共享父级 Token/成本预算、Usage/Step 聚合与失败即停语义。生产 `multi_agent_execution` 改为薄适配层，继续负责目录、Citation、Trace 与权威 Run 持久化，不再维护第二份角色编排算法。
- [x] **P8.4 第十增量：同配置真实模型策略对照**：`agent-task-eval --strategy-runtime-config` 在一个规范化配置内固定 Provider、Model、Pricing 和两个模板的单 Agent/研究/草拟/审校 Profile Snapshot，自动运行 Multi 候选与 Single 稳定侧并写入相同配置哈希。明文凭据字段严格拒绝，Case Timeout 最少 50 秒；不同配置哈希直接 `ineligible`，Web Case 可在 `allowed_tools` 内按需调用 `page_read`，但不得越出声明工具集合。
- [x] **P8.4 第十增量验证边界**：多角色核心测试覆盖角色/历史/工具隔离、结构化交接、父/角色预算预检与失败不回退；CLI 本地 OpenAI-compatible HTTP 集成覆盖真实 Runner 六次模型请求、签名报告、同配置哈希和双侧通过。Multirole/Eval/CLI Race、Service 关键路径 Race、受影响包 Vet、Agent 全模块和整仓 `go test ./...` 均通过；离线双门禁命令实跑为 `passed/passed`。配置样例只引用环境变量，Search/Page Read 使用确定性结构化证据沙箱。因此本增量证明真实模型调用适配器与编排可运行，不证明任何外部搜索质量、真实 Provider 收益或生产 P95。
- [x] **P8.4 第十一增量：长运行 Live Eval 可恢复护栏**：评测核心新增脱敏 `AgentTaskCaseEvidence`、连续前缀恢复和逐 Case Observer，可仅凭报告安全字段与工具调用计数重建完整聚合指标。Live CLI 在首个未完成 Case 前通过真实 Provider/Model 执行一次无副作用 Chat/`eval_preflight` Tool Call，连接、模型标识或工具能力异常立即失败；Case 执行错误快速终止且不固化失败 Case。
- [x] **P8.4 第十一增量：签名检查点与故障恢复**：`--checkpoint-dir` 按 Candidate/Stable 写入序号连续、逐条 HMAC 签名并链接上一 Payload SHA-256 的追加记录。记录不含输入、回答、Prompt、工具参数或工具结果；恢复严格绑定 Side、数据集/配置 SHA、环境、Seed、Timeout 和执行描述，拒绝篡改、序号缺口、未知文件与配置漂移。端到端测试模拟第二 Case Provider 503，确认首次只保存第一 Case、恢复只重跑第二 Case并生成签名报告。
- [x] **P8.4 第十一增量验证**：Eval/CLI 定向测试、Eval 与 CLI 串行 Race、受影响包 Vet、Agent 全模块串行回归和 20 Case 离线双门禁均通过；离线结果保持 `quality_gate=passed`、`strategy_gate=passed`。真实 LM Studio 冒烟在约 5 秒内于预检阶段明确失败，未创建报告或检查点。首轮组合 Race 的 Windows `compile.exe Access is denied` 已记录为 Issue 122，并以相同 Cache、`-p=1` 串行复跑通过。
- [x] **P8.4 第十二增量：显式 ToolChoice 契约**：真实预检确认 LM Studio 在未声明 `tool_choice` 时会把正确的函数调用 JSON 放入普通文本。Runtime 新增标准化 `ToolChoice` 和仅首轮生效的 `InitialToolChoice`；OpenAI-compatible Adapter 映射 `auto/required/none`，预检、Single Research 和 Multi Researcher 首轮使用 `required`，工具 Observation 后及 Drafter/Reviewer 保持自动选择。Runtime、Model、Multirole 与 Eval CLI 定向测试通过，真实 Adapter/Router 探针均返回标准 `tool_calls`。
- [x] **P8.4 第十二增量：首份真实单/多签名对照**：经用户确认后，使用固定 LM Studio `qwen2.5-3b-instruct`、`controlled-accounting-v1`、相同 Profile Snapshot、20 条 `agent-strategy-cases-v1` 和 90 秒 Case Timeout 完整执行 Multi Candidate 与 Single Stable 共 40 次 Case。两侧任务完成率、工具成功率均为 100%，错误、预算终止、越权写和虚构工具结果均为 0；Multi 工具选择准确率为 90%，高于 Single 的 75%。
- [x] **P8.4 第十二增量：晋级门禁诚实失败**：Multi 语义通过率为 60%，低于 Single 的 90%；平均估算成本为 `3050.2 / 1158.2 = 2.6336x`，P95 为 `12449 / 3322 = 3.7475x`。失败主要来自跨角色交接后遗漏证据中的关键术语及输出超过 800 字，策略门禁以 `candidate_semantic_rate_below_policy`、`semantic_gain_below_policy`、`p95_latency_ratio_exceeded` 拒绝晋级。未放宽阈值，也未把失败报告描述成 Multi-Agent 收益。
- [x] **P8.4 第十二增量：本地签名复验**：脱敏报告写入 `tmp/agent-task-eval/live-strategy-qwen25-20260801-v1.json`，绑定数据集 SHA-256 `ff27daf0...775b`、执行配置 SHA-256 `85e88cfa...f322` 与 Key ID `local-live-eval-20260801-v1`；`--verify-report` 独立复验通过。逐 Case Candidate/Stable HMAC 哈希链检查点完整保留，正文、Prompt、工具载荷和凭据未写入报告。
- [ ] **P8.4 剩余**：当前没有受控 MinIO Object Lock 凭据，失败报告尚未形成 WORM Version ID 回执；Multi-Agent 执行资格继续关闭。下一次资格评测应先定义新的通用 Profile Revision 或由用户启动更强且支持 Tool Call 的固定 Chat 模型，再使用全新配置哈希/检查点运行，不得覆盖本次失败基线或放宽门禁。只有新报告通过、人工复核并 WORM 归档后，才单独验证生产 Search/Page Read 召回与公网 P95，并决定是否开发并行角色和角色级 Checkpoint/Resume。

## 2026-08-01 工作区基线冻结与交付门禁

- [x] **生成产物隔离**：根 `.gitignore` 增加 `/.cache/` 与 `/tmp/`，覆盖仓库内 Go 构建缓存、评测临时报告、渲染产物和开发服务器日志；未删除任何现有文件，也未忽略 `.agents`、`.codex/agents` 或新增源码。
- [x] **Agent 核心门禁**：`multirole/eval/runtime/service` 与 `agent-task-eval` 普通测试通过；四个并发核心包的 Race 结果通过，组合冷构建超时后按 Service 单包复跑闭环；相同目标范围的串行 Vet 通过。
- [x] **Agent 全模块门禁**：Agent 全模块、Agent Service 及 MCP/Memory/RAG/Router/Eval 运维命令使用仓库内缓存和 `-p=1` 串行回归全部通过，不连接真实模型、Mongo、MCP、Brave 或其他外部服务。
- [x] **整仓 Go 门禁**：`go test -p=1 ./... -count=1` 与 `go vet -p=1 ./...` 全部通过；首次编译器退出与组合 Race 超时已按环境抖动记录为 Issue 123，并通过拆分复跑证明没有稳定源码失败。
- [x] **Web 门禁**：`web` 的 `npm.cmd run build` 通过，`vue-tsc` 与 Vite 共转换 880 个模块并生成生产产物；Browserslist 数据陈旧提示不影响构建结果，本轮未执行无关依赖升级。
- [ ] **Mobile 门禁**：`flutter analyze` 在工具链启动阶段 300 秒无输出超时，`flutter --version` 也未在 30 秒内返回；当前不能声称移动端通过，环境阻塞见 Issue 124。
- [ ] **提交边界**：本轮完成可验证工作区基线，但尚未擅自暂存或提交用户工作区；需要按 Agent 核心、Workflow/RAG、P7 Eval、P8 能力生态、跨端/部署/文档拆分可审查提交，避免把数百项历史改动压成单一提交。

## 2026-08-01 P8.4 第十三增量：Profile Set v2 与更强模型资格配置

- [x] **Profile Set 原子灰度**：研究父 Profile 成为唯一 Release Anchor；研究员、草拟者和审校者不能独立灰度。Catalog/AtomicResolver 在单一不可变快照中按父版本精确解析整套 Profile，缺版本、重复成员和非法角色 Release 均失败关闭。
- [x] **Run 级版本固定**：`agent.execution_strategy.v1` 计划追加可选 `profile_set_anchor/profile_set_version` 并纳入 SHA-256 摘要；执行阶段按计划中的精确版本重取整套角色配置，即使 Catalog 在规划与执行之间刷新，本次 Run 也不会漂移。Multi-Role 核心在任何模型调用前再次拒绝混合版本。
- [x] **通用 v2 内容契约**：站内/公网 Single 与 Multi 角色使用 v2 候选 Prompt，优先读取结构化证据、保留精确术语、过滤无关材料；默认直接返回一份完整成稿，不再混入研究摘要、风格判断和适用场景。用户指定长度优先，未指定时中文通常 180-600 字或其他语言相当信息量。
- [x] **受限推理模式控制**：Eval Runtime 增加 `provider_default/disabled/enabled`；当前只为 DashScope 显式映射 `enable_thinking`，仅注入 Chat Completion 且冲突时失败关闭，不开放任意 Provider 请求字段。
- [x] **固定云候选配置**：新增 `agent_strategy_runtime_config.qwen37-v2.example.json`，固定 `qwen3.7-plus-2026-05-26`、Profile Set v2、关闭 Thinking 的 Tool Calling 评测形态、上下文/输出上限和版本化费率；凭据仅引用 `DASHSCOPE_API_KEY`。
- [x] **离线验证**：Profile/Strategy/Multirole/Model/Eval CLI 与 Service 目标测试、Agent 全模块普通回归、并发核心和 Service 目标 Race、受影响包 Vet 均通过；并发替换测试确认 Profile Set 不混合 Catalog 快照。全部使用 Fake/本地 HTTP 夹具，不调用 LM Studio、DashScope、Mongo、MCP 或搜索 Provider。
- [x] **真实资格评测已授权**：用户在本机提供 `DASHSCOPE_API_KEY` 并明确允许固定合成评测数据出站与相关费用；旧 qwen2.5 失败报告继续保留且未覆盖。最终结果与边界见第十四增量。

## 2026-08-01 P8.4 第十四增量：ReAct 收束护栏与 qwen3.7 资格报告

- [x] **最终步收束护栏**：ReAct 在保留最后一步生成最终答案时移除 Tool Catalog 与 `tool_choice`，追加高优先级终止指令；若 Provider 仍返回非终态动作，Runner 在执行工具前以 `max_steps_exceeded` 失败关闭。`InitialToolChoice=required` 且总步数不足 2 时在模型调用前拒绝，避免“必须调用工具”却没有结果生成步的非法配置。
- [x] **可复现评测修复**：首轮 qwen3.7 报告暴露 `agent_strategy_cases.json` 的 10 个 `allowed_tools` 被重复写入首个 Web Case、其余 9 个 Case 缺失字段。加载器现拒绝任意层级重复 JSON Object Key，固定夹具逐 Case 断言 `web_search` 必需且 `page_read` 可选；修复后的契约升级为 `agent-strategy-cases-v2`，旧 v1 报告仅保留为缺陷证据，不作为资格结论。
- [x] **固定云模型完整对照**：使用 DashScope `qwen3.7-plus-2026-05-26`、`enable_thinking=false`、Profile Set v2、Pricing `dashscope-cn-qwen3.7-plus-2026-05-26-cny-2026-08-01` 和 90 秒 Case Timeout 完成 Multi Candidate/Single Stable 各 20 Case。Candidate 任务完成、读工具选择、语义与工具成功率均为 100%，无错误、预算终止、越权写或虚构工具结果；Stable 语义为 95%，其余对应指标为 100%。
- [x] **策略门禁通过**：Candidate 相对 Stable 的语义增益为 500 bps，任务与工具回归均为 0；平均估算成本倍率 `1.0714x`，P95 倍率 `0.8870x`，Candidate P95 `15,707ms`。签名报告位于 `tmp/agent-task-eval/live-strategy-qwen37-20260801-v7.json`，绑定数据集 SHA-256 `55f14d6f...10e46`、执行配置 SHA-256 `429f5e44...63e60` 和 Key ID `local-live-eval-20260801-v3`，独立 HMAC 验签通过。
- [ ] **资格边界与后续**：本报告证明固定模型/Profile 在确定性无副作用 Search/Page Read 证据沙箱中满足两个只读顺序模板的准入门禁，不证明真实 Brave/Page Read 召回、公网 P95、任意模板、并行角色、角色级恢复或写工具协作。下一步先人工抽检 Case 摘要并将报告归档到受控 MinIO Object Lock 获取 Version ID；在此之前生产 Feature Flag 不自动开启。

## 2026-08-01 P8.4 第十五增量：加密人工复核材料

- [x] **正文与报告继续隔离**：`agent-task-eval` 不改变 v2 签名报告、逐 Case 检查点或进度日志契约；它们继续只保存输出 SHA-256、字符数和脱敏评分。仅在 CLI 组合层用 Executor 装饰器短暂捕获本次 Candidate/Stable 的输入与最终输出，生产 Runtime 和 `internal/module/agent/eval` 不新增正文持久化依赖。
- [x] **显式双门禁复核包**：新增 `--review-bundle`。它只允许 Live `--strategy-runtime-config` 单/多对照，要求 `--allow-review-content`、`--enforce-gate`、`--enforce-strategy-gate`、独立的 base64 32 字节 Review Key/Key ID 和全新 `--out`/Bundle 路径；禁止与 Checkpoint、报告归档或既有文件覆盖组合。双门禁未通过时只保留签名报告，不生成复核包。
- [x] **加密与不可替换绑定**：复核包使用 AES-256-GCM，AAD 和加密载荷共同绑定最终签名报告 Payload SHA-256、报告 Key ID、数据集版本/SHA、Candidate/Stable 执行描述及逐 Case 评分/输出哈希。外层只暴露 Schema、算法、Review Key ID 和报告摘要；输入与模型正文不以明文出现在 Bundle、报告、Checkpoint 或日志中，载荷/文件尺寸有硬上限。
- [x] **受控打开**：新增 `--open-review-bundle + --review-report + --review-output`，必须同时提供报告 HMAC Key 和独立 Review Key，先验签报告，再解密并逐 Case 核对正文 SHA-256/字符数和完整结果；明文只写入新的 `0600 + O_EXCL` 本地文件，不输出到终端。该文件只供人工查看，不等于人工签认或发布审批证据。
- [x] **离线验证**：单元测试覆盖密文不含输入/回答、报告摘要篡改拒绝、严格 32 字节 base64 Key、Bundle 不可覆盖和显式内容授权；本地 OpenAI-compatible HTTP 夹具完成两个模板的 Candidate/Stable 双门禁、加密 Bundle、原报告验签及受控打开。`go test ./cmd/agent-task-eval -count=1` 已通过，全程未调用 DashScope、LM Studio、MinIO、Mongo、MCP 或公网搜索。
- [x] **复核材料后续已接续**：历史 `live-strategy-qwen37-20260801-v7.json` 按原隐私设计仍没有正文，无法从哈希逆向恢复，也不会伪造对应 Bundle。经用户重新确认费用与敏感正文捕获后，全新报告/Bundle 的完整重跑和机器辅助正文审阅已在第十六增量完成；外部人工签认与 WORM 仍未完成，且因内容资格被否决暂不进入晋级归档。

## 2026-08-01 P8.4 第十六增量：qwen3.7 全量正文复核与门禁校准

- [x] **全新付费资格运行**：经用户确认固定合成评测数据出站、敏感正文捕获和费用后，使用固定 `qwen3.7-plus-2026-05-26`、Profile Set v2、`agent-strategy-cases-v2`、全新报告/Bundle 路径且无 Checkpoint 完成 Candidate/Stable 各 20 Case；预检与全部 Provider 调用完成，无执行错误。
- [x] **签名与密文链路复验**：报告 HMAC、Payload SHA-256 `f1f96e91...267eb`、AES-256-GCM Bundle 绑定、数据集/执行配置身份及 40 个输出哈希/字符数均独立校验通过。评测和 Review Key 只以当前 Windows 用户 DPAPI 密文保存在忽略的 `tmp` 路径，明文仅进入执行进程环境。
- [x] **自动指标**：Candidate 20/20、Stable 18/20；语义通过率 `100%/90%`，平均 Token `3065.90/3523.95`，估算成本 `0.190004/0.184500 CNY`，P95 `16166/17356ms`。策略门禁记录语义增益 1000 bps、成本倍率 `1.0299x`、P95 倍率 `0.9315x`，总估算费用约 `0.374504 CNY`。Stable 两个 Web Case 因 839/848 字符超过 800 上限失败。
- [x] **40 份正文机器辅助审阅**：逐一对照输入、必需/禁用词和长度契约。未发现明显跨主题混入，但确认工具证据仅是“输入 + 必需关键词”；Candidate 12/20 返回证据不足并夹带评测占位元数据，其余内容也无法由证据验证；Stable 普遍以模型常识补写，存在无来源支撑的归因。该检查不冒充外部人工签认。
- [ ] **生产资格否决**：自动双门禁通过不能覆盖正文审阅结论，本报告固定标记为“内容资格不通过/不可判定”，不启用 Multi Feature Flag，也不作为 WORM 晋级对象。下一轮先开发 v3 实质证据夹具、Claims/Citations、证据不足分支、最终交付可用性、内部元数据泄漏和 groundedness 门禁；离线契约稳定后再申请云模型重跑。
- [ ] **受控环境后续**：本轮没有启动 MinIO、LM Studio、Docker 或其他用户侧软件。外部人工签认、Object Lock Version ID、真实 Brave/Page Read 召回与公网 P95 仍未完成。

## 2026-08-01 P8.4 第十七增量：v3 实质证据与内容资格门禁

- [x] **向后兼容的 Evidence Contract**：`AgentTaskCase` 新增可选 `evidence`，区分 `sufficient/insufficient`，并对 Evidence/Citation/Claim ID、URL、数量、字符预算、重复值、未知引用和 Claim 是否由同一条引用正文共同支撑做严格加载校验。原始证据只参与数据集哈希与内存评分，不进入报告、Checkpoint 或日志；签名报告 schema 继续为 `agent-task-eval-report/v2`，旧 v2 数据集和历史报告保持可验。
- [x] **确定性内容门禁**：充分证据任务要求全部 Claim Terms 出现在最终答案，且精确 `[CitationID]` 位于声明前后 240 字符窗口内；同时识别“证据充分却拒答”、固定无依据声明和内部评测元数据泄漏。证据不足任务必须明确返回“未检索到可靠证据/现有证据不足/没有可核验来源”之一，并受独立长度和无依据短语约束。失败继续写入既有 `semantic_failure_codes`，不扩张报告契约。
- [x] **20 条独立 v3 数据集**：新增 `agent_strategy_cases_v3.json`，包含 8 条站内实质证据、8 条公网实质证据和 4 条空证据任务；原 v2 文件不覆盖。充分证据覆盖并发、可观测性、RAG、MCP、Workflow 恢复、成本预算、HPA、Context、供应链、工具链升级、Secret 轮换和引用治理等可核验事实与权衡。文件 SHA-256 为 `72a3e436...a42557`。
- [x] **Runtime v5 证据投影**：评测沙箱将 v3 数据投影到既有 `platform.tweet_search.v1`、`web.search.v1` 和 `web.page.v1`，没有新增第二套 Tool Schema；`page_read` 只允许读取 Case 白名单 URL。多角色 Handoff 使用数据集精确 Citation ID；空检索以明确 `no-evidence` 控制记录继续进入 Drafter/Reviewer，不伪造成外部来源，也不把合法证据不足误判为执行器故障。Runtime/Strategy Executor 身份升级到 v5，历史 v4 报告不改写。
- [x] **固定 Profile Set v3**：新增 `agent_strategy_runtime_config.qwen37-v3.example.json`，仍固定 `qwen3.7-plus-2026-05-26`、关闭 Thinking、版本化费率与环境凭据引用；Single/Researcher/Drafter/Reviewer 均要求声明旁精确引用，空结果禁止用模型常识补写。文件 SHA-256 为 `0fdb6454...be8c6b`。
- [x] **离线验证**：加载器拒绝未知 Citation、无正文支撑 Claim、非法空证据结构和非只读 Case；评分覆盖缺 Claim、缺引用、引用距离过远、充分证据拒答、无依据声明、元数据泄漏与空证据装作有答案。20 条 Grounded Fake 全部通过；Platform/Web/Page 投影、URL 白名单和 Multi `no-evidence` 通过。定向普通测试、`race`、影响范围五包测试和目标 `go vet` 均通过，全程未调用 DashScope、LM Studio、MinIO、Mongo、MCP 或公网。
- [x] **资格边界与后续**：确定性 Terms/短语/240 字符邻近度只能提高内容质量下限，不能独立证明开放域事实正确性、完整性或写作品质。绑定报告、数据集和规则版本的签认/Judge 契约已在第十八、十九增量完成，v3 云模型运行已在第二十增量完成；外部人工签认和 WORM Version ID 仍未完成，生产 Multi Feature Flag 继续关闭。

## 2026-08-01 P8.4 第十八增量：版本化内容签认闭环

- [x] **独立 Signoff 契约**：新增 `agent-task-content-review-decision/v1`、`agent-task-content-review-signoff/v1` 与 `agent-task-content-review-rules/v1`。签认独立于 `agent-task-eval-report/v2`，绑定报告 Payload/Key ID、数据集版本与哈希、Candidate/Stable 执行配置哈希、Review Bundle Schema/Key ID/文件哈希、决策哈希、审阅时间和逐 Case 输出哈希，不修改历史报告或数据库。
- [x] **完整覆盖与确定结论**：每个 Candidate/Stable Case 都必须给出事实正确性、相关性、证据忠实度和写作质量 `pass/fail`；Case 缺失、重复、错序、非法维度、总结果与维度不一致、未知字段、重复 JSON Key、报告/Bundle 替换和签名篡改均 fail-closed。签认不保存 Input、Output 或自由文本备注。
- [x] **人工/Judge 隔离**：外部人工只保存假名 ID、`asserted_external` 和外部审阅记录 SHA-256；这不是独立身份认证。Judge 必须绑定 Provider/Model/Prompt/Config SHA-256，且永远只作为辅助信号，不能产生 `external_human_approved=true`。外部人工批准仍只是生产资格必要条件，不替代 WORM、真实搜索和发布审批。
- [x] **离线 CLI 闭环**：新增 `--create-review-signoff` 与 `--verify-review-signoff`。两条路径先验报告 HMAC，再解密并逐 Case 核验 Review Bundle，最后使用第三把独立 HMAC Key 创建/验证不可覆盖 Signoff；显式拒绝报告、Bundle 与 Signoff 密钥或 Key ID 复用。验证过程不写第二份明文正文。新增默认全拒绝的 20 Case v3 决策模板。
- [x] **验证与兼容**：领域与 CLI 覆盖创建、严格解码、完整覆盖、结论推导、Judge 降级、报告/Bundle 替换、HMAC 篡改、明文不泄漏和显式内容授权。本轮仅使用 Fake/临时加密材料，不调用 DashScope、LM Studio、MinIO、Mongo、MCP 或公网。
- [x] **受控自动运行后续已执行**：用户已重新确认固定合成数据出站、Review Bundle 正文捕获和 qwen3.7 费用，第二十增量完成全新 v3 Candidate/Stable 20+20 并通过自动门禁。独立外部人工逐 Case 审阅、Signoff、Object Lock 归档和真实 Brave/Page Read 验收仍是后续硬门禁；此前生产 Multi Feature Flag 保持关闭。

## 2026-08-02 P8.4 第十九增量：内容签认进入 WORM 与发布资格链

- [x] **资格证据包契约**：新增 `agent-task-content-qualified-evidence/v1`，把已签名 `agent-task-eval-report/v2` 与 `agent-task-content-review-signoff/v1` 组合为同一不可变对象。构造与解码同时复验报告/Signoff 双 HMAC、报告/Bundle/逐 Case 摘要绑定和外部人工批准；Judge 即使给出 approved 也不能形成生产资格对象。严格 JSON 拒绝未知字段、重复 Key、密钥或 Key ID 复用。
- [x] **归档前完整复验**：`agent-task-eval` 新增 `--archive-content-signoff`。操作员必须同时提供原报告、加密 Review Bundle、Signoff、显式正文授权以及三把独立密钥；CLI 在内存中重新解密并逐 Case 核验 Bundle 后才上传资格对象。`--require-archived-content-signoff` 按精确 MinIO Version ID 回读并复验双 HMAC，无需把 Review AES Key 或正文交给服务进程。
- [x] **发布门禁与兼容回滚**：新增默认关闭的 `AGENT_PROFILE_EVAL_CONTENT_SIGNOFF_REQUIRED`。开启时依赖原 `AGENT_PROFILE_EVAL_EVIDENCE_REQUIRED`，Profile 提交、批准和失败恢复只接受外部人工已批准的资格对象；关闭时继续兼容旧裸报告回执。现有 Proto/Gateway/Web `quality_evidence` 回执字段不变，`report_sha256` 在新模式下覆盖整个资格对象，历史归档不迁移、不改写。
- [x] **部署与验证**：Compose、Helm 和 `.env.example` 增加独立 Signoff Secret/Key ID，Helm 非法开关组合模板期失败。Eval、ObjectStore、CLI 和 Agent Service 四包定向测试通过；Helm 默认模板与 Compose 配置解析通过。全程未调用 DashScope、LM Studio、MinIO、Mongo、MCP 或公网。
- [x] **真实自动证据后续已执行**：第二十增量已获得当前轮次的数据出站、正文捕获和费用授权，并生成通过自动双门禁的 v3 qwen3.7 报告与加密 Bundle。真实外部人工签认、Object Lock Version ID、真实 Search/Page Read 召回与公网 P95 仍未完成；生产 Multi Flag 保持关闭。

## 2026-08-02 P8.4 第二十增量：v3 云评测、失败诊断与 Profile Set v5

- [x] **失败门禁可加密诊断但不可签认**：`agent-task-eval` 新增显式 `--capture-failed-review-bundle`，只允许和 `--review-bundle --allow-review-content` 一起使用。自动门禁失败时可写入绑定签名报告的 AES-256-GCM 诊断 Bundle；领域 Signoff 创建仍强制质量门禁与策略门禁同时通过，失败 Bundle 不能形成外部人工 Signoff 或 WORM/Profile 资格证据。
- [x] **Profile Set v3/v4 诚实收敛**：固定 `qwen3.7-plus-2026-05-26`、同一 20 条 `agent-strategy-cases-v3` 的首轮 v3 为 Candidate `12/20`、Stable `8/20`，第二轮 v4 为 Candidate `16/20`、Stable `15/20`，两轮均只因 Candidate 语义通过率低于 90% 而拒绝晋级。加密诊断确认 v4 已修复 4 条空证据任务的过短回答和错误拒答，剩余问题收敛为跨角色交接遗漏数字、单位、否定结果或治理约束；没有放宽数据集、Terms、Citation 邻近度或策略门槛。
- [x] **不可变 Profile Set v5**：新增 `agent_strategy_runtime_config.qwen37-v5.example.json`。Single 稳定侧的 Prompt 正文、工具权限和预算与 v4 保持一致，只按原子 Profile Set 契约提升版本元数据；Multi Researcher/Drafter/Reviewer 增加按标点拆分的事实 Coverage Unit、精确来源短语、数字+单位、成对数值、零/否定结果和限制条款静默核对。配置文件 SHA-256 为 `026ff998...04615`。
- [x] **第三轮自动双门禁通过**：Candidate `19/20`、Stable `15/20`，两侧任务完成率、读工具选择和工具成功率均为 100%，无执行错误、预算终止、越权写或伪造工具结果。Candidate 语义率 95%，相对 Stable 增益 `2000 bps`；平均估算成本倍率 `1.7911x`，P95 倍率 `1.9362x`，Candidate P95 `15139ms`，均通过既定 `90% / 500bps / 3x / 3.5x / 60s` 门槛。唯一失败 `strategy-v3-web-003` 保留正确 Citation 和全部必需关键词，但遗漏来源精确短语“读写权限”，已留给人工复核。
- [x] **费用与完整性**：第三轮 Candidate/Stable 报告估算费用分别为 `0.176430 / 0.098508 CNY`，合计 `0.274938 CNY`；三轮完整 20+20 报告累计估算 `0.814168 CNY`，另有两次不进入报告的预检调用，因此 Provider 最终账单可能略高但仍预计低于授权的 `1 CNY`。报告 Payload SHA-256 为 `dc2b2500...971fb`，Bundle SHA-256 为 `1f8e5879...684b2`；独立 HMAC 验签、DPAPI Key 解密、Bundle 报告绑定和 40 份正文哈希均通过，正文未输出终端。
- [x] **本地验证与生产边界**：Eval/CLI 定向普通测试、Race、Vet 和 v5 Profile 快照回归通过；回归保证 Stable 除版本元数据外与 v4 一致，并拒绝 Profile Set 混合版本。当前仅取得自动评分资格，不是 `external_human_approved`；下一步必须由独立外部人工逐 Case 审阅 40 份正文并生成 Signoff，随后才能归档到 Object Lock、验证真实 Search/Page Read 和决定是否开启生产 Multi Feature Flag。
- [x] **授权数量边界已最小化处置**：费用仍在授权范围内，但调优过程曾产生 v4、v5 两份各含 40 份正文的加密 Bundle。用户确认后已删除 v4 诊断 Bundle以及 v4/v5 的 `review.opened.json` 明文；当前仅保留最终 v5 签名报告、40 份正文加密 Bundle 和 DPAPI 密钥，所有产物仍位于被忽略的本地 `tmp`。后续 Live Eval 必须同时记录运行次数、Provider 调用数、正文捕获数和费用硬上限。

## 2026-08-02 P8.4 第二十一增量：Live 授权预算与外部人工交付

- [x] **独立签名授权清单**：新增 `agent-task-live-authorization/v1` 与 `--create-live-authorization`。授权严格绑定 Provider/Model、数据集版本/SHA-256、执行配置 SHA-256、签发/过期时间，以及最大运行次数、Provider 调用数、正文捕获数和估算成本；创建命令只读取本地配置和数据集，不构造 Provider 请求。授权 HMAC 与报告 HMAC、Review AES Key 必须独立。
- [x] **调用前预算硬拦截**：所有 Live Runtime/Strategy Eval 必须显式提供 `--live-authorization` 和持久 `--live-authorization-state`。运行启动先预留 Run 与正文数量；每次模型调用根据保守输入 Token 上界、最大输出 Token 和固定 Pricing 在委托 Client 之前预留调用次数与估算成本。授权过期、身份漂移、预算耗尽、记录篡改、序号缺口或未知文件均在出网前 fail-closed，进程崩溃不返还已预留额度。
- [x] **Append-only 本地账本与报告证据**：每个授权 ID 使用不可覆盖事件文件、HMAC 签名和前向 Payload SHA-256 链累计消费，竞争提交通过原子文件创建/链接重试。最终 `agent-task-eval-report/v2` 可选携带授权 ID、授权 Payload 摘要、Key ID、调用实例摘要和批准上限，并受既有报告签名保护；旧离线/历史报告省略该字段，签名形状保持兼容。
- [x] **外部人工 Decision 交付模板**：`--open-review-bundle` 可同时使用 `--review-decision-template` 生成绑定报告与 Bundle 的 `agent-task-content-review-decision/v1` 初稿。模板不复制正文或输出哈希，Candidate/Stable 每个 Case 的事实正确性、相关性、证据忠实度和写作质量均默认失败，总 Verdict 默认拒绝，Reviewer ID、外部记录 SHA-256 和审阅时间必须由真实外部人工补全；原模板不能直接生成批准 Signoff。
- [x] **本地验证与外部边界**：Eval/CLI 定向普通测试和 Race、并发账本 20 次重复测试、完整 `internal/module/agent/...` 串行回归及 Agent/CLI Vet 通过；覆盖授权创建/绑定/过期、跨实例原子预算、预算耗尽、账本篡改、调用前拒绝、授权证据防篡改、旧报告兼容和模板失败关闭。本轮未连接 DashScope、LM Studio、MinIO、Mongo、MCP 或公网，也未产生新费用或正文。
- [ ] **剩余生产资格**：账本当前是同一受控工作区、同一持久 State Root 内的本地护栏，不是跨主机中央配额服务；操作员不得复制授权后切换 State Root。下一步仍必须由独立外部人工完成 40 份正文 Decision/Signoff，再取得 Object Lock Version ID 并验证真实 Search/Page Read；完成前生产 Multi Feature Flag、并行角色、角色级恢复与写工具多角色执行继续关闭。

## 2026-08-02 P8.4 第二十二增量：Live 离线额度计划与模型迁移护栏

- [x] **版本化离线资源计划**：新增 `agent-task-live-plan/v1` 与 `--plan-live-evaluation`。命令只读取数据集和规范化 Runtime 配置，不读取 API Key、不构造 Provider Client；输出精确 Provider/Model、数据集/配置摘要、分侧 Case/Strategy、共享预检、调用最小值与上界、Token/成本硬预算及正文捕获上限。
- [x] **授权覆盖门禁**：Live Authorization 签发不再接受小于完整计划上界的 Provider 调用数或估算费用；Review 正文配置为 `0` 时明确禁止捕获，配置为非零时必须覆盖 Candidate/Stable 全量正文。这样授权不会在已知 `MaxSteps`/Profile 预算内签出一个必然可能中途耗尽的批次。
- [x] **固定 qwen3.7 计划证据**：`qwen3.7-plus-2026-05-26`、20 条 v3 Case 与 Profile Set v5 的本地结果为 `121..241` 次 Provider 调用、`1,240,482` Token 上界、`4,701,348` 微计价单位费用上界和 40 份可选正文。该上界约束授权，不代表实际消费预测；历史 v5 完整运行实际报告费用仍以既有签名报告为准。
- [x] **模型替换隔离**：计划显式声明模型按 `provider + exact_model + execution_config_sha256` 固定。切到 `qwen3.7-plus` 等其他标识会生成不同配置哈希，旧授权无法匹配，且必须新建资格报告；不能只替换名称后沿用 `qwen3.7-plus-2026-05-26` 的结果。
- [x] **本地验证边界**：CLI/Builder 测试覆盖 v5 精确预算、无 Credential 离线计划、不可覆盖写、授权调用/成本/正文不足拒绝、正文零捕获和模型变更哈希；CLI 普通测试与 Race、完整 Agent 模块串行回归和受影响包 Vet 通过。本轮未调用 DashScope 或任何外部服务。生产资格下一步仍是独立外部人工 Signoff，随后才允许 WORM 和真实 Search/Page Read 验收。

## 2026-08-02 P8.4 第二十三增量：Redis 多实例 Live 授权账本

- [x] **可选共享后端且保持本地兼容**：`agent-task-eval` 新增 `file|redis` 授权状态后端；默认 `file` 继续使用既有不可覆盖 HMAC 前向哈希事件文件，Redis 代码只存在 CLI 运维边界，不进入通用 Agent Runtime、Service 或模型适配器。Redis 配置使用严格 `agent-task-live-authorization-redis-config/v1`，凭据只能引用环境变量，并支持显式 TLS Server Name 与有界连接/操作超时。
- [x] **初始化与消费分离**：新增 `--initialize-live-authorization-state`。它只验签已有授权并初始化 Redis，不读取模型 Credential、不构造 Provider Client。Live 运行只消费已存在且身份完全匹配的共享账本，从不自动初始化；未初始化、授权/Key/上限漂移、过期或 Redis 状态不完整均在 Provider 请求前失败关闭。
- [x] **跨连接原子预算与重试幂等**：单个 Lua 事务使用 Redis 服务端时间校验有效期，原子检查并累计 Run、Provider Call、正文捕获和估算成本，同时写入审计 Stream 与 Reservation 去重摘要。不同 Redis 连接并发共享同一额度；同一 Reservation 的网络重试不会重复计费，超限调用不会到达委托模型客户端。
- [x] **状态丢失防恢复与报告证据**：每个授权建立不设 TTL 的持久 marker。State Hash 或 Audit Stream 被逐键删除/提前驱逐时，运行和重复初始化都要求撤销授权，不能重置额度。Redis 报告证据新增可选 `state_backend=redis` 与 Namespace SHA-256；历史离线/file 报告省略字段，原 v2 签名形状保持兼容。Redis 管理员、`FLUSHALL` 和 marker 删除仍属于明确信任边界，生产要求 ACL、TLS、AOF/备份和 `noeviction`。
- [x] **本地验证与外部边界**：CLI/Eval 普通测试和 Race、不同 Redis Client 并发预留、初始化幂等、Reservation 重放、额度越界、State/身份篡改、未初始化调用前阻断、完整 `internal/module/agent/...` 串行回归及目标 Vet 全部通过。测试使用进程内 `miniredis`；本轮未连接用户 Redis、DashScope、LM Studio、MinIO、Mongo、MCP 或公网，未产生模型费用。
- [ ] **剩余生产资格不变**：共享账本消除了受控多实例评测的本地 State Root 分叉，但不替代独立外部人工对 40 份正文的 Decision/Signoff，也不替代 WORM Version ID 和真实 Search/Page Read 验收。完成这些外部证据前，生产 Multi Feature Flag、并行角色、角色级恢复和写工具多角色执行继续关闭。

## 2026-08-02 P8.4 第二十四增量：Redis Live 授权检查与撤销

- [x] **脱敏状态检查**：新增 `--inspect-live-authorization-state`，输出版本化机器可读 JSON，仅包含授权和 Namespace 摘要、状态、四类额度/用量、审计序号及 Redis 服务端时间。命令只验签授权并读取共享账本，不读取模型 Credential、不构造 Provider Client，也不回显 Redis 地址、凭据、Prompt 或正文。
- [x] **原子撤销与不可回退审计**：新增 `--revoke-live-authorization-state`、伪名操作人和固定原因码。正常撤销在单个 Lua 事务内保留累计用量、冻结 marker/state、递增序号并追加 `authorization_revoked` Stream 事件；Redis 只保存操作人 SHA-256。重复撤销不重复写事件，撤销后初始化和预算预留均明确失败。
- [x] **状态丢失事故收口**：State/Stream 不完整时只允许 `state_integrity_incident`，仅把持久 marker 标记为 `marker_only` 撤销并报告 `integrity_status=state_lost`；不会重建零用量 Hash/Stream。该模式能冻结授权，但不能恢复或证明丢失前的完整审计历史。
- [x] **本地验证边界**：CLI/账本普通测试与 Race 覆盖状态快照、用量保持、序号递增、并发预留/撤销串行化、撤销幂等、操作人脱敏、双状态防误恢复、事故原因门禁和状态不重建；并发用例连续 20 次通过。完整 `internal/module/agent/...` 串行回归与目标 Vet 通过。测试使用进程内 `miniredis`；本轮未连接真实 Redis、DashScope、LM Studio、MinIO、Mongo、MCP 或公网，未产生模型费用。
- [ ] **生产资格门保持不变**：这项运维能力不替代 40 份正文的独立外部人工 Decision/Signoff、Object Lock Version ID、真实 Search/Page Read 或公网 P95。资格链完成前继续保持生产 Multi Feature Flag 关闭。

## 2026-08-02 P8.4 第二十五增量：Redis Stream 逐事件重放审计

- [x] **原子审计边界与有界分页**：`inspect` Lua 快照同时捕获用量、序号、事件数和末端 Stream ID；Go 管理路径只分页读取到该游标，快照之后的并发 Reservation 不会混入旧状态。事件数受授权最大 Run/Provider Call 数反向约束，读取使用 512 条分页和 30 秒总审计超时。
- [x] **逐事件语义校验与状态对账**：重放要求精确事件字段集合、从 0 连续序号、严格递增 Stream ID、授权窗口内单调事件时间和唯一可选撤销终态；Run/Provider Call 增量形状、四类累计用量及撤销摘要必须与 Hash/marker 快照一致。成功输出 `replay_status=verified`、`verified_event_count`、`last_stream_id` 与规范化 `stream_sha256`。
- [x] **写入协议双层失败关闭**：Go 入口与 Redis Lua 同时约束 `run_reserved` 只能增加一次 Run（可预留正文），`provider_call_reserved` 只能增加一次 Provider Call（可预留费用），并拒绝非法事件类型、负增量和无效 Subject 摘要，避免语义畸形事件进入共享账本。
- [x] **篡改与并发边界回归**：新增“删除合法事件并补入伪造事件但保持 `XLEN` 不变”的平衡篡改用例，旧长度/序号关系仍成立时，重放会因用量对账失败；另用 521 条事件跨越分页边界，并在快照后追加事件，验证旧游标与摘要稳定。关键并发/分页/篡改用例连续 20 次、Redis 账本 Race、完整 CLI 与 Agent 模块串行回归及目标 Vet 全部通过。
- [ ] **信任边界与生产资格不变**：Stream SHA-256 是可比较的防篡改摘要，不是 HMAC 或 Redis 管理员不可抵赖证明；生产仍需最小 ACL、AOF/备份、`noeviction` 和 Redis 外 WORM 留存。外部人工 Signoff、Object Lock Version ID、真实 Search/Page Read 与公网 P95 未完成，Multi Feature Flag 继续关闭。

## 2026-08-02 P8.5 第一增量：Unified Agent 产品 SLI 基础

- [x] **权威生命周期计数**：新增可注入 `UnifiedAgentProductObserver` 与生产 Prometheus 实现。任务开始只跟随 `AgentExecutionRun` 成功创建，Outcome 及其耗时/用量只跟随 Revision CAS 成功提交；创建失败不计为已接受，提交失败不伪造成完成结果。
- [x] **完成任务成本与失败密度**：新增 `agent_unified_tasks_started_total`、Outcome、Duration、Step、Token、微单位成本和 Tool Result 指标，可按 `outcome=completed` 计算完成率、P95、每完成任务 Token/成本与失败 Tool 数。Tool Name、用户、Run、模型和错误原文不进入 Label。
- [x] **Citation 结构有效率**：对用户可见平台推文和网页 Citation 做固定类型、ID 与资源定位绑定校验，输出 `source_type + validity` 低基数计数。该指标只证明结构契约，不证明事实正确、网页在线或回答 Grounded；语义质量继续由 v3 Eval 和外部人工签认负责。
- [x] **离线与线上职责分离**：固定任务集继续负责工具选择准确率和任务级澄清率；线上 `awaiting_human` 仅作为可重复的澄清转换信号，生产遥测不猜测用户真实意图。指标当前随 `AGENT_RECOVERABLE_RUNS_ENABLED=true` 的权威生命周期启用。
- [x] **离线验证**：低基数/结构有效性/成功提交边界/工具失败投影目标测试、完整 Agent 模块回归、Service Race、`cmd/agent-service` 编译和目标 Vet 全部通过；未启动 Redis、LM Studio、MinIO、Mongo、MCP 或公网服务，也未调用 DashScope。
- [x] **P8.5 后续**：发布采纳率与 Connector 激活/复用率已由跨请求、可重放、幂等产品事件归因完成；不以发布调用次数、进程内缓存或 Session 打开次数冒充业务指标。租户已安装的联合扩展目录已由第三增量完成。

## 2026-08-02 P8.5 第二增量：草稿采纳与 Connector 产品漏斗

- [x] **权威草稿采纳状态**：`AgentExecutionRun` 持久化 `publishable_draft`、首次发布 Tweet ID 与时间。确认发布只接受已完成且可发布的来源 Run，并用 Mongo 原子条件更新实现跨请求幂等；重复确认和 TweetService 幂等重放不会重复计数。
- [x] **只追加产品事件**：新增 `internal/module/agent/product` 领域和 Mongo `agent_product_events` Repository。确定性事件 ID 支持进程重启后的重放与修复，事件不包含 Prompt、正文、Tool 参数/结果、Endpoint 或凭据。
- [x] **Connector 真实激活与复用**：配置成功产生 `configured`；首个已审核工具被显式启用才产生 `activated`；治理后的工具调用成功才产生 `first_used`。同一 Connector 必须在至少两个不同 Agent/Workflow Run 中成功执行才产生一次 `reused`，同 Run 重试和 Session Pool 复用均不算产品复用。
- [x] **低基数指标投影**：新增草稿 `ready/published` 与 Connector `configured/activated/first_used/reused` Counter，只使用执行 Profile、策略、作用域和传输类型等固定枚举 Label。Prometheus 用于趋势，严格 Cohort 分析保留在授权后的 append-only 事件层。
- [x] **离线验证**：完整 `internal/module/agent/...` 回归、Product/Repository/Remote MCP/Agent Service 目标 Race、目标 Vet 与 `cmd/agent-service` 编译全部通过；重放测试覆盖第二个 Run 的使用事实已落库但复用标记尚未落库的中断窗口。未启动 Redis、LM Studio、MinIO、Mongo、MCP 或公网服务，也未调用 DashScope。
- [x] **P8.5 目录后续**：Capability/Skill/MCP 联合目录已由第三增量完成；真实 Mongo 下的事件索引/重放、真实外部 Connector 漏斗和看板展示仍需独立受控验收。

## 2026-08-03 P8.5 第三增量：租户已安装扩展目录

- [x] **统一只读契约**：新增 `internal/module/agent/extension`，定义 `agent.extension.v1`、三类来源、稳定排序、过滤绑定 Cursor、最大 256 项和单页 50 项硬上限；纯领域测试覆盖分页、过滤漂移、重复 ID、非法条目和副本隔离。
- [x] **复用权威来源**：Service 只聚合不可变 Capability Catalog、当前租户精确版本 Skill 和受治理 MCP Tool。MCP 仍由当前成员关系、Active Snapshot 与 Enabled Policy 过滤，响应不携带 Endpoint、Credential、输入 Schema 或 Tool Result。
- [x] **API 与 Web 使用面**：Proto 仅追加 `listAgentExtensions`；Gateway `GET /api/v1/agent/extensions` 只信任 JWT 租户并透传稳定 Cursor。Web 助手工具栏新增扩展目录，Skill 使用前精确解析版本，MCP 仅跳转既有管理/审核界面。
- [x] **独立灰度与回滚**：`.env.example`、Compose 和 Helm 增加 `AGENT_EXTENSION_CATALOG_ENABLED=false` 与页大小配置。关闭只撤销目录读取，不改变 Workflow Publication、Skill 投影、MCP Connection/Snapshot/Policy 或任何执行授权。
- [x] **离线验证**：`extension`、Remote MCP、Agent Service、gRPC、Gateway Handler/Router 六包普通回归通过；Extension/Service/Remote MCP/Gateway 目标 Race 与七包串行 Vet 通过，Agent 组合根编译通过。Proto 重生成稳定，Compose 静态配置、Helm 正常渲染与非法目录上限拒绝、Web `vue-tsc + vite build` 均通过；未启动 Mongo、Redis、RabbitMQ、Temporal、LM Studio、外部 MCP 或公网服务，也未调用模型 Provider。
- [ ] **真实性边界**：第四增量已补发布者身份与包签名的只读市场基础，但本轮目录自身仍不是安装市场。安装审批、依赖解析、恶意扩展扫描、Artifact 分发和版本撤回仍需独立控制面；真实项目跨成员目录和远程 MCP 健康状态还需受控环境验收。

## 2026-08-03 P8.5 第四增量：签名公共扩展市场基础

- [x] **独立供应链领域**：新增 `internal/module/agent/marketplace`，定义发布者、公钥状态、规范 Manifest、不可变 SemVer Release、声明权限、稳定 Cursor 和 Ed25519 签名复验。版本 ID 绑定规范清单；发布者暂停、密钥撤销、签名/摘要篡改和查询条件漂移均 fail-closed。
- [x] **只读运行时边界**：新增两个独立 Mongo Collection；发布者注册和签名版本写入只作为未来离线/控制面 Repository 能力，Service 仅依赖只读 `CatalogStore`，批量读取发布者并对每个结果重新验签。市场不会写入 Capability、Skill、MCP、Workflow 或 Runtime 权限。
- [x] **无密钥 API 与 Web**：Proto 仅追加市场查询 RPC；Gateway `GET /api/v1/agent/marketplace/extensions` 只信任 JWT 并返回 `no-store` 投影。Web 新增搜索、类型过滤和 Cursor 加载更多的只读市场面板，不展示安装按钮，也不返回公钥、原始签名、Artifact URL/字节、Endpoint 或 Credential。
- [x] **独立灰度与回滚**：`.env.example`、Compose 和 Helm 增加 `AGENT_EXTENSION_MARKETPLACE_ENABLED=false` 与 1-50 页大小配置。关闭时不创建市场索引，不删除市场记录，也不影响租户已安装扩展目录。
- [x] **离线验证**：Marketplace/Repository/Service/gRPC/Gateway/Agent 组合根完整受影响包回归通过；Marketplace、Repository、Service、gRPC 与 Gateway 目标 Race 和七包串行 Vet 通过。Proto 重生成前后 SHA-256 一致，Web `vue-tsc + vite build`、Compose 解析、Helm Lint/开启态渲染及 `catalogLimit=51` 负向门禁通过；Vite 预览由 HTTP 200 与监听端口双重确认。测试未连接 Mongo、Redis、RabbitMQ、Temporal、LM Studio、模型 Provider、外部 MCP 或公网。
- [x] **发布治理后续**：第五增量已补发布者归属、独立内部认证、密钥轮换/吊销、签名发布、版本撤回和追加审计；公开目录与管理控制面仍保持独立开关。
- [ ] **完整市场仍未完成**：Artifact 存储与下载、安装审批、依赖解析、恶意包扫描、所有者转移、租户安装状态和真实 Mongo 多副本验收仍需后续增量；当前不能宣称开放可安装市场。

## 2026-08-03 P8.5 第五增量：扩展发布与信任控制面

- [x] **独立认证与所有权**：新增 Marketplace 专属内部令牌和平台管理员配置，不复用 Profile RBAC。平台管理员注册发布者及不可变所有者；后续每次密钥、发布与撤回操作都由 Agent Service 从 Mongo 重新校验 Owner，Gateway 只负责 JWT 操作者和内部令牌注入。
- [x] **公钥生命周期**：系统只接收 Ed25519 公钥。轮换使用 Revision CAS，将旧 Active Key 转为 Retired；Retired Key 只能维持历史验签，不能发布新版本。吊销是终态，并使相关历史签名不再可信。私钥不进入 API、Repository、审计或响应。
- [x] **签名发布与撤回**：发布者提交规范 Manifest 与离线签名，服务端重新规范化、要求 Active Key、生成稳定 Release ID 和发布时间；同 Package/SemVer 不可覆盖。撤回使用 `published -> withdrawn` 终态迁移，保留签名证据和唯一版本记录。
- [x] **CAS 与追加审计**：发布者、版本记录均增加 Revision 和操作者时间戳；所有写操作先后追加 `requested/succeeded/failed` 事件，审计只包含低敏对象 ID、固定 Action/Outcome/Reason/Error Code，不记录签名、Artifact、Credential 或内容。
- [x] **API 与管理页**：Proto 追加独立管理 RPC；Gateway 提供 Access、Publisher、Key、Release 与 Audit REST API并返回 `no-store`。Web 新增 `/agent/marketplace/manage` 独立控制台，公开市场仍只读且不出现安装按钮。
- [x] **独立灰度**：`AGENT_EXTENSION_MARKETPLACE_ADMIN_ENABLED=false` 可在公开目录关闭时单独编排版本；Compose 与 Helm 将同一 Secret 注入 Gateway/Agent，Helm 对 Secret 和管理员列表做模板期门禁。关闭不会删除发布者、版本和审计。
- [x] **离线验证**：Marketplace/Repository/Service/gRPC/Gateway 五层目标测试与拆分 Race、八个受影响包串行 Vet、Agent/Gateway 命令编译、Proto 重生成哈希、Web 生产构建、Compose 解析、Helm Lint/正负向渲染和 Vite 管理路由 HTTP 200 均通过；未启动或访问 Mongo、Redis、RabbitMQ、Temporal、LM Studio、模型 Provider、外部 MCP 或公网。
- [ ] **待受控验收**：真实 Mongo CAS/索引、多副本并发写、Secret 轮换和撤销后大目录可用性仍需受控环境验证。Artifact 分发、扫描、依赖解析、安装审批与租户安装继续明确不在本增量。

## 2026-08-03 P8 产品收口审计：范围冻结

- [x] **进度判定**：Runtime、Tool、Workflow、Approval、Replay、联网、远程 MCP、Workflow-as-Tool、受限 Multi-Agent、Eval 与扩展信任控制面的代码主干已形成；当前主要缺口是统一入口体验、真实环境价值链、可审查交付基线和可重复演示，不是组件数量。
- [x] **市场与学习双目标**：产品定位固定为“面向社交内容研究、创作与运营的可治理 Agent 工作台”；技术学习聚焦 Go Runtime、ReAct、MCP、DAG、恢复、RAG、预算和可观测性。核心价值链固定为取证、带来源草拟、审批发布、自动化和效果追踪。
- [x] **停止扩张**：公共市场安装链路、并行/角色级恢复、多角色写协作、SubWorkflow/MapReduce/Aggregator、事件触发器、代码沙箱、新 Provider 和新中间件均转为需求驱动延期项，不再作为默认下一轮。
- [x] **G0 交付基线执行中**：已将 24,903 条原始状态中的 24,280 条本地 Go Cache 隔离，并把剩余工作区审计为 607 个工程文件和 16 个明确排除项；`docs/agent/DELIVERY_BASELINE.md` 固定九个堆叠审查批次、跨阶段热点文件、验证矩阵和回滚边界。00 Repository Governance 已提交为 `ce9ad55`；01 Platform Reliability 完成 72 项精确暂存、Proto 哈希、普通测试、Race、Vet 与隔离索引快照复验后提交为 `849cb56`。02 Agent Core 将初始 146 项文件名分类收紧为 53 项真实依赖闭包，分别以 Runtime Foundation `79ceb30`、Runtime 持久化 `b23e164` 和安全 Provider HTTP 路由 `d4cd553` 提交；三个索引快照均通过目标测试，涉及并发的包通过 Race，目标 Vet 通过。后续文件已按真实依赖归入 03 至 06 并逐批收口；Mobile、学习文件、课程报告删除和临时脚本均未触碰。
- [x] **G0 03 Workflow/RAG 已提交**：依赖闭包重审后，以确定性 DAG `70d696e`、分层 RAG/Memory `42b889b`、统一 Tool 治理 `febf094` 和持久 Run/Repository `5708150` 四个提交完成 52 项；索引快照发现并修复节点取消与临时错误竞态，另补齐 Agent Run 核算版本依赖。Tool 的只读检索依赖先以 04A Search/Evidence `dc753cd` 提交 14 项。当前纯 `HEAD` 快照对 02-03/04A 的 19 个包联合普通测试、9 个并发敏感包 Race 和 19 个包 Vet 均通过，未启动真实模型、ES、Qdrant、Mongo、Redis、MCP 或公网服务。Workflow Service 与 Cognitive/Session 适配按真实依赖改归 06 集成审查。
- [x] **G0 04 Search/MCP 已提交**：Search/Evidence 前置 `dc753cd` 与项目权限 `1214f65`、远程 MCP Runtime `15f9a63`、Connector 持久化 `f8e52e7`、内部 MCP 安全工具面 `cd32cf0`、签名验收工具 `9d3f547` 合计完成 62 项。纯 `HEAD` 快照对 10 个目标包普通测试和 Vet 全部通过，各索引快照的并发敏感包 Race 通过；同时修复取消 Context 复用 Session、Provider Config 空 Collection、亚毫秒 Redis TTL、发推幂等缺失和验收体积无上限等问题。测试只使用临时回环 Conformance 服务，未启动或访问真实 MCP、Brave、Mongo、Redis、模型或公网。三个 Service 组合适配文件按依赖保留给 06/07，不属于 04 欠账。
- [ ] **完成门禁**：依次完成九批精确暂存、分批验证和提交，把 G0 从 `Prepared` 改为 `Complete`；随后完成统一助手体验收口、联网/MCP/Workflow-as-Tool 三条真实价值链验收，以及十分钟演示/失败演示/启动手册/产品指标看板。四道门禁完成后 P8 进入 Maintenance，不自动开启新阶段。
- [x] **规划同步**：`docs/agent/UNIFIED_AGENT_PRODUCT_PLAN.md` 第 10 节、`docs/AGENT_RUNTIME_STRENGTHENING_PLAN.md`、Agent Runtime Context 和 Technical Debt 已同步范围冻结与真实状态。

## 2026-08-03 P0 稳定性增量：Agent 运行角色与热点播报单一 Owner

- [x] **可测试启动计划**：新增 `internal/module/agent/startup`，严格解析 `AGENT_PROCESS_ROLE=api|worker|all` 与 `AGENT_TRENDING_REPORTER_OWNER=temporal|disabled`；空值使用兼容默认值，未知角色、旧 `local/auto` owner 启动即失败。
- [x] **顶层生命周期隔离**：`api/all` 才启动 gRPC、Consul、内部 MCP、Semantic Anchor 初始化和 API 本地异步实验记录器；`worker/all` 才连接 RabbitMQ 并启动内容归因 Consumer、实验/工具/补偿巡检、Temporal Worker 与影子风控。Profile Catalog 同步保留在所有角色，避免 API 进程配置陈旧。
- [x] **唯一 Reporter Owner**：生产组合根删除进程内 `TrendingReporter.Start` 自动接线，只由固定 ID 的 Temporal `TrendingReporterWorkflow` 调度；Temporal 不可用时明确停播，不隐式切换第二条模型/发推链路。`AGENT_TRENDING_REPORTER_OWNER=disabled` 是无数据迁移的回滚开关，间隔由 `AGENT_TRENDING_REPORTER_INTERVAL` 配置。
- [x] **部署兼容**：`.env.example`、Compose 与 Helm 均默认 `all + temporal`；Helm 对角色与 owner 做模板期枚举校验。当前仍是一个 Deployment，独立 API/Worker Deployment 和组合根物理拆分保留为下一项架构债。
- [x] **离线验证**：启动计划普通测试、Race、Vet 通过；`cmd/agent-service` 在关闭隐式 Vet 后完成编译，`main.go` 定向 Vet 通过。完整包隐式 Vet 扫描 Elasticsearch 大型生成依赖时超时，已按真实边界记录到 `docs/ISSUES.md`。未启动 Mongo、Redis、RabbitMQ、Temporal、LM Studio 或公网服务，也未调用 DashScope。

## 2026-08-03 P0 稳定性增量：Agent 风控退出社交数据面

- [x] **最小只读信号**：Tweet Proto 增加内部 `GetAuthorPostingStats`，只返回窗口内最多两条创建时间戳；5 秒频率阈值仍由 Agent 风控策略判断，Agent 不读取 Tweet 表或正文。
- [x] **幂等治理命令**：内部 `ApplyTweetModeration` 当前仅支持 Shadowban。TweetService 校验 Tweet/Author、幂等更新可见性、严格失效基础缓存，并按 500 条分批用 Lua 原子移除活跃粉丝 Timeline 项和未读计数；重试只在 `ZREM` 实际命中时扣减未读。
- [x] **组合根降权**：`AgentActivities` 使用只包含 Create/Stats/Moderation 的窄客户端接口；`cmd/agent-service` 不再向 Temporal Activity 注入 GORM 或 FollowRepository。热点 Redis、ES、Qdrant 继续作为有界只读投影。
- [x] **兼容与离线验证**：Proto 仅追加 RPC/消息/枚举，并用描述符测试固定方法、字段号和枚举值；现有 Gateway HTTP API 不暴露特权治理命令。受影响包普通测试/Vet/Race、`internal/...`、`pkg/...` 与 `cmd/...` 分组回归全部通过。单体 `go test ./...` 因仓库枚举在 180 秒超时，未冒充全仓命令通过，见 Issue 152。测试使用 `miniredis`，未启动外部服务、模型或公网连接。
- [x] **服务身份认证**：已由下一增量补齐方法级认证、默认失败关闭和共享 Secret 部署契约；不把静态 Bearer 描述成 mTLS。
- [ ] **后续生产增强**：将最多 5000 活跃粉丝的同步清理演进为事务事件驱动、可重放的全量分批任务，并增加分类重试、DLQ 与进度指标。

## 2026-08-03 P0 稳定性增量：特权 Tweet RPC 服务身份认证

- [x] **方法级最小权限**：新增 `pkg/serviceauth`，只保护 `GetAuthorPostingStats` 与 `ApplyTweetModeration`。客户端仅对这两个完整方法名注入身份和 Token，普通 Tweet RPC 不携带内部凭据；服务端拒绝缺失、重复或错误 Header，并使用常量时间比较 Token。
- [x] **默认失败关闭**：未配置 `TWEET_INTERNAL_SERVICE_TOKEN` 时，Tweet 服务继续提供公开业务 RPC，但两个特权 RPC 固定返回 `Unavailable`；Agent 在出网前返回 `FailedPrecondition`。短 Token、空身份或非法身份会在组合根启动时失败，不存在匿名兼容回退。
- [x] **可部署与可回滚**：Compose 由同一环境变量向 Agent/Tweet 注入凭据；Helm 使用 `tweetService.riskControlAuth` 将同一 Kubernetes Secret 挂载到 Agent、Tweet v1 和 v2，启用但未填写 `existingSecret` 时模板渲染失败。关闭 Helm 开关会撤下 Secret 并自动回到“特权 RPC 不可用、普通 RPC 可用”的安全状态。
- [x] **低敏审计与验证**：拒绝日志只包含固定方法名和 `missing/invalid/unconfigured` 结果，不记录凭据或用户。拦截器单元测试覆盖方法隔离、错误/重复凭据、客户端覆盖旧 Header 与 fail-closed；Tweet/Agent 组合根串行编译、默认/启用 Helm 渲染、缺 Secret 负向门禁、Compose 配置解析和差异检查通过。
- [ ] **生产边界**：静态 Bearer 必须运行在受信网络或 mTLS/Service Mesh 上；真实 Kubernetes Secret 轮换、多副本滚动升级、拒绝率告警和网络层身份验收仍需独立受控演练。下一代码增量转向事务 Outbox 驱动的全量影子治理清理。

## 2026-08-03 P0 稳定性增量：事务事件驱动的全量影子治理清理

- [x] **状态与命令原子提交**：`outbox_events` 增加可空唯一 `dedup_key`；TweetService 在同一 UoW 内更新 `visible_type` 并幂等写入无正文的 `TWEET_MODERATED` Event。历史已 Shadowban 但没有治理事件的记录再次调用时也会补发一次，Outbox 写失败会回滚可见性变更。
- [x] **正确的全量游标**：Follow Repository 新增仅供内部任务使用的 `FollowerPageRepository`，按关注关系 `id DESC` 返回独立 `next_cursor`，不再把 follower user ID 误当关系游标，也不改变公开 Follow API。
- [x] **可恢复异步清理**：Canal 新增 `tweet.moderated` 路由；Timeline Consumer 使用 500 人页、Redis 页游标和 30 天完成标记，逐页调用幂等 Lua 同时移除 Timeline 项和递减未读。失败按 1/2/4 秒退避三次后进入 `queue.tweet.moderation.cleanup.dlq`，重复投递和 ACK 不确定窗口不会重复扣减未读。
- [x] **契约与观测**：`ApplyTweetModerationResponse` 追加 `cleanup_queued=3`，保留 `timelines_cleaned=2` 兼容字段；新增事件/页/扫描/移除/耗时低基数指标，Consumer 在 `2116` 暴露并由 Compose/Helm Prometheus 发现，KEDA 同时观察治理队列。
- [x] **本地验证**：治理命令、分页/恢复/重复投递、指标标签、Proto 描述符、Agent Activity、Canal 路由及受影响组合根测试/编译通过；Compose 主文件与 Helm/KEDA 模板解析通过。未启动 MySQL、Redis、RabbitMQ、Canal、模型或公网服务。
- [ ] **真实环境验收**：仍需在受控环境执行数据库回滚、Canal 重启重复投递、Redis 页间故障、三次失败入 DLQ、人工重放、多副本并发和 KEDA 扩缩容演练；完成前技术债保持 `Partial`。

## 2026-08-03 P0 稳定性增量：影子治理 DLQ 运维闭环

- [x] **默认只读检查**：新增 `cmd/timeline-moderation-dlq-replay`，默认以手动 ACK 有界读取治理 DLQ，校验 Routing Key、事件契约、消息大小与累计重放次数，生成报告后统一重新入队；检查模式不启用 Publisher Confirm，也不发布业务消息。
- [x] **显式限量重放**：执行要求 `--execute`、非空操作人、非空原因、1-100 条批量上限和 1-10 次累计上限。非法路由、毒消息、非法 Header 与超限消息继续留在 DLQ；合格消息清除消费重试计数并使用独立人工重放计数。
- [x] **Confirm 后 ACK**：使用专用 RabbitMQ Channel 进入 Publisher Confirm 模式；仅在 `twitter.events/tweet.moderated` 获得 Broker 确认后 ACK 原死信。发布失败重新入队当前及剩余消息；确认后 ACK 失败明确输出 `acknowledgement_uncertain`，不冒充重放成功。
- [x] **最小化审计报告**：JSON 仅包含消息摘要、事件键摘要、次数、固定 Outcome/Error Code 及操作人/原因摘要，不输出 Tweet/作者 ID、事件正文或审计输入原文。新增 Runbook 说明检查、执行、ACK 不确定窗口和生产验收边界。
- [x] **离线验证**：Fake 驱动测试覆盖检查不进入 Confirm、Confirm-before-read、Confirm-before-ACK、毒消息/上限/非法 Header、发布失败、ACK 不确定和报告脱敏；定向普通测试、Race、Vet 与命令编译通过。未连接 RabbitMQ、MySQL、Redis、Canal、模型或公网。
- [ ] **真实环境验收**：仍需按 `docs/timeline_moderation_dlq.md` 执行真实入 DLQ、检查、重放、Confirm/ACK 故障和多副本/KEDA 演练；完成前技术债保持 `Partial`。

## 2026-08-03 P0 稳定性增量：Consumer 分类重试与风控队列归属

- [x] **Timeline 失败转发独立化**：`cmd/consumer` 使用独立 RabbitMQ 连接承担失败发布，创建、删除与治理事件只允许进入固定 Retry/DLQ 路由；JSON 毒消息直接入 DLQ。失败路由取得 Publisher Confirm 后才 ACK 原消息，确认失败时按 1/2/4 秒有界等待后 requeue，日志仅记录路由、次数和正文长度。
- [x] **风控事件单一 Owner**：Agent Worker 直接声明并拥有 `queue.tweet.risk`，从 `twitter.events/tweet.created` 接收原始事件；Timeline Consumer 删除风控队列绑定和异步二次广播。风控重试经独立 `agent.risk.retry -> agent.risk.ingress` 返回，不会重放主事件并再次触发 Timeline。
- [x] **Temporal 去重与故障隔离**：风控使用稳定 `RiskControl-Tweet-{tweet_id}` Workflow ID、`REJECT_DUPLICATE` 和 AlreadyStarted 显式错误语义；重复消息安全 ACK。格式错误、非法 Retry Header 或重试耗尽进入 `queue.agent.risk.dlq.v1`；即使 Temporal 暂不可用，组合根也先声明队列以缓冲事件。
- [x] **Profile 内容互动一致语义**：内容互动消费者启用 Publisher Confirm，1/2/4 秒分类重试与 DLQ 均在 Confirm 后 ACK；失败发布采用有界退避 requeue，ACK 不确定和发布失败进入固定低基数指标。人工重放同时拒绝非法累计重放 Header。
- [x] **离线验证**：Timeline、Agent Consumer/Service 与两个组合根的定向测试和编译通过；覆盖 Confirm-before-ACK、毒消息、非法 Header、重试耗尽、发布失败退避、ACK 不确定、Temporal 重复以及专用拓扑。未启动 RabbitMQ、Temporal、Redis、Mongo、LM Studio 或公网服务，也未调用 DashScope。
- [ ] **真实环境验收**：按 `docs/consumer_retry_dlq.md` 演练真实 Confirm 超时/Channel 中断、三次失败入 DLQ、滚动升级重复事件和多副本恢复；风控受控检查/重放命令已由下一增量补齐，但真实 Broker 证据未完成，因此该技术债保持 `Partial`。

## 2026-08-03 P0 稳定性增量：Agent 风控 DLQ 运维闭环

- [x] **默认只检查**：新增 `cmd/agent-risk-dlq-replay`，默认通过手动 ACK 的 `basic.get` 有界读取 `queue.agent.risk.dlq.v1`；校验固定 Routing Key、1 MiB 正文上限、`TweetCreatedEvent` 最小身份契约和累计人工重放次数，输出报告后将全部消息重新入队。检查模式不启用 Publisher Confirm，也不发布业务消息。
- [x] **专用入口重放**：执行要求 `--execute`、非空操作人/原因、1-100 条批量上限和 1-10 次累计上限。合格事件只发布到 `agent.risk.ingress/tweet.created.agent-risk`，不会重放主 `twitter.events/tweet.created` 或再次触发 Timeline；自动 Retry Header 与 Broker Dead-letter Header 被清除，人工重放次数独立累计。
- [x] **Confirm 后 ACK 与重复吸收**：命令在读取消息前进入 Publisher Confirm 模式，仅在专用 Ingress 发布获确认后 ACK 原 DLQ 消息。发布失败放回当前及剩余批次；Confirm 成功但 ACK 失败输出 `acknowledgement_uncertain`。稳定 `RiskControl-Tweet-{tweet_id}` 与 Temporal `REJECT_DUPLICATE` 吸收该窗口内的重复启动。
- [x] **脱敏审计与取消语义**：报告只保存消息、Workflow 身份、操作人和原因的 SHA-256，以及固定 Outcome/Error Code；不输出 Tweet/作者 ID、推文正文或审计输入原文。Context 取消会把已读取但未处理的整批消息放回 DLQ。
- [x] **离线验证**：Fake 测试覆盖检查模式、Confirm-before-read/ACK、专用目标、Header 清理、毒消息、非法路由、超大消息、非法/超限次数、发布失败、ACK 不确定、取消回收和报告脱敏；未连接 RabbitMQ、Temporal、数据库、模型或公网。
- [ ] **真实环境验收**：仍需按 `docs/agent_risk_dlq.md` 演练真实入 DLQ、检查、限量重放、Confirm/ACK 故障、滚动升级重复和多 Agent Worker 副本；完成前 `Consumer 直接 requeue` 技术债保持 `Partial`。

## 2026-08-03 P0 稳定性增量：Timeline 恢复路由隔离与创建/删除 DLQ 闭环

- [x] **专用恢复拓扑**：新增 `timeline.ingress` 与 `timeline.retry`，创建、删除、治理使用独立 Timeline Routing Key。新失败只进入版本化 `.retry.v2`，TTL 到期只回 Timeline 主队列，不再重放 `twitter.events` 并触发风控或其他订阅者。
- [x] **无冲突滚动迁移**：不原地修改旧 Retry Queue 的 Dead-letter 参数，避免 RabbitMQ `PRECONDITION_FAILED`。旧队列不删除也不再由新路径写入，已有消息可按原拓扑自然排空；回滚时保留新持久 Exchange/Queue/Binding 即可继续交付在途消息。
- [x] **创建/删除受控重放**：新增 `cmd/timeline-event-dlq-replay`，强制选择 `created|deleted`。默认以手动 ACK 有界检查并重新入队；执行要求非空操作人/原因、1-100 条与 1-10 次上限，先幂等声明专用目标拓扑并启用 Confirm，再读取 DLQ。
- [x] **Confirm 后 ACK 与脱敏审计**：合格消息清除自动 Retry/Broker Dead-letter Header，只发布到 `timeline.ingress`；发布失败放回当前及剩余批次，ACK 失败输出 `acknowledgement_uncertain`。报告只保存消息/事件身份/审计输入摘要与固定结果码。
- [x] **治理重放一致化**：`cmd/timeline-moderation-dlq-replay` 同样在执行前声明专用拓扑，改为只发布 `timeline.ingress/tweet.moderated.timeline`，并清除 Broker Dead-letter Header。
- [x] **离线验证**：拓扑、失败路由和重放 Fake 测试覆盖专用目标、旧队列不重声明、默认检查、Topology/Confirm-before-read、Confirm-before-ACK、毒消息、次数上限、发布失败、ACK 不确定、取消回收与报告脱敏；未连接 RabbitMQ、Redis、MySQL、模型或公网。
- [x] **创建事件派生幂等**：已由下一增量补齐趋势原子投影、`sync_es` 唯一任务键和投影完成后 ACK；自动重试、Consumer 重启与 ACK 不确定窗口不再重复计分或重复入队。
- [ ] **剩余边界**：仍需按 `docs/timeline_event_dlq.md` 验收真实 Broker、旧队列排空、多副本 Claim/Lease、Lease 过期恢复及真实 ES/Qdrant 幂等成本，技术债保持 `Partial`。

## 2026-08-03 P0 稳定性增量：Timeline 创建事件幂等派生投影

- [x] **副作用完成后 ACK**：`tweet.created` 依次完成 Redis ZSet 扇出、趋势投影与 `sync_es` Outbox 入队后才 ACK。任一阶段失败进入既有分类 Retry/DLQ；普通作者批次写入和大 V 个人 Timeline 写入错误不再被日志吞掉。全阶段使用 30 秒有界 Context。
- [x] **趋势原子去重**：新增单次 Redis Lua 投影，使用 Tweet ID 的 72 小时 Marker 原子完成主题映射、作者/主题一小时限频与全局 ZSet 加分。主题先规范化、确定性排序并限制为 32 个；重复事件返回 `duplicate`，不会再次消耗限频或加分。当前键布局面向项目现有单实例 Redis，不宣称 Redis Cluster 跨 Slot 兼容。
- [x] **搜索同步唯一任务**：`outbox_tasks` 增加可空唯一 `dedup_key`，`timeline:sync_es:tweet:{id}:v1` 使用冲突忽略写入。Worker 成功后改为 `Success` 收据并保留 72 小时，每分钟最多分十批清理过期收据；毒载荷和未知类型保留为终态失败证据，不再直接删除去重依据。
- [x] **低基数观测**：Consumer 新增 `timeline_tweet_created_stage_total{stage,result}`，Stage 与 Result 均强制映射到固定枚举，覆盖 Fanout、Trend、Outbox、完成、重复、失败、Retry/DLQ 与 ACK 不确定，不使用 Tweet/User/Error 原文 Label。
- [x] **离线验证**：Consumer 完整单测、派生投影/Worker 目标 Race、Consumer/Repository/Consumer Cmd/Tweet Cmd Vet，以及 Repository、Consumer、Tweet Service 受影响包测试/编译通过。测试覆盖两次交付只产生一个 Timeline 成员、一次趋势分值和一份 Outbox，错误 Redis 类型写前失败，以及成功收据有界保留；未启动 RabbitMQ、MySQL、Redis 服务、ES、Qdrant、LM Studio 或公网。
- [x] **下一代码增量**：`outbox_tasks` 数据库原子 Claim/Lease、过期租约回收与 Worker Owner 已由下一增量完成。

## 2026-08-03 P0 稳定性增量：Outbox Worker 多副本 Claim/Lease

- [x] **兼容迁移与查询索引**：`outbox_tasks` 追加 `Processing` 状态、Lease Owner、每次领取重新生成的 Token 和 Lease Until，并为 Retry 扫描与 Lease 过期扫描建立独立复合索引；原状态值和可空唯一 `dedup_key` 保持兼容。
- [x] **MySQL 原子领取与围栏提交**：仓储在事务中使用 `FOR UPDATE SKIP LOCKED` 有界锁定可执行任务，安装 Owner/Token、递增 Attempt 并批量提交。成功或失败结果必须同时匹配 Task、Processing、Owner、Token 且 Lease 未过期；旧协程、过期协程和已被重新领取的协程只能得到 stale，不可覆盖新状态。
- [x] **过期恢复与重试语义**：每轮领取前最多恢复 100 个过期 Lease。未耗尽任务释放为 Failed 并重新进入既有指数退避，耗尽任务封存为终态失败；恢复同样采用 Skip Locked 和事务内条件更新，不与其他副本争抢同一行。
- [x] **并发、超时与收尾**：每批最多领取 10 条并立即并发执行，避免后排任务空耗 Lease；单任务执行限制 60 秒，Lease 为 90 秒，成功/失败回执使用不继承执行取消信号的独立 5 秒收尾 Context。实例 Owner 可由 `TIMELINE_OUTBOX_WORKER_ID` 指定，默认包含主机、PID 与随机后缀。
- [x] **低基数观测**：新增 `timeline_outbox_worker_operations_total{operation,result}`，只允许 Claim、Recover、Execute、Finalize、Cleanup 及固定结果枚举；记录 claimed/retryable/exhausted/stale 等计数，不把 Task、Tweet、Worker 或错误原文写入 Label。
- [x] **离线验证**：Worker Fake 测试覆盖成功收据、陈旧提交拒绝、毒任务终态、并发批次、过期恢复、有界清理和取消后的独立收尾；Repository DryRun 测试固定 Skip Locked、退避、过期扫描和 Owner/Token/Expiry 围栏 SQL。Consumer/Repository 普通测试与 Race、受影响包回归、Consumer/Tweet 组合根编译、目标 Vet 和差异检查通过；未启动 MySQL、Redis、RabbitMQ、ES、Qdrant、LM Studio 或公网服务。
- [ ] **下一代码增量**：把点赞/评论的 ACK-before-async 热度更新改为事件级幂等投影，并补齐失败分类、重复投递和 ACK 不确定窗口测试。

## 2026-08-05 Agent 面试演示阻断修复

- [x] **站内搜索进入统一 Runtime**：`platform.search` 使用受治理的 `runtime.platform_search` Profile，只开放 `hybrid_search_tweets`；上下文追问继承上一轮安全能力，显式新意图仍优先。
- [x] **证据与防幻觉门禁**：平台事实只允许来自结构化工具输出；身份、链接、时间、指标和全文缺失时必须明确不可用，不得补造。结构化 Observation 已可作为成功证据，无证据完成仍失败关闭。
- [x] **Workflow Provider 与错误交互**：新 LLM 节点和 Agent Service 装配默认使用 DashScope/qwen-plus，显式保存的 LM Studio 配置保持不变；连接拒绝、搜索不可用和运行错误改为页面内可关闭提示，不再使用阻塞式 `alert`。
- [x] **对话与能力入口一致性**：成功响应会立即补齐左侧会话摘要；空持久会话显示明确失败说明。扩展目录、市场与管理入口仅在后端探测确认可用后展示，联网搜索配置明确当前只支持 Brave Search。
- [x] **离线验证**：Web `npm run build` 通过；Agent Service 站内搜索、追问连续性、组合研究和无证据拒绝定向测试通过；Runtime 全包与 `cmd/agent-service` 装配测试通过。未调用 DashScope、LM Studio 或公网。
- [x] **运行中环境更新**：经用户确认，仅重建并重启 `frontend` 与 `agent-service`；Frontend `/agent` 返回 HTTP 200，新懒加载 Chunk 包含 DashScope 默认、空会话说明、Web Search 禁用说明和 Workflow 页面内错误提示。Agent 新镜像已运行并在 Consul 注册为 passing。
- [x] **部署构建收口**：修复 1.43 GB 以上 Docker 上下文泄漏并加入持久 Go BuildKit Cache；后续 Agent 单文件镜像重建降到 34.1 秒。原生 `alert` 已从 Workflow Chunk 移除。
- [x] **可观测启动修复**：共享 Trace Resource 不再混用旧 Schema URL；`pkg/trace` 测试与新容器启动日志确认 OTLP Tracer 初始化成功。
- [ ] **剩余运行边界**：Codex 浏览器控制受 Windows ACL 阻断，最终交互路径需用户手工点击；Temporal 服务端健康且 TCP 可达，但 Agent SDK 启动期 Dial 超时后关闭后台 Worker/趋势报告，主 Agent 演示路径不受影响。

## 2026-08-08 G0/G1 Agent 核心重聚焦与 VerifiedRunner

- [x] **优先级重置**：新增核心重聚焦计划与 20 项端到端任务矩阵；Marketplace、Publisher、Profile A/B、Task Template、管理 RPC 和固定 ExecutionProfile 的功能扩张冻结。
- [x] **完成契约**：新增 `TaskSpec`、Completion Criteria、Environment Snapshot、追加式 EvidenceLedger、Verifier 和确定性最低证据门禁；FinalAnswer 不再被目标架构视为完成证明。
- [x] **opt-in VerifiedRunner**：复用现有 AgentRunner，不复制 ReAct；任务工具取请求、环境和 Task Allowlist 的交集，完成后采集前后快照与结构化 Tool Observation 证据。
- [x] **有界修复**：验证失败仅在 `MaxRepairAttempts`、剩余 Step、Token、成本与总超时都允许时继续；累计 Run Step/Usage，耗尽时返回结构化预算错误，缺证据不可修复时返回 blocked。
- [x] **挂起、恢复与取消**：审批/人工等待生成版本化 Verified Checkpoint，不提前执行 Verifier；Resume 保留 Task、Evidence、初始快照、累计 Usage 和修复次数，同时重新解析当前工具目录，已撤权工具 fail-closed。
- [x] **首个领域证据适配器**：站内搜索只接受与同一步 Tool Action 配对的 `platform.tweet_search.v1` Structured Content；Collector 仅保存摘要/引用，Verifier 反向核对累计 Runtime Observation，伪造 Ledger 不得完成任务。`Runtime/Evidence` 目标测试以及 Service、gRPC、Workflow RAG 串行依赖回归通过；未连接模型、数据库、Redis、MCP 或公网。 完整 Agent 并行回归曾因 Windows 拒绝启动部分 Go compile/vet 子进程中止，已由串行直接依赖回归和目标 Race/Vet 覆盖本轮边界。
- [x] **默认关闭的站内搜索 Shadow**：新增全局 Kill Switch 与站内搜索 Shadow 开关，只有两者同时开启才消费既有 `RunResult` 执行 Collector/Verifier；不重复模型或工具调用，不改变 Legacy 响应。Prometheus 仅记录固定枚举的 Legacy/Goal 结果与证据差异。目标测试、Service/Runtime/Evidence/组合根串行回归及 Vet 通过；Windows 冷缓存 Race 编译五分钟超时，已终止遗留进程并记录验证缺口。
- [x] **G2 Twitter Read Environment Pack**：新增独立 `environment` 包，统一五个站内只读工具白名单并复用到 Service；能力只取当前 Catalog、Task Allowlist 和静态只读策略的交集，重复目录、分类升级、审批标记与非法 Schema 均 fail-closed。前后快照身份只包含工具名和分类，Metadata 不保存 Goal、描述、Schema、正文或凭据；Environment 不持有执行器，不能绕过 ToolExecutor。
- [x] **离线验证**：Environment、Runtime、Service 定向测试通过，覆盖白名单过滤、确定排序、可变数据隔离、上下文取消、低敏稳定快照和不安全目录拒绝；未启动服务、数据库、Redis、MCP、模型或公网。
- [x] **G2 Web Read Environment Pack**：抽取包内私有的通用只读 Catalog 内核，Twitter 行为保持兼容；新增 `WebReadEnvironment`，只暴露已注册且被 Task 允许的 `web_search/page_read`。Service 复用同一 Web 白名单并增加 Catalog Port 编译期契约；Provider/Page Reader 不可用时不会出现在 Environment，执行期仍由 Tenant Routing、Governor、SSRF Policy、凭据解析和 ToolExecutor 权威校验。
- [x] **Web Pack 离线验证**：Environment、Runtime、Evidence、WebSearch、MCP Tools、Service 和 Agent 组合根串行测试及目标 Vet 通过；覆盖注册缺失、策略过滤、不安全分类、非法 Schema、可变输入隔离和不含描述/Schema/凭据的稳定快照。未启动服务或访问公网/Provider。
- [x] **G2 Workflow-as-Tool Environment Pack**：新增按用户构造的 `WorkflowToolEnvironment` 与窄 Catalog Port。能力仅来自当前 Active Publication，并在每次目录读取时重新校验租户所有权、稳定工具名、Publication Revision、不可变 Workflow Revision/DSL Hash、DSL 可发布性和当前 Tool Policy；Task 仍做精确 Allowlist 交集。快照只保留工具名、分类、审批语义与不可逆绑定摘要，不保存 Publication/Revision 原始标识、DSL、描述或 Schema；Environment 不持有执行器。
- [x] **Workflow Pack 离线验证**：Environment 与 Service 定向测试通过，覆盖租户隔离、任务过滤、取消发布即时撤权、Revision/Hash 漂移排除、稳定快照、Schema/身份/分类异常 fail-closed 和可变 Schema 隔离；未启动服务、数据库、MCP、模型或公网。
- [x] **G2 External MCP Environment Pack**：新增按用户构造的 `ExternalMCPEnvironment` 与窄 Catalog Port，复用现有 `ListGovernedTools` 权威适配器；每次目录读取重新校验个人/项目当前成员关系、Active Snapshot、启用中的 Tool Policy、稳定限定名与精确分类，随后再与 Task Allowlist 取交集。快照只保留工具身份、分类、审批语义与不可逆绑定摘要，不保存 Connection/Snapshot 原始标识、Schema Hash、描述、Schema、Endpoint 或凭据；Environment 不持有执行器。
- [x] **External MCP Pack 离线验证**：Environment、Remote MCP 与 Service 定向测试通过，覆盖租户过滤、项目成员即时撤权、Policy 停用即时撤权、Snapshot Version/Hash 与 Policy 绑定漂移、分类降级拒绝、重复/超量目录、取消传播、低敏稳定快照和可变 Schema 隔离；Environment、Remote MCP、Runtime、Evidence、WebSearch、MCP Tools、Service 与 `cmd/agent-service` 扩大普通回归和目标 Vet 通过，五个本轮 Go 文件无格式差异，`git diff --check` 通过。未运行 Race；未启动服务或访问第三方 MCP、模型、数据库及公网。首次 Service 冷编译超过外层有界超时，复用项目内预热缓存后通过，详见 Issue 177。
- [ ] **下一增量**：完成 G2 写操作真实前后状态验证与领域 Verifier；生产 Goal 执行继续关闭，不新增 Environment Pack、Provider 或控制面。

## 2026-08-10 Agent Core Refocus G2：写操作事实验证闭环

- [x] **Tweet Write Environment Pack**：通过 TweetService `GetUserTimeline` 获取作者本人最近推文，只保存排序后的 `/tweets/{id}`、摘要和工具审批语义；转推正文、用户 ID、Credential 与原始内容不进入 Snapshot。
- [x] **领域完成验证**：`platform.tweet_publish.v1` 将 `tweet_id` 作为字符串稳定传递；Collector/Verifier 同时核验真实 Tool Action、结构化 Observation、Before/After Snapshot 和 Evidence Ledger，不能靠 FinalAnswer 或伪造 Ledger 宣称完成。
- [x] **幂等回放结果恢复**：ToolExecutor 命中持久化结果时恢复文本与 Structured Content，不重新调用 `create_tweet`；Verifier 可证明目标推文已存在且集合未变化。
- [x] **离线定向验证**：Environment、Evidence、MCP Tools 与 Agent Service 四包定向测试通过；未访问公网、模型、Mongo、Redis、RabbitMQ 或付费 API，也未启动/重启服务。
- [ ] **受控集成验收**：E2E-13/14 仍需在显式启动的本地 TweetService/Agent Runtime 上验证真实审批、写入、超时重试和 Timeline 可见性；生产 Goal execution 保持关闭，下一阶段进入 G3 Unified Planning。

## 2026-08-10 Agent Core Refocus G3：短期计划契约与确定性准入

- [x] **一至三步计划契约**：新增 `agent.short_plan.v1`、`ShortHorizonPlanner` 与 `tool/ask_human/respond` 三类短期步骤；计划只描述下一小段动作，不替代既有 ReActRunner，也不把模型输出视为执行授权。
- [x] **确定性准入边界**：`DeterministicShortPlanPolicy` 逐步校验 Task Completion Criteria、Task Allowlist、当前 Environment/Catalog 已授权工具交集、工具策略分类、剩余 Step Budget 与终止步骤顺序。连接所有权由调用方在构造 `AvailableTools` 时通过当前 Environment 解析，执行时 ToolExecutor 仍再次权威校验。
- [x] **审批语义不可伪造**：模型提案不包含授权字段；准入结果从当前 ToolDefinition 推导 Category 与 `ApprovalRequired`，write/risky 自动要求审批，并生成不含 Prompt、参数或 Credential 的 SHA-256 计划摘要。
- [x] **离线验证**：新增测试覆盖合法研究计划、任务越权、当前环境撤权、写操作审批、预算不足、完成条件遗漏和终止步骤乱序。Runtime、Strategy 全包测试及两包 Vet 通过；未启动服务、容器或模型，也未访问公网和付费 API。项目 `test_runner` 曾因 Windows ACL 在 Go 启动前失败，主进程以相同缓存和命令完成复验，详见 Issue 179。
- [x] **严格模型 Planner Adapter**：`ModelShortHorizonPlanner` 复用 Runtime `ModelClient`，只发送裁剪后的 Goal、Constraint、Completion Criteria 与当前 Tool 元数据；模型不接收工具 Schema、参数、Credential 或执行授权，也不能直接发起 Tool Call。
- [x] **结构化边界与有界解析修复**：仅接受单个精确 `agent.short_plan.v1` JSON 对象，拒绝 Markdown 围栏、尾随文本、未知字段、重复 Key、非法 UTF-8、过深和超大输出；解析失败最多追加一次通用纠错提示，不把无效模型正文重新注入 Prompt，Provider 故障不盲目重试。
- [x] **预算与成本可见性**：每次初始/纠错模型调用均记录 Attempts、Token Usage、模型与 Provider；调用前同时校验 Run Budget 和共享 `BudgetTracker`，调用后提交实际 Usage 与 Cost，预留失败不会触发模型请求。
- [x] **离线验证**：严格解析、一次修复、Provider 失败、Run/共享 Token Budget、成本预算与确定性 Admission 联合用例通过；Runtime、Strategy 全包测试和两包 Vet 通过。未启动服务、容器或模型，未访问公网和付费 API；`test_runner` 仍受 Issue 179 的 Windows ACL 阻断，主进程完成同命令复验。
- [x] **单一 Planning Coordinator**：新增 Runtime 协调器，把模型提案与确定性 Admission 组合为一个入口；结果只暴露已准入计划、摘要、模型/Provider、调用次数、修复次数和完整 Usage，不保留被拒绝提案。
- [x] **稳定修复边界**：只根据固定 `RunError.Code` 修复 `invalid_action/unknown_tool`，最多一次；预算、配置、取消和 Provider 错误不重试。第二轮只携带固定安全提示，不回灌被拒绝计划、工具名、目标内容或错误原文，调用方也不能伪造首轮 Repair Feedback。
- [x] **跨轮累计预算**：第二轮在原始 Run Budget 中扣除首轮实际 Token/成本，零余额时在模型调用前失败；共享 `BudgetTracker` 继续承担跨节点/并发预算。Planner Usage 会先规范化再累计，解析修复与 Admission 修复均可见。
- [x] **离线验证**：测试覆盖首轮准入、未知工具安全重规划、最多一次修复、预算耗尽、无效 Catalog、Provider 失败、澄清计划、伪造 Feedback 和 Usage 规范化；Runtime、Strategy 全包测试和两包 Vet 通过。未启动服务、容器或模型，未访问公网和付费 API；`test_runner` 仍受 Issue 179 阻断，主进程完成同命令复验。
- [x] **opt-in 计划执行编排**：新增 `PlannedVerifiedRunner`，先按当前 Task/Environment 解析工具交集并生成准入计划，再绑定 `StrategyPlanDigest`、扣除规划 Token/成本、缩窄工具目录并委托既有 `VerifiedRunner`；没有新增第二套 ReAct 或 Tool 执行路径。
- [x] **三类最小执行映射**：`ask_human` 保留 Verified checkpoint/resume，`respond` 禁用工具，read Tool 强制首步工具调用并检查成功 Observation；write/risky 在进入 ReAct 前明确拒绝。
- [x] **模型输出降权**：Developer 消息不包含模型产生的 Objective/原始计划，只投影经准入校验的步骤类型、工具名、Criterion ID 与摘要，防止模型输出被提升为高优先级指令。
- [x] **离线验证**：计划执行器定向用例、Runtime/Strategy 全包测试及两包 Vet 通过；未访问公网、模型或付费 API，未启动/重启服务。`test_runner` 仍被 Issue 179 的 Windows ACL 在 Go 启动前阻断，主流程完成同命令复验。
- [x] **失败成本保留**：VerifiedRunner 在底层 ReAct 返回错误时先合并该次 RunResult，再返回原错误；计划层因此能汇总已发生的失败步骤 Token/成本，不把失败调用误报为零消耗。
- [x] **执行/验证失败受控重规划**：`VerifiedRunner` 新增请求级可选 `VerifiedRepairBuilder`；仅 `tool_error` 与可重试的缺失证据可进入恢复，模型、超时、取消、预算和审批边界不自动重试。旧调用者不提供 Builder 时保持原 verifier-only 修复行为。
- [x] **固定脱敏恢复输入**：恢复 Planner 只收到 `execution_failed|evidence_missing` 固定原因码与 Criterion ID；原始 Provider 错误、工具正文、失败 Observation 和被拒绝计划均不回灌。失败 Tool Call 只追加固定占位 Tool 消息以闭合协议配对。
- [x] **权限与预算不扩张**：恢复计划最多一次，只能从首次已准入的只读 Tool 子集中继续收窄；Runtime 在执行前重验 Run 身份、模型、消息历史、Tool Schema/分类/审批属性和剩余 Step/Token/Cost/Timeout/Deadline，计划仍需重新通过确定性 Admission。
- [x] **证据与用量可审计**：根计划摘要继续绑定累计 Run，恢复计划作为独立 `RecoveryPlans` 证据返回；初始规划、恢复规划与全部执行尝试 Usage 聚合可见，失败步骤保留在累计 Step/Observation 中。
- [x] **离线验证**：新增工具失败恢复、缺失证据恢复、原始错误不泄漏、模型失败不恢复、Tool 扩权拒绝、伪造 Recovery Feedback 拒绝和无效原因码用例；主进程完整 Runtime 测试通过，专用 `test_runner` 复核 Runtime 全包测试、Runtime Vet 与 Environment/Evidence 定向测试均通过；未连接模型、MCP、数据库或公网，未启动/重启服务。
- [x] **显式能力默认路由**：生产默认改为 `ExplicitCapabilityPlanner`；精确能力提示仍经不可变 Catalog 解析，无提示只进入 `conversation.reply`，不再从用户关键词静态猜测搜索、草拟或 Workflow 路线。
- [x] **关键词兼容回退隔离**：`ConservativeCapabilityPlanner` 只由 `NewKeywordCompatibilityCapabilityPlanner` 显式构造；生产代码没有直接装配点，旧 Planner 单元测试继续保留为回滚证据。
- [x] **TaskOutcome 与 Artifact/Evidence 对照**：新增低敏结果投影和组合 Evidence Collector；Artifact 只保存类型、Digest、Reference、Criterion 与 supporting evidence IDs，不复制模型正文或工具正文。
- [x] **E2E-02/11/18 离线闭环**：固定夹具分别验证澄清挂起、研究后草拟的已验证 Artifact，以及工具失败后单次恢复；任务矩阵已记录 Outcome、Artifact、Evidence 和 Repair Decision 对照。
- [x] **AIOps 报告边界修复**：删除 `AnalyzeAlert` 中开发者用户目录硬编码；报告仅通过显式注入的 `AIOpsReportSink` 持久化，默认无本地文件回退，测试改用内存 Sink。
- [x] **离线验证**：主进程 Runtime 与 Service 完整测试、两包 Vet、显式 Planner 和 E2E 定向测试均通过；专用 `test_runner` 在 Go 启动前被 Issue 199 的 Windows ACL 阻断，未把环境失败记作测试结果。未访问模型、MCP、数据库、公网或付费 API，未启动/重启服务。
- [x] **G4 首个只读迁移任务**：站内搜索继续由 Legacy Runtime 唯一执行；只有全局 Goal 与平台搜索 Shadow 两个默认关闭开关同时开启时，Goal Collector/Verifier 才消费同一份已完成 `RunResult`，不会重复模型、工具或消息持久化。
- [x] **TaskOutcome 双记录**：新增 `observed_execution` 来源，明确表示 Goal outcome 是既有执行结果的验证投影，不伪造未驱动原执行的 admitted plan。结构化 Tweet ID 只进入 Evidence Digest/Reference，模型和工具正文不进入投影。
- [x] **E2E-05 离线契约**：服务级夹具断言 Legacy Runner 恰好调用一次，Legacy Citation 与 Goal Evidence 指向同一 `/tweets/{id}`；文本型工具输出可满足旧成功判断，但 Goal verification 必须 blocked。
- [x] **离线验证**：Runtime observed outcome 定向测试、Service E2E-05/Shadow 定向测试、Runtime/Evidence/Service 完整回归及三包 Vet 全部通过；未调用模型、MCP、数据库、公网或付费 API，未启动或重启服务。
- [x] **E2E-06 可信追问闭环**：首次搜索把结构化 Citation 投影为助手消息中的 `/tweets/{id}` 引用；后续仅接受同一用户对话内的显式 ID、序号或唯一引用，歧义、越界、多 ID 和伪造 ID 在 Runtime 前 fail-closed。
- [x] **详情工具与 Goal 对照**：追问运行时只暴露 `get_tweets_by_ids`，Action 参数必须精确选择上一轮引用，且 `platform.tweet_detail.v1` Observation 必须返回同一 ID 的非空正文。Goal shadow 复用同一次执行，分别记录 prior reference 与 detail observation，生成 `observed_execution` TaskOutcome。
- [x] **E2E-06 离线验证**：定向测试覆盖成功、歧义、伪造、多 ID、文本伪证据和单次执行双记录；Runtime/Evidence/MCP Tools/Service 完整回归及四包 Vet 通过。未调用模型、MCP、数据库、公网或付费 API，未启动或重启服务。
- [x] **E2E-07 公共 Web 研究证据链**：新增 digest/reference-only Collector/Verifier；只有 `web_search` 结构化来源、后续 `page_read` Action 和 `web.page.v1` 页面三者指向同一规范化公网 URL 时才通过，正文不复制进 TaskOutcome。
- [x] **默认关闭的 Web Research Shadow**：新增独立 `AGENT_GOAL_RUNTIME_WEB_RESEARCH_SHADOW_ENABLED`，必须与全局 Goal 开关同时开启；Legacy Runtime 仍是唯一执行所有者，Goal 只观察同一 `RunResult`，不重复模型、Provider 或页面请求。
- [x] **E2E-07 离线验证**：覆盖成功、仅搜索未读页、伪造页面 URL、伪造 Ledger 引用和单次执行双记录；Runtime/Evidence/Service/组合根串行测试与四包 Vet 通过。未调用模型、MCP、数据库、公网或付费 API，受控真实 Provider 集成门仍待执行。
- [x] **E2E-08 缺失 Web 证据治理**：Search/Page Collector 与 Verifier 为缺失观察、Provider/Page 错误、空结果、私网或不可解析引用输出稳定原因码；诊断证据只保存固定原因码摘要、步骤与动作等低敏结构，不复制错误正文、页面正文、URL 或最终回答。
- [x] **Legacy Web 完成护栏**：联网搜索和 Web 草拟在持久化前必须存在至少一个结构化、可引用的公网搜索结果；“正在获取，稍后返回”等无证据回答、空结果和无效引用直接阻断且不写入对话。普通搜索不强制 `page_read`，也不增加模型、Provider 或页面调用。
- [x] **E2E-08 离线验证**：覆盖空结果、Provider 错误、私网引用、Page 错误保留 Search 证据、低敏诊断、单次 Runner 与消息不落库；Runtime/Evidence/Service/组合根串行测试及四包 Vet 通过。未调用模型、MCP、数据库、公网或付费 API，未启动或重启服务。
- [x] **E2E-09 Grounded Draft 领域契约**：新增统一 Collector/Verifier，接受站内 Tweet 与公网 Web 两类结构化来源；草稿必须以精确 `[/tweets/{id}]` 或 `[public URL]` 标记把邻近主张绑定到真实来源，并生成只含正文摘要/引用的已验证 `content.draft` Artifact。
- [x] **默认关闭的 Grounded Draft Shadow**：新增 `AGENT_GOAL_RUNTIME_GROUNDED_DRAFT_SHADOW_ENABLED`，必须与全局 Goal 开关同时开启；站内/Web 草拟继续由 Legacy Runner 唯一执行，Shadow 只消费同一 `RunResult`。当前 Profile 未原地改写，Shadow 用于测量现有输出的引用格式符合率，不改变用户响应或持久化。
- [x] **E2E-09 离线验证**：覆盖平台/Web 成功、缺失引用、伪造引用、跨来源引用、与正文分离引用、Artifact 支持证据、专用开关和单次执行双记录；Runtime/Evidence/Service/组合根逐包测试与 Vet 通过。未调用模型、MCP、数据库、公网或付费 API，未启动或重启服务；受控真实 Provider/Profile 集成门仍待执行。
- [x] **E2E-10 显式重写约束契约**：新增规范化 `RewriteConstraintSpec` 与 Task 构造器，精确绑定中文/英文、JSON/Markdown 列表/纯文本和 Unicode 字符上下限；Task 漂移、未知条件或工具权限均 fail-closed，不从自然语言关键词推断约束。
- [x] **低敏 Artifact 与确定性完成判断**：Collector 仅记录版本化约束摘要、最终正文 SHA-256 和 Run 引用；Verifier 对瞬时正文执行 70% 主导文字脚本、严格输出结构和字符范围检查。任一条件失败都会令 `content.rewrite` Artifact 保持 failed，模型声称完成不能通过。
- [x] **E2E-10 离线验证**：覆盖中英文、三种格式、过短、超长、语言/格式错配、空正文、伪造 Artifact、Task 漂移和工具越权；Evidence/Runtime 全包测试与两包 Vet 通过。任务矩阵将本项定义为纯离线契约，因此未新增 Service Shadow、API 字段、Profile 或生产开关，也未访问模型、数据库、公网或付费 API。
- [x] **E2E-11 Research then Draft 顺序契约**：新增独立 Collector/Verifier，复用 E2E-09 的可信来源、精确 Citation 与 `content.draft` Artifact 校验，并额外要求匹配最终正文的 `final_answer` Action 必须位于可信研究 Observation 之后；顶层 FinalAnswer、伪造顺序证据、缺少研究或研究发生过晚均不能完成。
- [x] **默认关闭的 Research Draft Shadow**：新增 `AGENT_GOAL_RUNTIME_RESEARCH_DRAFT_SHADOW_ENABLED`，必须与全局 Goal 开关同时开启；仅挂接既有站内/Web 研究草拟 Profile 的已完成 `RunResult`，不重复模型、搜索 Provider、Tool 或消息持久化。指标使用固定 `content.research_draft` 低基数标签，不与 E2E-09 结果混计。
- [x] **E2E-11 受控离线集成**：平台与 Web Service 夹具覆盖单次 Runner、双记录、成功顺序、顺序倒置、缺失研究、开关隔离和伪造 Evidence；G3 离线计划夹具继续证明准入计划包含 research→respond。本项不修改不可变 Profile，也不宣称真实 Provider/Profile 符合率或生产 Goal 已开启。
- [x] **E2E-12 结构化冲突证据**：新增 `agent.evidence_assertions.v1` 与规范化 Task/Spec；只接受允许工具的成对成功 Observation，同一 Claim 的不同 canonical value 必须绑定不同公网引用，Ledger 仅保留摘要和引用。
- [x] **可验证澄清挂起**：`VerifiedRunner` 新增可选 `SuspendedRunVerifier`，未实现该接口的旧 Verifier 继续保持 inconclusive checkpoint 语义。冲突任务只有在全部证据之后发出精确配置的 `ask_human` 才返回 verified suspension；静默 FinalAnswer、提前/无关提问、同值、未配对 Observation 与伪造 Ledger 均 blocked。
- [x] **E2E-12 离线验证**：定向测试覆盖有效挂起、TaskOutcome、旧审批兼容、URL fragment 别名、错误时序、跨 Run 伪造和静默选边；Environment/Evidence/Runtime/Service 四包串行回归与四包 Vet 通过。未新增业务入口、Service Shadow、API、Profile、Provider 或生产开关，未访问模型、数据库、MCP、公网或付费 API，也未启动或重启服务。
- [x] **G4 下一增量**：E2E-13/14 Tweet 写入的受控进程内集成验收已由下一节完成；不扩张 Marketplace、Profile 或管理控制面，生产 Goal execution 继续关闭。
## 2026-08-11 Agent Core Refocus G4：E2E-13/14 受控写入闭环

- [x] **可信参数与模型契约**：`create_tweet` 的 Runtime/OpenAI 模型可见 Schema 不再暴露 `user_id` 与 `idempotency_key`；Service 在统一 ToolExecutor 校验前从认证身份和 Run/Action 生成值注入，并在 MCP 调用边界再次覆盖，模型只需提交正文且无法伪造租户或幂等身份。
- [x] **审批挂起与恢复**：受控夹具组合真实 ReActRunner、VerifiedRunner、ToolExecutor、内置 MCP Handler 与 Timeline Adapter。首次执行生成 `approval-tweet-1` checkpoint，写入次数严格为 0；显式授权后 Resume 同一 Action，写入一次并由权威 After Snapshot 验证目标 `/tweets/9001`。
- [x] **完整幂等回放**：使用相同 Run/Action/Tool 生成的稳定键再次执行完整 Goal；ToolExecutor 从持久结果接口恢复 `platform.tweet_publish.v1` Structured Content，MCP Handler/TweetService Fake 不再调用，Before/After 集合保持不变且 `TweetPublishGoalVerifier` 通过。
- [x] **离线验证**：3 个新增定向测试、Environment/Evidence/Runtime/ToolExecutor/Service 五包串行回归、五包 Vet、Gofmt 与 `git diff --check` 通过。未访问外网、模型、付费 API 或真实数据库，未启动/重启服务，也未向真实账号发布推文。
- [ ] **部署态验收边界**：仍需用户显式准备隔离账号与本地 TweetService/Mongo 后，验证持久审批记录、进程重启恢复、Timeline 最终可见性和真实超时窗口；完成前不宣称生产闭环。
- [x] **G4 下一增量**：E2E-15/16 用户/项目 MCP 只读调用、写操作审批及撤权即时失败已由下一节完成；复用现有治理能力，未新增 Connector、Profile、Marketplace 或管理 RPC。
## 2026-08-11 Agent Core Refocus G4：E2E-15/16 MCP 授权与撤权闭环

- [x] **Actor/Revision 授权快照**：External MCP Environment Snapshot 新增认证 Actor Digest 与 Connection Revision，并继续使用不可逆 Binding Digest 绑定连接、Schema Snapshot 和 Tool Policy；Decoder 会复算 Digest/Reference，跨 Actor 或篡改 Revision 均 fail-closed。
- [x] **只读 Goal 证据**：新增 `ExternalMCPReadGoalCollector/Verifier`，只接受任务允许、成对、成功且 Read-only 的 Runtime Action/Observation；Before/After 授权快照必须一致，Evidence 仅保留结果摘要、环境引用和工具哈希，不复制外部 MCP 正文、端点、凭据或原始连接 ID。
- [x] **审批后撤权即时失败**：受控夹具首次执行写工具只生成 `approval_required` ToolExecutor 审计，远程调用为 0；显式批准后撤销连接并提升 Revision，Resume 按当前 Environment Catalog 重新授权，在工具不可用阶段 blocked，远程 Caller 仍为 0。
- [x] **租户与项目访问边界**：Manager 的用户所有权和项目成员当前态检查继续作为唯一授权事实；本轮将 Connection Revision 投影到无凭据 Environment Binding，另一租户目录为空，既有项目成员撤权测试继续证明权限变化即时生效。
- [x] **离线验证**：E2E-15/16 四个定向用例、MCP Remote/Environment/Evidence/Runtime/ToolExecutor/Service 六包回归及六包 Vet 全部通过。未访问模型、真实 MCP、数据库、公网或付费 API，未启动/重启服务。
- [ ] **部署态边界**：真实外部 MCP Provider、Mongo 持久连接、项目成员撤权传播、进程重启审批恢复与生产 Goal execution 仍未验收，完成前不宣称生产闭环。
- [x] **G4 下一增量**：E2E-17 Workflow-as-Tool 精确发布 Revision、子 Run 绑定与结构化输出证据已由下一节完成；继续冻结 Marketplace、Profile/A-B 和新增管理控制面。

## 2026-08-11 Agent Core Refocus G4：E2E-17 Workflow-as-Tool 完成证据

- [x] **调用者与发布绑定**：Workflow Environment Snapshot 增加认证 Actor Digest，并提供严格解码；快照继续只暴露工具名、分类、审批语义和不可逆 Binding Digest，不保存原始 Publication/Revision/DSL 内容。
- [x] **结构化完成契约**：`workflow.run.v1` 结果显式绑定 Publication ID/Revision、Workflow Revision/DSL Hash、父 Run/Action、child Run、声明响应摘要与权威 `OutputJSON` 摘要；草稿更新不改变已发布 Revision。
- [x] **权威 child Run 验证**：新增窄只读 Resolver，Collector/Verifier 在用户隔离下重新读取 child Run，核对 Workflow/Revision、`runtime` 调用来源、父子谱系、成功状态、完成时间与持久输出摘要；Evidence Ledger 只保存 digest/reference。
- [x] **失败传播**：真实 DAG 子节点失败会持久化 failed child Run，父 Tool 仅返回固定失败信息并终止，避免内部节点错误正文泄露给模型，同时不得生成完成证据。
- [x] **离线验证**：Environment/Evidence/Service 四个目标用例通过；Environment、Evidence、Runtime、Workflow Tool、Service 五包普通回归与五包 Vet 通过。未访问模型、公网、Mongo、Redis、MCP 或付费 API，也未启动或重启服务。
- [x] **下一增量**：E2E-19 Checkpoint/Resume 已由下一节完成；真实 Mongo、多副本/进程重启和生产 Goal 路由仍待验收。

## 2026-08-11 Agent Core Refocus G4：E2E-19 Checkpoint/Resume 单写恢复

- [x] **挂起证据连续性**：VerifiedRunner 在挂起边界捕获当前 After Snapshot 并收集已成功 Observation，即使领域 Verifier 不实现 SuspendedRunVerifier，也不会丢失后续恢复所需的写入证据。
- [x] **单调 Checkpoint Revision**：VerifiedCheckpoint 增加从 1 开始的 Revision；再次挂起按前一 Revision +1 构造，零值/缺失 Revision fail-closed。每次合法 Resume 追加一条 `checkpoint_resume` Evidence，只保存摘要、Run/Revision 引用与 Step 索引，不保存人工回答或工具正文。
- [x] **双恢复单写集成**：受控夹具先在 Revision 1 审批挂起，审批恢复执行一次 `create_tweet` 后在 Revision 2 等待人工输入；两次 JSON 往返模拟持久化边界，最终仍是同一 Run、三个累计 Step、一条 Tweet 状态证据和两条 Resume Evidence。
- [x] **幂等与副作用门禁**：审批前写入为 0；审批后和人工恢复完成后写入始终为 1，Idempotency Store 完成计数为 1、无 Pending Record，审批完成计数为 1。
- [x] **离线验证**：Runtime 最小测试、Runtime 全包、E2E-13/14+19 定向回归、Environment/Evidence/Runtime/Workflow Tool/Service 五包普通回归及五包 `go vet` 全部通过。未访问模型、公网、Mongo、Redis、外部 MCP 或付费 API，也未启动/重启服务。
- [x] **固定矩阵衔接**：E2E-20 已在下一节完成；本轮 JSON 往返与内存 Store 仍不等于真实 AES-GCM Mongo Checkpoint、多副本 CAS Claim/租约和进程重启验收，生产 Goal execution 继续关闭。

## 2026-08-11 Agent Core Refocus G4：E2E-20 Provider Outage

- [x] **固定错误分类**：新增低敏 Provider failure code，区分 unavailable、rate limited、timeout、invalid request、unauthorized、canceled 与 unclassified；HTTP/上下文/网络错误使用确定性策略，兼容旧 Adapter 的未知错误仍受显式 Fallback 图约束。
- [x] **策略化回退**：ProviderRouter 只沿 Catalog 中显式且能力兼容的 Fallback 遍历；暂时性故障记录 `fallback_allowed`，认证/永久请求错误记录 `fallback_denied` 并保证后备 Provider 调用为 0，允许路线全部失败时记录 `fallback_exhausted`。
- [x] **blocked 完成语义**：Runtime 把失败模型 Step 与路由轨迹写入 RunResult；VerifiedRunner 只接受结构完整、无 Selected Model 的终止轨迹，将 Goal 投影为 blocked，并追加一条仅含摘要/引用的 Provider Routing Evidence。
- [x] **无伪造输出**：blocked 路径不生成 Assistant Message、FinalAnswer 或 Artifact，路由证据不携带 Criterion ID，不能冒充任务完成证据；Provider 错误正文、响应体和凭据不进入 Runtime 轨迹。
- [x] **受控集成验证**：真实 Catalog/ProviderRouter/ReActRunner/VerifiedRunner 与内存 Provider Client 覆盖允许回退成功、永久错误拒绝及全部路线耗尽；Model、Runtime、Service 三包测试和三包 `go vet` 通过。
- [ ] **剩余边界**：未连接 DashScope、LM Studio 或其他真实 Provider，未进行部署态故障注入和线上指标验收；生产 Goal execution 保持关闭。G4 固定迁移矩阵至此完成，下一阶段是 G5 清理与产品重开审计。
## 2026-08-11 Agent Core Refocus G5：删除关键词能力 Planner

- [x] **生产可达性审计**：仓库调用图确认 `ConservativeCapabilityPlanner` 与 `NewKeywordCompatibilityCapabilityPlanner` 只有测试调用者；默认组合根一直使用 `ExplicitCapabilityPlanner`，不存在配置或生产装配点。
- [x] **死分支删除**：删除关键词 Planner、命名兼容构造器及关键词自动路由测试；`ExplicitCapabilityPlanner` 构造器收紧为单一 Catalog 参数，用户正文不再决定产品能力。
- [x] **显式路由回归**：保留“未选择能力即 conversation.reply”和显式组合能力测试，新增未知能力、Planned/Unavailable 能力与 Context Cancel 负向断言；直接组装的 Skill 测试夹具显式注入 Planner。
- [x] **兼容边界**：保留 `compat.chat/consult/assist`，因为历史 Dialogue Mode 恢复和产品指标仍引用这些值；本轮没有迁移持久数据或删除旧会话能力。
- [x] **离线验证**：Planner/Unified Agent 定向测试、Agent Service 全包测试与 Service `go vet` 通过；未联网、调用模型/付费 API、数据库或 MCP，也未启动/重启服务。
- [ ] **下一增量**：审计 `unified_agent.go` Profile switch 与 Multi-Agent 固定模板的重复编排，只删除可证明无调用者或已有等价 Runtime 路径的分支；Marketplace/Profile 继续冻结。

## 2026-09-24 上线收口 L2：External MCP 只读 Planning-Execute

- [x] **独立默认关闭灰度**：新增 `AGENT_PLANNING_EXECUTE_EXTERNAL_MCP_READ_ENABLED`；只有 Planning 主开关、非零稳定用户灰度、External MCP 总开关与该路由开关同时满足时，`runtime.external_mcp` 才进入短计划执行，关闭后原样回退既有 Runtime 路径。
- [x] **只读目录最小暴露**：Planning 路径只从 `ListExecutableTools` 获取当前租户可执行的只读工具，风险/写工具不会进入 Planner 或计划任务；无只读工具时保留原治理路径，目录读取错误立即返回且不被后续查询掩盖。
- [x] **真实完成证据**：计划任务复用 `ExternalMCPReadGoalCollector/Verifier` 与 External MCP Environment；只有允许工具的成对成功 Action/Observation 且前后授权快照一致时才完成，缺失 Observation fail-closed。
- [x] **恢复时重新授权**：`goal.v1` Checkpoint 固定初始 Task 工具集合；人工澄清恢复重新读取当前连接、项目成员、Active Snapshot 与 Tool Policy，撤权后在 Runtime/远端调用前失败，不允许恢复时扩权。
- [x] **离线验证**：新增独立开关、读写混合目录最小暴露、缺失证据阻断、Legacy 回滚与暂停后撤权五类测试；Agent Service 全包测试及 Service race 检查通过。未访问真实 MCP、模型、公网、数据库或付费 API，未启动或重启服务。
- [ ] **下一批**：迁移 Workflow-as-Tool 只读 Planning-Execute，继续复用不可变 Publication/Revision、父子 Run 和权威 child output 证据；之后再评估审批写入迁移，不扩张 Marketplace、Profile 或新管理控制面。

## 2026-09-24 上线收口 L2：Workflow-as-Tool 只读 Planning-Execute

- [x] **独立默认关闭灰度**：新增 `AGENT_PLANNING_EXECUTE_WORKFLOW_READ_ENABLED`；只有 Planning 主开关、非零稳定用户灰度、Workflow-as-Tool 总开关与该路由开关同时满足时，`runtime.workflow` 才进入短计划执行，关闭后原样回退既有 Runtime 路径。
- [x] **当前发布目录最小暴露**：Planner 只接收当前用户 Active Publication 生成的只读 Workflow Tool；目录继续重验用户所有权、Publication Revision、不可变 Workflow Revision/DSL Hash 和可发布策略，未发布、漂移或重复绑定均 fail-closed。
- [x] **权威完成证据**：计划任务复用 Workflow Environment 与 `WorkflowToolGoalCollector/Verifier`；只有结构化结果同时匹配前后不可变发布快照、父 Run/Action、用户隔离的权威 child Run、成功状态和 OutputJSON digest 才完成，模型声明或伪造 Observation 不能通过。
- [x] **恢复时重新授权**：`goal.v1` Checkpoint 固定初始 Task 工具集合；人工澄清恢复重新读取当前 Active Publication 并与原集合取交集，暂停期间停用发布会在 Runtime Resume 前失败，不执行旧版本工具，也不允许恢复时扩权。
- [x] **回滚、指标与配置**：Workflow Profile 纳入低基数 Planning 指标；组合根、`.env.example` 与 Compose 增加独立开关，默认均关闭。Legacy Runtime 保持单开关即时回滚所有者，未新增执行器或 Profile 分支。
- [x] **离线验证**：新增独立开关、真实 child Workflow 成功证据、伪完成阻断、Legacy 回滚与暂停后停用发布五类测试；Agent Service、Runtime、Evidence、Environment 及全 Agent 包普通测试、Workflow 定向 Race 和 Agent/Cmd Vet 全部通过。未访问模型、Mongo、Redis、MCP、公网或付费 API，未启动或重启服务。
- [x] **下一批已完成**：External MCP 审批写入已迁移至 Planning-Execute，复用现有 Approval/Resume、稳定幂等键和当前授权证据；未扩张 Marketplace、Profile、Workflow 节点或管理控制面。

## 2026-09-24 上线收口 L2：External MCP 审批写入 Planning-Execute

- [x] **独立默认关闭灰度**：新增 `AGENT_PLANNING_EXECUTE_EXTERNAL_MCP_WRITE_ENABLED`，且强制依赖 `AGENT_UNIFIED_APPROVAL_RECOVERY_ENABLED`；关闭后继续使用原 governed Runtime，读灰度不因此扩大权限。
- [x] **Planner 不拥有授权**：写灰度读取当前租户 `ListGovernedTools`，确定性 Planning Policy 从 ToolDefinition 写入类别和审批要求；`PlannedVerifiedRunner` 只允许无审批 read 或必须审批的 risky/write，非法组合 fail-closed。
- [x] **完整审批检查点**：Planning 写动作返回 `approval_required` 时加密保存完整 `goal.v1` VerifiedCheckpoint，而非降级为 `react.v1`；一次性 Resume Grant 和恢复入口均识别该版本，并继续绑定原 Task、Plan、Environment Snapshot、动作与审批。
- [x] **重授权与幂等**：批准恢复重新读取当前连接、成员关系、Active Snapshot、Tool Policy 与 Profile，精确校验审批的 Run/Step/Tool/Input Digest/Idempotency Key；平台注入稳定远端幂等键，完整回归证明远端调用一次且重复 Resume Token 被拒绝。
- [x] **完成证据边界**：新增 `ExternalMCPGovernedGoalCollector/Verifier`，证明稳定租户授权/Schema/Policy 快照下的成对成功 Observation。通用 MCP 无统一业务查询接口，因此具体第三方后状态读回归入 L3 Live Acceptance；第一方 Tweet 继续由显式 `ConfirmPublishTwitter` 与 Timeline 后状态验证负责，不新增 Planner 发布能力。
- [x] **离线验证**：Runtime 写计划审批挂起、governed Evidence、服务路由及完整“计划 -> 挂起 -> 加密持久化 -> 批准 -> 恢复 -> 单次远端调用”回归已通过；Runtime/Environment/Evidence/Workflow Tool/Service 受影响包测试、关键链路 Race、受影响包 `go vet`、Compose 配置解析与 `git diff --check` 均通过。未访问模型、公网、真实 MCP、数据库或付费 API，未启动/重启服务。
- [ ] **下一批**：进入 L3 MCP/Workflow Live Acceptance 与生产部署收口；需要用户明确提供真实 MCP、部署目标和 Secret 交付方式后才执行联网/写验收。

## 2026-09-24 上线收口 L3：Helm 灰度入口与验收证据交付

- [x] **Planning-Execute 生产接线**：Helm Values 与 Agent Deployment 已接入主开关、稳定用户 canary、平台/Web/MCP 读写/Workflow 独立路由、输出上限和修复次数；模板校验所有依赖关系，默认保持关闭。
- [x] **会话与 Runtime 配置补齐**：Helm 接入 `AGENT_RUNTIME_V2_MODES` 和全部 `AGENT_SESSION_SUMMARY_*` 配置，避免 Compose 已可用而 Kubernetes 缺少同等能力。
- [x] **角色感知健康检查**：Agent Service 注册标准 gRPC Health Check；api/all 使用 gRPC startup/readiness/liveness，worker 使用 metrics TCP 探针；优雅关闭前先切换为 NOT_SERVING。
- [x] **验收报告交付**：MCP Acceptance Job 保持 `backoffLimit: 0`，支持将签名报告写入已有 PVC；模板拒绝相对挂载路径和带路径分隔符的文件名，避免写入越界或覆盖路径混淆。
- [x] **安全灰度样例**：新增 `values-agent-staging.example.yaml`，只提供占位镜像、资源和默认关闭的小流量配置，不包含真实 Endpoint、Token 或密钥。
- [x] **运维边界**：新增 `docs/agent_launch_runbook.md`，明确 Secret 文件交付、先读后写、Workflow 父子证据、故障演练、独立回滚和“离线通过不等于真实上线”的停止线。
- [x] **离线验证**：gRPC Health 定向测试、Helm 默认/灰度/worker/验收 Job 正向渲染和依赖/路径负向门禁已通过；未访问公网、模型、真实 MCP、数据库或付费 API，未启动/重启服务。
- [ ] **真实 L3 证据**：仍需在目标 Kubernetes 环境执行第三方 MCP 读/审批写、旧凭据与成员撤销、真实 Workflow Publication 父子链路、超时/证书/DNS/Pod 故障演练。
- [x] **下一批已开始**：L4 第一增量已新增独立生产 Compose 覆盖、仓库外 Secret 环境模板、不可变镜像/弱口令/跨开关预检和 Helm Mock Embedding 门禁；真实验收仍需部署目标和受控 Secret 文件。

## 2026-09-24 上线收口 L4：生产 Compose 静态基线

- [x] **开发/生产隔离**：保留现有 `docker-compose.yaml` 作为开发拓扑，新增 `docker-compose.production.yaml` 仅覆盖生产约束；生产覆盖使用 `!reset` 删除本地 build 与内部宿主机端口。
- [x] **不可变制品**：29 个服务均要求 `repository@sha256:<digest>`，禁止 `latest`、普通 tag 和模板零 digest；应用容器只能拉取已构建镜像。
- [x] **运行边界**：统一 `unless-stopped`、资源 reservation/limit、`no-new-privileges`；关键数据/业务服务增加健康检查，Frontend/Gateway 仅绑定 `127.0.0.1`，其余服务不发布宿主机端口。
- [x] **Secret 与依赖门禁**：真实环境文件必须位于仓库外；预检密码/JWT/内部令牌长度、两类 32 字节 Base64 主密钥、HTTPS Provider/媒体 URL，以及 Planning/MCP/Workflow/Web Search 跨开关依赖。
- [x] **真实模型路径**：Compose Agent/Consumer 强制 `MOCK_EMBEDDING=false` 并禁止 `host.docker.internal`；Helm 新增 `mockEmbedding=false` 默认值，Planning 开启时 Mock 直接渲染失败。
- [x] **离线验证**：使用无真实凭据的临时夹具完成 29 服务生产渲染与预检；示例占位 digest 负向测试 fail-closed。未拉取镜像、启动容器、访问公网或调用模型/API。
- [ ] **下一增量**：完善宿主机 TLS/Origin/Cookie 配置、数据库与对象存储备份恢复 Runbook、Agent/队列/Provider 告警和数据保留策略；随后执行 clean-host 与重启/回滚验收。

## 2026-09-24 上线收口 L4：TLS、安全入口、告警与恢复门禁

- [x] **唯一公网 TLS 入口**：新增 `edge-proxy`，仅其发布 `80/443`；HTTP 跳转 HTTPS，页面、API 和 WebSocket 分流到内部 Frontend/Gateway，证书与私钥通过仓库外 Compose Secret 只读挂载。
- [x] **Origin 与 JWT 收紧**：HTTP CORS 和 WebSocket 复用精确 Origin 白名单，生产拒绝 `*`；JWT 生成与 Gateway 验证共同绑定 RS256、过期时间、Issuer 和 Audience，生产缺失配置时拒绝启动。
- [x] **Alertmanager 鉴权去硬编码**：`/alerts` 默认使用 `Authorization: Bearer`，Token 来自至少 32 字符的 Secret 文件并常量时间比较；旧 Header 仅作为同 Secret 的滚动升级兼容入口，未配置返回 503，错误 Token 返回 401，API 文档已同步。
- [x] **生产观测基线**：Prometheus 接入规则与 Alertmanager，覆盖 Target、HTTP 5xx、队列、Provider/LLM、Tool、MCP、Planning、Summary 与费用异常；Loki/Prometheus/Alertmanager/Jaeger 配置保留期，Promtail 移除高基数 `trace_id` Label。
- [x] **备份完整性门禁**：新增七类备份产物的 SHA-256 Manifest 创建/验证脚本，离线测试证明单文件篡改会被拒绝；恢复顺序、RPO/RTO 和证据模板已写入 Runbook，但未声称创建了真实备份或完成恢复。
- [x] **生产预检增强**：校验证书有效期、证书/私钥匹配、公开 HTTPS URL、Origin、JWT、外部 Secret 与唯一公网端口；一次性假证书夹具通过 `31 services / 31 images` 正向渲染，通配 Origin 负向测试通过。
- [x] **离线代码验证**：Gateway Middleware/Router/Auth Service 目标测试与相关 Vet、PowerShell 语法、Compose 正向预检、备份创建/复验/篡改拒绝和 `git diff --check` 通过；未拉取镜像、启动/重启服务、访问公网或调用付费 API。
- [ ] **真实 L4 验收**：仍需目标主机、真实镜像摘要、受信域名证书和 Secret，执行 clean-host 启动、浏览器 Origin/WebSocket、告警投递、真实备份恢复、服务重启与无数据删除回滚；应用级数据删除任务也仍待实现。
- [ ] **下一批**：在用户提供目标部署参数后执行 L4 Live Acceptance；若参数尚未就绪，只准备不触网的验收记录模板与 L5 固定旅程，不再扩张控制面。

## 2026-09-24 上线收口 L4/L5：签名验收证据门禁

- [x] **固定检查目录**：新增 26 项 L3/L4/L5 Required Check，覆盖真实 MCP 读写/撤权、Workflow 父子链、clean-host、TLS、告警、备份恢复、重启回滚、数据删除、固定产品旅程和七类故障演练；目录不包含 Credential 或环境专属值。
- [x] **四态事实模型**：每项只能是 `not_run/passed/failed/blocked`；通过必须绑定证据，失败/阻塞必须写原因，未执行不能携带结果。任一 Required Check 未通过时 `-RequireReleaseReady` 返回非零。
- [x] **证据完整性**：Record、HMAC Key 和 Evidence Root 默认强制位于仓库外；记录只保存相对路径、大小和 SHA-256。计划变更、Record 编辑、附件缺失/改动、Key ID 错误和签名错误均 fail-closed。
- [x] **脱敏与大小边界**：每项最多 8 个、单个不超过 50 MiB 的证据文件；Reason 有界并拒绝常见 Secret 赋值形态。记录不保存 Prompt、模型正文、Token、Cookie、数据库转储或 Endpoint Credential。
- [x] **固定旅程定义**：Runbook 明确同一隔离账号上的对话压缩、站内/公网搜索、引用草稿、审批前零副作用、批准后单写、Run Inspection 与 Resume 不重放七步证据。
- [x] **自包含离线回归**：新增脚本自动覆盖 26/26 Release Ready、未完成严格拒绝、无证据伪通过、检查目录篡改、Record 篡改和 Evidence 篡改，并自动清理夹具；未访问网络、模型、MCP、数据库或付费 API，未启动/重启服务。
- [ ] **Live Evidence Pending**：该门禁只规范证据，不执行真实检查。下一批仍需目标服务器、域名/证书、镜像摘要、Secret 交付、Provider/Web Search/MCP 和数据保留参数后才能生成正式 Record。
## 2026-09-25 上线收口 L4：用户删除请求与只读预检

- [x] **持久化状态机**：新增 `agent_data_deletion_requests`，一个用户最多一个 Active 请求；请求通过 Revision、短租约和 CAS 在 requested、preflight running、Hold/Busy blocked、preview ready、failed 与 cancelled 间转换。
- [x] **与 Legal Hold 互斥**：预检复用现有用户级 retention subject lease；活动 Hold 不做清单查询，重叠清理任务返回 subject busy，Mongo 查询失败记录固定失败码并 fail-closed。
- [x] **分类只读清单**：按代码中可证明字段分别统计直接用户资产、共享项目/权限关系和必须另行治理的审计证据；覆盖 Workflow 定义/版本/状态/补偿、Provider、MCP、任务模板、发布绑定、Profile/Marketplace 和产品归因。
- [x] **默认安全运维入口**：新增 `agent-data-deletion-request`；`request/preflight/cancel` 默认 Plan-only，必须显式 `--execute` 才修改请求控制状态，`get` 只读。命令没有物理删除动作。
- [x] **不夸大完成度**：请求固定 `execution_supported=false`，Qdrant、MinIO 与 Tweet Service 内容保持外部未覆盖；没有用户 API 或删除执行器。
- [x] **离线验证**：Repository/Service/CLI 目标测试、Agent 全包测试、关键包与 CLI Race、Agent 与 CLI Vet 通过；覆盖分类/租户过滤、成功预览、Legal Hold、并发 Claim、清单故障、默认 Plan-only 与 CLI 分发。未连接 Mongo/MinIO、未访问公网或付费 API，未启动/重启服务。
- [x] **分阶段协议与控制面 CAS**：完成八阶段协议、预检快照、Mongo 双 Revision CAS、Retention Subject Lease/Legal Hold 互斥和无 Adapter 的 Claim/Complete Coordinator；CLI 与生产组合根均未接执行入口。
- [x] **能力与审计控制面**：已补不可变 Capability Snapshot、运行时漂移拒绝、有界哈希链审计和 Checkpoint/审计同 CAS；执行开始后预检不能覆盖历史。
- [x] **Adapter v1 契约**：已定义稳定 Operation Key、Legal Hold 短期决策、不可变 Receipt、幂等回放和权威 Post-state 零残留验证。
- [x] **Mongo owned-data Adapter 边界**：固定 18 类独占集合并由预检共用；MCP/项目共享关系与治理记录排除。事务内完成权威治理重验、逐集合删除/计数、零残留和持久幂等回执；Adapter 未注册，未接 Worker/CLI。
- [x] **离线验证**：Mongo Adapter/预检/通用契约目标测试、Agent 全包、Repository/Deletion Race、Vet 与差异格式检查通过；未连接 Mongo、未启动服务或容器、未访问外网或付费 API。
- [x] **后续批次已交付**：MinIO/Qdrant/Tweet Service Adapter 边界、持久外部操作 Journal 与跨存储零残留聚合已经实现但未注册；没有真实 Mongo 副本集、故障恢复和 Live Evidence 前继续保持不可执行。

## 2026-09-25 上线收口 L4：跨存储用户删除范围原语

- [x] **MinIO 当前键范围**：Tool Result Store 可按租户哈希前缀有界计数和删除；每页先验证所有 Key 均属于目标前缀且符合当前格式，出现跨租户/畸形 Key 时零删除失败，删除后再次观察到空列表才返回。
- [x] **Qdrant Filter 原语**：REST Client 增加精确 Count 与 `wait=true` Delete-by-filter，空 Filter、非法 Collection 名和异常响应均 fail-closed；共享 Episodic Memory 可复用既有字符串 `user_id` 服务端过滤。
- [x] **Tweet Service 端口契约**：固定 authored tweets/comments、likes、bookmarks、retweets、poll votes 六类清单，命令隐藏原始用户 ID，结果摘要绑定前后计数和完成时间且强制零残留。现有逐条 DeleteTweet 没有被误接为用户级删除。
- [x] **跨存储聚合**：报告精确要求共享关系、MinIO、Qdrant、Tweet Service、Mongo owned-data 五项 Capability Snapshot/Post-state；拒绝缺失、重复、Adapter 漂移、跨用户状态、未来观察和任意残留，报告仅保存 Subject Digest。
- [x] **离线验证**：Qdrant、Object Store、Deletion 三个目标包测试通过；未连接 MinIO/Qdrant/Tweet/MySQL/Mongo，未启动服务、访问公网或调用付费 API。
- [x] **后续 Adapter 边界已交付**：统一持久幂等/治理重验包装、Tweet 内部 RPC 与共享关系 Adapter 已实现但均未注册；在 Worker、最终确认、真实故障恢复和 Live Evidence 完成前 `execution_supported=false` 不变。

## 2026-09-25 上线收口 L4：Tweet 用户删除内部事务边界

- [x] **受保护内部 RPC**：新增 `ObserveTweetSubjectData` 与 `DeleteTweetSubjectData`，服务端和 Agent Client 都纳入既有 `pkg/serviceauth` 精确方法白名单；Token 缺失时 fail-closed，不暴露到 Gateway 或用户 API。
- [x] **单事务删除与派生清理**：Tweet Service 在一个 MySQL UoW 内硬删除主体拥有的 authored tweets/comments、likes、bookmarks、retweets、poll votes，同时移除作者 Tweet 的投票/互动依赖，并为每条作者 Tweet 写幂等 `TWEET_DELETED` Outbox。提交后只做尽力缓存失效，不能把缓存成功冒充事务成功。
- [x] **持久幂等回执**：新增 `tweet_subject_deletion_operations`，Operation Key 唯一绑定 Subject/Governance Digest，回执只保存前后计数、完成时间和 SHA-256；同键回放不重复删除或写 Outbox，命令最长有效 15 分钟，残留记录、篡改回执和冲突重放均 fail-closed。
- [x] **有界事务**：单次最多处理 1000 条作者 Tweet，超过上限返回明确错误，未实现批处理协议前不尝试无界事务。原始用户 ID 仅在内部认证调用与 SQL 边界使用，不进入持久删除回执。
- [x] **未注册 Agent Adapter**：新增 gRPC Adapter 负责请求/响应双重契约校验，但没有加入 Capability Registry、Worker 或 CLI；它不能替代调用前 Legal Hold 权威重验，也不改变 `execution_supported=false`。
- [x] **离线验证**：Agent Deletion、Tweet Service/gRPC/Repository、Agent/Tweet 组合根定向测试与相关 Vet 通过；覆盖成功、零残留、幂等回放、治理冲突、过期命令、超量拒绝、Proto 字段兼容和篡改回执。未连接 MySQL/Redis/RabbitMQ/Mongo/MinIO/Qdrant，未启动或重启服务，未访问外网或付费 API。
- [ ] **Tweet 历史残留边界**：本批零残留只证明固定六类主体记录；旧 `outbox_events` 的内容/身份保留及他人回复形成的不可见孤儿内容尚未进入权威清单。Canal 日志已停止输出 Payload，但生产删除前仍需明确有界事件保留和孤儿内容策略。
- [x] **共享关系 Adapter**：已在后续批次实现事务内安全解绑与三类所有权 blocker；未注册、未接 Worker。
- [x] **Worker 与最终确认**：已实现未注册的八阶段 Worker、失败阶段恢复、结构化写栅栏/破坏性 Adapter 回执复核、五存储零残留报告和最终状态原子 CAS。
- [x] **Agent Fence Authority**：Mongo retention subject 已持久化 Fence Identity，原子安装时重验 Legal Hold Revision；同请求重入、竞争请求、治理漂移和损坏记录均有离线契约覆盖。当前没有接入生产 Worker。
- [x] **Tweet 本地门闩已完成**：每用户固定行、事务锁、六类写入口拦截、删除前置校验与受认证安装 RPC 已由下一批交付。
- [ ] **下一批**：建立 Agent/Tweet 组合门闩回执，并让 Agent 侧全部主体写入口消费统一 Authorizer；完成跨服务栅栏前不设计后台调度或用户 API，不改变 `execution_supported=false`。

## 2026-09-26 上线收口 L4：Tweet 主体写门闩

- [x] **持久串行化锚点**：新增 `tweet_subject_write_fences`，每个用户固定一行；正常写与门闩安装都在业务 UoW 内先创建锚点再 `FOR UPDATE`，关闭“预检查后删除并发写回”的 TOCTOU 窗口。
- [x] **六类写入口覆盖**：发布 Tweet/投票、点赞、评论、投票、收藏与转发均在同一事务先调用 `AuthorizeSubjectWrite`；依赖缺失、记录损坏或已安装门闩都 fail-closed。收藏、转发和投票仓储已改为消费外层事务。
- [x] **删除顺序门禁**：用户级物理删除事务先调用 `RequireSubjectWriteFence`；未安装门闩时不读取删除清单、不删除业务数据、不写 Outbox 或完成回执。
- [x] **内部安装契约**：新增 `EnforceTweetSubjectWriteFence` gRPC，绑定 Request、Subject、Plan、Capability、Governance Digest 和短期 Deadline；同请求重放返回既有 Revision，竞争请求返回冲突。RPC 纳入既有服务身份精确白名单，不暴露到 Gateway。
- [x] **错误语义与生成契约**：用户写 RPC 将已删除主体、门闩损坏或门闩依赖缺失映射为 `FailedPrecondition`；Proto 字段号和方法兼容测试覆盖新增契约，Go/gRPC 文件由本地 `protoc` 重新生成。
- [x] **离线验证**：Tweet Service/Repository/gRPC 全包、Agent Deletion Client、Tweet/Agent 组合根定向测试通过；关键 Tweet 包 Race 与受影响包 Vet 通过。未连接 MySQL/Mongo/Redis/RabbitMQ，未启动或重启服务，未访问公网或付费 API。
- [ ] **仍未上线**：Agent 侧全部主体写入口尚未统一授权，Agent Mongo 与 Tweet MySQL Authority 尚未形成单一跨服务 Fence Receipt；生产 Capability Registry、后台调度、用户 API 与 Live Evidence 继续为空，`execution_supported=false` 不变。

## 2026-09-26 Agent 本地真实链路验收（非生产放行）

- [x] **隔离账号与真实模型**：通过正常注册/登录创建隔离账号，`qwen-plus` 完成对话 Run；没有复用管理员身份。DashScope 兼容请求的两处参数错误已修复，见 Issue 224。
- [x] **Planning-Execute 站内搜索**：临时启用 Recoverable Runs、49% 稳定灰度及仅站内搜索路由后，Run `6ab7bb8ca32162e8bebbe99a` 完成；持久记录显示 `runtime.platform_search`、`planning_calls=1`、`goal_status=verified`、`verification_status=passed`，Provider 为 DashScope，规划用量为 728 Token（Provider 精确值）。这不是联网搜索、MCP 或所有 Planning 路由的验收。
- [x] **Recoverable Runs 最小持久性**：已完成对话 Run `6ab7b3edff049d840935efba` 在 Agent 重启后仍可按原账号读取，状态 `completed`、revision 2；未验证挂起审批、租约接管或跨副本 Resume。
- [x] **显式发布**：草稿 Run `6ab7b6761f070a931b8f1a3b` 产出精确测试文案，调用 `/agent/confirm` 后 Tweet `2103819831835901952` 在详情 API 与作者时间线均可读回；Mongo Run 已记录 `publishable_draft=true` 和该 Tweet ID。只使用隔离测试账号，未自动发布。
- [x] **灰度回滚**：隔离账号无挂起/运行中 Run；临时检查点密钥、Planning、Recoverable、External MCP 和统一审批恢复环境变量均已撤回，Agent 容器保留修复后的镜像并恢复原有关闭状态。
- [ ] **真实 MCP 与审批恢复**：本机协议夹具仅提供 HTTP，带 Bearer 的连接被 HTTPS 安全门禁正确拒绝；没有真实第三方 HTTPS MCP 及凭据，未执行读/审批写、挂起恢复、撤权或故障注入。不得以本地夹具替代第三方证据。
- [ ] **其余上线门禁**：Workflow-as-Tool 父子 Run、浏览器 UI（本机自动化 ACL 故障）、持久 Checkpoint Secret、生产 TLS/备份恢复/回滚及 26 项签名验收记录仍未完成。此次本地成功不等于 L3/L4/L5 放行。

## 2026-09-26 Agent 演示问题复核（仍非生产放行）

- [x] **会话排版**：Agent 回答改为 Vue 安全结构渲染，支持段落、三级内标题、列表、引用、代码块及受限 HTTPS/HTTP 文本链接；默认对话提示词要求长回答自然分段。未引入 `v-html` 或 npm 新依赖，尚未在真实登录浏览器完成视觉验收。
- [x] **会话用量可见性**：对历史消息中的唯一 Run ID 有界并发读取现有权威 Accounting API，在对话顶部展示已核算总 Token，单条回答展示该 Run Token。旧消息无 Run ID、查询失败或子工作流数据不完整时标记“部分/不可用”，不把缺失数据记作零。隔离账号对话 `000000001fcb31ab13d6fc08` 的 Run `6ab7d03fde54a8607748bcd6` 实际读取到输入 734、输出 6、总计 740 Token；尚未验证大对话的线上延迟及浏览器展示。
- [x] **热榜零分根因**：本地 Redis `trends:global` 旧条目已衰减至约 `10^-40`，旧实现转成 `int32` 后返回 0；Tweet 查询现过滤 `<1` 的非活跃话题并对有效分数四舍五入，Explore/侧栏使用后端同一话题词，显示“近期热度”而非错误的“推文数”。旧推文可搜索不等于近期热点。Redis 回归与 Web 构建通过；受控保留回滚镜像并更新 Tweet/Frontend 后，`/agent` 返回 200，`/trends` 对当前无活跃话题返回空列表。浏览器视觉验收仍因本机 CUA ACL 故障未完成。
- [x] **发布与审批代码路径**：Workflow 编辑器已有显式 `PublishTweet` 节点；普通聊天无写工具，草稿发布是独立确认动作，所以这两条路不会自然弹出工具审批。PublishTweet Workflow、父子审批桥接和幂等恢复四个离线回归通过。
- [x] **真实 Workflow 审批**：隔离账号 Run `6ab7cfd1de54a8607748bc64` 挂起后在审批收件箱可见，拒绝后 Run 为 rejected 且目标推文不存在。另一隔离账号 Run `6ab7cfffde54a8607748bc95` 在审批前无推文，批准并签发恢复授权后只发布一条唯一标识的测试推文；重复 Resume 被拒绝，推文仍只有一条。这验证直接 Workflow 写节点的拒绝、批准和幂等性，不覆盖 Workflow-as-Tool 父子审批或重启恢复。
- [x] **部署状态**：Agent、Tweet、Frontend 从当前源码离线重建并受控切换，原镜像有 `rollback-20260926-*` 标签；Agent 临时验收开关回到 Compose 配置值：Planning-Execute、Recoverable Runs、External MCP 关闭，Workflow-as-Tool 保持开启。`/agent` 返回 HTTP 200。
- [ ] **非 MCP 待验收**：Workflow-as-Tool 父子审批、过期/重启恢复，浏览器排版与会话 Token 视觉检查，Provider 超时、预算耗尽、畸形模型、Redis/Mongo 重启，以及 L4 TLS/备份恢复/回滚/告警和 26 项签名证据门禁。MCP 真实第三方读写、撤权与故障演练由用户决定稍后执行。

## 2026-09-26 对话用量历史兼容修复

- [x] 根因：`AGENT_RECOVERABLE_RUNS_ENABLED=false` 时普通 Runtime 仍给消息生成 `run_id`，但不写入 Execution Run；`GetAgentExecutionRun` 又把关闭写入开关当成历史读取禁令，导致“运行用量”弹窗返回 `agent execution run store is unavailable`。上轮仅验证了开关开启期间创建的隔离 Run，未覆盖用户当前模式。
- [x] 已存在的 Run 在关闭新可恢复执行后仍可按用户身份只读；对话历史只投影 Mongo 消息中已保存的 Runtime Token 总量与估算标志。前端优先读权威 Run，缺失时展示“仅有消息记录用量”，既不伪造输入/输出拆分，也不把缺失当零。新回复保存后回读消息用量，页面切换时停止旧会话的结果回填。
- [x] Proto 新字段仅追加，旧客户端兼容；Go 定向回归、Gateway 回归、Web 构建通过。部署态登录浏览器视觉检查与旧记录实际回读仍待验证。

## 2026-09-27 全功能离线部署交付（线上待验收）

- [x] 生成 `20260927-ecs3-full`：11 个业务镜像、18 个第三方镜像和全量 Compose 所需的 Prometheus、Promtail、Grafana 挂载配置；旧版包与镜像保留用于回滚。
- [x] 独立 `.env` 沿用已有数据库、模型、内部服务和加密凭据，启用 Workflow-as-Tool、Skill Catalog、Recoverable Runs、Planning-Execute 100% 灰度、External MCP、统一审批恢复及其他已实现的用户路径；压缩包不含密钥。
- [x] Compose 静态解析、功能开关、镜像清单、29 个归档的 SHA-256 和压缩包内容校验通过。
- [ ] 新服务器受控替换 `.env`、导入镜像并重建服务后，分别验收工作流发布、真实模型、站内/Web、MCP 授权与审批恢复、发推和监控。真实 MCP 连接、搜索 Provider Key、Marketplace 管理员身份仍需用户侧配置；此批未修改线上服务，不等于生产放行。

## 2026-09-28 Python 客服 Support Query 边界（离线实现）

- [x] **统一内部端点**：新增默认关闭的 `POST /internal/v1/support/query`，通过 operation registry 执行只读能力；当前只注册 `content.list_recent`，没有问题类别路由，也没有在 Gateway 访问数据库。
- [x] **可信主体与纵深授权**：短期服务 JWT 严格校验 issuer/audience/subject/HS256/时间/jti；用户主体只取 `act.user_id`。`content.list_recent` 拒绝未知参数，并强制用该主体调用 Tweet `GetUserTimeline`，下游返回跨主体数据时 fail-closed。
- [x] **防重放与幂等**：JWT `jti` 和 `actor + tool_call_id` 均使用 Redis 原子键与 TTL；同一工具调用的相同请求可安全重试，不同请求返回 `tool_call_conflict`。Redis 缺失时启用配置直接拒绝启动，请求期故障返回固定 503。
- [x] **最小披露证据**：响应仅返回资源引用、可见性、时间和聚合计数，不返回 Tweet 正文、原始用户 ID、服务地址或下游错误文本；Trace 仅记录低基数 operation/result/duration。
- [x] **离线验证**：Support Query Module、Gateway Middleware/Handler/Router 和组合根目标测试通过，覆盖 JWT audience/actor、防重放、幂等冲突、未知 operation、伪造 user_id、跨主体下游响应与正文脱敏。
- [ ] **真实联调待执行**：尚未启用配置、重启 Gateway 或连接 Python 服务；需要同一份仓库外 Secret、真实 Redis/Tweet Service 和隔离普通用户完成端到端 smoke。离线通过不等于生产上线。
