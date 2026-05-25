# Language Rule

总是使用简体中文与我进行交流以及回复。

---

# File Permission Constraints

- 只能在当前项目根目录及其子目录内创建、修改、删除文件
- 禁止访问或修改项目目录之外的任何文件
- 禁止修改系统文件、用户配置文件
- 禁止使用 `-g` 全局安装任何包
- 危险命令（如 `rm -rf`）执行前必须确认
- 涉及删除、覆盖、大规模重构操作时必须先征求确认
- 违反规则时必须先告知用户并等待明确确认

---

# Repository Scanning Constraints

- 禁止无边界扫描整个仓库
- 分析前必须优先读取 `.agent/context/*`
- 大规模分析任务必须分阶段进行
- 单次任务必须限制在明确模块范围内
- 避免 recursive deep analysis 导致 context explosion
- 优先分析：
  - gateway
  - module
  - mq
  - agent
  - rag
- 禁止扫描：
  - node_modules
  - vendor
  - dist
  - build
  - logs
  - cache

---

# Global Engineering Principles

始终以大型生产级系统标准思考问题。

优先考虑：

- 可扩展性（Scalability）
- 可维护性（Maintainability）
- 可观测性（Observability）
- 故障隔离（Failure Isolation）
- 回滚能力（Rollback）
- 安全性（Security）
- 成本效率（Cost Efficiency）

避免：

- demo式实现
- 硬编码
- 强耦合
- 单点瓶颈
- 不可测试设计
- 隐式依赖
- 不可观测链路
- 巨型服务

做技术选型时必须分析：

- 为什么顶级厂商会/不会这样做
- 百万级用户下可能的问题
- 数据量扩大100倍后的瓶颈
- 如何支持未来 Agent/RAG/异步任务扩展
- 是否支持云原生部署
- 是否支持水平扩容
- 是否支持灰度与回滚

所有代码默认以：

- production-grade
- cloud-native
- observable
- scalable
- fault-tolerant

为目标进行设计。

避免只追求“功能能运行”，
更关注长期演化能力。

---

# Architecture Governance Rules

架构设计时必须：

- 优先考虑事件驱动（Event-Driven Architecture）
- 避免同步链路过长
- 避免跨服务事务
- 避免服务职责泄漏
- 避免 gateway 承担业务逻辑
- 服务之间必须边界清晰
- 服务必须具备独立演化能力

必须分析：

- 服务边界是否合理
- 是否存在未来耦合风险
- 是否存在链路爆炸风险
- 是否存在缓存雪崩风险
- 是否存在单点瓶颈
- 是否存在热点数据问题

涉及微服务变更时必须考虑：

- 服务发现
- 熔断降级
- retry
- timeout
- 限流
- backpressure
- rollback

---

# Event-Driven & MQ Rules

所有 MQ/事件驱动相关实现必须考虑：

- 幂等性（Idempotency）
- 重复消费
- poison message
- 消息堆积
- 顺序性
- retry storm
- DLQ（死信队列）

Consumer 必须：

- 可重试
- 可观测
- 可水平扩展
- 支持 graceful shutdown
- 支持 trace 透传

禁止：

- MQ 中直接写复杂业务逻辑
- 长事务 consumer
- 不可恢复异常导致消费阻塞

---

# Observability Rules

所有核心链路必须具备：

- structured logging
- metrics
- tracing
- audit log

必须支持：

- OpenTelemetry Trace
- Prometheus Metrics
- Grafana Dashboard
- 请求级 TraceID

异步链路必须：

- trace 可透传
- consumer 可追踪
- tool 调用可回放

禁止：

- silent failure
- 无日志错误
- 无 trace 的异步任务
- 不可观测 Agent 调用

---

# Production Code Quality Rules

默认要求：

- structured logging
- context timeout
- graceful shutdown
- retry strategy
- circuit breaker
- configuration externalization
- dependency injection
- interface abstraction
- unit testability

必须：

- error wrapping
- 明确错误边界
- 禁止吞错
- 所有 goroutine 必须可控退出

禁止：

- magic number
- hard-coded config
- panic-driven flow
- shared mutable state
- 全局状态污染
- 隐式初始化
- 不可测试代码

---

# Database & Storage Rules

数据库设计必须考虑：

- 索引策略
- 分页性能
- 热点数据
- 分库分表演化
- 读写分离
- cache consistency

禁止：

- N+1 Query
- 全表扫描
- 长事务
- 无索引排序
- 同步级联重计算

涉及 migration 时必须考虑：

- rollback
- backward compatibility
- online migration
- 灰度兼容

---

# Redis Rules

Redis 使用必须考虑：

- 缓存穿透
- 缓存雪崩
- 缓存击穿
- 热 key
- 大 key
- TTL 策略

禁止：

- Redis 作为永久存储
- 无 TTL cache
- 大量 keys scan
- 阻塞型操作

---

# Elasticsearch & Vector Retrieval Rules

向量检索/RAG 相关实现必须考虑：

- hybrid retrieval
- rerank
- embedding drift
- recall stability
- ANN latency
- index size growth

embedding pipeline 默认：

- 异步化
- 可重试
- 可监控
- 可回放

必须分析：

- ES 是否适合长期向量检索
- 是否需要独立 Vector DB
- HNSW 内存增长风险
- retrieval latency

禁止：

- 同步 embedding 写入主链路
- 大规模实时重建索引
- 无 fallback retrieval

---

# Agent & MCP Rules

Agent 系统必须：

- Tool Boundary 清晰
- MCP Tool 可观测
- Agent 调用可审计
- Tool 调用可回放
- Prompt 可追踪

必须考虑：

- context pollution
- tool injection
- hallucination
- runaway agent
- infinite retry
- token explosion

Multi-Agent 必须：

- 职责明确
- 上下文隔离
- 避免循环调用
- 避免共享污染 memory

禁止：

- Agent 直接越权调用数据库
- Agent 绕过 service boundary
- Agent 操作未审计资源

---

# Documentation Governance

## 1. 规划与实现跟踪（docs/PROJECT_PROGRESS.md）

- 每次完成功能模块后，自动更新对应任务状态（⬜→✅）
- 新增阶段时沿用现有表格格式
- 如果实现方案与原计划有偏差，必须记录偏差原因

## 2. 接口文档（docs/API_REFERENCE.md）

每次新增或修改接口后：

- 自动追加到对应分类
- 严格沿用现有格式：
  - 请求方法
  - 路径
  - 请求体
  - 响应体
- 必须包含：
  - 路径参数表格
  - JSON 示例

## 3. 问题追踪（docs/ISSUES.md）

当出现：

- 编译错误
- 测试失败
- runtime panic
- deployment failure

时：

- 自动追加问题记录
- 记录：
  - 问题
  - 原因
  - 解决方案
- 问题解决后必须更新状态

---

# Workflow Execution Rules

执行任何复杂任务时必须：

1. 先分析
2. 再规划
3. 再实施
4. 最后验证

涉及大型改动时：

必须先输出：

- architecture plan
- migration plan
- rollback plan
- risk analysis

禁止：

- 未分析直接重构
- 未验证直接覆盖
- 未评估直接改数据库
- 未确认直接删除文件

---

# Testing & Validation Rules

所有重要改动后必须：

- 编译通过
- 测试通过
- lint 通过
- 保证无明显 regression

新增功能时必须：

- 补充单元测试
- 补充 integration test（如适用）
- 验证 observability

禁止：

- 跳过错误
- 忽略失败测试
- 忽略编译警告

---

# Security Rules

必须考虑：

- JWT 安全
- 权限边界
- 敏感信息脱敏
- SQL 注入
- XSS
- SSRF
- CSRF
- Prompt Injection
- MCP Tool 越权

禁止：

- 明文密钥
- 硬编码 token
- 日志打印敏感信息
- 无权限校验接口

---

# Cost Efficiency Rules

设计方案时必须评估：

- CPU 成本
- 内存成本
- ES 存储成本
- 向量索引成本
- MQ 堆积成本
- 网络 IO 成本

避免：

- 无意义高频 embedding
- 无限制 context
- 大模型滥调用
- 无缓存 retrieval