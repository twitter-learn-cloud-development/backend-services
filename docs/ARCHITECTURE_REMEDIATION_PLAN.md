# 架构修复与功能完善开发计划

> 建立日期：2026-10-12。依据：`ARCHITECTURE_REVIEW_2026-10-12.md` 的 F1—F10。按功能模块组织、按轮次交付；每轮独立记录代码、测试、剩余风险。此计划不替代 `PRODUCTION_LAUNCH_COMPLETION_PLAN.md` 的真实部署验收。

## 执行约定

当前增量（2026-10-15）：M4-R2代码与离线验证已完成，覆盖下表R2-A至R2-E；后文早期交付记录中的“M4-R2 Planned”是当时状态，以本增量和模块总表为准。M8-R3现场与M2-R3完整容量继续Partial。

| 功能模块 | 轮次 | 本轮开发与验证结果 |
|---|---|---|
| Messenger | M4-R2-A 有界提示发布 | 入库后统一2秒发布预算、保留Trace、无后台发送goroutine；零订阅/失败分开计数，失败不改报入库失败 |
| Gateway | M4-R2-B Socket恢复边界 | 订阅ACK后ready、单writer/心跳/超时/大小上限/到期关闭、reader取消与等待，修复重复关闭通道；两入口编译和目标Go Test/Vet/Race通过 |
| Web | M4-R2-C 历史与补拉 | 会话/消息续页、精确ID去重/已读单调合并、独立恢复锚点和3页预算、继续同步/失败游标、前台/ready补拉；18项测试与构建通过 |
| Mobile | M4-R2-D 历史与补拉 | 同样分页/恢复状态、Widget卸载隔离、接收本账号其他设备消息、失败保留草稿；零分析问题和完整32项测试通过 |
| 契约/观测 | M4-R2-E 运维与边界 | API/运行手册/固定低基数指标和两条告警规则、持久投递需求；无新Proto/Schema或依赖升级 |

迁移与回退、准确命令、失败记录和现场验收清单见`MESSENGER_RECOVERY_RUNBOOK.md`与ISSUES247。先升级Gateway再升级客户端，兼容旧Gateway的transport ready/前台轮询；回退保留消息/索引。已读仍仅为HTTP本页接收者标记，没有设备ACK/展示确认；ID迟到提交、无幂等发送、持久投递、真实DB/断网/多副本/告警接收/容量尚未验收，不能标记Accepted。

- 状态：Planned / In Progress / Implemented（离线验证）/ Partial（代码/离线完成，必需 Live 证据未闭环）/ Accepted（真实环境验收）。未启用代码、Fake 测试和预检不得标记 Accepted。
- 单轮只处理一个明确模块；共享契约先完成生产者，再适配调用者，再验证。
- 现有用户未提交修改保留；不进行一次性重写、破坏性数据清理或无关依赖升级。
- 代码范围内的可逆开发与离线验证已获本次请求授权。部署、付费 Provider、真实外部写入、生产数据删除单列验收条件，不能用离线结果代替。
- 每轮验收：gofmt、目标测试、Vet、契约/依赖编译、diff check；涉及共享状态时 Race，客户端变化分别验证 Web/Mobile。
- 进度事实统一登记 `PROJECT_PROGRESS.md`，稳定结构变更同步 project_map，债务同步 technical_debt，失败同步 ISSUES。

## 模块—轮次总表

| 模块 | 轮次 | 对应发现 | 交付范围 | 前置 | 状态 |
|---|---|---|---|---|---|
| M1 搜索可见性与可信上下文 | R1 权威公共读取 | F1 | TweetService 新增有界批量公共读取，直接读取当前数据库，排除删除/隐藏/非公共记录；追加 Proto 契约 | 无 | Implemented（离线验证） |
| M1 | R2 MCP/RAG 接入 | F1 | 索引仅提供候选；当前正文经权威读取后进入 Reranker/Evidence/上下文；删除旧正文成功兜底；生产装配统一 Port | M1-R1 | Implemented（离线验证） |
| M1 | R3 回归与验收 | F1 | 删除、Shadowban、私密、旧正文、伪造响应、服务不可用、取消、限量批量与跨入口测试；目标/全模块/Vet/Race | M1-R2 | Implemented（离线验证）；Live 待验收 |
| M2 Feed 完整性 | R1 稳定分页 | F2 | 来源 limit+1、按消费候选推进游标、过滤后有界补齐；处理空页与隐藏候选、精确大 ID 与 HTTP 字符串游标 | M1 | Implemented（离线验证） |
| M2 | R2 缓存重建与关系校验 | F3/F8 | 空缓存初始化标记、按用户租约重建、关注事实批量校验、权威恢复大 V 关联/完整历史读取 | M3-R1 | Implemented（离线验证） |
| M2 | R3 跨端与容量验收 | F2/F3 | 跨端分页/去重/请求换代；R3-D追加独立只读事务/有界并发连续页/数据门槛/脱敏v2报告 | M2-R2 | Partial（跨端/工具离线及既有小数据集Live通过；v2真实并发与完整容量待验收） |
| M3 Follow 关系与派生任务 | R1 Cursor 契约 | F5 | 关系 ID Cursor、limit+1 内部前瞻、FollowerPage/FolloweePage；现有 API 字段兼容 | 无 | Implemented（离线验证） |
| M3 | R2 关系事件原子提交 | F8 | Follow/Unfollow与Outbox短事务、pair串行化/代次事件/重复状态；持久暂存队列/Relay路由，迁移和权限清单 | M3-R1 | Implemented（离线验证；持久消费归R3） |
| M3 | R3 晋升/降级与回填清理 | F3/F8 | 持久Consumer/双lane、作者共享进度/当前关系代次分页、Retry/DLQ/围栏/告警，移除旧owner、显式受控检查修复 | M3-R2 | Implemented（离线验证；开关默认关闭、Live未验收） |
| M4 私信历史与投递 | R1 历史分页 | F6 | 消息 ID Cursor；会话最新消息排序分页；limit+1、NextCursor、输入上限与页内已读 | 无 | Implemented（离线验证） |
| M4 | R2 跨端和实时恢复 | F6 | 双端历史/会话续页、ID去重、独立锚点3页补拉与继续；有界推送/Socket生命周期/失败指标；持久送达需求单列 | M4-R1 | Implemented（代码与离线验证；真实恢复/跨设备/容量待验收） |
| M5 删除与索引生命周期 | R1 删除 Outbox | F4 | 数据删除与 TWEET_DELETED 同事务；旧事件兼容，失败不返回伪成功 | 无 | Implemented（离线验证） |
| M5 | R2 版本化派生投影 | F4 | ES/Qdrant 有界持久删除任务、创建任务重验当前状态/墓碑；乱序/回执丢失重放 | M5-R1/M1 | Implemented（离线验证，采用当前事实重验） |
| M5 | R3 文本/向量拆分与对账 | F4 | 独立文本/向量任务与 lane、Profile/重试/告警；显式元数据对账、受控修复登记与发布感知保留计划 | M5-R2 | Implemented（离线验证；Live 未验收） |
| M6 互动热度一致性 | R1 原子幂等投影 | F7 | 稳定关系/评论事件、Redis原子去重/频控/计分、成功后ACK、专用Retry/DLQ | 无 | Implemented（离线验证；Live未验收） |
| M6 | R2 重放与多副本 | F7 | 专用有界重放/独立互动会话重建、服务器分钟桶原子衰减/TTL/基数上限 | M6-R1 | Implemented（离线验证；真实恢复/容量未验收） |
| M7 服务与运行隔离 | R1 数据所有权/迁移 | F9 | Owner目录/有界启动只读Schema、独立Bootstrap Job/访问计划、生产角色凭据/Tweet只读Follow Port | M3/M5 | Implemented（离线验证；真实Schema/账号/Job未验收） |
| M7 | R2 Agent 依赖与共享准入 | F9/F10 | API拥有Persona Port/MySQL与认知引擎；共享Run/Provider租约、估算预算、取消恢复、显式初始化与启用门禁 | M1 | Implemented（离线验证；默认关闭，真实依赖/费用/部署未验收） |
| M8 发布与功能收口 | R1 完整发布流水线 | F10 | 全cmd包级构建/12镜像矩阵、跨端与配置CI门禁、显式发布、干净源码/回执/配置/迁移digest清单与只读复验 | 核心代码轮次 | Implemented（代码与本机离线验证；真实CI/镜像/部署未Accepted） |
| M8 | R2 认证与剩余产品契约 | 功能账本 | 本地退出/不撤销回执、密码修改HTTP与两端入口、会话代次/401/持久化/Socket隔离、差异账本；删除继续关闭 | 认证专项设计 | Implemented（代码与本机离线验证；全设备撤销仍Planned，部署未Accepted） |
| M8 | R3 部署用户旅程 | 全部 | v2发布/环境/观察签名绑定、31项验收与CI离线夹具；精确staging/恢复/权限/故障与用户旅程另验收 | M1—M8代码验证 | Partial（工具Implemented且离线验证通过；用户本轮仅要求离线，真实现场未Accepted） |

执行顺序：M1 → M3-R1/M4-R1 → M2 → M5 → M3-R2/R3 → M6 → M7 → M8。出现共享契约依赖时先完成对应生产者；未通过本轮离线门禁不进入下一轮。

## M1 搜索可见性与可信上下文

架构：TweetService 保持当前业务事实与可见性所有者。新增公共检索专用批量读取，不接受任意用户 ID 作授权；搜索产品只消费当前公共推文，私人/粉丝/Shadowban 一律不进入公共知识召回。Agent `retrieval` Port 只负责有界调用和契约校验，Runtime 核心不依赖数据库/Proto。

迁移：追加 RPC/消息，不改既有字段编号；先升级 TweetService，再升级 Agent。旧服务返回 Unimplemented 时关闭公共检索并明确失败。ES/Qdrant 无需立即清库；本轮在正文进入模型前隔离旧索引，物理清理交给 M5。

回滚：回退 Agent 代码时必须继续关闭不安全公共检索或保留可见性修复，禁止重新启用索引正文兜底。追加 RPC 可保留；不回滚数据库数据。

风险：权威数据库增加批量读取负载；限量、去重、超时、一次调用与低敏错误控制成本。服务故障影响检索可用性，应返回明确失败，不能冒充无结果。可读性为查询时快照，不承诺整个模型生成期间不可变。

验收：混合候选含已删/隐藏/私密/旧正文时，只有当前公共正文进入重排、结构化 Evidence 和 RAG。权威调用失败没有正文成功返回；坏 ID、重复/越界响应、超量与取消被拒绝。测试使用 Fake/受控 HTTP，不使用真实 Provider。认知上下文失败采用既有“记录 failed 并省略该块”的普通对话降级，不能描述为站内检索已成功；显式 MCP 检索返回工具错误。

本模块只收口语义/混合搜索及公共知识召回；历史作者推文/按 ID 详情工具、其他列表的完整可见性与缓存治理需作为后续 M2/M5 的入口清单继续核查，不能将单模块修复宣称为全站授权审计完成。

## M2 Feed 完整性

M2-R3补充轮（2026-10-15，工具Implemented且离线验证通过，完整容量仍Partial）：强化现有`cmd/feed-read-acceptance`，不改生产Feed接口/SQL或客户端。

| 功能模块 | 轮次 | 开发计划 | 验证入口 |
|---|---|---|---|
| 容量工具 | R3-D1 并发与快照 | 每个样本独立只读RR事务，最多8并发/100样本/20页/60秒；连续ID与可见性/关注检查，不共享长事务 | Fake session并发/取消/关闭与非法页测试，Go Race |
| 数据与报告 | R3-D2 证据质量 | 有界关注对象探针/表行估计/EXPLAIN；最少有效页/行/关注门槛，固定脱敏失败码，v2范围/预算/分位数与失败统计 | CLI边界、空样本/关注false/泄漏/预算测试 |
| 操作收口 | R3-D3 使用与回退 | 更新命令与报告版本，明确只测候选SQL+关注；默认不联网/不读凭据，无DDL/种子/Redis，真实容量另验 | 目标Test/Vet/Race、CLI默认拒绝/格式检查 |

迁移：无DB/Proto/生产依赖变更，既有参数保留但报告升级v2、样本事务独立；旧v1报告保留历史解释。回退只回退验收工具，不改消息/Feed数据。风险：并发增加DB压力，池与页数/时长有界；RR各样本快照独立，不证明生产跨请求事务快照或实时变化。报告仅候选SQL和关注校验、没有Stats/Gateway/Redis/UI/完整数据集覆盖，不给出自动容量Accepted结论。当前仅开发与离线验证，真实依赖/规模与负载需按运行手册确认。

交付验证：验收入口与Tweet/Follow Repository目标Test/Vet/Race通过；补齐JSON的sample_checks_passed/latency_budget_exceeded后入口再次Test/Vet/Race通过。database/sql驱动夹具核验生产GORM调用、ReadOnly+RR/逐样本回滚和无Commit；并发取消、精确ID/lookahead/连续边界、空样本/不足门槛、关注missing/非法页、分位数和脱敏覆盖。实际命令、CLI默认拒绝/参数拒绝及格式检查登记PROJECT_PROGRESS；ISSUES248记录旧v1验证缺口。Web/Mobile与生产读取无改动，未重复客户端构建；没有真实依赖/数据写入/索引/压测或部署操作。详细参数和v1/v2兼容见FEED_ACCEPTANCE_RUNBOOK。

M2-R3 本轮方案：Web Home 使用可测试的 FeedPager 状态核心，Mobile FeedNotifier 使用请求 generation，列表按字符串 ID 去重，页签/账号/刷新换代丢弃迟到响应；同代加载互斥，错误保留成功页和失败游标，最多自动续拉 3 个无新增记录页，再提供继续按钮；UI 显示 Loading/Error/Retry/Empty/End，不以空数组判定结束。客户端行为变化不改服务契约/存储，可按客户端版本回退。离线扫描预算/并发夹具报告只能证明本机执行边界；补显式 `--allow-live` 的只读 MySQL 验收入口与清晰环境说明后再请求确认真实依赖。真实 Redis/MySQL 依赖未确认时不连接、不写演示数据、不把离线 Benchmark 当生产容量。

M2-R2 实施方案：新增 `FollowingFeedRepository`，在当前共享社交 MySQL 中用 `EXISTS active follows` 获取当前调用者自己的记录以及所有有效关注对象的 Public/Follows 推文；候选直接按推文 ID 有界分页，不枚举/截断关注列表、不按大 V Redis 集合决定候选。每批交付前由 FollowReadRepository 批量重验至多 100 个作者，取关/关系读取失败不返回旧权限正文。与原计划的 Redis 大 V 关联集合在线全量重建不同，本轮以权威查询恢复有效关联，移除 Feed 对残缺关联/每作者旧 100 条历史缓存的读取依赖；旧集合和派生后台任务仍归 M3-R3。普通缓存重建合并最新至多 1000 条权威候选，3 秒跨副本租约、Token 条件提交、最多 2 秒查询、v2 有 TTL 空/非空标记；非空缓存丢失时重建，原子合并保留并发 Fanout，失败不安装初始化标记。缓存故障只影响投影，当前数据库页仍可返回；权威查询/关注校验失败明确失败。避免低效的大 V N 路 Pipeline 和全量 Follow IDs 在线拉取。

M2-R2 迁移/回滚/风险：领域 Port 追加，当前生产 Repository 即已装配；无需 Proto/HTTP 字段变更、DDL、缓存清理或真实服务写入。保留旧 Inbox 写扩散 key，只追加 `initialized:v2/rebuild:v2` 短期控制键，旧版不消费该标记；回滚仅应用版本，保留数据。权威候选每页走 MySQL，Redis 当前仅为派生投影，不能宣称新增缓存命中优化或生产容量达标。Read-only follows 子查询延续当前共享数据库读取边界，未来 M7 数据所有权拆分需替换 Port 的授权读模型；真实索引/执行计划/多副本租约与高关注用户压测在 M2-R3/部署态验收。关系和正文检查为读取时事实，不是跨请求冻结快照。

M2-R1 实施方案：普通/大 V 来源各读取 101 条，合并按精确 ID 倒序去重；每批消费最多 100 条，当前请求最多扫描 1000 条，游标推进至实际消费候选，前瞻/尚未交付的可见候选留下一页。达到上限的全隐藏页允许 `tweets=[] / has_more=true` 且游标严格推进。候选正文使用当前 MySQL 批量事实，排除删除、他人 Shadowban/Private；当前该 Feed 与已有 pull 查询统一仅返回他人 Public（自己的记录可见），Follows 记录的关系授权/支持随 M2-R2 批量当前关注校验接入，不能用旧 Inbox 证明粉丝权限。拉模式仓储内部上限 101，不改变公开最大 100。Redis 读取保留既有 key/写扩散格式，以原子有界脚本展开 cursor/末端同分数桶、按精确整数排序和排他比较，避免 float64 Snowflake 分数碰撞漏项；桶超量/非法成员明确失败后拉取降级。Gateway 原样解析十进制 Cursor、拒绝非法/重复参数，HTTP next_cursor 改为十进制字符串以符合现有客户端类型；Proto 保持 uint64/字段编号不变。缓存源任何失败从原请求 Cursor 完整切换 pull，正文失败不冒充空页，不启动新增分页预热 goroutine。

M2-R1 迁移与回滚：先升级 TweetService，再升级 Gateway；不执行 DDL、不清 Redis、不改既有分发 key。HTTP 字符串游标修复旧 JSON Number 精度丢失，Web/Mobile 已透传字符串；已被旧版本舍入的在途游标需重拉首页。回滚只回退应用版本，会恢复原漏页/精度缺陷，应优先保留读取修复。风险：当前正文每页增加有界批量 MySQL 读取，同分数展开每桶最多 4096 个 ID、超过上限 fail-closed；1000 是消费候选/正文扫描上限，不是跨大 V 来源总读量上限，源扇出仍随关联数量增长。源码测试不代表容量验收。已有大 V 重建请求 1000 条但旧作者仓储截为 100，后台 singleflight/深历史恢复、普通 Inbox 空缓存和旧 Follow IDs-only 截断继续归 M2-R2；此轮不宣称候选来源全量完整。

范围：TweetService/cache、Feed Gateway、Web/Mobile 列表。保持 `next_cursor/has_more` 字段；Cursor 绑定已扫描候选边界，不依赖最后一个可见结果。来源有界读取，影子/删除/取关过滤后补齐，并设置扫描上限以控制全隐藏页成本。

迁移/回滚：缓存初始化采用新版本 key；允许按用户惰性重建，不清 Redis 全库。旧客户端继续使用数字字符串 Cursor；回退读实现不删除新缓存状态。灰度记录空页率、分页候选量、重建延迟、DB 降级量。

验收：30+ 条纯普通作者跨页无漏重；大 V/混合、全隐藏、空列表、缓存丢失、Redis 失败、并发新推文；两客户端加载/错误/结束行为一致。容量结果需记录数据集与负载，未实测不声明吞吐目标完成。

## M3 Follow 关系与派生任务

M3-R2 实施（2026-10-13）：新 Mutation Port 要求已有 UoW，通过持久 pair 协调行锁串行同一有向关注对，在锁内至多读取两条有效关系，历史重复明确失败。Follow新建关系，Unfollow按锁定关系ID/双方身份/有效谓词软删除，最多3秒事务同时登记 Outbox；关系代次+动作唯一键冲突回滚，重复当前状态保留 ErrAlreadyFollowing/ErrNotFollowing、不产生新事件，重新关注产生新关系/新事件键。旧事件JSON两身份字段保留，追加v1/关系代次/发生时间/W3C Trace Context；生产入口移除直接发布，配置UoW/Outbox/Schema门禁、持久follow.relations.v1拓扑和Relay路由。当前队列尚无Consumer，提交后原进程内缓存任务暂留；R2不宣称恢复闭环。

M3-R2 迁移/回滚/风险：手工加法SQL新增follow_pair_locks和可选active pair索引，本轮未执行；先停旧Writer/审查重复关系、引擎/主键/唯一键/账号权限，再部署路由/队列和全部新Follow副本。锁只覆盖遵守新路径的Writer，不允许旧新混合；保留协调行/事件/队列/位点，回退旧应用会恢复事件缺口。暂存队列增长、COMMIT不确定、旧无事件历史及缓存任务丢失继续待验收；最多100条旧清理/大V传播和唯一持久owner归M3-R3，详细流程见 FOLLOW_RELATION_OUTBOX_RUNBOOK.md。

M3-R1 已实现方案：公开 Service 复用已存在的 FollowerPageRepository，补 FolloweePageRepository；查询按关系 `id DESC / id < cursor` 取 limit+1，返回最后一条已交付关系 ID，而用户 ID 数组只用于显示用户。公开页大小仍默认 20、最大 100，后台分页上限与旧 Repository 方法保持独立。Gateway 严格解析 ID/Cursor/Limit，非法输入返回 400；空数组固定为 []。不改 Proto 字段、不做数据迁移；客户端继续原样回传字符串 Cursor，旧的在途分页需从 0 重启。回滚仅回退应用版本，既有关系数据不变；回退旧版本会恢复原分页缺陷，应优先保留本修复。本轮不迁移大 V 广播/Outbox/后台 owner。

范围：领域 Page Port、FollowRepository/Service/Proto/Gateway、Outbox/Consumer、Celebrity 缓存。公开 Cursor 实际绑定关系 ID；内部 Page 带 next_cursor/has_more，并保留前瞻行。

迁移：先补 Page 接口与兼容适配；关注关系事务内固化事件，再迁移唯一派生执行 owner。现有关系无需重写，旧缓存由有界对账修复。唯一约束、重新关注和软删除需要先确认现存重复数据，迁移不得直接丢数据。

回滚：新旧消费者只选一个执行；事件保持向后兼容。部署回退保留 Outbox 事实和游标；重复重放不得产生重复关系/计数。

验收：用户 ID 与关系 ID 不同序、limit=100；5000+ 粉丝晋升全覆盖；任务中断恢复、重复事件、取关后旧内容清理、Redis/MQ 失败。

## M4 私信历史与投递

M4-R1 当前实施：历史消息按 `id DESC / id < cursor` 取 limit+1，返回消息 ID 字符串游标。会话列表以每个会话当前范围内最大消息 ID 排序，版本化 `c1:user_id:snapshot_id:before_id` 游标绑定调用者和首次最大消息 ID，上界之后的新消息不改变同一分页链的位置；它是 ID 范围上界，不是跨请求数据库事务快照。会话分组先应用 snapshot，再对 MAX(id) 应用 before，避免先裁切消息再分组使会话重复。最新消息一次 Join 读取，未读数对交付会话批量查询；公共默认 20/最大 100。Proto/HTTP 字段不变，不新增表/清理历史数据。兼容空字符串和 "0" 为首页，其他非法或跨用户会话游标明确失败；旧在途分页重拉首页。为避免分页读取把尚未获取的新消息一并标记已读，本轮同时把旧异步全会话标记收紧为当前交付页接收者消息 ID 的同步幂等更新，失败返回明确错误，不标记前瞻行。这是已读边界从 M4-R2 前移的一项必要修正；客户端的分页控件/去重/重连继续留 M4-R2。

范围：Messenger Repository/Service、Gateway、Web/Mobile chat。消息列表以事实源稳定 Cursor 分页；会话列表单独定义最新消息排序及稳定并列键，不能混用两类 Cursor。

迁移/回滚：沿用现有响应字段，游标为空表示首页；非法游标返回明确错误。先升级 Messenger，再升级 Gateway，旧在途会话页从首页重拉。`deploy/mysql/migrations/20261012_messenger_pagination_indexes.sql` 仅提供显式迁移，执行前检查同名/等价索引、MySQL 在线 DDL 能力、锁等待和部署窗口，本轮未执行；回退应用时保留兼容索引，不删除历史消息。旧版本会恢复分页/全会话已读缺陷，应优先保留本修复。

风险边界：ID 上界不冻结已读状态，也不保证覆盖分页途中才提交的低 ID 消息；刷新首页获取最新事实。未读数仅统计上界内消息。单表分组消除了逐会话 N+1，但仍扫描调用者的历史范围，不能宣称百万级用户容量已验收；大历史用户应在后续引入事务维护的独立会话投影并核验查询计划。

验收：多页消息、同毫秒、会话超过一页、并发新消息、非法 Cursor、加载去重和重连补拉。Redis Pub/Sub 继续只作为实时信号，不能宣称持久投递。

## M5 删除与索引生命周期

M5-R1 实施方案（2026-10-13）：`TweetDeletionRepository.GetForDeletion` 在调用者 UoW 中仅选择 id/user_id/deleted_at，锁定有效行 `FOR UPDATE`，锁内校验作者；原软删除方法改为 `ExtractTx`，随后 `OutboxEventRepository.Create` 插入兼容的 TWEET_DELETED。单事务最多 3 秒，遵从更短的请求 Deadline；依赖/读取/删除/Outbox/提交失败向上传播，查询错误不再全部伪装成不存在。事件 Payload 保持旧 tweet_id/author_id，事件身份为 Outbox 主键与唯一 DedupKey `tweet-deleted:<tweet_id>`，CreatedAt 是事件入队毫秒时间；不追加正文或新 MQ Schema 字段。对已删/缺失记录维持 Service NotFound，不补造事件；并发删除只有有效状态转移写事件。唯一键冲突明确失败并回滚，而不是忽略冲突后提交删除。

M5-R1 迁移/回滚/风险：当前生产 TweetRepository 已实现追加 Port，构造参数/Proto/HTTP 字段不变，无新表或新列；部署前必须确认已有 outbox_events/dedup_key 唯一索引和事务表引擎，不能由离线模型推断部署 Schema 已就绪。先核验现有 Canal TWEET_DELETED 路由和 Consumer，再升级所有 DeleteTweet 写实例；灰度旧实例仍使用尽力发布，不能宣称混合版本具备全量原子保证。保留 Outbox 和软删除数据，优先保留本修复；回退旧应用会恢复提交后发布缺口，不清表、不清索引。数据库 COMMIT 回执丢失可能已提交，错误返回不代表已回滚；事务只保证删除与事件共同提交/共同回滚，客户端须读回当前状态。事件入库不等于已投递，Relay 健康、积压、Binlog 保留/位点和回放待独立验收。

M5-R1 缓存与观测：仅在提交成功后同步失效本地 L1，最多 800 ms 尝试 Redis 删除/广播，使用 WithoutCancel 保留 Trace、隔离提交后的调用方取消；失败记录固定低敏日志，不把已提交事实改报回滚。`InvalidateBaseTweet` 保留广播尝试并返回 DEL/PUBLISH 任一错误。请求 Span 标记提交/失败阶段，不把 Tweet/User ID 用作指标标签；未增加异步 Trace 传递或专用积压指标，Relay/投影观测仍须后续完善。

范围：Tweet UoW/Outbox、Canal、Consumer task lease、ES/Qdrant Adapter。业务删除提交与事件原子化；投影版本/墓碑或当前事实重验阻断旧创建复活。文本、向量任务失败互不阻塞。

迁移：兼容旧删除事件；新的稳定事件/投影身份逐步引入。索引全量重建需独立受控作业和限速、暂停、报告，禁止请求内重建。物理数据清理不在未授权环境执行。

回滚：保留墓碑和 Outbox；回退 worker 不能绕过版本检查。新任务类型先注册再发事件；未识别任务保持可恢复状态。

验收：MQ 失败仍可由 Outbox 恢复；创建/删除乱序、ES 成功/Qdrant 失败、回执丢失、租约过期和重启；最终当前事实与两索引一致。真实外部故障验收单列。

M5-R2 实施（2026-10-13）：删除消费在 Timeline 清理/成功 ACK 之前分别登记唯一的 `delete_es` / `delete_vector` 持久 OutboxTask。部分登记失败走既有 Confirm-before-ACK Retry/DLQ；重放以 DedupKey 补齐缺项。两个任务独立领取/成功/退避/耗尽，沿用 10 条批次、90 秒租约、60 秒执行、5 次尝试和 Owner/Token/Lease 回执围栏；失败回执固定低敏字符串。耗尽状态保留，不自动删除或不断重入队。旧 `sync_es` 任务名称/载荷继续兼容，新 Worker 改用当前 MySQL 正文，不再信任事件中的旧正文。

M5-R2 防复活选择：采用当前事实重验与协作写入串行化，不新增持久墓碑或对外实体版本。`TweetProjectionRepository` 在同一 MySQL primary 专用连接上获取 `tweet:projection:<id>` 会话命名锁（等待 5 秒、释放 2 秒），创建/删除投影共用，网络调用期间不开 SQL 长事务。读取包含软删除，当前缺失/已删/非 Public 时只清理两索引；Public 使用当前正文，写入后再次检查删除/可见性与正文变更。缺依赖、读取、写入或释放失败不标成功；锁结果不确定或释放失败丢弃连接，取消后仍有界释放。Qdrant 清理固定 tweets Collection 的规范 UUID 与字符串 tweet_id 别名，`wait=true` 并检查 completed 回执；ES 仅承认匹配 ID 的 deleted/not_found 或明确缺索引。未知 404/坏回执均失败。

M5-R2 Relay：`OnRow` 遍历所有 INSERT 行，异常列/形状/类型返回错误不跳过；同一有界 FIFO 队列追加 `OnPosSynced` 的事务/DDL/Rotate 边界，队列前序发布收到 Broker Confirm 后才能保存该边界。移除读取解析器最新 SyncedPosition 的定时保存与关闭排空后的强制保存；取消/未知路由不给新位点，Worker 错误通知主程序退出。生产 Relay 用独立 Confirm Publisher、Persistent 消息与路由+Payload 摘要 MessageId；重放身份稳定，不包含正文日志。保存失败退避到成功或取消；Redis SavePosition 最多 5 秒。缺失/已清理位点默认拒绝启动，操作人完成历史核查后才可显式启用两个恢复开关。按年龄自动 Outbox GC 停用，发布感知保留与补偿对账归 M5-R3。

M5-R2 迁移/回滚/风险：没有新表/Proto/HTTP 字段，使用既有任务唯一键/租约列；上线前核验实际 Schema、同一 primary、连接池和命名锁能力。先停并排空全部旧 Consumer（含在途 sync_es 和旧异步 ES 删除），升级新 Worker 再恢复消费；随后升级 Relay，并检查绑定/位点。旧 Worker 会将未知任务判为终止，不允许混合版本领取新任务。回滚要停新消费/领取、保留任务和位点，不能让旧 Worker 消费新类型，也不能恢复不安全投影写入/GC。异常启动开关不是自动修复或验收证明，不能在 Compose 默认开启。详细步骤见 `INDEX_LIFECYCLE_RUNBOOK.md`。

M5-R2 保证边界：同一 primary 命名锁只串行化遵守该 Port 的 Writer，网络超时/进程崩溃后的远端请求可能在锁释放后才生效；没有服务器侧版本围栏，不能宣称任意不确定回执下绝不复活。任务重放与后续对账必须收敛此状态，公共正文继续受 M1 权威读取保护。Broker Confirm 不证明队列路由或消费，单 active Relay/正确绑定/持久位点备份仍是部署条件；没有自动 Outbox 历史补偿。历史无事件删除、已清理 Binlog、隐藏但无后续任务的旧索引、Timeline 旧 5000 粉丝上限/异步缓存清理仍留后续治理。Embedding 与 BM25 创建任务尚耦合，拆分归 M5-R3。

## M6 互动热度一致性

范围：互动事件 Consumer、Redis Lua、Retry/DLQ、Hashtag Batch/Decay。频控、事件幂等和得分提交应是可恢复的原子边界；有界 worker，成功后 ACK。多实例衰减只允许一个时间周期提交一次。

迁移/回滚：新 marker/版本化 topology 与旧队列排空分开；不得双执行。保留任务与去重 marker 至安全重放窗口结束，回退不能清除历史幂等状态。

验收：重复互动只计一次；频控 TTL 原子写入；Redis 失败/进程崩溃可重试；多副本无重复衰减，队列最老消息年龄与 DLQ 指标可查。

## M7 服务与运行隔离

M7-R2实施（2026-10-13）：Persona使用domain应用Port和SQL适配器，MemoryManager不持有GORM；Worker不读取Persona DB凭据/连接/Schema，也不装配认知引擎或初始化语义锚点。API/all共享开启必须完整Runtime v2模式，配置门禁在业务依赖前；Redis策略Ready另在装配前检查。admission.Shared位于Runtime外，实现同slot/服务器TIME/原子global-user-workflow与Provider租约，6—60秒、每1/3续租、失效取消、token条件释放；热路径缺策略/漂移失败，不自动切本地或重建。受保护Runtime/Router、Workflow LLM及Agent兼容/策略SDK先预留估算输入+输出上限与可选统一上界单价预算，分钟预留不退。默认离线管理CLI显式初始化匹配摘要，三Compose/角色凭据与健康门禁、指标告警已接线，详细子项/迁移/回滚见AGENT_SHARED_ADMISSION_RUNBOOK。

M7-R2边界：仍使用现有Mongo/Redis/检索/模型等依赖，Worker并非已完成全部对象最小化；摘要/Embedding/RAG/Reranker/Temporal直接pkg/ai及独立Eval不在此Provider预算，不能宣称全项目费用封顶。生产模板仍单agent-service，真实API/Worker多副本/发现/抓取需验收。Redis丢失/淘汰/时钟异常、请求回执不确定与远端不响应取消会破坏强容量/计费保证；初始化声明不是持久RBAC，估算不是真实Tokenizer/账单。启用/变更/回退先停并排空全部旧owner，保留Redis控制状态/SQL/Mongo证据，不以租约过期当远端已终止；关闭开关回到本地会失去跨副本上限。本轮无新SQL/Live/部署，整体运行隔离与真实验收仍Partial。

范围：组合根、Repository Port、部署账号、迁移流程、在线 Admission。先制定 owner 再改依赖，不把所有同步读取机械改成 MQ。共享准入须覆盖 API 多副本及 Provider 额度，并有租约/续租/释放/故障策略。

迁移/回滚：先观测和最小权限演练，再切账号/装配；分服务灰度，保留凭据与配置回退。Schema 只追加兼容变化，迁移 Job 与应用启动分离。

验收：一个业务存储故障不阻塞无关角色启动；多副本同用户和 Provider 并发不超过部署上限；崩溃租约可恢复；数据库账号不能越权访问非 owner 表。

## M8 发布与功能收口

### M8-R3：部署验收工具与证据门禁（2026-10-15）

用户已明确本轮仅完成工具与离线验证，整体Partial，不把真实staging/31项用户旅程标记Accepted。规划和实现沿用功能模块—轮次：

| 功能模块—轮次 | 交付/证据 | 状态 |
|---|---|---|
| M8-R3-A 发布身份 | production-acceptance-record v2绑定独立审核的发布清单SHA256/source、环境快照SHA256/部署ID/前版清单；12应用镜像/9检查及基础设施digest、配置/Secret/Schema公开版本引用；候选或身份漂移拒绝 | Implemented（离线验证通过） |
| M8-R3-B 现场观察 | 每个Passed要求单独live观察及脱敏原始证据、check/版本/环境/actor/时间绑定，24小时新鲜度；有界JSON/重复键/路径reparse拒绝、签名核验、写锁、独占初始化 | Implemented（离线验证通过） |
| M8-R3-C 用户旅程 | 26→31项，补认证/搜索可见性/Feed私信/Schema owner/共享准入；留存检查明确数据删除继续关闭；完整操作/恢复/回退与现场待办 | 工具/目录Implemented；真实旅程Not run |
| M8-R3-D 兼容与CI | v1按原计划可读但不通过新release-ready；CI追加v2夹具。两夹具独立GUID目录且清理路径使用平台分隔符；当前结论不自动发布或部署 | Implemented（离线验证通过）；真实Actions未验收 |

最终验证：四脚本PowerShell AST通过；`test-deployment-evidence.ps1`为31项/24负例，`test-production-acceptance-record.ps1`为31项/v1不可release-ready；`go test ./internal/releaseplan ./cmd/release-manifest -count=1`、同范围Vet/Race通过。每命令显式repo/tmp/go-build-cache后`go run ./cmd/release-manifest --matrix`输出12服务通过；既有Go测试解析workflow YAML，production-config新夹具接线读回通过。签名仅排除根signature，嵌套注入被拒；CLI缓存权限已Resolved，见ISSUES246。没有HTTP/Proto或客户端行为变更，不重复Web/Mobile工具链。本机合成结果不是CI或现场Accepted。

架构/迁移/回退：复用既有HMAC验收记录，不引入新服务/数据库；新增v2与纯本地身份/观察校验，旧v1记录不自动重签/搬迁，旧计划摘要仍需原文件。新计划改变摘要，旧Passed不继承；新记录单独初始化，保留旧版证据。应用回退需要前版发布清单、备份读回及停止/排空不能混跑的owner，保留表、事件、位点和预算，不删除卷或恢复运行DDL。

信任与未验收：期望摘要必须独立审核；HMAC持有者可伪造现场陈述，工具只校验材料一致/新鲜/签名，不证明实际镜像正在运行、权限足够或日志内容真实。release_ready仅为证据门禁，deployment_accepted永远false；真实CI/Registry/环境配置、clean-host/恢复/告警/Provider/MCP/跨端旅程仍未Accepted。M4-R2仍Planned、M2-R3容量仍Partial，JWT撤销/数据删除继续关闭。详见DEPLOYMENT_ACCEPTANCE_RUNBOOK及DEPLOYMENT_ACCEPTANCE_2026-10-15；下一代码轮次可补M4-R2，现场验收独立推进。

M8-R2实施（2026-10-14）：已先定义本地退出、服务端撤销及密码修改语义，再补 Auth Logout 既有 RPC 实现与 Gateway `/auth/session`、`/auth/logout`、`/auth/password`。Logout 只回执 local，`all` 明确501；密码修改只信任 JWT Context 用户ID，4KiB请求/5秒下游、6–72 UTF-8字节、旧密码403与依赖503/504，保留bcrypt写入。Web/Mobile均补账号安全入口与会话代次，迟到登录/刷新/旧401不覆盖新会话；Web成对保存、损坏缓存清理/跨标签重载、身份变更重建页面与客服iframe/SSE隔离；Mobile串行持久化（已排队退出不被新登录跳过）、认证监听Router、通知换代及Socket Timer/旧回调关闭，并移除认证/Socket敏感日志。差异账本见AUTH_SESSION_CONTRACT；没有新增Proto/Schema/存储，也没有服务端撤销或全设备退出，既有JWT到期前有效，用户删除继续关闭。目标Go Test/Vet/Race、Web build/13测试与Flutter分析/测试见下方记录；真实跨设备/生产旅程未Accepted，下一M8-R3。

### 交付记录：M8-R2（2026-10-14）

| 功能模块—轮次 | 状态/证据 |
|---|---|
| M8-R2-A 认证契约 | Implemented；AUTH_SESSION_CONTRACT/API_REFERENCE，现有Proto字段保持；可信身份/参数/403/501/超时、bcrypt新旧密码测试 |
| M8-R2-B Web | Implemented；账号安全/密码/退出语义、会话代次与跨标签/残留持久化清理，Axios/SSE401隔离、Socket与客服页面换身份重建；13项状态测试和完整构建 |
| M8-R2-C Mobile | Implemented；账号安全入口、认证监听路由、串行清理与竞争隔离、通知换代、Socket取消重连/旧回调及去敏感日志；真实路由/表单与Notifier/Interceptor夹具 |
| M8-R2-D 产品差异 | Implemented（账本）；收藏列表/FollowList/DSL与治理端差异明确；全设备撤销、找回密码仍Planned，删除执行继续关闭 |
| 验证 | 最终`go test ./... -count=1`全仓通过；Auth/User/Gateway及domain相关`go vet`、Auth/User/Gateway `go test -race`通过；Web `npm.cmd test`13/13、`npm.cmd run build`通过。Flutter analyze最后4条大括号info修复后零问题，完整test25项通过，含Socket Timer取消/处置后不建连与旧网关非JSON错误。补充User领域not-found错误与数据库故障分离，新增夹具证明故障不伪报404或泄漏原始错误。 |
| 问题 | ISSUES245：首次Gateway夹具回环沙箱拒绝，正常提权复验通过；4项Dart风格提示修复，不禁用Lint。 |
| 未验收 | 真实JWT过期/浏览器多标签与跨设备/网络断开/Socket服务/密码MySQL并发与不确定回执；没有Provider、真实服务启动、DDL、Registry发布或部署。现有JWT自然过期前有效，无撤销保证。 |

迁移/回退：新HTTP路径是加法，旧本地Logout可继续使用；新客户端退出不等待HTTP回执，没有离线令牌重试队列。密码修改仅沿既有User所有权写入，无Schema/DB新增。回退不恢复敏感日志或依赖清理失败的旧行为。密码并发写尚无版本/CAS；完整会话撤销须另轮统一覆盖全部鉴权入口与存量Token，不能只加Gateway黑名单。生产接受仍false，按M8-R3部署证据门禁推进。

M8-R1实施（2026-10-13）：目录驱动12镜像（全部Compose build+独立Schema Job），Makefile/Go Dockerfile按包构建；CI完成Go Test/Vet/包级Linux/指定Race、Web锁安装与构建/状态测试、固定Flutter/锁/analyze/test、实际config-only生产夹具。默认仅候选OCI，main显式dispatch publish才读取Registry凭据上传、基础镜像须非零digest，不自动修改Git/Helm或部署。release-manifest默认离线目录，回执/组装/复验需真实干净Git版本，精确12镜像/9检查与模板/锁/SQL/Schema摘要、候选/发布区分、拒绝坏状态与伪部署Accepted，输出不覆盖。完整Go全仓/CI同范围Race/全仓Vet、Windows/Linux包级构建、Web完整build/七状态、Flutter analyze零问题/test16项、Compose九项/26验收记录夹具通过。Mobile门禁修复涵盖当前Color API、死导入、相关页面await后有效性/Workflow对话框Context和真实应用/迟到响应测试；本机代码验证Implemented，真实CI/镜像/Registry/权限/部署未Accepted。下一M8-R2，见RELEASE_PIPELINE_RUNBOOK。

范围：Makefile/CI/Docker/Compose/Helm、认证契约和客户端。发布清单完整绑定源码、镜像 digest、配置、迁移及验收报告。Logout 先定义本地退出/会话撤销/密码变更的产品语义，再接服务端；不盲目引入全局 Token 黑名单。

验收/回滚：各服务包构建、Go 全仓/Vet/受影响 Race、Web/Mobile 检查；配置门禁、clean-host、备份恢复、告警投递；真实 Provider/Profile/MCP/审批恢复均绑定固定身份与授权。每次部署保留前版 digest、迁移兼容声明和回退步骤。用户数据删除仍维持关闭，直到全写路径/围栏/故障验收满足原上线计划。

## 交付记录：M8-R1（2026-10-13）

| 字段 | 本轮事实 |
|---|---|
| 模块/轮次 | M8发布与功能收口—R1完整发布流水线；F10；A包级构建/B检查/C镜像矩阵/D发布证据 |
| 状态 | Implemented（代码与本机离线验证）；2026-10-13正常审批恢复后完成完整Go/Race/Web/Mobile，真实CI/镜像未Accepted |
| 代码 | .github/workflows/ci-cd.yaml、Makefile、Go服务/Schema与Web Dockerfile/.dockerignore、deploy/release/catalog.json、internal/releaseplan/与cmd/release-manifest、生产Compose预检/9项夹具、Web npm test入口；Mobile分析发现的API/导入/页面有效性与Workflow对话框Context、真实启动烟测及迟到回调测试 |
| 行为 | 全cmd按包编译；12组件/9门禁后才完整清单；默认候选OCI/仅显式main发布、基础digest门禁/源码SHA tag、移除自动Helm/Git写。CLI离线目录/精确回执/干净Git HEAD/配置迁移摘要/只读Verify，Published不代表部署Accepted |
| 已验证 | 目标发布包/CLI Test/Race与Vet、完整Go全仓/CI同范围工程Race、全仓Vet、全部cmd Windows/Linux CGO0编译、Web完整build/7状态、Flutter analyze零问题/test16项、Compose9项config-only、26验收记录自测/PS Parser/目录YAML。Mobile失败已修复复验，未降低Lint |
| 待验证 | 真实Actions/clean-host锁安装/12镜像/Registry拉取发布/生产Secret配置权限与部署旅程；本机离线验证不代表部署Accepted |
| 兼容与回滚 | 无API/Proto/DDL；Schema Job默认离线，不执行迁移；保留历史Helm/tag导出，不宣称已收口。精确前版digest/配置/迁移兼容审查，停/排空需唯一owner的旧实例，保留SQL/Outbox/位点/预算/运行证据 |
| 风险 | 回执无签名/真实Registry复验，可信CI需另确认；candidate digest可能无法拉取，手填Passed/digest无效；真实Secret/基础设施digest/部署配置身份沿既有验收流程，基础tag/Actions/apk源不证明完全可复现 |

实际通过命令（GOCACHE为仓库tmp/go-build-cache；恢复验证需回环/工具链权限的命令经过正常审批，仅Fake/已有SDK，不访问真实业务依赖）：

```powershell
go test ./internal/releaseplan ./cmd/release-manifest -count=1
go test ./... -run '^$'
go test ./... -count=1
go vet ./...
go build -trimpath -o .codex-tmp/m8r1-build/ ./cmd/...
# 本命令执行时进程临时CGO_ENABLED=0/GOOS=linux/GOARCH=amd64，完成后恢复。
go build -trimpath -o .codex-tmp/m8r1-linux/ ./cmd/...
go test -race ./internal/releaseplan ./cmd/release-manifest -count=1
go test -race ./internal/infrastructure/... ./internal/module/... ./internal/mq/... ./internal/gateway/... ./internal/releaseplan ./cmd/release-manifest -count=1
./scripts/test-production-compose.ps1
./scripts/test-production-acceptance-record.ps1
# web目录
npm run build
npm test
# mobile目录，已有依赖
flutter analyze --no-pub
flutter test --no-pub
```

完整go test基线失败于旧tmp生成文件，忽略tmp/go.mod隔离后完整执行通过；发布工具Windows路径/缺回执夹具、Compose进程空环境和Node22 TS入口问题已修复。历史Vite realpath EPERM/Flutter审批额度耗尽在本次正常审批恢复后复验通过。Flutter暴露39项warning/info和旧Widget计数器/未初始化Storage问题：采用当前Color API、相关await后mounted/正确Context、死导入清理及Mock Storage/ProviderScope的真实登录/表单/注册零API烟测与卸载后迟到成功/失败测试修复，analyze零问题/test16项通过，不禁用Lint。准确分类/复验见ISSUES244；没有本机依赖安装、镜像构建、Registry写、业务服务启动/DDL/部署。下一代码轮次M8-R2，真实部署状态与操作见 `RELEASE_PIPELINE_RUNBOOK.md`。

## 每轮交付记录模板

| 字段 | 必填内容 |
|---|---|
| 模块/轮次 | Mx-Ry、对应发现、日期 |
| 代码范围 | 本轮文件、生产装配与用户行为 |
| 状态 | Planned / In Progress / Implemented / Accepted |
| 验证 | 实际命令与结果，Fake/Live 区别 |
| 兼容/回滚 | 契约、数据、开关和旧版本行为 |
| 剩余风险 | 未完成任务、外部验收和依赖 |

## 交付记录：M1-R1/R2/R3（2026-10-12）

| 轮次 | 交付与验证 | 状态/剩余风险 |
|---|---|---|
| M1-R1 | 新增 `GetPublicTweets` Proto/生成代码、Tweet Service/grpc；一次 GetByIDs 当前事实读取，1—100 输入项、去重/排序、删除和非公共过滤；Service/Proto/gRPC 错误契约测试通过 | Implemented；新增内部只读 RPC 无 Gateway/客户端契约变更，无数据迁移 |
| M1-R2 | 新增 `agent/retrieval` 有界权威读取适配器；MCP 语义/混合搜索重排前校验；RAG 公共知识评分/上下文前校验；Agent 组合根注入；Fake HTTP/gRPC 验证旧正文替换与负向过滤 | Implemented；权威服务不可用时不返回旧正文；查询时快照，生成期间可见性变化不属于本轮原子性保证 |
| M1-R3 | 目标五包、Agent 全模块/Gateway/相关 cmd 扩大测试、Vet、MCP/RAG/Reader Race 和 diff check 通过；失败记录已更新 | Implemented（离线验证）；未执行真实 MySQL/ES/Qdrant/Provider/staging 旅程，不能标记 Accepted |

实际验证命令（工作区 GOCACHE，使用自动审查允许的沙箱外执行以支持既有回环测试夹具）：

```powershell
go test ./internal/module/tweet/service ./internal/module/tweet/grpc ./internal/module/agent/retrieval ./internal/module/agent/mcp/tools ./internal/module/agent/workflow/rag -count=1
go test ./internal/module/agent/... ./internal/gateway/... ./cmd/agent-service ./cmd/tweet-service ./cmd/consumer -count=1
go vet ./internal/module/agent/... ./internal/gateway/... ./cmd/agent-service ./cmd/tweet-service ./cmd/consumer
go test -race ./internal/module/agent/mcp/tools ./internal/module/agent/workflow/rag ./internal/module/agent/retrieval -count=1
```

部署顺序：先 TweetService、后 Agent；旧 TweetService 的 Unimplemented 导致安全失败。回滚不恢复索引正文兜底。历史作者/详情读取工具、其他列表可见性及索引物理清理尚未在本轮全量收口，留在 M2/M5 验收清单。下一代码轮次按总表执行 M3-R1（Follow Cursor 契约）和 M4-R1（私信历史分页），随后进入 M2。

## 交付记录：M3-R1（2026-10-12）

| 字段 | 本轮事实 |
|---|---|
| 模块/轮次 | M3 Follow 关系与派生任务 — R1 Cursor 契约；对应 F5 |
| 状态 | Implemented（离线验证）；真实 MySQL/部署读回未验收 |
| 代码范围 | domain FolloweePage Port；Follow Repository 页查询/组装；Service 两类公开列表；gRPC 输入/失败码；Gateway 参数解析、大整数字符串和空数组 |
| 用户行为 | 粉丝/关注列表按关系 ID 连续分页；用户 ID 与关系 ID 不同序不再跳漏；100 条页大小仍能发现第 101 条；非法游标不再静默重置首页 |
| 兼容 | 既有 Proto 字段编号/类型与 HTTP 字段保持不变，无 Schema/数据迁移；Web 原样回传字符串 Cursor，Mobile 当前无分页列表入口；旧版在途分页从 0 重启 |
| 验证 | SQL DryRun 检查作用域/软删除/排除式关系 Cursor/limit+1；Fake Page/直接 gRPC/HTTP 联合回归覆盖不同序 ID、空页、100/101 条、并发新关系、取消、缺失 Port、坏页与非法参数；目标/扩大回归、Vet、Race、Web 构建、gofmt/diff check 通过 |
| 回滚 | 仅应用版本回退、不删除数据；回退旧版会恢复原缺陷，应保留修复；未启用新产品开关或修改部署 |
| 剩余范围 | 旧 IDs-only 仓储调用仍在 Feed 降级、大 V 广播/对账；M3-R2/R3 负责事件和持久全量传播，M2 负责 Feed；真实查询执行计划、部署和跨端用户旅程仍需验收 |

实际通过命令（GOCACHE 为工作区 tmp/go-build-cache；含旧回环夹具的命令在自动审查允许的执行环境下复验）：

```powershell
go test ./internal/module/follow/... ./internal/gateway/handler -count=1
go test ./internal/module/tweet/... ./internal/mq/... ./internal/gateway/... ./cmd/follow-service ./cmd/tweet-service ./cmd/consumer ./cmd/gateway -count=1
go vet ./internal/module/follow/... ./internal/module/tweet/... ./internal/mq/... ./internal/gateway/... ./cmd/follow-service ./cmd/tweet-service ./cmd/consumer ./cmd/gateway
go test -race ./internal/module/follow/... ./internal/gateway/handler -count=1
# 工作目录 web
npm run build
```

本轮未连接真实数据库、MQ 或 Provider，未发布镜像/更改容器。下一轮为 **M4-R1 私信历史分页**，之后进入 M2 Feed 完整性。

## 交付记录：M4-R1（2026-10-12）

| 字段 | 本轮事实 |
|---|---|
| 模块/轮次 | M4 私信历史与投递 — R1 历史分页；对应 F6 |
| 状态 | Implemented（离线验证）；真实数据库/部署未 Accepted |
| 代码范围 | domain 分页/页内已读 Port；MySQL 消息/会话页与批量未读查询；Messenger Service；Gateway 参数/错误转换与批量公开资料；Mobile 数据层契约测试；独立索引迁移 SQL |
| 用户行为 | 消息按 ID 连续翻页；会话固定首次 ID 上界并按分组后的最大 ID 排他推进；默认 20/最大 100，使用 limit+1 前瞻；空页数组固定 []，结束游标为空；非法游标明确失败 |
| 已读边界 | 只同步更新本页交付、属于当前接收者的消息 ID，重试幂等；前瞻/未获取历史/新消息不被整会话误标。网络断开仍可能已提交已读，客户端展示确认协议留 M4-R2 |
| 兼容 | Proto 编号、HTTP 字段/路由保持不变；Web/Mobile 当前数据层原样回传字符串游标；版本化会话游标升级后旧在途页重新从首页获取；先 Messenger 后 Gateway |
| 已通过 | Messenger 全模块测试/Race、Gateway 新增分页测试、相关 Vet、完整 Messenger/Gateway/两个入口回归、Gateway Handler 全包 Race、Web 构建、Dart 格式化、Mobile 3 项定向契约测试/Analyze、gofmt 与 diff check；Fake/SQL DryRun 覆盖同毫秒/大 ID/100+1/并发新增/身份/坏页/取消/失败 |
| 首轮失败与复验 | 普通沙箱旧回环 socket/Vite realpath 受限，Dart 普通启动停滞后中止、提权自动审批额度耗尽；用户要求重新提交后，请求获准并完成同源码完整复验。ISSUES 232 已 Resolved（仅本轮执行路径），普通沙箱限制仍存在 |
| 迁移/回滚 | 新索引仅提供显式 SQL，未运行；预检 SHOW INDEX 和在线 DDL/锁窗口；应用回退保留兼容索引，不删除历史消息；旧版恢复原分页/整会话已读缺陷，优先保留修复 |
| 剩余风险 | 单表分组扫描调用者历史，不代表容量验证；ID 上界不冻结已读状态或低 ID 延迟提交。真实 MySQL 执行计划/索引/部署与多端旅程待验收；UI 去重/重连与持久投递需求归 M4-R2 |

本轮实际通过命令（Go 回归/Race 使用工作区 `tmp/go-build-cache`；含旧回环夹具及 Web/Mobile 工具命令在重新提交后获准执行）：

```powershell
go test ./internal/module/messenger/... -count=1
go test ./internal/gateway/handler -run 'TestMessenger' -count=1
go vet ./internal/module/messenger/... ./internal/gateway/... ./cmd/messenger-service ./cmd/gateway
go test ./internal/gateway/... ./cmd/messenger-service ./cmd/gateway -run '^$'
go test -race ./internal/module/messenger/... -count=1
go test ./internal/module/messenger/... ./internal/gateway/... ./cmd/messenger-service ./cmd/gateway -count=1
go test -race ./internal/gateway/handler -count=1
# 工作目录 web
npm run build
# 工作目录 mobile
dart format test/chat_pagination_contract_test.dart
flutter test --no-pub test/chat_pagination_contract_test.dart
flutter analyze --no-pub lib/features/chat/data/chat_repository.dart test/chat_pagination_contract_test.dart
```

完整离线门禁已通过，未连接真实数据库/MQ/Provider、发布镜像或应用索引迁移。真实 MySQL/部署旅程独立验收；下一轮按计划进入 M2-R1 Feed 稳定分页。

## 交付记录：M2-R1（2026-10-12）

| 字段 | 本轮事实 |
|---|---|
| 模块/轮次 | M2 Feed 完整性 — R1 Following Feed 稳定分页；对应 F2 |
| 状态 | Implemented（离线验证）；真实数据库/Redis/部署容量未 Accepted |
| 代码范围 | TweetService 候选扫描与旧混合缓存夹具；Timeline 精确 Redis 页读取；TweetRepository 内部 101 条；gRPC GetFeeds；Gateway 参数/错误/字符串 Cursor/批量资料；Mobile 数据层契约测试与未使用导入清理 |
| 结果 | 纯普通作者流不再第一页提前结束；混合来源按精确 ID 去重/排序；过滤后有界补齐，扫描预算用尽空页仍有推进游标；无前瞻结束时 Cursor=0；正文读取当前 MySQL，缓存旧正文不进入此 Feed |
| 契约 | Proto 方法/编号/uint64 不变；HTTP next_cursor 改为十进制字符串，与现有 Web/Mobile 字符串消费一致；非法/重复 Cursor/Limit 明确拒绝，失败低敏，结束/空数组 [] |
| 验证证据 | Cache 夹具覆盖 2^53 以上/不同十进制位数/uint64 上界/同分数/不存在游标；Service 覆盖 35 条连续多页、100/101、混合源重复/失败切换、当前正文替换、全隐藏 1000 扫描预算与后续可见记录、取消/坏页；SQL DryRun、HTTP/gRPC、Mobile 空页续拉/非法输入测试 |
| 已通过 | 扩大 Tweet/MQ/Gateway/三个服务入口测试、同范围 Vet；Tweet Service/Cache/Gateway Handler 全包 Race；Web 类型与生产构建；Dart 格式化、Mobile 两项 Test/定向 Analyze 无告警；本轮 gofmt/diff check |
| 首轮失败 | 普通沙箱回环受限；获准复验发现旧混合缓存测试缺新 GetByIDs 期望；适配后扩大回归通过。Mobile Analyze 报旧 unused_import，删除单行并复验。ISSUES 233 记录实际过程 |
| 部署/回滚 | 先 TweetService 后 Gateway；不修改 Redis key 或执行数据迁移；旧客户端已被舍入游标需重拉首页；回退应用保留数据，但恢复原分页与精度风险 |
| 剩余范围 | M2-R2 负责普通空缓存初始化/重建、当前关注批量重验/Follows 支持、完整关注/大 V 关联/深历史降级；M2-R3 负责跨端 UI 去重/空页加载/并发刷新与容量。其他 public/user/replies/media 列表不在本轮；实际 MySQL/Redis/staging/压测未 Accepted |

本轮扩大验证通过命令（工作区 `tmp/go-build-cache`；需要回环/客户端工具的命令获准沙箱外执行）：

```powershell
go test ./internal/module/tweet/... ./internal/mq/... ./internal/gateway/... ./cmd/tweet-service ./cmd/consumer ./cmd/gateway -count=1
go vet ./internal/module/tweet/... ./internal/mq/... ./internal/gateway/... ./cmd/tweet-service ./cmd/consumer ./cmd/gateway
go test -race ./internal/module/tweet/service ./internal/module/tweet/cache ./internal/gateway/handler -count=1
# 工作目录 web
npm run build
# 工作目录 mobile
dart format test/feed_pagination_contract_test.dart
flutter test --no-pub test/feed_pagination_contract_test.dart
flutter analyze --no-pub test/feed_pagination_contract_test.dart lib/features/tweet/data/tweet_repository.dart
```

离线门禁已通过，ISSUES 233 已关闭本轮阻断；普通沙箱回环权限限制未因此修复。本轮不发布镜像/变更容器/访问真实服务，下一轮 M2-R2。

## 交付记录：M2-R2（2026-10-12）

| 字段 | 本轮事实 |
|---|---|
| 模块/轮次 | M2 Feed 完整性 — R2 缓存重建与关系校验；对应 F3/F8 |
| 状态 | Implemented（离线验证）；真实数据库/Redis/部署与容量未 Accepted |
| 代码范围 | domain FollowingFeedRepository/FollowReadRepository；Tweet Repository 当前关系有界查询；Follow Repository 批量当前关系查询；Feed Service 生产读取/授权与恢复；TimelineCache v2 标记、租约与条件原子合并；目标夹具/SQL DryRun |
| 用户行为 | 空缓存不再返回伪空 Feed；全部关注对象与大 V 深历史不受旧缓存截断；旧关联缺项/取关残留不授予读取权限；当前粉丝可读 Follows，自身记录独立可见；当前关系/数据库失败明确失败 |
| 恢复边界 | 最新 1000 条缓存投影、3 秒共享租约/Token 围栏、2 秒查询、5 分钟初始化标记/7 天 Inbox；空/缺失分开，条件合并保留新 Fanout；失败不写成功标记，旧 owner 不能提交或释放新 lease。当前响应始终由权威页决定 |
| 方案偏差 | 使用当前共享 DB 只读关系子查询恢复所有有效关联；不在在线请求全量重建旧大 V Redis 集合，不调用旧 GetFollowees/ListByUserID 截断路径。旧后台集合修复/晋升传播归 M3-R3；权威每页读增加数据库成本，不能宣称新增缓存命中提升 |
| 兼容/回滚 | Proto/HTTP/客户端字段无变更，生产 Repository 可选 Port 已装配；无需 DDL，不改/清旧 Inbox，只追加 TTL 控制键；回滚应用保留数据，但旧版恢复原缺项/权限风险，应保留修复 |
| 验证 | 目标五包、扩大 Tweet/Follow/MQ/Gateway/四个服务入口测试、同范围 Vet、Tweet Service/Cache/Follow Repository Race、gofmt/diff check 全部通过；已知回环夹具在获准环境执行，无当前失败 |
| 证据范围 | SQL DryRun 验证 EXISTS 作用域/软删除/可见性/ID/Limit；Fake/Redis 验证 205 作者/1205 深历史、失效关联、候选后取关、粉丝/自身权限、空/丢失/失败恢复、16 并发单 owner、Token 过期/释放/TTL/并发 Fanout；不等于真实 MySQL 集成或分布式压力测试 |
| 剩余验收 | 真实 MySQL 索引/执行计划、稀疏高关注结果查询负载、Redis lease 故障与多副本容量、跨端 UI/用户旅程待 M2-R3。共享 follows 只读范围与独立库替代 Port 归 M7；旧派生任务仍待 M3-R2/R3 |

实际通过命令（Go 使用工作区 `tmp/go-build-cache`；含回环夹具的测试获准沙箱外执行）：

```powershell
go test ./internal/module/tweet/service ./internal/module/tweet/cache ./internal/module/tweet/repository ./internal/module/follow/repository ./internal/module/tweet/grpc -count=1
go test ./internal/module/tweet/... ./internal/module/follow/... ./internal/mq/... ./internal/gateway/... ./cmd/tweet-service ./cmd/follow-service ./cmd/consumer ./cmd/gateway -count=1
go vet ./internal/module/tweet/... ./internal/module/follow/... ./internal/mq/... ./internal/gateway/... ./cmd/tweet-service ./cmd/follow-service ./cmd/consumer ./cmd/gateway
go test -race ./internal/module/tweet/service ./internal/module/tweet/cache ./internal/module/follow/repository -count=1
```

本轮 HTTP/Proto/生产客户端没有变化，未重复 Web/Mobile 工具链检查；也未连接真实数据库/Redis、变更容器、执行 DDL 或数据清理。下一轮按总表进入 **M2-R3 跨端与容量验收**。

## 交付记录：M2-R3（2026-10-12）

| 字段 | 本轮事实 |
|---|---|
| 模块/轮次 | M2 Feed 完整性 — R3 跨端与容量验收；对应 F2/F3 |
| 状态 | Partial：跨端实现、离线门禁与真实 MySQL 小数据集 SQL 通过；完整容量/部署待验收 |
| 客户端 | Web FeedPager 核心接入 Home，Mobile FeedNotifier/HomeScreen；字符串 ID 去重、同代加载互斥、刷新/页签/账号换代隔离旧响应/旧 finally、失败保留已有列表与游标、至多 3 个无新增页自动续拉、继续/重试/结束状态；Mobile 互动迟到写回/延迟刷新也检查 generation/mounted |
| 契约补齐 | 首页推荐页签旧 ListTweets HTTP Cursor 仍为 Number，统一转换十进制字符串并补大整数 Handler 回归；Proto/服务分页算法不变。两端不把空页当结束，也不自行推导下一 ID |
| 验收工具 | 显式 `--allow-live` 的 `cmd/feed-read-acceptance`，只读事务复用生产候选/关系 SQL及 EXPLAIN；60 秒/最多 100 样本；无种子/DDL/Redis写入；输出去掉 SQL 条件/用户ID/凭据的计划摘要。可显式发现最近有效关注的已有用户；无 fixture 失败 |
| 已通过 | Web 7 项原生 Node 状态测试/生产构建；Mobile 6 项状态+2 项 HomeScreen Widget+2 项数据契约测试/定向 Analyze/格式化；验收 cmd/Tweet/Gateway/相关入口回归、Vet、Tweet Service/cmd Race、预算测试/Benchmark、gofmt/diff check |
| 离线基线 | Go1.25.5 windows/amd64、i9-13900HX、20x：可见页 39420 ns/op、1 批、28936 B/149 alloc；全隐藏 248610 ns/op、10 批、242436 B/1236 alloc。仅 Fake Scanner/测试Port/日志，本机执行与分配预算，不是 SQL/生产 QPS/p95 |
| Live 事实 | 初次 .env 的 3306 事务失败；用户授权启动后复用已有卷启动项目 MySQL 容器，实际 3307 healthy。MySQL8.0.46/316 条推文/7 条有效关注，fixture 1 个关注对象/40 条候选；20 次首页和第二页各返回20条，候选SQL+关注检查 p95 5.910/5.997ms，计划使用 PRIMARY 和 idx_follower；终页空样本独立记录 |
| 证据/限制 | 完整结果见 `FEED_ACCEPTANCE_2026-10-12.md`，操作矩阵见 `FEED_ACCEPTANCE_RUNBOOK.md`；ISSUES 234 的依赖阻断已解除。小数据集同事务重复SQL不代表完整GetFeeds/QPS/容量；高关注/稀疏长历史、Redis多副本、部署浏览器/真机及端到端并发仍待验收 |
| 回滚 | Web/Mobile 回退版本不改服务存储或字符串 Cursor；只读命令无部署/业务写功能。本次仅启动现有MySQL，保留数据卷，未执行DDL或改写业务数据/.env；需要停止用 docker stop twitter-clone-mysql-1。代码阶段后续为 M5-R1，M2-R3 完整Live证据仍须独立收口 |

实际通过命令（回环/工具权限受限检查在获准环境执行）：

```powershell
# 工作目录 web
node --experimental-strip-types --test tests/feed_pager.test.mjs
npm run build
# 工作目录 mobile
dart format lib/features/tweet/presentation/feed_notifier.dart lib/features/tweet/presentation/home_screen.dart test/feed_notifier_test.dart
flutter test --no-pub test/feed_notifier_test.dart test/feed_pagination_contract_test.dart
flutter analyze --no-pub lib/features/tweet/presentation/feed_notifier.dart lib/features/tweet/presentation/home_screen.dart test/feed_notifier_test.dart
# 工作目录仓库根，Go GOCACHE=tmp/go-build-cache
go test ./cmd/feed-read-acceptance ./internal/module/tweet/... ./internal/gateway/... ./cmd/tweet-service ./cmd/gateway -count=1
go vet ./cmd/feed-read-acceptance ./internal/module/tweet/... ./internal/gateway/... ./cmd/tweet-service ./cmd/gateway
go test -race ./internal/module/tweet/service ./cmd/feed-read-acceptance -count=1
go test ./internal/module/tweet/service -run '^$' -bench BenchmarkFeedScannerBudget -benchtime=20x -benchmem -count=1
```

## 交付记录：M5-R1（2026-10-13）

| 字段 | 本轮事实 |
|---|---|
| 模块/轮次 | M5 删除与索引生命周期 — R1 删除 Outbox；对应 F4 |
| 状态 | Implemented（离线验证）；真实事务锁/COMMIT 不确定性、Relay/索引与部署未 Accepted |
| 代码 | 追加删除身份读取 Port/有效行锁；软删除提取 UoW；DeleteTweet 事务内鉴权/删除/兼容 Outbox，移除直接 MQ 发布；3 秒事务上限、稳定唯一键、固定阶段日志/Span；提交后 L1/有界 Redis 失效，DEL 错误不再被广播成功掩盖 |
| 新测试 | 真实 GORM UoW + 生产 Tweet/Outbox Repository 在内存 SQL 驱动中运行：提交同一事务、begin/read/delete/outbox/commit 错误、未授权/缺失/取消、重复与并发删除、事务 Port 与 ID 边界；实际 miniredis Hook 检查缓存仅在提交后执行、提交后取消不跳过、缓存故障不改变已提交结果，DEL/PUBLISH 分别失败均被观察 |
| 检查 | 目标 DeleteTweet 测试通过；扩大 Tweet/Canal/MQ 与 TweetService/Consumer/CanalRelay 入口测试、同范围 Vet 通过；Service/Repository/Cache Race 通过；本轮 gofmt/diff check 通过 |
| 边界 | 内存 SQL 驱动模拟串行事务与确定的提交失败，不代表真实 MySQL 锁竞争或 COMMIT 回执丢失；miniredis 是进程内 Redis 夹具。没有执行真实数据删除、DDL、真实 MQ/索引写入、部署或依赖安装。Web/Mobile/Proto/HTTP 字段未变，未重复客户端构建 |
| 后续 | M5-R2 持久 ES/Qdrant 删除、创建当前事实重验/版本防复活、Relay 发布水位/受控补偿和故障重放；M2-R3 完整容量仍 Partial。现有账号删除生产执行继续关闭 |

实际通过命令（扩大回环检查在获准执行环境；GOCACHE=tmp/go-build-cache）：

```powershell
go test ./internal/module/tweet/service -run '^TestDeleteTweet' -count=1
go test ./internal/module/tweet/... ./internal/infrastructure/canal/... ./internal/mq/... ./cmd/tweet-service ./cmd/consumer ./cmd/canal-relay -count=1
go vet ./internal/module/tweet/... ./internal/infrastructure/canal/... ./internal/mq/... ./cmd/tweet-service ./cmd/consumer ./cmd/canal-relay
go test -race ./internal/module/tweet/service ./internal/module/tweet/repository ./internal/module/tweet/cache -count=1
```

## 交付记录：M5-R2（2026-10-13）

| 字段 | 本轮事实 |
|---|---|
| 模块/轮次 | M5 删除与索引生命周期 — R2 版本化派生投影；采用计划允许的当前事实重验方案 |
| 状态 | Implemented（离线验证）；真实 MySQL/MQ/索引故障、部署与恢复仍未 Accepted |
| 代码 | Consumer 独立持久 ES/Vector 删除、旧 sync_es 当前事实读回/写后重验、同 primary 命名锁 Port 与生产装配；ES/Qdrant 删除回执校验；Canal 多行/FIFO提交位点/Confirm发布/取消不越界、缺失或清理位点启动门禁、停用年龄GC |
| 测试 | 内存 SQL 驱动验证命名锁连接/读软删除/锁结果不确定丢弃/取消释放；当前正文、缺失/隐藏/私密/Follows、乱序在途创建、部分登记补齐、ES成功/Vector失败独立重试与陈旧完成回执；httptest 验证大ID/限定删除范围/404/坏回执；Relay阻塞发布取消/多行/位点保存重试/未知路由/稳定MessageId；真实Handler成功与部分失败的ACK顺序 |
| 已通过 | 定向测试；扩大 Tweet/MQ/Canal/MQ基础设施/ES/Qdrant 与三个入口回归，同范围 Vet；Consumer/Canal/Repository/ES/Qdrant Race；本轮 gofmt/diff check。ACK补测另作定向 Race 复验 |
| 范围限制 | Fake/内存驱动/httptest/miniredis，未写真实数据、调用真实Embedding、删除真实索引、重启服务或执行DDL。MySQL会话锁不能为取消后迟到的远端写提供服务端版本围栏；没有真实容量/恢复验收 |
| 后续 | M5-R3 独立文本/向量创建任务、只读对账、受控修复/发布感知保留/积压告警；M2-R3 完整容量仍 Partial |

实际通过命令（回环夹具在获准环境，GOCACHE=tmp/go-build-cache）：

```powershell
go test ./internal/mq/consumer ./internal/infrastructure/canal ./internal/module/tweet/repository ./pkg/es ./pkg/qdrant ./cmd/canal-relay -run '^Test(SearchProjection|Projection|Deletion|DeleteTweet|Relay|CanalResumePolicy|ESDelete)' -count=1
go test ./internal/module/tweet/... ./internal/mq/... ./internal/infrastructure/canal/... ./internal/infrastructure/mq/... ./pkg/es ./pkg/qdrant ./cmd/tweet-service ./cmd/consumer ./cmd/canal-relay -count=1
go vet ./internal/module/tweet/... ./internal/mq/... ./internal/infrastructure/canal/... ./internal/infrastructure/mq/... ./pkg/es ./pkg/qdrant ./cmd/tweet-service ./cmd/consumer ./cmd/canal-relay
go test -race ./internal/mq/consumer ./internal/infrastructure/canal ./internal/module/tweet/repository ./pkg/es ./pkg/qdrant -count=1
go test -race ./internal/mq/consumer -run '^TestDeletionMessage' -count=1
```

## 交付记录：M5-R3（2026-10-13）

| 字段 | 本轮事实 |
|---|---|
| 模块/轮次 | M5 删除与索引生命周期 — R3 文本/向量拆分与对账；对应 F4 |
| 状态 | Implemented（离线验证）；真实依赖、迁移、恢复、召回与部署未 Accepted |
| 方案与实现 | 创建ACK前独立sync_text/sync_vector登记，旧sync_es只登记子任务；两lane各批次5条，沿用租约/围栏/独立退避/耗尽保留。Text当前事实写ES、不调用模型；Vector固定Model/Dimension/Version，Embedding锁外、写前/写后重验与精确回执。元数据摘要对账、审批计划绑定、每Tweet原子修复任务登记、跨Tweet部分结果；可选Confirm首次回执、受控生命周期事件重放/发布感知保留计划；低基数指标和三告警 |
| 新验证 | Text在Embedding阻塞/失败时完成、lane独立领取、维度/Profile/事实变化拒绝写入、repair清别名后重写；计划稳定/漂移/部分登记、GORM内存SQL事务回滚/去重/冲突/allocator检查和首次回执保留；ES/Qdrant限定元数据范围/404/坏回执、CanalConfirm→回执→位点、默认CLI不读凭据/不联网、重放部分结果和互动路由拒绝；100ms阻塞Broker握手遵守Deadline，YAML解析与指标断言 |
| 迁移/灰度/回滚 | 手工SQL草案新建outbox_publications及可选lane索引，本轮未执行；默认CANAL_PUBLICATION_RECEIPTS_ENABLED=false。部署核验id AUTO_INCREMENT/唯一键、同primary/池/Profile，排空旧Consumer后升级全部新Worker。回执表就绪后才可选开启单activeRelay；回退先停新登记/领取，保留任务/表/索引/位点，不恢复年龄GC，不允许旧Worker消费新任务 |
| 风险与偏差 | 受控重建实现为显式最多100 IDs的任务登记，没有全库自动重建；保留实现仅生成候选、无删除模式。audit consistent仅是元数据一致，不核正文/向量质量；会话锁没有远端版本围栏。审批摘要/操作人是运维声明，非服务端RBAC；真实DDL/路由/消费/不确定回执/故障/历史范围/召回与容量另验收。点赞/评论重放因M6幂等未闭环而拒绝 |
| 已通过 | 目标Core/CLI与生产Repository内存SQL测试；扩大回归、同范围Vet，Consumer/Projection/Repository/Canal/MQ基础设施/ES/Qdrant/两CLI Race；最后完善后同范围复验全部通过；本轮gofmt与diff check通过 |
| 下一轮 | M3-R2 Follow关系事件原子提交，然后M3-R3持久分页传播；M2-R3完整容量继续Partial，M5真实验收独立收口 |

实际通过命令（回环夹具使用获准环境，GOCACHE=tmp/go-build-cache）：

```powershell
go test ./internal/module/tweet/projection ./cmd/tweet-index-reconcile ./cmd/outbox-publication-maintenance -count=1
go test ./internal/module/tweet/repository ./internal/module/tweet/projection -run 'TestProjectionRepairRepository|TestPublication' -count=1
go test ./internal/module/tweet/... ./internal/mq/... ./internal/infrastructure/canal/... ./internal/infrastructure/mq/... ./pkg/es ./pkg/qdrant ./cmd/tweet-service ./cmd/consumer ./cmd/canal-relay ./cmd/tweet-index-reconcile ./cmd/outbox-publication-maintenance -count=1
go vet ./internal/module/tweet/... ./internal/mq/... ./internal/infrastructure/canal/... ./internal/infrastructure/mq/... ./pkg/es ./pkg/qdrant ./cmd/tweet-service ./cmd/consumer ./cmd/canal-relay ./cmd/tweet-index-reconcile ./cmd/outbox-publication-maintenance
go test -race ./internal/mq/consumer ./internal/module/tweet/projection ./internal/module/tweet/repository ./internal/infrastructure/canal ./internal/infrastructure/mq ./pkg/es ./pkg/qdrant ./cmd/tweet-index-reconcile ./cmd/outbox-publication-maintenance -count=1
```

没有执行真实索引/业务/MQ写入、DDL、模型调用、部署或GC。运行边界与真实验收矩阵见 `INDEX_RECONCILIATION_RUNBOOK.md`；M5-R2记录中的耦合创建路径已由本轮覆盖。

## 交付记录：M3-R2（2026-10-13）

| 字段 | 本轮事实 |
|---|---|
| 模块/轮次 | M3 Follow关系与派生任务 — R2 关系事件原子提交；对应F8 |
| 状态 | Implemented（离线验证）；真实迁移/数据库竞争/提交不确定/队列恢复未Accepted |
| 代码 | 追加FollowMutationRepository/持久pair协调行、UoW限定Mutation/有界有效行锁与历史重复拒绝；Service 3秒关系+Outbox事务、稳定代次动作键/重复当前状态语义；v1兼容两身份字段/关系ID/时间/W3C Trace；生产依赖/Schema存在门禁与follow.relations.v1拓扑、Canal两路与publication只读检查，停止直接MQ发布 |
| 迁移与回滚 | 独立审核加法pair表/active pair索引，先停旧Writer并只读审查重复、引擎/allocator/唯一键/账号权限，再升级Relay/持久绑定和全部新Follow副本；本轮未执行DDL。回滚保留协调行/事件/队列/位点，回退旧Writer不保持原子保证，禁止混合写入或清库 |
| 新验证 | 真实GORM/UoW/仓储内存SQL：关系/事件共同提交、事务各阶段/身份生成/行数错误回滚、12并发重复仅一个事实/事件、refollow新代次、历史重复fail-closed、事件唯一键冲突回滚、取消/缺依赖/模拟COMMIT未知；旧JSON字段兼容/Trace/Schema验证、拓扑顺序及失败/Relay路由、publication可以检查但拒绝Follow重放 |
| 已通过 | 目标编译/Follow-Event-Projection回归；扩大Follow/Tweet/MQ/Canal/Gateway Handler与五入口测试、同范围Vet；Follow/Projection/Canal/Event/Logger Race；事件冲突补测定向Race；gofmt/本轮diff check |
| 边界 | Mutex SQL模型不证明实际MySQL锁或提交不确定；暂存队列没有Consumer/TTL/GC，需限制灰度/观察积压，Confirm不证明正确绑定/消费。旧提交后缓存goroutine/大V传播/取关100条清理尚未替换；历史无事件范围、跨owner账号/迁移Job归后续。HTTP/Proto/客户端字段未改，未重复客户端构建 |
| 后续 | M3-R3唯一持久Consumer owner、稳定分页/租约/Retry-DLQ、代次当前事实重验/晋升降级/对账；M2-R3完整容量继续Partial |

实际通过命令（GOCACHE=tmp/go-build-cache，回环夹具使用获准环境）：

```powershell
go test ./internal/module/follow/repository ./internal/module/follow ./internal/infrastructure/canal -run 'TestFollowOutbox|TestRelationTopology' -count=1
go test ./internal/module/follow/... ./internal/events ./internal/module/tweet/projection ./pkg/logger -count=1
go test ./internal/module/follow/... ./internal/module/tweet/... ./internal/events ./internal/infrastructure/canal/... ./internal/mq/... ./internal/gateway/handler ./pkg/logger ./cmd/follow-service ./cmd/tweet-service ./cmd/consumer ./cmd/canal-relay ./cmd/outbox-publication-maintenance -count=1
go vet ./internal/module/follow/... ./internal/module/tweet/... ./internal/events ./internal/infrastructure/canal/... ./internal/mq/... ./internal/gateway/handler ./pkg/logger ./cmd/follow-service ./cmd/tweet-service ./cmd/consumer ./cmd/canal-relay ./cmd/outbox-publication-maintenance
go test -race ./internal/module/follow/... ./internal/module/tweet/projection ./internal/infrastructure/canal ./internal/events ./pkg/logger -count=1
go test -race ./internal/module/follow/repository -run '^TestFollowOutboxEventKeyConflict' -count=1
```

Warn未初始化panic修复与同范围复验记录在ISSUES238；没有通过初始化测试掩盖错误。运维范围/迁移/权限/回滚/真实矩阵见 `FOLLOW_RELATION_OUTBOX_RUNBOOK.md`；本轮未启动真实服务、执行DDL、业务/MQ写入或部署。

## 交付记录：M3-R3（2026-10-13）

| 字段 | 本轮事实 |
|---|---|
| 模块/轮次 | M3 Follow关系与派生任务 — R3 晋升/降级与回填清理；对应F3/F8 |
| 状态 | Implemented（生产接线/离线验证）；开关默认false、DDL未执行，真实恢复/容量未Accepted |
| 方案与实现 | projection模块独立入口/Worker；v1校验+receipt与author/pair双任务原子登记后ACK；作者共享revision/epoch/cursor、100历史关系页+当前批查，5000/4500防抖及首次迁移继承；当前关系代次/推文历史页回填与清理、写后关系变化重置。每lane Claim1、15秒页/90秒租约，SQL进度/成功续页/5次页失败保留；Redis服务器时间/epoch/token围栏与TTL、Unread/marker幂等，独立Retry/DLQ/低基数指标告警 |
| Owner与治理 | Service旧goroutine/回填/取关/晋升降级广播删除，Align拒绝旧执行；Follow入口不再装配Tweet/Timeline或迁移Tweet表。Consumer用独立Ingress/Confirm连接/QoS10/关闭等待；默认关闭，缺新表拒绝启用。显式最多100scope的默认离线CLI，Live audit只读元数据、审批repair只登记任务，持久plan/operator/reason/operation摘要；正常取消释放claim恢复本次次数 |
| 迁移/回滚 | 新作者状态/入口receipt（含registration_token）与可选历史索引手工SQL未执行；先停旧owner/旧无laneWorker，核验primary/引擎/唯一键/allocator/租约/池/最小权限和正确拓扑后灰度开启。保留事件、receipt、状态、任务、队列/位点，回滚关闭领取并等有界在途页，不混合旧owner或清库 |
| 测试 | 5012粉丝完整传播/共享游标续跑/重复事件不全扫/降级；250推文、旧取关与新代次/写后变化/隐藏已删、错页/失败/陈旧claim/取消/防抖迁移；生产GORM内存SQL登记回滚/摘要冲突/ClientFoundRows/租约进度/中断次数/命名锁；miniredis过期与陈旧Begin/副作用/大整数/Unread/TTL/marker/错类型；MQ Confirm-before-ACK/毒消息/3次DLQ/独立拓扑；CLI范围/默认不读凭据或联网/写门禁、低基数指标/YAML规则 |
| 已通过 | 目标Engine/SQL/入口/CLI测试；扩大Follow/Tweet/MQ/Canal/MQ基础设施/Event/Gateway Handler与四入口回归/Vet；Follow/Tweet Cache/Repository/MQ Consumer/CLI Race；补齐迁移防抖/审计元数据/关闭租约后同范围最终复验全部通过；本轮gofmt/diff check |
| 风险与偏差 | 作者任务共享checkpoint并按当前事实收敛，不另发晋升/降级领域事件；没有自动全表发现或任意MQ广播重放。修复审批是本机运维声明而非服务端RBAC；audit仅检查关系/作者/大V标记，不逐条对账Timeline。DBComplete不保证Redis未淘汰、无DB历史关系的残留需显式pair；其他副本L1窗口、Redis Cluster不支持、热点作者串行/全历史成本/真实恢复容量另验收。旧2字段消息保留DLQ不补造代次，M2权威读取继续保护权限 |
| 后续 | 按总表进入M6-R1互动投影可靠消费；M4-R2仍Planned，M2-R3完整容量继续Partial。真实迁移/启用/联调独立收口 |

实际通过命令（GOCACHE=tmp/go-build-cache，回环夹具使用获准环境）：

```powershell
go test ./internal/module/follow/projection ./cmd/follow-projection-reconcile -count=1
go test ./internal/module/follow/projection -run '^TestSQL' -count=1
go test ./internal/module/follow/... ./internal/module/tweet/... ./internal/mq/... ./internal/infrastructure/canal/... ./internal/infrastructure/mq/... ./internal/events ./internal/gateway/handler ./cmd/follow-service ./cmd/consumer ./cmd/follow-projection-reconcile ./cmd/canal-relay -count=1
go vet ./internal/module/follow/... ./internal/module/tweet/... ./internal/mq/... ./internal/infrastructure/canal/... ./internal/infrastructure/mq/... ./internal/events ./internal/gateway/handler ./cmd/follow-service ./cmd/consumer ./cmd/follow-projection-reconcile ./cmd/canal-relay
go test -race ./internal/module/follow/... ./internal/module/tweet/cache ./internal/module/tweet/repository ./internal/mq/consumer ./cmd/follow-projection-reconcile -count=1
```

新SQL夹具遗漏events导入的首次编译失败和修复复验已登记ISSUES239。没有执行真实DDL/业务/Redis/MQ写入、启动真实依赖、发布镜像/部署或安装依赖；详细矩阵见 `FOLLOW_PROJECTION_RECOVERY_RUNBOOK.md`。

## 交付记录：M6-R1（2026-10-13）

| 字段 | 本轮事实 |
|---|---|
| 模块/轮次 | M6互动热度一致性 — R1原子幂等投影；对应F7 |
| 状态 | Implemented（离线验证）；真实依赖/多实例恢复/容量未Accepted |
| 写边界 | LikeMutationRepository在UoW中插入并当前FOR UPDATE读取实际关系，重复关系不产事件、再赞新ID；评论/点赞3秒事实+Outbox、engagement-v1实体键/时间/Trace和身份校验，旧JSON字段/HTTP/Proto兼容 |
| 消费边界 | 移除旧ACK后goroutine/内存batcher互动路径；两个队列各串行/8秒Context/关闭等待，当前事实Public/未删/作者/48小时，映射4096字节/32主题；近期创建回执缺失不伪成功。Lua服务器时间/摘要/类型数值预检，共享前三次1小时频控/2或5权重/24小时热度/7天回执原子提交，成功后ACK |
| 恢复与观测 | 专用Retry.v1经timeline.retry→timeline.ingress、1/2/4秒/3次、独立DLQ；Confirm后ACK，发布失败保留原消息/正常取消释放；固定日志/Trace/指标与三告警 |
| 验证 | miniredis生产Lua32并发重复/ACK丢失、共享频控/TTL/坏值无半提交、乱序/当前公共事实/过期标记/冲突/32主题界限/大ID；GORM内存SQLUoW/锁定LIMIT1/ClientFoundRows/再赞/Outbox回滚；稳定事件/Trace、MQ确认顺序/失败/取消、指标/YAML；扩大测试/Vet/Race/gofmt/diff全部通过 |
| 方案偏差 | 使用MQ原投递+Redis原子回执，不另建SQL互动任务；既有事实Outbox已持久化，投影成功前MQ或Confirm恢复队列拥有消息。R2专用Retry/DLQ前移保证R1失败可恢复；人工互动重放继续关闭，不借既有生命周期CLI广播互动 |
| 升级与回滚 | 无新DDL；真实校验现有唯一键/事务/权限，先停全部旧互动消费者，升级Producer/Relay兼容链后启动新Consumer。回滚暂停领取/等有界在途，保留Outbox/位点/队列/Redis标记，不直接恢复旧计分路径；本轮未执行真实操作 |
| 局限 | Lua不回滚运行时异常，预检不替代OOM/Redis AOF/故障切换验收；标记7天/年龄48小时并非永久exactly-once，Redis淘汰/flush会改变恢复边界。全局ZSet热key与标记空间、Redis Cluster跨slot、命名锁协作范围、历史已计分隐藏热度未撤销；分钟Ticker/50秒衰减锁和人工重放仍归R2 |
| 后续 | M6-R2受控重放/断连生命周期和多副本衰减时间桶；M2-R3完整容量继续Partial，M4-R2仍Planned |

实际通过命令（GOCACHE=tmp/go-build-cache，回环夹具使用获准环境）：

```powershell
go test ./internal/module/tweet/service ./internal/module/tweet/repository ./internal/mq/consumer -run 'Test(Persist|ApplyLike|Engagement|DeclareTimelineRecovery)' -count=1
go test ./internal/module/tweet/... ./internal/mq/... ./internal/events ./internal/infrastructure/canal/... ./internal/infrastructure/mq/... ./internal/module/notification/... ./cmd/tweet-service ./cmd/consumer ./cmd/canal-relay
go vet ./internal/module/tweet/... ./internal/mq/... ./internal/events ./internal/infrastructure/canal/... ./internal/infrastructure/mq/... ./internal/module/notification/... ./cmd/tweet-service ./cmd/consumer ./cmd/canal-relay
go test -race ./internal/module/tweet/service ./internal/module/tweet/repository ./internal/mq/consumer ./internal/events -count=1
```

SQL/JSON夹具错误与沙箱回环权限失败修复/复验见ISSUES240。没有执行DDL、真实服务/业务/Redis/MQ写入、镜像发布、部署或安装依赖；真实验收/升级/回滚矩阵见 `ENGAGEMENT_PROJECTION_RUNBOOK.md`。

## 交付记录：M6-R2（2026-10-13）

| 字段 | 本轮事实 |
|---|---|
| 模块/轮次 | M6互动热度一致性 — R2重放与多副本；对应F7 |
| 状态 | Implemented（离线验证）；真实依赖/多实例崩溃与容量未Accepted |
| 衰减 | Redis TIME分钟桶/7天完成状态与0.95分值更新/原Top100裁剪同Lua；最多10000基数、写前类型/桶/分值验证；保留剩余毫秒TTL、无TTL修复24小时，空桶登记、时钟回拨拒绝/遗漏桶跳过。移除旧50秒锁/ZINTERSTORE；逻辑owner为原子桶，进程无需持有租约 |
| 会话 | 每互动队列独立Consume+Confirm连接/QoS1，5秒初始化覆盖协议/声明/订阅，取消硬关闭拥有的transport；消费/Confirm关闭重建专用拓扑与订阅，不替换生命周期共享Channel。会话取消中断在途，关闭等待Watcher/循环和衰减，入口等待Timeline返回 |
| 重放 | 新engagement-dlq-replay默认离线不读凭据/联网，Live检查另显式allow-live；Execute限量/时间/累计上限和操作声明，只互动Ingress/Confirm后ACK。沿用生产事件解码，不补造身份/时间，毒消息/过期/次数耗尽保留；批次失败/取消保留、ACK不确定停止；摘要报告，既有生命周期CLI范围不变 |
| 观测 | 低基数trends_decay_bucket_total/seconds，新增衰减失败/容量/时钟和延迟告警；沿用互动失败/ACK/DLQ信号，无正文/用户/EventKey标签 |
| 测试 | 32并发同桶/重启重试/50秒窗口/跨桶/时钟回拨/遗漏桶、TTL/原排名/坏状态/超界限/取消；重放双路由/Confirmed ACK/身份时间/大小/次数/批次取消/发布失败/ACK不确定/元数据不变；会话两次失败后重建/关闭/专属Publisher/在途取消、net.Pipe协议取消与watcher替换、CLI默认离线/门禁/Deadline，指标/YAML。扩大测试/Vet/Race/gofmt/diff全部通过，本轮无新增失败 |
| 升级/回滚 | 无SQL迁移，新增Redis桶为加法状态；每Consumer新增4条Broker连接，核验ACL/真实绑定/持久化/预算。先停止全部旧衰减/互动owner再升级；回滚取消并等本轮在途退出，保留桶/标记/Outbox/位点/队列，禁止直接恢复旧衰减或混跑 |
| 偏差/局限 | 用Redis原子分钟桶代替长期进程owner，不补算遗漏桶，单次基数保护而非无限ZINTERSTORE。超基数不自动清理，持续写入仍可能增大热key；OOM/AOF/状态淘汰/Cluster跨slot不支持、原DLQ ACK不确定不受副本header严格全局计数约束，本机execute声明非持久审批/RBAC，检查requeue可能改顺序。Confirm不证明路由/消费；其他生命周期连接重建/完整关闭仍待后续 |
| 后续 | 按总表进入M7-R1数据所有权/迁移治理；M2-R3完整容量继续Partial，M4-R2仍Planned；真实故障矩阵独立收口 |

实际通过命令（GOCACHE=tmp/go-build-cache，回环夹具使用获准环境）：

```powershell
go test ./internal/mq/consumer ./internal/infrastructure/mq ./cmd/engagement-dlq-replay ./cmd/consumer -run 'Test(TrendsDecay|EngagementReplay|EngagementSession|EngagementRecovery|EngagementAlert|BoundedConnection|ReplayCLI)' -count=1
go test ./internal/module/tweet/... ./internal/mq/... ./internal/events ./internal/infrastructure/canal/... ./internal/infrastructure/mq/... ./cmd/tweet-service ./cmd/consumer ./cmd/canal-relay ./cmd/timeline-event-dlq-replay ./cmd/timeline-moderation-dlq-replay ./cmd/engagement-dlq-replay -count=1
go vet ./internal/module/tweet/... ./internal/mq/... ./internal/events ./internal/infrastructure/canal/... ./internal/infrastructure/mq/... ./cmd/tweet-service ./cmd/consumer ./cmd/canal-relay ./cmd/timeline-event-dlq-replay ./cmd/timeline-moderation-dlq-replay ./cmd/engagement-dlq-replay
go test -race ./internal/mq/consumer ./internal/infrastructure/mq ./cmd/engagement-dlq-replay -count=1
```

问题边界见ISSUES241。没有执行真实DDL/业务/MQ/Redis写入、真实重放、启动真实依赖、镜像发布/部署或安装依赖；运维矩阵见 `ENGAGEMENT_RECOVERY_RUNBOOK.md`，离线夹具不替代Accepted。

## 交付记录：M7-R1（2026-10-13）

| 字段 | 本轮事实 |
|---|---|
| 模块/轮次 | M7服务与运行隔离 — R1数据所有权/迁移；对应F9 |
| 状态 | Implemented（离线验证）；真实Schema/账号/权限/Job未Accepted |
| 运行边界 | 八MySQL进程ServiceDBConfig/只读CheckRuntime，移除启动AutoMigrate与MemoryManager Persona构造DDL；≤32表/5秒/每表512元数据项门禁，InnoDB/列存在/64位身份/NOT NULL/命名精确索引顺序唯一无截断，启用开关额外校验。规范驱动DSN/5秒初始化与读写，移除密码fallback |
| 迁移边界 | Owner目录稳定离线CREATE计划/SHA，schema-migrate默认不读凭据或联网，Live独立非root迁移账号/≤60秒，执行匹配计划/operator/reason/operation；同连接数据库+Owner锁，所有既有表先预检、仅建缺失表/写后重验，部分或不确定停止并核验后重跑，无AutoMigrate/ALTER/DROP/外部SQL输入。单次非root只读容器Job与按包Dockerfile已写入、未构建运行 |
| 权限/读Port | 只输出GRANT访问计划，表级DML/Follow只读/CDC显式replication例外、不含密码/账号创建/DDL/ALL/GRANT OPTION；生产八对角色凭据/强制策略与PS预检。TweetService只接FollowReadRepository，wrapper不导出写接口，共享候选SQL继续保留；domain.AgentPersona/UserPersona兼容别名 |
| 验证 | 所有Owner计划重复稳定/跨Owner拒绝，Schema错引擎/列/签名/索引唯一顺序/截断拒绝，预检无DDL/取消/摘要篡改/部分DDL/回执丢失重跑；生产GORM内存SQL同连接命名锁/参数化有界metadata/CREATE验证/取消后释放回执，配置凭据配对/生产禁止fallback/root/DSN字符与IO界限，CLI默认离线/执行门禁、权限范围/生产YAML/PS语法，Follow只读能力。扩大测试/Vet/指定Race/gofmt/diff全部通过 |
| 偏差与局限 | 本轮Job为owned bootstrap，不把AutoMigrate搬到另一个进程；既有表升级继续单独审具体SQL，不提供通用任意DDL执行或持久迁移版本账本。配置和GRANT计划不等于已创建账号/最小权限，门禁不覆盖所有长度/默认值/字符集/触发器，精确索引名拒绝异名等价索引；本机声明/报告非服务端审批。共享MySQL/Outbox/Feed跨表/Consumer仓储读/CDC服务器级权限与其他账号仍Partial |
| 升级回滚 | 先审实际差异/备份/停止旧DDL owner，独立建缺表和审核加法升级/账号授权，复验实际Schema/权限后启动只读门禁服务；回滚保留结构、Outbox/位点/回执和无DDL账号，禁止恢复旧AutoMigrate。M3默认关闭/生产删除关闭不变。本轮未执行真实操作 |
| 后续 | M7-R2角色依赖/Persona应用Port与共享准入；M2-R3完整容量继续Partial、M4-R2仍Planned，真实Schema/权限/Job矩阵独立验收 |

实际通过命令（GOCACHE=tmp/go-build-cache，回环夹具使用获准环境）：

```powershell
go test ./internal/infrastructure/schema ./internal/infrastructure/persistence ./internal/module/follow/repository ./cmd/schema-migrate -count=1
go test ./internal/infrastructure/schema ./internal/infrastructure/persistence ./internal/module/user/... ./internal/module/tweet/... ./internal/module/follow/... ./internal/module/messenger/... ./internal/module/notification/... ./internal/module/agent/workflow/rag ./internal/module/agent/startup ./internal/mq/... ./internal/infrastructure/canal/... ./cmd/user-service ./cmd/tweet-service ./cmd/follow-service ./cmd/messenger-service ./cmd/notification-service ./cmd/consumer ./cmd/canal-relay ./cmd/agent-service ./cmd/schema-migrate -count=1
go vet ./internal/infrastructure/schema ./internal/infrastructure/persistence ./internal/module/user/... ./internal/module/tweet/... ./internal/module/follow/... ./internal/module/messenger/... ./internal/module/notification/... ./internal/module/agent/workflow/rag ./internal/module/agent/startup ./internal/mq/... ./internal/infrastructure/canal/... ./cmd/user-service ./cmd/tweet-service ./cmd/follow-service ./cmd/messenger-service ./cmd/notification-service ./cmd/consumer ./cmd/canal-relay ./cmd/agent-service ./cmd/schema-migrate
go test -race ./internal/infrastructure/schema ./internal/infrastructure/persistence ./internal/module/follow/repository ./internal/module/tweet/service ./internal/module/agent/workflow/rag ./cmd/schema-migrate -count=1
```

导入/引擎后缀/旧接口夹具失败与修复最终复验见ISSUES242。Production YAML/Job/Dockerfile静态门禁和PowerShell Parser语法通过，不等于实际镜像/Compose配置/权限或部署验收。没有执行DDL/账号创建/授权、启动真实依赖、构建镜像/部署或安装依赖；具体Owner/例外/迁移/回滚见 `DATA_OWNERSHIP_MIGRATION_RUNBOOK.md`。

## 交付记录：M7-R2（2026-10-13）

| 字段 | 本轮事实 |
|---|---|
| 模块/轮次 | M7服务与运行隔离—R2 Agent依赖与共享准入；对应F9/F10，子项A Persona、B共享Run、C Provider、D管理/接线 |
| 状态 | Implemented（代码/离线验证）；开关默认false，真实权限/Redis/费用/部署未Accepted，整体隔离仍Partial |
| 生产装配 | domain.AgentPersonaStore/SQLPersonaStore；RAG移除GORM、MySQL与认知引擎仅API/all；shared adapter在Runtime外，main在依赖前校验完整Runtime模式、装配前校验Redis策略，注入Runner/Workflow与Provider；API不启动Worker，Worker无Persona DB |
| 行为 | 双client原子global/user/workflow与global/provider租约、续租/失效取消/条件释放/崩溃过期，嵌套Run借父scope仍独立工作流槽；出网前估算输入+最大输出与可选上界费用预留，服务器固定分钟，失败不退款、共享故障不转本地 |
| 管理与配置 | 默认离线agent-admission-admin；显式Live只读或匹配策略/操作声明初始化，共5秒；.env/三Compose透传，Worker生产预检跳过Persona凭据并探测9191；固定标签Counter及两项告警 |
| 测试 | Fake/SQL driver、双Redis client、受控HTTP覆盖竞争/嵌套/替代token/续租/取消/预算/坏状态/零调用/SDK输出上限/Persona owner/角色AST/YAML/CLI；完整Agent+Schema/Persistence/两入口Test/Vet及相关Race全部通过，PS Parser通过 |
| 契约/迁移 | 无Proto/HTTP/Web/Mobile变化、无新DDL；保持原Persona表/Unix秒时间与单Run预算、工具/Checkpoint审批。初始化不是业务热路径，不执行真实操作或自动清key |
| 风险/回滚 | 摘要/Embedding/RAG/Reranker/Temporal/Eval未纳入此共享费用；估算非账单、Redis丢失/淘汰/时钟/远端取消需真实演练。先停/排空全部旧owner再统一开关/策略，保留状态；回到false失去跨副本上限，不用TTL到期证明远端已停 |
| 后续 | M8-R1完整发布流水线；真实初始化/账号/多进程/容量、分角色副本/服务发现/抓取与告警独立验收，M2-R3完整容量仍Partial、M4-R2仍Planned |

实际通过命令（GOCACHE=仓库tmp/go-build-cache；含回环夹具使用常规审批允许的执行环境，均不访问真实依赖）：

```powershell
go test ./internal/module/agent/model ./internal/module/agent/startup ./internal/module/agent/admission ./internal/module/agent/repository ./cmd/agent-admission-admin ./cmd/agent-service -count=1
go test ./internal/module/agent/... ./cmd/agent-service ./cmd/agent-admission-admin ./internal/infrastructure/schema ./internal/infrastructure/persistence -count=1
go vet ./internal/module/agent/... ./cmd/agent-service ./cmd/agent-admission-admin ./internal/infrastructure/schema ./internal/infrastructure/persistence
go test -race ./internal/module/agent/admission ./internal/module/agent/model ./internal/module/agent/repository ./internal/module/agent/runtime ./internal/module/agent/startup ./internal/module/agent/service ./internal/module/agent/workflow/rag ./internal/module/agent/workflow/tool ./cmd/agent-admission-admin -count=1
```

模型策略变量遮蔽、Persona SQL夹具LastInsertId/参数顺序与普通沙箱回环初始化失败已修复或按环境复验，见ISSUES243。YAML/角色AST与PowerShell语法检查不替代实际Compose/权限/镜像/告警验收；未构建镜像/启动真实依赖、初始化真实Redis、执行DDL/账号/写入/部署或安装依赖。具体操作、覆盖清单与恢复边界见 `AGENT_SHARED_ADMISSION_RUNBOOK.md`。
