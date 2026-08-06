# Agent 可观测性契约

## 信号边界

Agent 执行同时产生三类互补信号：

| 信号 | 事实源 | 用途 |
|------|--------|------|
| 执行记录 | Mongo `agent_*_traces` | 用户隔离查询、运行控制台、审计证据 |
| Metrics | Agent `:9191/metrics` | 频率、延迟、Token、成本和错误趋势 |
| OTel Span | OTLP gRPC Collector `:4317` | Gateway、Agent、下游 gRPC 与逻辑 LLM/Tool 因果链 |

Mongo 是执行追踪的查询事实源。Prometheus 与 OTel 由同一 `Recorder` 扇出产生，但遥测不可用不能改变 Agent 的业务结果。

## TraceRecord

- `RunRecord`：mode、strategy、status、usage、budget、duration。
- `StepRecord`：step type、sequence、attempt、status、duration。
- `LLMCallRecord`：最终 model/provider、usage、cost、duration、error class。
- `ToolCallRecord`：tool/category、治理 decision、attempts、duration。

记录按 `(user_id, record_id)` 唯一，查询按 `(user_id, run_id)` 隔离。HTTP 入口：

```text
GET /api/v1/agent/workflow-runs/:id/traces
```

## 隐私规则

- Prompt、Completion、Tool Arguments 与 Tool Result 正文默认不持久化、不写 Metric Label、不写 OTel Attribute。
- Mongo 仅保存 SHA-256、字节长度、Usage 和错误分类；OTel 只保存长度和结构化执行属性，不保存正文或摘要值。
- Prometheus 禁止 `user_id`、`run_id`、`step_id`、模型名、URL 和错误原文 Label。
- 用户自定义 Provider 归一为有限枚举；未知值归入 `unknown`。

## OTel 配置

```dotenv
JAEGER_COLLECTOR_ENDPOINT=localhost:4317
AGENT_TRACE_SAMPLE_RATIO=1
```

采样率限制为 `0-1`，使用 ParentBased TraceIDRatioBased。Agent gRPC Server 及 Tweet/User gRPC Client 已安装 `otelgrpc` StatsHandler，进程退出时最多等待 5 秒 Flush。

当前逻辑 Run/Step/LLM/Tool Span、gRPC 和 Provider/MCP HTTP 传播均已接入。HTTP Transport 注入 W3C `traceparent`，MCP 服务端提取上下文；Span 只记录方法、协议、目标主机/端口和状态码，不记录 Path、Query、Header、正文或原始网络错误。运行事件流、Grafana 面板和大 Tool Result 对象存储引用仍属于后续增量。

## 当前指标

```text
agent_runs_total{source,strategy,status}
agent_run_duration_seconds{source,strategy,status}
agent_steps_total{source,step_type,status}
agent_step_duration_seconds{source,step_type,status}
agent_llm_requests_total{source,provider,status}
agent_llm_tokens_total{source,provider,direction,estimated}
agent_llm_estimated_cost_micros_total{source,provider,estimated}
agent_unified_tasks_started_total{execution_profile,strategy}
agent_unified_task_outcomes_total{execution_profile,strategy,outcome}
agent_unified_task_duration_seconds{execution_profile,strategy,outcome}
agent_unified_task_steps{execution_profile,strategy,outcome}
agent_unified_task_tokens_total{execution_profile,strategy,outcome,direction,estimated}
agent_unified_task_estimated_cost_micros_total{execution_profile,strategy,outcome,estimated}
agent_unified_task_tool_calls_total{execution_profile,strategy,outcome,result}
agent_unified_task_citations_total{execution_profile,strategy,outcome,source_type,validity}
agent_unified_draft_events_total{execution_profile,strategy,event}
agent_external_mcp_product_events_total{scope,transport,event}
```

Tool 指标由统一 ToolExecutor 产生，避免在 Trace Recorder 中重复计数。

`agent_unified_*` 是 P8.5 产品 SLI 投影：任务创建和终态只跟随权威 `AgentExecutionRun` 成功写入，不把未提交结果计为完成。按 `outcome=completed` 可计算单个完成任务的延迟、Token、微单位成本和失败 Tool 数；线上 `awaiting_human` 只表示澄清转换，任务级工具选择准确率/澄清率仍由带 Ground Truth 的固定 Eval 数据集计算。

Citation 有效性是无网络 I/O 的结构校验：平台推文要求 ID 与内部路径一致，网页要求 HTTP(S) URL 与稳定 Source ID 一致。该指标不声称事实正确或来源持续可访问。所有产品指标 Label 都是固定枚举，不包含 Tool Name、Citation ID、URL、用户、Run、模型或正文。

草稿与 Connector 漏斗来自确定性 append-only `agent_product_events`。草稿 `ready` 绑定 completed 且可发布的权威 Run，`published` 绑定首次原子记录的 Tweet；Connector `activated` 只在首个已审核工具显式启用时产生，`first_used` 只在治理调用成功后产生，`reused` 要求同一 Connector 在至少两个不同 Agent/Workflow Run 中成功执行。对应趋势口径为 `published/ready`、`activated/configured`、`first_used/activated` 和 `reused/first_used`；连接池复用不是产品复用。严格 Cohort 分析读取授权后的产品事件，不使用 Prometheus 高基数 Label。
