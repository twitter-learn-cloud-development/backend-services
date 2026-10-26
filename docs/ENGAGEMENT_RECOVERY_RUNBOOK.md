# M6-R2 互动恢复与热度时间桶

日期：2026-10-13。代码/验证状态以 `ARCHITECTURE_REMEDIATION_PLAN.md` 为准。本轮扩展 M6-R1，HTTP/Proto/领域事实不变；没有新 SQL 迁移，未执行真实写入、重放、服务重启或部署。

## 互动会话所有权

- 两个互动队列各独立消费连接 + Confirm 连接，QoS=1、串行一条投影。每个 Consumer 实例额外需要四条 Broker 连接；生命周期队列的旧共享连接不参与互动重建。
- 每次初始化总计最多 5 秒，包括拨号、握手、拓扑、QoS、Confirm 与订阅。使用 bounded connection 的原始 transport 取消 watcher 中断阻塞协议等待；成功后替换为会话 lifetime watcher。
- 消费连接关闭或 Confirm Channel 关闭，释放当前会话并等待 5 秒重建；重建只声明持久互动拓扑和绑定，重新启用 Confirm/QoS/订阅。不会调用共享 RabbitMQ.Reconnect 或替换另一消费者使用的 Channel。
- 当前投影沿用 8 秒 Context、当前事实锁和原子 Lua；会话取消中断在途投影，未 ACK 投递由 Broker 重送。Confirm 超时走已有有界 fallback，ACK/NACK 不确定由固定结果指标记录；不把未收到回执解释为未执行。
- 关闭取消会话/硬关闭其拥有的 transport，等待消息循环和 publisher watcher 退出；Consumer 也等待衰减循环，`cmd/consumer` 等待 Timeline Start 返回后继续退出。其他生命周期消费者的连接重建与完整关闭矩阵没有因此完成。

恢复拓扑限定 `tweet.liked` 与 `comment.created`：现有业务队列、专用 `.retry.v1`、互动 DLQ、`timeline.ingress` 对应 `.timeline` 绑定。重试仍为 1/2/4 秒、最多三次；Producer 路由与已有队列无破坏性变更。

## 有界时间桶衰减

- 删除旧分钟 ticker + 50 秒锁 + ZINTERSTORE 路径。新循环启动即尝试执行，此后每分钟执行；逻辑 owner 是 Redis 原子时间桶，不需要某个进程保持领导租约。
- `trends:decay:bucket:v1` 存储 Redis TIME 的当前分钟编号，保留 7 天。同桶请求返回 duplicate；旧完成桶大于当前 Redis 时间时返回 clock_regression，不倒退完成状态。桶/热度变化由同一 Lua 执行，进程在回执返回前退出后重试仍读同桶标记。
- 每次固定最多 10000 个现存主题；超过界限返回 oversized、报警并保留原状态，不执行大集合全量衰减。管理员须评估暂停相关消费者/控制写入及容量修复；本轮不提供自动清库、任意扫描或大集合维护工具，持续写入仍可能扩大全局 ZSet。
- 只读取原排名前 100 个主题和分值，先验证保留分值非负/有限/不超过 1e12，再裁剪其余主题，最后按 0.95 更新保留主题。先裁剪后更新，避免未衰减的尾部主题反超并进入保留集合。裁剪成本限制在 10000 基数内，真实延迟需压测。
- 保留 `trends:global` 剩余毫秒 TTL；历史无 TTL 的有效集合修复为 24 小时。不存在热度时也记录当前空桶；该分钟后来加入的主题从下一桶开始衰减。
- 停机错过的桶不补算，只执行当前桶一次，避免对刚恢复加入的分值追溯衰减。Redis 自身时钟回拨、标记淘汰/丢失、AOF/故障切换仍待真实验收；此方案不承诺永久 exactly-once，也不提供历史热度撤销。
- Lua 仍不回滚运行时错误。类型/桶/分值/基数预检覆盖常见错状态，不能替代 OOM、真实持久化和容量证据。单节点 Redis Client，跨 slot Lua 不支持 Redis Cluster。

## 专用重放命令

`cmd/engagement-dlq-replay` 默认离线，输出固定计划，不读取 MQ 凭据或建立连接。`--allow-live` 才连接 Broker；检查会暂取至多 100 条未 ACK 消息，并在结束后 NACK/requeue。因此它是无发布、无删除的保留检查，可能改变 DLQ 次序，并非 Broker 纯读取。

```powershell
# 默认离线计划：不连接 Broker
go run ./cmd/engagement-dlq-replay --event liked --limit 20

# 真实检查：另行确认依赖/环境后执行
go run ./cmd/engagement-dlq-replay --event comment --allow-live --limit 20 --timeout 30s

# 真实重放：先检查保留报告和修复依赖，再显式执行
go run ./cmd/engagement-dlq-replay --event liked --allow-live --execute --operator operator-id --reason approved-repair --limit 20 --max-replays 1 --timeout 30s
```

| 参数/约束 | 行为 |
|---|---|
| event | 仅 liked / comment，各自固定 DLQ，不接受任意 queue/route |
| limit | 1—100，默认20；单次取完有界批次后处理，避免 requeue 的 poison 在同批循环 |
| timeout | 1—60秒，默认30秒，覆盖连接、协议等待、取消息和 Confirm；setup 内再限制5秒 |
| execute | 必须配 allow-live 与非空 operator/reason；Operator≤128字节、Reason≤512字节，报告只保存摘要 |
| max-replays | 1—10，默认1；沿用 x-timeline-event-replay-count，递增重放副本的累计次数 |

检查使用与生产消费相同的 engagement-v1/兼容旧事件解码，不补造实体/时间；身份/版本缺失、错路由、256 KiB 上限、坏计数、次数耗尽、超过48小时或未来超过5分钟全部保留并报告固定错误码。过期事件不重放，即使重放也不会恢复热度。该检查不读取 MySQL/Redis，不证明当前公共事实、去重标记或投影依赖已经修好，实际 Consumer 会再次验证。

执行先声明互动恢复拓扑/Confirm，保留原正文、EventKey/Trace/MessageId，清理自动 retry/x-death 等传输头并递增累计次数。仅发布 `timeline.ingress` 的互动专用路由，Broker Confirm 后才 ACK 原 DLQ。发布失败/取消保留未处理批次；ACK 失败标记 uncertain 并停止，剩余批次保留。Confirm 不证明路由/消费成功，须核验实际绑定；原消息 ACK 不确定时其旧 replay-count 可能仍在 DLQ，Redis 原子事件回执保护重复投递，但不能把副本 header 当跨 ACK 不确定的严格全局次数账本。

运维报告只有消息/事件身份、操作人/原因摘要与固定结果；不要将 MQ 密码、正文或原始用户 ID 传入 reason 或保存到命令历史。没有服务端 RBAC/持久审批账本，execute 是本机操作人明确声明。既有 Timeline 生命周期和 Publication CLI 保持原路由范围，不扩展为任意互动广播。

## 指标、升级和回滚

- 沿用互动 fixed-label 指标；新增 `trends_decay_bucket_total{result}` 与 `trends_decay_bucket_seconds`，结果固定 applied/duplicate/empty/oversized/invalid_state/clock_regression/failed/cancelled。告警规则新增衰减失败/容量/时钟和延迟，正常取消不报警。Prometheus YAML 离线检查不代表真实 PromQL/exporter 标签和告警通知已验收。
- 升级前停止全部旧 R1 及更早衰减 owner；保留 `trends:global`、互动标记、频控、Outbox/Relay 位点和所有队列。校验同 primary、Redis ACL/TIME/Lua/TTL/持久化策略、Broker 最小拓扑与每实例连接预算，核验默认单次基数界限后再灰度启动。
- 新时间桶是加法 Redis 状态，无 SQL 迁移；首次接管只衰减当前桶，不按旧进程启动时间追算。旧锁可以自然过期，不主动删除缓存/标记。
- 回滚取消并等互动/衰减在途退出，保留桶和积压。旧版本不识别新桶，不能混跑或直接重新启用旧衰减；选择保持暂停，或使用本轮兼容版本恢复。

真实验收矩阵：多实例同桶/跨桶和时钟回拨；Redis 响应丢失、OOM/AOF/故障切换/marker 淘汰、100/10000/超界限基数和写入竞争；RabbitMQ consumer/publisher 分别断连、初始化握手阻塞、在途退出、Confirm 成功 ACK 丢失、恢复绑定/持久消息、共享生命周期队列不受重建影响；CLI 部分批次失败/取消/限量/真实权限与审计报告。全部未在本轮操作真实环境，离线夹具不得记 Accepted。
