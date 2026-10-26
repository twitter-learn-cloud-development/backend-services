# Following Feed 跨端与容量验收（M2-R3）

本文件区分已实现的离线客户端/扫描边界与真实部署验收。两端均使用当前 API 数据和字符串游标，不以本地状态代替数据库。真实容量结果必须记录数据集、环境、版本和范围；Fake Benchmark 不代表数据库吞吐。

## 可直接重复的离线检查

```powershell
# 工作目录 web，Node 22.13+；不安装新测试依赖
node --experimental-strip-types --test tests/feed_pager.test.mjs
npm run build
# 工作目录 mobile
flutter test --no-pub test/feed_notifier_test.dart test/feed_pagination_contract_test.dart
flutter analyze --no-pub lib/features/tweet/presentation/feed_notifier.dart lib/features/tweet/presentation/home_screen.dart test/feed_notifier_test.dart
# 工作目录仓库根
go test ./cmd/feed-read-acceptance ./internal/module/tweet/... ./internal/gateway/... ./cmd/tweet-service ./cmd/gateway -count=1
go vet ./cmd/feed-read-acceptance ./internal/module/tweet/... ./internal/gateway/... ./cmd/tweet-service ./cmd/gateway
go test -race ./internal/module/tweet/service ./cmd/feed-read-acceptance -count=1
go test ./internal/module/tweet/service -run '^$' -bench BenchmarkFeedScannerBudget -benchtime=20x -benchmem -count=1
```

客户端用实际生产状态核心与内存 HTTP/Provider 夹具：连续分页大整数不损失，按推文 ID 去重；同代加载互斥；刷新/账号/页签切换使旧响应及 finally 无效；失败保留已有列表与游标，重试相同失败页。空/全重复页最多自动续拉 3 页，再显示继续按钮；无推进游标明确失败而非无限循环。结束页不继续请求，卸载丢弃响应。Mobile Widget 覆盖 HomeScreen 空页继续与失败重试按钮。离线检查不替代两台真实设备/部署旅程。

## 只读 MySQL 入口

`cmd/feed-read-acceptance`默认拒绝Live；无`--allow-live`时不读取凭据或构造连接。显式Live后复用生产`FollowingFeedRepository`和只读`FollowReadRepository`，先做独立只读预检/EXPLAIN，再为每个样本建立独立只读Repeatable Read事务；样本内连续翻页，结束只Rollback，不Commit。不会执行DDL/AutoMigrate、写种子、连接Redis或更新投影。输出固定错误，不回显DSN/SQL条件/正文/用户ID；`--discover-viewer`只在预检选择最近有效关注的已有用户，与显式`--viewer-id`互斥。没有有效关注或有效行时失败，不把空样本当通过。

环境规则要求先告知并确认依赖已启动。开始前确认：

| 项目 | 要求 |
|---|---|
| MySQL | 已启动的测试/staging 实例，本地通常映射端口 3307，当前项目 `.env` 为 3306；以实际运行目标为准，不把生产库当测试默认目标 |
| 账号 | SELECT 只读账号；数据库提供 tweets/follows 当前 Schema；凭据配置在进程环境 `FEED_ACCEPTANCE_MYSQL_DSN`，不粘贴进对话或报告 |
| 数据集 | 使用已有受控fixture；操作人记录真实总量、作者分布/稀疏程度、深页Cursor和源码版本。工具只观察有界关注数和近似表行数，不创建fixture，也不据近似值证明规模 |
| 授权范围 | 当前身份已有只读访问权限；用户确认所需实例/端口/目的（候选读取与关注校验）和预计最多 60 秒/命令 |
| DB 版本/资源 | 操作人记录 MySQL 版本、CPU/内存、索引、热/冷缓存和副本数；不得由 Fake 推断 |

用户确认且环境变量已配置后，才可运行：

```powershell
# fixture_user_id/deep_cursor 必须使用已有受控数据实际值
go run ./cmd/feed-read-acceptance --allow-live --viewer-id=<fixture_user_id> --limit=20 --samples=20
go run ./cmd/feed-read-acceptance --allow-live --viewer-id=<fixture_user_id> --cursor=<deep_cursor> --limit=100 --samples=20
# 确认并发/页预算后，测有至少101个关注对象的指定fixture
# 最少页/行与延迟预算必须按实际数据和环境约定，此处阈值为命令示例
go run ./cmd/feed-read-acceptance --allow-live --viewer-id=<fixture_user_id> --workers=4 --pages=5 --samples=20 --limit=20 --min-followees=101 --min-pages=5 --min-rows=100 --max-p95-ms=200
```

### v2参数与工作量上限（2026-10-15，R3-D补充轮）

| 参数 | 默认/边界 | 语义 |
|---|---|---|
| `--workers` | 1，1—8 | 实际并发和连接池上限取workers与samples较小值；每个样本独立事务 |
| `--samples` | 20，1—100 | 总旅程数，非每worker次数；取消后未开始的任务记cancelled，不再建事务 |
| `--pages` / `--limit` | 1/20，1—20/1—100 | 每旅程页预算，每页读取limit+1；lookahead不消费，下一页从最后交付ID排他读取 |
| `--min-pages` / `--min-rows` | 1/1 | 每旅程最少非空页/总交付行；分别不能超过pages和pages×limit |
| `--min-followees` | 1，1—1000 | 预检最少不同有效关注对象，探针最多max(101,min+1)行；饱和时只是下界，非总数 |
| `--max-p95-ms` | 0 | 成功样本内候选SQL+关注校验页延迟的可选p95上限；0关闭、正值不超过60000，NaN/Inf拒绝 |

全次预检与负载共用60秒Context，连接/读/写协议超时5秒；最多2000次候选页查询、每页至多一次批量关注查询（最多100作者），加预检的有界关注探针、表元数据和起始页EXPLAIN。探针返回行数有界不等于数据库扫描量有界，稀疏过滤与DISTINCT的成本仍需真实EXPLAIN/资源证据。没有自动重试或持续无限负载，不能把较小的worker数视为全链路压力限制。

每页校验非零身份、ID严格倒序/小于请求Cursor、长度上限、软删除和他人可见性；所有交付的他人作者都必须在批量关注结果中为true。缺失或false明确失败，不只检查SQL是否报错。跨页单调排他ID同时防止重复；RR只覆盖单个样本，样本之间的快照可能不同，不证明生产客户端跨请求快照一致。

### v2报告、失败与证据范围

报告版本为`feed-read-v2`，原参数保留；2026-10-12的`feed-read-v1`文件继续作为历史，不改写旧耗时。v2改变为独立样本事务，不能与旧单事务热查询数字直接比较。仅默认门禁/参数/DSN/预检失败时不生成负载报告；进入负载后即使样本失败也先输出脱敏JSON，再非零退出。

| 报告字段 | 含义 |
|---|---|
| `scope/read_only/capacity_accepted` | 固定mysql_candidates_and_follow_check/true/false；仅候选SQL+关注，不自动容量接受 |
| `samples_requested/completed/failed/failure_counts` | 每个任务恰有成功或固定失败码，raw错误不写报告；任何样本失败导致退出非零 |
| `minimum_nonempty_pages/minimum_rows_per_sample/minimum_followees` | 本次操作人选择的最低数据要求，默认小值只适合连通性证据 |
| `nonempty_pages/rows_returned/samples_reaching_page_budget` | 所有样本中已验证的非空页/行、仍有lookahead但用尽页预算的样本数，可能包含随后失败的样本；未声称历史已读完 |
| `successful_sample_page_latency` | 仅完整成功样本的页延迟Count/p50/p95/max，含查询/关注/页校验，不含建事务/回滚、排队/预检 |
| `successful_sample_latency/load_elapsed_ms` | 成功旅程延迟含建事务/回滚，不含排队；负载墙钟包含全部worker/失败/排队，不含预检。未报告Gateway QPS |
| `sample_checks_passed/latency_budget_exceeded` | 数据/样本与可选页p95预算的工具判定，非容量签收；预算超限会非零退出 |
| `preflight_dataset/explain_estimates` | 观察时间/关注探针下界与是否饱和、InnoDB表行近似值及可用性、起始页计划摘要；去掉attached_condition，无全表COUNT，不包含真实CPU/热冷缓存/副本资源 |

固定样本错误包括`transaction_unavailable/candidate_read_failed/invalid_candidate_page/follow_read_failed/follow_validation_failed/insufficient_fixture/transaction_close_failed/cancelled`。失败样本的部分成功页不参与成功延迟分位数，必须同时查看失败率，避免幸存样本偏差。到期中的驱动查询可能归为candidate_read_failed等阶段错误；剩余未启动任务归cancelled。

v2仍未包含Stats/Hydrate、Gateway、Redis重建、网络/UI或开放环持续负载；高关注、稀疏深历史、真实并发/资源、索引部署、多副本及端到端容量都需独立证据。`sample_checks_passed=true`也不等于满足M2-R3完整容量，`capacity_accepted`始终false。

## R3-D本机验证记录

2026-10-15：命令、相关Tweet/Follow Repository目标Test/Vet/Race已通过。内存database/sql驱动夹具执行真实GORM查询与事务装配，检查每个样本独立ReadOnly+RR/回滚且无Commit；状态夹具覆盖精确大ID/lookahead、跨页排他边界、页/行门槛、关注missing、非法顺序/私密/删除、8worker参数上限/3worker实际并发、取消退出、连接关闭、固定失败码/报告不泄漏与分位数。最终命令与结果见PROJECT_PROGRESS，离线夹具中的百万表行估计为合成值，不代表真实数据库。

本轮未运行真实数据库或负载，未启动依赖/改写业务数据/应用索引；未修改生产Feed、HTTP/Proto或Web/Mobile，无需重复两端构建。回退只回退验收工具；旧v1报告仍可读作历史，不能混用版本字段或据此补签发布验收。

## 真实环境验收矩阵

| 场景 | 证据与判定 | 当前状态 |
|---|---|---|
| 普通/大 V/混合作者多页 | 两客户端真实 API 请求游标、ID 序列与当前 DB 事实一致，跨页无漏重 | 待 Live |
| 大于 100 个关注对象与深历史 | 受控大数据 fixture；首页/深页计划与耗时；不得出现旧关联集合截断 | 待 Live |
| 隐藏/删除/取关/粉丝可见 | 当前关系与可见性变化读回，两端明确失败/过滤，失败游标可重试 | 待 Live |
| 空页推进/账号与页签换代 | 慢响应/并发刷新/切账号；当前列表不受旧响应污染，继续/结束按钮状态正确 | 离线覆盖，部署待验收 |
| Redis 丢失与多副本重建 | 单 owner/Token 过期/新 Fanout 保留，记录最旧租约、失败率和恢复耗时 | 离线 Redis 夹具覆盖，真实故障待确认 |
| 并发容量/稀疏 Feed | 预先约定并发、持续时间和预算；记录 Gateway p95/错误率、DB 扫描/CPU/连接数、投影恢复调用量与资源上限 | 待独立负载计划与确认 |

当前离线 Benchmark 只计入 Go Scanner + Fake O(1) 页寻址 + 测试统计 Port + 日志的执行时间和分配；可见页 1 个候选批次、全隐藏页 10 个候选批次（101 条读取窗口/最多消费 1000）。输入模型不会产生真实 SQL 全表扫描或网络竞争。运行机器/命令/原始输出在当轮计划交付记录中登记，不将该数字外推为百万 DAU。

客户端回滚只回退 Web/Mobile 版本，不改服务器数据/游标契约。Live 只读命令没有发布动作；真实 Redis 故障/多副本/压测需按矩阵独立获准，不能由 `--allow-live` 的 MySQL SELECT 授权推断。
