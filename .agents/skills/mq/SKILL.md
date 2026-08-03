---
name: mq
description: 设计可重放、幂等、可退避和可观测的事件链路，避免重试风暴与静默丢失。用于 RabbitMQ、Outbox、Consumer、Canal、异步事件、重试或 DLQ 任务。
---

# MQ Skill

## 先读

- `internal/infrastructure/mq/`
- `internal/mq/producer/`、`internal/mq/consumer/`
- `internal/domain/outbox.go`、`outboxEvent.go`
- `cmd/consumer/`、`cmd/canal-relay/`

## 执行步骤

1. 定义 Event Name、Schema Version、Message ID、Aggregate ID 和发生时间。
2. 明确事实事务与事件发布的原子性：优先 Outbox，不做 DB 提交后“尽力 MQ”。
3. Consumer 先判幂等，再执行业务，再 Ack。
4. 将错误分类为永久、瞬时、下游过载；分别丢弃/DLQ、退避重试、暂停/限速。
5. 设计最大重试、DLQ、人工重放、顺序键和积压告警。
6. 透传 trace/correlation_id，记录处理时延和重试次数。

## 项目不变量

- 禁止把 `Nack(false, true)` 当通用重试；项目中现有位置是明确技术债。
- Consumer 必须支持重复投递，不依赖 exactly-once 幻觉。
- ES/Qdrant/Redis 派生写入必须可重放。
- Event Schema 只做向后兼容扩展；破坏性变化使用新版本/事件名。
- Consumer goroutine 和 Channel 必须在 Shutdown 时停止接收并完成/释放在途消息。

## 观测指标

- publish confirm failure、consume success/error、retry、DLQ。
- queue depth、oldest message age、processing latency。
- idempotency hit、poison message、下游 timeout。

## 验证

- 重复消息、乱序、永久错误、瞬时错误、Cancel/Shutdown。
- Outbox 事务与 Relay 重放测试。
- 不启动真实 RabbitMQ 的纯逻辑单测；协议/集成层再使用容器测试。
