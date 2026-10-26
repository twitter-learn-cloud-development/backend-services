# F2 全站客服与知识指南验收

日期：2026-10-02。范围：本机 Web 客服，不是全项目生产发布。

## 已验证

- Python 全量测试 112 项、ruff、pyright 通过；两套 Vue/TypeScript 构建通过，29 项 API/引用/会话竞态单元测试通过。
- 引用来自校验业务记录；外站、ID/path 不一致、非数字路由、无证据和重复引用拒绝。JSON/SSE response.completed 引用一致，Composer 幻觉清单回归同时核对真实链接。
- 6 篇操作指南发布到现有 Qdrant 专属 document_id，覆盖导航、内容权限、AI/Workflow、工作流步骤/FAQ及发推/回复/点赞/转发/书签/私信。真实 Embedding/检索命中指南；内部源码说明不进入用户检索正文。
- 窗口浏览器夹具通过：4 种合法回答状态、401/403/409/422/429/503/504反馈、临时失败同key/body重试、双向错误Origin/source拒绝、非法导航拒绝、资料未齐全立即撤销身份、同iframe换账号和迟到回复隔离、IME、收缩草稿保留、手机布局。16 次Chat全部被夹具拦截，不调用模型；原工作台浏览器回归与原站登录/注册页全站入口通过。
- Live 浏览器 run `21a702322a`：真实登录、共享身份、近期内容查询、引用幂等回放、原站真实推文详情、保留 iframe/草稿、同会话 Workflow 知识问答、手机布局、匿名 401、跨用户 403 通过。

报告及截图位于 Git 忽略的 `frontend/artifacts/`，没有密码或 JWT。创建的 2 个隔离用户和 1 条推文保留；执行了 2 次真实模型问答。

## 修正与边界

Astra 审查发现并修复未登录真实窗口误用 demo、资料异步加载时旧身份残留、迟到登录覆盖新会话三项竞态。Luna 负责受控浏览器夹具。

Luna 编写夹具后因运行额度中断，主代理接手完成复验：修正测试精确标签/请求捕获，并发现 Vue 响应式账号对象直接 postMessage 会抛 DataCloneError，已改为仅传普通字段快照。29 项单元测试包含即使 transport 忽略 AbortSignal，旧回复也不得覆盖新会话或清除新请求 busy 状态的验证。

中断后 5174 进程曾停止造成浏览器 connection refused，已恢复前端后重跑通过；这是环境中断，不是后端服务故障。

Live Workflow 答复曾使用不够准确的“本地/草稿级持久化”措辞，因此已将创建指南升级到 v2，明确 Save 是当前账号的服务端定义与版本保存，并复验实际向量召回。这不等于全部自然语言叙述已获形式化事实验证。

未执行原 Go Workflow、外部 MCP、发推审批或 Temporal 旅程，也未做负载、真实 429/503/504 故障注入、在途停机、生产 TLS/公网验收。夹具异常覆盖不等于真实故障验收。

面板使用 JSON，没有逐 Token 流式输出；SSE 引用结构已离线验证。刷新会清空助手内存列表，服务器历史仍按现有 TTL 存储，尚无全量历史列表 UI。正文按文本显示，不执行 HTML。

## 复验

### 后续 Workflow 503 回归（2026-10-02）

原句“Workflow 是什么，如何创建和运行？”暴露知识片段聚合误报矛盾，以及空补充
计划被拒绝导致的 503，现已修复并部署。新增回归后全量 Python 测试为 125 项，
ruff、pyright 通过。HTTP Live run `35d9f4dd` 经 5174 前端代理实测 200/answered，
7 条知识依据、无推文链接；同 key/body 重放一致。新增并保留 1 个隔离测试用户，
一次真实模型问答。此项是接口回归，没有重新执行全部浏览器或原 Go Workflow 旅程。
详情见 `docs/progress-v1.md` 的同名修正记录。

### 常规命令

项目根目录：`uv run pytest`、`uv run ruff check app tests scripts/publish_user_guide.py`、`uv run pyright`。
`uv run python -m scripts.publish_user_guide` 仅预检，加 `--publish` 才更新指南 ID。

frontend 目录：`npm test`、`npm run build`、`node scripts/browser-check.cjs`、`node scripts/widget-check.cjs`。
`node scripts/live-widget-check.cjs --live` 会创建测试数据并调用模型；不自动重跑。
`uv run python -m scripts.live_workflow_http_probe --live` 专项复验原句和同请求重放，
同样创建测试账号并调用模型。

浏览器脚本需要 Playwright（PLAYWRIGHT_MODULE_DIR 指向 node_modules）与 Edge。原站用 `http://localhost:5173/` 匹配当前 Gateway 开发 Origin；独立助手为 `http://127.0.0.1:5174/`。
