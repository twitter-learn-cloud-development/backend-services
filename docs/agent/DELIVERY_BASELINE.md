# Agent 可审查交付基线

> 状态：In progress / Batches 00-04 committed
> 基准提交：`c789212`
> 审计日期：2026-08-03
> 目标：把长期混合工作区转换为可审查、可回滚的 Agent 产品基线，不重写历史、不混入用户无关改动。

## 1. 当前事实

在加入 `/.codex-tmp/` 忽略规则前，完整状态包含 24,903 项，其中 24,280 项是仓库内 Go Build Cache。缓存隔离后，真实源码、配置、文档和用户文件状态为 623 项：

| 类型 | 数量 | 处理 |
|------|-----:|------|
| 可归类工程变化 | 607 | 按下述九个堆叠批次审查 |
| Mobile | 10 | 当前 Web Agent 收口不处理、不暂存 |
| 临时脚本 | 1 | `scratch_fix_jaeger.py`，来源确认前不暂存 |
| 用户课程报告删除 | 3 | 不属于工程基线，不暂存、不恢复 |
| 学习分支文件 | 2 | `docker-compose-learn.yaml`、`docs/learn/**` 独立处理 |

本清单只是工作区审计结果，没有执行 `git add`、`git commit`、reset、checkout、stash 或文件删除。

## 2. 必须排除的路径

建立基线时禁止使用无路径限制的 `git add -A`。以下路径必须保持在 Agent 提交之外：

```text
mobile/**
scratch_fix_jaeger.py
docker-compose-learn.yaml
docs/learn/**
（6）课程设计报告（含任务书).doc
（6）课程设计报告（含任务书) (1).doc
（6）课程设计报告（含任务书).pdf
.codex-tmp/**
```

`mobile/android/build/reports/problems/problems-report.html` 是已跟踪生成报告，本轮既不提交也不擅自回滚。

## 3. 堆叠审查批次

这些批次按依赖顺序应用。每个批次只承诺在其全部前置批次存在时可验证，不伪造彼此独立的历史。

| 顺序 | 批次 | 审计桶数量 | 责任边界 | 前置 |
|------|------|-----------:|----------|------|
| 00 | Repository Governance | 34 | `.agent -> .agents` 原生 Skill 迁移、规则、Context、Subagent、`AGENTS.md`、忽略规则 | 无 |
| 01 | Platform Reliability | 72 | Tweet 幂等、Outbox、Timeline/MQ Retry-DLQ、服务身份、风控跨服务边界及 Auth 明确正确性修复 | 00 |
| 02 | Agent Core | 146 → 53 | Runtime、Message、Model、Credential、基础 Repository/Service、预算和上下文 | 01 |
| 03 | Workflow and RAG | 47 → 52 | DSL/IR/Engine/Tool、Checkpoint/Replay、ES/Qdrant、Router/RAG 评测入口 | 02 |
| 04 | Search and MCP Connectors | 61 → 62 | Brave/Page Read、远程 MCP、项目权限、Session Pool、健康和验收工具 | 02、03 |
| 05 | Profile and Eval | 98 | Profile/Prompt 版本、Object Store、Agent Eval、质量门禁和受控归档 | 02、03、04 |
| 06 | Unified Agent Product | 64 | `RunAgent`、权威 Run、Workflow-as-Tool、Skill、模板、受限 Multi-Agent、产品事件和 Marketplace 控制面 | 02 至 05 |
| 07 | API and Web Surface | 48 | Proto/gRPC、Gateway、统一助手、审批/MCP/Profile/Workflow/Marketplace 页面 | 06 |
| 08 | Deployment and Evidence | 37 | Agent 组合根、跨服务 OTel 默认值、Compose/Helm、Grafana、环境示例、API/进度/运行手册 | 01 至 07 |

数量来自创建本文件前的 `git status --short --untracked-files=all`。本文件加入后文档桶会增加一项；数量只用于防止漏项，不作为代码质量指标。

01 实际暂存前按依赖重新审查：`internal/module/auth/grpc/auth.go` 的不可达重复返回属于平台正确性并保留在 01；`cmd/auth-service/main.go` 只有与其他服务一致的 OTel Collector 默认地址变化，改归 08。因而 01 从 73 调整为 72，08 从 36 调整为 37，总量仍为 607。

02 的 146 项来自文件名级初始分类，不是可独立编译的依赖闭包。实际按索引快照拆成 43 项 Runtime Foundation（`79ceb30`）、7 项 Runtime 持久化适配（`b23e164`）和 3 项安全 Provider HTTP 路由（`d4cd553`），共 53 项。其余 93 项仍在工作区，因直接依赖 Workflow、RAG、WebSearch、MCP、Profile 或 Unified Agent 组合根，改由 03 至 06 在各自依赖首次闭合时接收；后续批次实际数量在精确暂存时同步更新，607 总审计量不变。

03 实际按依赖闭包拆成 17 项确定性 DAG Core（`70d696e`）、9 项分层 RAG/Memory（`42b889b`）、15 项 Tool 治理（`febf094`）和 11 项 Workflow/Agent Run Repository（`5708150`），共 52 项。Tool 内置 WebSearch/PageRead 直接依赖 Search 领域，故先以 04A 的 14 项只读 Search/Evidence 原语提交 `dc753cd`，再提交 Tool；这是依赖顺序纠偏，不代表远程 MCP 或用户连接已经进入 04。Workflow Service、Cognitive/Session 和统一入口适配继续等待 MCP/Profile/组合根依赖，在 06 做跨域集成审查。

04 最终由 14 项 Search/Evidence 前置（`dc753cd`）和 48 项 Connector 闭包组成，共 62 项。Connector 闭包依次提交项目级访问治理（`1214f65`）、远程 MCP Runtime（`15f9a63`）、Connector 持久化（`f8e52e7`）、内部 MCP 安全工具面（`cd32cf0`）和签名验收工具（`9d3f547`）。纯 `HEAD` 快照中的 10 个目标包普通测试与 Vet 全部通过，并在各索引快照完成并发敏感包 Race；验收仅使用临时回环 Conformance 服务，未连接真实 MCP、Brave、Mongo、Redis、模型或公网。`service/external_mcp.go`、`service/provider_config.go` 和 `service/web_search_provider_context.go` 属于 06/07 的统一产品与 API 组合根，不是 04 的领域闭包欠账。

### 3.1 跨阶段热点文件

以下文件承载多个历史阶段，禁止为了制造漂亮提交而盲目按 hunk 拆分：

| 文件 | 当前 tracked diff | 基线归属 |
|------|------------------:|----------|
| `internal/module/agent/service/agent_service.go` | `+1080/-80` | 06 Unified Agent Product，依赖包全部就位后一次审查 |
| `internal/module/agent/service/workflow_service.go` | `+491/-57` | 03 Workflow and RAG |
| `internal/module/agent/repository/agent_repo.go` | `+266/-44` | 03 Workflow and RAG；其构造器与索引已直接绑定 Workflow Revision/State、Tool Governance 和 Agent Run 集合，随最早完整 Repository 闭包审查 |
| `internal/module/agent/service/risk_control.go` | `+342/-62` | 01 Platform Reliability |

如果热点文件在对应批次无法通过编译，只允许按明确依赖移动整个文件或最小完整符号，不按日期猜测历史归属。

### 3.2 生成文件规则

- `api/aiAgent/v1/aiAgent.proto` 与对应 `*.pb.go`、`*_grpc.pb.go` 必须同批进入 07。
- `api/tweet/v1/tweet.proto` 与对应生成文件必须同批进入 01。
- `web/dist`、Go Cache、测试二进制、Coverage 和本地评测输出不得进入任何批次。
- Proto 提交前必须重新生成并比较哈希，禁止手工修改生成代码。

## 4. 验证矩阵

| 批次 | 最小验证 | 扩大验证 |
|------|----------|----------|
| 00 | Skill/Context 路径存在；Markdown 与 `git diff --check` | 新任务可发现仓库 Skills |
| 01 | Tweet/Consumer/MQ/ServiceAuth 目标测试 | 对应 Race、Vet、Compose/Helm 路由渲染 |
| 02 | Runtime/Message/Model/Repository/Service 目标测试 | Agent 全包、Service/Repository Race、Vet |
| 03 | Workflow DSL/IR/Engine/Tool/RAG 目标测试 | Scheduler/Tool/RAG Race、离线 Router/RAG Eval |
| 04 | WebSearch/Remote MCP/Project/Acceptance 测试 | MCP Conformance；Live 仍需用户明确启动与授权 |
| 05 | Profile/Eval/ObjectStore 测试 | 固定录制结果门禁；不得自动调用付费模型 |
| 06 | Unified Agent、Workflow-as-Tool、Multi、Extension/Marketplace 测试 | Service/Repository Race、Agent 全包 |
| 07 | gRPC/Gateway 契约测试、Proto 稳定生成、Web Build | 桌面/移动视口 UI 冒烟 |
| 08 | 两个命令组合根编译、Compose Config、Helm Lint/正负向模板 | Grafana 查询与最小启动 Runbook |

最终栈统一执行：

```powershell
$env:GOCACHE="$PWD\.codex-tmp\go-cache"
$env:GOTMPDIR="$PWD\.codex-tmp\go-tmp"
go test -vet=off -p=1 ./... -count=1
go vet -p=1 ./...
Set-Location web
npm run build
```

Race 继续按包拆分，避免 Windows 冷缓存下单条命令长时间无反馈。真实 Mongo、Redis、RabbitMQ、MinIO、Brave、模型和 MCP 不由验证命令隐式启动。

## 5. 暂存与提交护栏

1. 只使用显式 Pathspec 暂存当前批次，完成后立即检查 `git diff --cached --name-status`。
2. 检查暂存区不得包含第 2 节路径、`.env`、凭据、评测正文、Build Cache 或生成报告。
3. 每个批次在提交前保存测试摘要；失败先更新 `docs/ISSUES.md`，修复并复验后再提交。
4. 批次之间不使用 `git reset --hard`、`git checkout --` 或自动清理命令。
5. 用户已于 2026-08-03 授权分批暂存和提交；00 Repository Governance 为 `ce9ad55`，01 Platform Reliability 为 `849cb56`，02 Agent Core 为 `79ceb30`、`b23e164`、`d4cd553`，03 Workflow/RAG 为 `70d696e`、`42b889b`、`febf094`、`5708150`，04 Search/MCP 为 `dc753cd`、`1214f65`、`15f9a63`、`f8e52e7`、`cd32cf0`、`9d3f547`；后续批次仍须逐批审计和验证。

建议提交标题：

```text
chore(repo): establish agent development governance
fix(platform): harden async delivery and service boundaries
feat(agent): establish governed runtime foundation
feat(agent): persist runtime context and traces
feat(agent): route providers through safe traced HTTP
feat(workflow): add durable dag execution and rag controls
feat(workflow): establish deterministic dag core
feat(rag): harden layered retrieval and memory isolation
feat(workflow): govern tool execution and approvals
feat(workflow): persist governed run state
feat(search): add governed web retrieval primitives
feat(agent): add governed web and mcp connectors
feat(agent): add versioned profiles and evaluation gates
feat(agent): unify assistant workflows and extension trust
feat(web): expose governed agent product surfaces
chore(deploy): wire agent runtime controls and evidence
```

## 6. 完成定义

可审查交付基线只有在以下条件全部满足后才从 `Prepared` 改为 `Complete`：

- 九个批次都有精确暂存清单，排除路径为零命中。
- 每个批次的最小验证有真实结果，最终栈回归通过。
- 所有提交可从 `c789212` 按顺序应用，并能在最终提交启动 Web 与 Agent 组合根。
- 工作区剩余变化只包含第 2 节明确保留的用户/学习文件，或有单独归属说明。
- 用户明确同意后才执行暂存和提交。
