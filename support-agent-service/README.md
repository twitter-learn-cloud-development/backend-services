# support-agent-service

源码位置：主仓库 `support-agent-service/`，与 Go 服务共用 Git；以下命令默认在本子目录执行。迁移与不含密钥的配置预检见 [`docs/repository-sync-2026-10-26.md`](docs/repository-sync-2026-10-26.md)。

面向 Twitter Clone 用户端的智能客服服务，使用 Python、LangChain 和 LangGraph 实现 Supervisor–Worker 调查链路。

当前阶段：V1 本机联合交付。R0～R7 主链路和真实依赖关键路径已通过；R8 的服务器生产准入与部分可靠性门禁递延。详见 [`docs/local-delivery-v1.md`](docs/local-delivery-v1.md)。

当前产品定位：**小栖只做使用向导**，站内内容任务转交 AI 助手继续尝试，不直接
查询账号内容或进行站内分析。当前质量检查为 190 项 Python、32 项前端测试及
构建通过；分工、跳转安全边界与最新专项验收见
[`docs/guide-role-v1.md`](docs/guide-role-v1.md)。下方调查链路与旧内容查询记录为
底层能力/历史阶段说明，不扩大正式小栖的知识库能力目录。

设计文档：[`docs/architecture-v1.md`](docs/architecture-v1.md)

开发计划：[`docs/development-plan-v1.md`](docs/development-plan-v1.md)

分轮计划：[`docs/iteration-plan-v1.md`](docs/iteration-plan-v1.md)

接口文档：[`docs/api-contract-v1.md`](docs/api-contract-v1.md)

OpenAPI 契约：[`contracts/support-agent-v1.openapi.yaml`](contracts/support-agent-v1.openapi.yaml)

开发进度：[`docs/progress-v1.md`](docs/progress-v1.md)

回答一致性与最新真实复验：[`docs/response-review-v1.md`](docs/response-review-v1.md)。
内容与 Workflow 复合请求分别展示可靠记录、操作说明和能力限制；无法查询的
后台/运行故障数据不会被伪装成已经解决。该历史阶段为 179 项 Python、29 项前端
测试；当前向导版的 190/32 项检查见上方角色验收，不代表服务器生产或故障门禁全部通过。

运行排障：[`docs/operations-v1.md`](docs/operations-v1.md)

本机交付：[`docs/local-delivery-v1.md`](docs/local-delivery-v1.md)

用户端 Demo 与设计蓝图：[`frontend/README.md`](frontend/README.md)、[`docs/frontend-blueprint-v1.md`](docs/frontend-blueprint-v1.md)。运行 `scripts/frontend.ps1 start -RestoreStack` 可在 Docker 重启后恢复现有依赖并启动 `http://127.0.0.1:5174/`。

评测说明：[`docs/evaluation-v1.md`](docs/evaluation-v1.md)

验收记录：[`docs/acceptance-v1.md`](docs/acceptance-v1.md)

## 核心原则

- Supervisor 理解问题、规划任务、汇总结论并生成客服表达。
- Planner 自主拆解问题，不在代码中维护固定问题分类或解决方案路由。
- Generic Worker 根据任务从统一 Capability Registry 动态选择只读工具调查事实。
- LangGraph 负责状态、动态并行、条件路由和会话恢复。
- LangChain 负责模型适配、Tool Calling 和 Structured Output。
- 用户侧收敛为一个 `/api/v1/support/chat` 入口；Go 侧通过统一 Support Query 协议承接 Agent 能力调用。
- 原有 Go 服务继续拥有业务数据与权限判断；Python 服务不直连业务数据库。
- V1 只处理当前登录用户可见的数据，不包含管理员能力和写操作。

## 当前运行配置

默认配置使用 `InMemorySaver`，可直接运行全部离线测试。生产多轮会话与知识检索需要配置：

```text
SUPPORT_AGENT_MONGODB_URI=mongodb://...
SUPPORT_AGENT_MONGODB_DATABASE=support_agent
SUPPORT_AGENT_QDRANT_URL=http://qdrant:6333
SUPPORT_AGENT_QDRANT_API_KEY=...
SUPPORT_AGENT_EMBEDDING_MODEL_NAME=text-embedding-3-small
SUPPORT_AGENT_EMBEDDING_DIMENSIONS=1536
```

`conversation_id` 直接映射为 LangGraph `thread_id`；MongoDB checkpoint 和会话归属记录默认保留 7 天。Qdrant 只写入 `published=true` 的用户可见文档，检索结果通过普通 `knowledge.search_published` Capability 进入 GenericWorker。

## 当前交付目标

先完成 Windows + Docker Desktop 的本机联合运行与演示，不把公网域名、TLS、服务器部署和生产 `1.0.0` 准入混入本阶段。真实 Chat、续聊和知识检索依赖父目录的主站服务及外部 DashScope 模型；它不是完全离线程序。首次验收、日常状态/启动/停止及副作用说明见 [`docs/local-delivery-v1.md`](docs/local-delivery-v1.md)。

用户端可在 [本机推特 Web](http://localhost:5173/) 的任意页面点击右下角「问问小栖」，复用现有登录账号。独立 [助手工作台](http://127.0.0.1:5174/) 保留演示/真实模式。可信推文引用、全站收缩窗口、使用指南知识库及实际验收范围见 [F2 记录](docs/frontend-acceptance-v2.md)；启动说明见 [前端 README](frontend/README.md)。

## 本地开发

```powershell
uv sync
uv run uvicorn app.main:app --reload
```

质量检查：

```powershell
uv run ruff format --check .
uv run ruff check .
uv run pyright
uv run pytest
uv run python -m app.evals.runner
```
