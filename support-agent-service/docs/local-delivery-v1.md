# V1 本机联合交付

更新时间：2026-10-26（源码目录并入主仓库，历史 Live 证据日期不变）

状态：当前机器的本机联合运行版已就绪；全新机器安装与服务器生产准入未验收。

## 交付范围

2026-10-05 当前用户体验改为使用向导：小栖只检索已发布指南；站内实际任务由
用户自行前往 `/agent` 继续尝试。旧近期推文 Chat 验收属于历史调查阶段。
最新角色与真实专项门禁见 [`guide-role-v1.md`](guide-role-v1.md)。

源码已并入 `twitter-clone/support-agent-service/`，与 Go 服务共用主仓库 Git，仍有独立 Python 依赖、前端和 Dockerfile。用户侧只有 `POST /api/v1/support/chat`；真实用户认证、MongoDB 会话、Qdrant 知识检索和 Jaeger Trace 使用本机联合栈，Go 内容查询仅保留历史验收代码。模型与 Embedding 仍调用外部 DashScope，因此“本机交付”不等于离线交付。旧相邻目录保留作迁移备份，不是后续代码修改入口。

本阶段不宣称服务器部署或生产准入：公网域名/TLS、反向代理、公网 `/internal/` 隔离、服务器密钥分发、备份恢复和容量验证均递延。真实 429/503/504 故障注入、Go 开关回滚、运行中旧客户端恢复和在途请求优雅停机仍是可靠性待办，不作为本机演示启动的阻塞项，也不记为通过。

## 首次搭建与验收

前提：Docker Desktop Linux Engine 正常运行；PowerShell 和 `uv` 可用；父目录（主仓库根）的 Git 忽略 `.env` 已配置 `DASHSCOPE_API_KEY` 与 `MONGO_PASSWORD`。该文件不迁移或复制。不要把它、容器环境或完整 Compose 展开结果贴进日志或报告。

在本项目根目录运行：

```powershell
.\scripts\run_live_acceptance.ps1
```

该命令是**全量验收**，不是日常启动：它会构建/启动联合服务、生成新的内部服务与审计密钥、调用可能计费的模型与 Embedding、创建 2 个隔离测试用户和 1 条测试推文，并在 `dist/live-acceptance/` 写入报告。测试用户和推文默认保留；不要为了重跑而清空共享数据库。需要使用其他目录时传入 `-TwitterRepo` 和 `-SupportRepo`。

此脚本的内容调查断言对应旧角色，尚未改成向导版，不作为当前准入命令，也不要
为启动现有系统重跑它。当前机器继续使用下方 frontend/local 脚本恢复已有容器；
全新机器搭建仍未验收。

验收通过的实际范围见 `docs/live-acceptance-v1.md` 和 `docs/progress-v1.md`；报告中的 `passed` 不代表本节列出的生产或故障门禁已经通过。

## 日常控制现有容器

用户体验入口：[主站](http://localhost:5173/)。正常登录后，从全站右下角展开
“小栖”客服；主站复用登录身份，不需在 Swagger 手工粘贴 JWT。助手前端目前是
独立的 Vite 进程，使用 5174，主站客服嵌入它。Docker 已有容器停止或重启后，
从本项目根目录执行：

```powershell
.\scripts\frontend.ps1 start -RestoreStack
```

此命令仅启动所列的已有联合依赖和应用容器，不清理数据、创建测试账号或轮换
密钥；不会启动机器上所有其他项目。末尾启动助手开发前端，请保持该终端运行。
若依赖容器缺失，会明确失败，不代替首次搭建。状态检查：

```powershell
.\scripts\frontend.ps1 status
```

完成首次搭建后，以下命令只控制现有 Support Agent 容器，不重建依赖、不轮换服务密钥、不新增测试用户或推文：

```powershell
.\scripts\local.ps1 status
.\scripts\local.ps1 start
.\scripts\local.ps1 logs -Tail 100
.\scripts\local.ps1 stop
```

`start` 要求现有 MongoDB、Qdrant、Auth 和 Gateway 容器已运行；它不会替你启动整套 Go 依赖。`stop` 只停止 Support Agent，保留共享依赖与持久卷。若容器已被移除，应重新执行首次搭建；如仅 Docker Desktop 重启导致其他依赖停止，先按 Go 项目的本机启动说明恢复联合栈，再执行 `start`。查看日志时注意不要转发可能包含用户数据的原始输出。

本机状态检查：

```powershell
Invoke-WebRequest -UseBasicParsing http://127.0.0.1:8000/health/ready
Invoke-WebRequest -UseBasicParsing http://127.0.0.1:9638/health
uv run ruff format --check .
uv run ruff check .
uv run pyright
uv run pytest
uv run python -m app.evals.runner
```

`/health/ready` 只代表 Support Agent 进程可接流量，不是 MongoDB/Qdrant 的深度健康检查。当前 Gateway 和用户 API 在联合 Live overlay 中绑定本机回环地址；其他 Go 基础设施端口不因此自动收紧。真实 Chat 演示应使用正常登录获得的用户 JWT，不使用正文伪造用户身份，也不使用测试 Fake Auth 冒充真实验收。

## 本机交付结论与后续阶段

本机交付的完成条件是：在上述环境可复现启动与停止、真实用户 Chat 首轮和续聊含证据、知识检索、质量检查、可定位的验收报告与明确的已知限制。当前联合 Live 关键路径及共享 MongoDB/Qdrant 重启后的持久化和新连接恢复已有证据；`scripts/local.ps1` 的 status/stop/start 已实测。2026-10-01 又运行了一次定向真实 Chat 首轮与续聊回归，将逐条内容 ID、发布时间、可见性与 Gateway 直接查询比对通过；它会创建隔离测试用户和推文、调用付费模型，但未重跑包括知识检索等全部门禁的全量验收。运行中旧客户端恢复未验证。

服务器交付作为下一阶段单独制定环境、网络、备份、安全和可靠性门禁。不得把本机交付状态直接标为生产 `1.0.0` 准入通过。

回答一致性最新实现、真实 HTTP 样本、已保留测试数据与未验收边界见
[`response-review-v1.md`](response-review-v1.md)。专项复验命令
`uv run python -m scripts.live_semantic_http_probe --live` 会新增一个隔离用户、一条
测试推文并调用四次真实 Chat；默认不执行，不应作为日常启动命令。

上述旧调查探针不适用于当前向导角色。向导复验使用
`uv run python -m scripts.live_guide_http_probe --live`：一个隔离用户、三次真实
Chat、零推文，仍不得作为日常启动命令。
