---
trigger: always_on
description: Repository-local development, documentation and verification rules.
---

# 项目开发规则

## 1. 每次请求

1. 先读 `.agents/context/project_map.md`。
2. 再读 `.agents/rules/production.md` 和本文件。
3. 按项目地图只加载相关 Context、Skill、Workflow 和源码。
4. 代码事实优先于旧文档；冲突时同步更新项目地图。

## 2. 修改边界

- 工作区可能已有用户改动；不得回滚、覆盖或格式化无关文件。
- 手工编辑使用 `apply_patch`；批量格式化只作用于本轮文件。
- 不手改 `api/*/v1/*.pb.go`、`*_grpc.pb.go` 等生成文件。
- 不提交根目录 `*.exe`、build cache、`web/dist`、上传文件等产物。
- 不为了顺手清理做无关重构、依赖升级或元数据变更。

## 3. 文档维护矩阵

| 变化 | 必须更新 |
|------|----------|
| 完成阶段/模块 | `docs/PROJECT_PROGRESS.md` |
| 编译、测试、panic、部署失败 | `docs/ISSUES.md`，解决后更新同一条 |
| HTTP/gRPC 契约 | `docs/API_REFERENCE.md` + 客户端适配 |
| 服务/目录/依赖/端口/存储归属 | `.agents/context/project_map.md` |
| Agent 阶段能力 | `.agents/context/agent_runtime_context.md` + 强化计划 |
| 稳定技术债状态 | `.agents/context/technical_debt.md` |

禁止只更新计划不更新代码，或代码完成后把未验证项标记为完成。

## 4. 验证原则

- 窄改动先跑目标包；共享契约、Runtime、Scheduler、Repository 变更扩大到全模块。
- 并发状态、goroutine、Timer、Lease、Cache 变更运行 `go test -race`。
- Go 变更执行 `gofmt`、测试和 `go vet`。
- Web 执行 `npm run build`；Mobile 执行 `flutter analyze/test`（受影响时）。
- 本轮文件执行 `git diff --check`；不因无关历史空格修改用户文件。
- 外部服务不可用时优先 Fake/Adapter 离线测试；真实集成测试单独标记。

## 5. 失败记录

`docs/ISSUES.md` 至少记录：

- 现象和失败命令。
- 根因：代码、环境、权限、外部依赖或超时。
- 影响范围。
- 修复与复验命令。
- `Open/Resolved/Blocked` 状态和日期。

禁止吞掉失败、只说“应该没问题”或在未复验时标记 Resolved。
