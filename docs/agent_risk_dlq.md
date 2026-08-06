# Agent Risk Control DLQ Runbook

`cmd/agent-risk-dlq-replay` 只处理 Agent 风控死信队列：

- Queue：`queue.agent.risk.dlq.v1`
- 允许的 DLQ Routing Key：`tweet.created.agent-risk.dlq`
- 重放目标：`agent.risk.ingress` / `tweet.created.agent-risk`

重放目标是 Agent 专用入口，不是主 `twitter.events/tweet.created`。因此人工恢复不会再次触发 Timeline、索引或其他原始事件消费者。

该命令不会声明 Exchange、Queue 或 Binding。执行前必须在 RabbitMQ 管理面核对 `agent.risk.ingress`、`queue.tweet.risk` 和 `tweet.created.agent-risk` Binding 均存在；不存在时先恢复 Agent Worker 拓扑，再运行命令。Publisher Confirm 证明 Broker 接受发布，但在 Binding 被管理员删除时不能单独证明消息已路由，因此不能跳过该前置检查。不要手工 Purge 或绕过校验搬运消息。

## 默认检查

```powershell
go run ./cmd/agent-risk-dlq-replay --limit 20 --max-replays 1
```

默认模式：

1. 使用手动 ACK 的 `basic.get` 有界读取最多 `limit` 条消息。
2. 校验 Routing Key、1 MiB 消息上限、Tweet/作者非零身份和累计人工重放次数。
3. 输出脱敏 JSON 报告。
4. 将所有已检查消息 `Nack(requeue=true)` 放回原 DLQ。

检查模式不会启用 Publisher Confirm，也不会发布消息。报告不包含 Tweet ID、作者 ID、推文正文、操作人或原因原文；消息、Workflow 身份和审计字段仅保存 SHA-256。

重新入队可能改变 DLQ 消息相对顺序。不要并行运行多个检查/重放命令，也不要把队列顺序当成审计证据。

## 显式重放

先确认 Temporal 与 Agent Worker 状态，再填写变更单中的操作人和非敏感原因：

```powershell
$env:DLQ_REPLAY_OPERATOR = "on-call@example.com"
go run ./cmd/agent-risk-dlq-replay `
  --execute `
  --limit 10 `
  --max-replays 1 `
  --reason "incident-1234 risk workflow recovery"
```

执行模式约束：

- `limit` 范围为 1-100；`max-replays` 范围为 1-10。
- `--operator`/`DLQ_REPLAY_OPERATOR` 与 `--reason` 都必须非空；报告只保存 SHA-256。
- 非法路由、坏事件、超大事件、非法 Header 和达到累计上限的消息继续保留在 DLQ。
- 合格消息移除自动重试 Header 与 Broker Dead-letter Header，增加独立人工重放次数和时间戳，同时保留 Trace/Correlation Header。
- 只有专用 Ingress 发布获得 RabbitMQ Publisher Confirm 后，原 DLQ 消息才会 ACK。
- 发布失败或命令取消会重新入队尚未安全处理的消息，并以非零状态退出。

## 重复与 ACK 不确定窗口

风控 Workflow ID 固定为 `RiskControl-Tweet-{tweet_id}`，启动策略使用 Temporal `REJECT_DUPLICATE`。如果重放消息对应的 Workflow 已存在，在线 Consumer 会把 AlreadyStarted 视为重复并 ACK，不会启动第二条风控工作流。

若 Broker 已确认专用 Ingress 消息，但原 DLQ 消息 ACK 失败，报告返回：

```json
{
  "outcome": "acknowledgement_uncertain",
  "error_code": "ack_failed"
}
```

此时先按报告中的 `workflow_identity_sha256` 核对 Temporal 运行和 Consumer 固定结果码，再检查原 DLQ 是否仍有该消息。不要立即提高重放上限；稳定 Workflow ID 能吸收重复启动，但重复投递仍会消耗队列和 Worker 资源。

## 报告结果码

| Outcome | Error Code | 含义 |
|---------|------------|------|
| `eligible` | - | 检查模式下可重放 |
| `replayed` | - | Confirm 与原消息 ACK 均成功 |
| `retained_invalid` | `unsupported_routing_key` / `malformed_event` / `oversized_event` / `invalid_replay_count` | 消息不满足风控契约，保留在 DLQ |
| `retained_replay_limit` | `replay_limit_reached` | 达到累计人工重放上限，保留在 DLQ |
| `retained_publish_failed` | `publish_failed` | 发布未获 Broker Confirm，当前及剩余消息重新入队 |
| `acknowledgement_uncertain` | `ack_failed` | 发布已确认但原消息 ACK 结果不确定 |

## 真实环境验收

代码与 Fake 测试不等于生产证据。技术债在以下受控演练完成前保持 `Partial`：

1. Temporal 不可用导致三次 1/2/4 秒重试后真实进入风控 DLQ。
2. 默认检查不启用 Confirm、不发布消息且消息数量不减少。
3. 删除专用 Binding 的负向演练必须阻止人工执行；恢复 Binding 后，显式重放只进入 Agent 专用 Ingress，不增加 Timeline/索引事件。
4. Confirm 超时、Channel 中断、命令取消和 ACK 不确定窗口。
5. 已存在 Workflow 的重复重放被 `REJECT_DUPLICATE` 吸收。
6. 多 Agent Worker 副本及滚动升级期间的队列深度、失败率和恢复时间。
