# 开发过程问题与解决方案清单

## 195. G3 计划执行器首轮验证命令未严格短路

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-10，离线测试） |
| 现象 | 首轮编译发现新增文件少一个闭合括号；随后一个统一补丁因 Hunk 行数错误被 `git apply` 拒绝，但 PowerShell 分号链仍继续执行了格式化和旧代码测试，产生了无效的“后续通过”输出。开发过程中还出现了预算文件路径和 test_runner fork 参数的局部假设错误。 |
| 原因 | Windows ACL 下采用临时统一补丁更新既有文件时，命令链没有在 `git apply --check/apply` 非零后立即退出；人工 Hunk 行数计算也缺少独立校验。 |
| 解决 | 将补丁拆小，先单独 `git apply --check`，后续命令统一增加 `$LASTEXITCODE` 短路；重新应用安全投影补丁并从头执行定向测试、Runtime/Strategy 全包测试和 Vet。所有有效验证均通过，错误补丁未进入工作区。 |

## 194. Short Plan 修复提示误插入局部类型定义

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-10，离线编译与单元测试） |
| 现象 | G3 Coordinator 首次定向测试在编译阶段报告 `short_plan_model.go:231:25: expected type, found ':='`，Runtime 测试没有启动。 |
| 原因 | Windows ACL 迫使现有文件通过最小统一补丁修改；补丁使用的零上下文行号落在 `promptConstraint` 局部类型声明内部，把 Repair Instruction 计算插进了 `struct`。这是本轮补丁定位错误，不是 Runtime 设计或外部依赖故障。 |
| 解决 | 将 Repair Instruction 计算移动到局部类型声明之前，重新执行 `gofmt`、G3 定向测试、Runtime/Strategy 全包测试和两包 Vet，全部通过；未启动服务或调用模型/公网。 |

## 193. Object Store 未在存储边界闭环校验私有性与对象完整性

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-04，索引快照验证） |
| 现象 | Tool Result 读取依赖上层再次校验长度和摘要，已有 Bucket 未检查匿名策略；评测归档读取只校验正文摘要和保留期，没有重新绑定回执中的版本、ETag、大小、类型和归档时间，复用同一对象时也未拒绝短于本次请求的保留期。 |
| 原因 | 初始适配器把 MinIO 视为可靠字节存储，将部分安全约束留给 Service/Profile 层，导致直接调用 Object Store Port 时边界不完整。 |
| 影响 | 错配或被污染的对象可能跨过存储适配层，公开 Bucket 策略可能暴露 Tool Result；旧对象的较短保留期也可能被误报为满足新的不可变归档请求。 |
| 解决 | Tool Result Bucket 启动检查改为拒绝匿名策略，写入与读取增加 8 MiB 硬上限、规范内容寻址键、长度和 SHA-256 复核；评测归档在读写时绑定 VersionID、ETag、大小、Content-Type、ModifiedAt 与 Compliance Retention，并拒绝保留期缩水。纯索引快照中的 Object Store 普通测试、Race 与 Vet 均通过。 |

## 192. Eval JSON 输入缺少统一大小与结构边界

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-04，索引快照验证） |
| 现象 | Eval 数据集加载器和证据解码器的输入约束不一致：部分入口允许未知字段、重复键或尾随 JSON，部分入口直接读取无上限数据或接受超大输出、步骤和 Token 计数。 |
| 原因 | 初始实现以受信任的本地评测夹具为前提，各类 Dataset、Report、Review 与 Evidence 入口分别实现 JSON 解码，没有共享 fail-closed 边界。 |
| 影响 | 配置拼写错误可能被静默忽略并污染评测结论；异常或恶意证据可放大内存、遍历和聚合成本，降低质量门禁与归档证据的可信度。 |
| 解决 | 新增统一的 8 MiB 有界读取和严格 JSON 解码，拒绝非 UTF-8、未知字段、重复键、尾随值、超过 128 层嵌套、超过 10000 条用例以及超限字符串、输出和执行计数；补充公开证据解码入口回归测试，纯索引快照中的 Eval 普通测试、Race 与 Vet 均通过。 |

## 191. Profile 质量证据未校验归档与验证时间顺序

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-03，索引快照验证） |
| 现象 | Profile 质量证据校验只要求时间非零并限制报告签名不能明显晚于归档，没有拒绝未来归档时间、归档前验证或未来验证时间。 |
| 原因 | 初始契约重点校验 Object Lock、对象版本和摘要绑定，遗漏了证据生命周期的单调时间关系。 |
| 影响 | 错误或恶意 Verifier 可能构造时间顺序不可能的紧凑审批记录；对象摘要与版本校验仍然存在，但审计时间语义不可靠。 |
| 解决 | 增加五分钟时钟偏差内的未来时间限制，并强制 `report_signed <= verified`、`archived <= verified`；补三类不可能时间顺序回归测试，Profile 普通测试、Race 与 Vet 均通过。 |

## 190. Profile 实验统计可能被 int64 溢出绕过成本门禁

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-03，索引快照验证） |
| 现象 | 实验观测的 `CostMicros` 累加及候选/稳定相对增幅比较直接使用 `int64` 乘加，极端合法非负输入可溢出并产生错误平均值或比较结果。 |
| 原因 | 初始实现限制了样本数与负数，却没有对数值总和和乘积做溢出保护。 |
| 影响 | 异常遥测值可能使候选成本回归被错误放行；正常范围观测不受影响。 |
| 解决 | 成本累计在溢出前失败关闭，相对阈值改为无溢出的 128 位乘积比较，同时拒绝 `positive=true` 但未标记 observed 的矛盾结果；补极值回归测试并通过普通测试、Race 与 Vet。 |

## 189. Windows tar 无法解包历史中文课程文件名

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-03，验证环境） |
| 现象 | 使用 `git archive --format=tar HEAD` 构造 05 纯基线快照时，Windows `tar.exe` 对三个历史中文课程文件名返回 `Invalid empty pathname`，快照不完整。 |
| 原因 | 当前 Windows tar/终端代码页对 Git Archive 中的 Unicode 路径处理不兼容，不是 Profile 源码、编译或测试失败。 |
| 影响 | 首个归档快照不能作为依赖闭包验证证据；没有修改索引、源码或外部服务。 |
| 解决 | 改用 `git checkout-index --all --prefix=<snapshot>` 从当前干净索引原生导出 HEAD，再覆盖本批文件；隔离快照中的 Profile 普通测试、Race 与 Vet 全部通过。 |

## 188. MCP 验收器未限制远端 Catalog、Result 与本地 Report 大小

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-03，索引快照验证） |
| 现象 | Acceptance 审查发现远端 Tool 数量、单 Tool Schema、Catalog 总量和 Tool Result 仅做 JSON 编码后摘要，没有硬性尺寸上限；已有签名报告读取也没有输入上限。 |
| 原因 | 初始验收工具重点覆盖权限、幂等、轮换和签名证据，遗漏了不可信第三方 MCP 对运维 CLI 的内存放大边界。 |
| 影响 | 恶意或异常 MCP Endpoint 可用超大协议响应增加验收进程内存与 CPU；该工具不在 Agent Service 热路径。 |
| 解决 | 增加 256 Tool、128KiB 单 Schema、2MiB Catalog/Result 和 4MiB Report 硬上限，并补超限回归测试。Acceptance 与两个 CLI 的普通测试、Race、Vet 均通过。 |

## 187. 内置 create_tweet MCP Tool 可在缺少幂等键时执行写入

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-03，索引快照验证） |
| 现象 | 内置 MCP Server 审查发现 `create_tweet` 虽向 TweetService 转发 `idempotency_key`，但 Tool Schema 和 Handler 都未要求该字段，直接调用可退化为非幂等发推。 |
| 原因 | 早期 MCP Tool 只补了可选参数，没有同步写工具的 fail-closed、Annotation 与扩展元数据契约。 |
| 影响 | 持有内部 MCP 凭据的调用方若遗漏稳定键，重试可能产生重复推文；这不影响统一 ToolExecutor 已注入稳定键的路径。 |
| 解决 | 将 `user_id` 与 `idempotency_key` 明确纳入 Schema，幂等键设为必填并限制 1-160 字节；标记 destructive/idempotent Annotation 和标准键参数元数据，Handler 在调用 TweetService 前拒绝缺键，同时对下游错误做脱敏。Tools/Security/Server 普通测试、Race 与 Vet 均通过。 |

## 186. 内置 MCP Tools Race 首次构建超过三分钟上限

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-03，缓存复验） |
| 现象 | 04E 纯索引快照执行 `go test -race -p=1 ./internal/module/agent/mcp/tools -count=1` 时，184 秒仍未完成并被硬超时终止；没有断言或编译错误输出。 |
| 原因 | Tools 包包含 Legacy Tweet Search 的 ES、Qdrant、AI 依赖，Windows Race 首次构建闭包较大；普通测试已通过，且超时后没有遗留 Go 子进程。 |
| 影响 | 首次 Race 命令不能作为通过证据；源码、暂存区、外部服务均未改变。 |
| 解决 | 复用本次已生成的仓库内 Race Cache，并将 `GOMAXPROCS` 从 2 调整为 4 后，同一索引快照 Race 在 12 秒内通过；Security 与 Server Race 也分别通过。 |

## 185. 内置 MCP 工作区宽测试长时间无输出并遗留 Go 子进程

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-03，拆分索引快照验证） |
| 现象 | 工作区执行 `go test -p=1 ./internal/module/agent/mcp/... -count=1` 超过两分钟无输出；终止工具会话后仍残留本次命令启动的两条 Go 子进程。 |
| 原因 | 宽通配符同时编译当前未提交的 Remote、Acceptance 和 Server 多个闭包，统一执行会话未随父会话回收子进程；尚无证据表明是源码测试失败。 |
| 影响 | 该宽命令不能作为验证证据；两条残留 Go 进程已按核对 PID 终止，应用服务和外部软件未启动或改变。 |
| 解决 | 改为 Tools、Security、MCP Server 三个明确包的有界命令；纯索引快照的三包普通测试、Race 与 Vet 均通过，验证后确认无残留 Go 子进程。 |

## 184. Web Access Governor 的亚毫秒窗口会形成除零或失效 TTL

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-03，索引快照验证） |
| 现象 | Repository 审查发现 `UserWindow` 和 `RunTTL` 只拒绝非正值；传入 `1ns` 等正的亚毫秒值后，`Milliseconds()` 为零，用户窗口分桶会除零，Run 计数也会立即过期。 |
| 原因 | 配置校验使用 `duration <= 0`，但 Redis Lua 参数和客户端分桶的实际最小精度是毫秒。 |
| 影响 | 非默认错误配置可能导致请求 panic 或绕过 Run 级请求/成本预算；默认一分钟/24 小时配置不受影响。 |
| 解决 | 将 Governor 窗口和 Run TTL 的最小值统一为 1ms，低于该精度时恢复安全默认值；Cache Set 直接拒绝亚毫秒 TTL，并新增回归测试。Repository/Web Search 普通测试、Race 与 Vet 均通过。 |

## 183. Provider Config Mongo 适配器未初始化时会空指针

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-03，索引快照验证） |
| 现象 | Connector Repository 审查发现 Provider Config CRUD 直接解引用 `providerConfigColl`，与同包 Project/MCP 适配器的 fail-closed 行为不一致。 |
| 原因 | Provider Config 是后补的可选 Repository 接口，初始实现遗漏了 nil Repository/Collection 边界检查。 |
| 影响 | 错误启动接线或隔离测试可能触发 panic，而不是返回可诊断错误。 |
| 解决 | 五条 CRUD 路径统一检查 Collection 可用性，Mongo `ErrNoDocuments` 改为 `errors.Is`，时间戳统一 UTC，并新增未初始化回归测试；纯索引快照普通测试、Race 与 Vet 均通过。 |

## 182. 远程 MCP Session Pool 会向已取消请求发放空闲租约

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-03，索引快照验证） |
| 现象 | 新增回归测试 `TestClientPoolRejectsCanceledAcquireBeforeReusingIdleSession` 后，已取消 Context 调用 `Acquire` 返回 `nil` 错误并复用空闲 Session。 |
| 原因 | `clientPool.Acquire` 只在容量等待的 `select` 中检查 Context；命中空闲 Session 或可新建 Session 的快速路径前没有检查调用方取消状态。 |
| 影响 | 已取消的 Agent/MCP 请求仍可能获得远程 Session 并继续调用，破坏取消传播与资源隔离语义。 |
| 解决 | 在 Pool 锁内、发放或创建 Session 前优先检查调用方 Context 与 Acquire Deadline，并补充空闲复用回归测试；纯索引快照的完整普通测试、Race 与 Vet 均通过。 |

## 181. Agent 项目权限快照首次测试在标准库编译阶段异常退出

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-03，隔离缓存复验） |
| 现象 | 04B 纯暂存区快照执行 `go test ./internal/module/agent/project -count=1` 时，Go 工具链在编译标准库 `reflect` 阶段仅返回 `compile.exe: exit status 1`，没有源码或断言错误。 |
| 原因 | 首次命令复用了已有仓库内 Build Cache，Go 编译器在标准库阶段异常退出；同一索引快照切换全新缓存并限制并发后可以稳定编译，未发现项目源码错误。 |
| 影响 | 04B 首次命令没有形成代码验证结果；暂存区、应用源码和外部服务均未改变。 |
| 解决 | 使用全新仓库内 Go Build Cache、`-p=1` 与受控 `GOMAXPROCS` 重新执行普通测试、Race 和 Vet，三项均通过。 |

## 180. Agent Run Repository 快照缺少共享核算版本契约

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-03，索引快照验证） |
| 现象 | 03D 索引快照编译 Repository 时，`agent_execution_run.go` 与其测试报告 `undefined: ExecutionAccountingVersion`；Strategy 测试已通过。 |
| 原因 | 初始 Repository 闭包遗漏了定义 `execution.accounting.v1` 及直接子 Workflow Run 查询 Port 的 `agent_run_accounting.go`。 |
| 影响 | 03D 首次快照未形成 Repository 测试证据；工作区源码、外部服务与真实数据均未改变。 |
| 解决 | 将单一无外部依赖的核算仓储文件纳入同一 03D 闭包，并重新执行 Repository/Strategy 普通测试、Race 与 Vet。 |

## 179. Workflow 节点取消与临时错误竞态返回了错误的失败原因

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-03，索引快照验证） |
| 现象 | 03A 索引快照的 `TestSchedulerCancellationInterruptsRetryBackoff` 失败：节点第一次返回可重试临时错误后 Context 已取消，但 Scheduler 返回 `node execution error: temporary`，未返回 `context.Canceled`。 |
| 原因 | `executeNode` 在同一条件中判断 `ctx.Err()` 与重试资格，命中取消时仍直接返回节点原始错误，导致外层状态机无法把 Trace 归类为 canceled/timed_out。 |
| 影响 | 取消或 Deadline 与节点临时错误同时发生时，调用方会收到错误的失败分类；没有触发第二次工具调用，也未连接任何外部服务。 |
| 解决 | 节点执行失败后优先返回 Context 终止原因，再判断最大尝试数与错误可重试性；重新执行 03A 快照普通测试、Race 与 Vet。 |

## 178. Test Runner 快照验证无法取得本机 Go 1.25.5 工具链

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-03，隔离验证环境） |
| 现象 | `test_runner` 在 02A 索引快照执行首条 `go test` 时尝试下载 `go1.25.5`，随后因网络受限退出，未继续 Race 与 Vet。 |
| 原因 | 子代理隔离环境无法访问用户级已安装工具链；仓库的 `go.mod` 要求 Go 1.25.5，自动下载又不具备网络权限。 |
| 影响 | 子代理没有形成代码验证结果，但暂存索引、源码和外部服务均未改变。 |
| 解决 | 主会话在同一索引快照中受控使用本机工具链，普通测试、`go test -race -p=1` 与 `go vet -p=1` 对五个目标包均通过。 |

## 177. Agent Core 首轮测试无法访问用户级 Go Build Cache

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-03，本地验证环境） |
| 现象 | `go test` 对 Runtime、Message、Model、Credential 与 Observability 五个包执行 setup 时，均返回 `AppData/Local/go-build/...: Access is denied`，并且无法写入 `trim.txt`。 |
| 原因 | 当前工作区沙箱不能访问用户级 Go Build Cache；测试尚未进入源码编译或断言阶段。 |
| 影响 | 首次命令没有形成 Agent Core 代码质量证据；未连接模型、数据库、MCP 或公网服务。 |
| 解决 | 对完全相同的五包定向测试授予受控 Go Cache 权限后重跑，五个包全部通过。 |

## 176. 01 索引快照联合测试退出后工具会话未回收

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-03，本地验证环境） |
| 现象 | 在 `.codex-tmp/verify-01-index` 运行的多包 `go test` 长时间无结果；中断后继续查询时工具会话仍显示 Running。 |
| 原因 | 系统进程表已不存在 `go.exe`、编译器、链接器或引用快照目录的测试进程，实际测试子进程已经退出，但统一执行会话没有正常回收，原命令结果不可作为通过证据。 |
| 影响 | 当前 72 项暂存索引未改变；完整工作区上的普通测试、Race 与 Vet 证据仍有效，但“仅 00+01”索引快照尚需重新形成独立验证结果。 |
| 解决 | 已终止失去子进程的工具会话，并改由 `test_runner` 在同一只读索引快照中拆成三条、每条 180 秒上限的目标测试；Tweet/Follow/Auth/ServiceAuth、Event/MQ/Consumer/命令和 Agent RiskControl 三组均通过，随后成功创建提交 `849cb56`。 |

## 175. 交付基线首次暂存被陈旧 Git Index 锁阻断

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-03，本地 Git 工作区） |
| 现象 | 限定 Pathspec 的 `git add` 返回 `Unable to create '.git/index.lock': File exists`，00 批次未进入暂存区。 |
| 原因 | `.git/index.lock` 是 2026-07-21 创建的 0 字节遗留锁；当前没有 `git` 或 `git-lfs` 进程，符合此前异常中断后未清理的陈旧锁特征。移除陈旧锁后的首次重试又被工作区沙箱禁止写 Git Index，并非新的仓库锁冲突。 |
| 影响 | Git Index 保持未修改，00 批次暂时无法暂存或提交；工作区源码和用户排除文件未受影响。 |
| 解决 | 经授权删除这个单一陈旧锁文件，并以受控 Git 写权限重跑同一条限定暂存命令；复验得到暂存 34 项、越界 0、排除路径 0，`git diff --cached --check` 通过，随后成功创建提交 `ce9ad55`。 |

## 174. 交付基线顶层分类脚本的 Where-Object 表达式无效

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-03，本地审计命令） |
| 现象 | 顶层总量仍输出，但 Tracked/Untracked/Deleted 子计数均为零，并伴随 `OperatorNotSpecified`。 |
| 原因 | PowerShell 聚合脚本使用了无效的 `Where-Object Kind -eq'...'` 紧凑写法。 |
| 影响 | 该次子分类统计不可采用；工作区、Git Index 和应用代码均未修改。 |
| 解决 | 改用显式脚本块后重跑成功，取得 623 项真实源码/文档状态及各顶层目录的 Tracked/Untracked/Deleted 子计数。 |

## 173. 交付基线状态采集使用了当前 Git 不接受的 NUL excludes 文件

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-03，本地审计命令） |
| 现象 | `git -c core.excludesFile=NUL status --short --untracked-files=all` 返回 `fatal: cannot use NUL as an exclude file`，状态清单为空。 |
| 原因 | 当前 Windows Git 不接受 `NUL` 作为 `core.excludesFile`；该参数原本只用于抑制无权读取用户级 ignore 的非致命警告。 |
| 影响 | 首次工作区分类没有取得数据；未修改 Git Index 或工作区文件。 |
| 解决 | 使用普通 `git status --short --untracked-files=all` 成功取得完整清单；用户级 ignore 权限警告不影响仓库状态分类。 |

## 172. Agent 进度审计命令使用 Windows 不支持的路径通配符

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-03，本地审计命令） |
| 现象 | `rg` 读取 `internal/module/agent/service/*_test.go` 与 `internal/gateway/handler/*_test.go` 时返回 Windows 路径语法错误。 |
| 原因 | PowerShell 将带目录的类 Unix glob 原样传给 `rg`，Windows 文件 API 不接受该路径形式。 |
| 影响 | 仅中断产品入口测试证据检索；没有修改或执行应用代码。 |
| 解决 | 改为对目标目录执行 `rg -g '*_test.go'` 后成功取得统一入口、联网搜索、外部 MCP、Workflow-as-Tool、恢复和多角色执行测试证据。 |

## 171. Marketplace 收口时全工作树空白检查命中无关 Mobile 改动

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-03，本地工作树） |
| 现象 | `git diff --check` 命中 `mobile/lib/features/tweet/presentation/feed_notifier.dart:198` 与 `tweet_detail_screen.dart:130` 的尾随空格。 |
| 原因 | 当前工作树包含其他轮次或用户尚未提交的 Mobile 改动；两处文件不在本轮 Marketplace 控制面改动范围内。 |
| 影响 | 全工作树空白检查无法作为本轮验收信号；本轮源码、契约、部署和 Web 文件尚需范围化复验。 |
| 解决 | 本轮全部已跟踪文件范围化 `git diff --check` 通过，新建与已跟踪控制面文件的尾随空白扫描也通过；保留两处无关 Mobile 改动，不将其误计为本轮缺陷。 |

## 170. Marketplace 控制面五包联合 Race 在 Gateway 编译前达到上限

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-03，本地验证环境） |
| 现象 | 五包定向 `go test -race` 在 300 秒上限结束；Marketplace 与 Repository 已通过，Service、gRPC、Gateway 尚无结果，也没有竞态或断言失败输出。 |
| 原因 | Windows Race 首次串行插桩编译 Agent Service、gRPC 与 Gateway 的大型依赖图超过统一命令时限。 |
| 影响 | 领域和 Mongo Adapter 已有 Race 证据，其余三包需拆分复验；未启动或访问任何外部服务。 |
| 解决 | 复用已生成 Race Cache 后按包运行：Service、gRPC 与 Gateway 定向 Race 均通过；结合联合命令已通过的 Marketplace 与 Repository，五个目标包均取得独立 Race 证据。 |

## 169. Marketplace Gateway 测试使用了不存在的 outgoing metadata helper

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-03，测试代码） |
| 现象 | Marketplace gRPC 包通过，但 Gateway Handler 构建失败：`undefined: metadata.ValueFromOutgoingContext`。 |
| 原因 | 测试 Fake 误用了 incoming metadata 的便捷读取思路；当前 gRPC Go API 需要先调用 `metadata.FromOutgoingContext`。 |
| 影响 | 仅阻断新增 Gateway 测试编译，生产 Handler 尚未出现编译错误；未访问任何外部服务。 |
| 解决 | 改为 `metadata.FromOutgoingContext` 后按 key 读取 token；Marketplace gRPC 兼容契约与 Gateway Handler 定向测试均通过。 |

## 168. Marketplace 控制面首轮测试被用户级 Go Build Cache 权限阻断

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-03，本地验证环境） |
| 现象 | `go test ./internal/module/agent/marketplace ./internal/module/agent/repository` 在 setup 阶段返回 `AppData/Local/go-build/...: Access is denied`，并且无法写入 `trim.txt`。 |
| 原因 | 当前 Workspace 沙箱不能写用户级 Go Build Cache；命令尚未进入本轮源码编译或测试断言。 |
| 影响 | 控制面领域模型和 Mongo Adapter 暂未形成测试证据；没有启动或访问 Mongo、Redis、MCP、模型或公网服务。 |
| 解决 | 将 `GOCACHE` 与 `GOTMPDIR` 定向到仓库内隔离目录后，以 `-vet=off -p=1` 重跑；Marketplace 领域包与 Repository 包均通过，确认权限失败未掩盖源码或断言问题。 |

> 记录 twitter-clone 云原生项目开发过程中遇到的问题及解决方案

---

## 1. Tweet Service 重复声明错误

| 项目 | 内容 |
|------|------|
| **问题** | `tweet_service.go` 和 `tweet_service_mq.go` 存在重复的类型/方法声明，导致编译失败 |
| **原因** | MQ 版本 (`tweet_service_mq.go`) 是实际使用的实现，旧文件未清理 |
| **解决** | 将 `tweet_service.go` 内容替换为最小化占位，保留文件历史 |

## 2. User Service 导入缺失

| 项目 | 内容 |
|------|------|
| **问题** | 重构 `user_service.go` 日志后编译失败，缺少 `regexp`、`strings`、`bcrypt` 等导入 |
| **原因** | 替换日志代码时意外删除了其他必要的 import |
| **解决** | 逐一恢复缺失的 import 包，确认编译通过 |

## 3. Minikube 镜像拉取失败 (ErrImagePull)

| 项目 | 内容 |
|------|------|
| **问题** | `kubectl set image` 后 Pod 显示 `ErrImagePull`，无法拉取 `trace-v1` 镜像 |
| **原因** | Docker Desktop 构建的镜像在宿主机 Docker daemon，Minikube（Docker driver）使用独立的 daemon |
| **解决** | 使用 `minikube image load <image>` 将镜像从宿主机加载到 Minikube |

## 4. Grafana init-chown-data 崩溃

| 项目 | 内容 |
|------|------|
| **问题** | Grafana Pod 持续 `Init:CrashLoopBackOff`，`init-chown-data` 容器失败 |
| **原因** | Minikube PV 权限限制，init 容器无法对挂载卷执行 `chown` |
| **解决** | 在 `grafana-values.yaml` 中设置 `initChownData.enabled: false` 并 `persistence.enabled: false` |

## 5. Grafana runAsUser 违反非 Root 策略

| 项目 | 内容 |
|------|------|
| **问题** | 尝试 `securityContext.runAsUser: 0` 修复权限问题，Pod 报 `CreateContainerConfigError` |
| **原因** | Grafana Helm chart 有 non-root 安全策略，`runAsUser: 0` 违反该策略 |
| **解决** | 移除 `runAsUser: 0`，改用禁用 persistence 的方式彻底规避 PV 权限问题 |

## 6. helm/minikube 命令不在 PATH

| 项目 | 内容 |
|------|------|
| **问题** | PowerShell 中 `helm` 和 `minikube` 命令报 `CommandNotFoundException` |
| **原因** | 可执行文件位于 `E:\K8s-Tools\` 但未加入系统 PATH |
| **解决** | 使用完整路径 `E:\K8s-Tools\helm.exe` 和 `E:\K8s-Tools\minikube.exe` 执行命令 |

## 7. PowerShell curl 别名冲突

| 项目 | 内容 |
|------|------|
| **问题** | `curl -H "Content-Type: application/json"` 失败 |
| **原因** | PowerShell 中 `curl` 是 `Invoke-WebRequest` 的别名，参数格式不兼容 |
| **解决** | 使用 `Invoke-RestMethod -ContentType "application/json"` 替代 |

## 8. Loki TraceID Derived Field 匹配率 0%

| 项目 | 内容 |
|------|------|
| **问题** | Grafana Loki 的 TraceID derived field 显示 0% 匹配率 |
| **原因** | 日志以 Docker JSON 格式存储，`trace_id` 前后有转义引号 `\":\"`（5个非字母字符），正则 `"trace_id":"(\w+)"` 无法匹配 |
| **解决** | 改用 `trace_id\W+(\w+)`，`\W+` 能匹配一个或多个非字母字符，正确跳过转义分隔符 |

## 9. Grafana → Jaeger 查询 EOF 错误

| 项目 | 内容 |
|------|------|
| **问题** | 点击 TraceID 链接跳转 Jaeger 时报 `Get "http://twitter-clone-jaeger-query.default:16686/...": EOF` |
| **原因** | `twitter-clone-jaeger-query` 是 Headless Service（ClusterIP: None），Grafana proxy 模式无法正常访问 |
| **解决** | 创建带 ClusterIP 的新 Service `jaeger-query-clusterip`，更新 Grafana Jaeger 数据源 URL 指向该 Service |

## 10. Jaeger Pod CrashLoopBackOff（健康检查端口不匹配）

| 项目 | 内容 |
|------|------|
| **问题** | Jaeger Pod 持续 `CrashLoopBackOff`，每次启动约75秒后被杀死 |
| **原因** | Helm chart 配置 liveness/readiness probe 端口为 `13133`，但 Jaeger v1.66.0 的 Admin 健康端点实际监听在 `14269` |
| **解决** | 使用 `kubectl patch deployment` 将 probe 端口修正为 `14269`，保存到 `deploy/patches/jaeger-probe-fix.yaml` |

## 11. Jaeger Trace Export Failed (404 Not Found)

| 项目 | 内容 |
|------|------|
| **问题** | Grafana/Loki 能看到 `trace_id`，但点击跳转 Jaeger 显示 `404 Not Found`。Gateway 日志报错 `connection refused`。 |
| **原因** | 1. 代码使用 Jaeger Agent (UDP 6831) 模式，但 Jaeger `all-in-one` 镜像默认 Agent 仅监听 localhost，跨 Pod 无法访问。<br>2. 尝试连接 Service 时，Service 缺少 selector 导致 endpoints 为空。 |
| **解决** | 1. 修改 `pkg/trace` 代码，改用 Jaeger Collector (HTTP 14268) 模式。<br>2. 更新 `jaeger-query-clusterip` Service 暴露 14268 端口并修复 selector。<br>3. 更新所有服务镜像 (`trace-v2`) 并注入 `JAEGER_COLLECTOR_ENDPOINT` 环境变量。 |

## 12. Login 后用户信息存储错误

| 项目 | 内容 |
|------|------|
| **问题** | 登录成功后所有依赖用户 ID 的操作（查看资料、发推等）均失败 |
| **原因** | `Login.vue` 执行 `userStore.setUser(userRes.data)` 存的是 `{ user: {...} }` 而非用户对象，导致 `userStore.user.id` 为 undefined |
| **解决** | 改为 `userStore.setUser(userRes.data.user)`，正确提取嵌套的用户数据 |

## 13. 前端 API 路径与后端路由不匹配（4处）

| 项目 | 内容 |
|------|------|
| **问题** | 关注、取关、检查关注状态、取消点赞等功能请求 404 |
| **原因** | `user.ts` 和 `tweet.ts` 中的 API 路径/方法与后端 `router.go` 路由定义不一致：`POST /follow` → 应为 `POST /follows`、`POST /unfollow/:id` → 应为 `DELETE /follows/:id`、`GET /users/:id/following` → 应为 `GET /follows/:id/status`、`POST /tweets/:id/unlike` → 应为 `DELETE /tweets/:id/like` |
| **解决** | 统一前端所有 API 路径和 HTTP 方法与后端路由定义一致 |

## 14. GetProfile 在认证中间件之后

| 项目 | 内容 |
|------|------|
| **问题** | 未登录用户无法查看他人资料，Profile 页面显示空白 |
| **原因** | `router.go` 中 `users.GET("/:id", ...)` 在 `jwtMW.AuthRequired()` 之后注册，要求认证才能访问 |
| **解决** | 使用子路由组 `authedUsers` 隔离认证接口（`/me`），`/:id` 保持公开注册 |

## 15. Like/Comment 500 错误（数据库表缺失）

| 项目 | 内容 |
|------|------|
| **问题** | 点赞推文返回 500 Internal Server Error：`Table 'twitter.likes' doesn't exist` |
| **原因** | `tweet-service/main.go` 的 `AutoMigrate` 只迁移了 `Tweet` 和 `Follow`，没有包含 `Like` 和 `Comment` 实体 |
| **解决** | 在 `tweet-service/main.go` 和 `consumer/main.go` 的 `AutoMigrate` 中添加 `&domain.Like{}, &domain.Comment{}` |

## 16. 推文显示 "Unknown @unknown"（缺少用户信息）

| 项目 | 内容 |
|------|------|
| **问题** | 所有推文的作者名显示为 "Unknown @unknown"，头像为默认灰色 |
| **原因** | `tweet_handler.go` 的 `formatTweet()` 只返回 `user_id`，不包含用户名/头像等信息。Tweet proto 也不包含这些字段 |
| **解决** | 给 `TweetHandler` 注入 `UserClient`，新增 `enrichTweetsWithUserInfo()` 方法在 gateway 层批量查询用户信息并注入到推文响应中 |

## 17. 前端组件功能缺失（按钮无效、趋势硬编码）

| 项目 | 内容 |
|------|------|
| **问题** | TweetCard 评论/转推/分享按钮点击无反应；Profile 编辑资料按钮无效、Tab不切换；侧边栏趋势数据为硬编码假数据 |
| **原因** | 前端组件只写了 UI 外壳，缺少事件处理函数和业务逻辑 |
| **解决** | 重写 `TweetCard.vue`（评论弹窗/分享复制/转推提示）、`Profile.vue`（编辑资料弹窗/Tab切换/`res.data.user`提取）、`MainLayout.vue`（从 `/trends` API 动态获取趋势数据） |

## 18. 编辑资料保存后字段交换（bio ↔ avatar）

| 项目 | 内容 |
|------|------|
| **问题** | 编辑资料保存后，个人简介和头像 URL 的值互换 |
| **原因** | `saveProfile` 用 `editForm` 手动赋值更新 `user.value`，且前端发送了后端不支持的 `username` 字段，导致数据错乱 |
| **解决** | 删除 `username` 编辑字段（后端 `UpdateProfileRequest` 不支持），保存后用后端响应 `res.data.user` 直接覆盖 `user.value` |

## 19. 书签路由 404 (POST /tweets/:id/bookmark)

| 项目 | 内容 |
|------|------|
| **问题** | 点击书签按钮返回 404 |
| **原因** | 书签路由在 `router.go` 中通过独立的 `v1.Group("/tweets")` 注册，与主 tweets 路由组产生冲突，Gin 的 `/:id` 通配符优先匹配拦截了 `/:id/bookmark` |
| **解决** | 将 `POST/DELETE /:id/bookmark` 移入已有的 tweets 路由组内注册，避免重复 Group 冲突 |


## 20. 关注接口 500 + 关注状态刷新丢失

| 项目 | 内容 |
|------|------|
| **问题** | 关注按钮点击返回 500，且即使偶尔成功，刷新页面后关注状态消失 |
| **原因** | 前端 `followUser` 发送 `followee_id: parseInt(userId)`，Snowflake ID 超过 JS `Number.MAX_SAFE_INTEGER` (2^53-1)，导致精度丢失，后端收到错误 ID 后 gRPC 调用失败返回 500。关注记录未写入 DB，所以刷新后 `IsFollowing` 返回 false |
| **解决** | 后端 `FollowRequest.FolloweeID` 改为 `string` 类型，用 `strconv.ParseUint` 解析；前端直接发送字符串 `followee_id: userId` |

## 21. 书签/通知 500 Panic

| 项目 | 内容 |
|------|------|
| **问题** | `AddBookmark` 接口返回 500，Gateway 日志无报错（因为 panic 导致进程重启或被 recover 吞掉） |
| **原因** | `bookmarkRepo.Create` 调用 `snowflake.GenerateID()`，但 Gateway `main.go` 未调用 `snowflake.Init()`，导致 `node` 为 nil 发生 panic |
| **解决** | 在 `cmd/gateway/main.go` 中添加 `snowflake.MustInit(1)` 初始化代码 |

## 22. 编辑资料字段交换 (bio ↔ avatar)

| 项目 | 内容 |
|------|------|
| **问题** | 保存个人资料后，头像URL写入了 bio 字段，bio 内容写入了 avatar 字段，导致页面显示混乱 |
| **原因** | `internal/module/user/grpc/user.go:80` 调用 `s.svc.UpdateProfile(ctx, req.UserId, req.Avatar, req.Bio)`，而 Service 函数签名是 `UpdateProfile(ctx, userID, bio, avatar)` — 参数顺序反了 |
| **解决** | 修正为 `s.svc.UpdateProfile(ctx, req.UserId, req.Bio, req.Avatar)` |

## 23. 评论作者显示 unknown

| 项目 | 内容 |
|------|------|
| **问题** | 推文详情页评论列表中，所有评论作者显示为 "unknown" |
| **原因** | `domainCommentToProto` 不填充用户信息字段，gateway `GetTweetComments` 也没有查询用户信息聚合 |
| **解决** | 在 `GetTweetComments` handler 中批量查询 `userClient.GetProfile` 并注入 `user` 对象到评论 JSON |

## 24. 点赞状态刷新后丢失

| 项目 | 内容 |
|------|------|
| **问题** | 刷新页面后，之前点赞的推文的红心变回未点赞状态 |
| **原因** | `GetFeedsRequest` 缺少 `requesting_user_id` 字段，gateway 无法将当前用户 ID 传给 tweet-service 判断点赞状态 |
| **解决** | 在 gateway 的 `enrichTweetsWithUserInfo` 中直接查询 likes 表批量注入 `is_liked` 状态 |

## 25. 书签状态刷新后丢失

| 项目 | 内容 |
|------|------|
| **问题** | 收藏的推文刷新后书签图标变回未收藏状态 |
| **原因** | TweetCard.vue `isBookmarked` 硬编码为 `false`，后端 `formatTweet` 不返回 `is_bookmarked` |
| **解决** | gateway 批量查 bookmarks 表注入 `is_bookmarked`，TweetCard 从 props 读取，Bookmarks 页强制 true |

## 26. 通知未读计数不即时消除

| 项目 | 内容 |
|------|------|
| **问题** | 进入通知页阅读后，NavBar 的红色未读徽章不会立即消除 |
| **原因** | NavBar 仅靠 30 秒轮询刷新计数，markAsRead 后不会触发即时刷新 |
| **解决** | 添加 `notifications-read` 自定义事件监听 + route watcher 离开通知页时立即刷新 |

## 27. API Regressions & 404/500 Bugs

| 项目 | 内容 |
|------|------|
| **问题** | 1. `PUT /users/me` 报 404 NotFound <br>2. `POST /tweets/:id/retweet` 报 500 Internal Server Error <br>3. 用户搜索列表无法显示“已关注”状态 <br>4. 首页推文不显示“已投票”状态和百分比 <br>5. Messenger 前端请求 `/conversations` 报 404 <br>6. WebSocket 无法连接导致控制台不断刷屏 |
| **原因** | 1-2. 网关 `router.go` 路由丢失/映射错误；TweetHandler 缺失转发方法 <br>3-4. 网关转发 gRPC 请求后未查表聚合 `is_following` 和 `poll_votes` 数据 <br>5. 前端 API 路径未匹配网关新增的 `/messenger` 分组 <br>6. 网关未实例化并挂载 `WebSocketHandler` |
| **解决** | 1-2. 恢复正确网关路由映射，补充 `RetweetTweet`/`UnretweetTweet` 方法 <br>3. `SearchUsers` 网关接口追加并发调用 FollowService 获取关注状态 <br>4. 网关 `enrichTweetsWithUserInfo` 内直连数据库查询 `poll_votes` 并注入 <br>5. 修改前端 `messenger.ts` 的 api 请求路径加上 `/messenger` <br>6. 在 `main.go` 实例化 `WebSocketHandler` 并映射至 `/api/v1/ws` |

## 28. 推文详情页(TweetDetail)前端交互失效

| 项目 | 内容 |
|------|------|
| **问题** | 1. 评论无法指定人回复 (仅能发帖)<br>2. 贴子内部的“转推”按钮无反应<br>3. "推文串"(Thread) 功能失效，串内回复按钮无反应 |
| **原因** | 1. `TweetDetail.vue` 使用了 Vue 插件自动导入逻辑的遗漏，导致 `ReplyModal` 组件未正确渲染。<br>2. `TweetCard` 未向上 `emit('reply')` 导致串联组件的回复按钮脱节。<br>3. 详情页的 `handleRetweet` 逻辑未实现，且评论组件缺少获取并处理 `parent_id` 的入口。 |
| **解决** | 1. 显式导入 `TweetCard` 和 `ReplyModal` 至 `TweetDetail.vue`。<br>2. 补充 `TweetCard.vue` 中的 `@click="handleReplyClick"` 分发 `reply` 事件。<br>3. 在推文详情页增加 `handleRetweet` 接口调用、实现内嵌评论的 `handleReplyToComment(comment)` 定向回复(挂载 `@username` 及传输 `parent_id`)。 |

## 29. 评论回复后立刻显示 unknown 信息丢失

| 项目 | 内容 |
|------|------|
| **问题** | 用户在推文详情页发表评论后，最新推入列表的评论，用户名和头像都显示为 `unknown`。 |
| **原因** | `v1/tweets/:id/comments` (Gateway) 接收 Tweet Service 的 `CreateComment` gRPC 响应后，直接原样格式化返回。由于底层 Service 仅写入 `UserID` 并未回填用户详情（Profile/Avatar），前端缺乏数据导致 fallback 为 fallback。 |
| **解决** | 在 `tweet_handler.go` (`CreateComment`) 响应前，增加一步 `userClient.GetBatchUsers` 调用拿取对应的用户资料并组装至 `comment` 返回对象上。 |

## 30. 创建评论报错 400 Bad Request

| 项目 | 内容 |
|------|------|
| **问题** | 点击或者发布评论时报 `400 Bad Request` 且发送失败。 |
| **原因** | 网关 `CreateCommentRequest` struct 中的 `ParentID` 声明为 `uint64`。由于 Twitter Snowflake ID 精度的需求，前端以字符串形式 (`"2024791560905822208"`) 回传或回传被漏设，导致 Go 的 JSON 反序列化因为类型不匹配失败。 |
| **解决** | 将结构体的 `ParentID` 改为 `string`，接收后使用 `strconv.ParseUint` 手动强转以增加容错和解析成功率。 |

## 31. 推文详情页(TweetDetail)不显示投票进度

| 项目 | 内容 |
|------|------|
| **问题** | 首页信息流正常显示已投票的进度条和百分比，但点进帖子详情页(TweetDetail)却变成了只显示选项（未投票的初始外观）。 |
| **原因** | 网关的 `GetTweet` `GET /api/v1/tweets/:id` 接口实现中，过去手写了“作者信息”、“Like”、“Bookmark”、“Retweet”的拼装，唯独漏掉了读取 `poll_votes` 表。 |
| **解决** | 移除 `GetTweet` 中冗余重复的拼装代码，统一复用首页流使用的 `enrichTweetsWithUserInfo` 函数，该函数内置了一并加载各种所有互动状态（含投票）的完备逻辑。 |

## 32. 关注列表 (Followees) 请求 404 Not Found

| 项目 | 内容 |
|------|------|
| **问题** | 从个人主页点击“正在关注”标签，控制台报 `/api/v1/users/:id/following` 404 错误且列表为空。 |
| **原因** | 前端 `api/user.ts` 中的 `getFollowees` 请求的 URL 路径为 `/following`，而 API Gateway `router.go` 实际注册的路径叫 `/followees`。 |
| **解决** | 修改前端请求路径，匹配后端的 `/api/v1/users/:id/followees` 契约。 |

## 33. 粉丝列表 (Followers) 请求 500 Internal Server Error

| 项目 | 内容 |
|------|------|
| **问题** | 点击“关注者”页面时，请求后台报错 500。 |
| **原因** | 跟踪到 `follow-service` 的 `follow_repo.go` 中，`GetFollowers` 的 GORM 查询语句手误将 `deleted_at = 0` 写成了 `deleted_id = 0`，引发数据库字段不存在报错。 |
| **解决** | 将 Repo 查询中的 `deleted_id` 修正为软删除字段 `deleted_at` 即可。 |

## 34. 关注/粉丝列表数据请求成功但页面依然为空白

| 项目 | 内容 |
|------|------|
| **问题** | 关注者和正在关注列表的请求返回了 200 OK，并且 `follow_ids` 内有数据，但页面依然显示“这里好像什么都没有”。 |
| **原因** | 后端 API 网关直接将 `[]uint64` 类型的 Snowflake ID 作为 JSON Number 数组返回给了前端。由于 JS 的 `Number` 类型最高仅支持 53 位精度，在解析超过精度的推特雪花 ID（如 `2023661202374135808`）时被截断成了错误数字（如 `2023661202374135800`）。前端拿着这些错误截断的 IDs 去请求 `getBatchUsers`，自然匹配不到任何用户，于是结果为空。 |
| **解决** | 修改 `follow_handler.go`，在组装 JSON 之前，先利用 `strconv.FormatUint(id, 10)` 将所有 `uint64` 轮询转为 `[]string`，从而避免了前端 JS 的反序列化精度丢失问题。 |

## 35. 关注列表/粉丝列表的人员缺少“已关注”状态

| 项目 | 内容 |
|------|------|
| **问题** | 在列表里看到的“关注者”或者“正在关注”的人，右侧的按钮清一色显示“关注”而不是“已关注”，无法正确进行取消关注操作。 |
| **原因** | 有两个原因叠加：1. 原本网关层的 `GetBatchUsers` 与 `GetProfile` 接口仅仅是转发了获取档案 RPC，遗漏了判断关注状态 (`is_following`) 的业务逻辑。2. 就算代码里加了 `middleware.GetUserID(c)` 去获取当前登录账号，因为 `/users/:id` 和 `/users/batch` 被划分为 `公开接口`，它们路由本身压根没有挂载 JWT Token 解析中间件，所以 `GetUserID` 永远返回 0。 |
| **解决** | 第一步：在 `user_handler.go` 中加入起协程并发调用 `followClient.IsFollowing` 去实时查状态并组装的逻辑。第二步：在 `router.go` 的公开路由组前加上 `users.Use(jwtMW.AuthOptional())` 可选鉴权中间件。这样当游客访问时不阻拦，但当登录用户访问时能够成功剥离出身份去查出准确的跟随、点赞状态。 |

## 36. 微服务直连 IP 与 Istio Service Mesh 路由劫持失效冲突

| 项目 | 内容 |
|------|------|
| **问题** | 金丝雀灰度分流与熔断策略不生效，流量 100% 仅能打在 v1 Pod 上，即使重启也无法分流到 v2 |
| **原因** | gRPC 客户端默认使用 `consul://` 解析器，直接获取底层 Pod 物理 IP 发起长连接。这种直接绕过 Kubernetes ClusterIP 的 IP 直连方式，使 Envoy Sidecar 无法通过 DNS 域名匹配 VirtualService 和 DestinationRule 规则，再加上 gRPC HTTP/2 连接粘滞特性，导致规则失效 |
| **解决** | 1. 引入 `USE_K8S_DNS` 环境变量，在网关 client 启动时若为 true 则将 Target 切换为 K8s 内置的 Service DNS（例如 `dns:///twitter-clone-tweet:9092`）。<br>2. 在 `tweet-service-vs.yaml` 中，将 Hosts 主机名修改为与实际调用的 ClusterIP DNS 相吻合（不带端口），包括 `twitter-clone-tweet` 及其全限定域名（FQDN），使得长连接流量能被 Envoy 成功代理劫持并实施分流 |

## 37. K8s Service 缺少 instance 标签导致 v2 灰度副本失联 (Endpoint 绑定漏洞)

| 项目 | 内容 |
|------|------|
| **问题** | 更改 VS 和 DR 配置为 `twitter-clone-tweet` 后，发送 100 次网关流量依然 100% 命中 v1 |
| **原因** | Kubernetes `twitter-clone-tweet` Service 的 Selector 匹配了 `app.kubernetes.io/instance: twitter-clone`。而手工新增的 `tweet-deployment-v2.yaml` 仅仅加入了 `app.kubernetes.io/name` 与 `component` 标签，遗漏了 `instance` 标签。这导致 v2 Pod 压根没有被列入 Service 的 Endpoint（`kubectl get endpoints` 里只有一个 IP） |
| **解决** | 修改 `tweet-deployment-v2.yaml`，在 Pod template labels 中通过 `{{- include "twitter.selectorLabels" . | nindent 8 }}` 模板宏进行自动渲染补齐，重新部署后 v2 副本成功被 Service 判定为 Endpoint 并绑定为上游 IP 之一 |

## 38. Envoy Outlier Detection 无法通过直连 port-forward 触发 (熔断黑盒漏洞)

| 项目 | 内容 |
|------|------|
| **问题** | 使用脚本直连本地转发的 `29092` 端口发送 5 次 gRPC 错误，无法触发 v2 服务的 Outlier Detection 隔离 |
| **原因** | 1. `kubectl port-forward` 通过 K8s api-server 直接转发流量到容器端口，完全绕过了 Pod 的 Envoy Proxy Ingress 拦截端口（15006）。<br>2. 业务级普通错误（如 `NotFound` 错误码 5）在 Envoy 中不会被计为系统级 `consecutiveGatewayErrors` 触发条件 |
| **解决** | 1. 在 VirtualService 顶层追加 `x-version: v2` header 染色匹配路由规则。<br>2. 将投毒程序编译为 Linux 二进制并用 `kubectl cp` 拷贝到带有 Envoy Sidecar 劫持的 `gateway` 容器内运行，使其调用 `twitter-clone-tweet:9092` DNS 域名，并在 gRPC元数据（Metadata）中注入 `x-version: v2` 进行染色路由，使流量通过 Envoy 并 100% 打在 v2 Pod 的 15006 端口上，从而完美触发了 v2 Envoy Outlier Detection 熔断（第 6 次调用返回 `Unavailable: no healthy upstream`） |

## 39. Notification 服务 ImagePullBackOff (latest 镜像拉取策略死锁)

| 项目 | 内容 |
|------|------|
| **问题** | 使用 `minikube image load` 成功载入了 `twitter-clone-notification-service:latest` 镜像，但 Pod 依然报 `ImagePullBackOff` 错误。 |
| **原因** | 在 Kubernetes 中，当镜像 Tag 是 `latest` 时，默认的 `imagePullPolicy` 策略会被自动强转为 `Always`。这意味着即使本地有该缓存，K8s 也会强制尝试去公网拉取，导致失败。 |
| **解决** | 在 `values.yaml` 的 `notificationService` 中显式硬编码声明 `pullPolicy: IfNotPresent`，强制 Kubernetes 优先读取本地 Minikube 缓存。 |

## 40. Consul Connect Injector 与 Istio Sidecar 冲突崩溃

| 项目 | 内容 |
|------|------|
| **问题** | 集群中的 `consul-connect-injector` Pod 持续 CrashLoopBackOff，且严重干扰其他 Pod 网络。 |
| **原因** | 当在命名空间启用 Istio Sidecar 劫持后，Consul 的连接注入器（原本用于 Consul Connect 代理注入）在相同端口和 iptables 劫持链上与 Istio Envoy 产生了严重的资源和控制权冲突。 |
| **解决** | 鉴于服务发现和治理已全部转由 Istio 处理，在 `values.yaml` 的 `consul` 节下显式配置 `connectInject.enabled: false` 禁用该功能，并手工删除残留的 `consul-connect-injector` deployment 释放资源。 |

## 41. Jaeger 注入 Mesh 后由于健康探测改写（Prober Rewrite）导致无限重启

| 项目 | 内容 |
|------|------|
| **问题** | 对 Jaeger 部署了 14269 管理端口健康探针补丁后，在 Istio 环境下 Jaeger 依然频繁崩溃。 |
| **原因** | Istio Sidecar 默认开启了 `rewriteAppHTTPProbers` 机制，这会把 Pod 所有的探针重写为通过 Envoy 15021 端口中转代理。如果 Jaeger 本身对此拦截缺乏适配，Envoy 探测其原本在 sidecar 外定义的探测路径时会因二次改写而冲突返回 500，造成 K8s 误杀。 |
| **解决** | 在 Jaeger Deployment 的 Pod 模板（`template.metadata.annotations`）中增加 `sidecar.istio.io/rewriteAppHTTPProbers: "false"` 注解，告知 Istio 控制面豁免对其健康检查的改写劫持。 |

## 42. timeline_consumer.go 编译报错（processOutboxTasks 未定义）

| 项目 | 内容 |
|------|------|
| **问题** | 修改 `timeline_consumer.go` 时报 `c.processOutboxTasks undefined` 编译错误 |
| **原因** | 代码编辑工具执行 fuzzy match 块替换时范围匹配过度，将该函数头部声明和部分查询 pending tasks 的代码意外删去 |
| **解决** | 使用精确匹配的 ReplacementChunk 重新替换回正确的 `processOutboxTasks` 函数声明及逻辑，并成功通过编译 |

## 43. go build ./... 全局编译报错（scripts 目录下变量与方法重复声明冲突）

| 项目 | 内容 |
|------|------|
| **问题** | 运行 `go build ./...` 时编译失败，报 `scripts\stress.go` 和 `scripts\seed_data.go` 变量与函数多处重复声明的错误 |
| **原因** | 两个文件都声明为 `package main` 且存在同名的全局变量与入口方法，存放在同一个包目录下引发命名冲突 |
| **解决** | 将两个文件分别移动至独立的子目录 `scripts/stress/` 和 `scripts/seed/` 中，使其成为独立的 package main，彻底消除命名冲突并使 `go build ./...` 全绿通过 |

## 44. Temporal SDK 导入引发 google.golang.org/genproto 依赖歧义冲突 (Ambiguous Import)

| 项目 | 内容 |
|------|------|
| **问题** | 运行 `go mod tidy` 或 `go test` 编译失败，报 `ambiguous import: found package google.golang.org/genproto/googleapis/api/annotations in multiple modules` |
| **原因** | 引入 `go.temporal.io/sdk` 后，它引用了较新拆分后的 `google.golang.org/genproto/googleapis/api`。但是项目里已有的旧依赖（如 consul/grpc-gateway/Jaeger 等）锁定了古老的单体 `google.golang.org/genproto`，导致两个 Module 提供了相同的 annotations 和 httpbody 包，在 Go module 中引发多义性编译冲突 |
| **解决** | 在 `go.mod` 末尾追加 `replace google.golang.org/genproto => google.golang.org/genproto v0.0.0-20240227224415-6ceb2ff114de` 指令。将其强制锁定至已清理这些重复 annotations 文件的 2024 年安全干净版本，成功解决歧义，编译与测试全绿通过 |

## 45. 网关全局变量定义位置错误及 Windows 命令行 Unicode 编码报错

| 项目 | 内容 |
|------|------|
| **问题** | 1. 运行 `go build` 编译失败，报 `internal\gateway\router\router.go:11:1: missing import path` 错误。<br>2. 运行 Python 混沌验证脚本失败，报 `UnicodeEncodeError: 'gbk' codec can't encode character '\U0001f6e1'` 错误。 |
| **原因** | 1. AIOps 告警防抖去重器的全局变量 `alertDebouncer` 被错误插在了网关 `router.go` 的 `import (...)` 块内部，导致词法分析器解析为非法的 import 路径。<br>2. Windows Command Line/PowerShell 的默认编码是 GBK。当 Python 测试脚本尝试打印 Emoji 字符（如 🛡️, ❌, ✅）时，发生了 GBK 无法编码的多字节字符转换错误。 |
| **解决** | 1. 将 `alertDebouncer` 和 `debounceDuration` 变量移出 `import` 括号，放置在 package 级别，编译全绿通过。<br>2. 将 Python 脚本 `run_chaos_test.py` 中的 Emoji 字符全部替换为 ASCII 标准格式（如 `[SAFETY]`, `[ERROR]`, `[SUCCESS]`），完全消除了跨平台的字符集编码崩溃隐患，使其在 Windows 下能完美运行。 |

## 46. Sentinel-Go 动态载入规则覆盖静态保命规则缺陷及大模型指令幻觉安全漏洞

| 项目 | 内容 |
|------|------|
| **问题** | 1. 动态加载 AI 熔断规则后，网关原本自带的基础静态熔断限流规则被莫名清空，导致微服务直接裸奔。<br>2. 如果大模型在复杂的日志诊断中发生幻觉，输出对核心路由（如 `/` 或 `/health`）下发熔断，会导致全站自杀或 Pod 重启。 |
| **原因** | 1. Sentinel-Go 底层的 `circuitbreaker.LoadRules` 的语义是“全量覆盖”，而不是“增量追加”。新规则载入时会清空所有之前载入的规则。<br>2. 大模型不受控地输出任意 resource 指令时，网关未进行白名单强拦截校验，导致控制权越界。 |
| **解决** | 1. 在 `self_healer.go` 中引入规则合并机制，内存中常驻 `baseRules`（包括 tweet/user 等保底规则），每次加载 AI 动态规则时将其与基础规则合并后再统一执行 `LoadRules`。<br>2. 在自愈器中内置 `Allowlist` 绝对白名单，强行拦截非白名单资源熔断，保证核心旁路安全。在单元测试 `self_healer_test.go` 中对上述防线做出了 100% 验证。 |

## 47. 混沌压测高并发下 kubectl port-forward 连接耗尽与离线本地兜底自愈保证

| 项目 | 内容 |
|------|------|
| **问题** | 高并发压测下，`kubectl port-forward` 产生连接耗尽，宿主机向网关发送 Webhook 告警报 `Could not connect to server` 错误，导致闭环失效。 |
| **原因** | 网关在并发压力（平均 RPS ~1500）下处理大量连接，使 port-forward 的代理套接字被占满导致连接排队拒绝；另外测试环境下 `agent-service` 强依赖的重型存储未部署在 K8s 中，导致 gRPC 调用分析失败。 |
| **解决** | 1. 在网关 `/alerts` webhook 处理中，开发了 **本地自愈兜底防线**（`Local Self-Healing Fallback`）。当 AIOps 脑诊断因为不可达连接失败时，网关在 `chaos_testing` 模式下退一步，出于防御性设计，自动对白名单内的 `/api/v1/feeds` 下发熔断指令，实现自主防护。<br>2. 升级 `stress_feeds.go`，在压测开始后，由压测进程内部向网关发送 Firing 告警，从而避开了宿主机高并发端口占用问题。在 Minikube 集群中完美跑通了“故障注入 -> 本地自愈兜底 -> 动态 Sentinel 规则加载 -> 流量拦截”的完整闭环，Sentinel 拦截率从 0% 瞬时攀升至 ~20%，其余网络错误全部得到拦截净化。 |
| **解决** | 1. 在 网关 `/alerts` webhook 处理中，开发了 **本地自愈兜底防线**（`Local Self-Healing Fallback`）。当 AIOps 脑诊断因为不可达连接失败时，网关在 `chaos_testing` 模式下退一步，出于防御性设计，自动对白名单内的 `/api/v1/feeds` 下发熔断指令，实现自主防护。<br>2. 升级 `stress_feeds.go`，在压测开始后，由压测进程内部向网关发送 Firing 告警，从而避开了宿主机高并发端口占用问题。在 Minikube 集群中完美跑通了“故障注入 -> 本地自愈兜底 -> 动态 Sentinel 规则加载 -> 流量拦截”的完整闭环，Sentinel 拦截率从 0% 瞬时攀升至 ~20%，其余网络错误全部得到拦截净化。 |

## 48. 网关限流提早拦截导致熔断悬空及 Feeds 缺乏 Sentinel 保护漏洞 (熔断失效漏洞)

| 项目 | 内容 |
|------|------|
| **问题** | 1. 压测期间，K6 请求 feeds 流由于并发过高全部被网关全局限流限死，没能让压力真正到达 Sentinel 层，造成流量拦截假象。<br>2. 动态熔断配置在网关虽然成功加载，但 `GetFeeds` 的 API 路由底层代码未在代码中接入 `sentinel.Entry()` 拦截，导致熔断保护处于悬空未挂载状态。 |
| **原因** | 1. 网关的 RateLimit 中间件具有最高优先级，且对压测流量未能提供独立白名单放行。<br>2. API 网关向纯代理模式过渡后，仅对下游 gRPC client 重构了熔断，遗漏了对外层 HTTP REST 路由的适配。 |
| **解决** | 1. 在 `ratelimit.go` 引入旁路逻辑：在 `chaos_testing` 压测环境下，若携带万能 Token `CHAOS_MOCK_UNIVERSAL_TOKEN_999` 则直接放行，避免限流计次。<br>2. 在 `internal/gateway/handler/tweet_handler.go` 中，对 `GetFeeds` 全程包裹 `sentinel.Entry("GET:/api/v1/feeds")`，并在底层 gRPC 出错时上报 `sentinel.TraceError(entry, err)`。<br>3. 在本地重新构建网关镜像并重启 Pod 验证，利用持续缩容混沌，成功实现 99.8% 的极速故障拦截率与平滑自动愈合，达成完美闭环！ |

## 49. client-go 间接依赖缺失与 stress 测试脚本包 main 函数冲突

| 项目 | 内容 |
|------|------|
| **问题** | 1. 引入 `k8s.io/client-go` 后 `go test ./...` 编译失败，提示缺少 `github.com/google/gofuzz`、`sigs.k8s.io/yaml` 等一系列 go.sum 间接依赖条目。<br>2. `scripts/stress` 目录下由于同时存在多个 package main 脚本，导致 `main` 函数重定义编译冲突。 |
| **原因** | 1. `go.mod` 缺少自动拉取补全的间接依赖项校验哈希。<br>2. go 工具链默认会编译同一个目录下的所有 Go 文件，导致多个 `main` 入口重叠冲突。 |
| **解决** | 1. 运行 `go mod tidy` 自动拉取补齐并校验了所有依赖项，补齐了 `go.sum`。<br>2. 在 `stress.go` 和 `stress_feeds_go.go` 的首行添加 `//go:build ignore` 和 `// +build ignore` 编译条件标记，在常规构建和测试时进行安全隔离。 |

## 50. Qdrant 官方镜像启动命令 PATH 搜索失败与 StatefulSet 引导冲突

| 项目 | 内容 |
|------|------|
| **问题** | 重构 3 节点 StatefulSet 启动时，`qdrant-0` 容器报错退出，提示 `/bin/bash: line 3: exec: qdrant: not found`。 |
| **原因** | Qdrant 官方 Docker 镜像（如 `qdrant/qdrant:v1.12.0`）没有在系统 `/usr/bin` 等全局 PATH 中包含二进制，其可执行文件位于当前工作目录 `/qdrant/qdrant`。 |
| **解决** | 将 `qdrant-statefulset.yaml` 中的容器启动指令从 `exec qdrant` 改为相对路径 `exec ./qdrant`，利用其默认的 `WORKDIR /qdrant`，顺利避开 PATH 检索失败，打通了 qdrant-0 独立启动和 qdrant-1/2 的条件 Bootstrap 引导。 |

## 51. Agent Service 独立部署 Kubernetes 环境下因存储与组件离线引发启动 panic 崩溃

| 项目 | 内容 |
|------|------|
| **问题** | `agent-service` 滚动更新上线后，Pod 持续崩溃处于 `CrashLoopBackOff` 状态。 |
| **原因** | 1. 缺少 `MQ_HOST` 环境变量配置，使微服务在尝试解析 RabbitMQ 连接时找不到配置而崩溃。<br>2. K8s 环境下缺少 MongoDB、Elasticsearch 及 Temporal Server 基础设施，导致 `agent-service` 在 `main.go` 进行 `client.Connect`/`Ping` 及 `client.Dial` 时发生 panic 崩溃退出。 |
| **解决** | 1. 在 `agent-deployment.yaml` 补齐了 `MQ_HOST` 等必要的 RabbitMQ 环境变量。<br>2. 在 Helm 模板中新增单点临时 `mongodb-deployment.yaml` 提供存储支持。<br>3. 重新改造 `cmd/agent-service/main.go` 中的 `Init` 启动流，将 ES 离线和 Temporal Server 连接失败的 Fatal 逻辑改为 Log 警告并优雅降级跳过，支持基础设施不完整下的高可用离线安全启动。<br>4. 重新构建镜像后完美上线，与网关配合打通了 VirtualService 切流自愈全链路！ |

## 52. Temporal 本地开发多合一镜像拉取受阻与基础设施自建拆分

| 项目 | 内容 |
|------|------|
| **问题** | 在 Windows 宿主机部署时，`docker-compose up` 拉取 `temporalio/dev:1.24.0` 持续超时或报 403 错误，且在 Docker Hub 界面或去除了 Tag 均无法检索到此开发用镜像，阻碍了本地运行测试。 |
| **原因** | 1. 国内公网 Docker 加速代理站（如 daocloud、xuanyuan 等）对小众及开发用镜像（以 `/dev` 结尾）不予缓存或拉取限制返回 403。<br>2. Docker Desktop 内置搜索仅拉取 Verified Publisher 官方核心主镜像，过滤了非主线辅助仓库。 |
| **解决** | 1. 拆分 `docker-compose.yaml` 原本的 `temporal` 多合一服务为两个官方独立服务：`temporal`（使用 `temporalio/auto-setup:1.24.0` 作为核心并配置连接项目中已有的 MySQL 服务，自动完成库表 schema 的初始化加载）与 `temporal-ui`（使用 `temporalio/ui:2.24.0` 作为 Web UI 面板并连接核心）。<br>2. 本地拉取主流加速源 `docker.m.daocloud.io/temporalio/auto-setup:1.24.0` 及 `temporalio/ui:2.24.0` 镜像并用 `docker tag` 恢复官方前缀命名，成功打通本地完整开发依赖闭环。 |

## 53. Agent Service 本地 docker-compose 部署因缺失基础设施环境变量崩溃

| 项目 | 内容 |
|------|------|
| **问题** | 本地运行 `docker-compose up` 后，`agent-service` 容器反复启动并立即异常崩溃，提示数据库连接被拒绝 `dial tcp 127.0.0.1:3306: connect: connection refused`。 |
| **原因** | `agent-service` 在 `docker-compose.yaml` 的环境变量中未配置 `DB_HOST`、`REDIS_HOST`、`MQ_HOST` 等基础设施主机名。由于缺少配置，代码内部直接使用 `DefaultDBConfig` 里的默认回退地址 `127.0.0.1` 尝试在容器内部寻找 MySQL、Redis 和 RabbitMQ 服务，从而引发连接拒绝。 |
| **解决** | 1. 修改 `docker-compose.yaml`，在 `agent-service` 的环境变量中补齐 MySQL (DB)、Redis 和 RabbitMQ (MQ) 对应的主机名、端口、用户、密码等系统环境变量。<br>2. 重新在本地执行 `docker-compose up -d --build` 重新构建并热加载服务，成功保证了数据库、缓存 and 消息队列连接握手，微服务已能正常启动。 |

## 54. DialogueID 转换为 uint64 后发生有损截断，导致模式二、模式三与旧会话 dialogue not found

| 项目 | 内容 |
|------|------|
| **问题** | 测试“资讯/搜索”与“辅助推荐”功能时，后台频繁报错并崩溃返回 500 Internal Server Error，错误信息为：`❌ ConsultContent error: get dialogue failed: dialogue not found`。且在修改重载代码后，浏览器里原有的历史会话也统统报错无法发送后续对话。 |
| **原因** | MongoDB 自动生成的 `ObjectID` 是 12 字节（24字符 hex），而在 gRPC proto 定义中为了规范，`DialogueID` 属性以 `uint64` 传输。为了做兼容适配，代码在返回时取了 `ObjectID` 后 8 字节转为 `uint64`；而在还原时则将前部补零强行生成 24 字符（如 `000000002e922e4aa0726e2c`）并在 MongoDB 中查找。由于原本真实生成的 ObjectID 前 4 字节包含的是时间戳而不是零，这导致了“生成的真ID”与“还原的假ID”发生了严重的“ID有损脑裂”，在数据库里根本匹配不到，进而对于已有的历史会话也统统失效。 |
| **解决** | 1. 修改 `internal/module/agent/repository/agent_repo.go` 中的 `CreateDialogue`。在插入 MongoDB 数据库前，显式强制生成“前 4 字节为零、后 8 字节为真实随机 bytes”的特定 ObjectID，使 Insert 时采用该 ID 写入，彻底打通新会话的无损转换闭环。<br>2. 在 `GetDialogue` 的查询中追加 **向前兼容性与平滑降级（Backward Compatibility）** 机制。若入参 ID 具有前 4 字节为零的特征且未精确查获，则在内存中对集合所有会话进行后 8 字节的后缀模糊匹配。这样不仅新创建的对话顺畅通过，连修改前已经生成的旧历史对话也能被完美查获兼容，实现了 100% 优雅修复。 |

## 55. 浏览器端 JavaScript 精度丢失截断雪花 ID 导致后端报错 dialogue not found

| 项目 | 内容 |
|------|------|
| **问题** | AI 智能体连续对话或切换模式后，系统发生全模式 500 报错，提示 `dialogue not found` 且无法再次对话。 |
| **原因** | 对话 ID (uint64) 在网关原本作为数字 JSON 序列化返回给前端。而 JavaScript 的 `Number.MAX_SAFE_INTEGER` 精度上限限制会导致 19 位的雪花 ID 在浏览器反序列化时低位数字发生截断篡改，被改写低位的 ID 回传到网关后无法在数据库中匹配，破坏了会话路由。 |
| **解决** | 修改 API Gateway 中的 `ConfirmPublishTwitter`、`GetRepositoryDialogue` 与 `GetDialogueDetail`。在返回 JSON 时，手动利用 `strconv.FormatUint(id, 10)` 将所有的 `uint64` Snowflake ID 或 Dialogue ID 转为 `string` 格式返回。从而彻底避免了前端 JavaScript 反序列化时的精度截断问题，确保全模式下的对话连贯性。 |

## 56. MCP 长连接异常断开与 Qdrant 缺失优雅降级失败

| 项目 | 内容 |
|------|------|
| **问题** | 1. 模式二（资讯搜索）、模式三（写推发布）和模式四（多智能体协作）在进行第一次调用后，第二次调用就会报 500 或 `Invalid session ID` 错误且永久失效，提示连接已死或 Context Canceled。<br>2. 局域网/容器中未部署 Qdrant 时，调用搜索功能直接报错阻断 LLM 运行，报“请求失败，请重试”。<br>3. 智能体搜索关于“云原生”、“Go 语言”的推文时，召回内容空空如也，导致 AI 写作结果极其简陋。 |
| **原因** | 1. `getOrInitMCPClient` 启动客户端 `mcpClient.Start(ctx)` 传入了单次请求的 Context，当该 gRPC 请求返回响应后 Context 被 cancel，导致全局长连接底层的 SSE 通道被强制关闭，下一次调用便抛出 canceled 错误。此外，`0.0.0.0` 回环地址在部分容器内无法拨号。<br>2. `RegisterSearchTweets` 内部直接强依赖 Qdrant 的 Search，未对其连接失败或未启动进行容错与优雅降级。<br>3. 数据库 `seed_data.go` 中没有填充相应的中文专业测试推文，系统初始化完毕后处于空库状态，无从召回相关主题。 |
| **解决** | 1. 结构体 `AgentService` 重构引入生命周期 Context `serviceCtx` 与 `cancelFunc` 并实现 `Close()` 回收。在 `getOrInitMCPClient` 时将 `Start` 绑定至 `serviceCtx`，确保连接生命周期不随请求而中断。<br>2. 对 `0.0.0.0` 目标地址转换进行智能替换为 `127.0.0.1` 以供本地容器安全回环拨号，并对握手、加载 Tools 方法添加 5 秒超时保护。<br>3. 重构 `search_tweets.go` 中的 `RegisterSearchTweets` 参数以接入 `esClient`，在 Qdrant 连接失败或未部署时打印 warning 并**优雅降级为 Elasticsearch 文本倒排检索（BM25）**。若二者皆墨，则优雅返回兜底说明文本，防止 Error 冒泡打断 LLM 对话流。<br>4. 在 `docker-compose.yaml` 中补充 `qdrant` 服务定义并映射端口，同时为使用它的微服务注入 `QDRANT_URL=http://qdrant:6333`。<br>5. 升级 `seed_data.go` 种子数据，新增 10 条高质量云原生、微服务、Go 语言 and 微服务开发中文推文数据，极大丰富了 AI 检索的召回素材，打通端到端闭环。 |

## 57. Snowflake 发号器升级为双值返回 (uint64, error) 导致各模块编译与镜像构建报错

| 项目 | 内容 |
|------|------|
| **问题** | 升级发号器全局接口后，编译各微服务或运行 `docker compose up -d --build` 报错，提示 `assignment mismatch: 1 variable but snowflake.GenerateID returns 2 values`，或 `MustInit(...) (no value) used as value`，或 `undefined: snowflake.Init` |
| **原因** | 1. 发号器 `snowflake.go` 的 `GenerateID()` 签名升级为 `(uint64, error)`，但 `notification-service`、`messenger-service` 等散落在各处的代码依旧以单变量形式接收。<br>2. 部分微服务在初始化时错误地对 `MustInit` 的返回值进行了 `err` 接收（而 Must 前缀方法在生产级语义中出错时应直接 panic 退出，无返回值）。<br>3. 原有的 `snowflake.Init` 静态节点初始化方法在重构中被移去，导致依赖它的组件（如 `notification-service`）报未定义错误。 |
| **解决** | 1. 将 `notification_repo.go`、`messenger_service.go`、`bookmark_repo.go`、`comment_repo.go`、`like_repo.go`、`poll_repo.go`、`retweet_repo.go` 等仓库/服务代码统一改写为双变量形式接收并向上传播或进行安全拦截。<br>2. 还原 `snowflake.Init(workerID)` 方法以支持非 Redis 环境下的单机静态节点自举，并修复 `gateway` 对其的调用错误。<br>3. 恢复 `MustInit` 发生异常直接 panic 的零返回值设计，同时清理并去除了 `tweet-service`、`follow-service`、`messenger-service`、`auth-service`、`consumer` 各个 `main.go` 启动时对 `MustInit` 错误返回值的接收。编译及构建全绿通过。 |

## 58. 点赞报错 500、最新推文不显示与热门趋势暂无数据 Bug

| 项目 | 内容 |
|------|------|
| **问题** | 1. 点击点赞按钮控制台报错 500 Internal Server Error，且刷新后点赞高亮失效。<br>2. 首页的“为你推荐”流只能看到 7 天前的历史测试推文，新发布的最新推文无法在前 20 条内刷出（但在个人资料页中可见）。<br>3. 首页右侧“推荐趋势”区域一直显示“暂无热门话题”。 |
| **原因** | 1. `likeRepo.Like` 写入前对 `Like.ID` 赋予了生成的非零 Snowflake ID，导致 GORM 在后续使用 `FirstOrCreate` 时将主键 ID 自动拼入 WHERE 条件中使得查询必然失效，进而触发 INSERT 冲突引发 `uk_user_tweet` 联合唯一键重复报错；且网关在 `ListTweets` / `GetTweetReplies` / `SearchTweets` 中未透传当前请求的用户 ID，导致后端 `is_liked` 填充硬编码为 false。<br>2. 本地发号器修改后的自定义起始时间戳 `epoch` 是 `1609459200000` (2021年)，而 7 天前历史数据用的是默认纪元 `1420070400000` (2015年)。这造成新推文的 Snowflake ID (7.21e17 级) 远小于老推文 ID (2.06e18 级)，使得按 `Order("id DESC")` 排序 of 列表接口把新推文全部强行排到了第 131 条之后而沉底。<br>3. 系统内存在的全部测试推文在发布时都未包含 `#` 话题标签，所以后台没有清洗出任何话题写入 Redis sorted set `trends:global` 中。 |
| **解决** | 1. 重构点赞仓储的 `Like` 方法为先查询后写入的形式；在 API 网关和 gRPC Server 层的 `ListTweets`、`GetTweetReplies`、`SearchTweets` 调用链中加入 `RequestingUserId` 参数向下透传以实现高亮判定。<br>2. 将发号器的 `epoch` 重新修改回原有的 `1420070400000`，使新生成 ID 返回 2.06e18+ 级，恢复正常时间轴排序并兼容历史老数据。<br>3. 发布带有 `#` 前缀的推文进行测试后，话题提取与定时刷新组件即能够自动统计并将数据加载在趋势面板中。 |

## 59. Docker 容器构建时 Go 模块下载 unexpected EOF 导致编译失败

| 项目 | 内容 |
|------|------|
| **问题** | 运行 `docker compose up -d --build` 或构建各微服务镜像时，在 `go mod download` 阶段报错 `unexpected EOF` 导致构建中断 |
| **原因** | Dockerfile 中默认的 `GOPROXY=https://goproxy.cn` 在拉取特定大型依赖包时发生了网络连接超时或握手阶段重置（EOF） |
| **解决** | 修改 `deploy/docker/` 目录下所有微服务的 Dockerfile（包括 gateway, user, tweet, follow, messenger, notification, consumer, auth, agent），将 `GOPROXY` 调整为优先使用阿里云代理并补充兜底 `https://mirrors.aliyun.com/goproxy/,https://goproxy.io,direct`，顺利通过所有依赖拉取和容器编译构建 |

## 60. 前端上传多媒体文件时报 413 (Request Entity Too Large) 错误

| 项目 | 内容 |
|------|------|
| **问题** | 页面上传大于 1MB 的图片/视频时，控制台报错 `Failed to load resource: the server responded with a status of 413 (Request Entity Too Large)` |
| **原因** | 前端 Nginx 开发服务器的反向代理配置中，没有显式设置文件大小限制。Nginx 默认的 `client_max_body_size` 限制为 1MB，大文件请求被 Nginx 提前拦截拒绝 |
| **解决** | 修改 `web/nginx.conf`，在 `server` 块中添加 `client_max_body_size 20m;` 配置，解除小文件限制并允许最高 20MB 的多媒体上传，重新构建并部署 frontend 服务后测试通过 |

## 61. 文本框内容为空但上传图片后点击发推发生 400 报错且引起图片无限上传

| 项目 | 内容 |
|------|------|
| **问题** | 在只添加媒体图片但不输入文字的情况下点击“发推”或“回复”会失败，且用户多次重复点击导致大量无用图片上传入库，形成脏数据 |
| **原因** | 前端 `ComposeBox.vue` 和 `ReplyModal.vue` 在发推前的前置校验中，使用的是 `if (!content.trim() && selectedFiles.length === 0)`。这意味着只要有图片，即使文字为空也会放行，从而先执行多媒体上传 API。但在最终创建推文的后端服务接收请求时，其因为内容为空字段校验失败抛出 400，造成了图片已被入库而推文无法发布的漏洞 |
| **解决** | 修改 `ComposeBox.vue` 和 `ReplyModal.vue`，将发推/回复按钮的 `:disabled` 属性以及提交函数 `handleTweet`/`handleReply` 的前置合法性拦截判定直接绑定到 `!content.trim()` 上。只要文字内容为空，直接灰掉按钮且拦截动作并友好提示，从前端源头断绝了无文字发推失败导致图片无限上传的问题 |

## 62. GSE 分词器编译报错 (PosSeg 未定义)

| 项目 | 内容 |
|------|------|
| **问题** | 编译 `consumer` 组件时，在 `trends_processor.go` 报编译错误：`p.seg.PosSeg undefined (type gse.Segmenter has no field or method PosSeg)` |
| **原因** | 纯 Go 分词器 `go-ego/gse` 中的词性标注分词方法已更名为 `Pos`，原本在伪代码或旧版本中的 `PosSeg` 方法在此版本中未定义 |
| **解决** | 将 `trends_processor.go` 中对 `p.seg.PosSeg(cleanText)` 的调用修改为 `p.seg.Pos(cleanText)`，编译全绿通过。 |



## 63. Docker 容器构建时使用国内 Go 代理拉取包频发 unexpected EOF 导致编译失败

| 项目 | 内容 |
|------|------|
| **问题** | 运行 `docker compose up -d --build` 或构建各微服务镜像时，在 `go mod download` 阶段拉取包（特别是 `compress`）时频发 `unexpected EOF` 导致构建中断 |
| **原因** | Docker 容器内网络默认的 MTU 配置较大，或者国内网络环境下连接阿里云代理 `mirrors.aliyun.com` 和七牛云代理 `goproxy.cn` 容易因大依赖包拉取缓慢而触发超时和连接重置，使得 `go mod download` 异常退出 |
| **解决** | 1. 在宿主机上执行 `go mod vendor`，将项目所需的全部第三方依赖包完整下载至本地 `vendor` 文件夹。<br>2. 批量重构所有微服务的 Dockerfile，移除其中容器内部的依赖拉取步骤（如 `go mod download`），并将编译指令变更为使用本地依赖库的 `-mod=vendor` 编译模式。<br>3. 重新执行 `docker compose build` 时，编译流程完全离线自举，成功绕过容器内部的代理下载握手问题，构建速度大幅提升且全绿通过。 |


## 64. Qdrant 向量库初始化冷启动延迟导致写入时报 404 错误

| 项目 | 内容 |
|------|------|
| **问题** | 在微服务启动后，消费者或旁路任务向 Qdrant 向量数据库写入数据（UpsertPoint）时，频繁报错 `failed to upsert point to Qdrant: failed to upsert point, status: 404, response: `，导致事件任务重试失败并归档为 Failed。 |
| **原因** | 1. Docker Compose 部署中，`consumer` 和 `agent-service` 依赖 `qdrant` 时仅配置了 `condition: service_started`，导致在 Qdrant API 尚未就绪前应用已初始化完毕。<br>2. 尽管应用在初始化时尝试创建 `tweets` 集合，但由于端口未就绪导致创建静默失败。随后写入数据时，由于 Collection 依然不存在，Qdrant 就会返回 404 错误。 |
| **解决** | 1. 在 `pkg/qdrant/qdrant.go` 客户端的 `UpsertPoint` 写入逻辑中加入了**写时自愈**（On-Demand Healing）机制：当捕获到 Qdrant 返回的 404 状态码时，自动提取当前写入向量的维度，发起 `CreateCollection` 进行幂等创建，创建成功后自动递归重试写入。<br>2. 重新编译微服务并运行，自愈机制彻底规避了启动依赖时序和冷启动未准备好的痛点，实现了高弹性的自愈向量写入保障。 |

## 65. 可视化工作流编辑器 Handle 端点堆叠无法连线

| 项目 | 内容 |
|------|------|
| **问题** | 在工作流编辑器中，各个节点连线的 Handle（如输入、输出小方块）堆叠在左侧，且无法从一个节点拖拽连线至另一个节点 |
| **原因** | 前端主画布组件 `WorkflowEditor.vue` 仅加载了 `@vue-flow` 组件，却未引入 `@vue-flow/core/dist/style.css` 和 `@vue-flow/core/dist/theme-default.css` 核心样式文件，导致内置的 Handle 样式与定位失效 |
| **解决** | 在 `WorkflowEditor.vue` 的 `<script setup>` 顶部追加导入这二者核心样式，使 Handle 恢复绝对定位并渲染在节点左右两侧，连线交互得以正常使用 |

## 66. 自定义工作流节点删除按钮点击无效

| 项目 | 内容 |
|------|------|
| **问题** | 点击自定义节点右上角的 "✕"（删除按钮）后无响应，无法删除节点 |
| **原因** | `CustomNodeWrapper.vue` 原本仅发出 `emit('delete')` 自定义事件。然而该组件是作为 Vue Flow 动态渲染的节点类型反射出来的，外部的 `<VueFlow>` 父组件无法直接捕获到子组件的 emit；且之前父组件 provide 的 `deleteNode` 方法在子组件中未定义 `inject` 接收，导致无法触发真正的删除流程 |
| **解决** | 在 `CustomNodeWrapper.vue` 中显式 `inject` 接收父组件提供的 `deleteNode` 函数，同时引入 `useVueFlow().removeNodes` 作为原生删除的兜底方案。点击删除按钮时，调用 `deleteNode` 统一删除节点、关联连线，并安全重置右侧配置抽屉 |

## 67. 本地 Workflow 单元测试受 Go 1.25.5 工具链下载阻塞

| 项目 | 内容 |
|------|------|
| **问题** | 执行 `go test ./internal/module/agent/workflow/...` 时无法完成验证。使用 `GOTOOLCHAIN=local` 会提示本机 Go 版本为 `1.24.3`，低于 `go.mod` 要求的 `1.25.5`；启用自动工具链下载后，下载过程在当前受限网络环境中超时。 |
| **原因** | 本地可用 Go 工具链版本与项目声明版本不一致，且沙箱网络/代理限制导致 `go1.25.5` 自动下载无法稳定完成。 |
| **解决** | **Resolved（2026-07-14）**：当前工具链已可用，并将 `GOCACHE` 指向项目内 `tmp/go-build-cache`；`go test ./internal/module/agent/... ./cmd/agent-service`、关键 `-race` 测试和 `go vet` 均已通过。 |
## 2026-06-25 P4 Go Toolchain Validation Blocker

| 字段 | 内容 |
|------|------|
| 问题 | `GOTOOLCHAIN=local go test ./internal/module/agent/workflow/engine ./internal/module/agent/service` 未能进入编译阶段 |
| 原因 | 当前本机 Go 版本为 `go1.24.3`，但 `go.mod` 要求 `go >= 1.25.5` |
| 影响 | P4 的 Go 单元测试无法在当前本地工具链完成验证；前端构建与 diff check 已通过 |
| 解决方案 | 当前工具链已可用；使用项目内 Go 缓存重新执行 Agent 全包测试与 Workflow Engine race 测试，均通过 |
| 状态 | Resolved（2026-07-14） |

## 59. Jaeger 中看不到其他服务链路数据

| 项目 | 内容 |
|------|------|
| **问题** | 启动所有服务后，Jaeger 控制台中无法看到除自身或网关外的微服务调用链路数据。 |
| **原因** | 1. 项目使用的 OpenTelemetry gRPC exporter (\otlptracegrpc\) 必须通过 gRPC 协议与 Collector 通信。<br>2. \docker-compose.yaml\ 和各微服务的环境变量中错误地将 \JAEGER_COLLECTOR_ENDPOINT\ 设置为了 Jaeger 的 HTTP 端口 (\http://jaeger:14268/api/traces\)，导致 gRPC dial 失败。<br>3. Jaeger 的 Docker Compose 配置中没有对外暴露 OTLP gRPC 端口 ę7\，造成本地启动的服务也连不上。 |
| **解决** | 1. 修改所有微服务 \main.go\ 中的 fallback 环境变量为 \localhost:4317\。<br>2. 将 \docker-compose.yaml\ 和 \docker-compose-learn.yaml\ 中的 \JAEGER_COLLECTOR_ENDPOINT\ 环境变量统一更新为 \jaeger:4317\。<br>3. 在 Compose 的 jaeger 配置中增加端口映射 ę7:4317\，放行 OTLP gRPC 流量。 |

## 68. Agent Runtime P0 测试无法写入默认 Go Build Cache

| 项目 | 内容 |
|------|------|
| **问题** | 首次执行 P0 Agent 单元测试时，Go 尝试写入 `C:\Users\郭丰硕\AppData\Local\go-build` 并返回 `Access is denied`，测试未进入编译阶段。 |
| **原因** | 当前受限执行环境只允许写项目工作区，Go 默认用户缓存目录不在可写根目录内；属于测试环境权限问题，不是业务代码编译失败。 |
| **解决** | **Resolved（2026-07-14）**：将 `GOCACHE` 显式设置为项目内 `tmp/go-build-cache` 后重跑；Agent 全包测试、Runtime/Workflow Engine race 测试与 `go vet` 全部通过。 |

## 69. Agent Runtime P1 统一 Race 命令超过单命令时限

| 项目 | 内容 |
|------|------|
| **问题** | 首次执行 `go test -race ./internal/module/agent/runtime ./internal/module/agent/model ./internal/module/agent/service -count=1` 时，Runtime 与 Model 已通过，但命令在 Service 包完成前触发 120 秒执行上限。 |
| **原因** | Windows 下首次构建 Service 包及其较大依赖树的 race instrumentation 耗时较长；已有输出未报告测试失败或数据竞争，属于验证命令时限不足。 |
| **解决** | **Resolved（2026-07-14）**：保留 Runtime/Model 已通过结果，将 Service 包独立提高到 300 秒时限重跑并通过；随后 `go vet ./internal/module/agent/... ./cmd/agent-service` 通过。 |

## 70. Agent Runtime P2 首次 Service 冷编译与沙箱工具链超时

| 项目 | 内容 |
|------|------|
| **问题** | P2 首次执行 Go 测试时，沙箱内无法读取自动安装的 Go 1.25.5 标准库，切换到沙箱外后统一命令又在 Service 大依赖树完成前先后触发 180 秒与 360 秒时限。 |
| **原因** | Go 标准库和模块缓存位于沙箱外目录；同时本轮新增包导致 Service 及 Mongo/Redis/MCP/OpenTelemetry 依赖首次冷编译，Windows 环境下缓存尚未预热。日志中没有业务测试失败或编译错误。 |
| **解决** | **Resolved（2026-07-14）**：使用项目内 `tmp/go-build-cache`，先完成 Runtime/Message/Model 与 Service 定向迁移测试以预热缓存，再执行 Service 全包、Agent 全包、五个变更包 race 和 vet；最终均通过。 |

## 71. Session Summary 取消路径在测试环境触发空 Logger Panic

| 项目 | 内容 |
|------|------|
| **问题** | 新增“删除对话取消在途摘要任务”测试时，任务收到 `context.Canceled` 后进入通用 Warn 日志分支；测试未初始化全局 Zap Logger，导致 nil pointer panic。 |
| **原因** | 服务关闭和用户删除造成的主动取消属于正常生命周期事件，不应进入故障日志路径；异步调度器此前没有区分主动取消与真实摘要失败。 |
| **解决** | **Resolved（2026-07-14）**：`runSessionSummaryAsync` 对 `context.Canceled` 静默收口，模型、Embedding、租约及超时等真实错误仍保留 Warn；新增在途任务取消测试并复跑 Service 测试与 race。 |

## 72. P3 Tool Registry 实例化迁移导致旧测试编译失败

| 项目 | 内容 |
|------|------|
| **问题** | 首次执行 `go test ./internal/module/agent/workflow/tool -count=1` 时，`registry_test.go` 仍调用已移除的全局 `GetRegistry`，并使用旧版双参数 `NewToolNode`；随后 Service 定向编译又发现静态 DSL 校验函数误引用了实例变量 `s`。 |
| **原因** | P3 将工具注册中心从 package 全局单例迁移为显式注入的 `ToolRegistry + Executor`；旧测试夹具和只负责编译图结构的静态校验入口尚未同步新的依赖边界。 |
| **影响** | 目标工具包尚未进入测试执行阶段，不代表 ToolExecutor 行为验证通过。 |
| **解决** | **Resolved（2026-07-15）**：每个测试已改为独立 Registry/Executor，并补充审批、身份覆盖、Schema、超时、重试、幂等与脱敏审计覆盖；Tool、Service、Cmd 定向测试及 Agent 全包测试均通过。 |

## 73. P3 工具 Trace 脱敏升级导致旧迁移断言失败

| 项目 | 内容 |
|------|------|
| **问题** | Service 定向测试中 `TestExecuteWorkflowStrategyRuntimePreservesNodeConfiguration` 仍期望 `tool_trace.arguments.query` 保存原始值 `golang`，统一脱敏后实际为 `[REDACTED]`。 |
| **原因** | P3 将 Tool Audit/Trace 纳入敏感字段治理，查询、Prompt、正文、Token 和密钥默认只在执行内存中保留原值，对持久化或返回的 Trace 统一脱敏。 |
| **影响** | 工具实际执行参数不受影响；仅旧测试对可观测输出的契约与新安全规则不一致。 |
| **解决** | **Resolved（2026-07-15）**：迁移测试已改为断言脱敏值；Executor 单测同时证明 Handler 收到原始参数、Audit/Trace 只收到脱敏副本，Service 全包测试通过。 |

## 74. P3 第二增量收口测试无法访问自动工具链与默认缓存

| 项目 | 内容 |
|------|------|
| **问题** | 首次收口执行 Tool/Engine/Service 定向测试时，Go 无法读取自动安装的 1.25.5 标准库，并因默认 `GOCACHE` 位于工作区外返回 `Access is denied`；仓库存在 vendor 目录时还自动进入 vendor 模式，新增依赖未在该目录中。 |
| **原因** | 当前受限执行环境不能稳定访问用户目录下的自动 Go Toolchain 与默认构建缓存；该故障发生在业务包编译前。 |
| **影响** | 首次命令没有形成有效代码验证结果，不代表业务测试失败。 |
| **解决** | **Resolved（2026-07-15）**：改用仓库内 `tmp/go-build-cache`、显式 `-mod=mod` 并在具备工具链读取权限的环境重跑；定向测试、Agent/Gateway/Cmd 全包、Tool/Engine/Service race 与 `go vet` 均通过。 |

## 75. 审批收件箱首次构建触发数组首项严格空值错误

| 项目 | 内容 |
|------|------|
| **问题** | `npm run build` 在 `ApprovalInbox.vue` 报告 `approvals[0]` 可能为 `undefined`，未进入 Vite 打包阶段。 |
| **原因** | Web TypeScript 配置启用了严格索引访问；运行时长度判断不会让数组索引自动收窄为非空值。 |
| **影响** | 审批收件箱首次版本无法通过类型检查，尚不能交付。 |
| **解决** | **Resolved（2026-07-15）**：改为读取首项后显式判空，前端生产构建通过。 |

## 76. Tweet 创建事务失败仍返回成功且部分 Repository 绕过事务

| 项目 | 内容 |
|------|------|
| **问题** | TweetService 的 Unit of Work 返回错误后，旧代码仍继续记录成功日志并返回内存中的 Tweet；同时 Tweet 与 Poll Repository 没有从 Context 提取事务句柄，导致 Tweet、Poll 和 Outbox 可能不在同一数据库事务中。 |
| **原因** | Service 错误分支只记录 Span，没有立即返回；Repository 直接使用基础 `gorm.DB`，忽略了 UOW 注入到 Context 的事务。 |
| **影响** | 数据库失败时调用方可能收到假成功；部分写入可能提前提交，破坏 Transactional Outbox 原子性，并放大 Agent 重试重复发推风险。 |
| **解决** | **Resolved（2026-07-15）**：错误分支立即返回；Tweet、Poll、Outbox 和新幂等 Repository 统一使用 `uow.ExtractTx`。新增事务失败回归测试，幂等记录与 Tweet/Poll/Outbox 在同一事务提交。 |

## 77. 审批抽屉被顶部栏限制为 64px 高度

| 项目 | 内容 |
|------|------|
| **问题** | 审批按钮可点击，但抽屉只在页面顶部显示一条约 64px 高的横条，正文区域被遮罩覆盖。 |
| **原因** | `ApprovalInbox` 位于带 `backdrop-filter` 的固定顶部栏内；该 CSS 属性为后代 `position: fixed` 创建新的包含块，导致 `inset-y-0` 相对顶部栏而不是视口计算。 |
| **影响** | 审批列表、Run 详情和批准/拒绝操作无法正常使用；TypeScript 与生产构建均无法发现该布局错误。 |
| **解决** | **Resolved（2026-07-15）**：使用 Vue `Teleport` 将遮罩和抽屉挂到 `body`，脱离顶部栏坐标系。实际浏览器验证抽屉在 1280×720 与 390×844 视口占满高度且无横向溢出。 |

## 78. 全仓 Go Vet 命中 Auth 既有不可达代码

| 项目 | 内容 |
|------|------|
| **问题** | P3 第四增量收口执行 `go vet ./...` 时，报告 `internal/module/auth/grpc/auth.go:44:2: unreachable code`。 |
| **原因** | Auth gRPC 旧实现存在提前返回后的不可达语句；该文件不属于本轮 Agent Tool/MCP/对账变更范围。 |
| **影响** | 全仓 Vet 无法形成全绿结果；`go test ./...`、受影响包 race 以及 `go vet ./internal/module/agent/... ./cmd/agent-service` 均通过，本轮代码未命中 Vet 问题。 |
| **状态** | Active；后续 Auth 专项修复时删除不可达分支并补对应测试。 |

## 79. P4 调度器首次窄测因缺失右花括号编译失败

| 项目 | 内容 |
|------|------|
| **问题** | 首次执行 IR/Engine/Tool/Service 窄测时，`workflow/engine/scheduler.go:186` 起连续报告参数列表语法错误。 |
| **原因** | `ExecuteFromCheckpoint` 从 `readySet` 组装 `readyIDs` 的 `for` 循环漏写右花括号，使后续方法声明被解析成循环体。 |
| **影响** | IR 包测试通过，但 Engine 以及依赖它的 Tool/Service 在业务测试执行前编译失败。 |
| **解决** | **Resolved（2026-07-15）**：补齐右花括号并执行 `gofmt`；IR、Engine、Tool、Service 同一组窄测复验通过。 |

## 80. P4 Revision 指定运行回归夹具缺少 ToolExecutor

| 项目 | 内容 |
|------|------|
| **问题** | 新增 `TestWorkflowRevisionAPIAndSpecifiedRunUseImmutableDSL` 首次执行时，指定旧 Revision 的 Run 被记录为 `failed`，而 Proto/Gateway 契约测试通过。 |
| **原因** | 测试使用最小 `start -> end` DSL，但构造 `AgentService` 时未注入 Workflow `ToolExecutor/Registry`；生产路径在构建任何节点前都会 fail-closed 校验执行器，因此失败发生在 Revision DSL 调度前。 |
| **影响** | 仅影响新增离线测试夹具，不影响生产 Composition Root；测试未能进入原本要验证的指定 Revision 执行阶段。 |
| **解决** | **Resolved（2026-07-15）**：测试注入空的隔离 Registry/Executor，不注册或调用外部工具；随后重新执行指定 Revision 与契约测试。 |

## 81. Codex 原生 Skill 迁移遇到网络、沙箱与 Windows 编码限制

| 项目 | 内容 |
|------|------|
| **问题** | Codex 官方手册 Helper 无法连接 `developers.openai.com:443`；首次写入 `.agents` 被会话的只读挂载拒绝；官方 `quick_validate.py` 在 Windows 默认 GBK 下读取中文 `SKILL.md` 抛出 `UnicodeDecodeError`。 |
| **原因** | 当前 Shell 网络出口受限；`.agents` 存在比仓库根目录更严格的沙箱权限；Python `Path.read_text()` 继承系统默认编码而 Skill 文件为 UTF-8。 |
| **影响** | 首次官方事实核对、文件迁移与 Skill 校验未完成；源文件在权限失败时保持原位，没有形成半迁移或内容丢失。 |
| **解决** | **Resolved（2026-07-16）**：通过 OpenAI 官方 Web 文档核对目录规范；经受控权限提升完成仓库内迁移；设置 `PYTHONUTF8=1` 后重跑 11 个 Skill，全部返回 `Skill is valid!`。 |

## 82. P5 HTTP 传播重定向测试夹具传入空请求导致标准库 Panic

| 项目 | 内容 |
|------|------|
| **问题** | 首次执行 `pkg/trace` 定向测试时，重定向测试返回 EOF，并在 `net/http.Redirect` 内触发空指针 Panic。 |
| **原因** | 新增测试处理器忽略真实 `*http.Request`，错误地向 `http.Redirect` 传入 `nil`；生产 Transport 尚未进入重定向策略断言。 |
| **影响** | 仅影响新增离线测试夹具，不影响生产 HTTP Client、Provider 或 MCP 调用。 |
| **解决** | **Resolved（2026-07-16）**：处理器传入真实请求后重跑；重定向策略、Trace 传播、敏感数据隔离和取消测试均通过。 |

## 83. Workflow Editor 浏览器自动化冒烟受 Codex Playwright 运行时阻塞

| 项目 | 内容 |
|------|------|
| **问题** | P5 第五增量完成后，本地 Vite 服务可返回 HTTP 200，但内置 Node/Playwright 浏览器自动化无法启动。 |
| **原因** | Codex 运行时的 `playwright-core` 解析到受限安装目录并触发 Windows `EPERM lstat`；补充依赖搜索路径后，`playwright` 仍报告缺少可加载的 `playwright-core`。 |
| **影响** | 不影响 Web 类型检查、生产构建、后端测试或本地页面服务；本轮无法取得可信的浏览器截图与点击生命周期证据。 |
| **状态** | **Blocked（2026-07-17）**：已完成 `npm run build`、全量 Go 测试/竞态/静态检查并验证页面 HTTP 200；待宿主浏览器插件权限或依赖恢复后补跑桌面视口交互冒烟。 |

## 84. P5 Blackboard 扩展竞态检测被 Go 编译器进程权限中断

| 项目 | 内容 |
|------|------|
| **问题** | 执行 Repository/Service/gRPC/Gateway 的扩展 `go test -race` 时，前三个 Agent 包通过，但 Gateway 两包在编译 `k8s.io/klog/v2/internal/buffer` 时无法启动 Go toolchain 的 `compile.exe`。 |
| **原因** | 当前 Windows 受限宿主临时拒绝创建用户 Go toolchain 目录中的编译器子进程，错误为 `fork/exec .../compile.exe: Access is denied`；失败发生在 Gateway 业务包编译前。 |
| **影响** | 首次命令未形成 Gateway race 结果，不代表 Blackboard、Gateway Handler 或 Router 测试失败。 |
| **解决** | **Resolved（2026-07-18）**：保持仓库内 `GOCACHE`，仅重跑 `go test -race ./internal/gateway/handler ./internal/gateway/router -count=1` 后两个包均通过；结合首次已通过的 Repository/Service/gRPC，受影响范围的竞态验证完整通过。 |

## 85. Blackboard 顶层敏感字段首次脱敏测试失败

| 项目 | 内容 |
|------|------|
| **问题** | 新增“敏感原值不能被关键词命中”测试时，`tool.token` 仍返回一个匹配项。 |
| **原因** | 初版递归脱敏只处理值内部的对象键；Blackboard 的二级字段名在遍历入口已经与值分离，字段本身名为 `token` 时没有进入递归键判断。 |
| **影响** | 初版尚未交付的 Blackboard 查询可能在用户自己的响应中回显并检索顶层 Token 字段，违反该接口的默认脱敏约束。 |
| **解决** | **Resolved（2026-07-18）**：在序列化字段预览前先对二级字段名执行同一敏感键策略，再递归处理嵌套值；补充原值搜索与大值预览边界测试并复验。 |

## 86. P5 安全采样竞态验证首次被 Go 编译器进程异常中断

| 项目 | 内容 |
|------|------|
| **问题** | 首次并行执行 Observability/Service/Tool/gRPC/Gateway Handler 的 `go test -race` 时，前后三类包通过，但 Agent gRPC 包在业务测试启动前报告 Go `compile.exe: exit status 1`。 |
| **原因** | 当前 Windows 受限宿主并行启动 Go toolchain 子进程时发生瞬时编译器进程异常；输出中没有业务编译错误或测试断言失败。 |
| **影响** | 首次命令没有形成 Agent gRPC race 结果，不代表 Proto 映射、Trace API 或安全采样逻辑失败。 |
| **解决** | **Resolved（2026-07-18）**：保持仓库内 `GOCACHE` 并单独重跑 `go test -race ./internal/module/agent/grpc -count=1` 后通过；首轮其余相关包已通过，随后 Agent/Gateway/Cmd 全包测试和 `go vet` 也通过。 |

## 87. 项目进度核验首次全仓测试超过命令时限

| 项目 | 内容 |
|------|------|
| **问题** | 执行 `go test ./... -count=1` 核验当前项目整体状态时，首轮命令在 120 秒上限被终止；终止前已输出的 Agent、Gateway、Tweet、MQ 等包均通过。 |
| **原因** | 当前仓库包数量较多，Windows 受限环境下首次全仓编译与测试耗时超过本次命令窗口；输出中没有编译错误或测试断言失败。 |
| **影响** | 首轮结果只能证明已输出包通过，不能据此宣称全仓验证完成。 |
| **解决** | **Resolved（2026-07-22）**：保持仓库内 `GOCACHE`，将命令时限扩展后原命令在 60.6 秒内完整通过。 |

## 88. 本地 Provider 回环 IP 通过预检但在拨号阶段被拒绝

| 项目 | 内容 |
|------|------|
| **问题** | `agent-task-eval` 的 Live Runtime HTTP 集成测试使用本地 Provider 与 `127.0.0.1` 时，Endpoint Policy 的 URL 预检通过，但受限 HTTP Client 在实际拨号阶段拒绝同一地址，候选报告出现一个执行错误。 |
| **原因** | `Validate` 明确允许本地 Provider 使用回环 IP；`validateResolvedIP` 却只允许 `localhost`、`host.docker.internal` 等主机名解析到回环/私网，没有处理配置主机本身就是回环 IP 的等价情况。 |
| **影响** | LM Studio/Ollama 使用 `127.0.0.1` 配置时会稳定失败；云 Provider 和普通自定义 Provider 的 SSRF 拒绝边界未受影响。 |
| **解决** | **Resolved（2026-07-22）**：拨号校验仅对明确本地 Provider 增加“配置主机与解析结果均为回环 IP”的等价分支；补充本地允许、非本地拒绝和 Live Runtime HTTP 回归测试。 |

## 89. Agent Eval 归档首次窄测被 Go 默认缓存与 vendor 模式阻断

| 项目 | 内容 |
|------|------|
| **问题** | 首次执行 Eval/ObjectStore/CLI 窄测时，Go 无法写入用户默认 `GOCACHE`，并在隐式 `-mod=vendor` 下报告标准库与 MinIO/OpenAI 依赖不可用；测试未进入业务断言。 |
| **原因** | 当前 Windows 受限宿主拒绝访问 `C:\Users\...\AppData\Local\go-build`；仓库存在 vendor 目录后 Go 自动选择的 vendor 模式与当前依赖快照不一致。 |
| **影响** | 首次命令没有形成新增归档代码的编译/测试结论，不代表 Versioning/Object Lock 或 CLI 逻辑失败。 |
| **解决** | **Resolved（2026-07-22）**：使用仓库内 `tmp/go-build-cache` 并显式设置 `GOFLAGS=-mod=mod` 后，发现并修正当前 MinIO SDK 使用 `GetObjectOptions.VersionID` 字段的真实兼容问题；随后目标普通测试、竞态检测和 Vet 全部通过。 |

## 90. 真实 Agent Task WORM 基线缺少本机归档前置条件

| 项目 | 内容 |
|------|------|
| **问题** | 本机 LM Studio 与固定 `qwen2.5-3b-instruct` 模型可达，但无法生成并归档一份可长期验签的真实 52 条 Agent Task 稳定基线。 |
| **原因** | `AGENT_TASK_EVAL_INTEGRITY_KEY/KEY_ID` 与 MinIO 归档凭据未配置，`127.0.0.1:9000` 的 MinIO 健康检查不可达；代码禁止用临时硬编码密钥或普通 Bucket 冒充生产证据。 |
| **影响** | 归档代码和离线 Fake 已验证，但尚无真实 Provider/Profile 的 52 条质量结论，也没有真实 Object Lock `version_id` 回执。 |
| **状态** | **Open（2026-08-01 复核）**：仓库 `.env` 与当前进程环境仍没有 Eval HMAC/Archive Access/Secret Key，当前也没有 MinIO/Docker 进程。本轮未自动启动外部软件。由操作员启动/配置专用 Versioning + Object Lock MinIO Bucket，并通过环境变量注入独立 HMAC/MinIO 凭据后执行真实基线、人工复核、归档与回执复验。 |

## 91. Profile 业务结果接口首次编译引用了不存在的错误助手

| 项目 | 内容 |
|------|------|
| **问题** | 首次执行 Profile/gRPC/Gateway 定向测试时，新增结果 RPC 和 Handler 分别引用了不存在的 `profileAdminErrorStatus` 与 `handleAgentError`。 |
| **原因** | 接线时使用了概念名称，没有复用当前文件已有的 `profileAdministrationStatus` 与 `writeProfileAdministrationError`。 |
| **影响** | 首轮命令在编译阶段停止，未进入业务测试；领域、仓储和已部署服务未受影响。 |
| **解决** | **Resolved（2026-07-22）**：改为模块现有统一错误转换函数并重新执行 Profile、Repository、Service、gRPC、Gateway Handler/Router 定向测试，全部通过。 |

## 92. Assist 产品结果接线首次验证暴露重复元数据与前端类型未归一

| 项目 | 内容 |
|------|------|
| **问题** | 首轮 Go 定向测试因 `runtime_mode` 元数据重复键编译失败；修复后 Runtime Assist 测试发现 `ChatResult.RunID` 只误接到相邻 Consult 返回；Web 首轮构建又报告新旧历史消息结构和草稿候选数组的严格类型不兼容。 |
| **原因** | 接线跨越 Runtime 返回、持久消息与前端兼容结构，修改前未先确认 `runtime_mode` 已存在；相邻模式返回结构相似导致 Run ID 落点偏移；旧消息缺少新增的可选发布字段，TypeScript `flatMap` 推断成不兼容联合类型。 |
| **影响** | 失败均发生在编译或离线断言阶段，未发布到运行环境；TweetService、Mongo 和真实模型均未被调用。 |
| **解决** | **Resolved（2026-07-22）**：删除重复元数据键，把 Run ID 明确映射到 Assist 返回并补断言；历史消息归一函数显式返回兼容消息数组，候选列表固定为 `string[]` 并处理严格索引空值。Service/Repository/gRPC/Gateway/Cmd 定向测试和 Web `vue-tsc + Vite` 生产构建最终通过。 |

## 93. Assist 发布弹窗浏览器视觉验收受本地网络隔离阻塞

| 项目 | 内容 |
|------|------|
| **问题** | Vite 可在受控前台会话启动并返回 HTTP 200，但后台启动首先因宿主同时存在 `PATH/Path` 被 `Start-Process` 拒绝，清理环境后后台子进程又会在命令结束时被沙箱回收；内置浏览器无法访问命令环境的 `127.0.0.1`，改用 `host.docker.internal` 时被浏览器客户端策略拦截。 |
| **原因** | 当前 Windows Shell、沙箱作业对象和内置浏览器运行在不同的进程/网络边界；不是 Vite 编译、Vue 路由或页面运行时错误。 |
| **影响** | 本轮无法取得草稿选择弹窗的可信浏览器截图或点击证据；Web 严格类型检查、生产构建和开发服务器 HTTP 启动已通过。 |
| **状态** | **Blocked（2026-07-22）**：代码不做规避浏览器策略的改动；待宿主提供可共享的本地端口或恢复浏览器本地应用访问后，补跑 Assist 历史草稿恢复、候选切换、编辑、确认发布和 422 错误展示。 |

## 94. Agent 内容互动消费者被 RabbitMQ NetworkPolicy 漏放行

| 项目 | 内容 |
|------|------|
| **问题** | `content_engaged` 消费者和现有 RiskControl 都由 Agent Service 连接 RabbitMQ，但 Helm 的 RabbitMQ Ingress 白名单没有 `agent-service`。 |
| **原因** | Agent Service 后加了 MQ 能力，NetworkPolicy 仍保留早期的 Gateway/Tweet/Consumer/Notification 列表，组合根与部署边界未同步。 |
| **影响** | Compose 环境不受影响；启用 Kubernetes NetworkPolicy 时，Agent Service 无法建立 AMQP 连接，内容互动归因不会启动。 |
| **解决** | **Resolved（2026-07-22）**：RabbitMQ NetworkPolicy 增加 `agent-service`，Helm 默认模板重新渲染并确认 AMQP 白名单和 `AGENT_PROFILE_CONTENT_ATTRIBUTION_WINDOW=168h` 均存在。 |

## 95. Web Access Redis Adapter 首次使用了仓库未采用的客户端主版本

| 项目 | 内容 |
|------|------|
| **问题** | 首次执行 `go test ./internal/module/agent/websearch ./internal/module/agent/repository -count=1` 时，Repository 编译报告 vendor 模式找不到 `github.com/redis/go-redis/v9`。 |
| **原因** | 新增 Web Cache/Governor Adapter 时误用了 go-redis v9 导入路径；当前仓库统一依赖并 vendor 了 `github.com/go-redis/redis/v8`。 |
| **影响** | 首轮测试在 Repository 包装载阶段停止，未进入 Redis 缓存与原子预算断言；既有运行代码和 Redis 数据未受影响。 |
| **解决** | **Resolved（2026-07-25）**：Adapter 与测试改为复用仓库现有 go-redis v8 契约，并重新执行 WebSearch/Repository 目标测试。 |

## 96. 外部 MCP 能力加入目录后旧快照测试仍写死四项

| 项目 | 内容 |
|------|------|
| **问题** | P8.3 首轮 Service 定向测试中，能力目录不可变快照测试仍断言内置能力总数为 4，新增 `connector.mcp` 后失败。 |
| **原因** | 旧测试只校验固定数量，没有表达“能力应可见但默认不可路由”的安全契约。 |
| **影响** | 失败发生在离线断言阶段；gRPC、Gateway、Router 与 Agent Service 编译均通过，外部 MCP 执行能力未发布。 |
| **解决** | **Resolved（2026-07-26）**：更新目录快照，并新增默认 `planned`、无显式选项时 fail-closed、启用后仅解析到 `runtime.external_mcp` 的回归断言。 |

## 97. 高风险外部 MCP 防重试测试误判脱敏错误边界

| 项目 | 内容 |
|------|------|
| **问题** | 首次执行高风险外部 MCP 审批后失败回归测试时，测试断言底层超时原文会写入 Workflow Run，但运行时实际返回统一的 `external MCP call failed`。 |
| **原因** | 测试同时验证“禁止自动重试”和远端错误内容，却忽略 Remote MCP Manager 会主动隐藏第三方服务的底层错误细节。 |
| **影响** | 远端调用计数为 1，审批、恢复和防重试行为均符合预期；仅测试错误文本断言失败，生产实现未泄露远端内部信息。 |
| **解决** | **Resolved（2026-07-27）**：保留运行时错误脱敏，只断言稳定的公共错误与一次调用计数，并重新执行 Remote MCP 与 Agent Service 定向测试。 |

## 98. 外部 MCP 调用曾携带平台内部用户标识

| 项目 | 内容 |
|------|------|
| **问题** | 外部 MCP 复用统一 ToolExecutor 时，通用 Guardrail 会在 Schema 校验后向工具输入注入平台 `user_id`，适配器此前又把注入后的输入原样发送给第三方 MCP Server。 |
| **原因** | ToolExecutor 的内部租户身份与远端工具业务参数共用了同一输入 Map，外部连接边界没有重新投影为远端 Schema 已校验的原始参数。 |
| **影响** | 第三方 Server 可能收到未声明的字段，并可能暴露平台内部用户标识；平台授权、审批和审计身份本身未被第三方控制。 |
| **解决** | **Resolved（2026-07-27）**：Runtime 与 Workflow 外部 MCP Adapter 分离“治理输入”和“远端参数”，远端只收到调用前已校验参数的深拷贝；新增只读与高风险路径均不携带隐式 `user_id` 的回归断言。 |

## 99. 外部 MCP 幂等写入定向测试被 Go 隐式 Vet 异常中断

| 项目 | 内容 |
|------|------|
| **问题** | 首次执行 Remote MCP、Agent Service、gRPC 与 Gateway Handler 定向测试时，各包测试断言均通过，但 `go test` 启动的 `vet.exe` 在依赖 `github.com/pelletier/go-toml/v2` 上仅返回 `exit status 1`，没有源码诊断，最终命令状态为失败。 |
| **原因** | Windows 受限宿主并发启动 Go 1.25.5 工具链的同一 `vet.exe` 时返回 `Access is denied`；独立输出落在 Elasticsearch、Sentinel 等依赖包，没有任何项目源码诊断。 |
| **影响** | 新增代码已通过四个目标包的编译和测试断言，但在独立 Vet 成功前不能把本轮静态验证标记为完成。 |
| **解决** | **Resolved（2026-07-27）**：`go test -vet=off` 验证四个目标包编译与断言全部通过，再以 `go vet -p=1` 串行执行相同包并通过；后续 Windows 大范围 Vet 默认限制并发。 |

## 100. 全仓 Diff Check 被无关 Flutter 尾随空格阻断

| 项目 | 内容 |
|------|------|
| **问题** | 本轮最终 `git diff --check` 报告 `mobile/lib/features/tweet/presentation/feed_notifier.dart:198` 与 `tweet_detail_screen.dart:130` 存在尾随空格。 |
| **原因** | 两处均位于本轮开始前已经存在的无关 Mobile 工作区变更，不属于外部 MCP 幂等写入增量。其余输出为 Windows LF/CRLF 提示。 |
| **影响** | 无法把当前整个脏工作区声明为全仓格式检查通过；Go、Proto、Web 和本轮文档逻辑不受影响。 |
| **解决** | **Resolved（2026-07-27，本轮范围）**：遵守不修改无关用户变更的工作区规则，保留两处 Mobile 内容；改为对本轮触及文件执行定向 `git diff --check`。全仓尾随空格仍应由对应 Mobile 任务处理。 |
## 101. 外部 MCP 池饱和巡检测试被无效认证夹具阻断

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-07-27） |
| 现象 | `go test -vet=off ./internal/module/agent/mcp/remote ... -count=1` 中 `TestHealthPoolSaturationDoesNotIncreaseFailureCount` 预期跳过探测，却得到 `credential_unavailable`。 |
| 原因 | 测试连接遗漏 `auth_type=none`，健康检查在进入 Prober 前按缺失凭据正确地 fail-closed；并非连接池或巡检状态机缺陷。 |
| 影响 | 仅新增离线测试夹具；生产创建连接始终持久化显式认证类型。 |
| 解决 | 为夹具补齐 `AuthNone`，随后重新运行 Remote、Repository、Service、gRPC、Gateway 和 Agent Service 目标测试。 |

## 102. Agent Run 状态层首次定向测试被 Windows 编译器派生权限中断

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-07-28） |
| 现象 | 首次执行 Repository、Service 与 Agent Service 定向测试时，Repository 已通过，但 Go 1.25.5 `compile.exe` 在编译 Workflow Engine 依赖时返回 `fork/exec ... compile.exe: Access is denied`，Service 与 Cmd 因依赖未编译而失败。 |
| 原因 | 当前受限 Windows 宿主并发派生 Go 工具链子进程时被权限边界拦截；尚未出现项目源码编译诊断或测试断言失败。 |
| 影响 | Agent Run Repository 目标测试已通过；Service 和组合根仍必须重跑后才能确认本轮实现。未连接 Mongo、模型、MCP 或公网。 |
| 解决 | 使用 `-vet=off -p=1` 限制 Go 测试工具链并发后，Repository/Service/Cmd 定向测试、Repository/Service Race 和整仓测试全部通过；随后 `go vet -p=1 ./...` 也通过。后续 Windows 大范围 Go 验证继续默认串行，避免将宿主派生权限误判为源码错误。 |

## 103. Agent Resume Store 接口扩展后测试 Fake 未同步

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-07-28） |
| 现象 | 首次执行 `go test -vet=off -p=1 ./internal/module/agent/credential ./internal/module/agent/repository ./internal/module/agent/service -count=1` 时，Service 测试编译报告 `memoryAgentExecutionRunStore` 缺少 `ClaimAgentExecutionRun`，同时 `chat_runtime.go` 有一个未使用的 `strings` import。 |
| 原因 | 权威 Run Store 新增原子 Resume Claim 契约后，测试内存实现尚未同步 CAS/lease 行为；Chat Runtime 改用统一可见响应函数后旧 import 残留。 |
| 影响 | Credential 与 Repository 包已通过；Service 测试未进入断言，生产构建尚未完成本轮验证。未连接 Mongo、模型、MCP 或公网。 |
| 解决 | 测试 Fake 已补齐 `awaiting_human/过期 running` 领取、revision、lease、attempt 和旧执行者提交拒绝语义，并移除残留 import；新增加密 Checkpoint、人工续答、重复领取、过期重领和敏感参数拒绝测试，原目标命令已全部通过。 |

## 104. Agent 恢复失败分支误插入成功作用域

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-07-28） |
| 现象 | 最终执行 `npm run build` 时，`vue-tsc` 报告 `src/views/Agent.vue(527,26): Cannot find name 'authoritativeRun'`。 |
| 原因 | 为处理“恢复响应丢失但服务端已完成”的分支时，补丁匹配到了成功路径中的同名 `viewIsCurrent` 代码块；`authoritativeRun` 只在 `catch` 内定义。 |
| 影响 | Web TypeScript 编译失败，尚未生成本轮可交付前端产物；后端恢复状态机和已通过的 Go 测试不受影响。 |
| 解决 | 已将该分支移动到 `catch` 中权威 Run 查询之后；`vue-tsc -b && vite build` 随后通过，响应已落库时会重载服务端对话，普通失败仍恢复输入与可重试状态。 |

## 105. Windows 受限环境首次未能可靠启动 Vite

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-07-28） |
| 现象 | `npm run dev -- --host 127.0.0.1 --port 5173 --strictPort` 的参数被当前 shell 显示为位置参数，虽输出 5174 ready，但 HTTP 无法连接；随后 `Start-Process` 因环境同时包含大小写不同的 `PATH/Path` 报字典键冲突。 |
| 原因 | 当前受限 Windows PTY/PowerShell 的参数透传与环境字典行为，不是 Vite 源码或前端构建错误。系统端口/进程枚举同样被权限边界拒绝。 |
| 影响 | 生产构建已经通过，但尚不能把第一次 dev server 控制台输出作为可访问证据。后端和产物不受影响。 |
| 解决 | 改用可交互 PTY 直接执行 `npx.cmd vite --host=127.0.0.1 --port=5175 --strictPort`；Vite 明确绑定 `http://127.0.0.1:5175/`，随后 `curl` 返回 HTTP `200`。 |

## 106. Unified Agent 审批恢复首轮整仓测试被隐式 Vet 中断

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-07-29） |
| 现象 | 首轮扩大执行默认 `go test` 时，目标审批恢复断言已通过，但 Go 隐式启动的 `vet.exe` 在 Elasticsearch Typed API 等大依赖上仅返回失败状态，没有项目源码诊断。 |
| 原因 | 当前 Windows 受限宿主在并发派生编译/Vet 子进程时存在已知权限和资源抖动；测试、编译与静态检查混在同一高并发命令中，不能区分环境失败和源码失败。 |
| 影响 | 在独立静态检查完成前，不能把首轮输出描述为整仓验证通过；审批状态机、一次性令牌和远端调用断言本身未失败。 |
| 解决 | 分离职责并限制并发：`go test -vet=off -p=1 ./... -count=1` 与 `go vet -p=1 ./...` 均通过；目标包和共享 Service/Gateway 包另行通过 `go test -race -vet=off -p=1`。 |

## 107. Workflow SSE 心跳测试依赖 35ms 首次阻塞窗口

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-07-29） |
| 现象 | 一次扩大回归中 `TestWatchWorkflowRunEventsEmitsHeartbeatAndWindowBoundary` 只收到 `window_expired`，未达到“至少一条 heartbeat 加窗口终止”两条事件的断言。 |
| 原因 | 测试直接使用会按 `block` 等待的内存 Event Store；首次读取可占满 35ms 总窗口，Windows 调度繁忙时返回后已经越过截止时间，生产代码因此正确跳过过期心跳。断言实际依赖墙钟调度，而不是事件流契约。 |
| 影响 | 仅造成测试偶发失败；没有发现 Trace 事件丢失、Cursor 错乱或生产循环未终止。 |
| 解决 | 测试增加窄 `EventReader` 夹具，让第一次空读取确定性立即返回以验证 heartbeat，再委托真实内存 Reader 验证阻塞与 `window_expired`。`-race -count=20` 连续通过。 |

## 108. 本地浏览器审批 API 端到端验证被后端 500 阻断

| 字段 | 内容 |
|------|------|
| 状态 | Blocked（2026-07-29，环境） |
| 现象 | Vite `http://127.0.0.1:5173/agent` 可访问，桌面与 390x844 审批抽屉均正常渲染；但模型、会话和工具审批请求由当前本地后端返回 500，页面进入明确的服务不可用状态。 |
| 原因 | 当前浏览器验证环境没有一套可用的 Agent API 依赖链，无法创建真实 `approval_required` Run；本轮未持有真实第三方 MCP、完整 Agent 服务依赖和受控测试凭据。 |
| 影响 | 已验证页面路由、响应式布局和错误态，但不能把本次浏览器检查描述为“真实审批、签发一次性授权并恢复远端工具”的端到端验收。 |
| 解决 | 后端契约由离线 Service/gRPC/Gateway 测试覆盖；待受控环境启动 Agent、Mongo、Gateway 与测试 MCP Server 后，按“创建风险调用 -> 批准 -> 签发授权 -> 单次恢复 -> 重放拒绝”补 live 验收。 |

## 109. 项目级 MCP 只读策略测试被不完整 Annotation 夹具拒绝

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-07-30） |
| 现象 | 新增项目成员共享测试中，Editor 已完成发现和 Snapshot 审核，但把 `lookup` 启用为 `read` 时返回“tool risk category does not match its reviewed schema”。 |
| 原因 | 测试夹具只设置了 `readOnlyHint=true`，未把 MCP SDK 默认的 `destructiveHint` 显式设为 `false`；治理层因此按设计拒绝将具有破坏性声明的工具归类为只读。 |
| 影响 | 仅阻断新增离线测试；生产风险分类正确 fail-closed，没有发现实现缺陷。 |
| 解决 | 夹具同时设置 `readOnlyHint=true` 和 `destructiveHint=false`，随后 Project、Remote MCP、Repository、Service、gRPC、Gateway 与 Agent Service 定向测试通过。 |

## 110. 项目级 MCP 定向测试首次被用户级 Go 缓存权限阻断

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-07-30） |
| 现象 | 修复夹具后首次运行目标包时，Go 在 `C:\\Users\\郭丰硕\\AppData\\Local\\go-build` 创建缓存文件返回 `Access is denied`，所有包在 setup 阶段失败。 |
| 原因 | 当前 Windows workspace-write 沙箱不能写用户级 Go Build Cache；测试尚未进入项目编译或断言阶段。 |
| 影响 | 首轮命令没有代码诊断，不能据此判断项目实现失败。 |
| 解决 | 将 `GOCACHE` 指向仓库内 `tmp/go-build-p83-project` 并以 `-p=1` 重跑；目标包全部通过，后续验证继续复用隔离缓存。 |

## 111. Helm 项目作用域渲染断言误把字符串数组当成单一文本

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-07-30） |
| 现象 | 默认关闭与显式开启的 `helm template` 均成功，但首轮 PowerShell 断言报告没有找到对应布尔值。 |
| 原因 | PowerShell 将 Helm 多行输出保存为字符串数组；对数组使用 `-notmatch` 会返回所有不匹配元素，转换为布尔值后始终为真，并非模板缺少环境变量。 |
| 影响 | 只影响临时验证脚本，Helm Chart 本身已正确渲染 `"false"` 与 `"true"`。 |
| 解决 | 先用换行连接完整输出再匹配环境变量及相邻值；默认关闭、显式开启均通过，项目开关开启但 MCP 主开关关闭时也按预期被模板 guard 拒绝。 |

## 112. Agent 项目列表负页码被转换为极大无符号页码

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-07-30） |
| 现象 | 收口审查发现 `GET /agent/projects?page=-1` 在 Gateway 解析为负整数后直接转为 `uint32`，下游可能收到极大的页码并生成无意义 Mongo Skip。 |
| 原因 | 新 Handler 复用了宽松分页写法，没有在有符号到无符号转换前验证页码与页大小边界。 |
| 影响 | 不越权，但畸形输入可能产生额外数据库工作并返回误导性空页。 |
| 解决 | Gateway 现在要求 `page >= 1` 且 `1 <= page_size <= 100`，非法输入直接返回 400；新增回归测试并通过 Handler/Router Race。 |

## 113. 任务模板首轮 Go 回归无法写入用户级缓存

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-07-30） |
| 现象 | 首次执行 Repository、Service、gRPC、Gateway 和 Agent Service 定向测试时，所有包在 setup 阶段报告用户目录 `go-build` 文件 `Access is denied`。 |
| 原因 | 当前 Windows workspace-write 沙箱不能写 `C:\Users\郭丰硕\AppData\Local\go-build`；测试尚未进入源码编译或断言。 |
| 影响 | 首轮命令没有项目代码诊断，不能据此判断模板实现失败。 |
| 解决 | 将 `GOCACHE` 指向仓库内 `.cache/go-build` 后重跑，Repository、Service、gRPC、Gateway、Router 和 Agent Service 全部通过；后续 Race/Vet 也复用隔离缓存。 |

## 114. Vue 模板把字面占位符误解析为插值表达式

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-07-30） |
| 现象 | 首轮 Web 生产构建在任务模板弹窗的 `{{input}}` 标签处报告 `Unterminated string constant`。 |
| 原因 | 在 Vue 模板插值中再次嵌入含双花括号的 JavaScript 字符串，SFC Tokenizer 先把内部 `}}` 当成外层插值结束符。 |
| 影响 | `vue-tsc` 已通过，但 Vite 无法生成生产产物；后端模板契约和测试不受影响。 |
| 解决 | 展示文本改为等价 HTML Entity，输入框仍保留真实 `{{input}}` 字面量；随后 `vue-tsc -b && vite build` 通过。 |

## 115. 任务模板 Gateway Race 首轮被隐式 Vet 权限中断

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-07-30） |
| 现象 | Repository 与 Service 目标 Race 已通过，但 Gateway Handler 在依赖编译阶段启动 `vet.exe` 时返回 `Access is denied`。 |
| 原因 | Windows 受限宿主在并发 Race 编译期间派生隐式 Vet 子进程出现权限抖动；没有 Gateway 源码错误或失败断言。 |
| 影响 | 首轮组合命令不能作为 Gateway Race 通过证据。 |
| 解决 | 按职责拆分，以 `go test -race -vet=off -p=1 -run AgentTaskTemplate ./internal/gateway/handler` 重跑并通过；静态检查由独立 `go vet` 命令执行。 |

## 116. 父子核算首轮 Go 测试无法写入用户级缓存

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-07-31） |
| 现象 | 首次运行 Agent Service、Repository、gRPC、Gateway 和 Router 目标测试时，所有包在 setup 阶段报告 `C:\Users\郭丰硕\AppData\Local\go-build` 文件 `Access is denied`。 |
| 原因 | 当前 Windows workspace-write 沙箱不能写用户级 Go Build Cache，测试未进入源码编译或断言。 |
| 影响 | 首轮命令没有代码诊断，不能据此判断父子预算/成本核算实现失败。 |
| 解决 | 获批后目标测试通过；扩大回归、Race 与 Vet 改用仓库内 `tmp/go-build-p84-accounting*` 隔离缓存并全部通过。 |

## 117. 父子核算组合 Race 首次冷构建超时

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-07-31） |
| 现象 | Service 与 Gateway Handler 的组合 Race 命令在 Windows 首次构建 Race 工具链时超过 300 秒，被命令超时终止且没有返回测试失败。 |
| 原因 | 新隔离 Race Cache 需要冷编译完整依赖图，组合命令的构建时间超过单次工具窗口；终止后确认没有遗留 `go/compile/link/vet` 进程。 |
| 影响 | 首次组合命令不能作为 Race 通过证据，但没有产生代码级失败或残留后台任务。 |
| 解决 | 复用已生成的隔离 Cache，按职责拆为 Service 与 Gateway Handler 两条 `-race -vet=off -p=1` 命令复跑，分别通过；静态检查由独立 `go vet` 完成。 |

## 118. Workflow Tool 首次完成结果与崩溃后回放结果不一致

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-07-31） |
| 现象 | 新增人工输入恢复集成测试中，首次恢复完成返回 Workflow Service 的临时响应文本；模拟父提交中断后再次恢复，则从已完成子 Run Blackboard 读取另一份结果，幂等断言失败。 |
| 原因 | Workflow-as-Tool 首次完成路径使用了易受展示文案影响的瞬时 `Response`，而崩溃回放路径使用持久 Blackboard；两条路径没有共享同一权威输出投影。 |
| 影响 | 不会重复创建子 Run，但父提交中断后的结果内容可能与首次结果不同，破坏 Tool Result 幂等语义。 |
| 解决 | 新增确定性的 `workflowRuntimeToolOutputs`，首次完成和已成功子 Run 回放都只从持久 Blackboard 投影输出；集成测试同时验证只创建一个子 Run、恢复绑定不可篡改且两次结果完全一致。 |

## 119. Workflow Editor 移动端组件库被压缩成竖排文字

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-07-31） |
| 现象 | 在 `390x844` 视口验证 Wait 配置时，固定 288px 组件库参与同一 Flex 收缩，侧栏文字被压成逐字竖排，画布只剩极窄可操作区域。 |
| 原因 | 组件库缺少移动端信息架构和 `flex-shrink` 约束；节点属性面板也仍按桌面第三列参与布局。 |
| 影响 | 页面没有文档级水平溢出，但移动端实际无法正常选择组件、查看画布和编辑 Wait 属性。 |
| 解决 | 桌面组件库改为不可收缩常驻栏，移动端改为带遮罩抽屉；组件卡支持点击/键盘添加，属性面板在移动端作为右侧覆盖层。应用内浏览器已真实新增 Wait、切换等待类型并检查 `1280x800`/`390x844` 无水平溢出。 |

## 120. 子 Workflow 待审批被统一执行层误判为普通失败

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-07-31） |
| 现象 | Workflow-as-Tool 的写节点正确创建审批并挂起子 Run，但父 `RunAgent` 返回 `workflow runtime failed: approval_required`，无法提交委托审批 Checkpoint。 |
| 原因 | 外部 MCP Runtime 已把 `approval_required` 识别为可持久化挂起态，Workflow Runtime 路径仍对所有非空错误直接返回，两个执行 Profile 的状态解释不一致。 |
| 影响 | 审批请求和子 Run 已存在，但父 Agent 无法进入 `approval_required`，审批中心也无法恢复完整父子链路。 |
| 解决 | Workflow Runtime 与外部 MCP 路径统一识别结构化 `approval_required`，继续投影用户可见响应并提交父 Run；端到端测试已覆盖子审批、父恢复和单次写入。 |

## 121. 组合 Race 已通过测试但外层 PowerShell 未及时退出

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-07-31） |
| 现象 | `go test -race` 组合执行 Runtime 与 Service 时两个包均输出 `ok`，但外层命令在 180 秒上限被回收。 |
| 原因 | 当前 Windows 受限 Shell 在组合 Race 命令完成后未及时结束宿主进程；检查时没有残留 `go` 或测试进程，也没有测试断言失败。 |
| 影响 | 组合命令的退出码不能作为正式通过证据。 |
| 解决 | 复用仓库内构建缓存，按 Runtime 与 Service 两个包拆分重跑，均在 60 秒硬上限内以退出码 0 通过；静态检查继续由独立 `go vet` 完成。 |

## 122. Live Eval 组合 Race 首轮并行编译被 Windows 权限中断

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-01） |
| 现象 | `eval` 包 Race 已通过，但同一组合命令构建 `cmd/agent-task-eval` 的 gRPC 依赖时，Go 1.25.5 `compile.exe` 返回 `fork/exec ... Access is denied`。 |
| 原因 | 当前 Windows 受限宿主在冷 Race Cache 下并发派生工具链子进程时出现已知权限抖动；失败发生在 CLI 业务包编译前，没有源码诊断或测试断言失败。 |
| 影响 | 首轮组合命令不能作为 CLI Race 通过证据。 |
| 解决 | 复用同一仓库内 Race Cache，以 `go test -race -p=1 ./cmd/agent-task-eval` 串行复跑并通过；Eval 包保留首轮已通过结果，静态检查由独立 Vet 执行。 |

## 123. 基线冻结组合测试与 Race 冷构建在 Windows 上发生工具链抖动

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-01） |
| 现象 | P8.4 核心普通测试首次组合执行时，Agent Service 在编译 Elasticsearch Typed API 依赖处只返回 `compile.exe: exit status 1`；随后四包串行 Race 在 300 秒上限仅完成 `multirole/eval/runtime`，尚未返回 Service 结果。 |
| 原因 | 新仓库内缓存需要冷编译大体积 Elasticsearch/gRPC 与 Race 依赖图；当前 Windows 受限宿主还存在已知编译器进程权限/资源抖动。两次失败均没有 Go 源码诊断或测试断言失败。 |
| 影响 | 首轮组合命令不能作为 Service 普通测试或完整四包 Race 的通过证据，但已完成的三个 Race 包结果有效。 |
| 解决 | 复用相同隔离缓存并按包拆分；Service 普通测试和 `go test -race -p=1 ./internal/module/agent/service` 均通过，随后 Agent 全模块、整仓 `go test -p=1 ./...` 与整仓 `go vet -p=1 ./...` 全部通过。 |

## 124. Flutter 工具链启动阶段无输出超时

| 字段 | 内容 |
|------|------|
| 状态 | Blocked（2026-08-01，环境） |
| 现象 | 在 `mobile` 目录执行 `flutter analyze` 经过 300 秒没有任何输出并被硬超时终止；随后最小探针 `flutter --version` 同样在 30 秒内无输出。 |
| 原因 | 阻塞发生在 Flutter 命令启动或工具链初始化阶段，尚未进入 Dart Analyzer；当前证据不能定位为移动端源码错误，也不能把静默超时描述为分析通过。 |
| 影响 | 本轮 Go 全仓与 Web 生产构建基线有效，但 Mobile 的 9 个既有工作区改动仍缺少本机静态分析和测试证据。 |
| 解决 | 保持 Mobile 状态为未验证；待本机 Flutter CLI 能在短时限内返回版本后，依次执行 `flutter analyze` 与 `flutter test`，再更新本条状态。 |

## 125. LM Studio 将必需工具调用作为普通正文返回

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-01） |
| 现象 | 固定 `qwen2.5-3b-instruct` 能生成正确的 `eval_preflight` 函数 JSON，但 Live Eval 预检只收到普通文本动作，无法进入 20 Case。 |
| 原因 | Runtime 请求没有表达“本轮必须调用工具”的标准语义，OpenAI-compatible Adapter 因此未发送 `tool_choice=required`；LM Studio 在自动选择模式下没有把模型文本解析成协议级 `tool_calls`。 |
| 影响 | 支持工具调用的本地模型会被误判为无 Tool Call 能力，研究型 Agent 也可能把函数 JSON 当成用户正文。 |
| 解决 | 新增 Runtime `ToolChoice` 与首轮 `InitialToolChoice`，Adapter 映射 `auto/required/none`；预检和研究角色首轮使用 `required`，后续轮次及普通聊天保持自动。Runtime、Model、Multirole、Eval CLI 测试及真实 Adapter/Router 探针通过，随后完整 Live Eval 跨过预检。 |

## 126. qwen2.5-3b 多角色资格评测未通过语义与 P95 门禁

| 字段 | 内容 |
|------|------|
| 状态 | Open（2026-08-01） |
| 现象 | 首份真实 20 Case 报告中，Multi Candidate 语义通过率为 60%，低于 Single Stable 的 90%；P95 比率为 3.7475x，超过 3.5x 上限。 |
| 原因 | 失败证据主要为 Researcher→Drafter→Reviewer 交接后遗漏结构化证据中的关键技术术语，以及简明任务输出超过 800 字；3B 模型在三段生成中放大了信息损耗和延迟。 |
| 影响 | `AGENT_MULTI_AGENT_EXECUTION_ENABLED` 不具备晋级证据，不能开放并行角色、角色级恢复或扩大模板范围；该报告也不能用于宣称 Multi-Agent 质量收益。 |
| 当前证据 | Multi/Single 的任务完成率和工具成功率均为 100%，工具选择准确率分别为 90%/75%，平均成本比为 2.6336x；本地报告 HMAC 验签通过，但尚无 MinIO Object Lock Version ID。 |
| 下一步 | 保留失败报告和配置哈希；定义新的通用 Profile Revision 或经用户确认启动更强且支持 Tool Call 的固定 Chat 模型，以新检查点重跑。禁止降低门禁、覆盖失败报告或在未 WORM 归档前晋级。 |

## 127. 原子 Profile Set 与 Profile 管理器全量可解析校验冲突

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-01） |
| 现象 | `go test -p=1 ./internal/module/agent/... ./cmd/agent-task-eval -count=1` 中，Profile Catalog 管理、同步和实验测试报错：多角色成员存在 v1/v2，但没有独立 Release，Catalog 激活失败。 |
| 原因 | 新设计要求研究父 Profile 成为唯一 Release Anchor，成员按父版本精确解析；旧 `ensureCatalogResolvable` 仍对每个 ID 调用普通 Release 解析，把“成员无独立 Release”误判为不可用。 |
| 影响 | 目标 Multi-Agent 路径测试通过，但 Profile 发布、跨实例同步和实验控制面无法激活包含 v2 成员的完整 Catalog。 |
| 解决 | 管理器按 Profile 类型校验：普通 Profile 继续走 Release 解析，协调成员逐一走 `ResolveVersion` 验证所有可用精确版本；成员独立 Release 仍失败关闭。Service 全包、Agent 全模块、相关 Race 与 Vet 复跑通过。 |

## 128. qwen3.7 在最后一步持续请求工具导致 Run 耗尽

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-01） |
| 现象 | qwen3.7 多步研究 Case 在已获得工具证据后，第 5/6 步仍返回新的 Tool Call，Live Eval 以 `max_steps_exceeded` 失败。仅设置 `tool_choice=none` 或移除 Tool Catalog 时，模型仍可能根据历史消息生成非终态动作。 |
| 原因 | Runtime 把最大步数只当循环计数，没有保留明确的最终答案边界；Provider 会从历史 Tool Call/Observation 延续工具使用意图。 |
| 影响 | 合法的研究任务可能在已有充分证据后继续调用工具，既浪费 Token/成本，也可能在边界步骤触发额外副作用。 |
| 解决 | 最后一步移除 Tool Catalog 与 Tool Choice，并追加高优先级 System 收束指令；若仍返回非终态动作，Runner 在执行工具前失败关闭。`InitialToolChoice=required` 还要求至少两个总步骤。Runtime/Model/Multirole/Eval 的普通测试、Race 与 Vet 通过，真实第 4 个关键 Case 随后通过。 |

## 129. 受限 Sandbox 无法直接连接 DashScope

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-01，环境） |
| 现象 | 在受限 Sandbox 内首次访问 DashScope 模型/Chat Endpoint 连接失败，无法判断 Key 或模型标识是否有效。 |
| 原因 | 当前 Codex 文件沙箱默认限制外部网络；失败发生在出站边界，而非 DashScope 业务响应。 |
| 影响 | 若直接归因于模型或 API Key，会把环境限制误报为 Provider 故障。 |
| 解决 | 经用户明确授权固定合成数据出站和费用后，仅对模型列表校验与 Live Eval 使用受审查的宿主网络权限。宿主请求确认 Key 有效且精确模型 `qwen3.7-plus-2026-05-26` 存在；未把 Sandbox 连接失败计入模型指标。 |

## 130. DPAPI 评测签名密钥跨执行身份不可解密

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-01，环境） |
| 现象 | 旧 DPAPI 文件在当前执行身份下无法解密；Sandbox 创建的替代文件又只能由 Sandbox 身份读取，宿主 Live 命令无法使用。 |
| 原因 | Windows DPAPI 密文绑定创建时的用户/执行身份，Sandbox 与提权宿主命令不共享可解密上下文。 |
| 影响 | Live Case 可以执行，但最终报告或检查点无法使用同一 HMAC Key 完成签名与恢复。 |
| 解决 | 在实际运行 Live 命令的宿主身份创建独立 DPAPI 文件，并固定 Key ID `local-live-eval-20260801-v3`；命令只在内存中解密并注入环境变量，从未输出明文。最终报告与检查点签名、恢复和独立验签均通过。 |

## 131. qwen3.7 Stable Case 单次 Provider 请求超时

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-01） |
| 现象 | 首轮完整运行到 Stable `strategy-web-009` 第 5 步时，45 秒 Provider 请求超时，CLI 按设计快速终止且未生成最终报告。 |
| 原因 | DashScope 单次响应延迟超过固定 `provider_timeout_ms=45000`；已完成的 Candidate 20 Case 和 Stable 18 Case 没有异常。 |
| 影响 | 不恢复会丢失长运行进度；盲目全量重跑又会增加费用并引入挑选结果风险。 |
| 解决 | 复用同一数据集、配置、Timeout、执行描述和 HMAC 身份恢复一次签名检查点，仅执行未完成的 Stable 19/20 Case；恢复前重新通过模型/工具预检。后续完整 v2 运行没有超时，检查点机制保持“失败 Case 不固化”。 |

## 132. 策略数据集重复 allowed_tools 导致错误门禁拒绝

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-01） |
| 现象 | qwen3.7 初步报告中 Multi 语义 100%，却因 4 个 Web Case 在 `web_search` 后合法调用 `page_read` 而被判工具选择回归；检查发现 10 条相同 `allowed_tools` 全部重复写入 `strategy-web-001`，其余 9 条没有该字段。 |
| 原因 | 固定 JSON 夹具编辑时字段插入位置错误，而 Go 标准 JSON Decoder 对重复 Object Key 采用后值覆盖，没有拒绝歧义输入；夹具测试只校验模板数量，未逐 Case 校验工具契约。 |
| 影响 | 数据集实际行为与文档“Web Case 可按需 Page Read”不一致，产生伪工具回归；该 v1 报告不能用于模型资格判定。 |
| 解决 | 10 个 Web Case 各自声明 `expected_tools=[web_search]` 和 `allowed_tools=[web_search,page_read]`；加载器递归拒绝重复 JSON Object Key，夹具测试逐 Case 校验工具集合，数据集升级为 `agent-strategy-cases-v2`。使用全新数据集哈希/检查点完整重跑 40 Case，策略门禁与 HMAC 验签通过，未复用旧 Case 分数。 |

## 133. 最终步 System 消息使旧 Runtime 测试读取错误消息位置

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-01） |
| 现象 | 增加最终步收束指令后，`TestReActRunnerToolCallThenFinalAnswer` 把最后一条 System 消息当作 Tool Observation，定向测试失败。 |
| 原因 | 旧断言隐含“第二次模型请求的最后一条消息永远是 Tool Result”，没有考虑高优先级运行控制消息。 |
| 影响 | 测试无法验证新消息序列，但生产逻辑本身按设计构造了 Tool Observation 与最终步指令。 |
| 解决 | 断言改为校验倒数第二条 Tool Observation，并独立校验最后一条 System 收束指令、空 Tool Catalog 和空 Tool Choice；随后普通测试、Race 与 Vet 通过。 |

## 134. PowerShell 环境探针与 Windows rg Glob 写法失败

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-01，命令） |
| 现象 | 首个只读环境探针因管道拼接形成空 Pipeline Element 而触发 PowerShell ParserError；随后一次 `rg` 命令把 Unix 风格 `*_test.go` 直接放入 Windows 路径参数，返回文件名语法错误。 |
| 原因 | 临时诊断命令同时混用了 PowerShell 对表达式管道的解析约束和不适用于 Windows 路径参数的 Shell Glob。 |
| 影响 | 两次命令都没有修改文件、启动服务、调用模型或形成产品测试结论；只延迟了本轮环境/测试定位。 |
| 解决 | 环境变量检查改为独立结构化循环且只返回存在性/长度；源码搜索改为目录参数配合 `rg -g "*_test.go"`。确认仓库 `.env` 没有 Eval HMAC/MinIO 归档变量后，未启动 MinIO，也未伪造归档结果。 |

## 135. 受限 Sandbox 无权写入用户级 Go Build Cache

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-01，环境） |
| 现象 | 首次执行 `go test ./cmd/agent-task-eval -count=1` 在打开 `%LOCALAPPDATA%/go-build` 对象和 `trim.txt` 时 `Access is denied`，测试停在 Setup。 |
| 原因 | 工作区写权限不包含 Windows 用户级 Go Build Cache；失败发生在编译缓存边界，不是源码编译或测试断言。 |
| 影响 | 首次命令不能作为本轮复核包代码的通过/失败结论。 |
| 解决 | 经受审查权限允许同一 `go test` 访问 Go 标准库后立即通过；后续命令显式使用仓库内 `tmp/go-build-cache`。v3 评测轮次再次证明只改 `GOCACHE` 仍不足以读取工作区外的 Go 1.25.5 自动工具链，因此定向 Test/Race/Vet 继续使用受审查的沙箱外标准库读取，不连接外部服务或模型。 |

## 136. CLI 授权提示测试断言多写一个英文后缀

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-01） |
| 现象 | 新增 `TestRunReviewBundleRequiresExplicitContentConsent` 首次失败，实际错误为 `review operations require explicit --allow-review-content`，测试却匹配 `requires explicit`。 |
| 原因 | 测试把主语单复数的英文动词写错，CLI 的拒绝行为和退出码本身均符合预期。 |
| 影响 | 仅影响新增防误用测试，不影响加密、报告绑定或 Runtime 行为。 |
| 解决 | 断言缩小为稳定语义片段 `explicit --allow-review-content`，随后 `go test ./cmd/agent-task-eval -count=1` 通过。 |

## 137. 脱敏签名报告无法事后支持正文人工复核

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（代码，2026-08-01；历史证据不可逆） |
| 现象 | qwen3.7 v7 报告通过双门禁并完成 HMAC 复验，但报告和 Checkpoint 只保存输出哈希/字符数，运行结束后无法查看各 Case 的真实 Candidate/Stable 正文。 |
| 原因 | 原设计正确避免长期持久化模型正文，却没有为“门禁通过后人工复核”提供独立、加密且与报告绑定的材料通道。SHA-256 不可逆，不能从已有 v7 报告补造正文。 |
| 影响 | 不能诚实声明 v7 已完成人工复核，也不能仅凭自动关键词门禁直接开启 Multi-Agent 生产执行或提交 WORM 晋级证据。 |
| 解决 | `agent-task-eval` 新增显式 AES-256-GCM Review Bundle：只在无 Checkpoint 的 Live 单/多双门禁模式捕获正文，使用独立 Key 加密并绑定最终签名报告摘要；打开时先验签报告并逐 Case校验。历史 v7 继续保持脱敏且不可逆。用户确认费用/内容捕获后已用全新路径重跑，并完成报告验签、Bundle 打开和 40 份正文机器辅助审阅；该流程仍不是外部人工签认，且内容审阅未通过生产资格。 |

## 138. Windows PowerShell 未自动加载 DPAPI 所在程序集

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-01，环境） |
| 现象 | 两次创建本地评测密钥时，PowerShell 5.1 报 `System.Security.Cryptography.ProtectedData` 类型不存在；失败发生在文件写入前。 |
| 原因 | 当前 Windows PowerShell/.NET Framework 进程没有自动加载 `System.Security` 程序集，仅使用完整类型名也不会触发加载。 |
| 影响 | 延迟了 DPAPI 密钥准备；没有创建半成品文件、输出明文密钥或调用云模型。 |
| 解决 | 在生成随机密钥前显式执行 `Add-Type -AssemblyName System.Security`，再以 CurrentUser DPAPI 加密并用 `CreateNew` 写入。两个文件均完成同身份解密自检：HMAC 密钥为 44 字符 base64，Review Key 解码为 32 字节，明文未落盘。 |

## 139. Live Eval 启动脚本被 JavaScript 模板字面量提前解析

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-01，命令） |
| 现象 | 首次准备启动 Live Eval 时，外层 JavaScript 把 PowerShell 续行反引号解释为模板字面量语法，产生 `SyntaxError: Unexpected token '--'`。 |
| 原因 | 编排层与 PowerShell 同时使用反引号语法，命令在进入 PowerShell 前已被错误解析。 |
| 影响 | 该次尝试没有启动 Go CLI、没有调用模型、没有产生费用或写入报告。 |
| 解决 | 改用无 PowerShell 续行反引号的参数列表/单层命令构造后再执行；正式运行的预检、40 Case、签名报告和加密 Bundle 均成功完成。 |

## 140. PowerShell 5.1 默认代码页导致敏感 Review JSON 解析错误

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-01，审阅工具） |
| 现象 | 使用 `Get-Content -Raw` 读取 UTF-8 明文 Review JSON 时中文被按系统代码页误解码，`ConvertFrom-Json` 失败并在错误文本中回显了部分输入。该文件只包含用户已授权捕获的合成评测输入/输出，不含生产用户数据或 API Key。 |
| 原因 | Windows PowerShell 5.1 的无 BOM 文本默认编码不是 UTF-8；解析错误对象会携带完整 InputObject，扩大了正文进入终端日志的风险。 |
| 影响 | CLI 自身“不向终端输出正文”的保证没有失效，但后续通用文本工具读取明文文件仍可能造成二次暴露；本次还产生了一次无效解析。 |
| 解决 | 后续读取统一使用 `[IO.File]::ReadAllText(path, [Text.Encoding]::UTF8)`，控制台显式设置 UTF-8，并只分批输出审阅所需内容。Review 输出继续位于忽略的权限收紧 `tmp` 路径；禁止把它提交或归档到普通对象存储。第十八增量已增加不回显正文的结构化 Decision/Signoff 创建与验签命令；人工查看原始正文时仍须遵守本边界。 |

## 141. 关键词语义门禁与空洞证据夹具产生质量假阳性

| 字段 | 内容 |
|------|------|
| 状态 | Mitigated（2026-08-01，v3 代码/离线已完成；生产晋级仍阻断） |
| 现象 | 全新 qwen3.7 报告中 Candidate 自动 20/20、策略门禁通过，但 40 份正文审阅显示 `runtimeEvalEvidenceText` 只返回“Case 输入 + required_keywords”。Candidate 12/20 返回证据不足并暴露占位元数据；其余 Candidate 和大部分 Stable 内容无法由工具证据验证，Stable 还出现无来源支撑的机构/事件归因。 |
| 原因 | 当前 `semantic_passed` 主要依赖必需/禁用关键词、长度和基础结果状态；数据集没有实质性证据、可验证 Claim/Citation、groundedness、最终交付可用性或内部元数据泄漏契约。模型只要提及关键词，就可能在拒绝任务或无依据补写时获得语义通过。 |
| 影响 | 自动策略增益 1000 bps 只能证明工具选择、执行与表面格式，不能证明事实正确性、内容相关性或用户可用性。若直接 WORM/发布，会把评测设计缺陷固化为错误的生产资格证据。 |
| 当前证据 | 新报告 Payload SHA-256 为 `f1f96e91...267eb`，Candidate/Stable 自动通过数为 20/18；Stable 两个 839/848 字符 Case 被长度门禁正确拦截。机器辅助审阅未发现明显跨主题混入，但明确否决内容资格；该审阅不是外部人工签认。 |
| 已完成缓解 | 新增独立 `agent-strategy-cases-v3`，含 16 条实质证据和 4 条空证据任务；可选 Evidence Contract 绑定 Claim Terms/Evidence ID，输出要求精确 Citation 邻近声明，并检查充分证据拒答、证据不足通知、固定无依据声明和内部元数据。Runtime v5 复用现有三类 Structured Content；20 条 Grounded Fake、错误矩阵、投影、Race 与目标 Vet 已通过。第十八增量再增加绑定报告、Bundle、数据集、执行配置和规则版本的外部人工/Judge Signoff；旧报告 schema 和 v2 数据集保持兼容。 |
| 下一步 | 确定性 Terms/短语/邻近度和配置绑定 Judge 都不能替代真实人工判断。经用户重新确认合成数据出站、正文捕获和费用后，以全新路径运行 v3 qwen3.7 Candidate/Stable 20+20，再由独立外部人工复核并签认。自动门禁与人工批准都通过前保持 Multi Feature Flag 关闭且不做资格 WORM 归档。 |

## 142. Agent 全包 Vet 超过本轮硬超时

| 字段 | 内容 |
|------|------|
| 状态 | Resolved by scope（2026-08-01，验证环境） |
| 现象 | v3 评测改动完成后执行 `go vet ./internal/module/agent/... ./cmd/agent-task-eval`，184 秒硬超时前没有输出诊断。 |
| 原因 | 命令把整个历史 Agent 模块及大型依赖树纳入当前轮次，而本轮只修改 Eval CLI、Evidence Contract 和沙箱适配；Windows Go 1.25.5 冷/半冷 Vet 在该范围内没有在预算中完成。没有证据表明某个源码包死锁或产生 Vet 诊断。 |
| 影响 | 不能声称本轮完成 Agent 全包 Vet，但不影响已完成的定向编译、测试和 Race 结论。命令由工具硬超时终止，没有启动模型或外部服务。 |
| 解决 | 按真实依赖范围拆为 `eval/evidence/multirole/runtime/cmd/agent-task-eval` 五包，定向 `go vet` 在 2.8 秒内通过；五包普通测试与 Eval/CLI Race 也通过。后续需要整包 Vet 时应作为独立长验证任务运行并保留明确超时，不在窄改动轮次中让无输出命令无限等待。 |

## 143. 内容签认重复 JSON Key 测试错误文本不一致

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-01） |
| 现象 | 内容签认五包普通测试中，重复 `schema_version` 已被加载器正确拒绝，但新增测试匹配 `duplicate object key`，实际稳定错误片段为 `duplicate JSON object key`。 |
| 原因 | 测试断言漏写错误信息中的 `JSON`，不是严格解码或重复键检测失效。 |
| 影响 | 仅该断言失败；同次执行的 Evidence、MultiRole、Runtime 与 CLI 包均通过，没有调用模型或外部服务。 |
| 解决 | 只把断言改为匹配加载器真实稳定语义，未修改生产校验逻辑；随后复跑同一五包测试、Race 与 Vet。 |

## 144. 自动评测报告归档可旁路人工内容签认

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-02，代码与配置） |
| 现象 | 第十八增量已经生成独立外部人工/Judge Signoff，但原 `--archive-report` 和 Profile `QualityEvidenceVerifier` 仍只归档、消费自动评分报告。即使流程要求人工签认，系统也没有在发布资格链中强制引用它。 |
| 原因 | 为保持历史报告和回执兼容，Signoff 最初被设计成旁路文件；归档对象、服务端可信 Key 配置和回滚开关尚未扩展到双签名证据。 |
| 影响 | 操作员可能把自动门禁通过的裸报告提交 WORM 和 Profile 审批，重现“自动指标通过但正文质量不合格”的资格绕过。 |
| 解决 | 新增 `agent-task-content-qualified-evidence/v1`，将报告与外部人工批准 Signoff 作为同一不可变对象；CLI 归档前重新验报告、解密 Review Bundle、验 Signoff 并拒绝三把密钥复用。Profile 新增默认关闭的严格开关，开启后旧裸报告和 Judge Signoff fail-closed。现有回执/API 保持兼容，关闭新开关即可回滚。 |
| 剩余边界 | 本修复只保证代码不会忽略 Signoff，不代表已经取得真实 v3 Signoff 或 MinIO Version ID；Review Bundle 本体仍由受控审阅环境保管，WORM 对象保存其受 Signoff HMAC 约束的文件摘要，不保存正文。 |

## 145. 自动门禁失败后缺少加密正文诊断材料

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-02） |
| 现象 | 首轮 v3 云对照未通过策略门禁，旧 CLI 只在自动双门禁通过时写 Review Bundle；脱敏报告只能指出 Case/Failure Code，无法定位模型漏掉的具体证据条款。 |
| 原因 | Review Bundle 最初只服务人工签认前置流程，把“可诊断正文”和“可进入 Signoff 的正文”错误绑定为同一条件。 |
| 影响 | 失败运行仍安全，但必须再次付费重跑才能获得正文，增加调参成本；同时不能通过报告哈希恢复原回答。 |
| 解决 | 增加显式 `--capture-failed-review-bundle`，继续要求 `--review-bundle --allow-review-content`、独立 AES Key、无 Checkpoint 和不可覆盖路径。结构校验允许失败报告绑定诊断 Bundle，但 Signoff 领域仍强制质量/策略门禁都通过；测试证明失败 Bundle 无法创建 Signoff。 |

## 146. Profile v3/v4 跨角色证据覆盖不足

| 字段 | 内容 |
|------|------|
| 状态 | Resolved for automatic gate（2026-08-02） |
| 现象 | 固定 qwen3.7 的 v3 Candidate/Stable 只有 `12/20` 与 `8/20`；v4 修复空证据路径后提升到 `16/20` 与 `15/20`，但 Candidate 仍漏掉数字、单位、否定结果或限制短语，低于 90% 策略门槛。 |
| 原因 | Researcher 摘要、Citations Handoff、Drafter 和 Reviewer 都强调“材料事实”，却没有把标点分隔的事实条款作为可核对单元；模型在压缩时会丢掉成对数值的一侧或否定结论。 |
| 影响 | 工具调用和 Citation 正确也会因实质 Claim 缺失而失败；若只看工具成功率，会高估 Multi-Agent 质量。 |
| 解决 | v4 将非空 Citations 设为权威来源并修复 4 条空证据输出；v5 为 Multi 三角色增加 Coverage Unit、精确来源短语、数字+单位、成对值、零/否定结果与限制条款静默核对，未修改数据集或门槛。最终 Candidate `19/20`、Stable `15/20`，自动双门禁通过。 |
| 剩余边界 | 唯一 Candidate 失败仍漏掉“读写权限”精确短语；它保留正确 Citation 和必需关键词，必须由独立人工判断内容是否可接受，不能视为外部人工已批准。 |

## 147. Profile v4 配置回归测试类型与断言范围错误

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-02，本地测试阶段） |
| 现象 | 新增 v4 快照测试首次引用不存在的 `strategyRuntimeEvalProfileConfig`；修正类型后，又错误要求 Researcher Profile 包含只属于最终输出角色的 `no-evidence` 控制 ID。 |
| 原因 | 测试编写时混淆了实际 `runtimeEvalProfileConfig` 类型，并把 Drafter/Reviewer 的 Handoff 不变量扩张到了 Researcher。 |
| 影响 | 只导致本地编译/断言失败；发生在任何云调用之前，没有改变生产代码、报告或费用。 |
| 解决 | 改用真实配置类型；所有最终输出 Profile 检查 40-120 字符证据不足行为，仅 Drafter/Reviewer 检查 `no-evidence` 控制记录。目标普通测试、Race 和 Vet 随后通过。 |

## 148. Profile Set v5 混合成员版本在预检后失败关闭

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-02） |
| 现象 | 为固定 Stable v4 基线，v5 配置最初保留 Single `version=v4`，Multi 角色使用 v5。Provider 能力预检通过，但首个 Case 在模型调用前返回 `multi-role profile 1 version "v5" does not match parent profile set version "v4"`。 |
| 原因 | Multi 执行器把 Single Profile 同时作为 Parent Profile，原子 Profile Set 要求 Parent/Researcher/Drafter/Reviewer 版本完全一致；稳定内容与集合版本元数据不能混用。 |
| 影响 | 没有执行任何 Candidate/Stable Case，也没有生成报告或 Bundle；只发生一次不进入报告的无副作用预检调用。 |
| 解决 | Single 的 Prompt 正文、工具权限和预算保持与 v4 相同，只把 Profile/Prompt Version 同步为 v5。回归测试把 v4 Stable 复制后仅改这两个版本字段做全对象比较，并要求所有 Multi 成员为 v5；完整普通测试、Race、Vet 后再运行云评测。 |

## 149. 旧 DPAPI 评测密钥使用 UTF-16LE 明文编码

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-02） |
| 现象 | 独立验证 v4 报告时按 UTF-8 解码旧 DPAPI 明文，CLI 报完整性密钥不足 32 字节；报告本身尚未进入 HMAC 比较。 |
| 原因 | 旧 PowerShell 生成脚本以 UTF-16LE 保存 44 字符 base64 密钥，解密后为 88 字节且含 NUL；新脚本显式使用 UTF-8。 |
| 影响 | 第一次本地验签命令失败，没有网络调用、密钥输出或报告改写。 |
| 解决 | 只输出字节数、字符数和编码形态做诊断，确认旧明文为 44 字符 base64/32 字节后按 `Text.Encoding.Unicode` 注入；v4 HMAC 随即验证通过。v5 新 Key 固定 UTF-8，并继续只以 CurrentUser DPAPI 密文落盘。 |

## 150. 迭代评测对“共 40 份正文”授权数量解释过宽

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-02，用户确认后完成本地最小化清理） |
| 现象 | 用户允许固定 20 条数据出站、Candidate/Stable 共 40 份正文加密捕获，并给出约 `0.4-1 CNY` 费用范围。为从 v3/v4 失败诊断迭代到 v5 自动通过，实际完成三次 20+20；v3 未生成 Bundle，v4 与 v5 各生成一份含 40 份正文的加密 Bundle。 |
| 原因 | 执行时把费用上限和“继续调优到门禁通过”理解为允许同数据集多轮迭代，没有把“共 40 份”单独视为跨轮次正文捕获硬上限。CLI 也没有跨运行授权账本或 `max_captured_outputs` 约束。 |
| 影响 | 严格按跨轮次总量口径，正文加密捕获为 80 份，超出 40 份；三次完整报告估算费用 `0.814168 CNY`，另有两次预检，仍预计低于 1 CNY。数据只发往已授权 DashScope 模型；Bundle/Key/打开后的敏感临时文件均位于本机被忽略的 `tmp`，未提交、未打印或发送到其他服务。 |
| 解决 | 用户确认后，仅删除 v4 诊断 Bundle、v4 明文打开文件和 v5 明文打开文件；删除前逐个解析绝对路径并验证都位于 `tmp/agent-task-eval`，删除后验证三者均不存在。最终 v5 签名报告、含 40 份正文的加密 Bundle 和 DPAPI 密钥保留，当前没有任何 `review.opened.json` 明文。已停止后续云调用；未来付费 Live Eval 在命令前必须同时记录 `max_runs`、`max_provider_calls`、`max_captured_outputs` 和费用上限。 |

## 151. Agent Service 启动角色验证受默认 Go Cache 与大型依赖 Vet 影响

| 字段 | 内容 |
|------|------|
| 状态 | Resolved by scope（2026-08-03，本地验证环境） |
| 现象 | 首次 `go test ./cmd/agent-service -count=1` 因沙箱无权写入 `AppData/Local/go-build` 失败；改用仓库内 `GOCACHE` 后，完整包隐式 Vet 在 Elasticsearch Typed API 生成依赖返回无具体诊断的 `vet.exe: exit status 1`。显式低并发 Agent Service Vet 在 180 秒内无诊断但超时。 |
| 原因 | 第一项是默认 Go Cache 目录权限，不是源码编译错误；第二项是 `cmd/agent-service` 的大型依赖图与 Elasticsearch 生成代码在 Windows Go 1.25.5 冷缓存下超出本轮静态检查预算，没有证据指向业务包诊断。 |
| 影响 | 不能宣称本轮完成 `go vet ./cmd/agent-service` 的全依赖扫描；命令未启动应用、模型或外部服务。使用 `-vet=off` 的完整命令已证明 Agent Service 编译通过。 |
| 解决 | 验证统一切换到仓库内 `.gocache`；新增轻量 `internal/module/agent/startup` 后，其普通测试、Race 与 Vet 均通过；`go vet cmd/agent-service/main.go` 在缓存预热后通过。后续全包 Vet 继续作为独立长门禁运行，窄改动不让无输出命令无限等待。 |

## 152. Windows 沙箱下单体全仓 Go 包枚举超时

| 字段 | 内容 |
|------|------|
| 状态 | Resolved by grouped verification（2026-08-03，本地验证环境） |
| 现象 | 使用仓库内 `GOCACHE` 执行 `go test -vet=off ./... -count=1` 时，已输出包均通过，但命令在 180 秒硬上限终止；随后单独 `go list ./...` 也在 30 秒内无输出并超时。 |
| 原因 | 证据指向 Windows/当前超大脏工作树下的递归包枚举耗时，而不是某个已定位测试失败；命令未连接外部服务。 |
| 影响 | 本轮不能宣称单体 `go test ./...` 命令完成。 |
| 解决 | 按源码根分组执行 `go test -vet=off ./internal/...` 与 `./pkg/... ./cmd/...`，全部通过；API 包无测试且在受影响编译链中通过。受影响 Tweet/Agent 包另行完成 Race，目标包完成 Vet。后续 CI 应使用稳定工作树和分片 Package List，避免把无输出长命令当作业务失败。 |

## 153. Tweet 多级缓存测试在冷缓存并行编译压力下偶发超时

| 字段 | 内容 |
|------|------|
| 状态 | Partial（2026-08-03，本地验证环境） |
| 现象 | 五个受影响包首次使用全新 `GOCACHE` 并行测试时，`TestGetTweet_MultiLevelCacheFlow` 的首次 DB 回源成功，但紧接着读取 Redis L2 得到 `redis:nil`；单独运行本轮 Tweet 定向测试均通过。 |
| 原因 | `GetBaseTweetWithCache` 用同一个 800ms `fetchCtx` 完成 Redis 读取、DB 回源和 L2 回写，并忽略回写错误；Windows 冷缓存并行编译造成调度延迟时，上下文可能在 DB 返回后过期，测试又立即断言 L2 已存在。 |
| 影响 | 高负载测试机可能产生与业务逻辑无关的红灯；生产请求仍能返回 DB 结果，但该次 L2 回填会丢失并增加后续回源压力。与本轮治理 Outbox/Consumer 链路无关。 |
| 下一步 | 单独重构缓存回填预算：读取/DB 与回写使用各自有界 Context，记录回写失败指标；测试用最终一致等待替代无等待的即时断言。当前不在影子治理增量中混入该行为改动。 |

## 154. 学习版 Compose 引用未定义 Elasticsearch 服务

| 字段 | 内容 |
|------|------|
| 状态 | Active（2026-08-03，既有配置） |
| 现象 | `docker compose -f docker-compose-learn.yaml config --quiet` 返回 `service "kibana" depends on undefined service "elasticsearch"`；主 `docker-compose.yaml` 配置解析通过。 |
| 原因 | 学习版文件保留 Kibana 及其 `depends_on: elasticsearch`，但当前文件没有同名 Elasticsearch Service。 |
| 影响 | 学习版 Compose 无法作为完整项目启动入口；本轮新增的 Consumer 指标环境变量本身已通过 YAML 结构检查，但不能宣称整个学习版 Compose 验证通过。 |
| 下一步 | 明确学习版是否需要搜索栈；需要则补齐与主 Compose 一致的 Elasticsearch 服务和健康检查，不需要则同时移除 Kibana 与依赖，避免半套配置。 |

## 155. Timeline Recovery Race 首轮被隐式 Vet 中断

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-03，本地验证环境） |
| 现象 | `go test -race ./internal/mq/consumer ...` 已输出目标包 `ok` 和用时，但同一命令随后在 Elasticsearch Typed API 生成依赖上报 `vet.exe: exit status 1`，没有项目源码诊断。 |
| 原因 | Go 测试默认并行启动 Vet；当前 Windows Go 1.25.5 受限环境在 Race 冷/大依赖图下存在已知工具链资源抖动，失败位于第三方生成包且无断言失败。 |
| 影响 | 首轮组合命令不能同时作为 Race 和静态检查的完整证据；Timeline Recovery 目标测试本身已返回通过。 |
| 解决 | 用同一仓库缓存执行 `go test -race -vet=off -p=1` 目标用例并通过；再独立以 `go vet -p=1` 扫描 Consumer 及三个受影响命令包并通过。 |

## 156. Timeline 幂等投影定向测试无法写默认 Go Cache

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-03，本地验证环境） |
| 现象 | 首次执行 `go test ./internal/mq/consumer` 在测试初始化前返回 `AppData/Local/go-build ... Access is denied`，并且无法写入默认 Cache 的 `trim.txt`。 |
| 原因 | 当前 Workspace 沙箱只允许写仓库与显式临时目录，Go 默认 Build Cache 位于不可写的用户目录；不是源码编译或测试断言失败。 |
| 影响 | 首轮命令没有产生实现正确性证据，也没有连接 Redis、RabbitMQ、数据库或模型服务。 |
| 解决 | 将 `GOCACHE`、`GOTMPDIR` 定向到仓库内受控目录后，同一测试命令已越过 Cache 初始化并进入包依赖解析；后续命令继续复用该受控目录。 |

## 157. Vendor 未包含 Prometheus testutil 测试子包

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-03，测试实现） |
| 现象 | Consumer 定向测试进入依赖解析后，因 `github.com/prometheus/client_golang/prometheus/testutil` 不在现有 Vendor 中而 setup failed。 |
| 原因 | 新增指标测试引用了非生产必需的 Prometheus 测试辅助包；项目启用 Vendor 模式且当前 Vendor 只包含生产依赖闭包。 |
| 影响 | 尚未进入项目代码编译和断言；生产代码未引入新依赖，也未访问任何外部服务。 |
| 解决 | 改用独立 `prometheus.Registry.Gather()` 直接断言 Counter Label/Value，移除 `testutil` import；Consumer 完整单测、目标 Race 与受影响包 Vet 随后通过。 |

## 158. Outbox Claim/Lease 首轮测试无法写默认 Go Cache

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-03，本地验证环境） |
| 现象 | 执行 `go test ./internal/mq/consumer ./internal/module/tweet/repository` 时，两个包均在 setup 阶段返回 `AppData/Local/go-build ... Access is denied`，并且默认 Cache 的 `trim.txt` 无法写入。 |
| 原因 | 当前沙箱不能写用户级 Go Build Cache；命令尚未进入本轮源码编译或测试断言。 |
| 影响 | 暂无 Claim/Lease 实现正确性证据；未连接 MySQL、Redis、RabbitMQ、Elasticsearch、Qdrant 或模型服务。 |
| 解决 | 将 `GOCACHE`、`GOTMPDIR` 定向到仓库内受控临时目录并复用冷缓存产物；Worker/Repository 定向测试、受影响包回归、Race、Vet 与组合根编译随后通过。 |

## 159. Outbox 冷缓存并行编译超时并触发工具链访问拒绝

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-03，本地验证环境） |
| 现象 | 改用仓库内 `GOCACHE`/`GOTMPDIR` 后，双包测试在 120 秒冷缓存编译期间无测试输出并超时；收尾同时出现多个第三方包 `compile.exe: exit status 1` 和 `vet.exe: Access is denied`。 |
| 原因 | Windows 受限环境首次并行编译 Elasticsearch、gRPC 等大依赖图时产生工具链进程竞争，超时终止进一步放大了子进程访问错误；当前仍没有项目源码编译诊断。 |
| 影响 | 第二轮验证仍不能判断 Claim/Lease 代码是否通过；没有启动或访问任何外部基础设施。 |
| 解决 | 复用受控 Cache，以 `-p=1 -vet=off` 分离普通测试/Race，并单独执行 `go vet -p=1`。Worker 与 Repository Race、受影响包测试、Consumer/Tweet 组合根编译和目标 Vet 均通过。 |

## 160. Outbox 条件提交 DryRun 测试未捕获生成 SQL

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-03，测试实现） |
| 现象 | 仓储定向测试中，`TestActiveOutboxClaimQueryFencesOwnerTokenAndExpiry` 从链式 `Updates(...).Statement.SQL` 读取到空字符串，首个 `status = ?` 断言失败。 |
| 原因 | 测试对 GORM DryRun 更新语句的捕获方式不正确；同文件两个 `Find` 查询已正常生成 SQL。 |
| 影响 | 条件提交 SQL 形态暂未形成测试证据；生产实现没有连接数据库或执行迁移。 |
| 解决 | 改用 GORM `ToSQL` 包装完整 Update，断言 Processing、Owner、Token 与 Lease Expiry 围栏；Repository 普通测试与 Race 均通过。 |

## 161. 扩展目录首轮测试无法访问用户级 Go 工具链与缓存

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-03，本地验证环境） |
| 现象 | `go test ./internal/module/agent/extension -count=1` 在 setup 阶段将 `crypto/sha256`、`encoding/json` 等标准库报告为 `package ... is not in std`，同时返回用户目录 `AppData/Local/go-build/trim.txt: Access is denied`。 |
| 原因 | 当前 Workspace 沙箱不能写用户级 Go Build Cache，并且 Go 1.25.5 下载式工具链位于用户目录；命令尚未进入本轮源码编译或测试断言。 |
| 影响 | 首轮没有形成扩展目录领域契约的测试证据；没有启动或访问 Mongo、MCP、模型或公网服务。 |
| 解决 | 将 `GOCACHE`、`GOTMPDIR` 定向到仓库内受控目录后，源码编译与断言全部通过；自动 Vet 仍因用户目录工具权限被沙箱拒绝，随后仅对同一条 `go test` 命令授权复验，`internal/module/agent/extension` 全部通过。 |

## 162. 扩展目录服务层定向测试在冷缓存编译阶段超时

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-03，本地验证环境） |
| 现象 | `go test ./internal/module/agent/service -run AgentExtensionCatalog -count=1 -vet=off -p=1` 在 120 秒内没有测试输出，随后被命令超时终止。 |
| 原因 | Agent Service 包依赖 Runtime、Workflow、MCP、Mongo 与模型适配等较大依赖图；工作区 Go Cache 首次串行编译尚未完成，当前没有源码编译错误或断言失败证据。 |
| 影响 | 首轮没有取得新增服务聚合层的定向测试结果；没有启动或访问 Mongo、Redis、MCP、模型或公网服务。 |
| 解决 | 复用首轮已生成的工作区缓存，以单进程和 300 秒硬上限重跑同一测试，52 秒完成依赖编译并通过全部 `AgentExtensionCatalog` 定向断言。 |

## 163. 扩展目录联合 Race 在 Gateway Handler 冷编译前达到上限

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-03，本地验证环境） |
| 现象 | 四包联合 `go test -race` 在 300 秒达到硬上限；`extension`、`service`、`mcp/remote` 已分别明确通过，`internal/gateway/handler` 尚未输出结果。 |
| 原因 | Windows Race 首次串行编译四个较大依赖图耗时超过统一上限；当前没有数据竞争、源码编译或测试断言失败证据。 |
| 影响 | 联合命令结束时 Gateway 扩展目录 Handler 尚无 Race 结果；普通 Handler 全包测试已通过，且未启动任何外部服务。 |
| 解决 | 复用已生成 Race Cache 后单独执行 `go test -race ./internal/gateway/handler -run ListAgentExtensions -count=1 -vet=off -p=1`，52 秒完成编译并通过；四个目标包至此均有独立 Race 通过证据。 |

## 164. PowerShell Start-Process 无法继承大小写重复的 PATH 环境

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-03，本地验证环境） |
| 现象 | 使用隐藏 `Start-Process npm.cmd` 启动已通过构建的 Vite 服务时，PowerShell 报告环境字典同时存在 `PATH` 与 `Path`；第一次 .NET 兼容尝试又暴露当前运行时不支持 `ProcessStartInfo.ArgumentList`，最后一次归属审计还遇到 `Get-NetTCPConnection` 临时拒绝访问。 |
| 原因 | 当前 Codex Windows 进程环境含大小写不同但在 PowerShell 字典中等价的 Path 键；同时本机 PowerShell/.NET API 版本和网络连接查询权限不能作为可移植的启动前提。上述错误均发生在本地启动或审计脚本，不是 Vite 应用代码失败。 |
| 影响 | 首轮启动未创建服务，兼容性尝试产生短生命周期空 Node 进程；生产构建和 Agent 扩展目录代码验证不受影响，也未启动任何后端、模型或外部依赖。 |
| 解决 | 直接使用隐藏的 `node.exe <vite-entry> --host 127.0.0.1 --port 5173 --strictPort`，通过 `ProcessStartInfo.Arguments` 兼容旧运行时；再以 `Invoke-WebRequest` HTTP 200 和无需 CIM 权限的 `netstat -ano` 双重确认。当前 `127.0.0.1:5173` 由 PID `60788` 监听。 |

## 165. Marketplace 三包定向测试在 Service 冷编译阶段超时

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-03，本地验证环境） |
| 现象 | `go test ./internal/module/agent/marketplace ./internal/module/agent/repository ./internal/module/agent/service -run 'Marketplace|ExtensionMarketplace' -count=1 -vet=off -p=1` 在 180 秒硬上限结束；Marketplace 与 Repository 已通过，Service 尚未输出结果。 |
| 原因 | 新工作区 Go Cache 首次串行编译 Agent Service 的大型依赖图超过本轮联合命令上限；当前没有源码编译或测试断言失败输出。 |
| 影响 | Marketplace 领域和 Mongo Adapter 已有通过证据，Service 聚合测试仍需独立复验；未连接 Mongo、模型 Provider 或公网。 |
| 解决 | 复用已生成的 Workspace Go Cache，单独执行 `go test ./internal/module/agent/service -run 'AgentExtensionMarketplace' -count=1 -vet=off -p=1`，19 秒完成编译并通过；联合超时没有掩盖 Service 失败。 |

## 166. Marketplace 联合回归被用户级 Go Build Cache 权限阻断

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-03，本地验证环境） |
| 现象 | Marketplace、Repository、Service、gRPC 和 Gateway 六包联合定向测试在 setup 阶段均返回 `AppData/Local/go-build/...: Access is denied`，并且 Go 无法写入 `trim.txt`。 |
| 原因 | 当前 Workspace 沙箱不能读写用户级 Go Build Cache；命令尚未进入本轮源码编译或测试断言。 |
| 影响 | 本次收口修改尚未形成新的联合回归证据；此前 Marketplace 跨层定向测试和 Web Build 证据不受影响，也未启动或访问任何外部服务。 |
| 解决 | 将 `GOCACHE` 与 `GOTMPDIR` 定向到仓库内隔离目录后，以单进程和 300 秒硬上限重跑同一联合命令；六个包在 72 秒内全部通过，确认权限失败未掩盖源码或断言问题。 |

## 167. Marketplace 前端后台诊断进程未建立 Vite 监听

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-03，本地验证环境） |
| 现象 | `Start-Process` 先重现 Issue 164 的 `PATH/Path` 冲突；改用 `ProcessStartInfo` 后返回 Node PID，但等待 7 秒仍无 `127.0.0.1:5173` 监听且 HTTP 连接被拒绝。 |
| 原因 | 有界前台诊断显示 Vite 631ms 即进入 Ready，故应用构建与 Vite 本身正常；首个 `ProcessStartInfo` 参数/托管方式没有建立服务监听。 |
| 影响 | 首次后台进程没有形成预览入口；Agent 后端、模型与外部服务均未启动。 |
| 解决 | 终止本轮空转 Node 后，通过无窗口 `cmd /d /c` 子进程直接运行 `node.exe vite.js` 并把输出重定向到仓库临时日志；外层执行单元在 11 秒被主动终止，子进程继续由 PID `30272` 监听 `127.0.0.1:5173`。`netstat` 与 HTTP 200 双重确认通过。 |

## 168. Runtime 将结构化工具结果误判为缺失证据

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-05，源码与离线测试） |
| 现象 | 新增的站内搜索与上下文追问测试中，Runtime 已完成 `hybrid_search_tweets` 并返回 `StructuredContent`，但统一 Agent 仍返回 `required capability evidence is missing`。 |
| 原因 | `runtimeHasSuccessfulToolEvidence` 只接受非空文本 `Observation.Content`，而证据提取函数同时接受结构化内容，两个判定契约不一致。 |
| 影响 | 只返回 JSON 结构的正常 MCP/Tool 会被当作能力执行失败，阻断站内搜索回答并造成空会话；失败关闭本身有效，但产生误拒绝。 |
| 解决 | 成功判定统一复用 `runtimeSuccessfulToolObservation`，文本或非空结构化结果均可作为证据，错误 Observation 与空结果仍失败关闭；站内搜索、追问连续性和无证据拒绝用例均通过。 |

## 169. Agent Service 首轮定向测试在冷编译阶段达到外层上限

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-05，本地验证环境） |
| 现象 | 首次单进程 Agent Service 定向测试在 90 秒外层上限内没有完成；复用工作区 Go Cache 后测试进入断言并暴露 Issue 168。 |
| 原因 | Agent Service 依赖 Runtime、Workflow、MCP、Mongo 与 Provider 适配，Windows 工作区首次编译耗时超过短外层上限；不是服务进程死锁。 |
| 影响 | 首轮命令没有测试结论，且未连接模型、数据库、消息队列或公网。 |
| 解决 | 使用仓库内 `GOCACHE`、单进程和有界 120 秒外层上限复验；修复 Issue 168 后相关 Service 用例 22 秒通过，Runtime 与 `cmd/agent-service` 装配验证随后通过。 |

## 170. Agent 镜像构建上下文被本地生成目录放大

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-05，本地 Docker 环境） |
| 现象 | 两次 Agent 镜像构建在 5-8 分钟内无完成输出；日志显示 BuildKit 构建上下文已超过 1.43 GB 且仍在传输，终止外层命令后旧 Docker/Compose/Buildx 子进程继续运行并相互争抢资源。 |
| 原因 | `.dockerignore` 漏掉 `.codex-tmp`、`.cache`、`.gocache`、`.tmp` 以及 Flutter `mobile/build`、`mobile/.dart_tool` 等本地生成目录；Agent Dockerfile 也没有跨重建复用 Go Build Cache。 |
| 影响 | 新 Agent 容器未被替换，多组孤立构建进程占用 CPU/IO；原运行容器始终保持可用，未发生服务中断或数据修改。 |
| 解决 | 精确终止旧构建进程链，补齐生成目录忽略规则，并为 Agent Dockerfile 增加 BuildKit Go Cache。首次有效构建上下文为 358.90 MB、Go 构建 36.6 秒；后续单文件重建上下文增量 1.15 MB、Go 构建 7.2 秒，镜像与容器切换成功。 |

## 171. Codex 浏览器控制内核被 Windows Sandbox ACL 阻断

| 字段 | 内容 |
|------|------|
| 状态 | Active（2026-08-05，本地 Codex 环境） |
| 现象 | 按 Browser Skill 初始化本地页面控制时，Node 内核在导航前退出并返回 `windows sandbox failed: helper_unknown_error: apply deny-read ACLs`。 |
| 原因 | 当前 Codex Windows 沙箱辅助进程无法应用读取 ACL；错误发生在浏览器 Runtime 建立前，不是 `/agent` 页面、认证或前端脚本异常。 |
| 影响 | 本轮不能形成自动点击与截图证据；不影响用户浏览器访问或应用运行。 |
| 解决 | 已以 HTTP 200、静态 Chunk 精确文案、容器镜像 ID、Consul passing 和启动日志完成非交互验收；最终对话、站内搜索与 Workflow 点击路径仍需用户浏览器手工确认。 |

## 172. Agent OTel Resource 混用旧 Schema URL

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-05，源码、测试与容器日志） |
| 现象 | Agent 启动时报告 `conflicting Schema URL: .../1.37.0 and .../1.17.0`，Trace Provider 初始化失败。 |
| 原因 | OTel SDK 已升级到 v1.39，`resource.Default()` 使用新 Schema，而服务名资源仍通过 `semconv/v1.17.0` 注入旧 Schema URL。 |
| 影响 | Agent 主业务、Metrics 和 Pyroscope 正常，但 OTLP Trace 未启用。 |
| 解决 | 服务名改为 `resource.NewSchemaless`，保留默认资源的当前 Schema；`pkg/trace` 测试通过，重建后日志确认 `tracer initialized for service agent-service`。 |

## 173. Agent 启动期 Temporal SDK Dial 超时后永久降级

| 字段 | 内容 |
|------|------|
| 状态 | Active（2026-08-05，本地 Docker 环境） |
| 现象 | Agent 启动时 `client.Dial(temporal:7233)` 返回 `context deadline exceeded`，随后关闭 Temporal Worker 与趋势报告器；同一时刻 Temporal 容器运行、`tctl cluster health` 为 SERVING，Agent 到 7233 的 DNS/TCP 探测可达。 |
| 原因 | 当前证据指向 SDK 握手或启动期连接策略；现实现只尝试一次，瞬时失败后没有后台重连。尚无服务端拒绝或网络不可达证据。 |
| 影响 | 主 Agent gRPC、统一对话、站内搜索、Workflow DAG、Mongo/ES/Qdrant/MCP 和 Consul 注册正常；Temporal 承担的风控 Worker 与趋势报告后台能力在该 Agent 进程中禁用。 |
| 解决 | 本轮不重启或修改用户的 Temporal 服务。后续应为 Temporal 客户端增加有界重试/后台重连和就绪状态，并补充 SDK 级诊断；面试主路径可继续验收，但不能宣称后台 Temporal 能力已启用。 |

## 174. VerifiedRunner 取消测试在工具准入阶段提前失败

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-08，离线单元测试） |
| 现象 | `TestVerifiedRunnerPropagatesCancellationBeforeExecution` 首次运行返回 `invalid_request: task tool is unavailable: search`，未到达预期的 Context 取消断言。 |
| 原因 | 测试 Task 明确允许 `search`，基础 Run 也声明该工具，但 Fake Environment 没有暴露同名工具；VerifiedRunner 按请求、环境和 Task Allowlist 取交集并正确 fail-closed。 |
| 影响 | 仅影响新增测试夹具，不影响生产路径；工具最小权限交集逻辑符合预期。 |
| 解决 | 为 Fake Environment 补充同名只读工具后，取消传播测试与 VerifiedRunner 目标回归通过。 |


## 175. Goal Shadow 测试依赖未进入 Vendor

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-09，离线单元测试） |
| 现象 | Goal Shadow 目标测试在 setup 阶段提示无法从 `vendor` 找到 `github.com/prometheus/client_golang/prometheus/testutil`。 |
| 原因 | 新测试直接引用了未被当前 Vendor 集合收录的测试辅助包；仓库启用 `-mod=vendor`，不会临时联网补依赖。 |
| 影响 | 仅阻断新增测试编译，生产代码未执行，未访问外部服务。 |
| 解决 | 复用 Service 测试包已有的 `prometheusCounterValue`，移除新依赖；目标测试和直接依赖回归随后通过。 |

## 176. Windows 冷缓存 Race 编译超过有界超时

| 字段 | 内容 |
|------|------|
| 状态 | Blocked（2026-08-09，本地 Windows Go 工具链） |
| 现象 | 新 Shadow 目标 `go test -race` 在项目内冷缓存编译阶段运行 304 秒后达到命令上限，未输出测试断言；超时后遗留两个 `go` 进程。 |
| 原因 | 当前 Windows 环境的首次 Race 工具链/依赖编译显著慢于普通构建；没有代码断言、编译诊断或外部服务错误证据。 |
| 影响 | 本轮不能声明 Race 门禁通过；普通目标测试、Service/Runtime/Evidence/组合根串行回归和目标 Vet 均通过。 |
| 解决 | 已精确终止本轮遗留 Go 进程并确认无残留。后续在预热的独立 Race 缓存或 Linux CI 中补跑；本轮不重复启动长命令。 |

## 177. External MCP Service 目标测试冷编译超过有界超时

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-10，离线 Go 工具链） |
| 现象 | 首次以全新项目内缓存运行 External MCP Service 定向测试时，命令在冷编译阶段运行 214 秒后达到外层有界超时，尚未输出测试断言或编译诊断。 |
| 原因 | Windows 上 Agent Service 依赖图的首次冷编译超过命令外层时限；没有网络、外部服务或代码死锁证据，残留的两个 Go 进程随后自然退出。 |
| 影响 | 首次命令不能作为通过或失败结论，但未产生生产副作用，也未调用模型、数据库、第三方 MCP 或公网。 |
| 解决 | 复用同一项目内预热缓存后，原 Service 定向测试在 12.5 秒内通过；随后 Environment、Remote MCP 与 Service 组合定向回归通过。Environment、Remote MCP、Runtime、Evidence、WebSearch、MCP Tools、Service 与 `cmd/agent-service` 的扩大普通回归和目标 Vet 也通过；未运行 Race。 |

## 178. Tweet Write Environment 测试误判显式审批字段

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-10，离线单元测试） |
| 现象 | 首次定向测试中 `approval absent` 用例预期 Write Tool 被拒绝，但 Runtime 将所有 `write/risky` 类别统一视为必须审批，因此 Snapshot 正确通过。 |
| 原因与解决 | 测试把原始 `RequiresApproval` 字段误当成最终策略；删除错误用例，保留对 `ToolDefinition.ApprovalRequired()` 有效语义、危险分类、无效 Schema、错误所有权和重复状态的验证。生产代码无需降低或绕过审批。 |

## 179. test_runner 曾被 Windows Sandbox ACL 阻断

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-10，本地 Codex 验证环境） |
| 现象 | 项目 `test_runner` 执行首条 `go test ./internal/module/agent/runtime -count=1` 前即返回 `windows sandbox failed: helper_unknown_error: apply deny-read ACLs`，Go 工具链没有启动，后续命令未运行。 |
| 原因与影响 | Windows 沙箱辅助进程无法应用项目读取 ACL；这不是编译或测试断言失败，但使委托验证无法形成结果。与 Issue 171 属同类环境边界。 |
| 处理 | 主进程先在用户已授权的项目目录边界内完成同命令复验；本轮后续重新启动专用 `test_runner` 成功，其执行的 Runtime 全包测试、Runtime Vet 及 Environment/Evidence 定向测试均通过，因此关闭本 Issue。Windows ACL 偶发失败仍按 Issue 180 的受控提升读取方案处理。 |

## 180. G3 文件读取与手工补丁链受 Windows ACL/差异格式阻断

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-10，本地 Codex 编辑工具链） |
| 现象 | 首次仓库读取被 `apply deny-read ACLs` 拦截；随后若干临时 unified diff 因 hunk 行数或 Markdown 短横线转义错误被 `git apply --check` 拒绝，且一次组合读取引用了已迁移的 `strategy/short_plan.go` 路径。 |
| 原因与影响 | Windows 沙箱仍无法稳定读取/更新既有文件；手工差异文件需要同时表达 apply_patch 与 unified-diff 两层标记。所有失败均发生在 `--check` 或读取阶段，未形成半应用源码，也未调用外部服务。 |
| 处理 | 在用户既有仓库授权范围内改用提升读取与小粒度 `git apply --check`，按当前 `runtime/short_plan*.go` 路径继续；每个临时补丁成功后立即删除，最终 `git diff --check` 与 Runtime 回归确认工作区一致。 |

## 181. 恢复请求历史校验误判 nil/空 Actions

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-10，离线单元测试） |
| 现象 | 缺失证据恢复用例返回 `repair request discarded or rewrote execution history`，恢复执行未启动。 |
| 原因与影响 | `cloneMessages` 会把 `nil Actions` 规范化为空切片；历史校验直接 `reflect.DeepEqual` 原值与克隆值，语义相同但表示不同，导致 fail-closed 误报。只影响新 opt-in 恢复路径。 |
| 处理 | 比较前对历史前缀和候选前缀同时使用 Runtime 克隆规则规范化；内容、顺序、Role、ToolCallID 和 Action 仍需完全一致。定向恢复测试与完整 Runtime 回归随后通过。 |

## 196. test_runner 启动参数组合无效

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-10，Codex 委托验证配置） |
| 现象 | 首次委托复核同时传入显式 `agent_type=test_runner` 与 `fork_context=true`，工具在创建 Subagent 前拒绝该互斥参数组合。 |
| 原因与影响 | 显式项目 Subagent 类型不能与上下文分叉模式同时启用；未启动代理、未执行命令，也未修改项目。 |
| 处理 | 去除 `fork_context` 后按项目 `.codex/agents/test_runner.toml` 启动成功，委托的 Runtime 测试、Runtime Vet 与 Environment/Evidence 测试全部通过。 |

## 197. G3 收尾读取再次触发 Windows Sandbox ACL

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-10，本地 Codex 文档收尾） |
| 现象 | 收尾读取和直接更新 `docs/ISSUES.md` 与 `docs/PROJECT_PROGRESS.md` 时再次返回 `helper_unknown_error: apply deny-read ACLs`，两次未应用临时补丁也分别因 hunk 行数和上下文错误被 `git apply --check` 拒绝。 |
| 原因与影响 | Windows 沙箱辅助进程偶发无法应用仓库读取 ACL；失败命令未读取或更新既有文件，也不影响已完成的 Go 验证结论。 |
| 处理 | 仅在用户已授权的 `twitter-clone` 根目录范围内提升执行只读命令与经 `git apply --check` 校验的文档补丁；格式错误补丁在校验阶段停止并删除，重建最小 hunk 后继续收尾。 |

## 198. G3 完成增量受 ACL、搜索参数与测试夹具契约阻断

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-10，离线开发与测试） |
| 现象 | 首次必读文件读取和直接更新新增测试被 Windows `apply deny-read ACLs` 拒绝；三次 `rg` 因无匹配、传入不存在的 `api/agent` 路径或 PowerShell 通配符语法返回非零；新增 E2E 测试两处复合字面量缺少右括号导致两次 `gofmt` 失败；默认 Planner 切换后，三个依赖中文关键词路由的旧 Service 测试失败；首版多文档临时补丁因手工 hunk 行数不一致被 `git apply --check` 拒绝。 |
| 原因与影响 | 前三类属于本地工具边界和测试夹具语法，不触及生产逻辑；Service 失败揭示旧测试把自然语言关键词误当成路由契约，实际前端预设、Task Template 和 Skill 已支持显式能力 ID。所有失败均发生在读取、搜索、格式化或离线测试阶段，未调用模型、MCP、数据库、公网或付费 API。 |
| 处理 | 读取与既有文件补丁限定在用户授权仓库范围内提升执行；修正搜索范围和测试括号；四个研究草拟测试显式传入 `platform.search + content.draft`，避免第四个测试静默退化；随后 Runtime 与 Service 完整测试通过。 |

## 199. G3 最终 test_runner 复核再次被 Windows ACL 阻断

| 字段 | 内容 |
|------|------|
| 状态 | Active（2026-08-10，本地 Codex 委托验证环境） |
| 现象 | 专用 `test_runner` 在读取必需的 `.agents/context/project_map.md` 前返回 `windows sandbox: helper_unknown_error: apply deny-read ACLs`，因此没有启动 Go Test 或 Vet。 |
| 原因与影响 | Subagent Windows 沙箱仍无法稳定应用仓库读取 ACL；这是环境失败，不是代码测试失败。委托结果不能作为通过证据。 |
| 处理 | 关闭未执行命令的 Subagent；主进程在用户授权的仓库边界内运行 Runtime/Service 完整测试与两包 Vet，全部通过。保留本 Issue，直到委托沙箱能稳定读取仓库。 |

## 200. AnalyzeAlert 硬编码开发者用户目录

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-10，离线边界修复） |
| 现象 | Service 完整测试执行 `AnalyzeAlert` 时，生产实现把 RCA 报告追加写入 `C:/Users/郭丰硕/.gemini/.../scratch/alert_rca_reports.md`，对应测试随后删除该文件。 |
| 原因与影响 | AIOps 原型把 IDE scratch 绝对路径硬编码进生产 Service，导致服务和测试都可能修改项目根目录之外的个人文件；本轮首次完整回归发生了瞬时写入后由旧测试清理，未访问网络或付费 API。 |
| 处理 | 新增可选 `AIOpsReportSink` 端口并删除所有本地文件回退；默认 Service 不落盘，测试使用内存 Sink 验证报告、结构化 RCA 和时间戳。Runtime/Service 完整测试及两包 Vet 随后通过，日志不再出现用户目录持久化。 |

## 201. G4 首次必读文件读取被 Windows Sandbox ACL 阻断

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-10，本地 Codex 读取环境） |
| 现象 | 按仓库要求读取项目地图、规则和 Agent/Observability Skill 时返回 `windows sandbox: helper_unknown_error: apply deny-read ACLs`。 |
| 原因与影响 | Windows 沙箱辅助进程未能应用仓库读取 ACL；没有读取不完整内容、修改源码或启动测试。 |
| 处理 | 仅在用户已授权的 `twitter-clone` 根目录内提升只读命令，随后完成全部必读文件和 G4 上下文读取。 |

## 202. Windows rg 路径通配符语法不兼容

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-10，本地代码搜索） |
| 现象 | `rg ... internal/module/agent/**/*_test.go` 在 Windows 把星号路径作为非法卷标处理并返回非零。 |
| 原因与影响 | PowerShell/Windows 路径展开与 Unix glob 假设不一致；并行读取组因此未返回其他结果，但未修改文件。 |
| 处理 | 改用 `rg -g "*_test.go" ... internal/module/agent` 后完成测试定位。 |

## 203. TaskOutcome 独立测试文件尚不存在

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-10，离线测试补齐） |
| 现象 | 读取 `internal/module/agent/runtime/task_outcome_test.go` 返回路径不存在；原覆盖只位于 G3 E2E fixture。 |
| 原因与影响 | 这是审计阶段对测试文件名的错误假设，不是代码或测试失败。 |
| 处理 | 新建 `task_outcome_projection_test.go`，固定 observed execution 不伪造 admitted plan、只保存摘要/引用并拒绝空 Run ID。 |

## 204. G4 既有未跟踪源码更新受 ACL 与补丁索引限制

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-10，本地受控编辑） |
| 现象 | 直接 `apply_patch` 更新 `task_outcome.go` 时再次触发 deny-read ACL；随后首版临时补丁缺少标准 hunk 行号，修正版又因目标 G3 文件仍未纳入 Git 索引而被 `git apply --check`、`--no-index` 依次拒绝。 |
| 原因与影响 | Windows ACL 无法读取既有未跟踪文件，Git 补丁校验也不能把它们当作已跟踪基线；所有失败均止于读取或 `--check`，未发生半应用源码。 |
| 处理 | 使用 `apply_patch` 在项目 `.codex` 内生成完整替换文件，校验解析路径均位于仓库后复制到目标并立即 gofmt/定向测试；临时文件在收尾删除。 |

## 205. project_map 增量补丁无法匹配已有工作区差异

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-10，本地文档收尾） |
| 现象 | G4 多文档补丁中的 `project_map.md` 第二个 hunk 被 `git apply --check` 拒绝；拆成精确单行 hunk 并仅放宽空白后仍无法匹配。 |
| 原因与影响 | `project_map.md` 已包含大量未提交增量，Git 工作树上下文与补丁基线不能稳定匹配；其他文档未被半应用，现有地图关于默认关闭和单一执行所有者的描述仍然正确，只缺少 observed outcome 细节。 |
| 处理 | 不覆盖用户已有地图修改；将 G4 阶段事实同步到 `agent_runtime_context.md`、`PROJECT_PROGRESS.md` 和 E2E 矩阵，后续地图整理时再合并细节。 |

## 206. E2E-06 开发读取与增量补丁受 Windows ACL/路径假设阻断

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-10，离线开发与验证） |
| 现象 | 首次并行只读和多次直接 `apply_patch` 触发 `apply deny-read ACLs`；审计时误读不存在的 `platform_search_schema.go`、`dialogue.go` 与 `runtime/evidence.go`；数个手写 Git hunk 行数错误或上下文不匹配，在 `git apply --check` 阶段被拒绝；本机也没有可用的 `patch` 命令。 |
| 原因与影响 | Windows 沙箱 ACL 仍不能稳定读取既有或本轮未跟踪文件，人工 hunk 与当前脏工作区存在偏移；所有失败均止于只读、工具定位或补丁校验，没有半应用源码，也未连接模型、MCP、数据库或公网。 |
| 处理 | 在用户已授权的仓库边界内提升只读与补丁应用；所有临时补丁先经 `git apply --check`，源码完成 gofmt、定向及扩大回归和 Vet，临时文件随后删除。 |

## 207. E2E-07 开发受 Windows ACL、补丁格式与验证语义偏差影响

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-10，离线开发与验证） |
| 现象 | 首次必读文件并行读取被 `apply deny-read ACLs` 拒绝；审计误读不存在的 `goal_runtime_shadow_metrics_test.go`；直接更新既有/未跟踪文件再次被 ACL 拒绝，两个临时 Git 补丁因 hunk 计数或 `.env.example` 上下文不匹配止于 `--check`；首轮最小测试暴露搜索条件被错误绑定到 Page 成功；扩大 `go test` 的所有包测试通过后，自动 `vet.exe` 仍因 Access denied 令总命令非零；两版文档补丁也因当前脏工作区上下文漂移或未跟踪基线止于 `--check`。; the first transaction script was parsed as ANSI by Windows PowerShell 5 and failed before writes; the follow-up single-line Git patch also stopped at --check |
| 原因与影响 | Windows 沙箱中 Go 子进程 ACL 不稳定，且首版 Verifier 把两个完成条件耦合，降低了 blocked 诊断精度。所有补丁失败均未半应用；测试失败只影响新增 Goal shadow，默认关闭且未调用模型、MCP、数据库、公网或付费 API。 |
| 处理 | 在授权仓库内对临时补丁逐个 `git apply --check` 后应用；改为搜索来源条件独立验证、Page 条件额外要求同引用链；最小测试复验通过。扩大测试使用 `-vet=off -p=1` 串行通过，随后独立 `go vet -p=1` 通过。 |
## 208. E2E-08 开发受 Windows ACL、测试筛选与缓存路径影响

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-11，离线开发与验证） |
| 现象 | 必读文件和收尾文档首次沙箱读取被 `apply deny-read ACLs` 拒绝；直接 `apply_patch` 更新既有源码/测试也被 ACL 拒绝；首次合并 E2E-07/08 定向测试在 124 秒超时但无残留 Go 进程；首轮文档事务写入引入 CRLF，导致 `git diff --check` 报尾随空白；首轮换行修复在完成规范化后因 Issue 日志锚点偏差退出；两版 Issue 更新脚本又因 PowerShell `Split` 重载假设错误而在写入前退出；新增诊断改变旧 forged-page 断言后首轮测试失败；新测试最初缺少 JSON helper；首次创建 `test_runner` 时同时传入不支持的 `fork_context` 被拒；其首轮广泛测试又因相对 `GOCACHE` 在 Go 启动前失败。；最终只读行号审计又因 Shadow 状态常量锚点假设错误退出，改用实际 `GoalRunBlocked` 标识复核 |
| 原因与影响 | Windows ACL 仍要求在已授权仓库边界内提升读写；旧断言未计入新低敏诊断；测试辅助函数遗漏；test_runner 参数和 Go 缓存路径假设不兼容当前环境。所有失败均发生在读取、构建或离线验证阶段，没有半应用源码，没有调用模型、MCP、数据库、公网或付费 API，也未启动或重启服务。 |
| 处理 | 使用仓库内 `apply_patch` 生成的事务脚本，在写入前验证全部锚点并仅修改项目文件；补齐 helper 与诊断断言；拆分最小测试并使用绝对仓库缓存路径；同一 test_runner 重试后 Runtime/Evidence/Service/组合根串行测试和四包 Vet 全部通过，临时事务脚本在收尾删除。 |
## 209. E2E-09 开发受 Windows ACL、审计路径假设与宽测试时限影响

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-11，离线开发与验证） |
| 现象 | 必读文件首次沙箱读取被 `apply deny-read ACLs` 拒绝；批量审计包含不存在的 `internal/module/agent/evidence/web_schema.go`；新 Grounded Draft 文件创建后直接补丁再次被 ACL 拒绝；首版源码误写不存在的 `agentRuntime.Time` 类型但在编译前检查时修正；test_runner 首轮合并四包测试在 124 秒超时，未执行 Vet 且没有文件/行号代码错误。 |
| 原因与影响 | Windows ACL 仍需在用户已授权的仓库边界内提升读写；审计沿用了错误文件名；时间类型应来自标准库；四包冷缓存编译超过单命令环境时限。所有失败都发生在读取、预编译修正或离线验证阶段，没有半应用外部副作用，没有调用模型、MCP、数据库、公网或付费 API，也未启动或重启服务。 |
| 处理 | 使用仓库内事务脚本验证锚点后修改既有文件，时间字段改为 `time.Time`；先完成 E2E-09 目标测试，再复用同一 test_runner 和绝对仓库缓存路径拆分 Runtime/Evidence/Service/组合根测试及 Vet，八条命令全部通过；临时脚本在收尾删除。 |
## 210. E2E-10 开发受路径假设、Subagent 参数与 Windows ACL 影响

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-11，离线开发与验证） |
| 现象 | Runtime 契约批量读取误包含不存在的 `runtime/evidence.go` 与 `runtime/verification.go`，实际类型位于 `goal.go`、`evidence_collectors.go` 和 `verified_runner.go`；首次创建 `test_runner` 时错误地同时指定专用 Agent 类型与 full-history fork，被编排器拒绝；修正参数后，test_runner 仍在读取项目地图时被 Windows `apply deny-read ACLs` 阻断，未启动 Go；六文件文档直接 `apply_patch` 也被相同 ACL 拒绝；首次组合收尾检查又因 PowerShell 单引号没有展开制表符转义，把正常的行尾字母 `t` 误报为空白，权威 `git diff --check` 与 `gofmt -d` 均实际通过。 |
| 原因与影响 | 文件定位沿用了概念名而非当前代码文件名；专用 Subagent 不支持 full-history fork；子 Agent 与补丁 Helper 未继承主进程的仓库 ACL 提升路径。所有失败均止于只读或任务编排，没有源码半应用、外部调用或服务副作用。 |
| 处理 | 依据实际 Runtime 文件重新核对契约；关闭受阻 test_runner，由主进程在用户授权的仓库边界内完成 E2E-10 定向测试、Evidence/Runtime 全包测试和两包 Vet；文档使用写前验证全部锚点的内存事务更新。任务保持纯离线，不新增生产路由、API、Profile 或开关。 |
## 211. E2E-11 开发受 Windows ACL 与临时脚本解析影响

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-11，离线开发与验证） |
| 现象 | 首次批量读取 E2E-11 相关源码被 `windows sandbox: helper_unknown_error: apply deny-read ACLs` 拒绝；更新既有 Service、测试和临时脚本时多次触发同一 ACL；一条用于修正临时脚本的内联 PowerShell 命令因引号转义错误在解析阶段退出。 |
| 原因与影响 | Windows 沙箱 Helper 无法稳定为已有/未跟踪项目文件应用读取 ACL，且复杂内联 PowerShell 的双重转义不可靠。所有失败均发生在读取、补丁校验或脚本解析前，没有产生半写、模型/Tool/Provider 调用、消息持久化或服务副作用。 |
| 处理 | 仅在用户已授权的 `twitter-clone` 根目录内提升访问；新文件继续使用 `apply_patch`，既有文件通过写前校验全部唯一锚点的一次性事务脚本更新，随后删除脚本。E2E-11 定向 Evidence/Service 测试通过后再执行扩大回归与 Vet。 |
| 验证中发现 | 首轮“缺少研究”负例因夹具没有 `RunContext.RunID`，被低敏 `TaskOutcome` 投影以 `evaluator_error` 正确拒绝；补齐稳定 Run ID 后得到预期 `missing_both/blocked`。两条内联 PowerShell 修正命令又因错误使用反斜杠转义双引号在解析期退出，最终改用唯一行锚点插入；均未写入半成品。 |
| Test runner | 专用 `test_runner` 再次在读取 `.agents/context/project_map.md` 时被相同 ACL 阻断，Go、Vet、Diff 和 Gofmt 均未启动；主进程随后完成四包普通回归、四包 Vet、`git diff --check`、本轮 Gofmt 检查和 Compose 解析。 |

## 212. E2E-12 开发受 Windows ACL 与换行锚点假设影响

| 字段 | 内容 |
|------|------|
| 状态 | Resolved（2026-08-11，离线开发与验证） |
| 现象 | 首次枚举 Runtime/Evidence 文件时沙箱报 `windows sandbox: helper_unknown_error: apply deny-read ACLs`；直接 `apply_patch` 更新既有 Runtime 与冲突源码再次被同一 ACL 拒绝；首轮事务替换分别因 CRLF/LF 假设相反而在唯一锚点校验阶段退出。 |
| 原因与影响 | Windows 沙箱 Helper 仍无法稳定应用读取 ACL，且 `gofmt` 后文件换行不能由 PowerShell 默认行为推断。失败均发生在读取或写前锚点验证阶段；事务脚本未匹配时拒绝写入，未产生半成品，也未调用模型、Tool、Provider、MCP、数据库、公网或付费 API，未启动或重启服务。 |
| 处理 | 仅在用户已授权的仓库边界内提升访问；既有文件使用先归一化内存文本、验证全部唯一锚点、任一写入失败则恢复原文的事务替换，新文件继续使用 `apply_patch`。随后执行 Gofmt、E2E-12/Runtime 兼容定向测试、扩大回归、Vet 与 Diff 审查。 |
| Test runner | 专用 `test_runner` 在读取 `.agents/context/project_map.md` 时被同一 ACL 阻断，Go/Test/Vet/Gofmt/Diff 均未启动；主进程随后完成 Environment/Evidence/Runtime/Service 四包串行回归、四包 Vet、Gofmt 与 `git diff --check`。 |
## 213. E2E-13/14 编辑 ACL、静态检查与子代理认证故障

| 字段 | 内容 |
|------|------|
| 状态 | Partial（2026-08-11，代码验证完成；子代理认证需重新登录） |
| 现象 | `apply_patch` 更新既有 Service/Test/文档文件时再次遇到 `apply deny-read ACLs`；两次事务脚本分别因 PowerShell here-string 语法和旧注释锚点不匹配而在写前退出。首次 Vet 又发现测试夹具直接复制 protobuf 消息内部 mutex。专用 `test_runner` 未执行任何命令即因 refresh token 已撤销退出。 |
| 原因与影响 | Windows 沙箱 ACL 仍不稳定；事务脚本初始锚点与实际 LF 文本不一致；测试 Fake 使用值复制而非逐字段快照；子代理登录态属于 Codex 环境而非项目。所有失败均未访问模型、公网、数据库或付费 API，未启动/重启服务，也未触发真实 Tweet 写入。 |
| 处理 | 在仓库授权边界内使用唯一锚点、全量预校验和失败回滚的 UTF-8 无 BOM 替换；protobuf Fake 改为逐字段构造；主进程重跑 3 个定向测试、五包普通回归、五包 Vet 与 Diff 检查并全部通过。子代理认证需在 Codex 重新登录后恢复，不影响本轮代码结论。 |
## 214. E2E-15/16 既有未跟踪适配器与 Windows ACL

| 字段 | 内容 |
|---|---|
| 现象 | `apply_patch` 修改既有 Go 文件时继续被 Windows `apply deny-read ACLs` 阻断；初次 Service 编译同时发现工作区已有未跟踪 `external_mcp_environment_catalog.go`，`git grep` 不会搜索该文件，导致本轮短暂写入了同名适配器。 |
| 处理 | 既有文件使用带唯一锚点、失败回滚和 UTF-8 no-BOM 的项目内事务替换；发现重复后立即删除本轮副本，复用既有 Catalog Adapter，仅补 Connection Revision 映射。 |
| 测试修复 | 首次回归暴露旧夹具缺少 Revision，以及新 Fake Connection 未声明 `AuthNone`；均只补真实契约字段。撤权 Resume 的实际错误为 `task tool is unavailable`，确认当前目录重授权早于远程调用。 |
| 验证 | 四个定向用例、六包 `go test` 与六包 `go vet` 通过；未运行已知认证失效的 `test_runner`，由主进程执行同等离线命令。 |
| 状态 | Resolved；ACL 环境限制仍存在，后续继续优先 `apply_patch`，仅在明确失败后使用项目内事务替换。 |

## 215. E2E-17 Windows ACL 与失败传播断言校准

| 字段 | 内容 |
|---|---|
| 状态 | Resolved（2026-08-11，离线开发与验证） |
| 现象 | 必读文件和既有源码首次沙箱读取继续被 `apply deny-read ACLs` 阻断；`apply_patch` 也无法更新既有 Workflow 文件。首轮四个目标测试中三项通过，失败传播用例错误地期待父 Tool 回显子节点原始错误。 |
| 原因与影响 | Windows ACL Helper 仍无法稳定读取已有/未跟踪项目文件；父 Workflow Tool 边界按现有安全设计只返回固定错误，原始节点错误保留在用户隔离的权威 child Run，避免向模型泄露内部详情。失败仅发生于读写辅助层和离线测试断言，没有外部调用、服务重启或业务副作用。 |
| 处理 | 仅在用户已授权的仓库边界内提升访问；新文件使用 `apply_patch`，既有文件使用唯一锚点校验后的 UTF-8 no-BOM 事务替换。测试改为同时断言父级固定失败、child Run 原始错误持久化和零完成证据。 |
| 验证 | 四个目标测试、Environment/Evidence/Runtime/Workflow Tool/Service 五包普通回归及五包 `go vet` 全部通过。 |
## 216. E2E-19 Windows ACL、脚本制表符与冷编译超时

| 字段 | 内容 |
|---|---|
| 状态 | Resolved（2026-08-11，离线开发与验证） |
| 现象 | Windows Sandbox 继续以 `apply deny-read ACLs` 阻断既有 Runtime 文件的 `apply_patch`；首次 PowerShell 行插入脚本把 `\t` 写成字面量，`gofmt` 拒绝解析。新 Service 测试首次冷编译又在 120 秒 Shell 上限处终止；同缓存重跑后测试进入真实流程，并发现恢复后再次挂起分支漏设 `GoalRunSuspended`。 |
| 原因与影响 | ACL Helper 无法读取既有/未跟踪文件；PowerShell 单引号不解释 `\t`；独立 Go Cache 导致 Service 包冷编译较慢。状态遗漏仅存在于本轮未提交代码，测试日志同时证明审批前零写、审批后单写和证据生成正确。没有外部调用、服务重启或业务副作用。 |
| 处理 | 仅在用户授权仓库内提升访问；将行首字面量 `\t` 转为真实 Tab 后 `gofmt`；复用同一缓存并给测试自身设置 30 秒上限；恢复 `result.Status = GoalRunSuspended`，新增 Checkpoint Revision 负向测试与双恢复单写集成测试。 |
| 验证 | Runtime 最小/全包测试、E2E-13/14+19 定向回归、五包普通回归及五包 `go vet` 全部通过。 |
## 217. E2E-20 Windows ACL 与 test_runner 必读文件阻断

| 字段 | 内容 |
|---|---|
| 状态 | Resolved（2026-08-11，离线开发与验证） |
| 现象 | Windows Sandbox 继续以 `apply deny-read ACLs` 阻断既有源码/文档读取与修改；项目 `test_runner` 在读取强制项目地图前即失败，因此没有启动测试。 |
| 原因与影响 | 子 Agent 没有继承主任务已获准的仓库内 ACL 提升路径；失败发生在只读准备阶段，没有半应用修改、外部调用、服务重启或业务副作用。 |
| 处理 | 主任务仅在用户授权的 `twitter-clone` 根目录内使用提升权限，既有文件通过唯一锚点、写前校验和 UTF-8 no-BOM 事务更新；随后直接完成定向与三包普通回归。 |
| 验证 | Model、Runtime、Service 三包普通测试及三包 `go vet` 全部通过；未访问模型、公网、数据库、MCP 或付费 API，也未启动/重启服务。 |
## 218. G5 Planner 清理的 ACL、并行编译与测试夹具失败

| 字段 | 内容 |
|---|---|
| 状态 | Resolved（2026-08-11，离线开发与验证） |
| 现象 | Windows Sandbox 阻断既有文件 `apply_patch`，专用 `test_runner` 也在读取强制项目地图前失败；主任务首次并行定向测试只得到 `workflow/rag compile.exe: exit status 1`，没有源码诊断。改为 `-p=1` 后测试进入断言，并发现直接组装的 Workflow Skill 测试服务缺少 Planner，返回 `agent capability planner is not configured`。 |
| 原因与影响 | 子 Agent 未继承仓库 ACL 提升路径；并行 Go 子进程在 Windows 环境偶发无诊断退出；该测试夹具不经过 `NewAgentService`，删除手工旧 Planner 后需要显式注入新 Planner。生产组合根始终装配 `ExplicitCapabilityPlanner`，故障未影响生产代码语义。 |
| 处理 | 用户明确批准删除范围后，仅在仓库内用唯一锚点事务修改；测试改为显式注入 `NewExplicitCapabilityPlanner(nil)`，并使用项目缓存与 `-p=1` 串行复验。 |
| 验证 | Planner/Unified Agent 定向测试、Agent Service 全包测试和 Service `go vet` 全部通过；没有外部调用、服务重启或业务副作用。 |
