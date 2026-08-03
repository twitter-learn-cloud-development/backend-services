# Repository Agent Instructions

## 每个请求的必读顺序

在分析、搜索或修改代码前必须先读取：

1. `.agents/context/project_map.md`：当前项目结构、依赖方向、任务定位和验证入口。
2. `.agents/rules/*.md`：工程治理和开发约束。

然后根据 `project_map.md` 的“任务定位表”按需读取：

- `.agents/skills/<name>/SKILL.md`：Codex 原生仓库级 Skill，由 `name/description` 自动发现并按任务触发；禁止每次全量读取。
- `.agents/context/domain_model.md`、`environment_context.md`、`technical_debt.md`、`project_overview.md`、`agent_runtime_context.md`：按领域、环境、技术债、全局背景或 Agent Runtime 任务读取。
- `architecture_audit_report.md`：仅用于架构评审、阶段规划和技术债审查；它是历史快照，不替代当前代码。

`.codex/agents/*.toml` 是项目级自定义 Subagent 配置，不属于 Skill。

若项目地图与代码冲突，以代码为准，并在同一任务内更新 `.agents/context/project_map.md`。
