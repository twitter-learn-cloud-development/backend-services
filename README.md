# Twitter Clone (Twitter-Cloud)

这是一个基于 **Go 语言 (Golang)**、**gRPC** 和 **云原生微服务架构** 构建的高性能社交网络平台。项目深度集成了 **AI Agent (智能体) 网络**、**AIOps 自研性能自适应调优系统** 以及 **Saga 分布式故障自愈引擎**，实现了“高并发 Feed 流 + 智能体协作 + 自动化治理”的端到端闭环，是生产级云原生和人工智能（AI + Cloud-Native）深度融合的实践范本。

---

## 🚀 项目技术亮点

### 1. 百万级高并发 Feed 流与多级缓存优化 (Cache Infrastructure)
- **零 GC 本地 BigCache 缓存**：引入 L1 进程内存二级 BigCache，规避 STW 扫描延迟，高频大V状态查询优先在本地解决。
- **随机 TTL 缓存雪崩防御**：L2 Redis 核心数据缓存采用“24小时 + 0~30分钟随机抖动”策略。
- **Singleflight 并发请求归并**：读热点在 Singleflight 层面结合 `select` 进行短超时（800ms）熔断，防止高并发下瞬间击穿缓存雪崩。
- **大V关注双阈值防抖 (Celebrities Guard)**：设计“5000 晋升 / 4500 降级”的滞后门限防抖，配合 Redis Pipeline 异步更新粉丝 L1 缓存，解除百万粉丝高频拉取导致的数据库 CPU 100% 负荷。
- **无感异步分页预加载 (Cursor Pre-warming)**：基于游标分页，检测 `hasMore` 后自动启动 2s 防超时预热协程提前拉取并预填充下页 L1/L2。

### 2. AIOps 智能 RCA 诊断与缓存自适应参数调优 (AIOps Closed-Loop)
- **Continuous Profiling 性能剖析**：无缝挂载 **Pyroscope** 收集 CPU/内存热点堆栈，由 **ProfilingAnalyzer** 实时抓取并解析 Top 5 耗时堆栈送入 AI 上下文。
- **大模型 RCA 根本原因分析**：API Gateway 并发安全 ERROR 日志环（RingBuffer）拦截 5xx 异常，结合火焰图数据调用百炼 LLM 进行根本原因分析。
- **自适应调优与防震荡冷却锁**：AI 自动下发 `TuneCacheConfig` 参数优化指令。通过 Redis **冷却排他锁（3分钟 Cooldown）** 与**参数安全护栏 (Guardrails)**，通过 Redis PubSub 广播给所有微服务执行配置零停机热重载，杜绝参数抖动震荡。

### 3. Saga 分布式故障编排与网格自愈 (Saga & Mesh Healing)
- **Temporal 状态机风控编排**：异步风控引入 **Temporal 流程编排引擎**，设计 anti-spam 影子风控（Shadowban）状态机，在不侵入主链路的前提下对恶意发帖执行防重投、Saga 幂等重试清洗与粉丝 Timeline 原子 Lua 洗地。
- **NetworkPolicy 边界防护**：严格实行白名单容器网络隔离，兼容 Istio 双向 TLS 与 Envoy 拦截端口。
- **K8s 动态客户端灰度切流自愈**：自愈器整合 `k8s.io/client-go` 的 `dynamic.Interface`，使用无类型 `unstructured` 操作 Istio VirtualService，配合乐观锁重试应对高并发冲突，故障时自动切流紧急止血。

### 4. AI Agent 多智能体网络与双路降级检索 (Agentic Mesh)
- **四大 Agent 协作创作模式**：
  - **模式一（直接对话）**：推特运营助手快速响应。
  - **模式二（RAG 语义搜索）**：双路并发召回，Qdrant 向量检索与 ES 文本检索（BM25）并发取数。
  - **模式三（AI 写推文）**：AI 多风格推文草稿生成、二阶段确认发布闭环。
  - **模式四（多 Agent 协作写推文）**：Search Agent 并行拉取领域素材、Style Agent 分析博主文风，Writer Agent 综合书写，**Review Agent 安全舆情审查官**执行合规拦截。
- **长连接生命周期控制 (SSE Context Fix)**：在进程级通过 `serviceCtx` 对 SSE 长连接进行生命周期脱钩控制，保障多轮对话保活不中断，并在退出时自动调用 `Close()` 彻底杜绝内存与协程泄漏。
- **双路优雅降级容错**：在向量库（Qdrant）未拉起或发生网络抖动时，Search 接口自动降级为 Elasticsearch 文本检索，即使两者皆不可达也会优雅返回兜底文本，消除 LLM 因冒泡 Error 导致思维中断。

---

## 🏗 服务架构总览

```
                              ┌──────────────────────────┐
                              │    Vue 3 Frontend Client │
                              │    http://localhost:5173 │
                              └──────────────────────────┘
                                           │
                                           │  REST / HTTP / WebSocket
                                           ▼
                              ┌──────────────────────────┐
                              │    API Gateway (BFF)     │
                              │    :9638 (Sentinel/OTel) │
                              └──────────────────────────┘
                                 │       │            │
                    gRPC (9091)  │       │            │  gRPC (9100)
         ┌───────────────────────┘       │            └─────────────────────────┐
         ▼                               ▼ gRPC (9092)                          ▼
┌──────────────────┐            ┌──────────────────┐                  ┌──────────────────┐
│   User Service   │            │  Tweet Service   │                  │  Agent Service   │
│   :9091 (Consul) │            │  :9092 (Consul)  │                  │  :9100 (gRPC/MCP)│
└──────────────────┘            └──────────────────┘                  └──────────────────┘
         │                               │                                     │
         ├───────────────────────────────┼─────────────────────────────────────┤
         ▼                               ▼                                     ▼
 ┌──────────────┐                ┌──────────────┐                      ┌────────────────┐
 │    MySQL 8   │                │   Redis 7    │                      │   MongoDB 7    │
 │    :3307     │                │   :6379      │                      │   :27017       │
 └──────────────┘                └──────────────┘                      └────────────────┘
                                         ▲                                     │
                                         │ 订阅广播                             │ 向量语义召回
                                         │                                     ▼
┌──────────────────┐                     │                              ┌────────────────┐
│     Consumer     │◀────────────────────┘                              │ Elasticsearch8 │
│ (Timeline/Sync)  │                                                    │   :9200        │
└──────────────────┘                                                    └────────────────┘
         ▲                                                                     ▲
         │ 异步事件扇出                                                         │ 双路并发
         ▼                                                                     ▼
┌──────────────────┐                                                    ┌────────────────┐
│   RabbitMQ MQ    │                                                    │ Qdrant Vector  │
│   :5672          │                                                    │   :6333 (HNSW) │
└──────────────────┘                                                    └────────────────┘
```

---

## 🤖 AI Agent 核心架构与模式

Agent Service 内部实现基于 MCP 协议（Model Context Protocol）的长连接复用，提供 4 种业务模式：

- **模式一：直接对话**
  直接接入云端大模型，作为您的推特助手实时解答运营、涨粉与推特算法问题。
- **模式二：语义搜索（RAG）**
  用户输入模糊语义描述，`agent-service` 发起双路并发检索，通过 Qdrant 进行高维向量 Cosine 相似度召回并结合 Elasticsearch 关键词文本倒排初筛，送入 Reranker 重排序，最终结合上下文数据通过大模型提炼回答。
- **模式三：AI 辅助写推文 & 二阶段确认发布**
  大模型为用户提供三种不同文风（正式版、轻松版、热点版）的 280 字推文草稿，用户点选后直接由前端调起内置 MCP `create_tweet` 工具自动在 Tweet Service 完成官方发布。
- **模式四：多 Agent 协作写推文（含 Reviewer 介入）**
  - **Search Agent**：根据创作领域并行检索相关推文充实素材。
  - **Style Agent**：拉取博主历史推文，评估文风和写作习惯。
  - **Writer Agent**：汇总素材、风格和要求完成草稿。
  - **Review Agent**：安全合规审查，保证推文内容不含有违法及敏感字段，生成 Markdown 格式的合规报告一同返回。

---

## 🛠 技术栈一览

| 类别 | 技术 |
| :--- | :--- |
| **开发语言** | Go (Golang) 1.25 |
| **Web / RPC** | Gin (BFF API), gRPC & Protobuf (Microservices) |
| **核心关系数据库** | MySQL 8.0 (基于 GORM 隔离分库) |
| **缓存 / 消息传递** | Redis 7.0 (Lua 脚本、Pipeline、PubSub 广播), RabbitMQ 3.13 (指数退避延迟重试、死信队列分流) |
| **AI 存储 / RAG** | MongoDB 7.0 (对话会话持久化), Qdrant 1.12 (向量搜索), Elasticsearch 8.13 (文本倒排/IK分词) |
| **分布式状态机** | Temporal 1.24 (Saga 流程编排) |
| **服务发现 / 限流** | Consul 1.15, Sentinel-Go (熔断限流安全白名单) |
| **性能调优与可观测性**| Pyroscope (持续剖析火焰图), Jaeger (OpenTelemetry 链路追踪), Prometheus & Grafana |
| **前端交互** | Vue 3, Pinia, Tailwind CSS |
| **部署架构** | Docker Compose (本地一键拉起), Helm Chart (Kubernetes 灰度部署) |

---

## 📦 快速开始与环境搭建

### 1. 复制并完善环境变量
复制 `.env.example` 为 `.env`：
```bash
cp .env.example .env
```
关键配置项如下：
```dotenv
# 数据库密码
DB_PASSWORD=your_mysql_password
MONGO_PASSWORD=your_mongo_password

# 阿里云百炼大模型（用于 AI 对话与推理）
DASHSCOPE_API_KEY=your_dashscope_api_key
DASHSCOPE_API_URL=https://dashscope.aliyuncs.com/compatible-mode/v1

# 本地 LM Studio API 地址（用于本地 Embedding 向量化，需预先加载 text-embedding-bge-m3 模型）
LM_STUDIO_API_URL=http://host.docker.internal:1234/v1
LM_STUDIO_MODEL_EMBEDDING=text-embedding-bge-m3
```

### 2. 启动基础设施与微服务容器
通过 `docker-compose` 一键拉起微服务集群与监控、自愈、数据体系（包括新增的 Qdrant Stateful 容器）：
```bash
docker-compose up -d --build
```
启动成功的容器列表：
- `user-service` / `tweet-service` / `follow-service` / `agent-service` / `gateway` / `consumer` / `notification-service` / `messenger-service`
- 基础设施：`mysql`, `redis`, `mongodb`, `elasticsearch`, `qdrant`, `rabbitmq`, `consul`, `temporal`, `temporal-ui`, `pyroscope`, `jaeger`, `prometheus`, `grafana`

### 3. 数据种子初始化 (Seeding Tech Tweets)
执行以下命令为系统灌入中文云原生和 Go 语言测试种子推文（含 K8s、eBPF、Redis等高质量推文）：
```bash
go run scripts/seed/seed_data.go
```
注入后，Consumer 会自动提取推文的 BGE-M3 向量数据并同步刷新至 **Qdrant** 与 **Elasticsearch**，确保 AI 智能体能成功检索到相关内容。

### 4. 运行前端
```bash
cd web
npm install
npm run dev
```
打开浏览器访问 `http://localhost:5173`。

---

## 📖 微服务阅读导航

在阅读本项目源码时，推荐遵循以下业务流方向：
1. **服务契约定义**：`api/*.proto` 涵盖了各服务间的高性能 RPC 通信方法。
2. **多级缓存与 Feed 排序**：
   - 进程缓存一致性监听：`internal/module/tweet/cache/timeline_cache.go`
   - Singleflight 并发归并：`internal/module/tweet/service/tweet_service.go`
3. **向量化事务发件箱设计 (Transactional Outbox)**：
   - 异步解耦提取器：`internal/mq/consumer/timeline_consumer.go`
   - 发件箱守护协程：`internal/mq/consumer/outbox_worker.go`
4. **Saga 状态机与影子风控**：
   - 风控工作流编排：`internal/module/agent/service/workflows.go`
   - 状态机事件监听：`internal/module/agent/service/activities.go`
5. **AI 智能体与 MCP 链路**：
   - 智能体决策 ReAct 循环：`internal/module/agent/service/agent_service.go`
   - 双路检索优雅降级：`internal/module/agent/mcp/tools/search_tweets.go`
   - 缓存参数自适应调优：`internal/module/agent/service/profiling_analyzer.go`

---

## 🔮 未来规划

| 方向 | 描述 | 优先级 |
| :--- | :--- | :--- |
| **多因子自愈恢复** | 引入 AIOps 网络自动切换与跨 Kubernetes 集群动态拓扑漂移。 | ⭐⭐⭐ |
| **智能防注入防火墙** | 在 Web 边界建立防 Prompt 注入与大模型幻觉隔离的拦截哨兵组件。 | ⭐⭐ |

---

## 📄 开源协议

本项目采用 [MIT](LICENSE) 协议，欢迎用于学习和二次开发。