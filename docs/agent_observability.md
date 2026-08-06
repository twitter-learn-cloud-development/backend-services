# Agent Runtime Observability

> 状态：P5 已完成的实现说明
> 最近核对：2026-07-18

## 1. 目标与边界

Agent 可观测性要回答四个问题：一次 Run 是否成功、耗时和预算消耗在哪里、模型与工具为何失败、历史证据能否在不重复副作用的前提下核验。

当前实现明确区分：

- Mongo Trace：租户隔离的查询事实源，保存 Run/Step/LLM/Tool 结构化记录。
- Redis Stream：长度和 TTL 有界的实时增量投递，不作为完整历史存储。
- OpenTelemetry：跨 Gateway、Agent、LLM Provider、MCP 和下游 gRPC 的因果链。
- Prometheus：低基数聚合指标和 SLI。
- Grafana：面向运行、模型和工具治理的操作面板。
- Replay：只读校验 Event/Snapshot/Compensation 证据，不调用 Scheduler、LLM 或 Tool。

Legacy 对话路径会随 Runtime v2 灰度逐步获得更细的 Step 信号；这不影响 Workflow 和 Runtime v2 的 P5 验收，也不能被描述为完整 Event Sourcing。

## 2. Trace 数据模型

| 记录 | 关键字段 | 用途 |
|---|---|---|
| `RunRecord` | run/workflow/user、mode、strategy、status、budget、usage、duration | 一次运行的总览与预算结果 |
| `StepRecord` | step、parent、type、sequence、attempt、status、duration | Runtime Step 或 Workflow Node 的执行顺序 |
| `LLMCallRecord` | provider/model、usage/cost、hash/length、template、sample status | 模型调用、费用和模板执行证据 |
| `ToolCallRecord` | tool/category/decision、attempt、hash/length、object reference | 工具治理、重试、熔断和大结果定位 |

所有 Mongo 读取均以 `(user_id, run_id)` 为所有权边界。API 在查询 Trace、事件、Replay 或 Blackboard 前先验证当前用户拥有 Run。

LLM Template 身份规则：

- Runtime Profile：使用版本化 Prompt Profile 的 ID/Version。
- Consult 内置模板：`consult.search.system` / `v1`。
- Workflow LLM 节点：`workflow.<workflow_id>.node.<node_id>`，Version 使用不可变 Revision ID，旧记录可回退为 `revision-N`。

Template 身份用于回答“当时执行了哪个模板版本”，不等同于 Prompt 发布、灰度、回滚或 A/B Test 系统。

## 3. Prompt/Completion 安全采样

默认行为只保存 SHA-256、UTF-8 字节长度、Usage 和成本，不保存正文。诊断预览通过独立开关启用，不复用 OTel Trace 采样率。

```env
AGENT_PROMPT_SAMPLING_ENABLED=false
AGENT_PROMPT_SAMPLE_RATIO=0.01
AGENT_PROMPT_SAMPLE_MAX_BYTES=512
```

安全规则：

1. 默认关闭；比例必须在 `[0,1]`。
2. 使用 Run/Step/内容哈希组成的稳定键确定性采样，多副本结果一致。
3. 预览默认 512 字节，硬上限 4096 字节；扫描默认最多 64 KiB，超出直接拒绝。
4. 疑似 API Key、Authorization/Bearer、Cookie、Token、密码、邮箱、手机号、身份证或带 Query/UserInfo 的 URL 直接拒绝。
5. 拒绝时不尝试“局部脱敏后保存”，避免规则遗漏造成残余泄露。
6. 样本只写租户隔离 Mongo Trace；禁止进入日志、OTel Attribute、Prometheus Label 或 Redis Key。

采样状态：

| 状态 | 含义 |
|---|---|
| `disabled` | 功能关闭 |
| `empty` | 内容为空 |
| `not_selected` | 未命中确定性采样比例 |
| `sensitive` | 命中保守敏感规则，未保存 |
| `oversized` | 超过扫描上限，未保存 |
| `captured` | 返回有界 UTF-8 预览 |

该机制是故障诊断预览，不是正式 DLP。生产环境若无明确数据治理、授权和保留期要求，应保持关闭。

## 4. OpenTelemetry

- Agent Service 使用 ParentBased OTLP gRPC TracerProvider，`AGENT_TRACE_SAMPLE_RATIO` 只控制 OTel Span。
- Agent gRPC Server 与 Tweet/User Client 使用 `otelgrpc`。
- Provider 和 MCP HTTP 通过可组合 Transport 注入 W3C `traceparent`。
- HTTP Span 只记录固定操作名、方法、协议、目标 Host/Port 和状态码。
- Path、Query、Header、Authorization、Prompt、Completion、Tool 参数/结果和原始网络错误文本不得进入 Span。

服务退出时限时 Flush。用户主动取消的 `context.Canceled` 不应升级为基础设施故障告警。

## 5. Prometheus 指标

Agent Service 在 `:9191/metrics` 暴露指标。Docker Prometheus 已抓取 `agent-service:9191`；Helm Deployment 暴露同名 metrics 端口和 Scrape Annotation。

核心指标：

```text
agent_runs_total
agent_run_duration_seconds
agent_steps_total
agent_step_duration_seconds
agent_llm_requests_total
agent_llm_duration_seconds
agent_llm_tokens_total
agent_llm_estimated_cost_micros_total
agent_tool_executions_total
agent_tool_duration_seconds
agent_tool_circuit_state
agent_tool_governance_reconciliations_total
agent_profile_experiment_observation_record_attempts_total
agent_profile_experiment_decisions_total
agent_unified_tasks_started_total
agent_unified_task_outcomes_total
agent_unified_task_duration_seconds
agent_unified_task_steps
agent_unified_task_tokens_total
agent_unified_task_estimated_cost_micros_total
agent_unified_task_tool_calls_total
agent_unified_task_citations_total
agent_unified_draft_events_total
agent_external_mcp_product_events_total
```

Label 仅允许有限枚举，例如 source、strategy、status、step_type、provider、direction、estimated、category、decision 和 error_code。禁止 `user_id`、`run_id`、`step_id`、Prompt、URL、模型自由文本或错误原文。

Unified Agent 产品指标以 Mongo `AgentExecutionRun` 的成功创建和 Revision CAS 提交为边界，仅在 `AGENT_RECOVERABLE_RUNS_ENABLED=true` 时产生样本。`execution_profile`、`strategy`、`outcome`、Tool `result`、Citation `source_type/validity` 都经过固定枚举归一化；动态 Tool 名、Citation ID/URL 和租户身份不会成为 Label。

草稿采纳和 Connector 漏斗由 Mongo `agent_product_events` 的确定性 append-only 事件投影。草稿 `ready` 只跟随已完成且 `publishable_draft=true` 的权威 Run，`published` 只跟随首次原子绑定的 Tweet；Connector `activated` 只表示首个已审核工具被显式启用，`first_used`/`reused` 只统计治理后成功执行且具有不同 Agent/Workflow Run ID 的调用。MCP Session Pool 打开或复用仅是技术信号，不能替代产品激活/复用。

常用口径：

```promql
# 统一入口任务完成率
sum(rate(agent_unified_task_outcomes_total{outcome="completed"}[5m]))
/
clamp_min(sum(rate(agent_unified_tasks_started_total[5m])), 1)

# 完成任务 P95 端到端耗时（包含人工挂起等待）
histogram_quantile(0.95,
  sum by (le) (rate(agent_unified_task_duration_seconds_bucket{outcome="completed"}[5m])))

# 每个完成任务的总 Token / 失败 Tool 调用
sum(rate(agent_unified_task_tokens_total{outcome="completed",direction="total"}[5m]))
/
clamp_min(sum(rate(agent_unified_task_outcomes_total{outcome="completed"}[5m])), 1)

sum(rate(agent_unified_task_tool_calls_total{outcome="completed",result="failed"}[5m]))
/
clamp_min(sum(rate(agent_unified_task_outcomes_total{outcome="completed"}[5m])), 1)

# 用户可见 Citation 结构有效率
sum(rate(agent_unified_task_citations_total{validity="valid"}[5m]))
/
clamp_min(sum(rate(agent_unified_task_citations_total[5m])), 1)

# 可发布草稿采纳率
sum(increase(agent_unified_draft_events_total{event="published"}[24h]))
/
clamp_min(sum(increase(agent_unified_draft_events_total{event="ready"}[24h])), 1)

# Connector 激活率 / 首用率 / 跨 Run 复用率
sum(increase(agent_external_mcp_product_events_total{event="activated"}[24h]))
/
clamp_min(sum(increase(agent_external_mcp_product_events_total{event="configured"}[24h])), 1)

sum(increase(agent_external_mcp_product_events_total{event="first_used"}[24h]))
/
clamp_min(sum(increase(agent_external_mcp_product_events_total{event="activated"}[24h])), 1)

sum(increase(agent_external_mcp_product_events_total{event="reused"}[24h]))
/
clamp_min(sum(increase(agent_external_mcp_product_events_total{event="first_used"}[24h])), 1)
```

`awaiting_human / started` 可观察澄清转换压力，但一个 Run 可多次挂起，因此它不是天然去重的“曾澄清任务占比”。任务级澄清率与工具选择准确率应使用固定标注数据集。Citation `validity=valid` 只证明 ID、类型和资源定位满足可信结构契约，不证明事实正确、来源仍在线或回答 Grounded；这些质量结论继续由版本化 Eval 与人工签认负责。

上述漏斗使用窗口内事件流量，适合观察趋势；严格 Cohort 转化分析应从授权后的 `agent_product_events` 读取主体关系，不能把 Prometheus Counter 比值解释为同一批用户的留存率。

## 6. Grafana 面板

Dashboard UID：`agent-runtime-ops`，Prometheus datasource UID：`prometheus`。

文件：

- Docker：`deploy/grafana/provisioning/dashboards/agent-runtime-dashboard.json`
- Helm：`deploy/helm/twitter-clone/dashboards/agent-runtime-dashboard.json`

16 个面板覆盖：

- Run Throughput、Success Rate、P95 和状态分布。
- Run/Step P95。
- LLM Request Rate、P95、Token 与估算成本速率。
- Tool Policy 决策、Tool P95、Open Circuit 数量与治理对账失败。

面板不提供租户或单 Run 下钻；详细定位应从鉴权后的运行控制台进入 Mongo Trace，避免把高基数身份注入 Prometheus。

## 7. 查询与故障定位

1. 从运行控制台读取 Run 摘要与独立 Trace。
2. 用 Run/Step 状态确认失败发生在模型、工具、审批、预算还是取消边界。
3. 用 Trace ID 在 Jaeger 查看跨 gRPC/HTTP 因果链。
4. 用 Grafana 判断是单次失败还是 Provider、Tool、熔断或对账的系统性异常。
5. 需要检查上下文时，使用版本化 Blackboard 查询；需要核验证据时，使用只读 Replay。
6. Prompt 采样未捕获时，根据 status 判断是关闭、未选中、敏感还是超长，不应临时改日志打印正文。

## 8. 验证入口

```powershell
$env:GOFLAGS='-mod=mod'
$env:GOCACHE='E:\GOProject\cloud\twitter-clone\tmp\go-build-cache'
go test ./internal/module/agent/observability ./internal/module/agent/service ./internal/module/agent/workflow/tool -count=1
go test -race ./internal/module/agent/observability ./internal/module/agent/service ./internal/module/agent/workflow/tool -count=1
go vet ./internal/module/agent/... ./internal/gateway/... ./pkg/ai ./pkg/trace ./cmd/...
```

配置收口还应验证两份 Dashboard JSON 可解析、Prometheus 抓取 Agent `9191`、Docker Compose 可展开，以及 Web 生产构建通过。
