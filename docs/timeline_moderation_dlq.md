# Timeline Moderation DLQ Runbook

`cmd/timeline-moderation-dlq-replay` 只处理影子治理投影清理死信队列：

- Queue：`queue.tweet.moderation.cleanup.dlq`
- 允许的 DLQ Routing Key：`tweet.moderated.dlq`
- 重放目标：`timeline.ingress` / `tweet.moderated.timeline`

检查模式不会声明 Exchange 或 Queue。执行模式会先在同一 RabbitMQ Channel 幂等声明 Timeline 专用 Ingress/Retry v2 拓扑，再启用 Publisher Confirm；拓扑、权限或 Confirm 初始化失败时不读取 DLQ。

## 默认检查

```powershell
go run ./cmd/timeline-moderation-dlq-replay --limit 20 --max-replays 1
```

默认模式只做以下操作：

1. 使用手动 ACK 的 `basic.get` 有界读取最多 `limit` 条消息。
2. 校验 Routing Key、事件大小、`TweetModeratedEvent` 契约和累计重放次数。
3. 输出脱敏 JSON 报告。
4. 将所有已检查消息 `Nack(requeue=true)` 放回原 DLQ。

检查模式不会启用 Publisher Confirm，也不会向业务 Exchange 发布消息。报告不包含 Tweet ID、作者 ID、事件正文、操作人或原因原文；可关联字段统一使用 SHA-256。

RabbitMQ 的重新入队可能改变 DLQ 消息的相对顺序。不要并行运行多个检查命令，也不要把队列顺序作为事故审计证据。

## 显式重放

先在变更单中确认影响范围，再设置非空操作人和非敏感原因：

```powershell
$env:DLQ_REPLAY_OPERATOR = "on-call@example.com"
go run ./cmd/timeline-moderation-dlq-replay `
  --execute `
  --limit 10 `
  --max-replays 1 `
  --reason "incident-1234 projection recovery"
```

执行模式的硬性约束：

- `limit` 范围为 1-100；`max-replays` 范围为 1-10。
- `--operator`/`DLQ_REPLAY_OPERATOR` 与 `--reason` 均为必填；报告仅保存它们的 SHA-256。
- 非法路由、坏事件、非法重放 Header 和达到累计上限的消息继续保留在 DLQ。
- 合格消息会移除旧 `x-retry-count` 及 Broker Dead-letter Header，增加独立累计重放 Header，然后持久化发布到 Timeline 专用 Ingress；不会重放原始事件总线。
- 只有 RabbitMQ Publisher Confirm 成功后才 ACK 原 DLQ 消息。
- 发布失败会重新入队当前及尚未处理的消息，并以非零状态退出。

## ACK 不确定窗口

若 Broker 已确认新消息，但原 DLQ 消息 ACK 失败，报告会返回：

```json
{
  "outcome": "acknowledgement_uncertain",
  "error_code": "ack_failed"
}
```

此时新事件可能已经进入正常队列，而原消息也可能仍在 DLQ。不要立即提高重放上限；先检查 Consumer 日志、Redis 完成标记和队列状态。治理清理使用事件完成标记和幂等 Lua，重复投递不会重复递减未读计数，但仍应保留事故记录。

## 报告结果码

| Outcome | Error Code | 含义 |
|---------|------------|------|
| `eligible` | - | 检查模式下可重放 |
| `replayed` | - | Confirm 与原消息 ACK 均成功 |
| `retained_invalid` | `unsupported_routing_key` / `malformed_event` / `oversized_event` / `invalid_replay_count` | 消息不满足治理事件契约，保留在 DLQ |
| `retained_replay_limit` | `replay_limit_reached` | 已达到人工重放上限，保留在 DLQ |
| `retained_publish_failed` | `publish_failed` | 发布未获 Broker Confirm，原消息重新入队 |
| `acknowledgement_uncertain` | `ack_failed` | 发布已确认但原消息 ACK 结果不确定 |

## 生产验收边界

单元测试只证明代码语义。技术债在以下受控演练完成前保持 `Partial`：

- 三次分类重试后真实进入 DLQ。
- 默认检查不丢消息。
- 显式重放在 RabbitMQ Confirm 成功后只回到 Timeline 业务队列。
- Confirm 超时、连接中断和 ACK 不确定窗口。
- 多 Consumer 副本、Redis 页间故障与 KEDA 扩缩容。
