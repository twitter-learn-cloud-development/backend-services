---
description: Production-grade architecture audit workflow for microservice, RAG, MCP and Agent systems. Analyze scalability, observability, async/event-driven architecture, service boundaries and future evolution risks based on real project structure.
---

# Architecture Audit Workflow

请以世界级互联网架构、生产级微服务系统、AI Agent Engineering 标准，
重新审视整个项目。

目标不是“功能是否能运行”，
而是分析系统在未来：

- 百万 DAU
- 千万级 Feed
- 百亿级数据
- 多 Agent 协作
- 长期迭代演化

下的：

- 稳定性
- 可扩展性
- 可维护性
- 故障隔离能力
- 可观测性
- 架构治理能力

---

# Core Audit Principles

必须始终以：

- production-grade
- cloud-native
- observable
- scalable
- fault-tolerant
- event-driven

标准进行分析。

避免：

- demo-style thinking
- tutorial-style suggestion
- superficial optimization
- 泛泛而谈

所有分析必须：
- 基于当前真实项目结构
- 基于当前真实模块职责
- 基于当前真实技术栈
- 基于真实工程演化风险

禁止只给理论答案。

---

# Audit Scope

重点分析：

## 1. Service Boundary

分析：

- 当前服务职责是否合理
- 是否存在职责泄漏
- 是否存在跨服务耦合
- 是否存在共享数据库风险
- 是否存在未来演化冲突
- Gateway 是否承载业务逻辑
- 是否存在隐藏的单体化趋势

---

## 2. Scalability & High Concurrency

基于：

- 百万级 DAU
- 千万级时间线
- 百亿级推文数据

分析：

- 哪些模块会先成为瓶颈
- Redis 是否存在热Key风险
- ES 是否存在写扩散问题
- MQ 是否存在积压风险
- 是否存在同步链路爆炸
- 是否存在缓存击穿/雪崩
- 是否存在数据库热点问题
- fanout 模型是否可持续

必须指出：
- 第一崩点
- 第二崩点
- 最危险扩展风险

---

## 3. Agent & MCP Architecture

分析：

- MCP Tool 边界是否清晰
- Tool 是否具备权限隔离
- Agent 是否可能失控
- 是否存在 Tool Injection 风险
- Multi-Agent 是否存在上下文污染
- 是否缺少 Agent Memory 治理
- 是否缺少 Skill Layer
- Agent orchestration 是否会失控

分析：
当前 Agent Service 是否属于：
- demo-agent
- production-agent
- orchestrated-agent
- autonomous-agent

并说明原因。

---

## 4. RAG & Semantic Retrieval

分析：

- 当前向量检索架构是否合理
- ES 是否适合作为长期向量数据库
- embedding 写入是否应该异步化
- 是否缺少 rerank
- hybrid retrieval 是否合理
- 当前召回链路是否稳定
- embedding drift 如何治理
- 多语言语义空间是否一致

分析：
未来数据扩大100倍后：
- ES
- embedding pipeline
- vector indexing

会出现什么问题。

---

## 5. Observability

检查：

- Trace 是否贯穿异步链路
- 是否缺少 metrics
- 是否缺少 structured logging
- 是否缺少 audit log
- Agent 调用是否可追踪
- Tool 调用是否可回放
- MQ 消费是否可观测
- 是否存在 blind spot

指出：
最危险的 observability 缺口。

---

## 6. Event-Driven Architecture

分析：

- 哪些同步调用应改为异步事件
- MQ 是否存在重复消费风险
- Consumer 是否具备幂等性
- 是否存在 poison message 风险
- 是否存在 retry storm 风险
- 是否缺少 DLQ
- 是否存在最终一致性问题

分析：
当前事件模型是否具备：
- 长期扩展能力
- 多 Consumer 演化能力
- 事件治理能力

---

## 7. Production-Grade Review

指出：

- 哪些实现属于 demo-style
- 哪些代码未来会成为技术债
- 哪些模块缺少治理
- 哪些服务未来最容易失控
- 哪些地方不符合大型互联网架构标准
- 哪些地方会导致未来重构灾难

---

## 8. Skill / Rule Extraction

基于真实项目结构：

为每个核心服务提炼：

- architecture skill
- anti-pattern
- scalability constraint
- observability requirement
- async guideline
- MCP boundary
- event-driven constraint
- future evolution risk

必须结合：
当前真实代码结构分析。

不要泛泛输出。

---

# Output Requirements

最终输出：

## A. System Risk Report

包括：

- 当前最大技术债
- 最大扩展性风险
- 最大稳定性风险
- 最大耦合风险
- 最大 Agent 风险

---

## B. Service-by-Service Analysis

针对每个核心服务输出：

- 当前职责
- 风险
- scalability risk
- observability gap
- anti-pattern
- future evolution issue

---

## C. Production Refactor Priority

给出：

- 必须立即重构
- 应该中期治理
- 可后期优化

三个优先级。

---

## D. Rule Extraction

自动生成建议：

- global rules
- workspace rules
- service rules

---

## E. Skill Extraction

自动生成建议：

- timeline.skill
- mq.skill
- rag.skill
- gateway.skill
- agent.skill
- observability.skill

并说明：
每个 skill 的职责。

---

# Important

不要输出教程。

不要只讲理论。

不要只给“最佳实践”。

必须：

- 基于真实项目结构
- 基于真实工程问题
- 基于真实生产系统演化

进行分析。