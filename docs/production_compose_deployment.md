# Production Compose Deployment

该覆盖面向单机演示和小规模 Beta。它不替代 Kubernetes，也不把本地开发 Compose 伪装成生产配置。

## 1. 交付边界

生产启动必须同时使用：

- `docker-compose.yaml`：服务拓扑；
- `docker-compose.production.yaml`：生产覆盖；
- 仓库外的私密环境文件：镜像 digest、密码、API Key、加密主密钥和公开域名；
- 仓库外的 TLS 证书、TLS 私钥与 Alertmanager Webhook Token 文件。

生产覆盖具有以下约束：

- 所有 31 个镜像必须使用 `repository@sha256:<digest>`，禁止 `latest` 和可变 tag；
- 应用服务禁用本地 build，部署只拉取已审计镜像；
- 只有 `edge-proxy` 发布宿主机 `80/443`；Frontend、Gateway 和基础设施均不发布宿主机端口；
- 所有服务配置资源限制、`unless-stopped` 与 `no-new-privileges`；
- 关键数据和业务服务配置健康检查；
- Redis、RabbitMQ、数据库、Mongo、MinIO、JWT 和内部 Tweet RPC 不使用开发凭据；
- Agent/Consumer 强制 `MOCK_EMBEDDING=false`，不依赖 LM Studio 或 `host.docker.internal`；
- Gateway CORS 与 WebSocket 共用精确 HTTPS Origin 白名单，不允许 `*`；JWT 验证固定 RS256、`exp`、Issuer 与 Audience；
- Prometheus 加载生产告警规则并投递到 Alertmanager；Loki、Prometheus、Alertmanager 与 Jaeger 均配置有界保留期。

## 2. 准备私密环境文件

复制 `deploy/compose/production.env.example` 到仓库外由部署用户独占的路径，例如 Windows 的 `%USERPROFILE%\twitter-clone-secrets\production.env`。不要把真实值写回示例文件。

要求：

1. 从镜像仓库取得每个镜像的真实 sha256 digest。
2. 密码至少 20 个随机字符，JWT 与内部服务令牌至少 32 个随机字符。
3. Provider Config 与 Run Checkpoint 主密钥必须分别是 32 个随机字节的 Base64 编码。
4. `JWT_ISSUER`、`PUBLIC_BASE_URL`、`AGENT_LLM_BASE_URL` 与 `MINIO_PUBLIC_URL` 必须是非回环 HTTPS URL；`JWT_AUDIENCE` 必须明确填写。
5. `GATEWAY_ALLOWED_ORIGINS` 必须包含 `PUBLIC_BASE_URL` 的 Origin，且每项都是无路径、无查询参数的 HTTPS Origin。
6. TLS 证书有效期必须超过 14 天，证书与未加密 PEM 私钥必须匹配；三个 Secret 文件均必须位于仓库外。
7. 初次部署保持 Planning、External MCP 和 Workflow 开关关闭。

Windows 可生成 Base64 主密钥：

~~~powershell
$bytes = New-Object byte[] 32
[Security.Cryptography.RandomNumberGenerator]::Fill($bytes)
[Convert]::ToBase64String($bytes)
~~~

宿主机应使用 ACL 限制环境文件只允许部署账户读取。环境变量仍可能被宿主机 Docker 管理员查看，因此主机管理员属于受信任边界；更高等级部署应改用 Kubernetes Secret/外部 Secret Store。

## 3. 离线预检

~~~powershell
./scripts/validate-production-compose.ps1 -EnvFile "$env:USERPROFILE\twitter-clone-secrets\production.env"
~~~

预检不会访问镜像仓库或启动容器，只会读取外部 Secret 文件的必要属性并在内存中渲染 Compose 配置。成功输出仅包含服务数、镜像数和公开主机名，不打印任何 Secret。

## 4. 启动与检查

预检通过后才允许拉取和启动：

~~~powershell
docker compose --env-file "$env:USERPROFILE\twitter-clone-secrets\production.env" `
  -f docker-compose.yaml -f docker-compose.production.yaml pull

docker compose --env-file "$env:USERPROFILE\twitter-clone-secrets\production.env" `
  -f docker-compose.yaml -f docker-compose.production.yaml up -d --no-build

docker compose --env-file "$env:USERPROFILE\twitter-clone-secrets\production.env" `
  -f docker-compose.yaml -f docker-compose.production.yaml ps
~~~

只有 `edge-proxy` 应出现 Published Port。不得为了调试临时公开 Gateway、Frontend、MySQL、Mongo、Redis、RabbitMQ、MinIO、Temporal 或可观测组件。

## 5. TLS、公网入口与身份边界

`edge-proxy` 是 Compose 唯一公网入口：HTTP 只做 HTTPS 跳转，HTTPS 转发页面、API 与 WebSocket，并设置 HSTS、`nosniff`、Frame、Referrer 和 Permissions Policy。证书与私钥通过 Compose Secret 只读挂载，不进入镜像或环境变量。

Gateway 在生产环境启动时要求 JWT Issuer/Audience 和至少 32 字符的 Alertmanager Token；`/alerts` 仅接受 `Authorization: Bearer <token>`，Token 从 Secret 文件加载。主应用当前使用 `Authorization` Bearer JWT，不使用认证 Cookie；Grafana Cookie 强制 Secure 与 Strict SameSite。未来若把主应用迁移到 Cookie，必须另行补齐 HttpOnly、CSRF 和会话撤销设计，不能把当前配置视为已完成 Cookie 认证迁移。

域名 DNS、受信 CA 证书、证书自动续期和真实浏览器 Origin/WebSocket 验收仍属于目标环境证据，静态预检不能替代。

## 6. 告警、保留与备份

- Prometheus 规则覆盖核心 Target、HTTP 5xx、RabbitMQ backlog、LLM/Provider、Tool、MCP、Planning、Session Summary 和估算费用异常。
- Promtail 不再把 `trace_id` 提升为 Loki Label，避免高基数索引；各观测组件保留期见 `docs/data_retention_policy.md`。
- 备份介质、恢复顺序、RPO/RTO 和演练证据见 `docs/production_backup_restore.md`。
- `scripts/production-backup-manifest.ps1` 只负责对七类备份产物生成/验证 SHA-256 清单，不负责创建数据库快照，也不等于恢复成功。

## 7. 回滚

1. 保留上一个通过预检的环境快照和镜像 digest 集。
2. 使用旧环境文件重新运行预检。
3. 执行 `docker compose ... up -d --no-build`，只切换镜像，不删除 Volume。
4. 验证健康状态、Agent Run/Checkpoint 和用户可见数据后再归档故障版本。

禁止通过 `down -v`、删除数据库目录或清空队列来完成回滚。

## 8. 尚未完成

- MySQL、Mongo、Redis、Qdrant、ES、MinIO 的真实备份创建与恢复演练；
- 目标域名、受信证书、续期、浏览器 Origin/WebSocket 与外部 Alertmanager 回调验收；
- 告警阈值在真实流量下的调优与通知接收人确认；
- 对话、Checkpoint、Tool Result、Eval 和对象存储数据的应用级删除任务；
- 使用真实镜像与 Secret 的 clean-host 启动和服务重启证据。

这些项目属于后续 L4/L5 批次，本文件和预检通过不能替代真实部署证据。

真实执行结果统一登记到 `docs/production_launch_acceptance.md` 定义的签名验收记录；未绑定证据的 `passed` 会被脚本拒绝，任一 Required Check 未通过时严格发布门禁返回非零。
