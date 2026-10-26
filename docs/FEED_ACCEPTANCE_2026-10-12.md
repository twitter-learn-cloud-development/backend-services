# M2-R3 验收证据（2026-10-12）

状态：跨端开发与离线门禁完成；真实 MySQL 小数据集只读 SQL 复验通过，高关注/深历史/并发容量及部署旅程仍待验收，不能标记 Accepted。完整步骤见 `FEED_ACCEPTANCE_RUNBOOK.md`。

## 当前代码与检查

| 检查 | 结果 | 实际范围 |
|---|---|---|
| Web 分页核心 | 7/7 通过 | Node 22.13 原生测试，导入生产 FeedPager；大 ID/重复/空页预算/页签换代/刷新竞态/失败精确重试/卸载/刷新失败后的替换 |
| Web 类型/构建 | 通过 | `npm run build`，Home 页签/账号换代、加载/重试/继续/结束界面；不等于真实浏览器部署旅程 |
| Mobile | 10 项通过 | 6 项 FeedNotifier 状态测试、2 项 HomeScreen Widget 测试、2 项内存 HTTP 契约测试；不启动模拟器或连接真实服务 |
| Mobile Analyze/格式化 | 通过 | 本轮 FeedNotifier/HomeScreen 与测试，无告警；限定文件 Dart 格式化 |
| Go 回归/Vet | 通过 | 只读验收 cmd、Tweet 全模块、Gateway 全模块、Tweet/Gateway 入口；只读门禁与 EXPLAIN 条件脱敏测试 |
| Go Race | 通过 | Tweet Service 与只读验收 cmd；Fake/回环夹具使用获准执行环境 |
| 格式/空格 | 通过 | 本轮 gofmt/diff check 与新文件尾随空格检查 |

首页“为你推荐”原 `ListTweets` HTTP next_cursor 还是 JSON Number；本轮统一为十进制字符串，补大整数 Handler 测试，避免新客户端状态校验拒绝默认页签。Proto 类型/编号和服务分页算法保持不变。关注流既有字符串契约不变。

## 离线扫描基线

命令：`go test ./internal/module/tweet/service -run '^$' -bench BenchmarkFeedScannerBudget -benchtime=20x -benchmem -count=1`。

环境：Go `go1.25.5 windows/amd64`；CPU `13th Gen Intel(R) Core(TM) i9-13900HX`；每个子场景固定 20 次。

| 场景 | ns/op | candidate_batches/op | B/op | allocs/op |
|---|---:|---:|---:|---:|
| 可见候选 `hidden_false` | 39420 | 1.000 | 28936 | 149 |
| 全隐藏 `hidden_true` | 248610 | 10.00 | 242436 | 1236 |

范围仅为 Scanner、O(1) 页定位 Fake、测试关系/统计 Port、日志与本机分配。可见页最多一个读取窗口、隐藏页最多 10 个窗口（每批前瞻 101、最多消费 1000）。无 MySQL/真实 Redis/网络/并发连接争用，不能据此推导生产 p95、QPS、百万 DAU 或 SQL 扫描复杂度。数值仅当前机器/当前代码单次基线，尚无长时间/多轮置信区间。

## 已尝试的 Live

用户已授权执行。专用 `FEED_ACCEPTANCE_MYSQL_DSN/FEED_ACCEPTANCE_VIEWER_ID` 未配置，按用户授权使用项目现有 `.env` 本地目标 `127.0.0.1:3306/twitter`，凭据只保留在子进程环境。命令：

```powershell
go run ./cmd/feed-read-acceptance --allow-live --discover-viewer --limit=20 --samples=20
```

`--discover-viewer` 仅在显式 Live 且没有用户 ID 时，在只读事务中选择最近一条有效关注关系的 follower_id；不创建/修改用户，不输出原始用户 ID。命令在建立只读事务时退出 1，固定错误 `read-only MySQL transaction unavailable`，没有输出性能样本或计划。此轮没有成功建立事务，因此未产生有效 Live 容量证据。

后续获准只读诊断：Docker 中没有运行中的 MySQL 容器；本机 3306/3307 两秒有界 TCP 检查均不可达。原因归为测试数据库未就绪；不把连接失败当业务/性能故障，不自动启动容器或写入种子数据。普通环境首次 Docker/socket 读取受权限限制，获准环境诊断结果作为当前依据。

## 获准启动与成功复验

用户随后明确授权“没有就启动个呗”。检查发现已有停止的 `twitter-clone-mysql-1`，镜像 `mysql:8.0`，复用 `twitter-clone_mysql_data` 数据卷；执行 `docker start twitter-clone-mysql-1` 后状态为 running/healthy，宿主映射为 `3307:3306`。只启动该现有容器，没有重建数据卷、执行 DDL 或写入 fixture。验收子进程使用现有本地凭据和实际端口 3307，未修改 `.env` 的 3306 配置；服务继续运行，需要停止时可执行 `docker stop twitter-clone-mysql-1`，保留数据卷。

真实环境：MySQL `8.0.46`；InnoDB Buffer Pool `134217728` 字节（128 MiB）。容器未设置独立 Memory/CPU 上限；Docker Engine 可见 32 CPU、`16628432896` 字节内存，属于共享运行环境，不能视为数据库专享资源。现有数据集共 316 条推文、7 条有效关注关系；最近有效关注的 fixture 用户有 1 个不同关注对象、40 条可读候选。未测作者大 V 分类，也不具备大于 100 个关注对象或长历史夹具。

每个命令 20 次样本，单连接、同一只读 Repeatable Read 事务，使用生产 SQL 和批量关注校验，未重启/清缓存以构造冷缓存。游标来自同一筛选条件的现有候选，未输出原始 ID。

| 样本 | Limit | 实际返回/次 | p50 ms | p95 ms | Tweet 访问/索引/估计行数 |
|---|---:|---:|---:|---:|---|
| 首页，`--discover-viewer --limit=20 --samples=20` | 20 | 20 | 4.924 | 5.910 | index / PRIMARY / 21 |
| 第 20 个候选之后的第二页，`--cursor=<existing_bound> --limit=100 --samples=20` | 100 | 20 | 5.007 | 5.997 | range / PRIMARY / 273 |
| 第 40 个候选之后的终页，同 Limit/样本数 | 100 | 0 | 2.226 | 2.981 | range / PRIMARY / 144 |

三个计划中的 follows 子查询均为 `ref / idx_follower / 1` 估计行。当前索引：tweets 的 PRIMARY(id)、idx_parent(parent_id)、idx_user_created(user_id,created_at)；follows 的 PRIMARY(id)、idx_follower(follower_id)、idx_followee(followee_id)。PRIMARY 有序候选读取加相关关系查找的成本仍可能随稀疏程度/历史增长；以上行数是 EXPLAIN 估计，未执行 EXPLAIN ANALYZE 或测量实际扫描行数。

首页/非空第二页证明现有 Schema、生产候选 SQL/关注批量查询和只读工具可在实际 MySQL 执行；终页空样本只记录边界查询，不能算非空容量通过。这些数字不包含完整 GetFeeds 过滤/Stats、Gateway、Redis 投影恢复、网络/UI，也不是 QPS 或大数据容量证据。依赖阻断已解除（ISSUES 234），但高关注/稀疏长历史、真实 Redis 多副本、部署浏览器/真机和端到端并发容量仍未覆盖，M2-R3 保持 Partial。
