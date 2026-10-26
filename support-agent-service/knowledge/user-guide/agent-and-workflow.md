# AI 助手和 Workflow 有什么区别

适用：Web `/agent` 与 `/agent/workflow`；更新于 2026-10-02。

AI 助手是对话入口：在 `/agent` 选择当前可用的对话模型，输入问题并发送。界面有“自动选择能力”“直接回答”“站内搜索”“内容草拟”等能力偏好。联网搜索、外部工具等是否可用取决于当前部署、配置及账号权限，出现选项不保证服务已启用。

Workflow（工作流）是把多个处理步骤及连接顺序保存成可复用流程。在 `/agent/workflow` 的画布里，节点表示步骤，连线表示步骤间关系；节点属性决定输入与处理配置。适合反复执行结构相近的任务。普通提问不要求创建 Workflow。

默认工作流是对话工作流，不会因为生成了文本就自动发布推文。Workflow 的保存、运行、发布给 Agent 是三件不同的事：保存定义与版本；运行执行一次流程；发布给 Agent 是把选定版本提供为助手可发现的工具。

“发布给 Agent”受到部署启用状态与流程校验限制，不能保证每个环境都能使用。保存新草稿不会自动替换已发布的版本；应以界面的已发布版本提示及真实返回状态为准。

AI 助手中的草稿也不是已发布推文。页面存在单独的“确认发布草稿”流程，需由用户核对并明确确认。只读客服可以解释这一区别，但不会代为执行发布。

## 来源

- twitter-clone `web/src/views/Agent.vue`：capabilityPresets、模型选择、确认发布草稿界面。
- twitter-clone `web/src/views/agent/WorkflowEditor.vue`：saveWorkflow、publishSelectedWorkflowTool、默认流程提示。
- twitter-clone `docs/API_REFERENCE.md`：工作流更新、运行与工具发布版本约束。
