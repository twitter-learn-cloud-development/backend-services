# V1 评测说明

更新时间：2026-09-28

## 固定基线

[`evals/dataset-v1.jsonl`](../evals/dataset-v1.jsonl) 保存五类离线回归记录：业务与知识联合调查、多轮指代、跨用户拒绝、伪造用户参数拒绝、证据不足时的部分答复。

运行命令：

```powershell
uv run python -m app.evals.runner --dataset evals/dataset-v1.jsonl
```

命令计算：

- `plan_quality`：需要规划的场景是否生成 1～5 个任务。
- `capability_recall`：期望能力是否包含在实际选择集合中。
- `evidence_faithfulness`：每个事实声明是否有 Evidence 支持。
- `permission_accuracy`：允许/拒绝结果是否符合预期。
- `latency_compliance`：端到端耗时是否位于场景预算内。

默认发布门槛为全部指标 `1.0`，任一指标不足时进程退出码为 1。可用 `--minimum` 调整实验分支门槛，但 V1 验收不得降低权限与证据忠实度要求。

## 数据更新规则

数据集中的 `selected_capabilities`、事实、授权结果和耗时是可替换的记录结果。连接真实模型和测试环境后，应把运行输出转换为相同协议再执行评分；不能为了通过门槛手工删除失败案例。

每次变更 Planner、Worker Prompt、Capability 描述、权限策略或模型版本时都要重跑：

```powershell
uv run ruff format --check .
uv run ruff check .
uv run pyright
uv run pytest
uv run python -m app.evals.runner
```

当前离线基线不替代真实模型稳定性、召回质量和容量测试；这些属于 Live 验收门禁。
