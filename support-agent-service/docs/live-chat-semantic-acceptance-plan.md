# Live Chat 语义验收开发计划

## 目标

2026-10-04 进度补记：动态授权能力目录及 Worker 状态保守归一已有实现；本批新增
结构化 gap、任务关联账本与有界独立模型语义评估，详见
[`gap-resolution-v1.md`](gap-resolution-v1.md)。知识片段误报矛盾已于 10-02 修正。
以下“已确认现状”是制定原计划时的诊断，不应视为当前所有条目仍未修复。
2026-10-05 补记：第三步的正式运行时最终评估与逐目标渲染已经实现，内部
`answer_text` 明确为直接用户答案，调用状态/缺参数证明由实际执行记录生成。
单元和图回归通过；真实样本中的超时和英文审查理由问题按实际结果迭代，详见
[`response-review-v1.md`](response-review-v1.md)。结构化校验不是自然语言满足性的
形式化证明；真实故障注入与新缺口闭环真实语义专项仍不记为通过。
本批没有仅因任意证据存在而把任务升级为成功。

修复实际内容查询已取得证据，但最终 Chat 被错误标记为 `unsupported` 或 `clarification_required` 的问题。规划必须受当前用户实际可用能力约束；最终回答必须保留可验证事实，并诚实说明未解决部分。

本计划不通过硬编码问题类别或接口列表解决问题，不把任意 evidence 的存在等同于完整回答，不降低执行层授权校验。

## 已确认现状

- `app/graph/supervisor/runtime.py` 的 planner 未接收能力目录，replanner 只接收聚合结果，缺少原问题和能力边界。
- compose 仅限制 `answerable=false` 时模型返回 `answered`，没有处理模型将充分结果降为 `unsupported` 或 `clarification_required` 的情况。
- `app/services/evidence_aggregator.py` 将所有非成功任务的 gaps 累积为不可回答条件；补充计划采用新任务 ID，旧 gap 不会自动得到解决。
- `app/graph/worker/runtime.py` 保留了工具证据，但直接采用模型 completion 的状态；`SUCCESS` 带 gaps 时，聚合器又可能忽略这些 gaps。
- 聚合器将同一 source_ref 的不同 safe_summary 视为冲突，可能混淆互补信息或不同时间观察。

## 动态能力目录链路

唯一来源为 `CapabilityRegistry.list_allowed(auth)`：运行时实际注册 → enabled/read_only/scope 过滤 → 请求级能力快照 → planner 和 replanner。worker 仍使用 resolver 选择能力，executor 仍执行独立权限检查。

`SupportRuntime` 直接注入 `registry.list_allowed` 作为目录提供者；目录读取不使用 `resolve(..., limit=8)` 的任务相关性截断。输出基于 `CapabilitySpec` 的 name、description、input_schema、output_schema、evidence_types，不另行维护问题分类表。resolver 仍供 worker 对候选能力排序。

在 `app/graph/supervisor/runtime.py` 注入目录提供者，单次请求读取并保存授权能力快照；在 `app/graph/state.py` 保存快照；在 `app/runtime.py` 将 `registry.list_allowed` 连接至 supervisor。planner 和 replanner 使用同一快照。目录不是执行授权凭据，实际执行时仍独立校验权限。

planner 只规划目录能够取得的证据，将成功标准限定为可验证事实，不因用户询问原因而凭空规划不可用的诊断能力。replanner 同时接收原始问题、能力快照、已获证据和未解决缺口，禁止重复安排当前能力无法完成的任务。目录和历史中的文本不允许覆盖系统指令。

## 证据与状态安全边界

- `answered`：原问题的必要目标已有充分证据覆盖，且不存在影响结论的未解决缺口或冲突。工具返回成功、存在任意 evidence、某个补充任务成功均不是充分条件。
- `partial`：存在可回答部分，但仍有必要信息缺失、执行失败、预算耗尽或证据不足；回答应呈现可靠事实和剩余限制。
- `clarification_required`：缺少用户能够补足、且对继续处理必要的信息。能力缺失和下游故障不属于用户澄清问题。
- `unsupported`：没有可回答部分，且请求明确不在允许的能力覆盖范围内。不能仅凭模型措辞或自然语言 gap 关键词判定。
- `WorkerStatus.FAILED` 与用户层状态不同。已采到可靠证据但模型宣称失败时，可保守归一为 `PARTIAL` 并保留 gaps；不得自动升为 `SUCCESS`。真实异常且无证据仍为失败。
- `SUCCESS` 携带实质 gaps 时不能被聚合为完整成功；必要时归一为 `PARTIAL`。
- 证据引用存在只保证来源可追踪，不保证模型 conclusion 中每项声明成立。不得把所有证据机械绑定到任意结论并视为事实验证。
- 不充分结果禁止被 composer 升为 `answered`；充分结果的状态由运行时约束，但必须同时确保回答正文与状态一致，不能仅重写标签。

当前自然语言 gaps 不足以可靠区分缺参数、能力缺失和技术故障。缺少结构化依据时采用保守 `partial`，不要推测 `unsupported` 或 `clarification_required`。

## 逐步实施与验收

### 第一步：接入动态能力目录

修改 supervisor、SupportState 和运行时装配，直接接入 `registry.list_allowed`；resolver 保持 worker 候选能力排序职责。先保持聚合语义不变，仅补充 planner/replanner 的信息和约束。

验收：运行中新增注册能力无需修改 planner 代码即可出现在目录；禁用和越权能力不可见；不同用户请求不串目录；首次规划与补充规划收到同一快照，补充规划包含原问题；既有 executor 授权测试通过。

### 第二步：修正 worker 的状态与证据保留

在 `format_result` 增加保守一致性规则并明确 completion 提示的状态含义。失败但有证据保留为部分结果；成功但无证据仍不得成功；实质 gaps 不能被忽略。

验收：FAILED+可靠证据不会丢证据且不升级为 SUCCESS；SUCCESS+gaps 不被视为完整成功；无证据、未知工具、预算耗尽和异常路径保持诚实状态；不暴露内部异常内容。

### 第三步：约束最终状态和正文一致性

在 supervisor compose 阶段将状态判定与模型措辞分开。充分性判断必须保持严格；不要将 `bool(evidence)` 改为充分性判断。必要时对状态和正文矛盾的 composition 执行有限修复或保守回退，不允许无限重试。

验收：充分结果不会仅因 composer 自行标记而变成 unsupported/clarification；缺证据、必要任务失败或冲突时不能 answered；部分结果展示已验证事实并明确局限；最终状态和正文语义一致。

### 第四步：显式解决补充计划的旧缺口

作为独立变更引入结构化 gap ID，以及补充任务的 gap 关联（例如 `resolves_gap_ids`）。只在对应补充任务成功且证据满足原缺口时关闭该 gap。跨轮聚合保留未解决缺口和历史证据；不能因任意新任务成功清空旧缺口。

验收：相关补充证据可关闭目标缺口；无关成功不关闭缺口；补充失败仍保留缺口；重复或未知 gap 引用被拒绝；计划预算与重规划上限仍生效。

现有 `tests/graph/test_support_runtime.py::test_evidence_gap_triggers_one_supplemental_plan` 固定断言补充成功后仍 partial。只有实现了明确的缺口解决机制才能调整期望，不能单独改断言掩盖问题。

### 第五步：评估冲突检测规则

另行核查同 source_ref 不同摘要的含义，避免将互补内容视为冲突。未经明确证据身份、时间和字段语义设计，不在前述最小修复中顺便放宽全部冲突检查。

验收：同 evidence_id 的不一致表示仍能检出；合法互补观察不误报；真实矛盾不会被静默合并。

## 回归测试与真实 Live 门禁

单元与图测试：

- `tests/unit/test_resolver.py`：授权目录、动态新增、禁用过滤、请求隔离。
- `tests/graph/test_support_runtime.py`：捕获两个规划阶段输入；充分与不充分结果的 composer 状态约束；正文一致性回退；补充计划不重复不可达任务。
- `tests/graph/test_generic_worker.py`：FAILED+证据、SUCCESS+gaps、无证据、未知工具和预算耗尽。
- `tests/unit/test_evidence_aggregator.py`：必要子任务缺失保持部分回答；失败结果证据保留；旧缺口只能由相关补充证据关闭。
- 集成测试使用真实注册目录与受控模型输出，覆盖内容查询成功、复合请求部分能力缺失、缺必要参数、下游暂时失败；保留现有业务和知识证据聚合回归。

Live 门禁在上述确定性检查通过后执行：以获授权的真实只读场景验证四条代表路径，检查实际工具观察、最终正文、status、evidence_refs 和重规划次数。成功查询应准确表达已知事实；复杂请求不得因局部成功虚报全部解决；不支持部分需诚实说明；技术失败不得伪装成用户缺信息。仅 HTTP 成功或 evidence_refs 非空不算通过。

记录每个场景的请求标识、期望语义、实际状态、证据引用和剩余缺口，日志不得泄漏令牌或无关用户数据。Live 结果如与受控测试不一致，先保存可复现轨迹再修复，不通过放宽断言消除失败。

## 已知风险

- 能力目录过大导致提示膨胀；需要可追踪的压缩或分层检索，不可静默截断后误称全量能力。
- 请求级快照可能与执行时状态变化不同步；执行层权限和启用状态检查必须保留。
- 模型对目标覆盖和缺口语义仍可能判断错误；状态约束不能取代证据充分性验证。
- 结构化 gap 关联会影响任务模型、修复提示、测试夹具和跨轮聚合，应独立提交验证。
- 既有 Worker Finding 将整段结论引用全部证据，存在引用正确但事实不受支持的风险。
- 若只修状态标签而未修正文，会产生 answered 与“无法回答”并存的用户可见矛盾。
- 同源不同摘要冲突启发式仍可能误伤，应作为明确后续项而非隐式删除安全检查。
