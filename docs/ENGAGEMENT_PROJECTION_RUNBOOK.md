# M6-R1 互动热度原子投影

> 当前 R2 恢复边界见 `ENGAGEMENT_RECOVERY_RUNBOOK.md`：独立互动会话/QoS1/5秒重建、服务器分钟桶衰减/保留TTL、默认离线的专用互动重放。下方“人工未开放、旧衰减仍保留、QoS10”的说明是R1交付时历史状态，已由R2代码覆盖；真实依赖验收仍未完成。

日期：2026-10-13。状态：Implemented（代码；验证结果见开发计划）。真实 MySQL/Redis/RabbitMQ、多实例故障恢复与部署仍待验收。

## 写入与事件身份

- TweetService 在现有主体写围栏/UoW 中调用 `LikeMutationRepository.ApplyLike`，插入后以锁定的当前读返回实际关系 ID。重复点赞不新增 Outbox；取消后重新点赞生成新关系 ID。不得依据 RowsAffected 判断是否新关系。
- 点赞/评论事实与 Outbox 在同一最长 3 秒事务提交。事件追加 `schema_version=engagement-v1`、`event_key=like:<关系ID>` 或 `comment:<评论ID>`、实体 ID、发生时间与 W3C Trace；原字段兼容，HTTP/gRPC 契约不变。Outbox DedupKey 对应 EventKey。
- 依赖现有 InnoDB `likes` 用户/推文唯一键、Snowflake 唯一 ID、Outbox 唯一键与主体门闩；本轮没有新 SQL 迁移。上线前验证真实键、权限、同 primary/UoW、提交不确定结果与并发冲突，代码模型和 Fake 不能证明实际 Schema。
- 旧点赞按用户/目标/作者/发生时间生成稳定摘要，改变 MQ MessageId 不改变去重；旧评论按 CommentID。旧事件缺少身份/发生时间、半升级格式、未来版本或超过 256 KiB，保留到 DLQ，禁止补造发生时间。

## 消费与原子回执

1. 两个队列各串行处理一条，不再为每次互动创建 goroutine 或写内存 HashtagBatcher。沿用 QoS 10，关闭停止接收并等待在途互动退出。
2. 消费上下文最长 8 秒，沿用 Tweet 当前事实会话锁；检查作者身份、Public、未删除、推文不早于 48 小时。命名锁只协调同 primary 协作 Writer，不是数据库网络调用期间的长事务。
3. 从 `tweet_tags:<ID>` 读取至多 4096 字节，最多 32 个有效规范化主题。近期公共推文没有映射且没有创建趋势回执时，视为创建投影尚未就绪，重试；已有创建回执且无映射可以完成 `no_topics`。
4. Lua 以 Redis TIME 判定互动时效；未来超过 5 分钟永久拒绝，发生超过 48 小时仅写 `expired` 回执。读取映射后 Lua 重验快照，预验所有 key 类型、频控整数与得分范围后，原子执行事件摘要检查、用户/主题频控、加分和结果标记。
5. 点赞权重 2、评论 5；与创建投影共用 `lock:user_tag_count:<用户>:<主题>`，一个 1 小时窗口最多前三次互动计分，达到上限不会继续增大计数。已有无 TTL 的有效频控在成功投影时修复 TTL。全局热度 TTL 24 小时，事件摘要标记 TTL 7 天。
6. 原子回执明确成功后才 ACK；ACK 丢失后重投在标记保留期不会再次计分。摘要冲突永久 DLQ，Redis/事实读/映射状态等瞬时失败走独立 Retry。正常取消仅释放原投递并 requeue，不消耗 retry；ACK/NACK 不确定均可观测。

Lua 隔离不提供运行时错误回滚，本轮对类型/数值先做预检，Redis OOM、持久化/故障切换和集群跨 slot 仍需真实验收。只有“保留标记且状态可用”的重复投递幂等；Redis flush/淘汰、回滚到旧 Writer 或 48 小时内丢失标记后重放可能改变热度，不宣称全局 exactly-once。超出标记保留期的旧事件凭发生时间拒绝再次加分。

## 恢复拓扑与观测

| 来源 | 业务队列 | 专用 Retry 队列 | DLQ |
|---|---|---|---|
| tweet.liked | queue.tweet.liked | queue.tweet.liked.retry.v1 | queue.tweet.liked.dlq |
| comment.created | queue.comment.created | queue.comment.created.retry.v1 | queue.comment.created.dlq |

Retry 发往 `timeline.retry` 的 `tweet.liked.timeline.retry` / `comment.created.timeline.retry`，1/2/4 秒 TTL 后只回 `timeline.ingress` 的对应 `.timeline` 路由；最多 3 次。DLQ 通过 `dlx.events.exchange` 对应来源 `.dlq`。复制原正文与元数据、递增 header、持久消息；确认发布成功后才 ACK 原消息。Confirm 失败先有界退避再保留原投递，不广播到业务 exchange。恢复最多追加 5 秒 Confirm/4 秒 fallback 等待。

新声明均为加法资源，保留既有队列和在途消息。本轮提供自动 Retry/DLQ，人工互动重放尚未开放；现有 Timeline/Publication 维护 CLI 继续只接受生命周期事件。创建投影延迟超过三次重试的互动仍保留 DLQ，待 M6-R2 受控重放，不悄悄丢弃。

`engagement_projection_events_total{kind,result}` 和 `engagement_projection_seconds{kind}` 使用固定标签，禁止用户/推文/EventKey/正文标签。结果覆盖 applied/duplicate/limited/no_topics/expired/malformed/failed/cancelled/ack_uncertain/consume_unavailable；故障 Trace 固定错误描述、日志不输出事件正文。`deploy/prometheus/rules/twitter-clone-engagement.yml` 给出失败、ACK 不确定和 DLQ 三个告警，实际 RabbitMQ exporter label 和告警通知须部署验收。

## 升级、回滚与后续

- 先停止所有旧互动消费者，确认旧在途请求退出；核验 Schema/Redis 容量、TTL 与持久化策略、MQ 绑定/最小权限；升级 Relay 兼容路径和 TweetService，再启动新 Consumer。禁止旧 ACK-before-write 与新实现混合运行；本轮未执行停服务、DDL、真实写入或部署。
- 若回滚，暂停领取并等待有界在途退出，保留 Outbox/位点、Retry/DLQ、Redis 去重与频控。旧消费者不读取新标记，不能直接恢复旧计分路径；保持队列积压，评估恢复方案后再执行。
- 热点主题集中于全局 ZSet，标记空间约为 7 天被消费事件数，需压测容量/淘汰策略；当前 Redis Client 为单节点，跨 slot Lua 不支持 Redis Cluster。
- 旧热度衰减仍是每副本分钟 ticker + 50 秒锁，本轮未证明多实例每时间桶只衰减一次。M6-R2 负责持久/时间桶围栏、专用受控重放、断连与崩溃/多实例真实矩阵。删除/隐藏不会在本轮撤销历史已计分热度。

离线证据：实际生产 Lua 的 miniredis 并发重复/ACK 丢失、共享频控/预检/TTL、乱序/隐藏/过期/冲突/主题上限；生产 GORM 内存 SQL 的 UoW/当前读/ClientFoundRows/回滚；Outbox 新关系与 Trace、MQ Confirm-before-ACK/失败/取消、指标/YAML。离线证据不代表 Redis AOF、MySQL 锁、Broker 绑定或真实崩溃恢复。
