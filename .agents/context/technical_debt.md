# Technical Debt Index（当前技术债与已落地护栏）

> 2026-10-15 M2-R3-D：旧SQL验收不消费关注布尔值/接受空样本和同事务单页范围已修复，cmd/feed-read-acceptance v2提供独立只读RR/回滚、有界并发连续页/数据门槛、预检估计与成功延迟/失败/截断/预算判定。11个入口顶层测试及相关Repository Test/Vet/Race、CLI门禁/格式通过；内存driver不证明真实MySQL权限或隔离。没有新依赖/迁移/生产接口/客户端变化或真实负载，M2-R3整体仍Partial；高关注/稀疏深历史/资源/Redis多副本/端到端容量另验，capacity_accepted恒false。见FEED_ACCEPTANCE_RUNBOOK与ISSUES248。

> 2026-10-15 M4-R2：双端历史/会话分页、精确ID去重/页内已读合并、独立锚点3页补拉与继续、ready/前台刷新、发送草稿/迟到响应隔离已实现；Messenger有界提示发布与Gateway生命周期修复、固定指标和告警规则。目标Go Test/Vet/Race/两入口编译、Web18测试/build、Flutter零分析问题/32测试通过。M4代码/离线Implemented，整体仍Partial：索引/真实SQL计划/断网/多端/多副本/容量与告警接收未Accepted；没有提交序号/持久收件箱/发送幂等键/设备ACK，低ID迟到提交和超时不确定提交仍存在。详情见MESSENGER_RECOVERY_RUNBOOK；覆盖下方旧M4-R2 Planned状态，M8-R3现场和M2-R3容量仍Partial。

> 2026-10-15 M8-R3：旧ReleaseID/同一原始证据可填满记录的身份缺口已用v2发布/source/环境快照/部署ID绑定与逐项live观察护栏收口；31项目录/24拒绝用例、v1不可release-ready、AST及发布工具Test/Vet/Race/12服务矩阵、CI接线通过。签名仅排除根signature、嵌套字段仍认证，路径/JSON有界、写锁/独占初始化和跨平台夹具清理已补。用户本轮仅要求离线，发布可靠性与M8-R3整体仍Partial：HMAC持有者可伪造陈述，期望摘要/材料来源/权限需独立审核；没有真实CI/镜像拉取/环境/备份恢复/告警/Provider/MCP/跨端旅程证据，deployment_accepted=false。M4-R2与M2-R3既有Planned/Partial未改变，用户删除及JWT全设备撤销继续关闭；操作与下一步见DEPLOYMENT_ACCEPTANCE_RUNBOOK/现场待办。

> 2026-10-14 M8-R2：本地退出与不撤销边界显式化，Logout RPC/HTTP能力回执与全设备501、可信Context密码修改和两端账号安全入口已实现。会话代次隔离旧登录/刷新/401、Web成对保存/跨标签/页面客服重建、Mobile串行清理/排队退出竞争/认证路由/通知换代、Socket停止重连与敏感日志移除；User领域未找到与数据库故障分离、Mobile旧网关非JSON兼容已补。最终Go全仓Test、认证/Gateway相关Vet/Race、Web build/13测试、Flutter零分析问题/25测试通过。认证安全整体仍Partial：无refresh/session版本/服务端撤销或全设备退出，退出和改密后既有JWT至到期仍有效；密码并发写无CAS、真实DB/Socket/跨设备/存储失败恢复/部署旅程未Accepted。删除空能力目录与unsupported资源仍阻断执行。差异账本和后续设计见AUTH_SESSION_CONTRACT；下一M8-R3，不用本机测试代替生产验收。

> 2026-10-13 M8-R1代码与本机验证Implemented：全cmd包级构建/12组件目录、跨端配置CI门禁、显式发布与干净源码/回执/迁移摘要/只读复验已实现，自动Git/Helm/latest发布移除；完整Go Test/CI同范围Race/Vet、Windows-Linux编译、Web完整build/7状态、Flutter零分析问题/16测试与配置夹具全部通过。Mobile已发现的死导入/弃用Color API、相关await后页面有效性与WorkflowContext、旧计数器烟测修复，不代表完整异步/真实接口审计。发布可靠性整体仍Partial：真实12镜像/Actions/锁安装/Registry签名与clean-host/权限/旅程未Accepted；候选digest不证明可拉取，未签回执非真实部署证据，基础设施/Secret/配置身份沿M8-R3；历史Helm/tag导出与公开基础tag/Actions/apk不证明全供应链可复现。下一代码轮次M8-R2，见RELEASE_PIPELINE_RUNBOOK/ISSUES244。

> 2026-10-13 M7-R2覆盖下方Agent装配/准入历史状态：Persona Port+SQL适配器、Worker移除Persona DB依赖/凭据、API拥有认知引擎，Runtime外Redis共享Run/Provider租约/续租/取消/估算预算、显式初始化、完整Runtime模式启用门禁和固定指标告警已实现；完整Agent Test/Vet/相关Race通过，开关默认false。整体隔离与成本治理仍Partial：Worker其他依赖仍保留、真实角色副本/权限/Redis持久化淘汰/时钟/不确定回执/容量/账单未验收，摘要/RAG/Embedding/Reranker/Temporal/Eval仍不受共享费用控制；租约不是外部Provider围栏，CLI声明不是持久RBAC，Persona主体删除写围栏未收口。停/排空owner后统一切换并保留状态，禁止Redis故障自动转本地或删键/换namespace规避预算。后续M8-R1和独立真实验收见ARCHITECTURE_REMEDIATION_PLAN/AGENT_SHARED_ADMISSION_RUNBOOK。

> 2026-10-13 M7-R1：运行AutoMigrate/Persona构造吞错DDL已移除；Owner目录/有界只读Schema/独立建缺表Job、生产八角色独立凭据/固定权限计划、Tweet只读Follow Port，扩大测试/Vet/Race通过。数据隔离仍Partial：未创建真实账号/执行GRANT/DDL/Job镜像或验收拒绝越权，配置不同用户不证明权限不同；共享MySQL/Outbox/Feed候选SQL/Consumer读仍保留，CDC服务器级binlog与其他账号路径需专项治理。角色分支前Agent MySQL/Persona应用Port与共享Admission留M7-R2，既有Schema长度/默认值/触发器等不由本轮门禁完整验证，精确索引名会拒绝历史异名等价索引。禁止回滚恢复旧运行DDL。

> 2026-10-13 M6-R2覆盖下方互动R1后续：原50秒衰减锁/ZINTERSTORE已移除，新增服务器分钟桶/原Top100原子裁剪+0.95/保留TTL和10000基数门禁；互动独立会话重建/协议取消/QoS1，新专用CLI默认离线且受控重放。扩大测试/Vet/Race通过。互动整体仍Partial：真实RedisAOF/OOM/淘汰/多实例崩溃/容量、Broker绑定/双连接恢复/权限未Accepted；超基数时衰减暂停，持续写入仍可扩大热key，需容量治理。Header计数不覆盖原DLQ ACK不确定、本机声明不是持久RBAC，其他生命周期连接恢复/完整关闭矩阵仍待后续；禁止混跑旧衰减owner。

> 2026-10-13 M6-R1：互动先ACK/逐条goroutine/内存batcher及分离频控计分已替换为有界串行消费、当前公共事实、Redis Lua原子回执后ACK；点赞新关系才写稳定事件，专用Retry/DLQ与观测，扩大回归/Vet/Race通过。整体互动可靠性Partial：旧多副本分钟衰减/50秒锁、人工互动重放/断连恢复管理归M6-R2，真实MySQL唯一键/锁、Redis持久化/淘汰/热点和Broker绑定未Accepted；Redis标记仅7天，丢失标记不证明全局exactly-once。旧LikeRepository.Like兼容方法仍保留，生产Service改用ApplyLike，禁止新Writer调用旧路径绕过事件。部署不得混跑旧互动消费者。

## 2026-10-12 当前代码评审覆盖

本节覆盖下方历史审计中已过时的能力判断；完整证据、影响和验收路径见 `docs/ARCHITECTURE_REVIEW_2026-10-12.md`。本轮未修复业务代码，7 个目标 Go 包测试、同范围 Vet 与 Web 构建通过不代表这些业务缺口已覆盖。

| 技术债 | 状态 | 当前证据 / 下一步 |
|---|---|---|
| P0 公共 Tweet 检索可见性 | Partial（M1 代码/离线闭环） | 2026-10-12 已新增直接 MySQL 的 GetPublicTweets 与共享 retrieval Adapter，MCP 语义/混合搜索和 MemoryManager 公共知识在重排/正文输出前校验，拒绝旧正文兜底；目标/扩大回归、Vet/Race 通过。真实删除/治理后的部署态读回、索引清理及其他作者/详情/列表入口仍待验收，不宣称全站授权完成。 |
| P1 Feed分页与缓存恢复 | Partial（M2-R1/R2/R3跨端与v2工具离线闭环、既有小数据Live SQL） | 当前关系/权限/扫描预算/租约与双端去重换代/空页续拉通过。2026-10-12 MySQL3307的316推文/fixture1关注对象首页/第二页p95 5.910/5.997ms仅旧v1单事务历史。R3-D的v2独立RR/有界并发连续页、true关注校验/数据门槛/固定报告Test/Vet/Race通过；未执行v2真实负载，近似表行数与有界关注样本不证明完整数据集。高关注/稀疏深历史/资源、Redis多副本和端到端容量仍待验收。 |
| P1 大 V 关联完整性 | Partial（M3-R3代码/离线闭环，默认关闭） | Feed权威读取隔离集合缺项；新Worker作者共享游标/历史关系100条页/当前关注批查、5000/4500防抖和首次迁移兼容，5012粉丝传播/断点/降级测试通过；旧Service广播/Align执行移除。真实新表/权限/绑定、Redis淘汰后强制修复、跨副本L1窗口/热作者/高历史容量未验收，不能宣称集合永久完整。 |
| P1 删除与互动投影可靠性 | Partial（M5-R1/R2/R3代码与离线闭环） | 删除事实/事件原子入库、独立删除及text/vector创建任务、双lane、锁外Embedding/Profile/当前事实重验与元数据audit/审批修复已接线；可选Confirm首次回执账本默认关闭、手工迁移未执行，显式创建/删除/治理重放与发布感知保留候选计划；积压/耗尽/遥测失败指标告警，扩大回归/Vet/Race通过。元数据一致不证明正文/向量质量，命名锁不防远端迟到写，真实Schema/后端/路由/消费/故障恢复/历史范围与召回未Accepted；无全库扫描/自动历史补偿/GC删除，互动ACK-before-async及计数幂等留M6，维护拒绝点赞/评论重放。 |
| P1 Follow 公共 Cursor | Partial（M3-R1 代码/离线闭环） | 2026-10-12 Service 已消费 FollowerPage/FolloweePage，返回关系 ID Cursor，独立公开 100 条上限与仓储 limit+1；Gateway 明确拒绝非法游标并保留大整数字符串/空数组。SQL DryRun、跨层 Fake、目标/扩大回归、Vet/Race/Web build 通过；真实 MySQL 与部署验收仍待完成。旧 IDs-only 方法保留，Feed/大 V 全量截断归 M2/M3-R3。 |
| P1 Follow 派生状态恢复 | Partial（M3-R2/R3代码与离线闭环） | 事实/Outbox原子，v1入口receipt/双任务登记后ACK、author/pair独立槽/100条页/当前代次/共享作者进度、SQL与Redis围栏/Retry-DLQ/指标/显式审批修复已接线，旧goroutine移除；扩大回归/Vet/Race和最终复验通过。开关默认false、DDL未执行，真实锁/提交/绑定/Redis故障/多实例恢复未Accepted；DBComplete不证明缓存未淘汰，无历史关系的残留需显式pair，热作者/深历史与全量发现/留存治理仍待后续。 |
| P2 发布与扩容边界 | Partial（M7-R1/R2/M8-R1代码与本机验证） | Owner/独立迁移/角色账号与Persona/共享准入、12镜像目录/全cmd包构建/跨端配置CI/显式发布/摘要清单已实现；完整Go/Race/Web/Mobile验证通过。真实账号/Redis/账单/CI/镜像/Registry/部署未Accepted，其他Provider与Worker依赖/历史Helm仍保留。 |

> 2026-09-26：工作流补偿状态转换、运行取消和最终提交已接入主体写入计数；已证明的无写入冲突释放计数，未知写入结果保留。其他 Agent 直接/后台写入、正计数受控恢复和真实 Mongo 竞争验收仍阻断生产删除。

> 2026-09-26：工作流事件、快照、补偿计划及运行游标已使用主体写入计数，批量写入要求同一用户。补偿执行状态、运行取消/最终提交与其他 Agent 直接写路径尚未统一；正计数恢复与真实 Mongo 演练前生产删除仍关闭。

> 2026-09-26：工作流定义、运行记录基础写入及旧版本补建已接入 Agent Mongo 主体计数。事件/快照/补偿/运行控制仍有直接写入，错误后正计数也缺少受控恢复；生产删除不得开启。

> 2026-09-26：Agent 对话创建和消息写入已用主体计数与围栏安装串行化；其余直接仓储/后台写入仍只有部分 Service 写前检查。异常中断留下的正计数会阻断删除，尚无人工对账与受控恢复能力。真实 Mongo 并发/故障验收前继续关闭生产删除。

> 2026-09-26 删除围栏债务：复合 Agent/Tweet 回执已实现但未注册。Tweet 侧事务门闩具备本地串行化；Agent 对话创建与消息写入已登记主体计数，其余入口多为写前读取围栏，检查至提交的竞态与后台写路径未闭环。删除执行仍不得开启。

> 最近核对：2026-09-26
> 详细证据：`architecture_audit_report.md`、`docs/AGENT_CORE_REFOCUS_PLAN.md`、`docs/AGENT_RUNTIME_STRENGTHENING_PLAN.md`、`docs/ISSUES.md`

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
| Timeline 创建派生投影 | Partial | `tweet.created` 在 ACK 前完成幂等 ZSet、72 小时趋势 Marker 和唯一 `sync_text/sync_vector` 入队；旧 sync_es 转子任务。两独立 lane、MySQL 8 Claim/Lease/Owner/Token围栏、过期恢复/60秒执行、低基数任务与持久积压观测；显式元数据对账/修复已实现 | 点赞/评论仍是 ACK-before-async 且计数未做事件级去重；索引成功但 Outbox 回执丢失仍可能重复模型/网络成本，元数据一致不证明索引正文/向量质量；真实后端/多副本/容量与Profile迁移召回未验收 |
| Agent 组合根职责过载 | Partial | `internal/module/agent/startup` 已提供 `api/worker/all` 启动计划；`cmd/agent-service/main.go` 按角色隔离 gRPC/MCP/Consul 与 MQ/Temporal/巡检，但依赖装配仍集中在单文件 | 下一步把 API/Worker 装配函数和 Kubernetes Deployment 物理拆开，继续收紧 Worker 依赖 |
| 热点播报双调度风险 | Resolved | 生产组合根只注册并启动 Temporal `TrendingReporterWorkflow`；`AGENT_TRENDING_REPORTER_OWNER` 只接受 `temporal|disabled`，本地 `TrendingReporter.Start` 不再自动接线 | 滚动升级先替换旧 Worker；长期可删除未注册的 Legacy Reporter 实现 |

## P1：架构演进债务

| 技术债 | 状态 | 代码证据 | 风险/下一步 |
|--------|------|----------|-------------|
| Workflow Event Sourcing 未闭环 | Partial | Run 固定不可变 Revision；StateEvent/Snapshot 幂等持久化并支持恢复、公开只读 Replay 和版本化 Blackboard 检索；独立 Trace、有界事件流、Reducer、节点状态机、持久 Compensation 与跨实例取消已落地 | 补长期保留/归档策略；审批型补偿仍需显式重试，不宣称完整 Event Sourcing 或所有补偿无人值守 |
| Qdrant 每用户一个 Episodic Collection | Partial | 新写入使用共享 `agent_episodic_memory` + 服务端 `user_id` Filter；迁移命令提供 `--verify-only` 与 JSON 报告；旧集合仅有界双读 | 在受控环境执行真实回填和只读验收后人工确认删除旧集合 |
| Agent Trace/Replay 不完整 | Resolved | Run/Step/LLM/Tool 独立 Trace、Metrics、OTel、只读 Replay、有界事件流、Tool Result 引用、Blackboard 检索、模板身份、安全采样与 Grafana 面板已落地 | Legacy 路径随 Runtime v2 灰度补细粒度信号；长期 Trace 留存/归档属于生产部署增强 |
| Agent 应用数据保留 | Partial | 默认关闭/Dry-run 的常规 Worker 已覆盖对话、Trace、Checkpoint 与 Tool Result；用户删除已有单活动请求、CAS 预检、八阶段协议、五类存储 Adapter、未注册的可恢复 Worker和 Agent Mongo 持久 Fence Authority。Tweet Service 已以每用户 MySQL 门闩行和事务锁覆盖发布、点赞、评论、投票、收藏、转发，并要求删除前已安装门闩；受认证安装 RPC 已具备。Worker 每阶段重验治理和 Receipt/Post-state，跨存储再次要求五项零残留 | 上线前仍需让 Agent 全部主体写入口消费统一 Authorizer，把 Agent/Tweet 两个 Authority 组合为 Worker 单一回执，并补后台生产装配、用户 API、所有权转移用例、旧 MinIO Key 策略，以及真实 Mongo/MySQL/MinIO/Qdrant 故障恢复和 staging 删除验收；预检与离线测试不等于生命周期闭环 |
| Messenger 历史分页与实时恢复 | Partial（M4-R1/R2代码与离线闭环） | 消息排他Cursor/会话ID上界、页内已读；双端历史/会话续页、去重/独立锚点3页恢复与继续/草稿隔离；有界Redis提示与Socket ready/心跳/到期/reader生命周期、固定指标。目标Go Test/Vet/Race、两入口编译、Web18测试/build、Flutter零问题/32测试通过 | 索引未应用，真实SQL计划/迟到提交/断网/多端/多副本/容量/告警未Accepted；大历史分组查询成本需独立会话投影，Pub/Sub非持久投递。提交序号/幂等发送/收件箱/设备ACK仍待设计，HTTP页内已读非展示回执。 |
| Auth Logout/Token 撤销 | Active | `module/auth/grpc/auth.go` 的 Logout 尚未实现；2026-10-12 已核对 Register 通过真实 UserClient 接线，不再作为剩余债务 | 明确服务端退出与 Token 撤销语义，补契约验收 |

## P2：Agent 强化中的 Partial

| 能力 | 状态 | 已有护栏 | 剩余工作 |
|------|------|----------|----------|
| Goal/完成验证 | Partial | TaskSpec、CompletionCriterion、Environment Snapshot、追加式 EvidenceLedger、领域 Verifier、低敏 TaskOutcome、Tweet Write 前后状态和 20 个端到端任务矩阵已建立；E2E-02/11/12/18 离线证据、E2E-05~09/11 默认关闭迁移对照、E2E-13/14 写入验收、E2E-15/16 MCP 授权撤权、E2E-17 Workflow 精确 Revision/child Run/输出验收、E2E-19 双恢复单写及 E2E-20 Provider Outage 验收已完成 | 生产 RunAgent 迁移、部署态 TweetService/Mongo 审批验收、真实 Profile/Provider 符合率、真实外部 MCP/持久授权和真实 Provider 故障演练 |
| VerifiedRunner | Complete | opt-in Adapter 已完成工具交集、快照、可信结构化证据、有界修复、checkpoint/resume、恢复重授权、累计预算、挂起边界证据结晶、单调 Checkpoint Revision、摘要化 Resume Evidence 和显式领域挂起验证；`PlannedVerifiedRunner` 已消费澄清、回答、只读计划及一次固定脱敏恢复，TaskOutcome 保留计划、失败 Observation、Artifact/Evidence 与 Usage | 固定迁移矩阵已完成；生产切换仍需部署态 Provider/Profile、审批恢复和外部依赖验收，不因离线矩阵通过自动开启 |
| Runtime | Partial | 统一 Runner、Action、Token/Cost Budget、并发准入、Usage、Rollout、Trace | Runtime v2 全模式灰度、共享式多实例并发配额 |
| Workflow-as-Tool | Partial | 用户显式发布、不可变 Revision/DSL Hash、Revision CAS、租户目录、统一 ToolExecutor、父子 Run 谱系、两类 Continuation，以及 E2E-17 Actor/Publication Snapshot、权威 child Run 与双输出摘要验证已完成 | 真实 Mongo/进程重启/生产 Goal 路由验收；不再扩张 Skill、模板或 Multi-Agent 控制面 |
| Model | Complete | model_kind 生效、Catalog Provider/Fallback 路由、Pricing Version、Endpoint Policy、固定 Provider 失败分类、策略允许/拒绝/耗尽轨迹、blocked 低敏证据与 Provider 低基数指标 | Provider 质量评估、真实故障演练与自动路由策略仍需 Eval 数据支撑 |
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

## 2026-08-10 Goal Runtime 债务校准

- G2 的写状态观察与领域 Verifier 已完成，不再把“缺少写操作 Before/After 事实验证”列为剩余债务。
- Goal/完成验证整体仍为 `Partial`：生产 RunAgent 未迁移到 Goal Runtime；E2E-13/14 与 E2E-19 已有受控进程内报告，但显式部署态 TweetService/Mongo 审批、真实加密 Checkpoint、多副本恢复与真实账号隔离验收仍缺失。
- G3 已完成：模型产生一至三步短计划，确定性 Admission 对 Catalog、Policy、Budget、所有权和 Approval 做逐步校验；G5 已删除无生产调用者的关键词 Planner 与命名兼容构造器，能力路由只接受显式选择并经 Catalog 准入。
- Marketplace、复杂 RBAC、更多固定 Profile 和管理 RPC 继续冻结，不因 G2 完成而重启扩张。
- G3 已完成 Runtime 短计划契约、确定性准入、严格模型 Adapter、Admission 协调层、opt-in VerifiedRunner 计划消费、一次安全重规划、显式能力默认 Planner 和 E2E-02/11/18 TaskOutcome 证据；写与风险计划仍 fail-closed。
- 当前优先债务转为 G4：首个只读任务在默认关闭开关下迁移到 Goal Runtime，保留 Legacy 单一执行所有者并双记录结果；不得重复模型或工具调用。
- E2E-15/16 已补齐受控进程内 MCP 证据与撤权语义，但真实外部 MCP Provider、Mongo 授权记录、项目成员撤权跨进程传播和生产 Goal Runtime 仍是明确技术债。
- E2E-17 已补齐受控进程内 Workflow 精确发布绑定、父子谱系和权威输出证据；真实 Mongo 持久 Run、进程重启与生产 Goal Runtime 仍是明确技术债。
- E2E-19 已补齐受控进程内双 Checkpoint Revision、Resume Evidence、挂起前 Observation 结晶和单写恢复；真实 AES-GCM Mongo Checkpoint、多副本 CAS Claim/租约、进程重启与生产 Goal Runtime 仍是明确技术债。
- E2E-20 已补齐受控进程内 Provider 失败分类、Catalog 允许回退、永久错误拒绝、路线耗尽 blocked 和无伪造输出证据；真实 DashScope/LM Studio 受控故障、线上路由指标与生产 Goal Runtime 仍是明确技术债。
- G5 首个清理增量已删除关键词能力 Planner 和专属测试，减少一条静态推断执行入口；`compat.chat/consult/assist` 仍服务历史 Dialogue 恢复与指标，未完成持久数据迁移前不得误删。下一优先债务是重复固定编排和 Eval TaskOutcome 报告审计，不是重开控制面。

## 2026-09-25 用户删除执行协议剩余债务

- 已完成存储无关的八阶段 Plan/Checkpoint、不可变 Capability Registry/Snapshot、预检快照持久化、Mongo 双 Revision CAS、控制面 Claim/Complete Coordinator，以及与 Checkpoint 同 CAS 的有界哈希链阶段审计。
- 通用破坏性 Adapter 已具备稳定 Operation Key、短期 Legal Hold 决策、摘要化 Receipt、幂等回放和权威 Post-state 零残留契约。未注册的 Mongo owned-data、共享关系、MinIO/Qdrant 与 Tweet Adapter 均已实现；未注册 Worker 会按八阶段短租约执行，每次破坏性写入前重签治理决策，失败后只重试当前阶段，再聚合五个权威 Post-state。最终收据绑定 Capability/Plan、Checkpoint Revision 和前七阶段回执，最后一次 Mongo CAS 同步写入 completed。项目、项目级 MCP 创建者或市场发布者所有权仍会阻断 Plan，不能被普通成员解绑隐式处理。
- 当前协议故意缺少可用 Capability，`ready=false` 且 `execution_supported=false` 是上线停止线，不应通过配置或手工改库绕过。
- Tweet 删除零残留当前只覆盖固定六类主体内容/关系和作者 Tweet 的关系依赖；历史 `outbox_events`/消息保留周期及他人回复形成的不可见孤儿内容尚未纳入权威清单。生产启用前必须给出有界保留/清理规则和 Live 证据，不能把本批 RPC 描述成完整账户抹除。
- Agent 侧 Mongo Fence Authority 已可持久安装 Fence Identity、以 Legal Hold Revision CAS 重验治理、同请求携新短期治理凭据重入，并通过 `AuthorizeSubjectWrite` 对损坏记录 fail-closed；它尚未被全部 Agent 写入口消费，也不覆盖 Tweet Service 本地写入。
- 真实执行前仍必须补 Agent/Tweet 全写路径接入、后台调度与显式启用控制、用户 API、所有权转移用例、生产装配与正式 Live Evidence；现有离线测试只证明 Worker 状态机、失败阶段恢复、Capability 漂移门禁、审计/Checkpoint/终态原子 CAS、Fence Authority 契约和跨存储报告契约，尚未进行真实 Mongo 多副本接管或 MinIO/Qdrant/Tweet 故障恢复。
