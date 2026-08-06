# ADR-003: 外部 MCP Session Pool 与主动健康巡检

- 状态：Accepted
- 日期：2026-07-27
- 范围：`internal/module/agent/mcp/remote`

## 背景

外部 MCP 的 Discovery、Ping 和 Tool Call 原先每次都执行 Start、Initialize、Close。该模型隔离清晰，但在 SSE 或需要会话初始化的 Streamable HTTP Server 上会放大握手延迟、连接数和代理负载。另一方面，仅在用户点击 Discovery 或执行 Tool 时才发现连接故障，无法提供稳定的运营信号。

连接复用不能弱化 Endpoint Policy、凭据轮换、租户隔离、Tool Policy 或审批边界；主动巡检也不能因为瞬时网络波动自动撤销已审核 Schema 或修改用户授权。

## 决策

1. MCP SDK Adapter 使用可关闭的有界 Session Pool。池身份由 Connection ID、Credential Version、Transport 和 Endpoint 派生，不包含可逆凭据。
2. 单个 Session 同一时刻只授予一个租约；同一连接可创建的 Session 数和全局 Session 数均由部署配置限制。达到上限时有界等待并返回明确背压错误。
3. Endpoint 或 Credential 身份变更、连接撤销后，旧身份被暂时封禁；空闲 Session 立即关闭，在途 Session 释放后关闭。服务关闭时停止复用并回收空闲 Session。
4. 主动巡检使用 MCP `ping`，通过 Mongo 独立健康租约在多实例间领取任务。健康写入不递增 Connection Revision，也不覆盖用户控制面字段。
5. 巡检使用批次、并发上限、超时、稳定抖动和指数退避。一次失败标记 `degraded`，达到连续失败阈值后标记 `unhealthy`，成功后恢复并清零计数。池饱和只推迟巡检。
6. 健康状态是诊断信号。它不修改 Discovery Status、Active Snapshot、Tool Policy，也不作为授予或拒绝真实调用的唯一依据。
7. Prometheus 只记录 Transport、固定结果/错误码、池事件和池状态，不使用 User、Connection、Endpoint 或远端错误文本作为 Label。

## 迁移与兼容

- Proto 仅追加健康字段；旧客户端可忽略。
- 旧 Mongo 文档缺少健康字段时按 `unknown` 读取，并因缺少 `next_health_check_at` 被视为到期，不需要停机回填。
- 新增健康到期/租约索引；现有 Connection、Snapshot 与 Tool Policy 文档结构保持兼容。
- `AGENT_EXTERNAL_MCP_POOL_ENABLED=false` 回到逐次建连。
- `AGENT_EXTERNAL_MCP_HEALTH_CHECK_ENABLED=false` 停止主动探测，不删除健康历史或授权数据。

## 后果与限制

- 常用连接减少重复握手，但需要维护有界进程内 Session 状态。
- 多实例租约减少重复探测，但不能替代生产代理、DNS、证书和第三方 Server 的真实环境验收。
- 控制面更新与已经开始的远端调用之间仍不存在分布式事务；失效机制阻止后续复用，但不会强制中断已经在网络中的副作用。
- 健康 `Ping` 成功只证明当时协议连接可用，不证明所有 Tool、权限或第三方幂等实现正确。

## 被否决方案

- 每次调用永久重新建连：实现简单，但握手成本和长连接抖动不可接受。
- 每个 Connection 永久保留一个共享 Client：缺少容量、并发和凭据轮换边界，容易形成热点与数据竞争。
- 巡检失败立即禁用工具：会把瞬时故障升级为授权变更，并导致多实例控制面抖动。
- 使用 Connection Revision 保存健康心跳：会与用户编辑、审核和策略 CAS 产生无意义冲突。
