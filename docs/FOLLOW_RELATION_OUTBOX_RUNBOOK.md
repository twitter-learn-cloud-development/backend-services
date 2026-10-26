# Follow 关系与事件原子提交（M3-R2，2026-10-13）

> M3-R3代码已提供持久Consumer/分页任务，旧Service goroutine与Align执行已停用，启用开关默认false、迁移未执行；当前路径覆盖见 [FOLLOW_PROJECTION_RECOVERY_RUNBOOK.md](FOLLOW_PROJECTION_RECOVERY_RUNBOOK.md)。下方“队列无Consumer/旧任务暂留”是R2历史交付状态。

状态：Implemented（离线验证），真实迁移/事务竞争/队列恢复和部署未 Accepted。前置 M3-R1 公共关系 Cursor 已实现；M2 的 Following Feed 仍从当前关注事实授权。本轮只闭合关系事实与事件的原子提交，不宣称派生缓存恢复完成。

## 功能模块—轮次

| 模块 | 轮次 | 本轮交付 | 后续 |
|---|---|---|---|
| Follow 写入 | M3-R2 | pair 行锁、3秒 UoW、状态变更与 Outbox 一起提交；重复状态不新增事件 | 真实 MySQL 锁/死锁/提交回执丢失 |
| 事件与 Relay | M3-R2 | v1 兼容事件、关系代次/唯一事件键、发生时间/W3C Trace Context；Confirm Relay 路由与持久暂存队列 | 实际绑定/保留/消费、旧历史范围与多副本恢复 |
| 派生执行 | M3-R3 Planned | 本轮仅保留提交后旧缓存任务，关注队列没有 Consumer | 唯一持久 owner、分页/租约/重试/DLQ、取关及重新关注代次重验、晋升/降级/对账 |

## 当前路径

```text
FollowService.Follow / Unfollow
 -> UoW（最多3秒）
 -> INSERT/锁定 follow_pair_locks(follower_id,followee_id)
 -> 锁内读取最多2条当前有效 follows
 -> INSERT新关系 / 按有效关系ID软删除
 -> INSERT OutboxEvent（唯一键冲突则整体失败）
 -> COMMIT
 -> 旧进程内缓存任务（暂存技术债，不能证明恢复）

outbox_events -> Canal FIFO -> Broker Confirm -> follow.relations.v1 暂存队列
                       -> 后续提交边界位点
```

缺少 UoW/Outbox/Mutation Port 时写入失败，不回退尽力 MQ。仓储旧 Follow/Unfollow 方法也走同一 Mutation Port，要求调用者已有 UoW；事实写入口必须由 Service 连同 Outbox 调用。读取构造器保持兼容，Proto/HTTP 的字段和响应形状没有变化。

pair 协调行解决“还没有有效关系行可锁”的竞争：同一有向关注对的协作 Writer 始终锁同一复合主键；不同关注对不共用锁，避免锁整个用户。取关后保留协调行，重新关注生成新的 Snowflake 关系 ID，不复活旧关系行。新路径不依赖对全量历史关系施加 pair 唯一约束；既有有效关系重复时，最多读取两条即可检测并失败，不静默合并/删除。

## 重复与代次语义

| 操作 | 结果 | 关系/事件 |
|---|---|---|
| 已关注时再次 Follow | ErrAlreadyFollowing，与既有 Service 语义一致 | 不新增 |
| 未关注时再次 Unfollow | ErrNotFollowing，与既有 Service 语义一致 | 不新增 |
| 有效关系取关 | 成功 | 软删除该关系；一条 USER_UNFOLLOWED |
| 取关后再次关注 | 成功 | 新关系 ID；一条新的 USER_FOLLOWED |
| 同一对多条有效历史关系 | ErrFollowDuplicateActive | 本轮不修历史，事务回滚 |
| UoW/读取/锁/ID/事实写/事件写失败 | 错误 | 确定的事务失败整体回滚 |
| COMMIT 回执丢失 | 错误，结果不确定 | 可能事实和事件都已提交；必须读回，不得推断已回滚 |

这只是按当前关系状态处理重复，不是跨取关/再关注代次的请求幂等；接口没有 request id/expected relation generation。延迟旧请求依然会针对当前状态执行。消费者必须根据事件关系 ID 和当前事实决定收敛动作，不能把旧关注/旧取关直接覆盖新代次。

## 事件契约

Outbox 类型 `USER_FOLLOWED` / `USER_UNFOLLOWED`；路由仍为 `twitter.events` 下的 `user.followed` / `user.unfollowed`。原 follower_id/followee_id 保留为 uint64 JSON 整数，追加字段仅供新消费者读取，不改变 Proto。

```json
{
  "follower_id": 17,
  "followee_id": 42,
  "relation_id": 61,
  "schema_version": "follow-relation-v1",
  "event_key": "follow:61:followed",
  "occurred_at_unix_ms": 123
}
```

示例使用小 ID/时间；真实关系为 Snowflake/Unix 毫秒。取关键为 `follow:<同一关系ID>:unfollowed`，重新关注使用新 ID；数据库 Outbox 主键是事件身份，非关系 ID。Outbox 的 dedup_key 唯一索引绑定代次+动作，不使用忽略冲突来提交事实。Outbox 沿用现有数据库主键分配，部署必须核验 allocator/唯一键和事务引擎。

有合法请求 Span 时保存 traceparent/tracestate，不保存正文或凭据；请求阶段日志和 Span 标记 relation/outbox/commit。最终持久 Consumer 的 Trace 提取、任务指标/积压告警、Retry/DLQ 与人工重放归 M3-R3。现有 publication-maintenance 可以检查这些 v1 事件，仍拒绝人工重放，直到消费幂等闭环。

## 迁移与权限

手工草案 `deploy/mysql/migrations/20261013_follow_atomic_events.sql` 本轮未执行。

1. 停止或冻结所有旧 Follow Writer，包括旧服务副本和直连脚本；旧版本绕过协调行，混合写入不能保证唯一有效关系。
2. 在受控窗口核验 follows/outbox_events/新协调表使用同一 MySQL primary、InnoDB 引擎，现有 outbox_events 主键分配和 uk_outbox_events_dedup_key 必须正确。启动门禁检查新表/Outbox/命名索引是否存在，不替代对引擎、索引真实唯一性和权限的运维审核。
3. 只读审查有效重复关系和现有索引，再独立审核/应用新增协调表和 `(follower_id,followee_id,deleted_at,id)` 索引。脚本不是历史修复，不自动清理重复数据；索引 CREATE 不能重复运行，已存在等价索引时跳过。
4. 先升级 Relay 路由；在恢复写入前创建并验证持久 `follow.relations.v1` 队列与两个绑定，再升级所有 Follow Service。服务启动会声明拓扑，但真实绑定/持久性需要验收。新协调表与 Outbox 不 AutoMigrate；现有启动 AutoMigrate follows/tweets 的历史边界未在本轮拆除，归 M7。
5. M3-R2 队列没有 Consumer、TTL 或自动回收，必须限制灰度窗口/流量，观察 RabbitMQ 存储和 backlog；M3-R3 上线前不要把长期生产队列堆积当已完成事件传播。没有正确绑定时 Confirm 也可能确认未路由的消息，必须保留 Outbox/位点/备份供后续受控补偿。

最小运行权限：follows SELECT/INSERT/UPDATE；follow_pair_locks SELECT/INSERT/UPDATE（MySQL ON DUPLICATE KEY 需要 UPDATE）；outbox_events INSERT；启动元数据读取所需权限。仅迁移账号具备 DDL。当前 Follow Service 的旧缓存功能仍读取 Tweet 表和 Redis，大 V 全量操作仍有截断；这些跨 owner 读取/账号拆分留 M7，不以本轮事务原子性推断已经隔离。

协调行存储随历史有向关注对增长，不做年龄 GC。账号删除/留存策略需要将该协调元数据列入数据所有权清单；删除前必须确认不存在并发 Writer 与恢复任务，本轮未实施清理。

## 回滚

停止新写入/Relay 发布并保留所有关系、Outbox、协调行、队列和位点；只回退应用版本，禁止删表/清队列。回退旧 Writer 会恢复事实/事件缺口并绕过 pair 锁，不能宣称原子保证仍存在，优先修正新版本。不得同时运行旧 Service 缓存任务和新持久 Consumer owner；M3-R3 切换时先停/排空旧执行路径再启用新 owner，队列事件不能由缺少代次重验的旧代码消费。

## 验证与剩余验收

离线运行真实 GORM UoW + Follow/Outbox Repository 的内存 SQL 驱动：事务外读写拒绝、pair/有效行 FOR UPDATE、有界2条读取、取关 ID+身份+有效谓词、故障整体回滚、12个并发重复关注只有一个事实/事件、重新关注代次分离、历史重复 fail-closed、事件冲突、取消与模拟 COMMIT 回执丢失。Mutex 模型不证明真实 MySQL 行锁、死锁或网络中断结果。拓扑 Fake 验证声明/绑定顺序与错误传播，路由/Schema/Trace/旧 JSON兼容有回归；这些不是实际 Broker 消费证明。

扩大 Follow/Tweet/MQ/Canal/Gateway Handler 与入口测试、Vet及受影响包 Race 已通过；具体命令见开发计划的 M3-R2 交付记录。本轮未启动真实服务、执行 DDL、业务写入/MQ重放或部署。

真实矩阵需要：新表/索引和权限读回，同一对/不同对并发、死锁和数据库回执丢失，Relay断连/重启/Confirm/正确绑定、队列留存与积压；历史重复范围和修复审核。下一轮 M3-R3 再验收全量分页回填/清理、旧事件与新代次乱序、晋升/降级持久进度及 Retry/DLQ。M2-R3 完整容量继续 Partial。
