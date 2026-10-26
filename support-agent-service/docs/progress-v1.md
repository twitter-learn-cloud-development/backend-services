# V1 开发进度

更新时间：2026-10-05

当前交付目标：**V1 本机联合运行版已就绪（当前 Windows + Docker Desktop 环境）**；服务器部署与生产准入未验收。操作与范围见 [`local-delivery-v1.md`](local-delivery-v1.md)。本机交付不等于完全离线：Chat 与 Embedding 仍使用外部 DashScope。

## 当前状态

用户已将小栖收窄为**产品使用向导**：正式目录只读取发布指南，站内检索/统计/
创作等任务提示用户前往站内 AI 助手，混合请求保留使用说明。旧内容调查能力的
验收记录是历史阶段，不代表当前小栖仍执行账号内容查询。分工与新门禁见
[`guide-role-v1.md`](guide-role-v1.md)。当前 Python **190 项**、前端 **32 项**测试
及构建通过；仍未进行服务器交付。

当前向导专项真实 HTTP run `40cc0c89` 三项通过：Workflow 操作说明 answered，
“站内什么类型推文较多” unsupported 并提供固定转交入口，混合请求 partial
且保留说明。新版镜像 healthy；只更新分工指南，未清理其他知识或用户数据。
站内 AI 实际统计能力和本轮浏览器点击尚未验收。

此前调查模式的回答一致性收尾已实现并更新本机镜像：原请求目标账本、逐目标最终评估、可信记录与
知识说明分别渲染、实际工具错误和缺参数证明。Python **179 项**、前端 **29 项**
测试通过，ruff/格式检查/pyright 通过；四条真实 HTTP 复验结果及剩余门禁见
[`response-review-v1.md`](response-review-v1.md)。保持统一自然语言接口、只读能力
和原有认证边界，不增加管理端或服务器交付范围。

F2（2026-10-02）已实现全站右下角可收缩客服、原站登录复用、可信推文引用与 6 篇操作指南检索。Python 112 项回归、29 项前端 API/引用/身份竞态测试和两套前端构建通过；窗口状态/安全矩阵与工作台浏览器回归通过。2 用户/1 推文/2 次模型问答的真实页面验收通过。具体证据、范围和剩余门禁见 [F2 验收记录](frontend-acceptance-v2.md)。本轮未改变统一自然语言入口或写死问题类别，也未执行用户 Workflow 写操作。

| 轮次 | 状态 | 验收结果 |
|---|---|---|
| R0 契约冻结与工程约定 | 已完成 | 架构、开发计划、分轮计划、接口文档与 OpenAPI 已建立 |
| R1 工程骨架与核心协议 | 已完成 | 服务可启动；健康检查、Schema、任务依赖和状态模型通过测试 |
| R2 ModelProvider 与 Capability Runtime | 已完成 | Registry、Resolver、Executor、Fake Model 和 Mock Capability 通过测试 |
| R3 GenericWorker Agent Loop | 已完成 | 动态工具选择、多轮工具循环、未知工具和预算终止通过测试 |
| R4 Supervisor 与并行主图 | 已完成 | 动态规划、并行调度、依赖波次、证据门与最终回答通过测试 |
| R5 统一 Chat API 与 SSE | 已完成 | JSON/SSE、认证边界、幂等、会话归属、限流、超时与断连通过测试 |
| R6 Go Support Query 联调 | 关键 Live 路径通过 | 真实用户与推文查询、脱敏、重放、冲突、伪造身份和用户隔离通过；故障/回滚边界仍待专项验证 |
| R7 多轮会话与知识检索 | 关键 Live 路径及隔离持久化门禁通过 | MongoDB 续聊、隔离库跨连接恢复/跨用户拒绝/显式删除/TTL 物理清理通过；Qdrant 向量关键路径通过；共享 Mongo/Qdrant 受控重启后的持久化与新连接恢复通过，运行中旧客户端恢复未验证 |
| R8 评测、可观测性与交付 | 本机交付就绪，生产准入递延 | Chat、指标、审计及首轮/续聊 Python→Gateway→Tweet 精确父子跨度关联通过；本机启停、Gateway loopback 与 Support Agent 实际限额已核查；故障注入、在途停机与主机外探测待做 |
| F1 用户端设计与交互 Demo | 已实现本机独立 Demo | Vue 3/TypeScript/Vite，演示与真实连接、账号认证、内存会话、忠实答复与证据引用、幂等重试；构建与 11 项 API 契约测试通过，桌面/手机浏览器夹具检查通过；真实模型页面验收和现有 Twitter Web 路由集成待后续 |
| F2 全站客服与操作指南 | 本机关键路径验收通过 | 全站收缩窗口、共享身份、可信推文跳转、6 篇发布指南；29 项前端测试及实际页面登录/引用/知识续聊验收通过，范围见 F2 记录 |

## 本批收尾：回答覆盖与一致性（2026-10-05）

- 正式运行时强制接入独立最终评估，按原请求目标分别保留记录、知识和限制；
  内部 `answer_text` 直接展示用户答案，不接受旧审核理由字段。
- 执行器生成工具错误及必填输入证明；未知引用、漏目标、混合记录/知识引用、
  冲突、稿件或评估异常/超时都保守回退。必要步骤失败不能伪装成用户缺参数。
- 179 项 Python、29 项前端测试及构建、ruff/pyright 通过；正式新镜像 healthy，
  四个本机入口 HTTP 200。容器停止后恢复已有项目依赖，保留原数据与密钥。
- 最终 run `d38dea52`：最近内容 answered，编号/UTC 时间/可见性与创建记录一致；
  复合问题 partial，保留中文 Workflow 操作说明与能力限制。其余两条 200/partial
  只验证诚实降级；Workflow 故障问题最终评估被拒绝，不能称为完整答案通过。
- 开发及收尾累计保留 5 个隔离用户、5 条推文，16 条真实 Chat 请求；完整关联、
  先前失败轨迹及限制见 [`response-review-v1.md`](response-review-v1.md)。

本批没有增加管理员数据、Workflow 写操作或服务器交付；下一批可推进用户端
历史会话恢复与知识来源详情，继续完善语义回归、目标标题和响应速度。

## 本轮：本机恢复与缺口闭环（2026-10-04）

- 起初相关容器与四个本机入口均停止；使用 `scripts/frontend.ps1 start -RestoreStack`
  恢复现有依赖和前端，不清理数据、不轮换密钥。主站 5173、助手 5174、Support
  readiness 8000、Gateway 9638 均恢复 HTTP 200；新版 Support Agent healthy。
- 新增稳定任务级 gap ID、`resolves_gap_ids`、跨补充轮任务账本和关闭记录；首轮、
  未知/关闭及重复引用拒绝，Worker 返回 task_id 与分派绑定。
- 仅成功无新 gaps 且有工具证据的关联任务进入独立语义评估；引用必须来自该任务，
  无关成功不关闭缺口，真实冲突仍保留。评估异常、非法输出、拒绝及超时安全降级。
  旧失败总结在缺口关闭后不混入当前总结，历史证据仍保留。
- 评估最多等待 8 秒并按剩余预算预留合成时间；结构化 gap 容量覆盖合法跨轮预算。
  语义满足性仍由模型评估，不宣称形式化事实保证。实现与风险见
  [`gap-resolution-v1.md`](gap-resolution-v1.md)。
- 新镜像仅更新 Support Agent，未再次重启共享数据服务；用户 Chat JSON/SSE、
  浏览器接口和原有认证边界不变，前端 29 项测试通过。
- 本批 Python 全量 **150 项通过**（新增 25 项），ruff 与 pyright 通过。
  Luna 新增 22 项缺口专项，主代理追加跨轮关闭引用、剩余预算跳过和最大容量检查；
  Astra 审查指出独立超时与结构化容量问题，均已修正并回归。
- HTTP Live run `34e3a7a5`：原句 Workflow 问答返回 200/answered、7 条知识依据、
  无伪造推文导航；同请求幂等重放一致。新增保留 1 个隔离测试用户，1 次真实模型
  问答。此项只验证现有真实知识链路兼容，不代表新缺口闭环已完成真实语义专项验收。

本批没有做服务端历史列表、引用详情、流式 UI、真实故障注入或服务器交付。
后续优先完成缺口闭环真实复杂问题语义验收及回答状态/正文一致性，再做历史会话
与来源展示；可靠性和生产门禁仍按原计划保留。

## 已实现模块

```text
app/main.py                         FastAPI app factory 与 lifespan
app/core/                           配置、结构化日志、统一异常
app/api/routes/health.py            liveness / readiness
app/domain/                         全部核心 Pydantic 协议
app/graph/state.py                  SupportState / WorkerState
app/integrations/model_provider.py  OpenAI-compatible 与 Fake Model Provider
app/tools/registry.py               扁平 Capability Registry
app/tools/resolver.py               动态候选能力检索
app/tools/executor.py               授权、Schema、超时与重试
app/tools/mock.py                   离线 Mock Capability
app/graph/worker/runtime.py         GenericWorker LangGraph 子图
app/services/evidence_aggregator.py 证据去重、冲突识别与可回答性判定
app/graph/supervisor/runtime.py     Supervisor 动态规划、并行主图与回答合成
app/api/routes/support.py           统一 Chat JSON/SSE 用户入口
app/api/auth.py                     可注入用户认证边界与显式 Fake Auth
app/api/sse.py                      安全 SSE 事件映射与断连取消
app/services/idempotency.py         并发安全的请求幂等控制
app/services/conversation_store.py  会话归属、TTL、LangGraph checkpoint 与上下文压缩
app/services/rate_limiter.py        V1 用户请求限流
app/runtime.py                      Supervisor、Worker 与 Capability Runtime 组装
app/integrations/service_token.py   短期 HS256 服务 JWT 签发
app/integrations/support_query.py   异步 Go Query Client、错误映射与 Capability 注册
app/domain/support_query.py         Query 请求、响应与 Problem 协议
app/domain/conversation.py          有界多轮上下文协议
app/domain/knowledge.py             发布文档与知识命中协议
app/integrations/knowledge.py       Qdrant ingestion/search 与知识 Capability
app/core/observability.py           OpenTelemetry、Prometheus 与脱敏审计
app/evals/                          固定数据集评分与发布门禁命令
compose.yaml                        本地依赖编排、资源限制与健康检查
deploy/                             Collector 与 Prometheus 配置
```

## 当前质量结果

```text
ruff format --check : passed
ruff check          : passed
pyright strict      : 0 errors, 0 warnings
pytest              : 190 passed (2026-10-05)
frontend unit tests : 32 passed; frontend build passed (2026-10-05)
offline eval        : fixed 5 cases, 5 metrics = 1.0 (2026-10-05); not Live semantic proof
compose config      : passed
runtime smoke test  : Web/workspace/ready/Gateway HTTP 200 (2026-10-05)
```

## Workflow 知识问答 503 修正（2026-10-02）

用户原句“Workflow 是什么，如何创建和运行？”曾返回 503。真实诊断显示三个
Worker 的知识检索均成功，但聚合器把同文档的不同片段、同片段不同查询的相关度
分数当作事实冲突（7 个片段被报告为 8 项矛盾）；随后模型返回空补充计划，被
原先要求至少一项任务的校验拒绝。不是 Docker、MongoDB 或 Qdrant 停机。

- 知识证据按片段识别；去重仅忽略查询相关度 `score`。同片段事实、版本、时间等
  字段发生变化仍判冲突，业务证据原有规则不变。这不构成跨片段语义矛盾的形式化验证。
- 首轮计划仍至少一项任务；可选补充计划允许空任务。无效/超预算补充计划不执行，
  保留已验证结果进行合成；真实缺口仍降级为 partial，不伪造 answered。
- 增加仅含异常类型与请求关联信息的诊断日志，不记录模型原文、凭据或用户正文。
- 全量 Python 测试 **125 项通过**，ruff、pyright 通过；仅重建并更新 Support Agent，
  未重启共享数据服务、未重新发布知识文档。
- HTTP Live 回归 run `35d9f4dd`：同一句问题经 5174 前端代理返回 **200 / answered**，
  7 条已发布知识引用、无伪造推文链接；同 key/body 重放返回同 case 与回答。
  创建并保留 1 个隔离测试用户，执行 1 次真实模型问答，重放未再次执行模型。

复验命令：`uv run python -m scripts.live_workflow_http_probe --live`。此命令会创建
测试账号并调用真实模型，须显式指定 `--live`；本次没有替用户的现有账号自动提交问答。

## R7 本轮结果

- `conversation_id` 映射为 LangGraph `thread_id`，只存用户可见问答，不持久化认证与请求态。
- MongoDB checkpoint、会话归属记录和 TTL 均支持 7 天默认保留；删除流程同时清理归属与 checkpoint。
- Planner 只接收有界最近消息与截断摘要，历史不会无限进入模型上下文。
- Qdrant ingestion 只写入已发布文档，保留文档 ID、版本、更新时间和 chunk ID。
- `knowledge.search_published` 使用与业务查询相同的 Capability/Observation/Evidence 链路，没有新增 Graph 分支或 KnowledgeWorker。
- 会话历史和知识文档都被标记为不可信数据，不能覆盖系统与工具策略。

## 联合 Live 验收结果与剩余门禁

2026-10-01 的逐条内容事实修正与本机 Live 回归已通过：Go `content.list_recent`
把推文存储的 Unix 毫秒时间按毫秒解析；Python Query Adapter 校验并保留逐条
`resource_ref`、`created_at`、`visibility`，Worker 与 Composer 对模型生成的编号和时间
采取可信记录渲染或降级。明确要求逐条列出编号/发布时间时，即使模型只给概括性文字，
最终清单也由本轮查询记录生成。只读 Live 探针返回正常的 2026 年时间；随后用新建的
隔离测试用户和一条推文运行真实 Chat 首轮及续聊，续聊清单中的每组 ID、UTC 时间、
可见性都与 Gateway 直接查询逐条匹配。认证、重放、幂等冲突、伪造身份、用户隔离和
指标检查同时通过；测试数据保留。此修正是针对结构化逐条内容事实的确定性防线，
不代表所有自然语言叙述都已获得形式化事实验证。

2026-09-29 的联合关键路径验收已通过：报告见
`dist/live-acceptance/live-acceptance-20260929170710-195cbd5c.json`。
真实 Chat 首轮状态为 `answered`，包含 1 条证据引用；续聊保持同一会话。
Support Query 安全边界、Qdrant + DashScope 向量检索、指标、审计与 Jaeger 服务发现均通过。
本机系统代理曾使续聊 POST 在到达服务前发生 `httpx.ReadError`；验收脚本对 loopback
客户端设置 `trust_env=False` 后完整复验通过。

本机交付已知限制与后续生产/可靠性待办（不宣称已通过）：

- 密钥按用户确认保留在 Go 仓库内、被 Git 忽略的 `.env`；不再将仓库外迁移列为本轮门禁，仍须防止密钥入库或输出到日志。
- 验证真实 429、503、504 与 Go 关闭开关回滚；JWT 重放和 tool_call 冲突已在上轮关键路径验收通过。
- 运行中 Support Agent 旧 Mongo/Qdrant 客户端与已有 Chat 会话的故障恢复仍待验证；共享依赖受控重启后的持久化与新连接恢复已通过。
- 验证在途请求优雅停机与资源限额下的行为；已核查限额不等于完成负载或停机验收。
- 从主机外核对公网 `/internal/` 不可达，再决定是否冻结 V1 `1.0.0`。

本机日常入口 `scripts/local.ps1` 的 status/stop/start 已在现有容器上实测，Support Agent 重新达到 healthy；该脚本只操作现有 Support Agent，不负责全新机器或 Docker 重启后的整套 Go 依赖自动恢复。首次全量验收脚本的语法、默认路径与 Compose 配置已静态核对，非 200 就绪响应的重试耗尽会失败；本轮未重跑会创建测试用户/推文并调用付费模型的全量验收。Go 仓库 `.env` 经 `git check-ignore` 确认被忽略。

## 本轮专项门禁结果

- Gateway Live overlay 已实测仅绑定 `127.0.0.1:9638`；本机 health 为 200，未认证 internal 为 401。该结果不是公网隔离实测。
- Support Agent HostConfig 已实测为 768 MiB、1 CPU、StopTimeout 30s，当前 healthy、无重启、无 OOM；在途请求优雅停机未测。
- Jaeger 完整 Chat 首轮与续聊的 Python → Gateway → Tweet parent span 精确匹配，实际跨服务关联通过。
- 隔离数据库 `support_agent_probe_e6f8ae9f25ea` 的跨连接恢复、跨用户拒绝、显式删除、60s TTL 与物理清理通过，`conversations`、`conversation_checkpoints`、`conversation_checkpoint_writes` 最终均为 0；该项测试本身未重启 Mongo 服务，受控重启验证见下项。
- Go supportquery 的 429/503/504 单测通过；Python MockTransport 的 429/504 重试上限、每次新 JWT、同 tool_call_id 检查通过。这些结果不是实际 Live 故障注入，不关闭真实故障门禁。
- Qdrant 新 probe 的真实插入、召回、撤回通过，撤回后按 document_id 验证 count=0；未据此宣称 Qdrant 服务重启恢复通过。
- 经用户授权，依次对共享容器 `mongodb` 和 `qdrant` 执行受控重启，持久化卷未删除。run ID `restart-7c70335c79f24e81`：Mongo 专属库的 owner/checkpoints/writes 在重启前后均为 `1/3/3`，新连接恢复 2 条会话消息；Qdrant 同一文档在重启后 count=1 且向量检索命中。Support Agent ready、Gateway→Tweet 只读查询通过。最终仅清理本次 conversation 与 document_id，Mongo 三类记录和 Qdrant point 均为 0；未 drop 数据库。此项不证明运行中旧客户端或已有 Chat 续聊恢复。
- 真实 429/503/504、Go 开关回滚、在途停机和主机外探测仍未做。后续分批方案见 `docs/live-acceptance-next-batch.md`。
