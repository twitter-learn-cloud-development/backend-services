# Domain Model（领域模型与数据所有权）

> 最近核对：2026-07-15
> 代码事实源：`internal/domain/*.go`、`internal/module/agent/repository/*.go`

> 2026-10-14 M8-R2：密码仍归User/MySQL，仅保存bcrypt hash；Auth转发ChangePassword并实现无副作用的local Logout回执，Gateway身份来自JWT Context。两端成功改密后清当前会话；JWT没有密码版本/服务端会话或撤销查询，旧令牌自然过期前仍有效，不能把本地代次当服务端授权状态。无新表/Proto字段，全部设备撤销与密码并发版本/CAS仍Planned，删除执行能力继续关闭；契约见AUTH_SESSION_CONTRACT。

> 2026-10-13 M7-R2：AgentPersonaStore为按显式用户ID读取/写入Persona的应用Port，SQLPersonaStore仅有界读agent_personas和身份键upsert，不构造DDL；保持原字段/Unix秒updated_at。MemoryManager不持有GORM，API/all拥有该适配器，Worker不打开MySQL。共享Run/Provider租约和分钟估算预算为Redis治理状态，不是领域事实/Provider账单/持久等待队列，也不替代Mongo主体删除围栏或审批授权；Persona写围栏仍待专项，数据删除保持关闭，真实权限/预算故障未Accepted。

## 1. 全局约定

- 社交领域主键主要使用 Snowflake `uint64`。
- 社交实体时间字段主要使用 Unix 毫秒 `int64`，并非统一 `time.Time`。
- Agent Mongo 模型使用 `primitive.ObjectID`，gRPC 兼容层同时保留字符串 Dialogue Key 与旧 `uint64` ID。
- 软删除字段常以 `DeletedAt == 0` 表示未删除。
- Repository 接口属于领域边界；Gateway 不应直接实现或绕过这些接口。

> 2026-10-13 M7-R1：Schema Owner目录独立于业务写入者，Outbox为共享基础设施表但事件语义归生产模块；运行进程只校验、独立Owner Job仅创建缺表/既有漂移另审核。AgentPersona归domain，RAG UserPersona兼容别名不再构造DDL；TweetService仅注入FollowReadRepository，生产wrapper没有Follow写能力，候选SQL仍共享follows。生产角色独立凭据/访问计划只表示接线，不证明已创建账号/授最小权限或分库；真实Schema/权限未验收，详情见DATA_OWNERSHIP_MIGRATION_RUNBOOK。

> 2026-10-13 M6-R2：领域事实/HTTP/Proto不变。互动恢复只保留/重送原事件，专用CLI不补造发生时间，操作摘要报告不充当持久审批/全局重放次数账本；当前公共事实仍由消费端重验。热度衰减派生状态trends:decay:bucket:v1为服务器分钟桶，7天TTL/有界原Top100裁剪与分值同Lua，只执行当前桶，不恢复遗漏桶历史或隐藏后的热度撤销；Redis丢失状态/持久化故障未Accepted。

> 2026-10-13 M6-R1：LikeMutationRepository只在已有UoW返回实际关系ID及created，按当前FOR UPDATE读取判断，重复点赞不产事件；物理取销后再赞是新关系。点赞/评论事实与Outbox同3秒事务，追加engagement-v1/like或comment实体键/发生时间/Trace，旧字段兼容。MQ互动热度是有时效Redis派生回执，不能代替MySQL事实；原子去重/频控/加分后ACK、7天标记/48小时年龄仅提供保留期幂等，真实Redis淘汰/持久化与多副本衰减未验收。

## 2. 社交领域实体

| 聚合/实体 | 核心字段或语义 | 所有者/存储 |
|-----------|----------------|-------------|
| `User` | Username、PasswordHash、Email、Avatar、Bio、CoverURL、Website、Location | User / MySQL |
| `Tweet` | UserID、ParentID、Content、MediaURLs、Type、VisibleType；互动统计是聚合字段 | Tweet / MySQL + Redis cache |
| `Comment` | 评论独立实体；Tweet 同时用 ParentID 表示回复关系 | Tweet / MySQL |
| `Like`、`Bookmark`、`Retweet` | 用户与推文的交互关系 | Tweet / MySQL/Redis |
| `Poll`、`PollOption`、`PollVote` | 推文投票聚合 | Tweet / MySQL |
| `Follow` | FollowerID -> FollowingID | Follow / MySQL |
| `FollowPairLock` | 有向FollowerID/FolloweeID复合主键协调行，跨取关/重新关注保留 | Follow / MySQL，手工加法迁移未执行 |
| `AuthorState`（follow/projection） | 作者revision/epoch/大V状态/共享历史关系cursor/complete/last_repair摘要 | Follow投影Worker / MySQL，新表手工迁移、开关默认关闭 |
| `IngressReceipt`（follow/projection） | 唯一event_key、事件摘要、随机registration_token和时间；与两个任务共同登记 | Follow投影入口/维护 / MySQL，保留防重复、无年龄GC |
| `Message` | ConversationID、SenderID、ReceiverID、Content、IsRead | Messenger / MySQL |
| `Conversation` | 非持久表投影：Peer、LatestMessage、UnreadCount | Messenger Service |
| `Notification` | UserID、ActorID、Type、TargetID、IsRead | Notification / MySQL |
| `OutboxTask` | 有状态异步任务：Status、Retries、ErrorMsg | 基础设施/业务 Worker |
| `OutboxEvent` | 事务内写入的不可变领域事件 | 业务事务 + Canal/Relay |
| `OutboxPublication` | EventID唯一绑定事件类型/canonical Payload摘要与首次ConfirmedAt；可选operation/operator/reason摘要 | Relay/显式维护 / MySQL，开关默认关闭、手工迁移待执行 |
| `TweetCreateIdempotency` | UserID + IdempotencyKey 唯一绑定请求摘要与已提交 TweetID | Tweet / MySQL |

注意：旧文档曾把 Messenger 描述为 MongoDB；当前 `internal/module/messenger/repository/message_repo.go` 使用 GORM/MySQL。MongoDB 当前主要归 Agent 会话与工作流使用。

M4-R2私信不变量：MySQL Message是历史事实，Redis Pub/Sub只是可丢失实时提示；发布失败不改变已入库成功，未新增发送幂等键或设备ACK。GET只把本页接收者消息标记已读，非UI展示/设备送达；客户端相同ID合并保持已读单调。恢复锚点只在完整HTTP扫描触达旧锚点/结束后更新，Socket与发送回执不能前移；旧历史游标独立，单次恢复最多3页且可继续。ID不是提交序列，迟到低ID超出已扫窗口仍有限制，不能宣称永久完整或exactly-once。

## 3. Tweet 关键不变量

- `ParentID == 0` 表示非回复；回复分页必须使用 Cursor，不能用无界 Offset。
- `VisibleType` 包含 Public、Follows、Private、Shadowban；读取和分发均需执行可见性规则。
- Following Feed 当前 `FollowingFeedRepository` 通过共享社交 MySQL 的有效 follows 只读子查询限定作者与 Public/Follows，作者自身记录独立允许；FollowReadRepository 批量重验当前关系。Redis Inbox/大 V 关联不授权正文，缓存恢复只合并有界候选投影；未来独立存储拆分需替换授权读 Port，而不是把共享表读取当跨服务写入许可。
- `MediaURLs` 是 JSON 字段；API/DB/索引三侧格式必须一致。
- Like/Comment/Share 数量不是 `tweets` 表直接字段，读取时由统计表或 Redis 聚合。
- 创建推文、可选 Poll、发布 Outbox Event 与可选 `TweetCreateIdempotency` 必须使用同一 Unit of Work；Repository 写入必须从 Context 提取事务句柄。
- 单条推文删除在同一 UoW 中锁有效行/校验作者、软删除并写入兼容 `TWEET_DELETED`；删除事件使用唯一 `tweet-deleted:<tweet_id>` 键，已删请求不再次产出事件。Outbox 入库不是投递完成，COMMIT 回执失败也不能推断事务已经回滚；索引清理与重放独立验收。
- Consumer 消费删除事件先登记独立 `delete_es/delete_vector` 任务再成功 ACK，部分登记通过幂等重放补齐；创建与删除投影共用同 MySQL primary 的 `TweetProjectionRepository` 会话锁与当前事实读取（包含软删除），旧事件只作身份候选。只有当前 Public 正文允许写公共索引，写后再次重验；会话锁不替代远端服务版本围栏和故障后的持久对账。
- 创建 ACK 前登记独立 sync_text/sync_vector；旧 sync_es 只登记子任务。文本不依赖 Embedding，向量固定 Model/Dimension/Version，模型调用在会话锁外，返回后重验事实再写入。任务只保留身份/Profile/版本与可选修复摘要，当前正文从 MySQL 读取。
- ProjectionDigest 是当前事实摘要；ES/Qdrant 的相同元数据不证明实际正文/向量质量。显式最多100 IDs的 audit 不扫描全库；审批后 repair 只登记持久任务，每个 Tweet 原子事务/去重，跨 Tweet 可以部分完成，Vector repair 清除旧别名后重写可能暂时缺失。
- Confirm 回执只证明 Broker 接收。可选账本写入先于 Relay 后续位点；重复身份保留首次确认时间，冲突拒绝。发布感知保留只生成候选，不删除；点赞/评论未完成事件级幂等前不能经维护命令重放。
- 发推幂等键按用户隔离；相同键/相同摘要返回原 Tweet，相同键/不同摘要拒绝。可重试 Agent/Workflow 调用必须传稳定键，旧客户端省略键时保持原行为。
- `TWEET_MAX_CONTENT_LENGTH` 是业务可配置限制，不能重新硬编码为 280。

## 4. Timeline 与事件模型

Follow状态变更当前通过同一primary/UoW锁pair协调行，再有界锁读有效关系并与Outbox一起提交。重复当前状态保留 AlreadyFollowing/NotFollowing，不增加事件；历史多条有效关系拒绝，重新关注新Snowflake关系代次。USER_FOLLOWED/USER_UNFOLLOWED追加v1/关系ID/动作事件键/发生时间/Trace，旧两身份字段兼容；pair不是请求幂等键，不冻结跨代次延迟旧请求。Outbox唯一键冲突必须回滚事实，COMMIT失败可能已共同提交。持久follow.relations.v1本轮无Consumer，缓存旧异步owner待M3-R3迁移，不能将事件入库/Confirm视为派生完成。

以上末句为R2历史状态；R3已装配可选持久入口/双lane，默认关闭。Service旧owner移除；新入口receipt与author/pair Outbox任务原子登记后ACK，成功页更新持久检查点，关系任务重验当前有效代次、作者共享游标按历史关系页批验当前关注。重新关注不由旧事件直接覆盖，写后变化重置页；Redisepoch/服务器租约时间拒绝陈旧副作用，SQL回执仍按Owner/Token/Lease围栏。作者Complete仅证明当次页完成，不证明Redis未淘汰，显式维护强制重扫；新表/索引未迁移，真实恢复/容量未验收。

```text
Tweet transaction
  -> Tweet row
  -> OutboxEvent(TWEET_CREATED)
  -> Relay/RabbitMQ
  -> Timeline Consumer
       -> Redis Inbox/ZSet
       -> OutboxTask(sync_text) -> Elasticsearch document
       -> OutboxTask(sync_vector + EmbeddingProfile) -> Qdrant vector
```

Consumer 必须按 Event/Message ID 幂等；ES/Qdrant 属于派生索引，必须允许重放和对账。

## 5. Agent 持久化模型

代码位置：`internal/module/agent/repository/`。

| 模型 | 存储 | 关键语义 |
|------|------|----------|
| `Dialogue` | Mongo `dialogues` | 用户、标题、模式、更新时间、摘要游标/版本/租约 |
| `DialogueMessage` | Mongo `dialogue_messages` | user/assistant/system/tool、Metadata、ToolCallID |
| `WorkflowDefinition` | Mongo `agent_workflows` | 用户所有权、名称、当前 DSL 读模型及 `current_revision_id/current_revision_number`；旧定义按 v1 兼容并在首次运行/更新时懒迁移 |
| `WorkflowRevision` | Mongo `agent_workflow_revisions` | 不可变 DSL、SHA-256 Hash、用户/工作流所有权和单调版本号；`(workflow_id, revision_number)` 唯一 |
| `WorkflowRunRecord` | Mongo `agent_workflow_runs` | 输入/输出、固定 Workflow Revision、状态、Checkpoint/StateVersion、ResumeToken Hash、审批绑定、错误与时间；`revision` 仍表示 Run 乐观锁版本 |
| `WorkflowStateEvent` | Mongo `agent_workflow_state_events` | Blackboard Delta 的 append-only 边界持久化；`(run_id, sequence)` 唯一，内容哈希不同的重复序号 fail-closed |
| `ToolApprovalRequest` | Mongo `agent_tool_approvals` | 用户/Run/Step/Tool 绑定、脱敏参数、状态机、审批/执行租约、过期时间、审批人与乐观锁版本、Run 对账标记 |
| `ToolExecutionRecord` | Mongo `agent_tool_executions` | 用户/Tool/幂等键唯一领取、输入摘要、执行租约和可回放结果；陈旧执行由治理对账标记失败后允许重试 |
| `UserPersona` | MySQL `agent_personas` | L1 长期画像 |
| Episodic Summary | Qdrant | L2 摘要向量与结构化 Payload，按 user/source/version 隔离 |

会话摘要通过消息计数游标和 Mongo 租约声明增量区间；失败不得推进游标。

## 6. 数据所有权约束

- User/Tweet/Follow/Messenger/Notification 应由对应服务拥有写路径。
- Agent 调用社交能力应通过 gRPC/MCP Tool；现有 Activity 直连 MySQL/Redis 属于待治理技术债，不是推荐模式。
- Workflow Resume Token 不保存明文；Run 仅保存 SHA-256 哈希，并以状态条件更新保证单次领取。
- 审批到期后，治理对账必须将关联的挂起 Run 置为失败并清理 Checkpoint、Waiting Node、Resume Token 与审批绑定；不能留下可再次恢复的旧状态。
- 审批执行租约和工具执行租约都必须可回收；前者可退回已批准状态继续领取，后者转为失败后由同一幂等键安全重试。
- Elasticsearch/Qdrant 是可重建派生数据，不应成为业务唯一事实源。
- Redis 不保存不可恢复的永久事实。
- 跨服务操作禁止依赖分布式事务；使用 Outbox、幂等 Consumer、补偿和状态机。

## 7. 变更检查

修改领域模型时同时检查：

1. GORM Tag、索引与在线迁移兼容。
2. Proto/HTTP DTO 是否需要新增字段。
3. Redis/ES/Qdrant 派生结构是否需要版本化或回填。
4. MQ Event 是否保持向后兼容。
5. Web 与 Mobile 是否都需要适配。
6. Repository Mock、契约测试和批量查询是否覆盖新字段。
