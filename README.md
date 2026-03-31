# Twitter Clone (Twitter-Cloud)

这是一个基于 Go 语言、gRPC 和微服务架构构建的高性能云原生推特克隆（Twitter Clone）项目。项目涵盖了从后端微服务治理到前端响应式界面的完整实现。

## 🚀 项目亮点

- **云原生微服务架构**：服务化拆分（用户、推文、关注、私信、通知等），解耦业务逻辑。
- **全链路追踪 & 监控**：集成 Jaeger (OpenTelemetry)、Prometheus 和 Grafana，实现可观测性。
- **高性能时间线算法**：利用 RabbitMQ 异步扇出（Fanout）技术和 Redis 缓存，支持极速 Feed 流刷新。
- **现代化前端**：基于 Vue 3 + Vite + Tailwind CSS 构建，提供流畅的用户体验。

## 🏗 服务架构

项目采用微服务架构设计，核心组件包括：

- **API Gateway**: 统一的 RESTful 入口，负责路由转发、JWT 认证、限流 (Rate Limiting) 和统一错误处理。
- **User Service**: 用户注册、登录、个人资料管理及社交属性扩展。
- **Tweet Service**: 推文发布（支持文本/图片/视频/投票）、点赞、评论、转发及全站搜索。
- **Follow Service**: 处理用户间的关注、取关关系及统计汇总。
- **Messenger Service**: 实现私信（Direct Messages）功能，支持 WebSocket 实时通信。
- **Notification Service**: 实时通知系统，通过 RabbitMQ 监听事件并分发。
- **Consumer**: 异步逻辑处理中心（如推文扇出到粉丝时间线、热门话题 `#Hashtag` 提取）。

## 🛠 技术栈

| 类别 | 技术 |
| :--- | :--- |
| **后端代码** | Go (Golang) |
| **Web 框架** | Gin (Gateway), gRPC (Internal Communication) |
| **数据库** | MySQL 8.0 (Persistence), Redis 7.0 (Caching) |
| **消息队列** | RabbitMQ (Asynchronous Events) |
| **服务治理** | Consul (Service Discovery), Sentinel (Circuit Breaker) |
| **链路追踪** | Jaeger / OpenTelemetry |
| **监控报警** | Prometheus & Grafana |
| **前端框架** | Vue 3, Pinia (State Management), Vue Router |
| **前端样式** | Tailwind CSS |
| **虚拟化** | Docker & Docker Compose |

## 📦 快速开始

### 1. 克隆项目
```bash
git clone <project-url>
cd twitter-clone
```

### 2. 基础设施启动
项目依赖 MySQL, Redis, RabbitMQ 等，建议通过 Docker 一键启动：
```bash
docker-compose up -d
```

### 3. 运行后端服务
你可以手动编译各服务二进制文件，或在 IDE 中直接运行：
- **API Gateway**: `cmd/gateway/main.go` (端口: 9638)
- **其他微服务**: 各个 `cmd/*-service/main.go`

### 4. 运行前端
```bash
cd web
npm install
npm run dev
```
访问 `http://localhost:5173` 即可进入系统。

## 📖 学习记忆指南

如果你是为了学习该项目，建议遵循以下阅读路径：
1. **入门**: `docker-compose.yaml` 了解全局组件。
2. **骨架**: `api/*.proto` 了解各服务间的契约。
3. **接口**: `internal/gateway/router/router.go` 查看所有功能入口。
4. **核心逻辑**: `internal/module/tweet/service/` 观察如何处理推文业务。
5. **高性能**: `internal/mq/consumer/timeline_consumer.go` 学习推特核心的时间线推送方案。

## 📄 开源协议
本项目采用 [MIT](LICENSE) 协议。

## 下一步计划
### 🚀 推特 AI Agent 项目架构与演进规划
#### 1. 核心决策与定位
开发策略：不另起炉灶，在现有的推特 Go 微服务架构上扩展。

新增组件：新建独立微服务 agent-service，与已有的 user / tweet 等服务平级。

交互协议：采用 MCP (Model Context Protocol) 行业标准，将发推、查询等封装成 AI 可调用的 Tools。

#### 2. Agent 的核心双模型场景
场景一：智能查询与推荐模型 (Search & Recommend Agent)
业务表现：用户输入“我最近迷上了健身，推荐三个博主”或“帮我找找最近很火的 XX 事件”。

底层逻辑：

AI 解析用户意图（提取标签：健身）。

结合用户画像，调用后端检索工具。

将返回的数据（博主列表、推文）进行提炼，甚至尝试返回“不定式”的灵活 UI 数据结构给前端渲染。

场景二：内容创作与执行模型 (Write & Execute Agent)
业务表现：用户发送图片、思路碎片，AI 负责润色出几版推文供用户选择，并直接完成发布。

底层逻辑：

基于多模态大模型理解图片和文本。

利用 ReAct 循环（思考 -> 找工具 -> 执行 -> 确认）。

用户确认后，Agent 调用 MCP 接口（如 twitter-mcp-server）自动完成发推，实现全自动闭环。

#### 3. 基础设施升级与数据流 (近期的“硬骨头”)
为了让 Agent 有足够聪明的“大脑”和丰富的“上下文”，需要进行以下基建升级：

① 搜索引擎升级 (ES + Canal)
痛点：MySQL LIKE 查询太慢且无语义理解。

方案：引入 Elasticsearch，配置 IK 中文分词器。使用 Canal 监听 MySQL 的 binlog，实现推文数据的实时增量同步。

在 Agent 中的作用：作为 RAG（检索增强生成）的数据源，给 Prompt 提供精准的上下文。

② 用户画像分层存储策略 (零新增组件方案)
MySQL：存静态画像和 Agent 历史对话记录（使用 JSON 字段，暂不引入 MongoDB，降低运维成本）。

Redis：存近期活跃行为和搜索词（ZSet + TTL，高频读写，做短期记忆）。

Elasticsearch：存用户偏好的向量（kNN 字段），用于语义级推荐匹配。

#### 4. 社交图谱与进阶推荐 (Neo4j 引入计划)
定位：作为项目的第二阶段目标，主要用于解决复杂的关系网查询。

为什么用 Neo4j：传统的 MySQL 找“朋友的朋友”（二度/三度人脉）需要极其消耗性能的多层 JOIN，而 Neo4j 作为图数据库，采用 Index-Free Adjacency（无索引邻接）底层设计，通过指针直连，沿关系查询的速度是 MySQL 的指数级倍数。

应用场景：
 
维护用户间的复杂社交网络。

实现基于关系的精准好友推荐（PersonalRank / 共同关注）。

挂钩用户兴趣，快速检索出“跟我兴趣相同，且在社交距离内的人”。

Action Items (开发路线图)
Step 1: 基础设施搭建

接入 Elasticsearch，配置中文分词器。

跑通 Canal，将 MySQL 的推文/用户数据平滑同步到 ES。

Step 2: 构建 MCP Server

编写 AI 可调用的内部接口（ 发推、查推、查用户 ）。

Step 3: 开发 agent-service

搭建双模型业务流（查询推荐流 + 创作发布流）。

将 ES 检索结果封装入 Prompt，提供 RAG 增强。

Step 4: 探索 Neo4j (Future)

建立节点（用户、话题）和边（关注、点赞）的图关系，开发基于图谱的推荐引擎。 