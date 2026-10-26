# Project Map（Agent 必读项目导航）

> 状态：当前仓库导航的唯一规范入口（Canonical Navigation Context）
> 最近核对：2026-09-26
> 模块：`twitter-clone`
> Go：`1.25.5`

> 2026-10-26 仓库同步：Python 客服源码并入 `support-agent-service/`，与 Go 主仓库共用 Git，独立 Python/前端依赖与镜像。正式小栖仅注册发布指南检索；会话 Checkpoint 使用独立 Mongo 集合，向导不执行账号内容查询或 Workflow。主站仍通过 5174 iframe 集成，后端 8000；`compose.live.yaml` 在主站 Compose 合并时使用 `./support-agent-service` 构建路径。仅源码与离线验证，不重启、部署或扩大生产发布目录，旧相邻目录保留备份。见 `support-agent-service/docs/repository-sync-2026-10-26.md`。

> 2026-10-15 M2-R3-D：cmd/feed-read-acceptance追加capacity.go（有界worker/连续页校验/报告）和mysql.go（生产Repository只读装配/预检）。v2每样本独立RR/回滚，≤8并发/100样本/20页/总60秒；关注结果逐作者true、有效页/行/关注门槛，有界关注探针/TABLE_ROWS估计/起始EXPLAIN；固定错误与成功分位数/失败/截断/可选p95预算，capacity_accepted恒false。无新依赖/DB/Proto/生产或客户端改动；工具及相关Repository Test/Vet/Race、CLI门禁与格式通过，11个入口顶层测试含内存driver装配与并发取消。仅离线，无真实负载/DDL/种子；M2-R3容量仍Partial，旧v1小数据SQL保留历史。参数/字段/回退见docs/FEED_ACCEPTANCE_RUNBOOK.md。

> 2026-10-15 M4-R2：Messenger入库后统一2秒Redis提示预算/Trace、固定结果指标；Gateway订阅ACK后ready、单writer/心跳/有界读写/到期关闭、取消并等待reader。Web的utils/messageHistory.ts、Mobile的features/chat/presentation/message_history.dart保持独立旧历史cursor/完成同步锚点/恢复cursor，ready/前台/30秒前台周期最多3页补拉并可继续；字符串ID去重/已读单调合并，会话刷新重置快照链，草稿与迟到响应隔离。同账号其他设备发送也合并。无新Proto/Schema/依赖；告警沿现有Prometheus rules通配目录。目标Go Test/Vet/Race、两入口编译、Web18测试/build、Flutter零问题/32测试通过。代码Implemented，M4整体Partial；真实索引/MySQL计划/断网/多端/多副本/容量/告警未Accepted，无幂等发送/持久投递/ACK，低ID迟到提交有局限。运行/回退/现场清单见docs/MESSENGER_RECOVERY_RUNBOOK.md；覆盖旧记录M4-R2 Planned，M8-R3现场与M2-R3容量仍Partial。

> 2026-10-15 M8-R3：scripts/production-acceptance-record.ps1追加v2，纯本地校验在scripts/lib/deployment-evidence.ps1，31项目录沿deploy/acceptance/production-launch-checks.json。v2绑定经独立审核的已发布release manifest/source/环境快照/部署ID/前版摘要，每项Passed要求相同身份/检查/actor/时间和原始证据摘要的live观察（24小时）；严格有界JSON/路径reparse/全字段HMAC/写锁/独占初始化，旧v1可读但不通过release-ready。两PS夹具/CI production-config接线、31项/24拒绝用例、发布工具Test/Vet/Race/12服务矩阵通过，真实Actions未执行。用户本轮仅要求工具与离线验证，整体Partial；没有staging/Registry/恢复/Provider/告警操作，deployment_accepted仍false。操作/兼容/信任边界和现场待办见DEPLOYMENT_ACCEPTANCE_RUNBOOK及DEPLOYMENT_ACCEPTANCE_2026-10-15；M4-R2仍Planned、M2-R3容量仍Partial，后续代码可补M4-R2。

> 2026-10-14 M8-R2：认证契约见docs/AUTH_SESSION_CONTRACT.md；Auth既有Logout RPC回执本地清理，Gateway新增GET /auth/session、POST /auth/logout、POST /auth/password（全设备all明确501，Context身份/4KiB/5秒/密码字节与错误映射）。Web/Mobile新增/account/security，代次隔离迟到登录/刷新/401；Web AuthSession成对保存/损坏缓存/跨标签重载、Axios/SSE隔离、身份变化重建页面与客服iframe/Socket；Mobile认证驱动appRouterProvider替代静态Storage路由、串行持久化/排队Logout竞争、通知换代、Socket取消Timer/旧回调与去敏感日志。User GetByID将SQL未找到映射domain.ErrUserNotFound，改密不把数据库故障伪报404；旧网关非JSON错误有兼容提示。JWT仍无状态，退出和改密不提前撤销，无新Proto/Schema/存储；全设备撤销/找回密码Planned，数据删除继续关闭。最终全仓Go Test、认证/Gateway相关Vet/Race、Web build/13测试、Flutter零分析问题/25测试通过。代码Implemented、真实部署未Accepted；下一M8-R3。

> 2026-10-13 M8-R1当前发布边界：deploy/release/catalog.json驱动12镜像（全部Compose build+独立Schema Job），Makefile与Docker按cmd包构建；CI包括Go全仓/Vet/指定Race/全部cmd Linux、Web锁安装/build/npm test、固定Flutter3.44.2与生产config-only夹具。默认候选OCI，仅main显式dispatch publish与审核基础digest允许Registry上传，不自动写Git/Helm/部署。releaseplan/CLI默认离线，清单要求干净Git版本/精确12镜像9检查/模板锁与迁移摘要，Verify拒绝漂移/候选/伪部署完成。完整Go Test/CI同范围Race/全仓Vet、Windows-Linux包构建、Web完整build/7测试、Flutter analyze零问题/test16项、配置夹具通过；Mobile相关API/页面有效性/对话框Context与启动迟到回调测试门禁已修复。代码本机验证Implemented，真实CI/镜像/Registry/部署未Accepted，历史Helm/tag导出保留；下一代码轮次M8-R2，见RELEASE_PIPELINE_RUNBOOK。

> 2026-10-13 M7-R2当前运行边界：domain.AgentPersonaStore/Agent SQL适配器，MemoryManager不持有GORM；仅API/all装配Persona MySQL/只读Schema/认知引擎，Worker不读取Persona凭据/连接。admission.Shared位于Runtime外，用Redis TIME/同slot原子global/user/workflow和Provider租约、续租/失效取消/条件释放及分钟估算Token/可选费用预留；Runtime/Router、Workflow LLM、Agent兼容/Workflow策略SDK均已注入。AGENT_SHARED_ADMISSION_ENABLED默认false，开启API/all须完整Runtime v2模式并匹配独立初始化的Redis策略，不自动切本地。cmd/agent-admission-admin默认离线，Live共5秒/匹配摘要/操作声明；三Compose/Worker生产凭据与健康门禁/固定指标告警已接线，完整Test/Vet/Race通过。摘要/Embedding/RAG/Reranker/Temporal/Eval未纳入共享费用，其他Worker依赖保留；无新DDL/真实初始化/部署，整体隔离与故障/容量/费用验收仍Partial，见AGENT_SHARED_ADMISSION_RUNBOOK。

> 2026-10-13 M7-R1当前存储治理边界：`internal/infrastructure/schema/` 提供Owner/runtime目录、≤32表/5秒只读metadata校验、稳定离线CREATE计划/同连接命名锁/缺表Bootstrap与权限计划。八MySQL入口和MemoryManager构造不再AutoMigrate；ServiceDBConfig在production要求独立非root成对凭据，Compose与预检已接线，NewDB规范DSN/初始化与I/O有界。`cmd/schema-migrate` 默认离线、Live≤60秒/执行计划与操作声明、只建缺失Owner表，独立Job/按包Dockerfile未构建或运行。Tweet装配FollowReadRepository只读wrapper，Persona归domain兼容别名；共享Feed SQL/Outbox/CDC例外仍保留。扩大回归/Vet/Race完成，真实Schema/账号/权限/Job/部署未Accepted；旧表漂移拒绝并另审SQL，回滚保留结构/事件，禁止恢复旧运行DDL。

> 2026-10-13 M6-R2当前恢复边界（覆盖下方R1）：`engagement_session.go` 为每互动队列持有独立Consume/Confirm连接/QoS1、5秒协议初始化与取消、专用拓扑重建/关闭等待；`trends_decay.go` 使用Redis TIME分钟桶原子衰减/原Top100裁剪/TTL，最多10000基数、7天完成状态，移除旧50秒锁/ZINTERSTORE。`cmd/engagement-dlq-replay` 默认离线，显式Live检查/执行最多100条/60秒、仅互动专用Ingress/Confirm后ACK、固定摘要报告；旧生命周期CLI仍限制原路由。扩大测试/Vet/Race通过，无SQL迁移或真实操作；Redis持久化/淘汰/实际Broker绑定/崩溃/容量未Accepted。缺失桶不追算、旧新衰减owner不可混跑，标记不等于永久exactly-once。

> 2026-10-13 M6-R1 互动边界：TweetService点赞使用同UoW的LikeMutationRepository当前锁定读/实际关系ID，重复关系不产Outbox；点赞/评论追加engagement-v1/实体键/Trace。Consumer的engagement_projection/delivery原子Lua去重+共享频控+计分，当前公共事实/32主题/48小时年龄，7天Redis回执后ACK；两队列各串行/有界关闭，专用Retry.v1/DLQ与低基数观测。扩大测试/Vet/Race离线完成；无需新DDL，真实Schema/Redis持久化/绑定/容量未Accepted。旧衰减/人工互动重放仍待M6-R2，旧消费者不可混跑，Redis丢标记不保证全局exactly-once。

> 2026-10-13 M3-R3当前派生边界（覆盖下方R2历史状态）：`internal/module/follow/projection/` 提供v1入口原子双任务/持久回执、作者共享revision/epoch/cursor、100条历史关系/推文页与当前代次重验、SQL租约检查点/Redis服务器时间+epoch围栏、独立Retry/DLQ/指标告警。`cmd/consumer` 在FOLLOW_PROJECTION_WORKER_ENABLED=true时装配唯一逻辑owner、专用连接/QoS/关闭等待；默认false，新状态/回执表和历史索引手工迁移未执行。FollowService旧回填/清理/广播已移除、Align拒绝，入口不再装配Tweet/Timeline或迁移Tweet表。显式 `cmd/follow-projection-reconcile` 默认离线/最多100scope、审批后仅登记修复。扩大回归/Vet/Race及最终复验通过；真实Schema/权限/绑定/Redis淘汰恢复/多实例/容量未Accepted，不能把DBComplete当缓存永久完整。

> 2026-10-13 M3-R2 关注写边界：`FollowMutationRepository` 在已有UoW中锁定持久 `follow_pair_locks`，至多读两条有效关系、历史重复fail-closed；Follow/Unfollow状态变更与USER_FOLLOWED/USER_UNFOLLOWED Outbox在3秒事务共同提交。v1事件保留旧身份字段、追加关系代次/事件键/时间/Trace；生产FollowService装配UoW/Outbox和Schema存在门禁、持久 `follow.relations.v1` 两路绑定，Canal路由已追加。队列当前无Consumer，仅暂存事件；原提交后缓存goroutine仍待M3-R3唯一持久owner/分页/代次重验治理。手工迁移未执行，旧新Writer禁止混用；离线测试/Vet/Race通过，真实Schema/权限/锁/提交不确定/绑定恢复未Accepted。

> 2026-10-13 M5-R3 当前索引边界（覆盖下方 R2 历史路径）：`split_projection.go` 在创建 ACK 前登记独立 `sync_text/sync_vector`，旧 `sync_es` 仅登记子任务；文本/向量各 5 条独立 lane，Embedding 在会话锁外执行，向量任务固定 Model/Dimension/Version，写前/写后重验当前事实。`tweet/projection/` 提供显式最多100 IDs的元数据对账与审批摘要绑定的持久修复登记；两个维护 CLI 默认不联网，repair只入任务、publication重放仅创建/删除/治理。Canal可选 Confirm 首次回执账本默认关闭，手工SQL未执行；保留仅候选计划，年龄GC继续停用。生产接线/离线回归与Vet/Race完成，真实迁移/模型/后端/路由/恢复/召回和容量未Accepted；元数据一致不等于正文或向量质量。

> 2026-10-13 索引生命周期/Relay 边界：Consumer 的 `search_projection.go` 登记独立 `delete_es/delete_vector` 持久任务后才允许成功 ACK，旧 `sync_es` 在同一 MySQL primary 命名锁下读取当前 Public 正文/写后重验；删除适配器固定单 Tweet 的 ES 文档/Qdrant UUID与字符串别名。`TweetProjectionRepository` 的会话锁占连接但不开网络调用期间的 SQL 事务。Canal 多行转发，`OnPosSynced` 提交边界与发布同 FIFO，Broker Confirm 后保存位点；取消不越过未确认消息，未知路由明确退出，年龄GC停用，缺失/清理位点默认拒绝启动。M5-R2离线验证完成，历史补偿/发布感知保留、远端不确定写回执及真实故障收敛仍待后续，不等于全链路Accepted。

> 2026-10-13 单条推文删除边界：`TweetDeletionRepository.GetForDeletion` 在 UoW 中锁有效行并仅读取授权身份，`service/tweet_deletion.go` 将作者校验/软删除/兼容 `TWEET_DELETED` Outbox 同事务提交，删除仓储提取事务句柄。提交后有界 L1/Redis 失效；缓存 DEL/PUBLISH 错误均可观察。生产依赖不变，服务不再直接发布删除 MQ。M5-R1 离线验证完成，真实锁/提交不确定性与 Relay/ES/Qdrant 的恢复闭环仍待后续轮次，不代表全链路删除验收。

> 2026-10-12 公共检索读取边界：`TweetService.GetPublicTweets` 提供 1—100 候选 ID 的当前 MySQL 公共正文批量读取，绕过缓存；Agent 的 `retrieval/` 适配器供 MCP 搜索与 `workflow/rag` 公共知识召回共用，正文在 Reranker/Evidence/上下文之前重验。部署应先升级 TweetService，旧服务缺少 RPC 时禁止回退索引正文。开发/验收状态见 `docs/ARCHITECTURE_REMEDIATION_PLAN.md`。

> 2026-10-12 Follow 公开列表读取边界：Service 消费 `FollowerPageRepository/FolloweePageRepository`，按关注关系 ID 生成 Cursor，公开默认 20/最大 100，仓储保留 limit+1 前瞻。旧 IDs-only 仓储方法仍供未迁移的 Feed 降级和大 V 作业使用，其全量截断债务未因此解决。轮次状态见修复计划 M3-R1。

> 2026-10-12 Messenger 历史读取边界（M4-R2补充）：Service消费`ConversationPageRepository/MessagePageReadRepository`；消息按ID倒序，会话以首次ID上界内最新消息分组分页，版本化Cursor绑定调用者。已读仅更新本页接收者消息，非UI展示确认；Gateway批量补公开资料。新索引仍属M4-R1手工迁移；M4-R2双端已接入有界HTTP恢复，Redis Pub/Sub仍是可丢失提示，真实恢复未验收。

> 2026-10-12 Following Feed 当前读取边界：`FollowingFeedRepository` 在当前共享 MySQL 中按有效关注关系/自身记录进行有界候选读取，不依赖旧 Follow IDs 截断或大 V Redis 关联/深历史缓存；`FollowReadRepository` 每批重验作者当前关系，Public/Follows 与作者自身记录分别授权。`service/feed_pagination.go` 每批最多消费 100、请求最多扫描 1000，Cursor 绑定已消费候选；Gateway 返回字符串 Cursor、批量资料。Redis Inbox 仅为可恢复投影，`cache/feed_recovery.go` 使用 v2 空/非空标记、3 秒租约和原子条件合并至多 1000 条，不覆盖并发 Fanout；实际验证/迁移边界见修复计划 M2-R2，跨端/容量归 M2-R3。旧大 V 分发后台仍待 M3-R3。

> 2026-10-12 跨端Feed（M2-R3-D补充）：Web utils/feedPager.ts接Home，Mobile FeedNotifier接HomeScreen，负责精确ID去重/请求换代/有界空页续拉；推荐ListTweets Cursor为字符串。cmd/feed-read-acceptance当前v2默认拒绝Live，独立只读事务/有界并发连续页/EXPLAIN，不启动服务、写数据或访问Redis。原v1同事务小数据结果仅为历史，真实容量仍Partial，不能由Fake或v2工具通过推定生产吞吐。

> 2026-09-26 Agent 数据删除边界：Mongo 主体写入计数已覆盖对话/消息和工作流定义、运行、派生状态及补偿状态转换；其他 Agent 直接/后台写入口、遗留正计数恢复与真实 Mongo 故障验收仍未闭环，生产删除关闭。详见 `docs/PROJECT_PROGRESS.md`。

## 1. 使用规则

1. 每个新用户请求开始分析前，先读本文件，再按“任务定位表”读取相关文件。
2. 本文件只描述稳定结构、依赖方向和入口，不复制具体业务实现。
3. 事实优先级：实际代码/配置 > `go.mod` 与 Proto > 本文件 > 其他设计或进度文档。
4. 如果代码与本文件冲突，以代码为准，并在本次改动中同步更新本文件。
5. `architecture_audit_report.md` 是历史审计快照，不等于当前实现；仅在架构评审、阶段规划和技术债任务中读取。

## 2. 一屏拓扑

```text
Web (Vue 3) / Mobile (Flutter)
             |
             v
Gateway (HTTP/WebSocket, Consul discovery)
             |
             +--> Auth/User/Tweet/Follow/Messenger/Notification (gRPC)
             |
             +--> Agent Service (gRPC :9100, loopback MCP :9200, metrics :9191)
                        |
                        +--> Agent Runtime / Strategy / Message / Model / Profile
                        +--> Workflow DSL / Engine / Tool / RAG
                        +--> MongoDB (dialogue/workflow/run/project/approval/tool result)
                        +--> MinIO (private large Tool Result objects + versioned/WORM Agent Eval reports)
                        +--> ES (BM25) + Qdrant (vector)
                        +--> Public Web Search Provider (optional, governed egress)
                        +--> LLM/Embedding Provider

Tweet/Follow/... --> MySQL + Redis + RabbitMQ --> Consumer/Canal Relay
Agent AIOps jobs --------------------------------> Temporal (optional)
```

## 3. 顶层目录

| 路径 | 责任 | 进入条件 |
|------|------|----------|
| `cmd/` | 所有可执行程序的装配入口 | 服务启动、依赖注入、端口、生命周期 |
| `api/<domain>/v1/` | Proto 与生成的 gRPC 契约 | API/RPC 字段或方法变化 |
| `internal/domain/` | 社交领域实体与仓储接口 | User/Tweet/Follow/Message 等领域语义 |
| `internal/module/` | 各业务模块实现 | 服务、仓储、gRPC、缓存逻辑 |
| `internal/gateway/` | HTTP/WebSocket 入口、鉴权、gRPC Client | Web API 与路由问题 |
| `internal/infrastructure/` | MySQL、Mongo、Redis、MQ、Canal 等适配 | 基础设施连接和实现 |
| `internal/mq/` | RabbitMQ producer/consumer | Timeline、通知、异步事件 |
| `pkg/` | 可跨模块复用的基础库 | AI、ES、Qdrant、日志、指标、注册、Tracing |
| `web/` | Vue 3 + Vite + Pinia 前端 | 桌面 Web UI |
| `mobile/` | Flutter + Riverpod 客户端 | 移动端 UI |
| `support-agent-service/` | Python 使用向导、独立前端、指南、契约与测试 | 小栖问答、会话隔离、知识检索及本机联合运行 |
| `deploy/`、`docker-compose*.yaml` | 容器、监控与本地基础设施 | 部署、端口、依赖服务 |
| `docs/` | 计划、进度、API 与问题记录 | 交付记录和阶段治理 |
| `.agents/` | 项目上下文、治理规则与原生仓库级 Skills | 开发前治理与按需 Skill 触发 |
| `.codex/agents/` | Codex 项目级自定义 Subagent 配置 | 测试、审查等专用子任务 |

## 4. 可执行服务地图

| 程序 | 默认端口/形态 | 核心代码 | 主要依赖 |
|------|---------------|----------|----------|
| `cmd/gateway` | HTTP `8080`；`.env.example` 使用 `9638`；可选内部 Support Query | `internal/gateway/`、`internal/module/supportquery/` | Consul、Redis、各 gRPC 服务、MinIO；Support Query 默认关闭并要求服务 JWT、Redis 防重放/幂等 |
| `cmd/auth-service` | gRPC `9097` | `internal/module/auth/` | User gRPC、Redis |
| `cmd/user-service` | gRPC `9091` | `internal/module/user/` | MySQL、Redis |
| `cmd/tweet-service` | gRPC `9092` | `internal/module/tweet/` | MySQL、Redis、RabbitMQ、Follow 数据 |
| `cmd/follow-service` | gRPC `9093` | `internal/module/follow/` | MySQL关系/UoW/Outbox、Redis仅ID初始化、RabbitMQ持久关注拓扑；启动要求pair表/既有Outbox唯一索引，事实写入不再起派生goroutine，事件由Relay投递给可选Follow Worker |
| `cmd/messenger-service` | gRPC `9094` | `internal/module/messenger/` | MySQL（GORM）、Redis Pub/Sub |
| `cmd/notification-service` | gRPC `9095` | `internal/module/notification/` | MySQL、Redis、RabbitMQ |
| `cmd/agent-service` | `AGENT_PROCESS_ROLE=api|worker|all`；API 暴露 gRPC `9100` + loopback MCP `9200`，所有角色暴露 Prometheus `9191` | `internal/module/agent/` | Mongo、Redis、ES/Qdrant、LLM/私有MinIO保留；Persona MySQL/认知引擎仅API/all，MQ/Temporal仅Worker/all。可选共享Run/Provider准入默认关闭，API启用要求完整Runtime v2/独立初始化策略；Worker直接拥有风控队列/Retry/DLQ，默认all |
| `cmd/agent-admission-admin` | 默认离线；显式Live共5秒 | `internal/module/agent/admission/` | 离线输出策略摘要；Live读取或匹配摘要/操作声明初始化Redis，不清租约/预算，无reset模式 |
| `cmd/release-manifest` | 默认离线目录/矩阵，回执/组装/只读复验 | `internal/releaseplan/`、`deploy/release/catalog.json` | 不构造Docker/Registry Client；回执/清单要求干净Git版本与精确镜像/检查摘要，候选/发布分离，deployment_accepted始终false；验证CLI最多5秒读取Git metadata |
| `cmd/agent-mcp-acceptance` | 显式 Live 的远程 MCP 一次性验收命令 | `internal/module/agent/mcp/acceptance/`、`internal/module/agent/mcp/remote/` | 必须传 `--allow-live`；写探针额外要求 `--allow-write`，报告只保存摘要/固定错误码并可 HMAC 签名 |
| `cmd/agent-mcp-conformance` | 回环长驻 MCP 协议夹具，默认 `127.0.0.1:9320` | `internal/module/agent/mcp/acceptance/`、MCP SDK Server | 只绑定回环地址；用于本地协议、幂等、故障和验收器测试，不代表真实第三方证据 |
| `cmd/agent-rag-eval` | 显式 live 的只读 RAG 对比命令 | `internal/module/agent/eval/`、`pkg/es`、`pkg/qdrant`、`pkg/ai`、`internal/module/agent/model` | 必须传 `--allow-live`；`--strategies` 按依赖闭包懒初始化 Provider；出站连接受 Endpoint Policy/受限 HTTP Client 保护；输出策略与 Provider 失败率 JSON 报告 |
| `cmd/agent-router-eval` | Cascade Router 基线与 Provider 对照命令 | `internal/module/agent/eval/`、`internal/module/agent/workflow/rag/`、`internal/module/agent/model/endpoint_policy.go` | 默认 lexical 且不连接外部服务；semantic/llm/full 必须显式 `--allow-live`，输出错投、Stage、Provider 错误、Token/成本 JSON 报告 |
| `cmd/agent-task-eval` | Agent 行为/安全评测、单/多策略对照、签名报告、加密人工复核/失败诊断材料、版本化归档与质量门禁命令 | `internal/module/agent/eval/`、`internal/module/agent/multirole/`、`internal/module/agent/objectstore/`、固定任务集、录制结果或受控 Runtime Adapter | 默认离线；Live 必须显式使用 `--allow-live`、一个 Runtime 配置、`--live-authorization` 和持久授权账本。单机默认使用 `--live-authorization-state` 的 file 账本；多实例可显式选 Redis 后端并先独立初始化，inspect/revoke 管理模式只读模型无关状态或原子冻结授权；完整状态检查固定原子末端 Stream ID，分页重放事件并对账用量后输出稳定摘要。签发授权前可用 `--plan-live-evaluation` 在不读取 Credential、不构造 Provider Client 的前提下计算调用最小值/上界、Profile Token/费用和正文上限；授权必须覆盖完整调用与费用计划。授权由独立命令离线签发，绑定 Provider/精确 Model、数据集/配置哈希、有效期及运行、Provider 调用、正文捕获和估算成本上限；每次模型调用在出网前向选定账本原子预留，过期、撤销、篡改或超预算立即失败。策略模式在同一规范化 Provider/Model/Pricing/Profile Snapshot 和配置哈希内执行真实模型单/多对照；启动先做真实 Chat/Tool Call 预检，执行错误快速终止，`--checkpoint-dir` 用逐 Case HMAC 哈希链在同身份连续前缀上恢复。固定任务 JSON 拒绝重复 Object Key，当前策略契约为 `agent-strategy-cases-v3`。工具仍为无副作用结构化证据沙箱。策略门禁只比较两个已开放的只读研究草拟模板，报告可 HMAC 签名验签并归档到私有 MinIO Object Lock；无 Checkpoint 的 Review Bundle 用独立密钥加密输入/正文并绑定最终报告哈希。打开 Bundle 可同时生成默认全拒绝且待人工补全的外部审阅 Decision 模板。显式 `--capture-failed-review-bundle` 可保存失败诊断密文，但 Signoff 仍拒绝失败报告。模型名变化会生成新配置哈希，旧授权和资格报告不得混用；门禁不自动发布 Profile 或扩大 Multi-Agent 范围 |
| `cmd/agent-memory-migrate` | Episodic 迁移与只读验收命令 | `pkg/qdrant`、`internal/module/agent/workflow/rag/` | 仅接受显式用户 ID；支持 dry-run、原始 Point ID 迁移、`--verify-only` JSON 验收和显式删除旧集合 |
| `cmd/agent-profile-dlq-replay` | Profile 内容互动 DLQ 检查与限量人工重放命令 | `internal/module/agent/consumer/`、`internal/infrastructure/mq/` | 默认只检查并重新入队；执行要求显式操作人/原因，校验事件与累计重放上限，Broker Confirm 成功后才 ACK；报告不输出事件正文 |
| `cmd/agent-tool-result-orphan-audit` | 私有 MinIO Tool Result 孤儿对象只读审计 | `internal/module/agent/objectstore/`、`service/tool_result_orphan_audit.go`、Mongo Tool Execution 引用 | 有界分页并应用年龄宽限；逐对象核对 `output_reference.key`，输出键摘要而非正文。任何 Mongo 查询失败均 fail-closed；命令没有删除模式，不替代 Legal Hold 兼容的生命周期方案 |
| `cmd/agent-data-deletion-request` | 用户删除请求与只读资源预检运维命令 | `repository/data_deletion_request.go`、`service/data_deletion_request.go` | `request/preflight/cancel` 默认只输出计划，必须显式 `--execute` 才修改请求控制状态；`get` 只读。命令不提供物理删除动作，预检固定 `execution_supported=false` |
| `cmd/agent-risk-dlq-replay` | Agent 风控 DLQ 检查与限量人工重放命令 | `internal/module/agent/service/risk_control_replay.go`、`internal/infrastructure/mq/` | 默认只检查并重新入队且不启用发布模式；执行要求显式操作人/原因，校验风控事件与累计重放上限，只回专用 `agent.risk.ingress`，Broker Confirm 成功后才 ACK；报告不输出 Tweet/作者 ID 或正文 |
| `cmd/timeline-event-dlq-replay` | Timeline 创建/删除 DLQ 检查与限量人工重放命令 | `internal/mq/consumer/timeline_event_replay.go`、`timeline_recovery_topology.go`、`internal/infrastructure/mq/` | 默认只检查并重新入队；执行前幂等声明 Timeline 专用恢复拓扑，校验事件/累计次数并只回 `timeline.ingress`，Broker Confirm 成功后才 ACK；报告不输出 Tweet/作者 ID 或正文 |
| `cmd/engagement-dlq-replay` | 互动DLQ有界检查/重放 | `internal/mq/consumer/engagement_replay.go`、`engagement_session.go`、MQ bounded connection | 默认离线不读凭据；显式allow-live/执行声明，最多100条/60秒，毒消息/过期保留，只回互动Ingress，Confirm后ACK；检查NACK/requeue可能改变DLQ顺序 |
| `cmd/timeline-moderation-dlq-replay` | Timeline 影子治理清理 DLQ 检查与限量人工重放命令 | `internal/mq/consumer/moderation_replay.go`、`internal/events/`、`internal/infrastructure/mq/` | 默认只检查并重新入队且不启用发布模式；执行要求显式操作人/原因，校验路由、事件和累计重放上限，只回 `timeline.ingress`，Broker Confirm 成功后才 ACK；报告只输出摘要与固定结果码 |
| `cmd/consumer` | RabbitMQ consumer；Prometheus `2116` | `internal/mq/consumer/` | RabbitMQ、MySQL、Redis、ES、Qdrant、异步 Embedding；创建 ACK 前完成幂等 ZSet/趋势与独立 sync_text/sync_vector 登记；两 lane 独立领取和低基数监控，旧 sync_es 转子任务；失败仍经独立 Confirm Publisher/专用 Retry/DLQ |
| `cmd/canal-relay` | CDC relay | `internal/infrastructure/canal/` | MySQL Binlog/Canal、RabbitMQ |
| `cmd/tweet-index-reconcile` | 显式 IDs 的一次性维护 CLI | `internal/module/tweet/projection/`、Tweet Repository、ES/Qdrant | 默认离线；Live 显式依赖目标/Profile，audit 只读元数据，repair 额外审批摘要/allow-write且只登记持久任务；最多100 IDs、60秒 Context |
| `cmd/outbox-publication-maintenance` | 显式事件的检查/Confirm重放/保留计划 CLI | `internal/module/tweet/projection/`、Outbox publication Repository、Canal Confirm Publisher | 默认离线；检查不连Broker；重放额外审批摘要/allow-write，限创建/删除/治理；最多100 IDs、60秒Context与5秒Broker初始化，无删除模式 |
| `cmd/follow-projection-reconcile` | 显式作者/关系检查与入队维护 CLI | `internal/module/follow/projection/` | 默认离线、scope最多100；Live显式DB/Redis目标，audit只读元数据，repair审批摘要/操作声明绑定且仅入任务；Context60秒，无删除或任意MQ广播 |
| `cmd/schema-migrate` | Owner建缺表/Schema检查/运行账号访问计划 | `internal/infrastructure/schema/`、Persistence | 默认离线计划；Live独立迁移账号/最多32表60秒；Execute匹配计划与操作声明、同连接锁、只CREATE缺表不ALTER/DROP；access-role只输出固定GRANT计划 |

服务发现与公共基础能力：`pkg/registry`、`pkg/config`、`pkg/logger`、`pkg/metric`、`pkg/trace`、`pkg/profiler`、`pkg/serviceauth`。`pkg/serviceauth` 只提供方法级内部 gRPC 身份凭据与 fail-closed 守卫，不承载业务授权。

## 5. Agent 模块地图

```text
cmd/agent-service/main.go                 # 组合根：gRPC/内部 MCP/指标/存储/Temporal/后台任务
  -> startup/                             # 纯配置启动计划：api/worker/all 与唯一 Trending Reporter owner
  -> grpc/                                # Proto 边界；鉴权后的 user/model/workflow 参数
  -> service/                             # 用例编排；RunAgent 仅按显式能力/Catalog 路由，无能力选择时进入对话；不再根据用户关键词猜测产品路径
       -> runtime/                        # Runner、TaskSpec、Verified Checkpoint/Resume、Environment/Evidence/Verifier、Action、Budget、Usage；不依赖 Service
       -> environment/                    # G2 Environment Packs；目录/任务/策略能力交集与低敏快照，不执行工具
       -> evidence/                       # Platform/Web 可信结构 Schema；站内搜索 Goal Collector/Verifier 适配器
       -> retrieval/                      # 当前公共 Tweet 批量读取 Port / gRPC Adapter，MCP 与认知召回共用
       -> strategy/                       # 无 I/O Multi-Agent 准入、模板与版本化计划证据
       -> multirole/                      # 存储无关的顺序角色聚合核心；生产 Service 与 Eval 共用
       -> message/                        # Token-aware 上下文装配；近期原文受预算裁剪，Service 将模型蒸馏摘要与长期记忆注入统一 Runtime；Mongo 到期任务、租约 Worker 与低基数指标保证重启可恢复
       -> model/                          # ModelClient、Catalog、Endpoint Policy、Provider Adapter
       -> profile/                        # 不可变 Agent/Prompt Catalog、AtomicResolver、原子 Profile Set、Release 校验与确定性分桶
       -> project/                        # Agent 项目、成员角色、User Directory 与 MCP AccessResolver
       -> observability/                  # Run/Step/LLM/Tool TraceRecord、Recorder/Reader 契约
       -> objectstore/                    # 私有 MinIO Tool Result Adapter + Agent Eval Object Lock Archive；不进入 eval/workflow 核心
       -> repository/                     # Mongo dialogue/message/workflow run/publication/Agent execution run/project/trace/summary/tool governance/Profile versions/RBAC/experiments
       -> workflow/
            dsl/                          # 版本化 JSON DSL 强类型契约
            ir/                           # 确定性 Compile Plan、依赖与写冲突校验
            engine/                       # 并行波次调度、单写者状态、挂起/恢复
            tool/                         # ToolSpec/Executor、内置工具、审批、幂等、熔断与指标
            rag/                          # Cascade Router、Persona、Episodic、ES+Qdrant 检索
       -> mcp/                            # 回环 MCP、远程 Adapter/治理，以及隔离的生产验收 Runner/Conformance
```

关键约束：

- `runtime/` 不得反向依赖 `service/`、Mongo、Redis 或 MCP SDK。
- `environment/` 只依赖 Runtime 契约和只读 Catalog Port。Twitter/Web Pack 通过通用只读内核取静态策略、当前 Catalog 与 Task Allowlist 交集；Workflow-as-Tool Pack 按用户读取当前 Active Publication 并重验不可变发布绑定；External MCP Pack 复用权威 `ListGovernedTools`，按调用者重新校验个人/项目当前成员关系、Active Snapshot 与 Tool Policy，并要求限定名和 Runtime/Policy 分类精确一致。重复工具、非法身份/Schema/分类均 fail-closed；快照不保存 Goal、描述、Schema、正文、凭据、Connection/Snapshot/Hash 原始值或 Workflow/MCP 原始绑定，资源访问与执行前授权仍由权威 Adapter/ToolExecutor 校验。
- Goal Shadow 仍为默认关闭的观察路径：全局 `AGENT_GOAL_RUNTIME_ENABLED` 与平台搜索、Web 研究、Grounded Draft、Research Draft Shadow 开关只有同时开启才消费既有 `RunResult`，不得重复模型、工具或 Provider 请求。独立的 Planning-Execute 生产迁移覆盖 `runtime.platform_search`、`runtime.web_search`、`runtime.external_mcp` 与 `runtime.workflow`；主开关、稳定用户分桶和各路由开关均默认关闭。Web 灰度必须形成绑定同一公网 URL 的 `web_search` 与 `page_read` 双证据；MCP 只读灰度使用当前租户 `ListExecutableTools`，MCP 写灰度额外要求 `AGENT_PLANNING_EXECUTE_EXTERNAL_MCP_WRITE_ENABLED` 与现有 Unified Approval Recovery，使用 `ListGovernedTools`，Planner 只能请求动作，工具类别和审批要求由确定性策略从权威目录写入计划。Workflow 灰度只暴露当前用户 Active Publication，并以不可变 Publication/Workflow Revision、父子 Run 和权威 child output 共同判定完成。完整 `goal.v1` Verified Checkpoint 与普通 `react.v1` Checkpoint 复用同一加密存储字段并按版本区分，人工澄清和审批恢复均重新读取当前 Tool Catalog；MCP 重验连接、项目成员、Active Snapshot、Tool Policy、审批、动作与幂等绑定，Workflow 重验 Active Publication 与绑定摘要。通用 MCP 完成证据只证明稳定授权快照下的成功 Tool Observation，具体第三方业务后状态必须在 L3 由选定 MCP 的读回验收证明。第一方 Tweet 发布仍由显式 `ConfirmPublishTwitter` 驱动，不暴露为 Planner 自主写能力。API 只暴露计划摘要、用量、验证状态和 Checkpoint 身份，不暴露 Task、Evidence、参数或密文。

- Legacy Web 搜索与 Web 草拟的完成护栏独立于 Goal shadow 开关：只有既有单次 `RunResult` 含至少一个结构化、可引用的公网搜索结果时才允许持久化回答；空结果、Provider/Page 错误和私网/畸形引用 fail-closed，普通搜索不强制 `page_read`，不得因此增加模型、Provider 或页面 I/O。
- Grounded Draft Shadow 同时覆盖站内研究草拟与 Web 研究草拟：只接受可信结构化来源，并以精确 `[/tweets/{id}]` 或 `[public URL]` 标记把草稿 Artifact 绑定到来源 Evidence；它独立默认关闭，不是生产完成护栏，也不修改不可变 Profile。
- E2E-10 Rewrite Constraint 当前仅为 `evidence/` 纯离线契约：约束必须由调用方以 `RewriteConstraintSpec` 显式提供并绑定到规范化 Task，Verifier 才检查中英文主导文字脚本、JSON/Markdown 列表/纯文本结构和 Unicode 字符范围。它不解析用户关键词、不接 Service Shadow、不修改 Profile，也不新增生产执行分支。
- E2E-11 Research Draft Shadow 复用 E2E-09 来源/Citation/Artifact 事实，并要求可信研究 Observation 的 StepIndex 严格早于匹配正文的终止 `final_answer` Action；独立开关默认关闭，只观察站内/Web 研究草拟的同一 Legacy `RunResult`。
- `AGENT_PROCESS_ROLE=api|worker|all` 控制顶层生命周期：`api/all` 启动 gRPC、Consul 与内部 MCP，`worker/all` 启动 MQ Consumer、治理巡检与 Temporal Worker。热点播报只允许 `temporal|disabled`，生产组合根不得自动启动进程内 Ticker。
- Temporal 风控 Activity 不得持有 GORM、FollowRepository 或直接修改 Timeline Key；近期发帖信号与治理命令通过 TweetService 内部 gRPC。TweetService 在同一 UoW 提交可见性和幂等 `TWEET_MODERATED` Outbox，Canal 投递 `tweet.moderated`，Timeline Consumer 按关注关系 ID 稳定分页并用 Redis 游标/完成标记重放全量清理。`GetAuthorPostingStats`、`ApplyTweetModeration`、`ObserveTweetSubjectData` 与 `DeleteTweetSubjectData` 必须经过 `pkg/serviceauth` 的精确方法白名单认证；未配置凭据时只关闭这些特权 RPC，不得退化为匿名调用。用户级 Tweet 删除只处理主体拥有的六类记录及作者 Tweet 的依赖关系，在同一 MySQL UoW 写入逐 Tweet `TWEET_DELETED` Outbox 和持久摘要回执；单次最多 1000 条作者 Tweet，超量 fail-closed。Agent gRPC Adapter 当前未注册为删除 Capability，不能绕过 Legal Hold 重验或 `execution_supported=false` 停止线。ES/Qdrant/Redis 热点读取属于有界只读投影。
- 风控队列归 Agent Worker 所有，直接以 `queue.tweet.risk` 订阅原始 `twitter.events/tweet.created`；Timeline Consumer 不得二次广播风控事件。失败重试只能经 `agent.risk.retry -> agent.risk.ingress` 返回风控队列，不能重放到主事件交换机；人工 DLQ 重放同样只允许回专用 Ingress。Temporal Workflow ID 固定为 `RiskControl-Tweet-{tweet_id}` 并使用 `REJECT_DUPLICATE`，用于覆盖滚动升级、ACK 不确定和人工重放造成的重复投递。
- Timeline、Agent 风控和 Profile 内容互动消费者在正常失败路径中均先向固定 Retry/DLQ 路由取得 Publisher Confirm，再 ACK 原消息。Timeline 自动重试和人工重放只能经 `timeline.retry -> timeline.ingress` 回到自有队列，禁止重放 `twitter.events`。只有失败路由基础设施不可用时才允许有界等待后 requeue；进程关闭时允许直接 requeue 未提交消息。不得记录消息正文或把用户、Tweet、Run ID 放入指标标签。
- `tweet.created` 的派生处理顺序固定为 Timeline ZSet -> Redis 趋势 Lua -> `sync_es` Outbox -> ACK。趋势使用 Tweet ID 的 72 小时 Marker，主题规范化后最多 32 个；Outbox 使用唯一 `dedup_key` 并保留 72 小时 Success 收据。Outbox Worker 依赖 MySQL 8 `FOR UPDATE SKIP LOCKED` 原子领取，使用 Owner + 每批新 Token + 90 秒 Lease 防止多副本同时执行；过期 Attempt 被有界恢复，旧 Token 无权提交。单任务执行限制 60 秒并发启动，数据库收尾另有 5 秒上限。该护栏覆盖自动 Retry、重启和 ACK 不确定，不代表跨 Redis/MySQL/RabbitMQ 或 ES/Qdrant 的严格 exactly-once；外部写成功但回执提交失败时，固定文档/Point ID 可收敛状态，Embedding 与网络调用成本仍可能重复。
- Provider 差异放在 `model/` Adapter；业务模式只传模型/预算/Profile。
- `message/` 管上下文优先级和 Token 桶；RAG Chunk 不做请求内有损截断。
- `repository/` 只做持久化，不调用模型或工具。
- 旧 Workflow DSL 缺省按 v1 编译，新保存定义补齐版本；未知版本拒绝。Scheduler 只消费 `workflow/ir` 计划，节点只读 `StateView` 并返回 Delta，由协调器按声明顺序单写合并。
- Workflow 已使用独立不可变 Revision，Run 可固定指定 `workflow_revision_id`；Blackboard StateEvent 与周期 Snapshot 按 `state_version` 幂等持久化，周期游标使用专用原子更新且不覆盖运行控制字段，Resume 通过 Snapshot + Event 重建并校验 Checkpoint。显式全局状态写入支持七种内置 Reducer；Retry/Timeout/Cancel/Skip/Suspend 使用显式状态转换和确定性退避。DSL 顶层共享预算统一限制节点尝试、并发、总超时、Token 和成本，直接 LLM、Runtime Agent 与 Legacy 回退调用均在请求前预留并在恢复后继续累计。Tool 补偿通过 `agent_workflow_compensations` Journal 按反向拓扑持久执行，复用 Tool Policy/Approval/幂等并支持显式恢复；后台 Reconciler 安全回收普通工具的过期执行租约，独立脱敏 Journal API/控制台允许人工推进 `planned/failed/expired executing`，审批挂起和有效租约不可抢占。Run API 支持用户隔离分页、详情、持久跨实例取消及独立 Trace 查询，禁止用进程内 Cancel Map 冒充可靠控制。`GET /agent/workflow-runs/:id/traces` 返回脱敏 Run/Step/LLM/Tool 记录；`GET /agent/workflow-runs/:id/events` 使用租户隔离、TTL/长度有界的 Redis Stream 投递脱敏增量事件并支持游标恢复，Mongo Trace 仍是完整查询事实源；`GET /agent/workflow-runs/:id/replay` 提供用户隔离、哈希校验、无副作用的只读事件/快照/补偿证据；不得宣称完整 Event Sourcing 或所有补偿无人值守执行。
- `GET /agent/workflow-runs/:id/blackboard` 按目标 `state_version` 从最近快照与有界事件区间重建、校验并分页检索 Blackboard；游标绑定版本和过滤条件，值默认递归脱敏并限制预览大小，查询不执行 Scheduler/LLM/Tool。
- 所有工具通过 `workflow/tool` 的 `ToolSpec + ToolHandler` 注册，并由实例化 `ToolRegistry/Executor` 统一执行；JSON Schema 使用 `jsonschema/v6` 编译校验，Write/Risky 默认进入持久化 Approval Gate。
- Tool Result 由统一 Executor 执行 JSON 体积校验；超过硬上限直接失败，超过内联阈值经 Service Port 写入独立私有 MinIO 桶。Mongo 幂等记录只保存内联结果或对象引用，Trace 只保存哈希、长度和无凭证引用；`workflow/tool` 不依赖 MinIO/Mongo。
- 审批与工具执行结果位于 Agent Mongo Repository；Service 提供持久化 Adapter，`workflow/tool` 不依赖 Mongo。恢复令牌数据库只保存哈希；已批准审批可按 Run revision 签发 5 分钟内有效的一次性新授权，每次签发原子轮换并使旧令牌失效。
- 工具熔断按工具名维护 Closed/Open/Half-Open 状态；幂等结果回放必须先于熔断判断，不能因下游暂时熔断破坏已提交结果的可回放性。
- Tool Prometheus Label 只允许有限枚举（工具、类别、来源、决策、错误码、熔断状态），禁止放入 user/run/input/error text 等高基数字段。
- Agent 执行 Recorder 扇出到 Mongo、Prometheus 与 OTel；Mongo 是查询事实源。Agent gRPC Server 和 Tweet/User Client 使用 `otelgrpc`，Provider/MCP HTTP 使用保留原 Client 安全策略的 Transport 包装与 W3C Trace Context；指标禁止 user/run/step/model/error 原文 Label，HTTP Path/Query/Header 与 Prompt/Completion/Tool 正文不得进入 Span。Prompt/Completion 预览采样独立配置、默认关闭、确定性有界并拒绝疑似敏感内容，只允许进入租户鉴权后的 Mongo Trace。Agent 指标由 `:9191` 暴露，Docker/Helm 的 Grafana 面板固定引用 `prometheus` datasource UID。
- 后台治理对账负责回收过期审批和陈旧执行租约；审批过期时必须终止对应挂起 Run，并清理恢复字段，避免永久悬挂。
- 内置 MCP 只允许回环地址，HTTP Bearer Token 与 Tool Middleware 认证缺一不可；令牌不得写日志，未显式配置时仅使用进程内随机令牌。
- 可重试发布工具把稳定执行键传给 TweetService；Tweet/Poll/Outbox 与用户级幂等绑定在 MySQL 同事务提交。
- Runtime v2 由 `AGENT_RUNTIME_V2_MODES` 按模式灰度，Legacy 暂时保留回滚。
- P8 `RunAgent` 使用可注入不可变 Capability Catalog 与保守 Planner。`conversation.reply` 使用无工具 Runtime Profile；`content.draft` 只有消息与 Run 持久化成功后才返回绑定 `source_run_id` 的 Artifact；单独 `platform.search` 使用 `runtime.platform_search`，上下文追问继承最近一次安全能力，`platform.search -> content.draft` 使用独立组合 Profile。两条检索路径都只开放 `hybrid_search_tweets`，并要求成功的文本或结构化 Tool Observation。Web `Agent.vue` 统一调用 `RunAgent`，旧 API 与 Feature Flag 保留回滚。
- P8.2 的 `internal/module/agent/websearch` 提供 Brave Adapter、受治理 Page Reader、Redis 来源缓存、用户/Run 准入预算和 `kind=web_search` 用户级加密 Provider Config。动态 Provider 与平台兜底共享并发闸门、Cache 和 Governor；未选择用户配置且没有平台 Key 时 fail-closed。站内搜索、Web Search 和 Page Read 输出版本化 Structured Content，Citation 只从受信任 Action/Observation 生成；公网请求受 SSRF、禁重定向、DNS Rebinding、Prompt Injection、超时、并发、数量、字符和响应体边界保护。
- P8.3 的 `internal/module/agent/mcp/remote` 提供个人/项目级加密 Connection、部署托管项目凭据 Resolver、独立 Egress/Feature Flag、Streamable HTTP/SSE Discovery、`server_id.tool_name` 命名空间、不可变 Schema Snapshot、Snapshot-bound Tool Policy、有界 Session Pool 和跨实例主动健康巡检。`internal/module/agent/project` 维护 `owner/editor/viewer`、Revision CAS 成员关系和 User Service 真实用户校验；MCP 只依赖 `AccessResolver`，列表、按 ID/Server 解析、目录与真实调用均重查当前成员关系。用户凭据加密保存；项目 Bearer 连接可改用严格绑定项目/Endpoint/Auth 的托管引用，Token 从专用只读 Secret 目录按调用解析，Connection 不保存 Token/密文，共享成员不能读取密钥。同版本 Secret 文件轮换只切换 Session Identity；Registry Version 漂移会拒绝执行，必须 CAS 重存并清空 Snapshot/Policy。Workflow 可逐次审批 `risky` 与契约完整的声明式幂等 `write`；Unified Agent 在 `AGENT_RECOVERABLE_RUNS_ENABLED`、`AGENT_EXTERNAL_MCP_ENABLED` 和 `AGENT_UNIFIED_APPROVAL_RECOVERY_ENABLED` 同时启用时，也从当前用户治理交集装配 `read/risky/write`。独立 `agent_execution_runs` 使用 Revision CAS 保存权威生命周期；`ask_human/tool_call` 共享加密 `react.v1` Checkpoint、Resume Claim 租约和租户隔离恢复 RPC。工具授权签发、恢复及真实调用前重新校验 Profile/Prompt、Connection、Credential、Snapshot、Schema、Policy、审批和动作/输入/幂等绑定；一次性令牌只保存哈希并轮换，恢复精确动作不重放模型或已成功 Step。`risky` 最多一次远端尝试，`write` 仅以平台稳定键做有限同键重试；拒绝或过期终止 Agent Run。`mcp/acceptance` 通过独立显式 Live/Write 命令复用生产网络与 SDK 边界，生成不含 URL/凭据/原始结果的可签名报告；回环 Conformance 只用于框架自测。项目作用域、托管凭据、审批恢复和验收 Job 均默认关闭；真实 Brave/第三方 MCP、旧 Token 撤销、Kubernetes Secret 轮换和多副本受控验收尚未完成，第三方幂等声明不是严格 exactly-once 证明。
- P8.4 Workflow-as-Tool 位于 `repository/workflow_tool_publication.go`、`service/workflow_as_tool.go` 与 Unified Agent `runtime.workflow` 路径。发布记录按用户隔离并用 Revision CAS 绑定不可变 Workflow Revision/DSL Hash；继续保存草稿不改变已发布行为。`AGENT_WORKFLOW_AS_TOOL_ENABLED` 默认关闭，关闭时拒绝新发布和 Runtime 发现/调用，但允许查询与停用。可发布 DAG 必须无补偿、无 `agent`、无递归 Workflow Tool；只读工具不得声明审批，写工具必须同时声明逐次审批与幂等，风险工具必须逐次审批，审批型发布还要求 Recoverable Run、统一审批恢复、Run Store 与 Checkpoint Cipher 全部可用。外部 MCP 节点按当前 Snapshot/Policy 做同等校验。`wait` 仅允许显式 `resume_mode=human_input` 和非空有界问题；外部回调、用户恢复令牌及未知工具继续 fail-closed。动态工具复用统一 ToolExecutor；子 Workflow Run 保存 `parent_run_id/parent_action_id`，父子准入预算独立且父工具超时约束子运行。第二增量在 `internal/module/agent/skill/` 与 `service/workflow_skill_catalog.go` 增加版本化 Skill 契约：Skill 由当前用户 Active 发布记录确定性投影，精确版本绑定发布/Workflow Revision、DSL Hash、Profile/Prompt、预算、指令、单一工具和输出 Schema；`skill.run -> runtime.skill` 只接受显式 `skill_id + version`，并在规划、Runtime 和真实工具调用前重校验绑定。`AGENT_SKILL_CATALOG_ENABLED` 默认关闭且依赖 Workflow-as-Tool；关闭只撤销目录和路由，不改发布元数据。
- L3 部署入口位于 `deploy/helm/twitter-clone/`：`values-agent-staging.example.yaml` 提供默认关闭的小流量 Planning-Execute 示例，Agent Deployment 按 api/all 与 worker 角色分别使用 gRPC Health 与 metrics TCP 探针，MCP Acceptance Job 可把签名报告写入已有 PVC。上线顺序、Secret 交付、真实 MCP/Workflow 验证和独立回滚见 `docs/agent_launch_runbook.md`；Helm 渲染或回环测试不等于真实生产证据。
- L4 单机生产基线由 `docker-compose.production.yaml`、`deploy/compose/production.env.example` 与 `scripts/validate-production-compose.ps1` 组成：生产只接受 digest 镜像、仓库外 Secret 文件、回环公开端口、资源/重启/健康约束和真实 Embedding；本地开发 Compose 保持独立。操作与回滚见 `docs/production_compose_deployment.md`。预检不会拉镜像或启动容器，不能替代 clean-host、TLS、备份恢复和故障演练。
- L4 Agent 应用数据保留位于 `repository/data_retention*.go`、`repository/data_deletion_*.go`、`service/data_retention*.go`、`service/data_deletion_request.go`、`internal/module/agent/deletion/`、`objectstore/minio_subject_deletion.go`、`workflow/rag/qdrant_subject_deletion.go`、`service/tool_result_orphan_audit.go` 及三个运维命令：Worker 以 Mongo 全局租约、逐候选 Claim、用户级清理租约、有界批次和超时处理常规过期数据；用户删除八阶段协议以不可变 Capability Snapshot 绑定 Adapter/Version，阶段审计与 Checkpoint 同一 Mongo CAS 原子追加。通用外部 Adapter 通过 Mongo Journal 在外部写前事务重验 Legal Hold Revision/Evidence，持久化 before-state、排他租约和不可变回执，进程中断后可按同一 Operation Key 对账重放。未注册的 MinIO Adapter 固定私有桶与租户哈希 Key Manifest；未注册的 Qdrant Adapter 固定共享集合 `user_id` Filter，并要求显式证明旧 `episodic_user_<id>` 集合已离线迁移退役，否则拒绝构造 Capability。未注册的共享关系 Adapter 在单个 Mongo 事务内重验治理与所有权，只解绑非 Owner 项目成员、删除个人 MCP Connection/Snapshot 和 Profile Role Binding，并持久化零残留回执；项目所有权、项目级 MCP 创建者关系或 Marketplace Publisher 所有权均要求先显式转移。`repository/data_deletion_write_fence.go` 已把 Agent 侧栅栏身份持久化到 retention subject。Tweet Service 另以 `tweet_subject_write_fences` 的每用户固定行和 `FOR UPDATE` 锁实现本地持久门闩；发布、点赞、评论、投票、收藏、转发均在同一 MySQL UoW 内先授权再写，物理删除事务反向要求门闩已安装。内部 `EnforceTweetSubjectWriteFence` RPC 纳入服务身份白名单。Agent 事实源尚未接入全部 Agent 写入口，Agent/Tweet 门闩也尚未组合成 Worker 的单一跨服务回执。共享 MCP/项目关系、删除请求和治理记录不进入 owned-data 清单。生产 Capability Registry 仍为空，运维 CLI 不暴露物理删除；用户 API、后台调度、跨存储执行装配及真实 Mongo/MinIO/Qdrant/MySQL 故障恢复证据仍不存在。MinIO 已孤立 Key 仍只能只读审计，WORM Eval 对象不在范围内；删除执行与 staging 删除/恢复证据仍是上线前置条件，见 `docs/data_retention_worker_runbook.md`。
- P8.5 第三增量在 `internal/module/agent/extension/` 与 `service/agent_extension_catalog.go` 增加 `agent.extension.v1` 租户已安装目录。它只聚合不可变 Capability、精确版本 Skill 和 `ListGovernedTools` 已过滤的 MCP Tool，使用有界过滤、确定性排序与过滤条件绑定 Cursor；不返回 Endpoint、Credential、输入 Schema 或 Tool Result，也不提供第二条执行路径。`GET /agent/extensions` 只信任 Gateway JWT 租户；Web `AgentExtensionCatalogDialog.vue` 对 Skill 做精确版本解析，对 MCP 只跳转既有管理面。`AGENT_EXTENSION_CATALOG_ENABLED` 默认关闭且不改任何来源数据。
- P8.5 第四、五增量在 `internal/module/agent/marketplace/`、`repository/extension_marketplace.go`、`service/agent_extension_marketplace.go` 与 `service/extension_marketplace_manager.go` 建立签名公共目录及独立发布控制面。公开读取仍经只读 Store 逐条复验发布者、密钥、Manifest、Release ID 和 Ed25519 签名；管理写入由专属内部令牌保护，JWT Actor 在 Agent Service 端按平台管理员或 Mongo 中不可变 Owner 重新授权。系统只保存公钥；Active Key 可发布，轮换将旧 Key 置为 Retired，Revoked Key 使历史签名失效。发布者与版本使用 Revision CAS，版本只能从 Published 终态撤回，所有写操作追加 requested/succeeded/failed 低敏审计。公开目录与管理面分别由 `AGENT_EXTENSION_MARKETPLACE_ENABLED`、`AGENT_EXTENSION_MARKETPLACE_ADMIN_ENABLED` 默认关闭；`GET /agent/marketplace/extensions` 仍无密钥/Artifact URL/安装授权，`/agent/marketplace/manage/*` 与 `ExtensionMarketplaceAdmin.vue` 只治理元数据和离线签名。Artifact 分发、扫描、依赖解析、安装审批、Owner 转移和租户安装尚未实现，不能称为可安装开放市场。
- P8.4 第五、六增量的 Tool Continuation 位于 `runtime/types.go`、`runtime/checkpoint.go`、`runtime/runner.go`、`service/agent_execution_*`、`service/workflow_as_tool.go` 与 `web/src/components/agent/ApprovalInbox.vue`。父 Agent 将子 Run、父 Action、发布 Revision/DSL Hash 和版本化 Continuation 放入加密 Checkpoint；人工输入恢复携带子恢复凭证，审批恢复只保存子审批引用。子 Workflow 独占审批事实与一次性 Grant，审批中心将子 Grant 交给父 Agent Resume，父 Runtime 在同一 Tool Action 内恢复子 Run，不创建第二条审批或父级令牌。恢复重新校验租户、谱系、Action、Revision/Hash、审批摘要/幂等键与 Grant；子 Run 已成功而父提交中断时从权威 Blackboard 回放确定性输出。拒绝或过期同步终止子 Run 与委托父 Run。
- P8.4 第七至十七、二十增量的 Multi-Agent 准入、执行与评测位于 `internal/module/agent/strategy/`、`internal/module/agent/multirole/`、`service/multi_agent_strategy.go`、`service/multi_agent_execution.go`、`eval/agent_strategy_gate.go`、`eval/agent_task_evidence.go` 和 `cmd/agent-task-eval/{strategy_runtime_executor,runtime_executor,live_run,checkpoint,review_bundle}.go`。Strategy 是无 I/O 准入领域层；Multirole 是不依赖 Service、Mongo 或 MCP SDK 的顺序聚合核心，生产 Service 与 Eval 共用同一角色隔离、交接、父预算和失败语义。研究父 Profile 是原子 Profile Set 的唯一灰度 Anchor，计划固定 Anchor/Version，执行从单个 Catalog 快照解析同版角色并在模型调用前拒绝混合版本。`AGENT_MULTI_AGENT_PLANNER_ENABLED` 与 `AGENT_MULTI_AGENT_EXECUTION_ENABLED` 分别控制准入和真实执行。首个执行器只支持两个只读研究草拟模板及顺序 `researcher -> drafter -> reviewer`；执行关闭或准入不满足时回退原单 Agent，角色执行后失败不隐藏重跑。ReAct 最后一步移除工具目录并追加高优先级收束指令，Provider 若仍返回非终态动作会在工具执行前失败关闭。`agent-task-eval --strategy-runtime-config` 在同一 Provider/Model/Pricing/Profile Snapshot 和配置哈希中运行真实模型单/多对照，配置哈希不同直接 `ineligible`；Live 预检真实 Chat/Tool Call，逐 Case 检查点仅保存脱敏指标重建证据并用 HMAC 与前向哈希链防篡改，Provider 错误不会固化失败 Case。复核材料由 CLI 层独立 AES-256-GCM Bundle 承载，绑定签名报告并禁止与 Checkpoint/归档同跑，不改变 Eval 核心脱敏报告；显式失败诊断 Bundle 不能创建 Signoff。第十七增量为 `AgentTaskCase` 增加可选、仅属于数据集的 v3 Evidence Contract；报告 schema 保持 `agent-task-eval-report/v2`。独立 `agent_strategy_cases_v3.json` 提供 16 条实质证据与 4 条合法证据不足任务，声明 Claim/Terms/Evidence ID、精确方括号 Citation、拒答/无依据声明/内部元数据规则；沙箱按现有 `platform.tweet_search.v1`、`web.search.v1`、`web.page.v1` 投影，不新增生产 Tool Schema，执行身份升级为 v5。第二十增量的固定 qwen3.7/Profile v5 云对照为 Candidate `19/20`、Stable `15/20`，自动双门禁和报告/Bundle 复验通过；确定性短语/邻近度仍不是完整事实裁判，外部人工 Signoff 与 WORM 资格未完成，因此 Feature Flag 继续关闭。并行角色、角色挂起/审批恢复、写工具和任意拓扑仍未开放，旧 Multi API 不参与新路径。
- P8.4 第十八、十九增量的内容资格链位于 `eval/agent_task_content_review.go`、`eval/agent_task_content_qualified_evidence.go`、`cmd/agent-task-eval/{content_review_signoff,report_archive}.go` 与 `objectstore/agent_task_quality_evidence.go`。Decision/Signoff 逐 Case 绑定报告、Review Bundle 和四维人工结论；`agent-task-content-qualified-evidence/v1` 将已验签报告与外部人工批准 Signoff 作为同一个 Object Lock 对象。归档前 CLI 必须用三把独立密钥重新验报告、解密 Bundle 和验 Signoff；Profile 严格模式仅持有报告/Signoff HMAC Key，不解密正文。`AGENT_PROFILE_EVAL_CONTENT_SIGNOFF_REQUIRED` 默认关闭且依赖旧证据开关，关闭可兼容裸报告回执；现有回执/API 不迁移。v3 云自动报告与加密 Bundle 已存在并通过双门禁，但仍无真实外部人工记录或 WORM Version ID。
- P8.4 第二十一至二十五增量在 `cmd/agent-task-eval/{live_plan,live_authorization,live_authorization_redis,live_authorization_redis_admin,live_authorization_redis_audit,review_decision_template}.go` 与 `eval/agent_task_output.go` 增加 Live 离线资源计划和授权预算闭环。计划固定 Provider/精确 Model、数据集/配置哈希并汇总调用、Token、费用与正文上界；签名授权、报告签名和 Review 加密密钥必须独立，授权必须覆盖完整调用/费用计划。默认 file 后端在运行和每次 Provider 调用前消耗本地 append-only HMAC 账本；可选 Redis 后端要求先用独立命令显式初始化，再由 Lua 按 Redis 服务端时间原子校验和累计四类额度、Stream 审计及 Reservation 去重，不同连接共享同一预算。无 TTL marker 发现 State/Stream 丢失后要求撤销授权，运行路径不会重新初始化；管理检查固定原子末端 Stream ID，分页重放精确事件、对账状态并输出可外部留存的稳定 SHA-256。签名报告可选携带授权 ID/摘要/预算证据；Redis 额外绑定后端和 Namespace SHA，历史报告形状不变。Stream 摘要不是 HMAC；Redis ACL/TLS/AOF/备份、`noeviction`、外部 WORM 和管理员权限仍属于部署信任边界。
- Profile 版本选择独立于 Runtime/Provider Rollout：内置、Mongo 已发布版本和 `AGENT_PROFILE_RELEASES` 分层构造不可变 Catalog，使用用户稳定分桶；`AtomicResolver` 只在完整校验后替换快照。协作角色通过父 Profile Release 原子选版，角色 Profile 不接受独立 Release；计划摘要固定 Profile Set Anchor/Version，运行时从单一快照精确解析，避免跨刷新混版。管理面已有项目级 RBAC、双人发布审批、管理 UI、append-only 脱敏审计和跨实例刷新。可选 Profile 实验控制器绑定 Release revision，采集脱敏 Run 可靠性指标与三类固定业务结果；Assist 显式确认发布通过窄应用 Port 校验来源 Run，以稳定幂等键调用 TweetService，并短期保存不含正文的 Tweet/Run 映射。真实发布可记录 `draft_published=true`，归因窗口内首个外部点赞/评论可经事务 Outbox、Canal 和 Agent 独立重试/DLQ 队列记录 `content_engaged=true`；自赞、超时和未操作不推断负样本。专用运维命令可限量检查/重放内容互动 DLQ，只有 Broker Confirm 后才 ACK，毒消息和超限消息继续保留。运行与业务护栏回归时按 CAS 自动回滚，但不会自动提升候选。`agent-task-eval` 已提供 52 条固定任务、受控 Live Runtime Adapter、签名质量门禁和 Object Lock COMPLIANCE 私有归档，Profile 发布会按精确对象版本复验回执；运行热路径不访问对象存储。录制夹具不等于真实模型/MCP 基线。Agent Service 是授权和来源运行校验事实源，Gateway 只传递 JWT 身份并做契约转换。外部组织目录、真实 Profile Eval 基线、明确拒绝动作和受控线上样本仍未完成。
- Temporal 当前只注册风险控制与热点播报；重复实现用户 DAG 语义的实验 bridge 已删除，不要宣称用户自定义 DAG 由 Temporal 托管。

- P8.4 第三增量的显式任务模板位于 `repository/agent_task_template.go`、`service/agent_task_template.go` 和统一 `RunAgent` 路径。`agent_task_templates` 是独立于 Workflow DAG 的不可变单输入预设：用户只能从自己的 completed 权威 Run 创建，自行提供含唯一 `{{input}}` 的指令；服务端不复制会话或输出正文，只保存源 Run Revision/结果摘要、Profile、Capability 和 Skill/Profile/Prompt 版本证据。执行前重新校验模板、源 Run 和当前路由，新 Run 记录模板 ID/Revision；模型和用户 Provider Config 保持当次选择。`AGENT_TASK_TEMPLATES_ENABLED` 默认关闭且依赖 Recoverable Runs，关闭执行仍允许列表与 CAS 归档。
- P8.4 第四增量的父子核算位于 `repository/agent_run_accounting.go`、`service/agent_run_accounting.go`、权威 Agent/Workflow Run 模型及 `GET /agent/runs/:id/accounting`。父 Run 与直接子 Workflow Run 写入 `execution.accounting.v1` 的预算上限、Step/Node、Token、估算成本和 Pricing Version；旧记录不解析业务 JSON 或 Trace。查询按 `user_id + parent_run_id + started_at + _id` 索引有界读取，返回父级、子级和总计并用 `complete/partial/unavailable` 暴露证据完整性。该投影不递归、不保存正文，也不改变父子独立准入预算。

## 6. 数据与中间件归属

| 组件 | 当前用途 | 主要访问方 |
|------|----------|------------|
| MySQL | 社交领域主数据、Tweet 发布幂等绑定，以及 Agent 自有 Persona/配置数据；风控 Activity 不直连社交表 | User/Tweet/Follow/Messenger/Notification、Agent Repository |
| MongoDB | Agent Dialogue、Message、Workflow Revision/DAG Run/StateEvent/Snapshot、Workflow Tool Publication、Unified Agent Execution Run、Agent Project/成员、Run/Step/LLM/Tool Trace、Tool Approval/Execution Result、Summary Cursor/Lease、Profile 版本/审批/项目角色绑定/实验运行与业务结果观测/审计、外部 MCP Connection/Schema Snapshot | Agent Repository |
| Redis | 缓存、Timeline ZSet、实时 Pub/Sub、幂等/配置、Agent Run 有界事件投递 | Gateway 与多数服务、Agent |
| RabbitMQ | 推文、Timeline、通知、风险控制等异步事件；Timeline 用 `timeline.retry/timeline.ingress` 隔离恢复投递，Agent 风控与 Profile 内容互动使用自有 Retry/DLQ，均采用 Confirm-before-ACK | Producer、Consumer、Agent |
| Elasticsearch | 推文 BM25/全文检索 | Consumer、Agent RAG/MCP |
| Qdrant | 推文向量与共享 `agent_episodic_memory` Episodic Memory；共享集合必须使用 `user_id` Payload Filter | Consumer、Agent RAG、`cmd/agent-memory-migrate` |
| Consul | 服务注册发现与部分动态配置 | Gateway、gRPC 服务 |
| Temporal | Agent 风控和热点播报的可选持久工作流 | Agent Service |
| MinIO | 公共媒体、私有大型 Tool Result、私有 Agent Eval Object Lock 报告使用不同 Bucket/访问/保留语义 | Gateway 上传链路、Agent Service、`agent-task-eval` 运维命令 |

## 7. 前端地图

### Web

- 技术栈：Vue 3、TypeScript、Vite、Pinia、Axios、Vue Flow。
- 路由：`web/src/router/index.ts`。
- API 封装：`web/src/api/`；状态：`web/src/stores/`；共享组件：`web/src/components/`。
- 业务页面：`web/src/views/`。
- AI 助手：`web/src/views/Agent.vue`。
- 全站只读客服：根 `web/src/App.vue` 挂载 `components/SupportWidget.vue`，懒加载独立 Python 客服前端 iframe（本机 5174，`VITE_SUPPORT_ASSISTANT_URL` 可配置）。复用当前用户账号，通过精确 Origin/source 的内存消息桥传身份；查询仍由独立 support-agent-service 的统一 Chat API 处理，不进入原 Go Agent 路由。推文引用导航 `/tweets/:id`。
- 外部连接与项目成员：`web/src/components/agent/ExternalMCPDialog.vue`、`AgentProjectDialog.vue`；项目作用域关闭或 API 不可用时个人连接表单保持可用，托管凭据只输入部署下发的引用，不录入或回显 Secret。
- 工具审批收件箱：`web/src/components/agent/ApprovalInbox.vue`；批准后查询最新 Run、签发短期一次性授权并立即恢复，不持久化明文授权，支持跨浏览器/跨设备处理。
- 工作流画布：`web/src/views/agent/WorkflowEditor.vue`，节点属性在 `web/src/components/agent/NodePropertiesDrawer.vue`；已保存 Workflow 可显式发布/停用不可变只读 Revision 供 Unified Agent 调用。
- 工作流运行追踪：`internal/module/agent/observability/` 定义存储无关契约、安全预览采样与模板身份字段，`repository/execution_trace.go` 持久化完整查询记录，`repository/execution_event_stream.go` 通过 Redis Stream 有界投递增量事件；Workflow Editor 分别展示节点、模型和工具调用，并按游标断线恢复。
- 工作流状态检索：`internal/module/agent/service/workflow_blackboard.go` 从持久快照和范围事件构建版本稳定、脱敏的字段页；Workflow Editor 运行控制台支持版本、路径、关键词和游标查询。
- 工作流只读回放：`internal/module/agent/service/workflow_replay.go` 装配并校验证据，Workflow Editor 的运行回放对话框消费 Gateway 结构化响应。
- 工作流 DSL 必须与 `internal/module/agent/workflow/dsl/` 和 gRPC/API 契约同步。

### Mobile

- 技术栈：Flutter、Riverpod、Dio、go_router。
- 功能代码：`mobile/lib/features/`；共享能力：`mobile/lib/core/`。
- Web 与 Mobile 并非自动同步；改用户可见行为时分别确认影响范围。

## 8. 任务定位表

| 任务 | 首读文件/目录 |
|------|---------------|
| 新增/修改 HTTP API | `internal/gateway/router/`、`handler/`、`client/`、对应 `api/*/v1/*.proto` |
| Python 客服内部 Support Query | `internal/module/supportquery/`、`internal/gateway/handler/support_query.go`、`middleware/support_query_auth.go`、`router/support_query.go`、`cmd/gateway/main.go` |
| 修改 gRPC 契约 | 对应 `.proto`、生成脚本/Makefile、模块 `grpc/`、Gateway client |
| Tweet/Timeline | `internal/module/tweet/`、`internal/mq/`、`cmd/consumer/` |
| Follow事实/事件/派生状态 | `service/follow_outbox.go`、`repository/follow_mutation.go`、`projection/`、`topology.go`、`cmd/follow-service/consumer/follow-projection-reconcile`；当前派生/启用/恢复边界见 `docs/FOLLOW_PROJECTION_RECOVERY_RUNBOOK.md`，事实事务见R2 Runbook |
| 登录/JWT | `internal/module/auth/`、`internal/gateway/middleware/` |
| Agent 统一入口/兼容模式 | `internal/module/agent/grpc/`、`internal/module/agent/service/unified_agent.go`、`internal/module/agent/service/agent_service.go`、`internal/module/agent/service/agent_profiles.go` |
| Agent Runtime | `.agents/context/agent_runtime_context.md`、`internal/module/agent/runtime/`、`model/`、`message/`、`profile/` |
| Multi-Agent 准入/执行/对照评测 | `internal/module/agent/strategy/`、`internal/module/agent/multirole/`、`internal/module/agent/service/multi_agent_strategy.go`、`service/multi_agent_execution.go`、`service/unified_agent.go`、`internal/module/agent/eval/agent_strategy_gate.go`、`internal/module/agent/eval/agent_task_content_review.go`、`cmd/agent-task-eval/strategy_runtime_executor.go`、`cmd/agent-task-eval/content_review_signoff.go` |
| Unified Agent Run 状态/恢复 | `internal/module/agent/repository/agent_execution_run.go`、`internal/module/agent/service/agent_execution_run.go`、`service/unified_agent.go`、`docs/adr/ADR-004-authoritative-agent-run-lifecycle.md` |
| 自定义工作流 | `internal/module/agent/workflow/dsl/`、`ir/`、`engine/`、`tool/`、`internal/module/agent/service/workflow_service.go`、Workflow Editor |
| Workflow-as-Tool | `internal/module/agent/repository/workflow_tool_publication.go`、`internal/module/agent/service/workflow_as_tool.go`、`service/unified_agent*.go`、`api/aiAgent/v1/aiAgent.proto`、Workflow Editor |
| 版本化 Skill 目录/执行 | `internal/module/agent/skill/`、`internal/module/agent/service/workflow_skill_catalog.go`、`service/unified_agent*.go`、`repository/agent_execution_run.go`、`api/aiAgent/v1/aiAgent.proto`、`internal/gateway/handler/agent_handler.go`、`web/src/views/Agent.vue` |
| 租户已安装扩展目录 | `internal/module/agent/extension/`、`internal/module/agent/service/agent_extension_catalog.go`、`internal/module/agent/mcp/remote/`、`api/aiAgent/v1/aiAgent.proto`、`internal/module/agent/grpc/agent_server.go`、`internal/gateway/handler/agent_handler.go`、`web/src/components/agent/AgentExtensionCatalogDialog.vue` |
| 签名公共扩展目录 | `internal/module/agent/marketplace/`、`internal/module/agent/repository/extension_marketplace.go`、`internal/module/agent/service/agent_extension_marketplace.go`、`api/aiAgent/v1/aiAgent.proto`、`internal/module/agent/grpc/agent_server.go`、`internal/gateway/handler/agent_handler.go`、`web/src/components/agent/AgentExtensionMarketplaceDialog.vue` |
| 显式任务模板 | `internal/module/agent/repository/agent_task_template.go`、`service/agent_task_template.go`、`service/unified_agent.go`、`api/aiAgent/v1/aiAgent.proto`、`internal/gateway/handler/agent_handler.go`、`web/src/api/agent.ts`、`web/src/views/Agent.vue` |
| 父子 Run 预算/成本核算 | `internal/module/agent/repository/agent_run_accounting.go`、`repository/agent_execution_run.go`、`repository/dialogue_model.go`、`service/agent_run_accounting.go`、`service/workflow_budget.go`、`api/aiAgent/v1/aiAgent.proto`、`internal/gateway/handler/agent_handler.go`、`web/src/views/Agent.vue` |
| Unified Agent 产品漏斗/归因 | `internal/module/agent/product/`、`internal/module/agent/repository/product_event.go`、`repository/agent_execution_run.go`、`service/product_events.go`、`service/unified_agent_product_metrics.go`、`internal/module/agent/mcp/remote/metrics.go`、`cmd/agent-service` |
| RAG/Memory | `internal/module/agent/workflow/rag/`、`internal/module/agent/service/cognitive_context.go`、`session_summary.go`、`internal/module/agent/eval/`、`pkg/es`、`pkg/qdrant`、`cmd/agent-memory-migrate`、`cmd/agent-rag-eval`、`cmd/agent-router-eval` |
| MCP 工具/外部连接/项目共享/托管凭据/生产验收 | `internal/module/agent/mcp/`、`internal/module/agent/mcp/remote/`、`internal/module/agent/mcp/acceptance/`、`internal/module/agent/project/`、`internal/module/agent/repository/agent_project.go`、`internal/module/agent/workflow/tool/`、`cmd/agent-service`、`cmd/agent-mcp-*` |
| 公网搜索/Page Read | `internal/module/agent/websearch/`、`internal/module/agent/evidence/`、`internal/module/agent/model/endpoint_policy.go`、MCP/Workflow 注册与 Capability Catalog |
| 模型/自定义 API | `internal/module/agent/model/`、`internal/module/agent/service/model_selection.go`、`internal/module/agent/workflow/tool/credentials.go` |
| Agent 用户删除请求/协议 | `internal/module/agent/deletion/`、`internal/module/agent/repository/data_deletion_request.go`、`internal/module/agent/service/data_deletion_request.go`、`cmd/agent-data-deletion-request`、`docs/data_retention_*` |
| Web UI | `web/src/views/` -> `components/` -> `stores/` -> `api/` |
| 部署/端口/观测/上线验收 | `docker-compose*.yaml`、`deploy/`、`scripts/validate-production-compose.ps1`、`scripts/production-backup-manifest.ps1`、`scripts/production-acceptance-record.ps1`、`deploy/acceptance/production-launch-checks.json`、`docs/production_*`、各 `cmd/*/main.go`、`.env.example` |
| 架构评审/技术债 | 本文件、`architecture_audit_report.md`、`.agents/context/technical_debt.md`、强化计划 |

## 9. 生成文件与噪声边界

- `api/*/v1/*.pb.go`、`*_grpc.pb.go` 是生成文件；修改 `.proto` 后重新生成，禁止手工维护生成内容。
- 根目录 `*.exe`、`bin/`、`web/dist/`、Go build cache、上传文件不是架构事实，分析时默认忽略。
- 工作区允许存在用户未提交修改；禁止为了“干净”而回滚不属于当前任务的文件。
- 搜索优先使用 `rg`/`rg --files`，先按任务定位表限定目录，避免每次全仓扫描。

## 10. 验证入口

```powershell
# Go Agent（受限环境使用工作区缓存）
$env:GOCACHE='E:\GOProject\cloud\twitter-clone\tmp\go-build-cache'
go test ./... -count=1
go test -race ./internal/module/agent/eval ./pkg/qdrant ./internal/module/agent/service ./internal/module/agent/grpc ./internal/gateway/handler ./internal/gateway/router -count=1
go vet ./...
go run ./cmd/agent-router-eval --out tmp/router-eval/report.json # 纯离线词典/默认层基线，不连接模型
go run ./cmd/agent-task-eval --stable-results internal/module/agent/eval/testdata/agent_task_recorded_results.json --enforce-gate --out tmp/agent-task-eval/report.json # 固定录制结果的评分/门禁契约验证，不代表真实模型质量
go run ./cmd/agent-task-eval --dataset internal/module/agent/eval/testdata/agent_strategy_cases.json --dataset-version agent-strategy-cases-v2 --results internal/module/agent/eval/testdata/agent_strategy_multi_results.json --stable-results internal/module/agent/eval/testdata/agent_strategy_single_results.json --min-cases 20 --enforce-gate --strategy-gate --enforce-strategy-gate --out tmp/agent-task-eval/strategy-comparison.json # 单/多质量成本P95门禁契约自测，不代表生产性能
go run ./cmd/agent-task-eval --plan-live-evaluation tmp/agent-task-eval/qwen37-v5.live-plan.json --dataset internal/module/agent/eval/testdata/agent_strategy_cases_v3.json --dataset-version agent-strategy-cases-v3 --strategy-runtime-config internal/module/agent/eval/testdata/agent_strategy_runtime_config.qwen37-v5.example.json # 纯离线计划；不读取 API Key、不调用 Provider，模型或 Profile 改变后必须重算
go run ./cmd/agent-task-eval --initialize-live-authorization-state --live-authorization tmp/agent-task-eval/live-strategy.authorization.json --live-authorization-state-backend redis --live-authorization-redis-config internal/module/agent/eval/testdata/agent_task_live_redis_config.example.json --live-authorization-key-id live-authorization-v1 # Redis 多实例账本显式初始化；只验签授权并连接 Redis，不构造模型 Provider
go run ./cmd/agent-task-eval --inspect-live-authorization-state --live-authorization tmp/agent-task-eval/live-strategy.authorization.json --live-authorization-state-backend redis --live-authorization-redis-config internal/module/agent/eval/testdata/agent_task_live_redis_config.example.json --live-authorization-key-id live-authorization-v1 # 脱敏检查共享额度并逐事件重放到原子 Stream 边界；不读取模型 Credential
go run ./cmd/agent-task-eval --allow-live --runtime-config internal/module/agent/eval/testdata/agent_task_runtime_config.example.json --live-authorization tmp/agent-task-eval/live-baseline.authorization.json --live-authorization-state tmp/agent-task-eval/live-authorization-state --out tmp/agent-task-eval/live-baseline.json # 真实模型调用还必须先离线签发匹配授权并配置独立授权/报告 HMAC；工具仍为无副作用沙箱
go run ./cmd/agent-task-eval --dataset internal/module/agent/eval/testdata/agent_strategy_cases.json --dataset-version agent-strategy-cases-v2 --allow-live --strategy-runtime-config internal/module/agent/eval/testdata/agent_strategy_runtime_config.example.json --live-authorization tmp/agent-task-eval/live-strategy.authorization.json --live-authorization-state tmp/agent-task-eval/live-authorization-state --preflight-timeout 20s --case-timeout 90s --timeout 45m --checkpoint-dir tmp/agent-task-eval/live-strategy-checkpoint --progress --strategy-gate --enforce-strategy-gate --out tmp/agent-task-eval/live-strategy-comparison.json # 同配置真实模型单/多对照；授权须为该数据集/配置新签且 State Root 不得切换
go run ./cmd/agent-task-eval --dataset internal/module/agent/eval/testdata/agent_strategy_cases.json --dataset-version agent-strategy-cases-v2 --allow-live --strategy-runtime-config internal/module/agent/eval/testdata/agent_strategy_runtime_config.example.json --live-authorization tmp/agent-task-eval/live-strategy-review.authorization.json --live-authorization-state tmp/agent-task-eval/live-authorization-state --case-timeout 90s --timeout 45m --enforce-gate --enforce-strategy-gate --allow-review-content --review-bundle tmp/agent-task-eval/live-strategy.review.enc.json --review-key-id review-key-v1 --out tmp/agent-task-eval/live-strategy.review-report.json # 付费重跑且捕获正文，授权必须预留完整正文数量；禁止 checkpoint/archive
go run ./cmd/agent-task-eval --verify-review-signoff tmp/agent-task-eval/v3.content-signoff.json --review-report tmp/agent-task-eval/v3.report.json --review-bundle-input tmp/agent-task-eval/v3.review.enc.json --allow-review-content --review-signoff-key-id content-signoff-v1 # 离线复验报告、Bundle 与签认；三类密钥均只由环境变量注入，Judge 不能冒充人工批准
go run ./cmd/agent-task-eval --archive-report tmp/agent-task-eval/live-baseline.json --archive-config internal/module/agent/eval/testdata/agent_task_archive_config.example.json --archive-receipt tmp/agent-task-eval/live-baseline.archive-receipt.json # 要求专用私有 MinIO Bucket 启用 Versioning 与 Object Lock COMPLIANCE
# 将 archive receipt 粘贴到 /agent/profiles 发布申请；AGENT_PROFILE_EVAL_EVIDENCE_REQUIRED=true 时申请/批准/重试都会按精确对象版本复验
go run ./cmd/agent-rag-eval --allow-live --dataset internal/module/agent/eval/testdata/rag_cases.json --out tmp/rag-eval/report.json # 仅在依赖服务已显式启动且用户明确允许 live 时运行
# 终端一常驻运行本地回环夹具；终端二显式执行一次验收。真实第三方需使用独立无凭据配置和受控 Secret。
go run ./cmd/agent-mcp-conformance
go run ./cmd/agent-mcp-acceptance --allow-live --allow-write --config internal/module/agent/mcp/acceptance/testdata/conformance_config.example.json --out tmp/agent-mcp-acceptance/report.json

# Web
Set-Location web
npm run build

# Flutter
Set-Location mobile
flutter analyze
flutter test
```

测试范围应按改动扩展：窄改动先跑目标包，共享契约/Runner/Repository 改动再跑 Agent 全包与 `-race`。

## 11. 维护触发器

出现以下任一变化时，同一提交必须更新本文件：

- 新增、删除或重命名 `cmd/` 服务、核心模块、存储或消息通道。
- API、Agent Runtime、Workflow、RAG 的依赖方向发生变化。
- 端口、主执行路径、灰度开关或持久化归属改变。
- “任务定位表”指向的入口失效。

不要把每日进度、具体 Bug、完整文件清单写进本文件；这些内容分别进入 `docs/PROJECT_PROGRESS.md` 和 `docs/ISSUES.md`。

## 12. Agent Goal Runtime 定位补充（2026-08-10）

- Environment/Goal Contract：`internal/module/agent/environment/`、`internal/module/agent/runtime/goal.go`、`internal/module/agent/evidence/`。
- Tweet Write 事实源：`internal/module/agent/service/tweet_write_environment.go` 只读调用 TweetService Timeline；写动作仍唯一经过 MCP `create_tweet` 与统一 ToolExecutor。
- 结构化发布证据：`internal/module/agent/mcp/tools/create_tweet.go` -> Runtime Observation -> `evidence.TweetPublishGoalVerifier`；禁止从 FinalAnswer 或 Tool 文本解析完成状态。
- 当前依赖方向：Service Adapter -> Environment/Runtime Contract；MCP Tool -> Evidence DTO；Environment 不依赖 Executor，Evidence 不执行 Tool。
- G3/G4 短期规划与只读生产迁移：`short_plan.go` 定义无授权字段的短计划与确定性准入，`short_plan_model.go`/`short_plan_coordinator.go` 负责严格模型提案、完整记账和一次 Admission 修复；`planned_verified_runner.go` 绑定 opt-in VerifiedRunner，`planned_verified_recovery.go` 仅把 `tool_error`/缺失证据映射为一次固定脱敏恢复。`service/planning_execute.go` 以默认关闭、稳定用户灰度接管 `runtime.platform_search`、`runtime.web_search` 与 `runtime.external_mcp` 只读路径，分别复用对应 Collector/Verifier；Web 只有同时观测可信搜索来源和其绑定页面正文才算完成，MCP 只有当前租户授权目录稳定且存在成对成功只读 Observation 才算完成。权威 Run 只保存计划摘要、规划核算、目标/验证状态和 Verified Checkpoint 身份。人工澄清暂停使用加密 `goal.v1` Checkpoint，恢复时重新读取当前 Catalog 并求交授权工具；MCP 恢复额外重验当前绑定，Workflow/写工具仍走既有路径。
- E2E-12 冲突边界：`evidence/evidence_conflict_goal.go` 只接受与允许的只读 Tool Action 配对的 `agent.evidence_assertions.v1`；同一 Claim 的不同 canonical value 必须来自不同公网引用。`VerifiedRunner` 仅对显式实现 `SuspendedRunVerifier` 的领域 Verifier 验证挂起，其余审批/人工等待保持原有 inconclusive checkpoint 语义。合法冲突必须在证据之后发出精确配置的 `ask_human`，静默选边、提前/无关提问和伪造 Ledger 均 blocked。
- E2E-13/14 写入边界：内置 `create_tweet` 的模型可见 Schema 只暴露正文，`user_id` 与稳定幂等键由 Service 在 ToolExecutor Schema 校验前可信注入并在 MCP 调用边界再次覆盖。受控进程内夹具已串联 ReAct 审批 checkpoint/resume、真实 MCP Handler、ToolExecutor 幂等结果恢复、Timeline Before/After 与领域 Verifier；部署态 TweetService/Mongo 审批事实仍需单独显式验收。
- E2E-15/16 MCP 边界：External MCP Environment 的低敏快照绑定认证 Actor Digest、当前 Connection Revision 与不可变 Schema/Policy Digest；只读 Goal 仅接受成对成功 Observation。写工具先经 ToolExecutor 产生审批审计，Resume 必须重新读取当前目录，连接或项目访问撤销后在远程 Caller 前 fail-closed。受控夹具不连接真实外部 MCP，也不启用生产 Goal execution。
- E2E-17 Workflow-as-Tool 边界：Workflow Environment Snapshot 绑定认证 Actor Digest 与不可变 Publication/Workflow Revision 摘要；Tool Structured Content 显式携带发布 Revision、Workflow Revision/DSL Hash、父 Run/Action、child Run、声明响应摘要与权威 OutputJSON 摘要。Collector/Verifier 通过窄只读 Resolver 重新读取用户隔离的 child Run，失败 child 只向父 Runtime 传播固定 Tool 错误且不得生成完成证据。受控夹具使用内存 Publication/Run Store，不启用生产 Goal execution。
- E2E-19 Checkpoint/Resume 边界：VerifiedCheckpoint 使用从 1 开始的单调 Revision；每次合法 Resume 追加仅含摘要/引用的 checkpoint_resume Evidence。挂起边界先读取当前 After Snapshot 并收集已完成 Action 证据，使审批恢复后已经执行的写操作在后续 ask_human Checkpoint 中保持可验证；再次恢复只从待处理 Step 继续，不重放已成功写入。受控夹具用 JSON 往返模拟持久化，使用内存 Approval/Idempotency Store，不代表真实 Mongo、多副本或进程重启验收。
- E2E-20 Provider Outage 边界：Model Router 只沿 Catalog 显式、能力兼容的 Fallback 图遍历；固定低敏错误分类决定 `fallback_allowed`、`fallback_denied` 或 `fallback_exhausted`，认证/永久请求错误不得调用后备 Provider。Runtime 只保存模型/Provider 标识、固定失败码和路由决策，不保存错误正文或凭据；VerifiedRunner 仅把结构完整且没有 Selected Model 的终止轨迹投影为 `blocked` 和摘要化 Provider Routing Evidence，不生成 FinalAnswer 或 Artifact。受控夹具使用内存 Provider Client，不连接真实 Provider，也不启用生产 Goal execution。

## 13. Production Compose 定位补充（2026-09-24）

- `docker-compose.production.yaml` 是单机演示/小规模 Beta 的生产覆盖；31 个服务全部要求不可变镜像摘要，只有 `edge-proxy` 发布宿主机 `80/443`，Gateway、Frontend 和基础设施保持内部网络可达。
- `deploy/nginx/production.conf.template` 负责 TLS、HTTP 跳转、页面/API/WebSocket 反代与基础安全头；证书、私钥和 Alertmanager Token 由仓库外文件映射为 Compose Secret。
- `scripts/validate-production-compose.ps1` 是启动前 fail-closed 门禁，校验镜像、Secret、证书/私钥配对、公开 HTTPS URL、Origin/JWT、资源/健康检查和 Agent 开关依赖；它只解析配置，不启动服务。
- `deploy/prometheus/rules/twitter-clone-production.yml`、`deploy/alertmanager/`、`deploy/loki/` 与 `deploy/promtail/` 承担生产观测基线；备份完整性入口为 `scripts/production-backup-manifest.ps1`。静态门禁不等于 clean-host、真实恢复、告警投递或 Provider/MCP Live Acceptance。
- `deploy/acceptance/production-launch-checks.json` 固定 L3/L4/L5 的 26 项 Required Check；`scripts/production-acceptance-record.ps1` 只生成/更新/复验仓库外 HMAC 签名记录及证据 SHA-256，不能执行 Live Check。`scripts/test-production-acceptance-record.ps1` 是无外部依赖的门禁自测。

## 14. 用户删除跨存储边界（2026-09-26）

- Agent/Tweet 复合围栏位于 `internal/module/agent/service/data_deletion_composite_write_fence.go`，Tweet RPC 客户端位于 `internal/module/agent/deletion/tweet_grpc_client.go`；返回的 v2 回执包含两项权威的确定性摘要。它们尚未注册到生产 Worker。
- `internal/module/agent/repository/data_deletion_write_fence.go` 的用户主体 `active_writes` 计数与围栏安装原子互斥；`agent_repo.go` 的对话创建/单条和批量消息写入先登记再写，结果不确定时保留阻断。其他 Agent 写路径、遗留计数对账和真实 Mongo 并发测试仍待完成。
- Agent 用户写入口通过 `AgentService.authorizeSubjectWrite` 调用 Mongo 仓储授权；对话创建与消息持久化额外使用主体计数，其余直接仓储/后台写入及检查到提交之间的竞态仍需治理。不得据此打开生产删除。

- `internal/module/agent/objectstore/minio_tool_results.go` 提供当前格式 Tool Result 的用户前缀计数和删除原语；范围严格固定为 `tool-results/{tenantHash}/`，对象列表出现跨租户或畸形 Key 时在删除前 fail-closed。旧格式对象不在此范围，必须经独立审计迁移。
- `pkg/qdrant/qdrant.go` 提供要求非空 Filter 的精确计数与 `wait=true` Filter 删除原语；共享 Episodic Memory 的生产 Adapter 只能使用字符串 `user_id` 等值过滤，禁止空 Filter 或删除整个 Collection。
- `internal/module/agent/deletion/tweet_service_contract.go` 固定 Tweet Service 用户级删除端口，覆盖 authored tweets/comments、likes、bookmarks、retweets 与 poll votes。Tweet Service 通过每用户固定门闩行串行化门闩安装和六类主体写入；物理删除必须在同一事务确认门闩已存在。现有逐条 `DeleteTweet` 不能充当该端口。
- `internal/module/agent/deletion/cross_store.go` 只接受共享关系、MinIO、Qdrant、Tweet Service 与 Mongo owned-data 五个权威 Post-state，要求 Capability Snapshot 精确绑定、每项唯一且残留总数为零；报告不保存原始用户 ID。
- `internal/module/agent/repository/data_deletion_relationship.go` 固定安全解绑清单：非 Owner 项目成员、个人 MCP Connection/Snapshot 与 Profile Role Binding 在同一 Mongo 事务内清理并固化幂等回执；项目 Owner、项目级 MCP 创建者或 Marketplace Publisher Owner 任一存在都会在写前 fail-closed。
- `internal/module/agent/service/data_deletion_worker.go` 是未注册的可恢复执行所有者：逐阶段 Claim/Complete、每次破坏性操作前重验治理、立即验证 Adapter Receipt/Post-state、再次聚合五存储零残留，并以最终收据绑定前序证据；最后一次 Checkpoint/审计 CAS 原子写入 completed 状态。
- Worker 构造要求完整 Adapter 集和 Subject Write Fence。未注册的 Agent/Tweet 复合 Adapter 已能形成双权威回执；Tweet 本地门闩已覆盖六类主体写入，Agent 主要 Service 入口完成写前授权，但尚未关闭检查至提交竞态及所有直接/后台写路径。后台调度、CLI 与生产组合根未接入。真实 Mongo/MySQL 故障恢复与 Live Evidence 完成前，`execution_supported=false` 仍是强制停止线。
