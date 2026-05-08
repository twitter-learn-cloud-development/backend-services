# Twitter Clone (Twitter-Cloud)

这是一个基于 Go 语言、gRPC 和微服务架构构建的高性能云原生推特克隆项目，现已集成 AI Agent 能力，支持语义搜索、智能对话和 AI 辅助创作。

## 🚀 项目亮点

- **云原生微服务架构**：服务化拆分（用户、推文、关注、私信、通知、AI Agent 等），解耦业务逻辑。
- **全链路追踪 & 监控**：集成 Jaeger (OpenTelemetry)、Prometheus 和 Grafana，实现可观测性。
- **高性能时间线算法**：利用 RabbitMQ 异步扇出（Fanout）技术和 Redis 缓存，支持极速 Feed 流刷新。
- **AI Agent 能力**：基于 MCP 协议构建 AI 工具层，支持语义搜索、RAG 增强检索和 AI 辅助写推文。
- **混合语义搜索**：Elasticsearch + IK 中文分词 + bge-m3 向量模型，支持关键词和语义双路召回。
- **现代化前端**：基于 Vue 3 + Vite + Tailwind CSS 构建，提供流畅的用户体验。

## 🏗 服务架构

项目采用微服务架构设计，核心组件包括：

| 服务 | 职责 |
| :--- | :--- |
| **API Gateway** | 统一 RESTful 入口，路由转发、JWT 认证、限流、统一错误处理 |
| **User Service** | 用户注册、登录、个人资料管理及社交属性扩展 |
| **Tweet Service** | 推文发布（文本/图片/视频/投票）、点赞、评论、转发、全站搜索 |
| **Follow Service** | 用户关注、取关关系及统计汇总 |
| **Messenger Service** | 私信功能，支持 WebSocket 实时通信 |
| **Notification Service** | 实时通知系统，通过 RabbitMQ 监听事件并分发 |
| **Consumer** | 异步处理中心：推文扇出、热门话题提取、推文向量化写入 ES |
| **Agent Service** 🆕 | AI Agent 服务，基于 MCP 协议提供三种 AI 能力模式 |

## 🤖 AI Agent 能力

Agent Service 基于 MCP（Model Context Protocol）标准协议构建，提供三种核心模式：

### 模式一：智能对话

直接接入阿里云百炼 qwen3.6-plus，作为推特助手回答用户问题。

### 模式二：语义推文搜索（RAG）

用户输入自然语言描述 → bge-m3 Embedding 模型向量化 → ES kNN 语义搜索召回推文 → LLM 结合召回内容生成回答。支持纯语义搜索和关键词 + 语义混合搜索两种模式。

### 模式三：AI 辅助写推文

用户提供碎片化想法 → LLM 生成多个不同风格的推文草稿 → 用户确认后通过 MCP Tool 直接调用 tweet-service 完成发布，实现创作到发布的全自动闭环。

### 模型分工

| 职责 | 模型 | 部署方式 |
| :--- | :--- | :--- |
| 文本向量化 | bge-m3 | 本地 LM Studio |
| 对话 / 推理 | qwen3.6-plus | 阿里云百炼 API |

## 🛠 技术栈

| 类别 | 技术 |
| :--- | :--- |
| **后端语言** | Go (Golang) |
| **Web 框架** | Gin (Gateway), gRPC (内部通信) |
| **数据库** | MySQL 8.0, Redis 7.0, MongoDB 7.0 |
| **搜索引擎** | Elasticsearch 8.13 + IK 中文分词器 |
| **AI 框架** | MCP (Model Context Protocol), RAG, ReAct |
| **AI 模型** | qwen3.6-plus (百炼), bge-m3 (本地) |
| **消息队列** | RabbitMQ |
| **服务治理** | Consul (服务发现), Sentinel (熔断降级) |
| **链路追踪** | Jaeger / OpenTelemetry |
| **监控报警** | Prometheus & Grafana |
| **前端框架** | Vue 3, Pinia, Vue Router |
| **前端样式** | Tailwind CSS |
| **容器化** | Docker & Docker Compose |

## 📦 快速开始

### 1. 克隆项目

```bash
git clone <project-url>
cd twitter-clone
```

### 2. 配置环境变量

复制 `.env.example` 为 `.env` 并填写配置：

```bash
cp .env.example .env
```

关键配置项：

```dotenv
# 数据库
DB_PASSWORD=your_password
MONGO_PASSWORD=your_password

# 阿里云百炼（LLM 对话）
DASHSCOPE_API_KEY=your_key
DASHSCOPE_API_URL=https://dashscope.aliyuncs.com/compatible-mode/v1

# LM Studio（本地 Embedding）
LM_STUDIO_API_URL=http://localhost:1234/v1
LM_STUDIO_MODEL_EMBEDDING=text-embedding-bge-m3
LM_STUDIO_MODEL_CHAT=qwen3.6-plus
```
# 注意注意！！！
以上关键性配置均需要在 `docker-compose.yaml` 中单独配置。因为我需要在本地启动 LM Studio，所以需要在 `docker-compose.yaml` 中单独配置以便与项目其他服务共存。


### 3. 启动基础设施

```bash
docker-compose up -d
```

### 4. 启动本地 Embedding 模型

在 LM Studio 中加载 `text-embedding-bge-m3` 模型并启动服务。

### 5. 运行前端

```bash
cd web
npm install
npm run dev
```

访问 `http://localhost:5173` 进入系统。

## 📖 学习阅读指南

建议按以下路径阅读：

1. **全局组件**：`docker-compose.yaml`
2. **服务契约**：`api/*.proto`
3. **接口入口**：`internal/gateway/router/router.go`
4. **核心业务**：`internal/module/tweet/service/tweet_service.go`
5. **时间线推送**：`internal/mq/consumer/timeline_consumer.go`
6. **AI Agent**：`internal/module/agent/service/agent_service.go`
7. **MCP Tools**：`internal/module/agent/mcp/server.go`

## 🗺 未来规划

- **多 Agent 协作写推文**：Style Agent 分析用户历史推文风格 → Search Agent 召回同类热门内容 → Writer Agent 综合生成高度契合用户风格的草稿，三个 Agent 通过 MCP 协议串联。
- **Neo4j 社交图谱**：引入图数据库，由 Graph Agent 分析二度/三度人脉，由 Semantic Agent 匹配兴趣向量，实现"与我兴趣相同且在社交距离内"的精准推荐。

## 📄 开源协议

本项目采用 [MIT](LICENSE) 协议。