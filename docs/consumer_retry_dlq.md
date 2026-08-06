# Consumer Retry/DLQ Runbook

本文说明 Timeline、Agent 风控和 Profile 内容互动消费者的失败语义。它是部署与故障演练入口，不代表真实 RabbitMQ 已完成验收。

## 1. 所有权与拓扑

| 链路 | 主队列 | Retry | DLQ | 原始路由 |
|------|--------|-------|-----|----------|
| Timeline 创建 | `queue.tweet.fanout` | `queue.tweet.fanout.retry.v2` | `queue.tweet.fanout.dlq` | `twitter.events/tweet.created` |
| Timeline 删除 | `queue.tweet.delete` | `queue.tweet.delete.retry.v2` | `queue.tweet.delete.dlq` | `twitter.events/tweet.deleted` |
| Timeline 治理 | `queue.tweet.moderation.cleanup` | `queue.tweet.moderation.cleanup.retry.v2` | `queue.tweet.moderation.cleanup.dlq` | `twitter.events/tweet.moderated` |
| Agent 风控 | `queue.tweet.risk` | `queue.agent.risk.retry.v1` | `queue.agent.risk.dlq.v1` | `twitter.events/tweet.created` |
| Profile 内容互动 | `queue.agent.profile.content-engagement.v1` | 点赞/评论各自的 `.retry.v1` 队列 | `queue.agent.profile.content-engagement.dlq.v1` | `tweet.liked` / `comment.created` |

`queue.tweet.risk` 由 Agent Worker 声明和消费。它直接订阅原始 `tweet.created`，Timeline Consumer 不拥有该队列，也不发布衍生风控事件。风控 Retry Queue 只死信回 `agent.risk.ingress`，禁止回到 `twitter.events`。

Timeline 首次投递仍从 `twitter.events` 进入。新的失败重试只发布到 `timeline.retry`，版本化 `.retry.v2` 队列到期后死信回 `timeline.ingress`，因此不会再次广播给原始事件订阅者。旧 `.retry` 队列不原地改参数也不自动删除，仅用于滚动升级时排空已有消息。

## 2. 失败语义

1. 业务瞬时失败按 1、2、4 秒 TTL 进入专用 Retry Queue，最多三次。
2. 格式错误、非法 Retry Header 或重试耗尽的消息进入专用 DLQ。
3. Retry/DLQ 发布使用独立 Publisher Channel，并在 Broker Confirm 成功后 ACK 原消息。
4. Confirm 发布失败时不 ACK；消费者先做有界等待，再 `Nack(requeue=true)`。该路径只保护失败路由基础设施故障，不承担业务重试计数。
5. 进程关闭时，尚未提交的消息直接 requeue，避免关机窗口丢失。
6. ACK 返回错误视为 `acknowledgement_uncertain`。处理和发布可能已经成功，排障时必须按幂等键核对，不得直接假定消息丢失。

Temporal 风控使用固定 `RiskControl-Tweet-{tweet_id}` Workflow ID 和 `REJECT_DUPLICATE`。滚动升级期间旧实例可能仍发布历史衍生路由，重复事件会在工作流启动边界被拒绝并 ACK。

## 3. 受控检查与重放

- Timeline 创建/删除 DLQ 使用 `cmd/timeline-event-dlq-replay`，详见 `docs/timeline_event_dlq.md`。重放只回 `timeline.ingress`。
- Timeline 治理 DLQ 使用 `cmd/timeline-moderation-dlq-replay`，详见 `docs/timeline_moderation_dlq.md`。重放同样只回 `timeline.ingress`。
- Profile 内容互动 DLQ 使用 `cmd/agent-profile-dlq-replay`，详见 `docs/agent/PROFILE_RELEASES.md`。
- Agent 风控 DLQ 使用 `cmd/agent-risk-dlq-replay`，详见 `docs/agent_risk_dlq.md`。重放只回 `agent.risk.ingress`，不会重新广播原始 `tweet.created`。

日志和报告不得输出事件正文、凭据或用户输入原文；Prometheus Label 不得包含用户、Tweet、Run、Workflow ID 或错误原文。

## 4. 真实环境验收

在受控环境逐项记录证据：

1. 注入一次和三次可恢复业务失败，确认 TTL 分别为 1/2/4 秒且最终只处理一次。
2. 注入毒消息和非法 Retry Header，确认直接进入正确 DLQ，后续正常消息不被阻塞。
3. 在发布 Retry/DLQ 时中断 Confirm、关闭 Channel 和恢复 Broker，确认原消息只在有界等待后 requeue。
4. 在 Confirm 成功、ACK 返回错误窗口核对下游幂等结果和重复投递行为。
5. 使用 Timeline 与风控命令执行检查、限量重放和 ACK 不确定演练，确认报告无 Tweet/作者 ID、正文和审计输入原文。
6. 验证 Timeline Retry/人工重放只进入 `timeline.ingress`，不再触发 Agent 风控与其他原始事件订阅者；记录旧 Retry Queue 排空证据。
7. 滚动升级旧、新 Agent Worker，确认双链路重复由稳定 Temporal Workflow ID 吸收。
8. 运行多副本并观察队列深度、`publish_failed`、`requeued`、`acknowledgement_uncertain` 和 DLQ 指标。

以上证据未完成前，技术债状态保持 `Partial`。
