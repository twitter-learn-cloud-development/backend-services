# 小栖使用向导与站内 AI 助手分工

生效日期：2026-10-05，由用户确认。保持用户端统一 Chat 接口，不增加问题分类接口。

## 当前分工

小栖解释产品功能、规则、导航和操作方法，如 Workflow 创建/保存/运行、如何使用
站内 AI 助手、如何在页面发推或查看记录。它不执行内容检索、站内统计/分析、
创作、账号记录查询或用户工作流。这类实际任务提示用户前往站内 `/agent` 继续
尝试；是否具备所需能力取决于该助手当前部署和权限，不保证统计或操作成功。

“如何使用搜索”是向导问题，“帮我搜索内容”是实际任务。模型根据语义判断，
不使用关键词分类表。混合请求保留已验证操作说明，只转交任务部分。指南缺失、
模型故障或工具失败不自动转交，以免掩盖当前向导故障。

## 执行与协议边界

- 正式 `build_support_runtime` 只注册 `knowledge.search_published`，不再注册
  `content.list_recent` 或创建 Go Support Query 客户端。旧适配器、测试和底层
  Go 接口保留，不删除相邻项目功能；旧服务配置不能重新扩大小栖的能力目录。
- `GuideTaskPlan.guidance_required` 表示是否包含任何使用说明目标，包括混合
  请求。为 true 时至少有一个证据任务；纯任务请求可为 false、空 tasks，但必须
  保留非空原请求目标并经过独立最终 review，空计划本身不是转交凭据。
- 原请求覆盖评估采用 `handoff` 表示产品分工。向导专用提示 schema 不包含调查
  模式的 `capability_unavailable`；缺少指南用 insufficient_evidence，实际故障
  保留 execution_failure。非法输出继续保守回退，不把失败强行改成转交。
- 纯转交使用原有 `unsupported` 状态，说明小栖不执行此任务，并生成固定
  `suggested_actions: ["open_site_ai_assistant"]`；混合请求为 partial。不是 answered，
  也不表示站内 AI 助手已收到或完成任务。
- 前端只接受上述精确 action；固定映射可信主站 HTTP(S) origin + `/agent`。
  不解析模型提供的 URL，不自动跳转或发送问题、令牌与会话。嵌入窗口优先使用
  已验证父页面 origin，链接由用户点击并在新页打开，使用 noopener/noreferrer。

## 指南与界面

欢迎语、示例问题、演示答复和页脚均改为使用向导，不再承诺查询自己的近期推文。
`user-guide-content-support` 已更新为 `2026-10-05-v2`，说明分工、转交与可见性
概念；通过 `publish_user_guide --publish --document-id user-guide-content-support`
仅替换该文档旧片段，其他五篇指南不修改。发布后 Workflow 和社交操作检索仍命中。

## 回归与限制

受控回归覆盖纯说明、纯转交、混合保留、指南检索故障、空计划漏目标拒绝、
guidance_required 的任务约束，以及配置了旧 Go Query 仍不注册业务能力。
前端覆盖固定 action、无 action 不显示入口、恶意 URL 和查询/凭据不进入跳转地址。
目前 Python 190 项、前端 32 项测试及构建通过，ruff、格式和 pyright 通过。

首轮 HTTP run `480c8f78` 的 Workflow 说明通过，但站内统计问题误用旧能力不足
类型、混合请求没有取得指南证据，严格检查失败；不记为交接通过。随后收紧
向导输出 schema，并增加 guidance_required，避免混合请求直接空计划。

最终真实 HTTP run `40cc0c89` 的三项专项检查均通过：

| 请求 | 结果 | request_id |
|---|---|---|
| Workflow 是什么，如何创建和运行？ | 200 / answered，保留指南来源，无转交动作 | a9e3e13c-1e7d-4a0b-af22-7d2d5724d561 |
| 站内什么类型推文较多 | 200 / unsupported，固定转交入口，无统计结论或来源 | 689ad4bf-3984-454b-9ad5-485ba438e79e |
| Workflow 保存运行说明 + 站内推文类型分析 | 200 / partial，保留操作说明及来源，仅任务部分转交 | dc442ebc-c006-4f41-9190-ac5fbf530480 |

当前镜像已部署并 healthy。本轮两次探针累计新增并保留两个隔离测试用户、
六条真实 Chat，不创建推文、不删除历史数据。检查证明的是向导分工和受控转交，
不是站内 AI 统计能力验收，也不替代浏览器按钮点击或逐个 UI 标签的人工核对。

模型的角色判断仍是语义判断，不能形式化保证所有措辞都正确；错误评估或预算
耗尽仍可能回退，不应以按钮存在作为答案充分的证明。知识来源详情、目标标题
自然化、历史会话 UI、流式前端和服务器交付不是本轮范围。

旧调查模式的 `live_semantic_http_probe`、近期内容 Chat 验收断言和联合全量验收
脚本只保留作历史证据，不能作为当前向导版的准入门禁。当前角色专项命令为：

```powershell
uv run python -m scripts.live_guide_http_probe --live
```

默认不执行；显式运行会新增保留一个隔离用户、发送三条真实 Chat，不创建推文。
