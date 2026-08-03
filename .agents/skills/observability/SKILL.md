---
name: observability
description: 建立请求、异步事件与 Agent Run 可关联的低基数可观测信号。用于日志、Metrics、Trace、审计、告警、Jaeger、Prometheus、Loki、Pyroscope 或 Agent Replay 任务。
---

# Observability Skill

## 先读

- `pkg/logger/`、`pkg/metric/`、`pkg/trace/`、`pkg/profiler/`
- `deploy/prometheus/`、`deploy/grafana/`、`deploy/promtail/`
- 对应服务 `cmd/*/main.go` 的初始化和 Interceptor

## 三类信号

| 信号 | 必须回答 |
|------|----------|
| Logs | 发生了什么、在哪个 user/run/node/tool、错误边界是什么 |
| Metrics | 频率、延迟、错误率、饱和度、队列/预算是否接近上限 |
| Traces | HTTP/gRPC/MQ/LLM/Tool 的因果链和耗时分布 |

## 执行步骤

1. 先定义诊断问题和 SLI，再添加埋点，禁止为了“有指标”随意打点。
2. 复用 Context 中的 trace/span/run ID，不自行生成不可关联 ID。
3. Metric Label 只用低基数字段；禁止 user_id、tweet_id、prompt、error message 作为 Label。
4. Error 日志记录边界、分类和 wrap 后错误，不重复打印同一错误栈。
5. 异步事件在 Header 透传 Trace；Consumer 创建新 Span 并链接 Producer。
6. Agent 记录 Run/Step/Model/Tool/Usage/Estimated/Approval，正文与密钥默认不进入日志。

## 项目不变量

- OTLP gRPC 统一使用 `4317`，不要再配置 Jaeger 旧 HTTP Collector 端点。
- 正常 `context.Canceled`/用户主动取消不作为故障告警。
- Silent failure 禁止；降级必须有可查询的 Counter/日志。
- Prompt/Completion 诊断预览必须与 OTel 采样解耦，默认关闭、确定性、有扫描/预览硬上限，并对疑似密钥、凭证与直接身份标识 fail-closed；样本只进入租户隔离的 Mongo Trace，禁止写入日志、Span 或 Metric。
- Prompt Template ID/Version 是执行证据字段，不得放入 Prometheus Label，也不得据此宣称已经实现 Prompt 发布、灰度或 A/B 管理。
- 公开 Replay 是校验历史证据的只读路径，不调用 Scheduler、LLM 或 Tool；不得将其描述为任意副作用的自动重放。
- Agent Mongo TraceRecord 是查询事实源；Prometheus/OTel 通过 Recorder 扇出。Metric 禁止 model/user/run/step 等动态 Label，OTel Span 禁止 Prompt/Completion/Tool 正文与摘要值。
- Workflow Run 实时事件使用 Redis Stream 作为有界投递通道，必须按租户与 Run 隔离、支持稳定游标/重置/心跳/取消并设置长度和 TTL；Mongo Trace 仍是完整查询事实源，事件投递失败不得改变业务运行结果。
- 大型 Tool Result 正文不得进入 Trace。统一 Executor 先执行硬上限，超过内联阈值时写入私有对象存储；Trace 只保存哈希、长度、存储类型和无凭证引用。对象归档失败或关闭时必须 fail-closed，禁止降级写入 Mongo 大文档。
- Blackboard 调试查询必须使用租户隔离、完整性校验和版本稳定游标；敏感键、单值预览、页大小、字段数和重放事件数必须有界，禁止把完整状态复制进 Trace 或指标。
- HTTP Trace 只记录方法、协议、目标主机/端口和状态码；禁止 Path、Query、Header、正文及可能包含完整 URL 的原始错误。包装 Transport 时必须保留调用方 Endpoint Policy、Redirect、Timeout 与取消语义。

## 验证

- 单测 Metric Label、脱敏和 Context 传播。
- 本地通过 Jaeger/Prometheus 查询一次真实链路。
- 检查高基数、重复日志、无界 Payload、采样策略及 Grafana datasource/query 可加载性。
