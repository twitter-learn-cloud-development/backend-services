---
name: architecture-audit
description: Evidence-driven architecture audit workflow for this Go social platform and Agent Runtime. Use for architecture review, strengthening plans, service-boundary audits and technical-debt prioritization.
---

# Architecture Audit Workflow

## 1. 触发条件

仅在以下任务使用：

- 全局/模块架构评审。
- 大规模重构、服务拆分、数据迁移或阶段规划。
- 百万 DAU/大 V Fanout/Agent 多租户等容量推演。
- 技术债和面试叙事真实性审查。

普通 Bug、单接口或单组件修改不要启动全架构审计。

## 2. 必读与范围

1. `.agents/context/project_map.md`
2. `.agents/rules/production.md`
3. 与范围对应的 Context/Skill
4. `architecture_audit_report.md`（只作历史对照）
5. 当前代码、配置和测试

先声明范围：目标模块、用户场景、数据规模假设、明确不分析的目录。禁止无边界全仓扫描。

## 3. 证据账本

每个重要结论必须记录：

| 字段 | 含义 |
|------|------|
| Claim | 要证明的能力或风险 |
| Status | `Implemented` / `Partial` / `Planned` / `Missing` / `Contradicted` |
| Evidence | 当前文件、符号、配置、测试或运行输出 |
| Impact | 用户、SLA、成本、数据或安全影响 |
| Confidence | High / Medium / Low |

不得使用“文档写了”“截图看起来有”作为唯一实现证据。

## 4. 审计步骤

### Step A：当前拓扑

- 从 `cmd/*/main.go` 找组合根和进程职责。
- 从 Proto/Gateway 找同步调用链。
- 从 Repository/Infrastructure 找数据所有权。
- 从 MQ/Outbox/Consumer 找异步链路。
- 从 Runtime/Workflow/MCP 找 Agent 执行边界。

输出实际拓扑，不先画理想架构。

### Step B：边界与耦合

检查：

- Gateway/Agent 是否直接访问其他服务数据。
- 模块是否跨用 Repository 或共享表。
- Runtime 是否依赖基础设施。
- Workflow UI/DSL/Engine/Tool 是否语义一致。
- Temporal、本地 Scheduler、后台任务是否重复 owner。

### Step C：容量与故障

把“百万 DAU、千万 Feed、100 倍数据”当压力假设，不当现状。

- 第一瓶颈、第二瓶颈和级联故障路径。
- Redis hot/big key、MySQL hotspot/N+1、MQ backlog/retry storm。
- ES/Qdrant 写放大、索引增长和 Embedding 迁移。
- LLM token/cost/concurrency、Tool runaway、长会话内存。
- timeout、retry、backpressure、circuit breaker、graceful shutdown。

### Step D：一致性与恢复

- 事实源与派生索引。
- Outbox、幂等、DLQ、重放、对账。
- Workflow Checkpoint/Resume、Approval、写工具 Idempotency。
- Schema/Proto/Embedding/Provider Config 迁移和回滚。

### Step E：安全与可观测

- JWT/user_id/所有权、SQL/XSS/SSRF、Credential、Prompt/Tool Injection。
- Logs/Metrics/Trace/Audit 是否能关联 HTTP -> gRPC -> MQ -> Agent Run/Tool。
- Label 高基数、敏感信息和 Replay 数据保留。

## 5. 风险排序

风险评分至少考虑：

- Severity：用户/数据/SLA 后果。
- Likelihood：在当前负载和代码下发生概率。
- Blast Radius：单用户、单服务还是全局。
- Detectability：能否在用户投诉前发现。
- Migration Cost：修复复杂度与兼容风险。

优先级：

- P0：数据/安全/全局可用性，立即护栏。
- P1：近期规模或迭代会阻塞，进入下一阶段。
- P2：中期演进，先补观测/测试再重构。
- P3：优化项，不包装成阻塞问题。

## 6. 输出格式

### A. Executive Summary

- 当前成熟度与证据。
- 最大 3-5 个风险。
- 明确哪些能力只是 Planned/Partial。

### B. Findings First

按 P0 -> P3 输出，每项包含：Evidence、Failure Mode、Impact、Recommendation。

### C. Target Architecture

只画与发现对应的目标边界；说明保持不变的部分。

### D. Migration Plan

每阶段包含：代码范围、兼容策略、数据迁移、Feature Flag、验收、回滚。

### E. Verification

给出单测、契约、race、压测、故障注入、观测查询和容量指标。

### F. Context/Skill Updates

只有稳定结构或方法发生变化时才更新 `.agents`；每日进度写 Docs，不污染长期 Context。

## 7. 禁止事项

- 不基于真实代码给通用“大厂最佳实践”清单。
- 不把所有同步调用都机械改成 MQ。
- 不建议一次性重写或无迁移路径拆服务。
- 不把 Shared DB、Lock-based Blackboard、Per-user Collection 等现实隐藏在理想命名后。
- 不把测试未运行、外部依赖未验证的能力标记完成。
- 不为增加架构感而引入无收益的新中间件。
