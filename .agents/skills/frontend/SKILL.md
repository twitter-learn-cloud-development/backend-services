---
name: frontend
description: 维护真实可用的跨端交互、持久状态和 API 契约，不用前端临时状态掩盖后端缺陷。用于 Vue Web、Flutter Mobile、路由、状态、无限滚动、AI 助手或 Workflow Editor UI 任务。
---

# Frontend Skill

## 先读

- Web：`router -> views -> components -> stores -> api`
- Mobile：`features -> presentation/application/data -> core`
- 涉及接口时读取 API Contract Skill。
- 涉及画布时读取 Workflow Skill。

## 执行步骤

1. 复现并明确数据源、Route、Store、请求参数和响应状态。
2. 区分 Server State、页面本地状态和持久用户配置。
3. 定义 Loading、Empty、Error、Retry、End-of-list 和并发请求去重。
4. 保存型页面必须在重新进入/刷新后回显后端真实数据。
5. 返回按钮优先使用 Router History，并为无历史入口提供安全 fallback。
6. 无限滚动使用 Cursor + IntersectionObserver/Scroll Controller，直到 `has_more=false`。

## Agent UI 不变量

- 切换 Chat/Consult/Assist/Multi/Workflow 不应隐式创建新 Dialogue，除非用户显式新建。
- 左侧 Dialogue 与右侧 Message 必须以同一 Dialogue ID 为事实源。
- 模型选择器只显示 Chat-capable Model。
- 自定义 Workflow 必须明确选中具体 Workflow，不默认静默取第一条。
- Credential 字段只显示 Reference/配置名，绝不回显明文 Key。

## Workflow Editor 不变量

- Node/Edge 使用稳定 ID，尺寸和 Handle 位置不因文本改变而漂移。
- Edge 可选中并删除；保存/重载后拓扑和属性一致。
- Chat、Writer、Publish、Search、Router、Wait、End 组件语义明确。
- 运行错误定位到 Node，并提供真实错误，不伪造后台进度。

## 验证

- Web：`npm run build`，必要时用 Playwright 检查桌面/移动视口。
- Mobile：`flutter analyze`、`flutter test`。
- 检查长文本、空列表、慢请求、重复点击、返回导航和刷新恢复。
- 不用纯截图判断数据正确；结合网络响应和持久化状态。
