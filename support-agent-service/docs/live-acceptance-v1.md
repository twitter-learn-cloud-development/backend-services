# 联合 Live 验收手册 v1

## 目标

本验收把 Python `support-agent-service` 与 Go Twitter 主仓库的真实 Auth、Gateway、
Tweet Service、Redis、MongoDB、Qdrant、Jaeger 和 DashScope 串成一条链路。它不使用
Fake Auth 或内存数据库替代 Live 结论。

## 外部前置条件

- Docker Desktop Linux Engine 已启动，预计首次构建与验收需要 10～30 分钟。
- 本机端口 `8000`、`6333`、`8081`、`9638`、`16686`、`27017` 可用。
- Go 主仓库 `.env` 已配置 `DASHSCOPE_API_KEY`、`DASHSCOPE_API_URL`、
  `MONGO_PASSWORD` 及原有 Compose 所需变量。
- DashScope 使用 OpenAI 兼容端点；本次账户可用的聊天模型为 `qwen-plus`，向量模型为
  `text-embedding-v4`，向量维度为 1536。

聊天模型已在本机用相同密钥完成最小调用。`qwen3.6-plus` 因账号免费额度限制返回 403，
因此本轮验收改用 `qwen-plus`；
`text-embedding-v4` 支持 OpenAI 兼容 Embeddings 和 1536 维向量。

## 密钥与测试数据策略

- Support Query HS256 Secret 与审计 HMAC Key 每次运行随机生成。
- Secret 只注入当前 PowerShell 进程和容器环境，不写 `.env`、Compose 实值或报告。
- DashScope Key 与 MongoDB 密码目前从 Go 项目内被 Git 忽略的 `.env` 读取；脚本不打印
  原值，也不会将其写入新文件。这保证密钥不进入版本控制，但不等同于物理上位于仓库外。
- 每次创建两个唯一命名的隔离普通用户和一条样本推文；随机密码仅驻留脚本内存，
  不写报告。用户和推文保留用于后续回归追踪，Qdrant 探针文档在验收后删除。

## 执行

在 Python 项目根目录运行：

```powershell
powershell -NoLogo -NoProfile -ExecutionPolicy Bypass -File scripts/run_live_acceptance.ps1
```

默认保留容器供日志和 Jaeger 检查；如只希望停止 Python 容器，添加
`-StopSupportAgentAfter`。脚本不会删除 Volume、用户或推文。

## 门禁

1. Auth JWKS、Gateway、Qdrant、Support Agent readiness 可达。
2. 真实 Qdrant + DashScope Embeddings 完成写入、检索、清理闭环。
3. 创建两个普通用户；真实 RS256 用户 JWT 可被 Python JWKS 适配器验证。
4. 用户 A 创建推文；内部查询仅返回安全资源引用，不返回正文或 `user_id`。
5. 服务 JWT 重放返回 401；同工具调用不同参数返回 409；伪造用户字段返回 400。
6. 用户 B 不可观察到用户 A 的样本推文。
7. 用户端 Chat 经模型规划、通用 Worker、内部能力获得证据，并使用 MongoDB 延续会话。
8. Prometheus 必需指标存在；审计事件可见；Jaeger 能发现 Support Agent 服务。

脱敏 JSON 报告写入 `dist/live-acceptance/`，该目录已被 Git 忽略。

## 最近一次结果

2026-09-29 报告：`dist/live-acceptance/live-acceptance-20260929170710-195cbd5c.json`。
首轮 Chat 返回 `answered` 与 1 条证据引用，续聊保持同一 `conversation_id`；其余上述
门禁均通过。Windows 系统代理曾使本机续聊请求在到达服务前断开，因此验收脚本对
`127.0.0.1` 的 HTTP 客户端使用 `trust_env=False`。报告不代表 429/503/504 故障注入、
重启恢复、TTL/删除或公网暴露检查已完成。
