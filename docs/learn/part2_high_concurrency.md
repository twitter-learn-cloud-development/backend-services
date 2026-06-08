# Part 2: 百万高并发 Feed 流与 AI Agent Mesh 网络 (Day 11 - Day 15)

---

## 🗓️ Day 11: Feed 流多级缓存引擎与防击穿 (GetFeeds 模块)

### 🎯 学习目标与安排
- **文件范围**：[tweet_service_mq.go](file:///e:/GOProject/云原生/twitter-clone/internal/module/tweet/service/tweet_service_mq.go) (读链路), [l1_cache.go](file:///e:/GOProject/云原生/twitter-clone/internal/module/tweet/cache/l1_cache.go) (本地缓存), [timeline_cache.go](file:///e:/GOProject/云原生/twitter-clone/internal/module/tweet/cache/timeline_cache.go) (读缓存组件)
- **核心目标**：吃透 “请求归并” 与 “多级缓存穿透防护” 在社交 Feed 流中的落地细节。

### 🛣️ 请求链路全景 (Request Flow)
1. 客户端通过网关发起 `GetFeeds` 请求。
2. `tweet-service` 拦截：通过 Redis 批量 Pipeline 并行提取普通关注 ZSet 缓存与大V关注 ZSet 缓存。
3. 内存归并排序：将不同来源的 Snowflake ID 进行去重与时间戳降序截断，得到 Top-N ID 列表。
4. 查 L1 缓存：穿透本地 `BigCache` 内存哈希，获取已格式化的推文 JSON 数据。
5. 查 L2 与 Singleflight 归并：未命中的 ID，通过 `singleflight.DoChan` 归并并发请求。仅放行一个单协程去查 L2 Redis 缓存（或查 MySQL），并更新 L1 本地缓存，向所有挂起等待的通道返回共享数据。
6. 异步预热：若有后续页（`hasMore` 为 true），非阻塞启动独立的 Go 协程异步拉取并填充 L1/L2 缓存，实现无感秒开。

### 🔑 核心重点 (Key Focus)
- **并发归并隔离**：深入理解 `singleflight.Group` 共享通道结构，防止下游缓存击穿与雪崩。
- **无锁零拷贝**：看 BigCache 的切片底层数据设计，如何防 GC 扫描，避免垃圾回收引起的抖动。

### ✨ 技术亮点 (Architecture Highlights)
- **简历高光设计**：利用本地内存（BigCache）作为 L1、Redis 作为 L2 的多级分布式缓存，辅以 `singleflight`，使得核心 feeds 接口吞吐率提升数十倍，即使 Redis 发生秒级网络延迟，核心数据库依然有坚固防线。

### ⚠️ 避坑指南 (Gotchas)
- **Singleflight 永久挂起**：若在 singleflight 回调的闭包函数中发起下游 gRPC 或数据库查询时未显式绑定超时 Context，一旦下游卡死，上游所有在单机上挂起等待该 Key 的连接都将永久假死阻塞。
- **异步 Context 取消**：严禁将 gRPC/HTTP 请求的短生命周期 Context 传递给异步预加载协程，当 HTTP 响应返回时该 Context 会被 cancel，导致预加载协程提前崩溃。

### 🚀 进阶玩法 (Advanced Play)
- **概率性提前刷新 (Probabilistic Early Expiration)**：在 L1 本地缓存到期的最后 5%~10% 的窗口期内，利用随机数算法在后台隐式生成一个协程刷新缓存，从而让上游用户始终命中已刷新缓存，达成 100% 绝对零延迟读取。

### 🛠️ 动手挑战 (Hands-on Challenge)
- **任务**：在本地代码中，将 L1 缓存 BigCache 的 TTL 缩短为 1 秒，并利用 K6 压测脚本对 GetFeeds 进行并发 1000 请求压测。
- **验证**：在 singleflight 查 L2 逻辑内部打印一条 Debug 日志。验证 1000 个高并发请求下，是否只有 1 次日志输出，其余 999 个请求均通过 singleflight 合理合并，阻断了雪崩。

---

## 🗓️ Day 12: 大V推特混合 Feed 流与防抖架构 (Celebrity Push/Pull Hybrid)

### 🎯 学习目标与安排
- **文件范围**：[timeline_cache.go](file:///e:/GOProject/云原生/twitter-clone/internal/module/tweet/cache/timeline_cache.go) (大V ZSet 校验), [follow_service.go](file:///e:/GOProject/云原生/twitter-clone/internal/module/follow/service/follow_service.go) (关注流与防抖门限)
- **核心目标**：研究在高并发下，如何基于 Push 与 Pull 混合模式解决社交网络的核心痛点。

### 🛣️ 请求链路全景 (Request Flow)
1. 粉丝数判定：当用户发布推文时，检查该作者是否属于大V（缓存于 Redis `global:celebrities` 且在本地内存 L1 中备份）。
2. 普通作者发帖（写扩散）：通过异步 Consumer 直接广播推送给所有活跃粉丝的 Timeline ZSet 缓存。
3. 大V作者发帖（读扩散）：仅写入该大V专属的 `user_timeline:<ID>` ZSet 缓存，不广播给粉丝。
4. 粉丝刷新 Timeline（混合拉取）：系统并行拉取普通 Timeline ZSet，并去 Redis 批量提取粉丝所关注的所有大V的专属 ZSet 缓存，在内存中执行 Merge Sort，最终截取并返回 Top-100。
5. 粉丝关系变更（防抖保护）：用户关注/取关时，触及 5000/4500 粉丝数浮动门限，防抖机制拦截频繁的广播。

### 🔑 核心重点 (Key Focus)
- **非对称阈值**：理解为什么大V判定需要使用两个不同的门限值（5000 粉丝晋升，4500 粉丝降级），这是经典的网络防抖（Hysteresis）算法。
- **本地二级本地缓存**：了解 L1 本地内存 `celebrityLocalCache` 带并发读写锁的本地 TTL 设计，防 Redis 读压力被高并发穿透。

### ✨ 技术亮点 (Architecture Highlights)
- **混合 Feed 流弹性架构**：大V发帖不进行写扩散，消除了百万级粉丝瞬间带来的高并发写开销；同时限制单大V ZSet 长度，让内存 Merge Sort 的时间复杂度控制在 $O(M \log K)$（$M$ 为大V数，$K$ 为页大小），完美化解了分布式系统的社交“大V写雪崩”效应。

### ⚠️ 避坑指南 (Gotchas)
- **占位符丢失导致缓存穿透**：大V的推文可能为空（例如未发过推文）。如果 ZSet 为空且未加任何占位保护，查询时会判定为“未初始化”而频繁穿透去查底层 DB，引发 SQL 飙升。必须写入特殊的占位符与 `:initialized` Key 作为标记防穿透。

### 🚀 进阶玩法 (Advanced Play)
- **活跃粉丝动态判定**：非活跃粉丝（如超过 30 天未登录的用户）直接剔除出写扩散的粉丝列表，改用纯拉（Pull）逻辑。当该粉丝重新上线时再触发追溯拉取，以极大节省内存占用。

### 🛠️ 动手挑战 (Hands-on Challenge)
- **任务**：手动向 Redis 的 `global:celebrities` 集合添加一个用户的 ID 模拟大V。让该用户发帖后，观察该推文是否**没有**被推入粉丝的 ZSet 缓存，但在粉丝重新拉取 Feed 流时，控制台显示在内存中成功拉取了大V专属时间线并将其合并输出。

---

## 🗓️ Day 13: ES/Qdrant 向量发件箱 Worker 双写异步同步器 (Transactional Outbox)

### 🎯 学习目标与安排
- **文件范围**：[timeline_consumer.go](file:///e:/GOProject/云原生/twitter-clone/internal/mq/consumer/timeline_consumer.go) (事件消费), [outbox_worker.go](file:///e:/GOProject/云原生/twitter-clone/internal/mq/consumer/outbox_worker.go) (发件箱轮询), [outbox.go](file:///e:/GOProject/云原生/twitter-clone/internal/domain/outbox.go) (模型结构)
- **核心目标**：吃透分布式写扩散中，如何利用发件箱模型消除大模型 Embedding 接口同步调用瓶颈。

### 🛣️ 请求链路全景 (Request Flow)
1. 用户发布推文，`tweet-service` 将推文落入 MySQL 关系型数据库，并在同个本地事务中向 `outbox_tasks` 表写入一条待向量化同步任务。
2. 事务提交后，服务迅速向 RabbitMQ 广播发帖事件。
3. Timeline 消费者收到事件后，快速把推文 ID 写入粉丝的 Timeline Redis ZSet，此步直接返回，不等待向量化，耗时降至微秒级。
4. 后台常驻的 `OutboxWorker` 轮询线程以指数退避算法安全提取 pending 任务。
5. Worker 调用 AI Embedding 接口生成 1024 维度的文本向量。
6. 双写机制：向量及推文内容双写写入 Qdrant 数据库及 Elasticsearch，并将 outbox 任务置为成功（或超时物理清理）。

### 🔑 核心重点 (Key Focus)
- **两阶段事务落盘**：重点关注如何在一条 SQL 事务内原子级写入主推文与发件箱任务表，保证“双写一致性”的前提不倒。
- **并发锁与冲突重试**：分析 `OutboxWorker` 在分布式环境下防止多实例重复拉取同一个 pending 任务的更新锁策略。

### ✨ 技术亮点 (Architecture Highlights)
- **出色的高并发写入解耦**：将原本发帖时“同步请求大模型向量化并写入 ES/Qdrant”（单次发帖时延超 1s 且易由于网络抖动直接崩掉）的操作，解耦为事务写入 + 异步对账同步，发帖吞吐量与可用性瞬间提升数个数量级。

### ⚠️ 避坑指南 (Gotchas)
- **Snowflake 精度截断**：Qdrant 默认只接受 uuid 或者是指定范围的数字作为 Point ID。直接将 64 位无符号整型（uint64）的雪花 ID 传给 Qdrant 会导致超出 JavaScript 精度或强转溢出。必须使用转换算法 `ConvertSnowflakeToQdrantID` 将其降维映射，保证幂等。
- **发件箱爆表**：如果同步进程在后台发生持续异常，发件箱会由于持续写入而爆表。必须引入物理删除成功任务（DELETE）与对重试 5 次失败的任务置为 `Failed` 并封存审计的退避策略。

### 🚀 进阶玩法 (Advanced Play)
- **多引擎对账机制**：在后台启动定时差分对账 Worker，对比 MySQL、ES 和 Qdrant 的数据总量，检测并自动回填数据同步链路上的漏单数据。

### 🛠️ 动手挑战 (Hands-on Challenge)
- **任务**：暂时关停本地的 `qdrant` 容器，然后通过 API 发布 5 条新推文，观察 `outbox_tasks` 表中任务的状态和重试计数的自增。重新拉起服务后，验证自愈机制是否能够重新拉起并完成消费。

---

## 🗓️ Day 14: AI Agent 模式二 RAG 搜索与双引擎优雅降级 (RAG Search & Fallback)

### 🎯 学习目标与安排
- **文件范围**：[search_tweets.go](file:///e:/GOProject/云原生/twitter-clone/internal/module/agent/mcp/tools/search_tweets.go) (MCP Tool 端), [agent_service.go](file:///e:/GOProject/云原生/twitter-clone/internal/module/agent/service/agent_service.go) (LLM ReAct 执行端)
- **核心目标**：深度剖析 RAG（检索增强生成）在微服务中的“高并发并行召回”、“二次精排（Rerank）”以及向量库故障时的优雅降级方案。

### 🛣️ 请求链路全景 (Request Flow)
1. 用户在前端 Agent 界面提交语义搜索请求。
2. `agent-service` 驱动百炼大模型进入 ReAct 决策，大模型识别到查询意图，下发 `search_tweets_by_semantic` 工具调用。
3. 向量路与倒排路并发：通过 `errgroup` 并行发起向量化 Embedding 请求。
4. 双路初筛召回：
   - 向量路：向 Qdrant 检索 HNSW 索引（向量相似度）。
   - 倒排路：向 Elasticsearch 检索 BM25 文本匹配（通过 IK 中文分词）。
5. Rerank 重排精排：对双路合并后的候选集执行精排重算（Reranker），以降低大模型召回的上下文噪音。
6. 延迟回表（Late Materialization）：拿着胜出的 Top-K 推文 ID，通过 gRPC 批量请求 `tweet-service` 获取最新的真实推文实时字段（如喜欢数、状态）。
7. **优雅降级**：若 Qdrant 故障，自动回退只查 ES 倒排路并正常响应；若两路都挂，优雅返回提示文本而非抛错。

### 🔑 核心重点 (Key Focus)
- **Reranker 参数配比**：了解 Rerank 对检索噪音的过滤效果。
- **Tool 零 Error 机制**：理解为什么 MCP Tool 函数在两路检索皆墨时**必须返回自然语言文本**而不是返回 `error`，这是为了防大模型在捕获 RPC 错误时发生幻觉或崩溃。

### ✨ 技术亮点 (Architecture Highlights)
- **生产级容灾 RAG 漏斗**：双路并发不仅将网络等待耗时压低，还通过 ES-BM25 与 Qdrant-HNSW 的多重互补召回机制保证了在纯英文或专业术语下的精准匹配。Qdrant 与 ES 双路由优雅降级，极大增强了智能体底层核心工具的安全稳定性。

### ⚠️ 避坑指南 (Gotchas)
- **大模型幻觉风暴**：RAG 召回如果没有做延迟回表直接把缓存里的旧数据塞给 LLM，如果推文已经被用户删除，大模型依据旧缓存数据回答，会有幻觉。必须以回表后的实时数据为准。

### 🚀 进阶玩法 (Advanced Play)
- **多维度混合过滤**：在 Qdrant 向量检索时通过 Payload 动态传递 Filter（例如关注关系或影子风控可见性属性），在向量检索内部（HNSW）直接执行硬隔离，进一步提升初筛召回的精度。

### 🛠️ 动手挑战 (Hands-on Challenge)
- **任务**：暂时停止本地的 `qdrant` 容器，然后调用“咨询搜索（模式二）”向 AI 助手搜“帮我搜搜关于云原生的推文”。验证降级机制是否自动调用 ES，且无报错响应。

---

## 🗓️ Day 15: AI Agent 模式三与模式四多智能体协同网络 (Agentic Mesh & SSE)

### 🎯 学习目标与安排
- **文件范围**：[agent_service.go](file:///e:/GOProject/云原生/twitter-clone/internal/module/agent/service/agent_service.go) (多Agent协作、SSE生命周期管理)
- **核心目标**：理解并发多智能体协同体系中，长连接生命周期的优雅脱钩与并行 IO 检索。

### 🛣️ 请求链路全景 (Request Flow)
1. 客户端向 `/agent/multi`（模式四）或 `/agent/assist`（模式三）发起连接。
2. 进程内 SSE 回环拦截：客户端拨号前自动将 `0.0.0.0` 转换为 `127.0.0.1` 避开容器路由，且服务端 `SSEServer` 动态响应其 Origin，通过严格握手。
3. 客户端调用 `mcpClient.Start(s.serviceCtx)`，将 SSE 长连接流绑定至长生命周期的服务级 Context，确保请求返回后连接不断。
4. 协作开始：
   - 模式三：写想法 ➔ LLM 生成草稿。用户选择草稿后二阶段 `Confirm` 绕过大模型直调 Tool 发推。
   - 模式四：`errgroup` 并行发起 Search Agent、Style Agent、Reference Agent 三路数据抓取，整合后给 Writer Agent 协作输出，并由 Review Agent 执行安全舆情审查。

### 🔑 核心重点 (Key Focus)
- **连接生存周期管理**：为何不能把请求的短 Context 传入 Start？吃透 gRPC 端点请求释放导致的连接暴毙。
- **并发 IO 与无锁传递**：看协程中无锁内存隔离设计，如何消减高负载时的锁竞争。

### ✨ 技术亮点 (Architecture Highlights)
- **高响应多智能体协作**：用原生的 Go 协程并发取代了重量级 Python 框架（CrewAI/LangChain）的串行大网络开销，时延压缩到极致，并在输出流中完美拼接舆情合规评估，保证内容生产合规。

### ⚠️ 避坑指南 (Gotchas)
- **资源永久假死与泄漏**：在退出系统或重载配置时，必须在退出处理中显式调用 `svc.Close()` 触发长连接底层 Context 释放，否则未断开的 SSE 读取协程会永久停留在后台，导致 Goroutine 内存泄漏。

### 🚀 进阶玩法 (Advanced Play)
- **Synamic BasePath 支持**：在多租户或者分布式多活集群下，实现带有租户 Session 状态的动态 BasePath 连接池，避免大吞吐下的连接单点负载瓶颈。

### 🛠️ 动手挑战 (Hands-on Challenge)
- **任务**：故意把 `agent_service.go` 中 `getOrInitMCPClient` 里的 `s.serviceCtx` 改回 gRPC 的 `ctx`。
- **验证**：执行两次连续发问，证明第二次对话抛出 500 崩溃，并在改回 `s.serviceCtx` 后验证回归测试全绿通过。
