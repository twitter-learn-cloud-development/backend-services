# Part 3: 分布式状态机编排与 AIOps 混沌网格自愈 (Day 16 - Day 20)

---

## 🗓️ Day 16: AIOps 持续性能剖析与配置自适应智能热调优 (Profiling & Tuning)

### 🎯 学习目标与安排
- **文件范围**：[profiling_analyzer.go](file:///e:/GOProject/云原生/twitter-clone/internal/module/agent/service/profiling_analyzer.go) (火焰图热点提取), [agent_service.go](file:///e:/GOProject/云原生/twitter-clone/internal/module/agent/service/agent_service.go) (调优核心逻辑), [dynamic_config.go](file:///e:/GOProject/云原生/twitter-clone/pkg/config/dynamic_config.go) (无锁热重载)
- **核心目标**：吃透在大规模分布式系统中，如何将大模型 RCA 诊断与多级缓存热重载（TuneCacheConfig）打通为全自动自适应闭环。

### 🛣️ 请求链路全景 (Request Flow)
1. 监控告警：网关因 QPS 突增或 CPU 负载过载产生告警。
2. 告警派发：Webhook 将告警及上下文 error 日志派发至网关自愈组件，调用 `AnalyzeAlert`。
3. 火焰图抓取：`ProfilingAnalyzer` 在后台发起对 Pyroscope（持续性能分析器）的 HTTP 请求，抓取并过滤出当前微服务占用 CPU 排名前 5 的本级及下游微服务耗时热点堆栈简报。
4. LLM 脑诊断：大模型综合告警、Loki 错误日志和火焰图，发现瓶颈是缓存穿透或排序函数高负载，诊断为 `TimelineCacheCPUOverload`。
5. 下发自愈指令：大模型下发包含 `TuneCacheConfig` 自愈行为的 JSON 指令，指定 L1/L2 TTL 以及异步翻页预加载深度。
6. 分布式冷却锁（3分钟 Cooldown）：自愈接收器在 Redis 中抢占分布式冷却排他锁，防止 AI 连续调优引起参数剧烈震荡。
7. 参数重载与广播：校验数值是否在安全护栏（Guardrails）范围内，成功后持久化至 Redis KV，并向 Redis Channel 广播配置更新。所有运行的 Pod 监听到事件后，通过 `atomic.Pointer` 零锁原子替换，实现参数零停机热更新。

### 🔑 核心重点 (Key Focus)
- **动态配置无锁并发安全**：深入看 `dynamic_config.go` 内部是如何利用 `atomic.Pointer` 来存储配置的。为什么在读多写极少的场景下这比读写锁（RWMutex）更具伸缩性？
- **防 Flapping 冷却抢占**：看 Redis `SetNX` 设置带 TTL 锁的实现，这是高弹性自愈系统里防止参数震荡和系统雪崩的核心保护锁。

### ✨ 技术亮点 (Architecture Highlights)
- **AIOps 智能自调优自治**：该设计实现了系统“自我感知性能瓶颈 - 诊断根本原因 - 下发参数指令 - 本地原子热重载生效”的高阶闭环，展示了可观测性火焰图与 AI 参数微调在微服务治理中的最高生产水准。

### ⚠️ 避坑指南 (Gotchas)
- **脑裂与新 Pod 配置丢失**：如果只通过 Redis 广播动态配置，当发生 K8s 动态扩容或 Pod 重启时，新拉起的 Pod 如果只读本地静态配置文件，会导致和现存已调优的 Pod 的参数不一致。必须采取“新 Pod 启动时先去 Redis KV 读取最新持久化参数 + 订阅广播”的双保险设计。

### 🚀 进阶玩法 (Advanced Play)
- **强化学习反馈调优 (RL-Tuning)**：在多次调整参数后，通过收集随后的 Prometheus Metrics（例如 CPU 使用率是否回落，网关 QPS 是否平滑上升），为 AI 建立收益评估模型，自适应探索出最佳的缓存参数配置比率。

### 🛠️ 动手挑战 (Hands-on Challenge)
- **任务**：运行缓存自适应参数演练脚本 `test_adaptive_tuning.py`。
- **验证**：验证在 3 分钟内连续调用两次 `TuneCacheConfig`。第一次应显示更新成功，第二次由于冷却锁保护，应直接返回观察冷却阻断，防范由于大模型幻觉引发的配置震荡。

---

## 🗓️ Day 17: Temporal 分布式 Saga 风控工作流编排与原子 Lua 洗地 (Temporal Saga)

### 🎯 学习目标与安排
- **文件范围**：[workflows.go](file:///e:/GOProject/云原生/twitter-clone/internal/module/agent/service/workflows.go) (工作流编排), [activities.go](file:///e:/GOProject/云原生/twitter-clone/internal/module/agent/service/activities.go) (节点逻辑), [risk_control.go](file:///e:/GOProject/云原生/twitter-clone/internal/module/agent/service/risk_control.go) (自愈清洗器)
- **核心目标**：掌握在复杂分布式微服务架构下，基于 Temporal（Saga）构建具备“非侵入性”、“状态自愈”与“幂等回滚”的分布式事件清洗网络。

### 🛣️ 请求链路全景 (Request Flow)
1. 发布推文：用户发帖成功，触发发件箱向 RabbitMQ 广播消息。
2. 独立消费者捕获事件：`timeline_consumer` 订阅 `tweet.created` 写入普通时间线；Agent Worker 自己拥有的 `queue.tweet.risk` 同时直接订阅同一原始事件，两个消费者互不转发。
3. 工作流启动：`RiskControl` 接收到消息，向 **Temporal Server** 发起 `TweetRiskControlWorkflow`，设置 WorkflowID 为 `RiskControl-Tweet-{TweetID}` 并使用 `REJECT_DUPLICATE`（依靠引擎拒绝重复启动，防止消息重投和滚动升级双链路重复）。
4. 频率与相似度审计：工作流并发调 Activity 提取作者近期发帖频率，调用 Qdrant 检查内容与已知垃圾文本的余弦相似度。
5. 影子封禁（Shadowban）：如果判定为垃圾推文，工作流修改推文属性为 `visible_type=4`（仅自己可见）。
6. 原子洗地回滚（Saga 补偿）：Activity 并发调用 **Redis Lua 脚本**，批量从所有粉丝的 Timeline ZSet 缓存中原子移除该垃圾推文 ID（ZREM）并递减未读数。
7. Ack 确认：全部自愈工作流状态确认持久化后，TimelineConsumer 确认 Ack，防死锁。

### 🔑 核心重点 (Key Focus)
- **非确定性禁忌 (Workflow Determinism)**：深入吃透 Temporal 工作流代码中决不能包含任何 Go 原生协程、随机数、本地时间（如 `time.Now()`）或者原生 HTTP 调用。工作流的所有状态回放都必须由系统托管，任何非确定性操作都会直接引起 Workflow 崩溃。
- **Lua 脚本并发安全性**：理解为何洗地时必须采用 Lua 脚本并在内部加入切片复制防 race 机制。

### ✨ 技术亮点 (Architecture Highlights)
- **Saga 级事务自愈回滚**：当风控判定发生异常或者微服务节点故障导致某步失败时，Saga 引擎能自动执行回滚路径，将受影响的粉丝时间线状态安全复原，并且基于 Workflows 的“持久化执行历史”保证了事务最终一致性。

### ⚠️ 避坑指南 (Gotchas)
- **工作流历史溢出 (History Explode)**：在周期性自愈哨兵（如 TrendingReporter 舆情监控）中，如果长时间运行一个工作流而不调用 `ContinueAsNew` 机制清理历史，事件历史数累加到 50,000 以上会引发 Temporal 系统强行熔断异常。

### 🚀 进阶玩法 (Advanced Play)
- **分布式事务 TCC (Try-Confirm-Cancel) 架构融入**：针对交易或者敏感数据更新，将活动节点拆分为 Try（预留资源）、Confirm（物理提交）与 Cancel（释放资源），以实现比 Saga 更严苛的资源隔离和状态一致性。

### 🛠️ 动手挑战 (Hands-on Challenge)
- **任务**：向系统发一条内容为“spam similarity text”的敏感推文，观察 RabbitMQ 消费和 Temporal Web UI 控制台（:8233）中的执行流。
- **验证**：证实工作流是否启动，并且查看数据库中的推文属性是否变更为影子封禁；同时查看粉丝的 Timeline 缓存中该推文 ID 是否已被原子洗地清除。

---

## 🗓️ Day 18: Sentinel-Go 流量网格熔断限流与静态/动态规则合并 (Sentinel-Go)

### 🎯 学习目标与安排
- **文件范围**：[self_healer.go](file:///e:/GOProject/云原生/twitter-clone/internal/gateway/self_healer.go) (动态熔断接收与合并), [tweet_handler.go](file:///e:/GOProject/云原生/twitter-clone/internal/gateway/handler/tweet_handler.go) (网关熔断挂载点)
- **核心目标**：掌握 Sentinel-Go 在微服务网关层对核心读链路（如 GetFeeds）执行基于“错误率”与“慢调用比率”的主动熔断拦截。

### 🛣️ 请求链路全景 (Request Flow)
1. 大流量涌入：网关转发 `/api/v1/feeds`。
2. 熔断判定（Sentinel Entry）：进入 API 路由代码前，执行 `sentinel.Entry("GET:/api/v1/feeds")`。
3. 状态收集：如果下游 `tweet-service` 发生超时（比如慢调用比率超 40%）或者报错，Sentinel 拦截并记录样本错误。
4. 熔断开启：当触发阈值（错误率 > 50%），网关瞬间熔断该 API，不发送 RPC 给下游，快速向前端返回 429 / 降级数据。
5. 脑自动恢复：待熔断窗口期过去，Sentinel 自动尝试半开状态，放行单请求，检测正常后全量愈合恢复。
6. 自愈自更新：AIOps 告警生成后下发熔断规则，自愈引擎（`self_healer.go`）捕获指令，将动态限流规则与系统静态保底规则合并后热载入 Sentinel。

### 🔑 核心重点 (Key Focus)
- **全量覆盖缺陷与合并**：Sentinel-Go 的 `LoadRules` 采用的是“全量覆写”设计。重点看代码中如何在内存里维护 `baseRules`，并在每次热重载时进行合并后统一 Load，防止动态加载将静态基础规则清空挂空挡。
- **熔断挂载点的上下文注入**：了解如何在 Handler 的 defer 中利用 `sentinel.TraceError` 将下游 gRPC 发生的 RPC 错误类型传递给滑动窗口。

### ✨ 技术亮点 (Architecture Highlights)
- **高可用双级防护网**：网关拥有前置分布式限流（Redis Rate Limiter）过滤非法与高频请求，后置 Sentinel 流量防线隔离下游雪崩。网关白名单机制旁路压测流量，使得系统在遭遇混沌高并发时依然保持高拦截低消耗。

### ⚠️ 避坑指南 (Gotchas)
- **熔断资源越界导致挂起**：若没有对大模型输出的熔断 resource 规则进行 `Allowlist`（允许列表）校验隔离，大模型可能会幻觉下发对 `/health` 或者是网关基础路由的熔断规则，直接导致全站服务不可用及 Pod 重启死亡。

### 🚀 进阶玩法 (Advanced Play)
- **Sentinel 多活自适应热点参数限流 (ParamFlow)**：针对热门博主的 Profile 页面，将博主 `user_id` 注入为热点参数限制，一旦单博主 QPS 突破上限，仅对其执行秒级降级，其他博主访问不受任何影响。

### 🛠️ 动手挑战 (Hands-on Challenge)
- **任务**：故意在 `tweet_handler.go` 中模拟 gRPC 调用超时报错，使用 K6 发起 Feeds 流的高频刷新。
- **验证**：截图证实 Sentinel 熔断是否瞬时生效，页面直接快速返回 429 报错，且 Sentinel 拦截率从 0% 迅速攀升至 100%，成功实现了对下游的“止血保护”。

---

## 🗓️ Day 19: OTel 全链路追踪跨协程延续与 PLG 日志级联 (Observability)

### 🎯 学习目标与安排
- **文件范围**：[follow_repository.go](file:///e:/GOProject/云原生/twitter-clone/internal/module/follow/repository/follow_repository.go) (异步链路Span传递), [main.go](file:///e:/GOProject/云原生/twitter-clone/cmd/gateway/main.go) (网关 RingBuffer ERROR 日志拦截)
- **核心目标**：攻克高并发系统下由于“非阻塞异步协程开发”引起的 OTel Trace 追踪穿透截断问题，以及实现日志到链路 Trace 的极速级联跳转。

### 🛣️ 请求链路全景 (Request Flow)
1. 客户端请求进入 API 网关，网关抽取 HTTP Header 里的 trace-parent 并注入 OTel Context。
2. 网关通过 gRPC 传递给下游 `tweet-service`。
3. 异步协程处理：当服务在异步拉取大V或计算时，通过 `context.WithoutCancel` 剥离取消信号，但在内存中完整复制并透传 Span 上下文至新启动的 goroutine 中。
4. 追踪延续：异步协程内部创建的子 Span 成功在 trace 拓扑图中展现为级联父子树形结构，Trace 绝不中断。
5. 错误捕获：若发生 500 报错，网关内置的线程安全固定容量 20 元素 RingBuffer 日志环立刻捕获该 context 附近的 ERROR 日志，配置 derivedFields 提取日志中的 `trace_id`，在 Grafana 界面直接生成 Jaeger 跳转超链接，实现一键定位。

### 🔑 核心重点 (Key Focus)
- **非阻塞上下文脱钩 (Context Detach)**：吃透 `context.WithoutCancel` 的妙用。为什么高并发下直接传递请求的 `ctx` 会在请求返回时因为 context cancelled 导致异步追踪的 Span 发生幽灵截断和数据丢失？
- **并发安全日志环**：重点看 RingBuffer 的 `sync.Mutex` 或无锁并发竞争控制。

### ✨ 技术亮点 (Architecture Highlights)
- **全站终极可观测性拓扑**：配置了 Loki 与 Jaeger 间的 Derived Fields。开发者在查看某微服务的崩溃日志时，可一键点击 trace_id 直接无缝跳转至该请求的分布式全链路拓扑图（Jaeger UI），精确定位到底是 MySQL 慢查询、Redis 异常还是 AI API 波动，排障时延降至秒级。

### ⚠️ 避坑指南 (Gotchas)
- **Span 忘记 End**：若在异步协程内开启了 `tracer.Start`，但因为 Panic 或者在分支 return 时遗漏了 `defer span.End()`，该 Span 将永远保持 Open 状态，在内存中堆积导致 OTel 收集器产生内存耗尽。

### 🚀 进阶玩法 (Advanced Play)
- **OTel 自动指标关联 (Trace-to-Metrics-to-Logs)**：实现全自动的指标到链路跳转。当 Prometheus 检测到某服务的某个 API p99 延迟突破 2s 时，Grafana 直接列出该时段该 API 发生超时的 Trace ID 采样列表，完全实现 AIOps 智能治理。

### 🛠️ 动手挑战 (Hands-on Challenge)
- **任务**：在本地故意让 follow-service 的某个异步操作耗时增加，打开 Jaeger 控制台（:16686）搜索相关的 Trace 记录。
- **验证**：截图确认异步查询大V关注的后台协程的 Span 是否成功挂载在主 Gateway API Span 的下方，证明 Trace 上下文级联传递成功。

---

## 🗓️ Day 20: 混沌测试、K8s 动态切流与 CI/CD 自动化 GitOps 大阅兵 (Chaos & CI/CD)

### 🎯 学习目标与安排
- **文件范围**：[network-delay.yaml](file:///e:/GOProject/云原生/twitter-clone/tests/chaos/network-delay.yaml) (混沌规则), [stress_feeds.go](file:///e:/GOProject/云原生/twitter-clone/scripts/stress/stress_feeds.go) (K6 剧本), [ci-cd.yaml](file:///e:/GOProject/云原生/twitter-clone/.github/workflows/ci-cd.yaml) (GitOps 工作流)
- **核心目标**：演练在 Kubernetes (Minikube) 分布式网络遭遇网络延迟 5s 的极端混沌故障下，微服务系统的灰度切流自愈表现以及 CI/CD 大图景。

### 🛣️ 请求链路全景 (Request Flow)
1. 开发者提交代码更新，推送到 GitHub 触发 `ci-cd.yaml` 自动化构建。
2. GitHub Actions 并行执行测试 ➔ 容器多阶段构建 ➔ 镜像推送至 Docker Hub。
3. ArgoCD 监听配置仓库，自动拉取最新 Helm chart values 并执行 self-heal（自愈同步）部署至集群。
4. 混沌注入：应用 `network-delay.yaml` 规则，模拟 Redis pod 与 Gateway 网络发生 5 秒的高额延迟故障。
5. 压测触发：K6 压测剧本启动，携带安全混沌旁路 Token 绕过网关限流，高并发请求 Feeds 流。
6. 自愈反应：网关高延迟触发 Prometheus  firing 告警，AIOps 诊断后调用 K8s 动态客户端对 VirtualService 下发 gray-cut 切流指令，将原本在 v2 pod 上的 10% 灰度流量瞬间回滚为 v1 pod 100% 承接，故障消除。

### 🔑 核心重点 (Key Focus)
- **灰度发布流量拓扑**：吃透 Helm chart values 参数对 Istio `VirtualService` 里的权重分配。
- **乐观锁并发冲突保护**：理解 client-go 动态客户端在调用 `Update` 时遇到 conflict (409) 错误时，为什么必须使用 `RetryOnConflict` 乐观锁进行重试。

### ✨ 技术亮点 (Architecture Highlights)
- **终极云原生治理闭环**：该演练打通了从 GitHub Actions CI ➔ ArgoCD GitOps CD ➔ Chaos Mesh 混沌故障注入 ➔ Prometheus 可观测性指标捕获 ➔ AIOps 动态 VirtualService 回滚灰度切流的完整自愈防御链路。系统处于极强的高可用自治状态。

### ⚠️ 避坑指南 (Gotchas)
- **金丝雀回滚失效**：如果 Pod 扩容速度太慢，或者存活探针（Liveness Probe）配置的检测延迟过大，当灰度流量切回 V1 时，V1 的副本可能因瞬间涌入的流量超载而挂死，造成级联崩溃。必须配置 HPA 自动弹性伸缩防御。

### 🚀 进阶玩法 (Advanced Play)
- **基于 Flagger 的渐进式金丝雀发布 (Progressive Canary)**：引入 Flagger 控制器，在金丝雀发布期间自动以每 1 分钟增加 5% 权重的速率递增新版本流量，期间若有告警自动 100% 自动回滚，实现完全无人值守发布。

### 🛠️ 动手挑战 (Hands-on Challenge)
- **任务**：在本地 Minikube 环境中手动应用灰度切流 VirtualService YAML，调整 v1 与 v2 的流量权重（如 50:50）。
- **验证**：使用 K6 压测 feeds 流，通过查看不同版本 Pod 的日志，截图证明流量是否严格按照 50% 比例分发到不同的微服务 Pod 实例。
