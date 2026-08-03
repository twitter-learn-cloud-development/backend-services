# Domain Model（领域模型与数据所有权）

> 最近核对：2026-07-15
> 代码事实源：`internal/domain/*.go`、`internal/module/agent/repository/*.go`

## 1. 全局约定

- 社交领域主键主要使用 Snowflake `uint64`。
- 社交实体时间字段主要使用 Unix 毫秒 `int64`，并非统一 `time.Time`。
- Agent Mongo 模型使用 `primitive.ObjectID`，gRPC 兼容层同时保留字符串 Dialogue Key 与旧 `uint64` ID。
- 软删除字段常以 `DeletedAt == 0` 表示未删除。
- Repository 接口属于领域边界；Gateway 不应直接实现或绕过这些接口。

## 2. 社交领域实体

| 聚合/实体 | 核心字段或语义 | 所有者/存储 |
|-----------|----------------|-------------|
| `User` | Username、PasswordHash、Email、Avatar、Bio、CoverURL、Website、Location | User / MySQL |
| `Tweet` | UserID、ParentID、Content、MediaURLs、Type、VisibleType；互动统计是聚合字段 | Tweet / MySQL + Redis cache |
| `Comment` | 评论独立实体；Tweet 同时用 ParentID 表示回复关系 | Tweet / MySQL |
| `Like`、`Bookmark`、`Retweet` | 用户与推文的交互关系 | Tweet / MySQL/Redis |
| `Poll`、`PollOption`、`PollVote` | 推文投票聚合 | Tweet / MySQL |
| `Follow` | FollowerID -> FollowingID | Follow / MySQL |
| `Message` | ConversationID、SenderID、ReceiverID、Content、IsRead | Messenger / MySQL |
| `Conversation` | 非持久表投影：Peer、LatestMessage、UnreadCount | Messenger Service |
| `Notification` | UserID、ActorID、Type、TargetID、IsRead | Notification / MySQL |
| `OutboxTask` | 有状态异步任务：Status、Retries、ErrorMsg | 基础设施/业务 Worker |
| `OutboxEvent` | 事务内写入的不可变领域事件 | 业务事务 + Canal/Relay |
| `TweetCreateIdempotency` | UserID + IdempotencyKey 唯一绑定请求摘要与已提交 TweetID | Tweet / MySQL |

注意：旧文档曾把 Messenger 描述为 MongoDB；当前 `internal/module/messenger/repository/message_repo.go` 使用 GORM/MySQL。MongoDB 当前主要归 Agent 会话与工作流使用。

## 3. Tweet 关键不变量

- `ParentID == 0` 表示非回复；回复分页必须使用 Cursor，不能用无界 Offset。
- `VisibleType` 包含 Public、Follows、Private、Shadowban；读取和分发均需执行可见性规则。
- `MediaURLs` 是 JSON 字段；API/DB/索引三侧格式必须一致。
- Like/Comment/Share 数量不是 `tweets` 表直接字段，读取时由统计表或 Redis 聚合。
- 创建推文、可选 Poll、发布 Outbox Event 与可选 `TweetCreateIdempotency` 必须使用同一 Unit of Work；Repository 写入必须从 Context 提取事务句柄。
- 发推幂等键按用户隔离；相同键/相同摘要返回原 Tweet，相同键/不同摘要拒绝。可重试 Agent/Workflow 调用必须传稳定键，旧客户端省略键时保持原行为。
- `TWEET_MAX_CONTENT_LENGTH` 是业务可配置限制，不能重新硬编码为 280。

## 4. Timeline 与事件模型

```text
Tweet transaction
  -> Tweet row
  -> OutboxEvent(TWEET_CREATED)
  -> Relay/RabbitMQ
  -> Timeline Consumer
       -> Redis Inbox/ZSet
       -> Elasticsearch document
       -> Qdrant vector
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
