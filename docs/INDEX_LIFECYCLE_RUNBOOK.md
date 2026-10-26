# 索引生命周期与 Relay 验收（M5-R2，2026-10-13）

> 当前 M5-R3 已拆分 text/vector 创建与独立 lane，旧 sync_es 转登记子任务；新增显式元数据对账、受控修复/发布回执与保留计划见 [INDEX_RECONCILIATION_RUNBOOK.md](INDEX_RECONCILIATION_RUNBOOK.md)。下方为 R2 交付记录，描述的耦合创建路径已由 R3 覆盖；命名锁/删除回执/Relay位点边界仍适用。

状态：Implemented（离线验证），真实部署与故障未 Accepted。M5-R1 保证单条软删除与事件原子入库；本轮把删除消费转换为持久任务，并约束协作投影写入和确认发布位点。此文不授权真实业务删除、索引清库或故障演练。

## 当前路径

```text
Tweet 删除事务 -> TWEET_DELETED Outbox -> Canal FIFO -> Broker Confirm
                                              -> 事务边界位点持久化
tweet.deleted -> 登记 delete_es / delete_vector -> Timeline 清理 -> ACK
                    -> 独立 Claim / Execute / Complete 或 Fail
sync_es -> 同 Tweet 会话锁 -> 当前 MySQL Public 正文 -> 写入 -> 再验当前事实
```

事件名称/旧 tweet_id、author_id 字段和 `sync_es` 载荷继续兼容。任务身份为独立主键与 `timeline:<task_type>:tweet:<id>:v1` 唯一键。两个删除任务逐项登记：第一项提交第二项失败时，不返回登记完成，使用既有确认发布的 Retry/DLQ 转发；后续重放补齐缺项。成功 ACK 不表示两个索引已完成删除。

每批最多领取 10 个任务、最多 60 秒/任务、90 秒租约、最多 5 次执行；并发上限由批次控制。成功/失败回执核对 Owner/Token/未过期 Lease。耗尽任务留在 Failed 状态供独立诊断，未实现自动修复或人工重放 CLI。成功任务沿用 72 小时保留，之后可由重复事件生成相同目标的幂等删除。记录任务状态和错误类别，报告不包含载荷、用户/Tweet ID 或正文。

## 当前事实与锁

`TweetProjectionRepository` 在一个专用 MySQL 连接上获取 `tweet:projection:<id>` 会话命名锁，等待最多 5 秒，退出时最多 2 秒释放；取消后仍尝试释放，锁回执不确定或释放失败就丢弃该连接。读当前事实只允许同锁绑定的 ID，包含软删除记录；不存在或非 Public 只清理两索引，读取失败不使用事件正文兜底。公共候选使用当前正文，索引写入后再读删除/可见性/身份/正文时间事实；变化时清理或保留任务失败重试。

锁占用连接，网络调用期间不持有 SQL 事务。命名锁属于单 MySQL 服务器，所有协作 Worker 和事实读取必须使用同一 primary；不要分流到不同副本、独立实例或代理的任意后端。需要评估连接池、普通请求连接预留、Worker 批次和锁等待。主库切换、连接断开、Worker 崩溃及远端操作迟到必须分别验收。

方案采用计划允许的“当前事实重验”，没有新增持久墓碑或服务端单调投影版本。锁无法撤回已被远端接收的网络请求：超时/取消后请求可能在锁释放之后才完成。现有重放任务能再次重验/清理，后续 M5-R3 对账仍是必需项；在没有服务端版本围栏、迟到写验证和对账证据前，不能宣称任意故障下绝不复活。M1 公共检索正文仍通过当前数据库授权。

命名锁会话生命周期依据 [MySQL 8.0 文档](https://dev.mysql.com/doc/refman/8.0/en/locking-functions.html)；项目不把它当多主/跨服务器锁。Canal 队列边界使用 [go-mysql v1.15.0 OnPosSynced 的实现](https://github.com/go-mysql-org/go-mysql/blob/v1.15.0/canal/sync.go)，来源版本与 go.mod 一致。

## 删除回执

| 后端 | 范围 | 完成条件 |
|---|---|---|
| ES | tweets 单文档十进制 ID | 匹配 ID 的 deleted/not_found，或明确 index_not_found_exception；未知404/坏JSON/下游故障失败 |
| Qdrant | tweets Collection 的规范 UUID，随后字符串 tweet_id 等值别名过滤 | 两次 `wait=true` 返回 completed；明确 tweets Collection 不存在可幂等成功；未知404/acknowledged/坏JSON失败 |

不会删除整个 Index/Collection，也不碰 Episodic Memory。旧异常点（错误 ID、缺少字符串 tweet_id 等）不由本入口枚举清理，必须另作有界审计。ES 文档删除和 Qdrant 删除没有跨存储事务，独立任务分别重试。

## Relay 位点

Canal `OnRow` 读取所有 INSERT 行，列/类型/形状异常返回错误而非静默跳过。`OnPosSynced` 将事务/DDL/Rotate 边界入同一个 2000 项有界 FIFO；单 Worker 先完成前序消息 Confirm 再保存该边界。保存失败退避重试，Redis保存最多5秒/次；取消不给新边界，未确认记录从原持久位点重放。未知事件路由停止Worker并通知主程序退出，保留原位点等待排查。

专用 Confirm Publisher 使用 Persistent 消息与路由+Payload SHA-256 MessageId，重发身份稳定；不保存/输出正文作为诊断。Broker Confirm 只证明 Broker 接收发布，不证明队列绑定、消费者接收或实际索引效果。MQ 通道重连仍需独立验证，当前退避不能替代重连能力。部署必须先建立持久 Exchange/Queue/Binding，保留单 active Relay（没有跨实例位点所有者租约）。Redis 位点需要可靠持久化与备份，不能把 Redis 丢失后的空位点当首次正常运行。

| 配置 | 默认 | 使用条件 |
|---|---|---|
| CANAL_ALLOW_CURRENT_POSITION_BOOTSTRAP | false | 缺失位点时默认停止；新环境或独立完成旧 Outbox 核查后，由操作人显式允许从当前主库位点起步 |
| CANAL_ALLOW_PURGED_POSITION_RECOVERY | false | 已清理位点默认停止；完成已不可达历史的补偿/核查后，才允许从最旧保留 Binlog 回放 |

这两个开关不执行补偿，不自动重放 Outbox 表历史。`.env.example` 仅提供默认 false；不修改用户 `.env` 或在 Compose 默认开启。容器部署若获准临时使用，应通过评审后的环境 override 明确传入，并在操作后关闭。

按年龄自动 OutboxEvent GC 已停用：年龄无法证明发布完成，不能在积压、位点不确定或历史恢复期间清除事件。需要监控数据库增长；发布感知保留/历史补偿作业归 M5-R3。本轮保留事件，不执行真实清理。

## 部署与回滚

1. 备份现有 OutboxTask/OutboxEvent、Redis位点与实际索引版本；记录当前镜像、索引/Collection/Embedding配置和队列绑定，不导出正文/凭据到报告。
2. 核验既有唯一键/租约列、同一 MySQL primary/命名锁能力/连接池。没有新表或本轮DDL，但旧部署Schema可能尚未具备既有列。
3. 停并排空所有旧 Consumer，包括在途 sync_es 与 ACK后无持久状态的旧ES goroutine；升级新 Worker 后恢复消费。禁止新旧 Worker 混合领取任务，旧代码会把新任务类型判终止。
4. 核验所有业务队列已绑定，再升级单 active Relay；检查持久位点有效、两个风险开关默认关闭。缺失/清理位点不能为了启动方便直接开启。
5. 使用已有受控fixture按下面矩阵验收，采集任务状态/固定结果/资源，不把Fake和小样本数字推导为生产规模证明。

回滚应先停止消费/领取并记录在途结果，保留任务、位点和事件。不要回退到忽略当前事实的旧创建写入、未知类型Worker或年龄GC。若版本必须回退，将新增任务保留待兼容Worker恢复，不通过删除任务/清位点让系统“看似正常”。没有索引清库或全量重建动作。

## 验证证据与真实矩阵

已通过：定向用例、Tweet/MQ/Canal/MQ基础设施/ES/Qdrant和三个入口扩大回归、同范围Vet，以及Consumer/Canal/Repository/ES/Qdrant Race。补测实际删除Handler：正常路径 outbox/outbox/ack，部分失败 outbox/confirmed retry publish/ack；定向Race通过。内存SQL驱动验证实际仓储的连接绑定/释放，httptest验证协议范围与回执，miniredis验证真实Handler缓存接线；均不代表真实服务器、进程重启或实际数据删除。

| 待真实验收场景 | 必需证据 |
|---|---|
| 删除与旧创建任务乱序、多实例同ID | 当前事实、两索引最终状态、任务回执和锁等待；同primary/实际SQL连接确认 |
| ES成功/Qdrant失败/耗尽恢复 | 两任务独立状态/尝试次数，重放只补失败端，耗尽仍有持久证据 |
| 远端接受请求但丢回执，随后删除/Worker退出 | 远端迟到写、租约恢复和对账收敛；不能只检查客户端超时 |
| 发布已确认但未存位点即进程退出 | 旧位点重放、重复任务身份、没有未发布事件被跨越 |
| 多行事务、MQ/Redis断连、取消/关闭 | 所有行可重放、位点不越界、连接重建与队列绑定效果 |
| 缺失/已清理位点、保留策略 | 默认拒绝启动；历史补偿可复查，有发布感知保留证据后才能清理 |
| 存量隐藏/异常点/历史无删除事件 | 有界只读差异报告与单独获准修复，不假设本轮自动覆盖 |

未执行真实MySQL锁竞争/MQ/索引写入、付费Provider或部署。Embedding故障仍会阻塞旧sync_es的BM25写入，文本/向量创建拆分、告警与对账归下一轮M5-R3；Timeline旧粉丝上限和异步缓存投影仍待M3治理。
