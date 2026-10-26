# 文本/向量对账与受控补偿（M5-R3，2026-10-13）

状态：代码与离线验证已实现，真实依赖、迁移、模型调用和恢复验收待执行。此文是操作流程，不代表已授权在真实环境写入、重放或删除。M5-R2 的当前事实读取、同 primary 会话锁、删除回执与 Relay FIFO 边界继续适用；旧 `sync_es` 现在只登记独立子任务。

## 功能模块—轮次

| 功能模块 | 轮次 | 本轮实现 | 后续验收 |
|---|---|---|---|
| 公共文本索引 | M5-R3 | ACK 前登记 `sync_text`，独立领取；当前 Public 正文写 ES，附事实摘要/投影版本，不调用 Embedding | 实际 ES 映射、查询可见性、故障后读回 |
| 公共向量索引 | M5-R3 | `sync_vector` 固定 Model/Dimension/Version；模型调用在会话锁外，写前/写后重验；独立重试与耗尽保留 | 固定模型、实际 Collection 维度、模型故障、多实例恢复与召回基线 |
| 对账与修复 | M5-R3 | 显式 1—100 IDs 的元数据只读对账；审批摘要匹配后仅登记持久修复任务 | 历史范围清单、真实权限/故障、修复完成后的第二次对账 |
| 发布与保留 | M5-R3 | 可选 Confirm 回执账本；显式事件检查、受控 Confirm 重放和发布感知保留候选计划 | 单 active Relay、绑定/持久位点/账本恢复；真实路由和消费证明；安全 GC 独立设计 |
| 监控 | M5-R3 | 固定 kind/result/state 标签的次数/耗时/积压/最老年龄；耗尽、老积压、遥测失败告警 | 真实 Prometheus 加载、抓取和告警送达 |

## 任务路径与兼容边界

```text
tweet.created -> Fanout/趋势 -> sync_text + sync_vector 登记 -> ACK
旧 sync_es -> 幂等登记 sync_text + sync_vector -> 旧任务完成
文本 lane: sync_text / sync_es / delete_es
向量 lane: sync_vector / delete_vector
每 lane 每批最多 5 条，90s 租约、60s 执行、最多 5 次尝试
```

向量建模期间不持有 Tweet 命名锁，文本 lane 可以继续；索引写入/删除与当前事实重验仍短暂争用同一 Tweet 会话锁，并非任意索引后端故障下完全隔离。空依赖、错误模型/维度、非有限向量、未知 HTTP 回执均失败；向量失败不会把文本任务一并置失败。后台每分钟更新持久积压，MySQL 查询超时/失败保留旧 Gauge 并累加失败指标，不报告伪空队列。

新任务 Payload 保存身份、投影版本和 Embedding Profile，修复任务追加 operation、plan/operator/reason 摘要，不保存事件正文。旧任务保持读取兼容；排空旧 Consumer 后才能让新任务开始登记，旧 Worker 会把未知类型判为终止，禁止混合领取。失败/耗尽任务保留供对账，禁止盲目清空或重置重试数。

Consumer 使用 `LM_STUDIO_MODEL_EMBEDDING`、`TWEET_EMBEDDING_DIMENSION`（默认 1024）、`TWEET_EMBEDDING_VERSION`（默认 v1）。初始化仅创建缺失 Collection，使用同一 Profile 的维度；不修改既有 Collection。改模型/维度/版本必须另行评估召回和迁移，旧任务遇到 Profile 不匹配会失败，不会悄悄用新模型生成向量。ES 兼容保留旧向量映射，文本写入不发送该字段。

## 元数据只读对账

两个命令默认仅输出离线范围计划，不读取凭据或构造远程 Client：

```powershell
go run ./cmd/tweet-index-reconcile --tweet-ids=17,18 --embedding-model=fixture-model --embedding-dimension=2
go run ./cmd/outbox-publication-maintenance --event-ids=17,18
```

上述 IDs/Profile 仅为离线示例。真实操作前按 environment_context 确认目标依赖、用途和时间，使用隔离 fixture 或经过审核的显式清单，不在对话/命令输出中发送 DSN/密码。两个命令没有全表发现、全库重建或清库模式。操作 Context 总计 60 秒，MySQL 单次 I/O 5 秒；维护重放的 Broker 建连、协议/Channel 初始化最多 5 秒并遵从更短 Deadline。每批可能因范围/后端延迟而超时，应拆小清单，不能扩大超时掩盖故障。

真实对账需配置本机环境引用 `PROJECTION_MYSQL_DSN`、`PROJECTION_ES_URL`、`PROJECTION_QDRANT_URL`，ES 可用 `PROJECTION_ES_API_KEY`。DSN 指向与 Worker 相同 primary，禁止 multiStatements；ES/Qdrant 仅接受显式 http(s) 根地址，拒绝 URL 内凭据/Query。当前 Qdrant 适配器没有 API Key 配置，认证代理场景需先完善适配，不可绕过认证。CLI 不加载 `.env`，不 AutoMigrate，不调用模型。

获准依赖访问后，`--allow-live --mode=audit` 读取当前事实、ES 投影元数据、Qdrant 规范点元数据及精确字符串别名计数。状态包括 consistent、missing、drift、profile_drift、aliases、residue；未知响应或查询失败整批失败，不变成空成功。报告只包含主体/事实/目标摘要、状态和 Profile 摘要，不输出正文、向量或原始身份。

这里的 consistent 只表示版本/摘要/副本元数据匹配，不验证实际索引正文、向量数值或召回质量；手工篡改正文但保留摘要不会被发现。ES 可见性延迟、非协作 Writer、取消后的远端迟到写也可能使计划变化。命名锁没有服务器侧版本围栏，M1 的权威正文读取仍必须保留。

## 审批后的修复

先保留只读报告和完整显式 IDs，核对实际部署 Profile，再授权 task registration。执行须同时提供 `--allow-live --allow-write --mode=repair --approve-plan=<报告摘要> --operation-id=<稳定操作名> --operator=<操作人> --reason=<原因>`，使用相同目标、IDs 和 Profile 参数。

CLI 重新对账并比较摘要，目标、事实或索引状态变化会拒绝旧计划；登记前再次锁内重验事实。每个 Tweet 的至多两个任务在 3 秒 SQL 事务中整体登记、唯一键去重，并校验持久任务类型/载荷一致；实际表的 `outbox_tasks.id` 必须为 AUTO_INCREMENT，否则拒绝维护登记。常规 Consumer 仍使用 Snowflake 主键，部署前核验数据库 allocator 和唯一键，内存 SQL 驱动不替代实际 Schema 证据。

命令只登记任务，Worker 执行真实索引副作用；跨 Tweet 不是整体事务，错误可能返回已登记数量，非零退出必须保留部分结果。用相同 operation 重试可去重，若索引已经收敛导致摘要改变，需要审核新计划。公共缺失/漂移登记对应 sync；已删/缺失/非公共残留登记对应 delete。Vector repair 先清除规范点/别名再写新规范点，失败可能暂时缺失，依赖持久任务重试和后续对账收敛。

operation/operator/reason 是本机操作人声明及持久摘要，并非服务端 RBAC 或签名审批系统；权限由独立运维流程、受控命令环境和最小权限 DB/后端账号保障。日志/报告摘要不替代外部操作审计。

## 发布回执、重放与保留

手工迁移草案：`deploy/mysql/migrations/20261013_projection_operations.sql`，本轮未执行。先核验现有引擎、唯一键、ID 分配和索引，评估在线 DDL；新建 `outbox_publications` 和可选 lane 索引。索引脚本不支持重复 CREATE INDEX，必须先检查是否已存在。`CANAL_PUBLICATION_RECEIPTS_ENABLED=false` 默认关闭；表审核就绪后才可开启，Relay 开启时要求表存在，不自动迁移。

开启回执后：Confirm -> 持久回执 -> 后续事务边界位点。写回执失败在进程内重试且不推进位点；重启可能重新发布，消费者仍须幂等。账本只承认相同 event ID/type/canonical payload digest，重复写保留首次 confirmed_at；冲突明确失败。Confirm 不证明正确绑定、消费成功或索引完成。

检查使用 `PUBLICATION_MYSQL_DSN` 和 `MQ_HOST/PORT/VHOST` 绑定计划目标；`--allow-live --mode=inspect` 或 `retention-plan` 不连接 Broker。重放需额外 allow-write、approve-plan、operation/operator/reason；先比较不可变事件计划，然后 Confirm 发布、登记回执，每个事件最多 5 秒，事件间 100 ms 节流。部分完成保留计数/回执，重试同一计划会再次发布已确认事件，不能宣称 exactly-once。只允许创建、删除、治理重放；点赞/评论仅可检查，M6 完成事件幂等前重放拒绝。

保留候选仅是创建时间和匹配首次 Confirm 时间都超过 72 小时的显式事件；不证明安全删除条件齐备。命令没有 delete 模式，年龄 GC 继续停用；实际回收还需要 Binlog/检查点恢复窗口、路由/消费证据、备份和业务留存审核，不自动清理事件或回执。

## 灰度、回滚与验收

1. 核验 Schema/primary/Profile/命名锁和连接池；先停止并排空旧 Consumer/投影写入，升级全部 Worker 后恢复登记。不要让旧 Worker 领取新类型。
2. 默认保持回执开关关闭；审核并独立应用加法迁移后升级单 active Relay，核对绑定/持久位点，再选择开启回执。保留事件、任务和位点备份。
3. 回滚先停新登记/领取，保留新任务/回执表/索引；关闭回执开关不恢复年龄 GC。旧应用不能安全执行新任务，需先处理积压/恢复兼容 Worker，禁止清库作为回滚。
4. 隔离环境分别验证模型断连但 BM25 可完成、ES/Qdrant 部分故障、登记/Confirm/回执/COMMIT 丢失、进程重启、多实例与远端迟到写；每次修复后重新对账，并从 M1 入口验证权威公共正文。
5. 验证 Prometheus 告警配置加载和送达、查询执行计划/活跃积压容量、旧无事件删除/隐藏记录的范围清单、Profile 迁移召回。离线 YAML 解析不是 promtool 或实际送达验收。

M2-R3 的高关注/深历史/端到端并发容量继续 Partial。下一代码轮次按总表进入 M3-R2 Follow 关系事件原子提交；M5 的真实验收独立保留。
