# 下一批联合 Live 验收

范围说明：本文件记录本机交付之后的可靠性与服务器生产准入待办，不阻塞当前机器的 V1 本机联合运行版；未执行项不得写成通过。当前交付操作见 [`local-delivery-v1.md`](local-delivery-v1.md)。

## 状态与执行原则

上轮关键路径报告见 `docs/live-acceptance-v1.md`：真实 Chat、续聊、内部查询安全边界、向量写入/召回及基础可观测性已完成。该报告不证明故障注入、依赖重启、TTL、完整 Trace 或公网隔离通过。

本轮 Gateway loopback 绑定已实施并实测：Python Live overlay 将端口覆盖为 `127.0.0.1:9638`，本机 health 返回 200、未认证 internal 请求返回 401。后者证明本机认证拒绝，不证明公网隔离；主机外探测尚未执行。

Trace 原断链问题已修复并通过 Jaeger 实测：完整 Chat 首轮与续聊的 Python → Gateway → Tweet parent span 精确匹配。此结论来自实际父子关联，不仅是相同 trace ID 或服务发现。

本轮 Support Agent 实测 HostConfig 为 768 MiB、1 CPU、StopTimeout 30s，状态 healthy、无重启、无 OOM；在途请求优雅停机仍未测试。

隔离 Mongo 数据库 `support_agent_probe_e6f8ae9f25ea` 已通过跨连接恢复、跨用户拒绝、显式删除、60s TTL 与物理清理；三类记录最终均为 0。跨连接恢复仅指关闭后重新建立连接，不是 Mongo 服务重启。

Go supportquery 的 429/503/504 单测已通过；Python MockTransport 的 429/504 重试上限、每次新 JWT、同 tool_call_id 检查已通过。这些是受控测试，不是真实 Live 故障注入。Qdrant 新 probe 已完成真实插入、召回和撤回，并按 document_id 验证 count=0。

用户确认继续使用 Go 仓库内且被 Git 忽略的 `.env`，不执行仓库外密钥迁移。经用户授权，共享 MongoDB/Qdrant 受控重启后的持久化与新连接恢复已通过；运行中 Support Agent 旧客户端及已有 Chat 会话恢复未验证。真实 429/503/504、Go 开关回滚及主机外探测仍未执行。以下未注明完成的操作均为待做；不得仅因 HTTP 200、ready=200 或日志出现服务名而判定通过。

## 第一组：不破坏现有服务的检查

### 1. 密钥来源（用户已确认保留现状）

当前 `scripts/run_live_acceptance.ps1` 读取 Go 仓库 `.env` 并传入 Compose `--env-file`。用户确认继续使用此文件；它被 Git 忽略，但物理上并不在仓库外。

- 不迁移、复制或删除该文件；检查 Git 忽略状态和敏感值是否意外进入日志、报告或跟踪文件。
- 如将来要求仓库外来源，再单独设计显式来源与路径校验，不静默回退。

验收：按用户确认的 `.env` 运行，明确记录其仓库内位置与 Git 忽略属性；无密钥进入日志、报告或新仓库文件。环境注入不意味着 Docker 管理员不可读取密钥。

### 2. 网络与资源核查

状态：Gateway 本机绑定与上述响应、Support Agent 实际资源限额及当前健康状态已实测；其他服务暴露面、限额下负载表现和优雅停机不在本次已通过范围内。

- 检查 Gateway 实际端口仅为 `127.0.0.1:9638`，不存在 IPv4/IPv6 通配绑定；检查本地与容器间查询正常。
- 盘点 MySQL、Redis、Mongo、Qdrant、Jaeger 等宿主机端口。仅输出 ports 等白名单字段；本轮不擅改 Go 仓库。
- 实际 Live 合并的是 Go base Compose 与 `compose.live.yaml`，不包含本仓库 `compose.yaml`。检查容器 HostConfig 的 CPU、内存和 StopTimeout，不能引用后者的限额作为 Live 证据。
- `health/ready` 只反映应用状态，不检查 Mongo/Qdrant；恢复判断需实际数据操作。

验收：保留实际绑定、限额和相关只读检查结果。收紧数据库端口或资源限额若导致重建，移至第三组。

### 3. Trace 与公网检查

状态：首轮及续聊的 Python → Gateway → Tweet 精确父子关联已通过；以下 Trace 修复步骤已完成。公网入口检查仍待外部探测条件。

- 修复 Trace 注入时使用当前 OpenTelemetry context；对本次 Chat 的 trace ID 验证 Python HTTP → supervisor → worker → capability → Gateway → Tweet RPC 的父子关系，并对应请求标识。
- 公网检查需要用户指定实际域名/IP、入口拓扑与外部探测位置。只对指定资产发少量请求。
- 区分配置检查、宿主机绑定和外部入口阻断三项。API 401 表示接口可达但认证拒绝，不代表网络隔离；SPA HTML 200 也不能直接判为内部接口暴露。
- Go production overlay 隐藏 Gateway 端口，但当前 Live 未应用它。Nginx `/internal/` 可能落至 frontend，需检查实际响应与请求落点。

验收：父子 span 无缺失父节点；公网请求不进入内部查询处理链路。缺少外部探测条件时记录“未验证”。

## 第二组：隔离数据与独立故障环境

先明确允许创建和清理的测试数据库、collection、用户及 UUID。每项使用唯一 run ID，删除仅限此次专属数据；不清空共享库、不删除旧测试用户或推文。

### 4. 429 / 503 / 504 与有限重试

状态：Go supportquery 429/503/504 单测、Python MockTransport 429/504 重试上限/新 JWT/同 tool_call_id 检查已通过。下述独立故障环境及真实 Live 故障注入仍待执行，不能以单测或 MockTransport 结果替代。

- 使用独立 Gateway 与受控下游 gRPC 错误/延迟，验证 ResourceExhausted、Unavailable、DeadlineExceeded 分别映射为真实 Go HTTP 429、503、504；报告注明受控故障集成测试。
- 真实 Python client/executor 接入该环境，验证最多三次尝试、同 tool_call_id、每次新 JWT、最终 TEMPORARY_ERROR 及脱敏响应。
- Python Query 默认 2s、Go Query 默认 3s。专项环境令 Python HTTP及 capability 超时大于 Go 超时，或让下游立即返回 DeadlineExceeded；客户端超时另测，不能冒充收到 HTTP 504。
- 不对共享 Gateway 连发大量请求触发每 IP 限流。当前 executor 重试为 `sleep(0)`，需记录无退避这一限制。

验收：三类状态及错误码可追踪，重试不超预算，故障解除后恢复；故障不得被 Chat 虚报为已完成。

### 5. Mongo TTL 与删除

状态：隔离库 `support_agent_probe_e6f8ae9f25ea` 的跨连接恢复、跨用户拒绝、显式删除及 60s TTL/物理清理已通过，`conversations`、`conversation_checkpoints`、`conversation_checkpoint_writes` 最终均为 0。共享 Mongo 服务现已完成受控重启，专属会话在新连接上恢复；下列删除中途失败恢复策略和运行中旧客户端恢复仍待验证。

- 使用独立数据库 `support_agent_acceptance_<run_id>`、60s TTL 和专属 conversation UUID。
- 检查 `conversations`、`conversation_checkpoints`、`conversation_checkpoint_writes` 索引及最终清理；TTL 用例写入后不调用会刷新 expires_at 的 `require_owner`。
- 分别验证到期不可访问和后台最终删除，采用有界轮询。
- 通过资源层执行删除并检查三个集合；当前无用户 DELETE HTTP 路由，不宣称 API 删除已验收。
- `ConversationResources.delete` 先删 owner 后删 checkpoint，非原子；验证第二步失败后的残留与恢复策略。

验收：专属记录到期不可访问，最终被清理；正常删除无残留、跨用户无删除权限，其他数据不变。

### 6. Qdrant 撤回与恢复预演

状态：新 probe 的真实插入、召回、撤回已通过，撤回后按 document_id 检查 count=0。共享 Qdrant 服务受控重启后，同一专属文档 count=1 且向量检索命中；更新路径与运行中旧客户端恢复仍待验证。

- 使用独立 collection 和唯一 document_id，验证写入、召回、更新、撤回，再查询实际 point 数为零。
- 新探针已补充撤回后的实际计数检查；后续继续保留按 document_id 的 count=0 门禁。
- 当前没有 Qdrant TTL 机制；本项验证显式撤回/删除，不宣称 Qdrant TTL 已实现。
- 可先在独立 Mongo/Qdrant 容器演练重启恢复，不中断现有联合栈；报告明确环境差异。

验收：文档可召回、撤回后确实不存在；独立容器重启后专属数据和连接恢复。清理前再次核对 run ID 与资源归属。

## 第三组：需具体维护窗口的联合栈操作

执行前确定容器 ID、Compose project、挂载卷、影响范围、最长中断时间与恢复步骤，并取得对应操作授权。不使用 `down -v`、volume 删除、共享库清空或故意 OOM 压测。

| 门禁 | 操作与通过条件 |
|---|---|
| 开关回滚 | Gateway 的 `SUPPORT_QUERY_ENABLED` 启动时读取；关闭需重建/重启。关闭后内部路由不注册，普通接口正常；恢复后新 JWT 查询成功，Python 如实表达暂不可查询。 |
| Mongo 重启 | 已获授权并完成：专属会话标记重启前后 `1/3/3`，新连接恢复 2 条消息；原运行中 client 和已有 Chat 续聊仍待验证。 |
| Qdrant 重启 | 已获授权并完成：专属文档重启后 count=1 且检索命中，清理后 count=0；原运行中 client 恢复仍待验证。 |
| 优雅停机 | Dockerfile Uvicorn grace=20s，Live 未显式配置 stop grace。先配置足够宽限期，再测可及时完成和超时的在途请求；检查停止接新请求、有限取消、无强杀、关闭日志与 trace 刷新。 |
| 限额与其他端口收紧 | 优先独立实例验证，再经维护窗口应用现有服务。检查实际限额、健康与有限负载表现；数据库端口覆盖可能重建对应容器。 |

每次只操作目标服务，不因验收顺带重建整套依赖。Gateway loopback 已实施，不应为故障回滚自动恢复通配暴露。

## 证据与完成门槛

每项记录 run ID、环境、目标资源、期望、实测结果、时间、请求/trace ID、恢复及清理结果。报告不得包含密钥或无关用户数据。

先运行受控回归，再执行独立 Live 项，最后执行获授权的维护窗口项。失败、跳过、外部条件不足分别记录；不得将受控测试结果替代现有联合栈或公网结论。全部生产必要门禁有证据且剩余风险明确处置后，才讨论服务器生产版本冻结。
