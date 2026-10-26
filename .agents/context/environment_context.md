# Environment Context（运行环境与验证入口）

> 最近核对：2026-08-02
> 事实源：`.env.example`、`docker-compose.yaml`、`cmd/*/main.go`

> 2026-10-15 M2-R3-D：FEED_ACCEPTANCE_MYSQL_DSN保持原只读账号变量，默认CLI在读凭据前拒绝Live。v2参数新增workers1—8/pages1—20/min-pages/min-rows/min-followees1—1000/可选max-p95-ms；samples总数≤100，池=min(workers,samples)，预检和所有样本共60秒，协议超时5秒。每样本独立RR/回滚，预检仅有界关注/表行估计/EXPLAIN；无新环境变量/依赖/DDL/种子/Redis，原v1报告不改写。Test/Vet/Race和CLI门禁离线通过，未连接真实目标；数据集/实例/只读权限/负载预算仍按FEED_ACCEPTANCE_RUNBOOK先确认，工具通过非容量接受。

> 2026-10-15 M4-R2：无新环境变量/Schema/依赖。Messenger提示发布总2秒；Gateway订阅ACK3秒、写5秒、Ping25秒、Pong60秒、JWT自然到期关闭；两端前台30秒补拉、每次最多3页。deploy/prometheus/rules/twitter-clone-messenger.yml沿已有rules通配目录，抓取仍为Gateway2110/Messenger2115；真实Prometheus/告警未验证。使用既有vendor/Node/Flutter3.44.2，Go显式工作区GOCACHE，回环夹具/SDK缓存正常审批复验；目标Test/Vet/Race/入口编译、Web18测试/build、Flutter零问题/32测试通过。没有启动业务依赖/真实DDL/业务写入/部署；现场前置和回退见MESSENGER_RECOVERY_RUNBOOK。

> 2026-10-15 M8-R3：没有新增业务环境变量。验收v2在Initialize/SetCheck/Verify都显式提供ReleaseManifestFile/ExpectedReleaseManifestSHA256/ExpectedSourceSHA/EnvironmentSnapshotFile/ExpectedEnvironmentSnapshotSHA256/Environment；签名密钥/记录/脱敏原始证据默认在仓库外受控目录，路径reparse拒绝。快照仅含镜像digest和公开配置/Secret/Schema版本引用，禁止真实Secret或其明文哈希；data_deletion_enabled和token_revocation_supported必须false。每Passed另需同身份的live观察/原始摘要，24小时内有效；v1不通过新发布门禁。用户本轮仅要求离线，31项/24拒绝用例、v1降级、AST/发布工具Test/Vet/Race/12服务矩阵通过；每个Go命令显式GOCACHE=repo/tmp/go-build-cache避免新shell默认缓存权限失败。无真实staging/依赖启动/发布/部署；签名材料一致不证明现场真实或获流量批准，deployment_accepted=false，见DEPLOYMENT_ACCEPTANCE_RUNBOOK。

> 2026-10-14 M8-R2：没有新增部署参数/依赖/Schema。认证仍是现有RS256/JWKS无状态JWT，退出和改密不提前撤销；不能通过配置宣称全设备退出。使用已有Go/Node/Flutter SDK及Fake/miniredis/httptest验证，首次Gateway回环拒绝已正常提权复验通过。目标Go Test/Vet/Race、Web build/13状态、Flutter零分析问题/认证路由与Socket离线夹具通过，最终次数见PROJECT_PROGRESS/ISSUES245；无真实服务/凭据/数据库/Provider/镜像/部署操作。客户端旧网关密码路径可能返回非JSON错误，新Mobile保留明确的结果不确定提示。下一M8-R3真实部署证据与用户旅程，须按环境范围单独验收。

> 2026-10-13 M8-R1：CI由go.mod选Go1.25.5，Node22/npm ci与固定Flutter3.44.2/lockfile，catalog驱动12镜像与可审核基础digest；默认候选OCI，main显式publish才用Registry凭据，组织环境审核需管理员实际配置。release-manifest默认离线/干净Git版本，metadata5秒64KiB、输入1MiB/最多64组件SQL/O_EXCL；真实Secret/.env不入清单，仅公开模板/锁/迁移身份。历史自动审批额度/回环与realpath限制本次正常审批恢复后完成验证：完整Go Test/CI范围Race/Vet、Windows-Linux包编译、Web完整build/7测试、Flutter analyze零问题/test16项、生产配置夹具通过。只使用已有SDK/依赖和本机Fake，无安装/业务启动/镜像/Registry/部署验收；旧tmp用忽略go.mod隔离，产物优先.codex-tmp，真实CI/权限/clean-host旅程仍未Accepted。见RELEASE_PIPELINE_RUNBOOK。

> 2026-10-13 M7-R2：AGENT_SHARED_ADMISSION_ENABLED默认false，NAMESPACE=agent-v1/LEASE=30s，Run64/User2/Workflow2，Provider全局8/每Provider8，输出4096/分钟估算Token250000，费用预算与单价默认0。策略字段/边界见AGENT_SHARED_ADMISSION_RUNBOOK；三Compose透传。API/all启用必须完整AGENT_RUNTIME_V2_MODES（推荐all）并由管理CLI显式初始化匹配Redis摘要，热路径不初始化/不回退本地。管理CLI默认离线，Live读取REDIS_HOST/PORT/PASSWORD/DB，连接/操作总5秒；不支持专用ACL USER/TLS/Cluster/Sentinel。MySQL Persona仅API/all，生产Worker跳过Agent DB凭据检查/探测9191，其余七账号仍要求独立；当前仍单agent-service模板，副本发现/抓取需另验收。离线Test/Vet/Race/YAML/PS语法通过，无真实初始化/Schema/权限/Provider/部署验收；估算预算未覆盖摘要/Embedding/RAG/Reranker/Temporal/Eval。

## 1. 配置优先级

1. 进程环境变量 / 本地 `.env`。
2. Consul 动态配置（仅已接入的服务/配置项）。
3. `configs/config.yaml` 或代码 fallback。
4. `.env.example` 只是模板，不能作为运行事实。

禁止把 `.env`、API Key、JWT Secret 或数据库密码写入文档、DSL、日志和测试快照。

> 2026-10-13 M7-R1覆盖历史自动迁移：八MySQL入口与Agent Persona构造均只读Schema，不再AutoMigrate。运行配置前缀USER_SERVICE/TWEET_SERVICE/FOLLOW_SERVICE/MESSENGER_SERVICE/NOTIFICATION_SERVICE/CONSUMER/AGENT_SERVICE/CANAL_RELAY的_DB_USER/PASSWORD须成对；APP_ENV=production或DB_REQUIRE_SERVICE_CREDENTIALS=true要求独立非root，生产不能关闭，local未配置对时兼容DB_USER/PASSWORD。Production Compose/预检要求八对不同账号，启动Schema≤32表/5秒且精确索引名/形状；NewDB拨号/Ping与I/O5秒。schema-migrate默认离线、Live用独立DB_MIGRATOR_USER/PASSWORD和DB_HOST/PORT/NAME，≤60秒/同连接锁/只建缺表/匹配计划与声明；独立Compose Job/按包Dockerfile未运行构建。既有表升级仍审SQL，真实账号/DDL/授权/Job未验收，启动前必须完成Schema/权限准备，回滚不得恢复运行DDL。

> 2026-10-13 M6-R2（覆盖下方R1互动连接/衰减状态）：每互动队列独立Consume/Confirm连接/QoS1，每实例新增4条连接，初始化/拓扑/订阅总5秒并用原始transport取消阻塞。Redis分钟桶/7天完成状态、最多10000主题/原Top100裁剪/0.95一次衰减，保留剩余TTL/无TTL修复24小时，跳过遗漏桶/时钟回拨拒绝。新engagement-dlq-replay默认离线，MQ凭据仍沿用MQ_*且仅allow-live读取，最多100消息/60秒；执行需操作声明。无新SQL或环境变量/Compose变更，旧owner必须停止，回滚保留桶/队列/标记，真实依赖/容量未验收，见ENGAGEMENT_RECOVERY_RUNBOOK。

> 2026-10-13 M6-R1：无需新DDL，依赖既有likes用户/推文唯一键、Outbox唯一键/主体门闩与同primary事务。Consumer新增互动专用Retry.v1/DLQ绑定，仍用现有QoS；各队列串行/8秒Context，失败另有5秒Confirm与最多4秒等待。Redis Lua最多32主题/4096字节、共享频控1小时/热度24小时/标记7天，按服务器时间拒绝48小时外加分；Redis Cluster跨slot不支持。升级必须停止所有旧先ACK计分消费者，回滚保留积压与标记，不混跑旧Writer。离线回环/GORM驱动验证完成，本轮未启动真实依赖/执行DDL/真实写入/部署；人工互动重放和多实例衰减归M6-R2，见ENGAGEMENT_PROJECTION_RUNBOOK。

> 2026-10-13 M3-R3：FOLLOW_PROJECTION_WORKER_ENABLED默认false，Compose透传；开启前独立审核 `20261013_follow_projection.sql` 新作者状态/入口回执（含registration_token）与历史索引、既有Outbox租约/唯一键/allocator、同primary/命名锁/池/最小权限和独立Broker绑定。Consumer启用后用专用Ingress/QoS10和Confirm Publisher，页100条/15秒/90秒租约、各lane Claim1、关闭等待；FollowService旧派生owner已移除。维护CLI默认离线，Live用FOLLOW_PROJECTION_MYSQL_DSN/REDIS_ADDR与可选Redis USER/PASSWORD，最多100scope/60秒，写入额外审批摘要且只登记；CLI无TLS选项，Redis Cluster跨slot Lua不支持。本轮未执行迁移/真实写入/部署，详情见FOLLOW_PROJECTION_RECOVERY_RUNBOOK。

> 2026-10-13 M3-R2：FollowService写入需同primary/InnoDB的follow_pair_locks/follows/outbox_events及事件唯一键，启动检查pair表/Outbox/命名索引存在，不代替真实引擎/allocator/权限验收。手工SQL `deploy/mysql/migrations/20261013_follow_atomic_events.sql` 本轮未执行；所有旧Writer须先停止，新增pair锁不能围栏旧版本。FollowService启动声明持久follow.relations.v1绑定user.followed/unfollowed，Relay先升级路由；本轮没有关注Consumer、TTL或GC，必须限制灰度窗口和观察暂存积压/磁盘。运行账号权限/迁移与回滚见 `docs/FOLLOW_RELATION_OUTBOX_RUNBOOK.md`，真实DDL/数据库写/MQ/部署尚未执行。

> 2026-10-13 M5-R3：`cmd/tweet-index-reconcile` / `cmd/outbox-publication-maintenance` 默认离线，显式 IDs 最多100个；真实访问仍须先确认依赖，操作 Context 60秒，MySQL I/O和维护Broker初始化最多5秒。前者通过 PROJECTION_MYSQL_DSN/PROJECTION_ES_URL/PROJECTION_QDRANT_URL（可选ES API Key）指定同primary目标，CLI显式Model/Dimension/Version须匹配Worker；后者用PUBLICATION_MYSQL_DSN及MQ环境配置。写模式额外要求allow-write和审核计划摘要/operation/operator/reason，不自动启动服务或迁移；无GC删除模式。Consumer维度/版本来自TWEET_EMBEDDING_DIMENSION/VERSION（默认1024/v1），不自动改既有Collection。CANAL_PUBLICATION_RECEIPTS_ENABLED默认false，新表/可选lane索引见手工SQL与 `docs/INDEX_RECONCILIATION_RUNBOOK.md`，本轮未执行真实DDL或后端写入。

## 2. 本地端口

| 组件 | 端口 |
|------|------|
| Gateway | 代码 fallback `8080`；`.env.example` 配置 `9638` |
| User / Tweet / Follow | `9091` / `9092` / `9093` |
| Messenger / Notification / Auth | `9094` / `9095` / `9097` |
| Agent gRPC / MCP | `9100` / `9200` |
| MySQL / Redis / Mongo | `3307` / `6379` / `27017` |
| Elasticsearch / Qdrant | `9200` / `6333`（HTTP）、`6334`（gRPC） |
| RabbitMQ | `5672`；管理端 `15672` |
| Consul / Temporal | `8500` / `7233`；Temporal UI `8233`（以 Compose 为准） |
| Jaeger / Prometheus / Grafana | `16686` / `9090` / `3000` |
| MinIO | `9000`；Console `9001` |
| Loki / Kibana / Sentinel / Pyroscope | `3100` / `5601` / `8858` / `4040` |

端口冲突或 Compose 变化时，以当前 `docker-compose.yaml` 和进程日志为准。

## 3. 数据与 AI 配置

- MySQL：社交领域主数据与 Persona。
- Consumer 投影任务使用同一 MySQL primary 的会话级 `GET_LOCK` 串行化同一推文，等待最多 5 秒、释放最多 2 秒；占用连接但不持有网络调用期间的数据库事务。不要把投影读取或命名锁分散到不同副本/服务器。Canal 缺失位点/已清理 Binlog 默认拒绝启动；只有完成历史事件核查的操作人可以显式设置 `CANAL_ALLOW_CURRENT_POSITION_BOOTSTRAP=true` / `CANAL_ALLOW_PURGED_POSITION_RECOVERY=true`，它们不代表历史已恢复。按年龄自动 Outbox GC 已停用，发布感知保留作业仍待 M5-R3。
- MongoDB：Agent Dialogue/Workflow/Run，不是 Messenger 主存储。
- Elasticsearch：全文/BM25；Qdrant：向量。
- LM Studio：本地 OpenAI-compatible Provider，默认 `http://localhost:1234/v1`。
- LM Studio、Docker Desktop、Minikube 等由用户交互启动的本地软件不视为常驻依赖。执行 Live 验收前必须先告知用户所需软件、精确模型、端口、用途和预计时长，并等待用户确认已经启动；未经确认时的 `connection refused` 只表示依赖未启动，不能登记为项目故障。
- P8.4 当前自动资格报告固定 Chat 模型 `qwen3.7-plus-2026-05-26` 与 Profile Set v5；`qwen2.5-3b-instruct` 只保留为真实失败基线。单/多 Agent 20 Case 搜索工具仍为无副作用评测沙箱，不需要 Embedding 模型。P6 RAG/Router 当前默认使用 `text-embedding-bge-m3`；`text-embedding-nomic-embed-text-v1.5` 不是当前固定评测配置，可作为后续模型迁移对照，不能与 BGE 结果混用为同一基线。
- Live Eval 默认使用本地 file 授权账本；多实例时可选 Redis 共享账本，配置示例为 `internal/module/agent/eval/testdata/agent_task_live_redis_config.example.json`。Redis Username/Password 只从配置指定的环境变量读取；生产要求 TLS/ACL、AOF/备份、`noeviction` 和受控 marker 生命周期。运行真实 Redis 初始化或故障演练前也必须先告知用户所需地址、用途和预计时长并等待确认；普通测试只使用进程内 `miniredis`。
- DashScope：默认云端 Chat Provider。
- 容器访问宿主 LM Studio 时使用明确允许的 `host.docker.internal`；不要把任意私网 Base URL 加入 allowlist。
- 工作流使用 `credential_ref`，不接受明文 `api_key`。
- `AGENT_TOOL_APPROVAL_TTL` 控制待审批有效期，默认 `15m`；恢复令牌只在挂起响应返回一次。
- Tool Result 默认内联上限 `64 KiB`、硬上限 `1 MiB`，对应 `AGENT_TOOL_RESULT_INLINE_MAX_BYTES` 与 `AGENT_TOOL_RESULT_MAX_BYTES`。Docker 中显式启用 `AGENT_TOOL_RESULT_OBJECT_STORE_ENABLED`，并使用独立私有桶 `agent-tool-results`；关闭开关后超过内联阈值的结果 fail-closed，不降级写入 Mongo。
- Workflow Run 事件流默认每 `10s` 心跳、单连接 `2m` 后要求游标重连；Redis Stream 默认最多保留约 `2000` 条并设置 `24h` TTL。对应配置为 `AGENT_WORKFLOW_EVENT_HEARTBEAT`、`AGENT_WORKFLOW_EVENT_STREAM_WINDOW`、`AGENT_WORKFLOW_EVENT_STREAM_MAX_LENGTH`、`AGENT_WORKFLOW_EVENT_STREAM_TTL`。
- Agent OTel 使用 `JAEGER_COLLECTOR_ENDPOINT` 的 OTLP gRPC `4317`；`AGENT_TRACE_SAMPLE_RATIO` 范围为 `0-1`，默认 `1`。
- Tweet Service 启动时 AutoMigrate `tweet_create_idempotency`；这是加法迁移，回滚旧版本时保留该表以避免丢失重试历史。

`.env.example` 中“MongoDB For Messenger”和“ES Text & Vector”注释是历史描述；实际归属以上述代码事实为准，后续应单独修订模板。

## 4. 启动顺序

1. 基础设施：MySQL、Redis、Mongo、RabbitMQ、Consul、ES、Qdrant、MinIO。
2. 核心 gRPC：User、Tweet、Follow、Messenger、Notification、Auth。
3. Consumer/Canal Relay。
4. Agent（LM Studio/云模型可按功能选择启动）。
5. Gateway。
6. Web/Mobile。

Consumer 默认在 `2116` 暴露 Prometheus；Compose 由 `consumer:2116` 抓取，Helm Pod 使用注解发现。该端口只提供指标，不是业务入口。

Temporal、Jaeger、Prometheus、Grafana、Loki、Sentinel、Pyroscope 可按开发场景启用；Agent 对 Temporal 连接失败有降级路径，但对应后台能力会不可用。

## 5. 常用验证

```powershell
# Go 全仓（按任务缩小范围；大型仓库不应每次都全跑）
$env:GOFLAGS='-mod=mod'
$env:GOCACHE='E:\GOProject\cloud\twitter-clone\tmp\go-build-cache'
go test ./internal/module/<target>/... ./cmd/<target> -count=1

# Agent 共享边界变更
go test ./internal/module/agent/... ./cmd/agent-service -count=1
go test -race ./internal/module/agent/<changed-packages> -count=1
go vet ./internal/module/agent/... ./cmd/agent-service

# Web
Set-Location web
npm run build

# Mobile
Set-Location mobile
flutter analyze
flutter test
```

受限执行环境中，Go 1.25.5 工具链/标准库可能位于工作区外；测试应使用工作区 `GOCACHE`，必要时申请沙箱外执行，不能把权限失败误判为业务编译失败。

## 6. 调试边界

- LM Studio `connection refused`：先区分宿主/容器网络和服务监听地址。
- 需要用户启动 LM Studio 或其他本地软件时，先执行上面的交互确认流程；不要自行假设软件应当常驻，也不要在用户未确认时反复探测。
- `Invalid model identifier`：使用 Provider 实际已加载的模型 ID；Chat 选择器不得包含 Embedding 模型。
- ES 正常但语义检索失败：继续检查 Embedding 和 Qdrant，不要只看 ES。
- Agent 工作流失败：依次检查 Workflow Run、节点错误、Tool/MCP、Provider，再检查模型输出。
- 修改端口/环境变量时同步 `.env.example`、Compose、代码 fallback 和环境 Context。
