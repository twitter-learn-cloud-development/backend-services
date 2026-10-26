# 小栖 · 用户端助手 Demo

Vue 3 + TypeScript + Vite，桌面三栏和手机布局。默认界面演示无需账号、模型或 Docker；真实连接使用现有 Twitter 用户的邮箱/密码和已运行的 8000/9638 服务。

从 Python 项目根目录启动：

```powershell
.\scripts\frontend.ps1 start
# Docker 重启后，同时恢复现有本机依赖：
.\scripts\frontend.ps1 start -RestoreStack
.\scripts\frontend.ps1 status
```

访问 [http://127.0.0.1:5174/](http://127.0.0.1:5174/)。首次启动需要 Node 22.12+（或 Vite 7 支持的 Node 版本）和 npm；脚本在本目录运行 `npm ci`。Ctrl+C 停止前端，后端容器保持运行。脚本只启动已存在的指定容器，不创建/重建栈、轮换密钥或重置数据。

直接开发与验证：

```powershell
cd frontend
npm ci
npm run dev
npm run build
npm test
```

`scripts/browser-check.cjs` 使用 Playwright 与本机 Edge，检查桌面/手机、演示发送、模式隔离、模拟真实 API 的首轮/续聊/503 重试、账号退出清理。可把可用的 `node_modules` 路径写入 `PLAYWRIGHT_MODULE_DIR`，脚本从中加载 `playwright`；否则要求当前环境可解析该包。截图和报告生成在 Git 忽略的 `artifacts/`。浏览器夹具测试不使用真实凭据、不调用模型，不等于真实模型验收。

功能：自然语言输入、显式示例问题填入草稿、连续对话、答复复制、状态与证据引用、连接设置、用户登录、停止等待、同请求幂等重试。输入法组合键不会误发；Shift+Enter 换行。JWT、账号和对话仅存于内存，刷新会清空，界面会话列表并非服务端历史列表。

当前边界：JSON 请求；没有假造的工具阶段或逐 Token 流式输出；不把 `answer` 解析为结构化推文卡片；没有证据详情接口；`npm run preview` 只预览静态界面，真实 API 代理仅在 `npm run dev` 中启用。视觉字体使用在线字体作为渐进增强，加载失败时采用系统中文字体。

设计和后续集成蓝图见 [`../docs/frontend-blueprint-v1.md`](../docs/frontend-blueprint-v1.md)。

## 全站右下角客服

推特 Web 的 `App.vue` 已挂载 `SupportWidget.vue`，访问 [本机推特](http://localhost:5173/) 后，在任意路由点击「问问小栖」。它复用现有登录账号，无需再次输入密码。收起与路由跳转保留会话；退出或换账号清空会话并废弃旧响应。5174 独立演示也提供同款入口。

当前为本机交付：推特前端 `VITE_SUPPORT_ASSISTANT_URL` 默认 `http://127.0.0.1:5174/`；助手前端 `VITE_TWITTER_WEB_URL` 默认 `http://127.0.0.1:5173`。部署到其他 Origin 时必须同步配置两个构建变量、API 反代和精确 Origin 允许列表，不能直接沿用本机地址。iframe 会话消息严格校验 source/origin，JWT 不进入 URL 或助手 Storage；原站已有的登录存储机制没有因此变成内存认证。

`scripts/widget-check.cjs` 是窗口、身份隔离和引用安全的受控浏览器夹具；`scripts/live-widget-check.cjs --live` 是显式真实验收，会创建两个隔离用户、一条测试推文，并执行两次模型问答。报告不保存凭据，测试数据保留。不要把夹具结果当成真实模型结果。

6 篇使用指南位于 `../knowledge/user-guide/`，覆盖 Workflow 与日常社交操作，通过现有 `knowledge.search_published` 能力检索。根目录执行 `uv run python -m scripts.publish_user_guide` 仅预检；加 `--publish` 才使用配置的 Embedding/Qdrant 更新专属文档 ID，不清空其他文档。
