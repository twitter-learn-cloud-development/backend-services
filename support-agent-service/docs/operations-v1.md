# V1 运行与故障排查

更新时间：2026-09-29

本机联合交付的推荐入口、全量验收副作用及日常控制命令见 [`local-delivery-v1.md`](local-delivery-v1.md)。本文件下方的独立开发启动和普通 `compose.yaml` 不等同于真实 Go 联合栈。

## 1. 本地启动

安装依赖并运行离线服务：

```powershell
uv sync
uv run uvicorn app.main:app --host 127.0.0.1 --port 8000
```

默认使用内存会话存储，不连接 MongoDB、Qdrant 或 OTLP；未配置真实模型、用户 JWT 和 Go Query 时，这只验证进程与基础 API，不能作为真实 Chat 演示。`/health/live` 表示进程存活；`/health/ready` 只在 lifespan 已启动且未进入关闭阶段时返回 `200`。

## 2. 独立开发 Compose 环境

复制环境变量模板，至少替换审计散列密钥：

```powershell
Copy-Item .env.example .env
docker compose up -d --build
docker compose ps
```

本仓库普通 `compose.yaml` 用于独立开发，不等同于 Go `docker-compose.yaml` 与本仓库 `compose.live.yaml` 合并的真实联合栈。普通 Compose 启动 Support Agent、MongoDB、OpenTelemetry Collector 和 Prometheus。Qdrant 使用可选 profile：

```powershell
docker compose --profile knowledge up -d --build
```

启用知识检索时同时设置模型与 Qdrant 所需变量；普通 Compose 的变量名与联合 Live overlay 不完全相同，以各自 Compose 配置为准：

```text
QDRANT_URL=http://qdrant:6333
EMBEDDING_MODEL_NAME=<embedding-model>
MODEL_API_KEY=<provider-key>
```

聊天模型还需要 `MODEL_NAME`；OpenAI-compatible 自建端点同时配置 `MODEL_BASE_URL`。生产认证适配器应由现有 Gateway 注入，不能使用测试 Fake Auth。

真实本机联合运行请用 [`local-delivery-v1.md`](local-delivery-v1.md) 的首次验收入口；日常 `scripts/local.ps1` 只管理现有 Support Agent 容器，不重建/关闭共享 MongoDB、Qdrant 或 Go 服务。首次验收会调用可能计费的外部模型并创建测试用户与推文，不应当作日常启动命令。

## 3. 观测入口

| 入口 | 用途 |
|---|---|
| `GET /health/live` | 进程存活 |
| `GET /health/ready` | 接流量状态；启动和关闭阶段返回 503 |
| `GET /metrics/` | Prometheus 指标 |
| `http://127.0.0.1:9090` | Compose Prometheus |
| `docker compose logs otel-collector` | 本地 Collector debug trace 输出 |

关键指标：

- `support_agent_http_requests_total`、`support_agent_http_request_duration_seconds`
- `support_agent_support_requests_total`、`support_agent_support_run_duration_seconds`
- `support_agent_worker_runs_total`、`support_agent_active_workers`
- `support_agent_model_calls_total`、`support_agent_model_call_duration_seconds`、`support_agent_model_tokens_total`
- `support_agent_capability_calls_total`、`support_agent_capability_call_duration_seconds`

Trace 只记录 route、状态、请求/会话/任务/工具调用 ID 和 Capability 名称，不记录 Authorization、用户消息、工具参数或原始异常。审计主体使用 HMAC 后的短标识。

## 4. 优雅停机

联合 Live overlay 配置 Docker 停机宽限 30 秒，Uvicorn 配置 20 秒 shutdown timeout。应用在 lifespan 收尾时关闭 HTTP/Qdrant/MongoDB 资源并刷新 OTLP BatchSpanProcessor；在途请求期间 readiness 切换的先后顺序和实际优雅停机效果尚未经过专项实测，不能仅凭配置判为通过。

## 5. 常见故障

### `/health/ready` 返回 503

- 应用仍在启动或正在关闭。
- 查看 `docker compose logs support-agent`。
- MongoDB 配置错误通常会在应用构建会话存储时失败。

### Chat 返回 `model_not_configured`

检查 `MODEL_NAME` 与 `MODEL_API_KEY`。模型配置缺失不影响 liveness、readiness 和 metrics。

### Chat 返回 `conversation_temporarily_unavailable`

检查 MongoDB 网络、磁盘与 TTL collection。服务不会把连接串或驱动异常返回给用户。

### Knowledge Observation 为 `capability_temporarily_unavailable`

检查 Qdrant、embedding endpoint 和 `EMBEDDING_DIMENSIONS`。collection 已存在时，维度必须与配置一致。

### 没有 Trace

检查 `SUPPORT_AGENT_OTLP_TRACES_ENDPOINT` 是否包含 `/v1/traces`，以及 Collector 的 4318 端口是否可达。应用会在关闭时刷新批量 Span。

## 6. 安全检查

- 真实密钥不得提交到 Git 或进入报告/日志。用户选择将本机联合栈的提供商和 Mongo 凭证保留在 Go 仓库内、被 Git 忽略的 `.env`；这不等于仓库外存储。
- `/metrics/` 不直接暴露公网，只允许监控网络访问。
- MongoDB 与 Qdrant 使用持久卷；生产环境必须开启认证、TLS、备份和网络隔离。
- 日志抽检不得出现 Bearer Token、用户原文、完整私信、工具参数或异常栈。
