# Follow 派生任务与恢复（M3-R3，2026-10-13）

状态：Implemented（代码接线及离线验证完成）；`FOLLOW_PROJECTION_WORKER_ENABLED=false` 默认关闭，手工迁移/实际依赖/恢复和容量未 Accepted。此文不授权真实 DDL、Redis写入、MQ重放或部署。

## 功能模块—轮次

| 模块 | 轮次 | 交付 | 部署边界 |
|---|---|---|---|
| 关注入口 | M3-R3 | v1事件校验；持久入口回执和作者/关系两个任务同事务登记后ACK | 独立Ingress连接/QoS10、Confirm发布连接，实际绑定另验收 |
| 晋升/降级 | M3-R3 | 5000/4500防抖、作者共享revision/epoch/cursor；100条历史关系页+当前关系批查，成功保存后续页 | 首次状态缺失时防抖区间继承Redis已有全局标记；无标记时按非大V处理 |
| 回填/清理 | M3-R3 | 当前有效关系代次重验、100条作者推文历史页；取关/已删/非公开残留移除，Public/Follows按当前关系添加 | Inbox最多1000，页级执行不等于无限缓存；M2权威Feed仍决定正文授权 |
| 恢复/治理 | M3-R3 | SQL租约与检查点、Redis执行epoch/token/服务器时间围栏，独立Retry/DLQ、显式检查/审批修复任务 | 不宣称exactly-once、Redis永久耐久或生产容量 |
| 唯一执行入口 | M3-R3 | 移除Service进程内回填/清理/广播；旧AlignCelebrityCache拒绝执行，所有修复交给Worker | 升级必须停旧副本/排空旧goroutine，禁止混合owner |

## 持久链路

```text
M3-R2关系+Outbox事务 -> Relay Confirm -> follow.relations.v1
 -> 校验v1/action -> MySQL receipt + follow_author_v1/follow_pair_v1 -> ACK
 -> 每轮作者/关系各Claim1（90s租约，单页15s）
 -> 同primary作者会话锁 -> 检查有效Owner/Token/Lease
 -> 持久epoch -> Redis Begin围栏 -> 执行100条页 -> 当前关系再验
 -> SQL成功续页/完成回执；失败保留检查点、按既有Outbox退避
```

入口回执的event_key/digest/registration_token固化两个任务的原子登记；重复事件核对摘要，不因MySQL ClientFoundRows误判新插入。任务成功清理后仍保留入口回执，重复投递不重新跑已完成任务。回执/作者状态没有年龄GC，治理保留与账号删除归数据owner方案，不能盲目清表。

作者任务共享作者状态，稳定状态已完成时重复事件无需重扫粉丝；状态变化或显式修复重置游标并增加revision。历史关系页包含已取关行，再以当前有效关系批查决定添加/移除，因此降级也会清理历史残留，超过5000名粉丝不被旧IDs-only上限截断。历史重复有效关系在批查/Pair读取时拒绝，不自动丢数据；粉丝计数使用有界超时的distinct follower统计。

Pair任务不按事件旧动作直接写缓存，而是读取当前有效关系/代次；重新关注的新代次阻止旧取关动作误删当前内容。写后关系变化重置游标，当前关系未变才推进。历史推文页包含软删除/隐藏记录供精确移除；回填整个历史扫描保持有界页，但耗时随历史量增长，不能将Fake页吞吐称为容量结果。任务不为其他入口提供授权，M2当前事实/关系重验必须保留。

成功续页将任务回到Pending并清零本页失败计数；页数不消耗最多5次失败预算。失败保持原Payload检查点、沿用有界Outbox退避，5次后保留耗尽任务。正常关闭使用活跃租约围栏释放并恢复本次领取次数，不将重启计为业务失败；回执不确定/失效由租约恢复，不能推断未执行。没有自动重置耗尽任务或无限requeue。

SQL会话锁占专用连接但不跨Redis请求持有SQL事务；锁等待5秒、释放2秒，回执不确定丢弃连接。所有新Follow投影写入在同一作者锁内；不同作者可水平扩容，热门作者串行是明显容量边界。作者和关系各预留领取槽，避免等待作者初始化的Pair任务挤占作者推进。

## Redis与重试边界

Redis Lua校验持久作者epoch、Opaque LeaseToken与Redis TIME下的租约截止时间；Begin拒绝较旧epoch，过期Begin与写入拒绝。epoch用十进制字符串比较，避免大整数精度；任务/推文ID按字符串保留，ZSet仍沿用既有float score和M2精确读取。围栏/用户大V集合/Timeline设置7天TTL；Unread清理只减实际移除数量并保留原TTL，重复删除不会重复扣减。Pair页失效初始化/重建租约标记，旧缓存回填不能提交旧marker。

全局 `global:celebrities` 沿用现有Set（未增加新永久权威源），同进程L1立即失效；其他副本L1仍存在配置TTL窗口。Redis丢失/过期、缓存被其他非协作writer改写或老副本仍运行时，不构成全局瞬时一致性保证。作者DB检查点Complete不证明Redis未被随后淘汰；需要显式修复重新传播。Redis Cluster跨slot Lua不支持，当前适配面向现有单实例/Sentinel路由；真实故障/时钟差异和拓扑另验收。

注册瞬时失败经 `follow.retry.v1`，1/2/4秒消息TTL，只回 `follow.ingress.v1 -> follow.relations.v1`，不会再次广播给其他业务队列。坏JSON/非v1/动作不符/非法重试头或3次失败进入 `follow.relations.dlq.v1`；Confirm成功后才ACK。失败发布仅在有界等待后重新入队，取消不盲目ACK；Broker连接恢复5秒退避。旧只有两身份字段的事件会被保留进DLQ，需审核显式当前事实修复，不伪造代次。

MQ DLQ没有自动消费/重放；耗尽的SQL任务也保留。不提供任意原消息广播工具，明确IDs的检查修复命令重新登记当前事实收敛任务。Confirm不证明正确绑定或最终消费，持久任务登记ACK也不证明缓存已完成。

## 显式对账与修复

`cmd/follow-projection-reconcile` 默认离线：

```powershell
go run ./cmd/follow-projection-reconcile --pairs=17:42
go run ./cmd/follow-projection-reconcile --author-ids=42 --mode=repair
```

上述ID仅是离线示例。两种scope互斥，最多100条，拒绝0/重复/自关注，无全表发现、清库或自动索引重建。默认不读取Credential、不构造Client，输出scope_count而非身份。真实访问前按environment_context确认依赖、用途/范围和时间，凭据仅放本机环境变量，不写对话。

真实读取用 `--allow-live`、`FOLLOW_PROJECTION_MYSQL_DSN` 和 `FOLLOW_PROJECTION_REDIS_ADDR`，可选Redis USER/PASSWORD；CLI不加载.env，不AutoMigrate/启动服务。Context60秒，单次连接/I/O5秒。当前CLI没有TLS选项，生产TLS-only环境须先扩展适配，禁止关闭认证/TLS作绕过。

只读audit逐显式subject锁内检查当前关系/代次、distinct粉丝数、作者revision/未完成检查点、全局与用户大V标记；报告只含摘要/布尔/计数。它不逐条比较Timeline，也不证明所有历史用户集合完整。目标摘要绑定DB/Redis地址，不包含凭据。

repair同时要求allow-write、approve-plan、operation-id、operator、reason；重算计划、注册前再检查subject摘要，变化拒绝旧授权。每subject至多两任务/入口回执原子登记，跨subject失败返回部分已登记数量和非零退出；命令只入任务，Worker执行Redis副作用。作者scope强制历史关系重扫，pair scope另加作者推文回填/清理；如果Redis成员指向从未有任何DB关系的用户，作者历史页不能发现，必须显式pair scope。

修复任务持久保存plan/operator/reason/operation摘要；操作人声明不是服务端RBAC或签名审批，真实权限由外部运维审核和最小账号保障。维护检查outbox_tasks.id为AUTO_INCREMENT后使用表分配，正常Ingress继续Snowflake；必须确认实际Schema，不从模型注释推断allocator。原审批计划重试可以去重；运行后的状态变化会产生新计划，需要重新审核。不同修复/状态变化可能重启共享作者游标，持续写入/状态震荡下的收敛时长需压测。

## 迁移、灰度、回滚与验收

1. 先停全部旧Follow派生owner和旧Consumer（旧无lane过滤的Worker可能领取未知任务），保留Outbox/队列/位点。
2. 核验M3-R2前置表/唯一键、现有Outbox租约列/唯一键/allocator，同primary/InnoDB/命名锁/连接池；审查重复和现有索引，独立应用 `20261013_follow_projection.sql` 的新状态/回执表及可选历史索引。脚本未执行，不重复创建已有等价索引，不自动清理数据。
3. Consumer开关默认false，新代码停用旧Service任务后靠Outbox/队列保留；启用前完成Schema/正确绑定/QoS/最小权限检查。Compose透传开关；其他部署形态需显式注入，镜像发布仍独立验收。启动开关为true但表缺失会拒绝，不能伪装已启用。
4. runtime账号需follows/tweets有界SELECT、状态/回执SELECT/INSERT/UPDATE、Outbox登记/Claim/进度/恢复权限及元数据读；DDL只给迁移账号。FollowService已经不装配Tweet仓储/Timeline缓存，也不迁移Tweet表，但旧Follow AutoMigrate仍留M7；Consumer的共享DB读边界没有因此拆库。
5. 回滚关闭worker/停止新领取，等有界在途页释放，保留表、回执、任务、队列和位点。不得恢复旧Service goroutine与新Worker混合写入，不允许旧Worker消费新类型；优先修正新版本。

已有离线证据：5012粉丝全覆盖/共享游标续跑与去重、降级清理、250条推文/旧取关与新代次/写后关系变化、坏页/失败检查点/取消/陈旧claim；实际GORM内存SQL验证双登记回滚/摘要冲突/ClientFoundRows/租约围栏/中断次数恢复；miniredis验证过期/陈旧Begin和写入、可见性/Unread/marker/错类型；MQ Fake验证Confirm-before-ACK/Retry-DLQ/独立拓扑，CLI默认离线/非法scope/写授权，指标低基数/YAML解析。SQL Mutex/协议Fake/miniredis不替代实际数据库/Broker证据。

Prometheus指标与耗尽/老积压/遥测失败三告警仅在worker启用时有数据；同DB多个Worker Gauge不应直接相加当作全局数量。真实抓取/规则加载/送达未验收，YAML解析不是promtool。真实矩阵还需迁移/最小权限、Redis丢失/重建、断连/回执不确定/重启、多实例/旧新owner切换、百万粉丝/深推文历史/事件峰值和热作者压力；M2-R3完整容量继续Partial。

下一代码轮次按总表进入M6-R1互动投影可靠消费；M4-R2私信跨端/实时恢复及部署验收仍在计划中。
