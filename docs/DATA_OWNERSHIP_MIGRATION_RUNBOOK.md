# M7-R1 数据所有权、账号与独立迁移

日期：2026-10-13。本轮完成代码边界与离线验证，实际 Schema、账号/授权、镜像构建和部署尚未验收。共享 MySQL 仍是当前事实，表所有权不代表已经分库。HTTP/gRPC 契约不变。

## 当前所有权与例外

| Owner | MySQL 表 | 运行写入者 / 受控例外 |
|---|---|---|
| User | users | UserService；其他服务通过RPC读公开资料 |
| Tweet | tweets、likes、comments、retweets、bookmarks、polls、poll_options、poll_votes、tweet_create_idempotency、tweet_subject_write_fences、tweet_subject_deletion_operations | TweetService；Consumer读取当前推文与投影身份 |
| Follow | follows、follow_pair_locks、follow_author_projections、follow_projection_receipts | FollowService写事实/pair门闩；启用Follow投影时Consumer写派生状态/回执。TweetService只读当前关系；Consumer只读当前/历史关系 |
| Messenger | messages | MessengerService；会话是有界读取投影，没有独立conversation表 |
| Notification | notifications | NotificationService/其消费Worker；Gateway通过RPC读取 |
| Outbox基础设施 | outbox_events、outbox_tasks、outbox_publications | 事件语义归Tweet/Follow生产者，事实与事件同UoW；Consumer写租约任务，Relay/显式维护可写发布回执。共享表是受控例外，账号不能靠“基础设施owner”获得业务表写权限 |
| Agent | agent_personas | Agent自有Persona；当前仍在共享MySQL，Mongo会话/工作流另归Agent。UserPersona兼容类型别名指向domain.AgentPersona，构造MemoryManager不再隐式迁移 |

`internal/infrastructure/schema/catalog.go` 是本轮可执行目录，按Owner生成建表计划、按runtime role给出启动校验范围。追加手工索引纳入该目录：Messenger两项分页索引、Follow当前pair/作者历史、Tweet作者历史、Outbox任务lane。Follow派生两张表由独立迁移命令的明确额外模型加入。

TweetService改为注入 `domain.FollowReadRepository`，生产装配 `NewFollowReadRepository` 只导出BatchIsFollowing，不能转成FollowRepository写接口。Feed候选SQL仍查询共享follows，Consumer仍使用Follow仓储读当前/历史关系；这是渐进边界，不能声称已经通过远程服务替换所有跨表读取。Agent角色装配/Persona应用Port与共享Runtime准入留M7-R2。

| Redis / 派生资源 | 当前责任与生命周期 |
|---|---|
| tweet缓存 / tweet_invalidations | TweetService及其L1/L2失效链；源仍MySQL |
| timeline: / unread:timeline: / user timeline / 初始化与修复标记 | Timeline/Follow派生Worker、Tweet读取恢复；Timeline最多1000/7天，恢复不能授权正文 |
| global:celebrities / user:celebrities: | Follow投影Worker拥有迁移/代次写入；Tweet/Consumer按当前事实读取，有界TTL/修复边界见Follow Runbook |
| tweet_tags: / trends:global / lock:user_tag_count: / idempotency:engagement: / trends:decay:bucket:v1 | Consumer互动/创建/服务器桶衰减；48小时映射、24小时热度、1小时频控、7天互动/桶标记，不能作永久事实 |
| notifications:user: Pub/Sub | Messenger与Notification共享实时通道，Gateway订阅；不是持久投递事实，恢复需求见M4-R2 |
| canal:position:tweet-service | Relay唯一发布位点，Confirm后推进；命名历史不代表只转发Tweet事件 |
| Snowflake分配/运行租约、共享配置 | 基础设施跨服务资源，需分配器/租约权限单独评估；本轮未改变Redis ACL/DB/namespace |
| Agent Mongo / ES tweets / Qdrant tweets与共享episodic / 私有MinIO对象 | Agent持久能力与Consumer派生索引按现有owner访问；本轮未执行Mongo索引、向量集合或对象生命周期迁移 |

## 运行服务只读校验

八个MySQL进程（User/Tweet/Follow/Messenger/Notification/Consumer/CanalRelay/Agent）使用ServiceDBConfig并在启用运行前调用CheckRuntime，不执行AutoMigrate。Follow投影/Canal publication回执只在对应开关启用时额外校验。检查失败拒绝启动，不回退到改表。

- 每次启动校验Context最多5秒，元数据仅查当前DATABASE()和固定表名：每表一项TABLES、至多512列/索引项，SQL参数化、固定错误，不读取业务行。
- 校验InnoDB、所有模型持久列存在、声明NOT NULL与主键、64位ID的BIGINT UNSIGNED/有符号int64、主键/索引名称及列顺序、唯一性和不含前缀截断。没有校验所有字符串长度/字符集/默认值/触发器/实际执行权限，不能替代完整Schema审计或业务验收。
- 本轮要求目录中的索引名称与形状准确匹配；历史等价索引但名称不同也会拒绝。先审查SHOW INDEX/重复与锁成本，单独批准加法索引或重命名，不能为了启动静默创建重复索引。
- NewDB初始化/Ping和driver拨号、读写分别限制5秒，DSN由MySQL驱动编码，移除硬编码密码fallback。模块SQL仍沿用现有Logger，本轮Schema元数据/DDL使用不记录SQL的Logger；没有解决所有业务SQL日志正文暴露。

## 独立Bootstrap Job

`cmd/schema-migrate` 默认离线，生成当前编译版本的owned-bootstrap-v1表结构、CREATE TABLE SQL和SHA-256，不读取凭据或联网。它是首次建表/补缺表入口；不会执行AutoMigrate、ALTER、DROP、任意SQL文件、自动回填或修改既有数据。

```powershell
# 离线计划
go run ./cmd/schema-migrate --owner outbox
go run ./cmd/schema-migrate --owner tweet

# 确认目标依赖后，以独立检查账号/迁移账号作真实元数据校验
go run ./cmd/schema-migrate --owner tweet --allow-live --timeout 60s

# 完成计划审查后，显式创建缺失表；替换摘要/声明，不把密码放参数中
go run ./cmd/schema-migrate --owner tweet --allow-live --execute --approved-plan-sha256 reviewed-sha256 --operator operator-id --reason approved-bootstrap --operation change-id --timeout 60s
```

- Owner固定为user/tweet/follow/messenger/notification/outbox/agent；每次≤32表/60秒，默认60秒。Live使用DB_HOST/PORT/NAME与必须显式配置、非root的DB_MIGRATOR_USER/PASSWORD；不自动加载.env，机密只由作业环境注入，运行容器不持有迁移凭据。
- Execute必须匹配本次重新生成的计划SHA以及非空有界operator/reason/operation；没有外部SQL/Plan输入，也不能用access-role混合执行。报告只输出操作摘要与逐表created_verified/already_present/partial_or_uncertain/unverified结果，需由操作人保存到审计系统。本机声明/摘要不是服务端审批/RBAC、持久迁移版本账本或签名授权。
- 在同一个MySQL连接上以DATABASE/owner摘要命名锁串行化，等待最多2秒；先校验所有已存在表，再执行任一缺失表CREATE。现存表有漂移则在任何DDL之前退出。创建后重新读取元数据验证，重跑只校验/跳过已成功的表；SQLStore拒绝无锁连接执行DDL。
- MySQL DDL隐式提交，不把多表创建包装为可回滚事务。DDL响应丢失、取消或权限失败时保留可能已创建表，输出partial/uncertain并停止；确认真实状态后用同一计划重跑。Driver读写5秒可能早于60秒作业窗口超时；首次建表也必须先评估MDL、磁盘和恢复，不盲目增加超时。
- 锁释放用独立2秒Context并检查回执。命名锁仅协调同primary本工具，不能阻止旧版本AutoMigrate、DBA或其他DDL；升级前停止这些owner。
- CREATE包含当前模型和手工read/lane索引，不建外键。Follow/Outbox可提前建立尚未启用功能的表，但不自动打开开关。Snowflake/维护allocator与既有AUTO_INCREMENT策略仍须真实核验，启动检查不证明分配器安全。

打包入口为 `deploy/docker/schema-migrate.dockerfile`，按包构建，builder/runtime由显式审核镜像参数提供；独立 `deploy/compose/schema-migrate.yaml` 默认离线、单次退出、只读非root容器，镜像需审查不可变digest。它不挂进生产自动启动序列，Live前须显式选择目标网络/地址和短期迁移凭据。本轮只完成静态/离线门禁，未构建镜像或运行Job。

已有表升级仍使用已审核SQL：Messenger分页、Follow原子事件/派生状态、Outbox projection operations；旧SQL不是幂等runner，不能对已存在索引直接重复执行。针对本轮启动检查发现的其他漂移，应另产具体SQL并审核，不修改历史SQL来伪造已经迁移。

## 最小权限与接线

```powershell
# 仅输出SQL计划，不创建账号、不应用GRANT，不读取凭据
go run ./cmd/schema-migrate --access-role tweet-service --database twitter --account twitter_tweet --account-host 10.20.%
```

账号/数据库标识固定字符集，Host必须显式指定；计划只有GRANT，不包含密码、CREATE USER、ALL、DDL或GRANT OPTION。按实际启用功能进一步缩减后由DBA应用，回读SHOW GRANTS并验证拒绝越权写/DDL：

| 运行账号 | 允许范围 |
|---|---|
| User/Messenger/Notification | 仅各自表DML |
| Tweet | 自有表DML、follows仅SELECT、outbox_events SELECT/INSERT/UPDATE用于同UoW与OnConflict |
| Follow | follows DML、pair门闩与outbox_events SELECT/INSERT/UPDATE；不授Tweet表写 |
| Consumer | tweets/follows SELECT、outbox_tasks DML；启用Follow投影时author/receipt SELECT/INSERT/UPDATE，未启用可省略 |
| Agent | 仅agent_personas DML；业务读取通过既有Tweet/User RPC |
| CanalRelay | outbox_events SELECT、可选publication SELECT/INSERT/UPDATE；显式REPLICATION SLAVE/CLIENT服务器级例外，没有年龄GC DELETE |

DML四项是当前模块能力上界，业务删除未使用的DELETE可继续收窄；这个计划不检查既有权限、继承role或运行环境，也不会撤销历史宽权限。CDC服务器级replication可能读取更广binlog，必须另验收TLS/网络/过滤/最小账号，不能由事件表GRANT宣称完全隔离。迁移账号独立短期，按Owner授必要CREATE/元数据权限；本轮不提供自动账号 provisioning。

八个角色的环境前缀分别是USER_SERVICE、TWEET_SERVICE、FOLLOW_SERVICE、MESSENGER_SERVICE、NOTIFICATION_SERVICE、CONSUMER、CANAL_RELAY、AGENT_SERVICE。`<PREFIX>_DB_USER/PASSWORD` 必须成对，不能混用legacy密码；APP_ENV=production或DB_REQUIRE_SERVICE_CREDENTIALS=true要求非root独立凭据，生产不能用false关闭门禁。普通本地未提供角色对时保留DB_USER/PASSWORD兼容。

Production Compose明确传入八对凭据和强制策略；部署预检拒绝缺失、占位、root、复用账号或与显式迁移账号相同。凭据字段配置不等于数据库权限已实现；DB_HOST/PORT/NAME仍共享，保持当前事实UoW/命名锁和Feed读语义。基础Compose的local共享账号是历史兼容，不应用到生产；MySQL/Temporal/Gateway等其他共享账号路径、Redis/Mongo/Provider ACL和服务间授权仍独立治理。

## 升级与回滚

1. 固定精确版本，分别输出/审查Owner计划、访问计划和现存Schema差异，核验备份/恢复与变更窗口。避免每副本生成不同计划或使用另一版本摘要。
2. 停旧运行进程DDL/旧迁移owner，独立创建缺表、审核并执行既有表加法迁移；复验当前primary的引擎、完整索引、锁/allocator/权限与业务读写。
3. DBA创建不同runtime账号并授实际必要权限，真实验证拒绝跨表写/DDL、metadata可见；用生产预检、启动校验、业务旅程与回归报告灰度启动。M3功能开关默认false、删除能力关闭等边界不因迁移改变。
4. 若回滚，停止新流量/Worker并保留所有表、索引、Outbox/位点/回执/派生状态；保留运行账号无DDL权限，不恢复旧AutoMigrate。旧版本可能因DDL权限被拒绝，选择兼容版本或先修正启动脚本，不能为恢复而授回宽权限或删新增结构。

真实待验收：各Owner冷启动/已有Schema/坏索引/重复行、迁移前检查/并发owner/MDL超时/DDL回执丢失、正确目标primary/权限继承、每角色业务操作与越权拒绝、binlog权限/TLS、网络/镜像构建/备份回滚；容量扩大100倍时metadata bounded不变，但跨表查询/同库连接预算/热点与迁移锁依然需要容量证据。离线GORM SQL/Fake/元数据驱动、配置/YAML和Race不能替代这些验收。
