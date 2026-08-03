# Environment Context（运行环境与验证入口）

> 最近核对：2026-08-02
> 事实源：`.env.example`、`docker-compose.yaml`、`cmd/*/main.go`

## 1. 配置优先级

1. 进程环境变量 / 本地 `.env`。
2. Consul 动态配置（仅已接入的服务/配置项）。
3. `configs/config.yaml` 或代码 fallback。
4. `.env.example` 只是模板，不能作为运行事实。

禁止把 `.env`、API Key、JWT Secret 或数据库密码写入文档、DSL、日志和测试快照。

## 2. 本地端口

| 组件 | 端口 |
|------|------|
| Gateway | 代码 fallback `8080`；`.env.example` 配置 `9638` |
| User / Tweet / Follow | `9091` / `9092` / `9093` |
| Messenger / Notification / Auth | `9094` / `9095` / `9097` |
| Agent gRPC / MCP | `9100` / `9200` |
| MySQL / Redis / Mongo | `3307` / `6379` / `27017` |
| Elasticsearch / Qdrant | `9200` / `6333`（HTTP）、`6334`（gRPC） |
| RabbitMQ | `5672`；管理端 `15672` |
| Consul / Temporal | `8500` / `7233`；Temporal UI `8233`（以 Compose 为准） |
| Jaeger / Prometheus / Grafana | `16686` / `9090` / `3000` |
| MinIO | `9000`；Console `9001` |
| Loki / Kibana / Sentinel / Pyroscope | `3100` / `5601` / `8858` / `4040` |

端口冲突或 Compose 变化时，以当前 `docker-compose.yaml` 和进程日志为准。

## 3. 数据与 AI 配置

- MySQL：社交领域主数据与 Persona。
- MongoDB：Agent Dialogue/Workflow/Run，不是 Messenger 主存储。
- Elasticsearch：全文/BM25；Qdrant：向量。
- LM Studio：本地 OpenAI-compatible Provider，默认 `http://localhost:1234/v1`。
- LM Studio、Docker Desktop、Minikube 等由用户交互启动的本地软件不视为常驻依赖。执行 Live 验收前必须先告知用户所需软件、精确模型、端口、用途和预计时长，并等待用户确认已经启动；未经确认时的 `connection refused` 只表示依赖未启动，不能登记为项目故障。
- P8.4 当前自动资格报告固定 Chat 模型 `qwen3.7-plus-2026-05-26` 与 Profile Set v5；`qwen2.5-3b-instruct` 只保留为真实失败基线。单/多 Agent 20 Case 搜索工具仍为无副作用评测沙箱，不需要 Embedding 模型。P6 RAG/Router 当前默认使用 `text-embedding-bge-m3`；`text-embedding-nomic-embed-text-v1.5` 不是当前固定评测配置，可作为后续模型迁移对照，不能与 BGE 结果混用为同一基线。
- Live Eval 默认使用本地 file 授权账本；多实例时可选 Redis 共享账本，配置示例为 `internal/module/agent/eval/testdata/agent_task_live_redis_config.example.json`。Redis Username/Password 只从配置指定的环境变量读取；生产要求 TLS/ACL、AOF/备份、`noeviction` 和受控 marker 生命周期。运行真实 Redis 初始化或故障演练前也必须先告知用户所需地址、用途和预计时长并等待确认；普通测试只使用进程内 `miniredis`。
- DashScope：默认云端 Chat Provider。
- 容器访问宿主 LM Studio 时使用明确允许的 `host.docker.internal`；不要把任意私网 Base URL 加入 allowlist。
- 工作流使用 `credential_ref`，不接受明文 `api_key`。
- `AGENT_TOOL_APPROVAL_TTL` 控制待审批有效期，默认 `15m`；恢复令牌只在挂起响应返回一次。
- Tool Result 默认内联上限 `64 KiB`、硬上限 `1 MiB`，对应 `AGENT_TOOL_RESULT_INLINE_MAX_BYTES` 与 `AGENT_TOOL_RESULT_MAX_BYTES`。Docker 中显式启用 `AGENT_TOOL_RESULT_OBJECT_STORE_ENABLED`，并使用独立私有桶 `agent-tool-results`；关闭开关后超过内联阈值的结果 fail-closed，不降级写入 Mongo。
- Workflow Run 事件流默认每 `10s` 心跳、单连接 `2m` 后要求游标重连；Redis Stream 默认最多保留约 `2000` 条并设置 `24h` TTL。对应配置为 `AGENT_WORKFLOW_EVENT_HEARTBEAT`、`AGENT_WORKFLOW_EVENT_STREAM_WINDOW`、`AGENT_WORKFLOW_EVENT_STREAM_MAX_LENGTH`、`AGENT_WORKFLOW_EVENT_STREAM_TTL`。
- Agent OTel 使用 `JAEGER_COLLECTOR_ENDPOINT` 的 OTLP gRPC `4317`；`AGENT_TRACE_SAMPLE_RATIO` 范围为 `0-1`，默认 `1`。
- Tweet Service 启动时 AutoMigrate `tweet_create_idempotency`；这是加法迁移，回滚旧版本时保留该表以避免丢失重试历史。

`.env.example` 中“MongoDB For Messenger”和“ES Text & Vector”注释是历史描述；实际归属以上述代码事实为准，后续应单独修订模板。

## 4. 启动顺序

1. 基础设施：MySQL、Redis、Mongo、RabbitMQ、Consul、ES、Qdrant、MinIO。
2. 核心 gRPC：User、Tweet、Follow、Messenger、Notification、Auth。
3. Consumer/Canal Relay。
4. Agent（LM Studio/云模型可按功能选择启动）。
5. Gateway。
6. Web/Mobile。

Consumer 默认在 `2116` 暴露 Prometheus；Compose 由 `consumer:2116` 抓取，Helm Pod 使用注解发现。该端口只提供指标，不是业务入口。

Temporal、Jaeger、Prometheus、Grafana、Loki、Sentinel、Pyroscope 可按开发场景启用；Agent 对 Temporal 连接失败有降级路径，但对应后台能力会不可用。

## 5. 常用验证

```powershell
# Go 全仓（按任务缩小范围；大型仓库不应每次都全跑）
$env:GOFLAGS='-mod=mod'
$env:GOCACHE='E:\GOProject\cloud\twitter-clone\tmp\go-build-cache'
go test ./internal/module/<target>/... ./cmd/<target> -count=1

# Agent 共享边界变更
go test ./internal/module/agent/... ./cmd/agent-service -count=1
go test -race ./internal/module/agent/<changed-packages> -count=1
go vet ./internal/module/agent/... ./cmd/agent-service

# Web
Set-Location web
npm run build

# Mobile
Set-Location mobile
flutter analyze
flutter test
```

受限执行环境中，Go 1.25.5 工具链/标准库可能位于工作区外；测试应使用工作区 `GOCACHE`，必要时申请沙箱外执行，不能把权限失败误判为业务编译失败。

## 6. 调试边界

- LM Studio `connection refused`：先区分宿主/容器网络和服务监听地址。
- 需要用户启动 LM Studio 或其他本地软件时，先执行上面的交互确认流程；不要自行假设软件应当常驻，也不要在用户未确认时反复探测。
- `Invalid model identifier`：使用 Provider 实际已加载的模型 ID；Chat 选择器不得包含 Embedding 模型。
- ES 正常但语义检索失败：继续检查 Embedding 和 Qdrant，不要只看 ES。
- Agent 工作流失败：依次检查 Workflow Run、节点错误、Tool/MCP、Provider，再检查模型输出。
- 修改端口/环境变量时同步 `.env.example`、Compose、代码 fallback 和环境 Context。
