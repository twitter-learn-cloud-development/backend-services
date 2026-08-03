---
name: timeline
description: 在当前写扩散基础上维护稳定 Cursor、缓存一致性，并为 Hybrid Fanout 演进保留边界。用于 Feed、Timeline、发推分发、Redis ZSet、热门流、无限滚动或大 V Fanout 任务。
---

# Timeline Skill

## 先读

- `internal/module/tweet/`
- `internal/module/follow/`
- `internal/mq/consumer/timeline_consumer.go`
- `internal/module/tweet/cache/timeline_cache.go`
- Gateway/Web/Mobile 对应 Feed API

## 当前事实

- Timeline 主要使用 RabbitMQ Consumer 向 Redis ZSet 写扩散。
- 推文详情事实源是 MySQL；Redis 是可重建缓存/索引。
- ES/Qdrant 同步属于异步派生链路。
- 大 V Hybrid Fanout 尚未完整实现，不得描述成已上线。

## 执行步骤

1. 明确 Feed 类型：For You、Following、User、Replies、Media、Trending。
2. 使用稳定的 Snowflake/时间 + ID Cursor，禁止 Offset 深分页。
3. 定义可见性、删除、Shadowban、Block/Follow 过滤在哪一层执行。
4. 写扩散使用 Pipeline/批量、Fanout 上限、背压和积压告警。
5. 读路径批量 Hydrate User/Tweet，避免 N+1。
6. 无限滚动返回 `next_cursor/has_more`，不是一次拉取所有推文。
7. 可重试、审批恢复或后台发布调用必须生成稳定的用户级 `idempotency_key`；Tweet、Poll、Outbox 与幂等绑定必须共享事务。

## Hybrid 演进

- 普通用户写入 Follower Inbox。
- 大 V 只写 Author Outbox，读时与 Inbox 多路归并。
- 阈值基于粉丝量、写入成本和热点动态配置。
- 删除/可见性变更必须同时处理 Inbox/Outbox 派生数据。

## 反模式

- 为大 V 同步遍历百万 Follower。
- Redis `KEYS/SCAN` 参与在线请求。
- 无 TTL/无上限 Timeline ZSet。
- 每条 Feed 单独查 User/Tweet。
- Consumer 错误后立即无限 requeue。

## 验证

- Cursor 无重复/无漏项、删除与可见性、空页、并发新推文。
- Consumer 重复投递与积压、Redis 故障降级。
- 发布端同键同输入回放、同键异输入冲突、事务失败不返回成功。
- 压测区分 publish latency、fanout throughput、feed hydration latency。
