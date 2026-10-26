# 2026-10-26 源码并入主仓库

## 目录与边界

规范源码位置为 `twitter-clone/support-agent-service/`，与主仓库共用 Git，不建立嵌套仓库。
保留 Python 的 `pyproject.toml`、`uv.lock`、Dockerfile、前端锁文件和独立测试；
Go 与 Python 仍为不同服务。旧相邻目录保留原文件与本机依赖作备份，不再作为新开发入口。

未复制 `.env`、虚拟环境、node_modules、缓存、构建结果、Live 报告、临时 PDF/简历图片。
主仓库既有私人文档或产物的历史不在本轮重写范围，忽略规则只防止后续误加入。
已跟踪的 Android 自动生成报告仅移出版本管理，工作区原文件保留；旧镜像部署导出
`docker-compose.deploy.yaml` 保留本机，继续从脚本生成，不纳入本次源码提交。

## 路径变化

- 合并 Compose 以主仓库 `docker-compose.yaml` 为第一个文件，相对构建路径为
  `./support-agent-service`。独立开发的 `compose.yaml` 继续相对本子目录运行。
- 旧全量 Live 脚本默认以客服父目录定位主站；它保留历史调查模式验收，不是当前
  向导的准入或日常启动入口，迁移验证不执行它。
- 从本子目录使用 `scripts/frontend.ps1 start -RestoreStack` 恢复已有本机栈；
  本轮不执行启动命令、不重启容器、不轮换密钥。
- 模型与数据库凭据仍留在主仓库 Git 忽略的 `.env`，不复制到新目录或提交中。

只校验配置、不读取真实 `.env`、不启动服务的示例（主仓库根目录执行）：

```powershell
$env:SUPPORT_QUERY_JWT_SECRET = 'configuration-check-placeholder'
$env:AUDIT_HMAC_KEY = 'configuration-check-placeholder-not-a-secret'
$env:SUPPORT_AGENT_MONGODB_URI = 'mongodb://configuration-check:27017/support_agent_live'
$env:DASHSCOPE_API_KEY = 'configuration-check-placeholder'
$env:DB_PASSWORD = 'configuration-check-placeholder'
$env:MONGO_PASSWORD = 'configuration-check-placeholder'
$env:MINIO_USER = 'configuration-check-user'
$env:MINIO_PASSWORD = 'configuration-check-placeholder'
docker compose --env-file support-agent-service/.env.example -f docker-compose.yaml -f support-agent-service/compose.live.yaml config --quiet
```

## 验证记录

迁入后在新目录重新创建虚拟环境并从锁文件离线安装，未复制旧 `.venv`。实际通过：

- Python：190 项 pytest；ruff check、133 个文件格式检查、pyright 零错误。
- 客服前端：锁文件离线安装、32 项测试及 Vue/TypeScript/Vite 构建。
- 主站 Web：18 项测试及构建。Browserslist 数据过期提示非阻塞，本轮不升级依赖。
- Go：完整 `go test ./... -count=1`、`go vet ./...` 通过。
- Mobile：`flutter analyze --no-pub` 零问题，`flutter test --no-pub` 32 项通过。
- PowerShell：迁移脚本 AST 解析通过；占位凭据下 Compose 合并配置校验通过，
  展开后的 Support Agent build context 精确指向新目录，没有读取真实 `.env` 或启动服务。
- 待提交源码扫描没有命中本机已配置长密钥、常见 Provider/GitHub/AWS Token、JWT
  或私钥块模式；这是本轮有限检查，不是完整历史密钥审计。
- 首轮暂存空白门禁发现历史文档/配置的行尾与 EOF 空白，限定迁入文件机械规范化后
  复验通过；详情见主仓库 `docs/ISSUES.md` 第 249 项。

五条固定离线评测夹具通过，只验证计划/证据/权限等评分协议，不代表真实模型质量。
新机器安装、运行中进程切换、服务器部署、Race 与 GitHub Actions 实际执行分别验证。
2026-10-05 的真实 Live 证据保留原日期，本轮未重跑。

## 提交与远程

主仓库已有开发成果单独记录为 `396cb18`（559 个文件，含生成报告解除跟踪），
本子目录、独立离线 CI 与迁移记录另作提交。目标远程为既有 `origin/main`，
不改 remote、不强制推送、不重写历史；GitHub 测试完成情况与源码推送结果分别报告。

## 回退

旧目录完整保留，运行中的旧容器不受源码复制影响。需要回退时使用旧目录运行方式；
Git 层面可单独回退迁移提交，不回滚用户原有 Go/Web/Mobile 开发成果，不删除持久卷。
