mq.skill (消息队列技能)
职责：系统异步化解耦和最终一致性保障。
规范：
1. 负责实现生产者确认模式（Publisher Confirms）
2. 消费者幂等性判重（利用 Redis SetNX 存储 Message ID）
3. 指数退避重试（Exponential Backoff）
4. DLQ 分流监控