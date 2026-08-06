# Timeline 创建/删除 DLQ Runbook

`cmd/timeline-event-dlq-replay` 只处理 Timeline 创建或删除投影的死信消息。`--event` 必须显式指定：

| Event | DLQ | 允许的 DLQ Routing Key | 重放目标 |
|-------|-----|------------------------|----------|
| `created` | `queue.tweet.fanout.dlq` | `tweet.created.dlq` | `timeline.ingress/tweet.created.timeline` |
| `deleted` | `queue.tweet.delete.dlq` | `tweet.deleted.dlq` | `timeline.ingress/tweet.deleted.timeline` |

禁止把人工重放发布回 `twitter.events`。该交换机是原始业务事件总线，重放到那里会再次触发 Agent 风控及其他订阅者。

## 默认检查

```powershell
go run ./cmd/timeline-event-dlq-replay --event created --limit 20 --max-replays 1
go run ./cmd/timeline-event-dlq-replay --event deleted --limit 20 --max-replays 1
```

默认模式不会声明或修改 RabbitMQ 拓扑，也不会启用 Publisher Confirm。命令使用手动 ACK 的 `basic.get` 有界读取 1-100 条消息，校验固定 Routing Key、1 MiB 正文上限、事件最小身份契约和累计人工重放次数，输出脱敏报告后将整批消息重新入原 DLQ。

报告只包含消息摘要、事件身份摘要、固定 Outcome/Error Code，以及可选操作人/原因摘要；不包含 Tweet ID、作者 ID、推文正文或审计输入原文。检查会改变消息重新入队后的相对顺序，禁止并行运行多个检查命令。

## 显式重放

先确认故障已解除并创建变更记录：

```powershell
$env:DLQ_REPLAY_OPERATOR = "on-call@example.com"
go run ./cmd/timeline-event-dlq-replay `
  --event created `
  --execute `
  --limit 10 `
  --max-replays 1 `
  --reason "incident-1234 timeline recovery"
```

执行模式会按以下顺序失败关闭：

1. 在同一 RabbitMQ Channel 幂等声明 `timeline.ingress`、`timeline.retry`、三个 Timeline 主队列的专用绑定和版本化 `.retry.v2` 队列。
2. 启用 Publisher Confirm；任一步失败时不读取 DLQ。
3. 校验每条消息，移除自动 Retry 与 Broker Dead-letter Header，增加独立人工重放次数和时间 Header。
4. 只发布到所选事件的 Timeline 专用 Ingress。
5. Broker Confirm 成功后才 ACK 原 DLQ 消息；发布失败会放回当前及剩余批次并以非零状态退出。

操作人和原因在执行模式下必须非空；报告只保留二者 SHA-256。非法路由、毒消息、非法 Header 和达到累计上限的消息继续保留在 DLQ。

## ACK 不确定与幂等边界

若新消息已获 Confirm，但原 DLQ ACK 失败，报告返回：

```json
{
  "outcome": "acknowledgement_uncertain",
  "error_code": "ack_failed"
}
```

这代表新消息可能已进入 Timeline 主队列，同时原消息仍可能留在 DLQ。创建事件现在按以下边界吸收重复：

- Timeline ZSet 使用相同 Tweet ID 的 `ZADD`，重复写不增加成员。
- 趋势投影以 Tweet ID 保存 72 小时 Redis Marker，在同一个 Lua 中写主题映射、限频计数和趋势分值；Marker 命中时不重复计分。
- `sync_es` 使用唯一 `timeline:sync_es:tweet:{tweet_id}:v1` 去重键；完成任务作为 Success 收据保留 72 小时后有界清理。

因此自动重试、Consumer 重启和通常的 ACK 不确定窗口可以安全重投，但这不是无限期 exactly-once。Outbox Worker 使用 MySQL 8 原子 Claim、Owner/Token 围栏和 90 秒 Lease，多个 Consumer 副本不会同时领取同一 Attempt；60 秒执行超时后由过期恢复重新开放任务，旧 Token 不能提交结果。超过 72 小时的人工重放，以及 ES/Qdrant 已成功但 Outbox 回执提交失败后的 Embedding/网络调用成本仍需先对账。不要并发重复执行同一 DLQ 批次；按消息摘要核对 Consumer Stage/Outbox 指标、Timeline 投影、趋势 Marker 与 `outbox_tasks`，并保留事故记录。

Publisher Confirm 与预先声明的持久 Queue/Binding 证明 Broker 已接受并可路由消息，不证明 Consumer 已完成业务处理。

## 版本化迁移与回滚

- 新失败进入 `timeline.retry` 下的 `.retry.v2` 队列，TTL 到期只回 `timeline.ingress`，不会重放原始业务事件总线。
- 旧 `.retry` 队列不改参数、不删除；升级前已在其中的消息仍按旧拓扑自然排空。排空完成并留存证据后再由独立变更删除旧队列。
- 回滚旧 Consumer 前不得删除 `timeline.ingress`、`timeline.retry` 或 `.retry.v2`；其中已有消息依赖持久绑定返回共享主队列。
- 滚动升级期间新旧实例共享主队列，处理语义仍是至少一次。

## 生产验收边界

代码和 Fake 测试不等于真实 Broker 验收。技术债在以下演练完成前保持 `Partial`：

- 1/2/4 秒自动 Retry 只回 Timeline，不触发 Agent 风控或其他原始事件订阅者。
- 旧 Retry Queue 在滚动升级中自然排空且没有新消息进入。
- 默认检查不丢消息，显式重放只进入专用 Ingress。
- Confirm 超时、Channel 中断、拓扑权限不足和 ACK 不确定窗口。
- 多 Consumer 副本、Redis 故障、72 小时去重窗口边界、Lease 过期/旧 Token 拒绝，以及外部索引成功但数据库回执失败的搜索同步成本对账。
