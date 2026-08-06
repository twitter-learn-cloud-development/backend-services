# Agent Profile 版本与发布

## 运行时模型

Agent Service 的请求热路径只读取内存中的不可变 `Profile Catalog`。目录以
`profile_id + version` 唯一标识执行快照，更新时先构造并校验完整的下一代目录，
再通过 `AtomicResolver` 一次替换指针。正在执行的请求继续持有自己的 Profile
副本，不会在运行中漂移，也不会直接查询 MongoDB。

当前内置 Profile：

| Profile | 版本 | 默认选择 |
|---|---|---|
| `assist.draft` | `v1`、`v2` | `v1` |
| `workflow.react` | `v1` | `v1` |
| `workflow.plan_execute` | `v1` | `v1` |
| `multi.search/style/writer/review` | `v1` | `v1` |

## 持久化模型

MongoDB 使用八个独立集合，不扩张对话仓储接口：

- `agent_profile_versions`：不可变 Profile/Prompt 快照。新版本只能以 `draft`
  创建，发布使用 `revision` 条件更新为 `published`。快照 JSON、Schema 和
  SHA-256 哈希在创建后不允许修改。
- `agent_profile_releases`：每个 Profile 一条可变发布指针，保存 stable、
  candidate、基点比例和 salt，使用乐观版本号防止并发覆盖。
- `agent_profile_audit_events`：append-only 管理审计，只保存操作 ID、动作、结果、
  操作者、revision 与快照哈希，不保存 Prompt 正文、Provider 凭据或请求体。
- `agent_profile_publish_approvals`：发布申请状态机，绑定不可变版本的 revision 与
  snapshot hash；保存申请/审批身份、执行租约和有限错误码，不保存 Prompt 正文。
- `agent_profile_role_bindings`：每个用户一条动态项目角色绑定，使用 `user_id` 唯一索引
  和 revision CAS，角色仅允许 viewer/editor/approver/admin。
- `agent_profile_role_audit_events`：append-only 角色变更审计，保存操作、操作者、目标用户、
  角色、revision 和有限错误码，不保存 JWT 或内部令牌。
- `agent_profile_experiments`：绑定 Profile、stable/candidate、Release revision、策略、聚合统计
  与终态决策；每个 Profile 同时只允许一个 `running` 实验。
- `agent_profile_experiment_observations`：按 Run ID 幂等保存版本、分组、成功标记、耗时和
  估算成本；可由受信任业务入口单次回填固定类型的正/负结果，不保存用户 ID、Prompt、
  Completion、Tool 输入或响应正文。

只有 `published` 版本会进入目录。加载时严格检查 Schema、快照哈希、记录身份、
预算、工具白名单和 Release 引用。任一校验失败时不替换当前内存目录。

当前已实现应用层 `ProfileCatalogManager` 的草稿创建、发布 CAS、Release CAS、
进程内原子重载、受保护管理 API、append-only 审计，以及 Redis 通知加周期反熵的
跨实例刷新。运行请求仍只读取 `AtomicResolver`，不读取 MongoDB 或 Redis。

## 管理 API 与认证

Gateway 在 `/api/v1/agent/profile-catalog` 下提供以下接口：

- `POST /versions`：创建不可变草稿；
- `GET /versions`、`GET /versions/:profile_id/:version`：分页查询版本；
- `POST /versions/:profile_id/:version/publish-requests`：编辑者提交发布申请；
- `GET /publish-approvals`、`POST /publish-approvals/:id/decision`：查询并由另一账号审批；
- `POST /publish-approvals/:id/retry`：恢复失败或租约过期的发布执行；
- `POST /versions/:profile_id/:version/publish`：默认关闭的 break-glass 直发入口；
- `GET /releases/:profile_id`、`PUT /releases/:profile_id`：查询或 CAS 更新 Release；
- `GET /audits`：按 Profile 分页查询脱敏审计事件。
- `POST/GET /experiments`：启动或分页查询 Release 绑定的运行时安全实验；
- `GET /experiments/:id`：读取实验策略、聚合统计和当前决策；
- `POST /experiments/:id/evaluate`、`POST /experiments/:id/stop`：按 revision CAS 立即评估或停止；
- `POST /experiments/:id/outcomes`：管理员服务账号按已有 Run/Event ID 幂等回填业务结果；
- `GET/PUT/DELETE /role-bindings`：管理员分页查询、CAS 更新或删除动态角色；
- `GET /role-audits`：管理员分页查询 append-only 角色审计。

管理入口采用三层约束：正常 JWT 登录、Agent Service 的 `viewer/editor/approver/admin`
角色判定、Gateway 到 Agent Service 的 `AGENT_PROFILE_ADMIN_TOKEN` 内部令牌。Gateway
先查询权限用于路由和 UI，Agent Service 在实际管理 RPC 内再次校验，避免把前端显隐或
Gateway 本地状态当成最终安全边界。令牌至少 32 字符，
不得由浏览器提交、写日志或放入 Helm 明文 values；Kubernetes 使用已有 Secret 引用。
角色分别由 `AGENT_PROFILE_VIEWER_USER_IDS`、`AGENT_PROFILE_EDITOR_USER_IDS`、
`AGENT_PROFILE_APPROVER_USER_IDS` 和 `AGENT_PROFILE_ADMIN_USER_IDS` 提供静态 break-glass
来源，并与 Mongo 动态绑定合并。只有静态 `admin` 是根管理员，能够授予或撤销动态
`admin`；动态管理员只能管理普通角色。正常发布要求 editor 与不同账号的 approver。

`AGENT_PROFILE_DYNAMIC_RBAC_ENABLED=false` 会关闭动态绑定读写并回退到纯环境变量角色；
静态角色不会被 API 覆盖或删除。当前是项目级 RBAC，尚未接入组织目录或统一授权服务。

跨实例刷新使用 `AGENT_PROFILE_CHANGE_CHANNEL` Redis Pub/Sub 消息作为失效提示；消息
不携带 Prompt，只包含操作与 revision 元数据。接收实例始终从 MongoDB 重新加载、完整
校验并原子替换。`AGENT_PROFILE_SYNC_INTERVAL` 默认 30 秒，即使通知丢失也会最终收敛。

## 发布优先级

Release 合并优先级为：

1. 内置默认发布；
2. MongoDB 持久化发布；
3. `AGENT_PROFILE_RELEASES` 环境变量应急覆盖。

环境变量使用严格 JSON：

```json
[
  {
    "profile_id": "assist.draft",
    "stable_version": "v1",
    "candidate_version": "v2",
    "candidate_basis_points": 1000,
    "salt": "assist-v2-canary"
  }
]
```

- `candidate_basis_points` 范围为 `0..10000`；`1000` 表示 10%。
- 部分灰度必须提供稳定 `salt`；修改 salt 会重新分桶。
- `0` 固定选择 stable，`10000` 固定选择 candidate。
- 未知 Profile/Version、重复 Release、非法比例和坏快照都 fail-closed。
- 环境覆盖不能掩盖数据库中的非法持久化配置；两层都会独立校验。

## 稳定分桶

部分灰度使用 `SHA-256(profile_id + salt + user_id)` 计算稳定桶。同一发布配置下，
同一用户跨请求、跨进程稳定命中同一版本。Prompt 中的 `user_id` 只在选版后的
返回副本渲染，不进入共享目录。实际命中的 Prompt ID/Version 会进入 LLM Trace，
Assist 对话元数据保存 Profile/Prompt 版本。

## 运行时实验门禁

`AGENT_PROFILE_EXPERIMENTS_ENABLED=true` 时，管理员可为已有 candidate 流量的 Release 启动
实验。实验快照绑定 stable/candidate 版本、流量基点、salt 和 Release revision；环境变量
覆盖控制的 Release 禁止启动实验。Runtime v2 完成或失败后，旁路 Recorder 只投递 Run ID、
实际 Profile 版本、成功标记、耗时和估算成本。请求路径只做有界非阻塞入队，后台消费者查询
当前实验并落库；队列满、进程退出或存储失败可能丢失观测并延后判定，但只告警且不改变用户
请求结果。队列大小由 `AGENT_PROFILE_EXPERIMENT_OBSERVATION_QUEUE_SIZE` 配置。

达到每组最小样本后，控制器比较候选组相对稳定组的错误率、P95 延迟和平均估算成本。
策略可选 `response_accepted`、`draft_published` 或 `content_engaged` 业务结果门禁；启用后还会等待
每组最小结果样本，并在候选结果率下降超过阈值时触发同一回滚流程。信号只能绑定已有运行观测，
同值请求可重放、冲突值拒绝，模型和 Prompt 不能写入该入口。
Assist 的 `draft_published` 正向来源已接入：阶段一返回最小化 Run ID，用户选择并编辑草稿后显式
确认；服务端校验该 Run 属于当前用户、模式为 Assist 且已完成，再使用稳定幂等键调用 TweetService。
只有推文真实创建成功才旁路写入正向结果。用户未点击、页面关闭或超时都不能直接解释为负样本。

`content_engaged` 正向来源也已接入：确认发布成功后，Agent Mongo 只保存带 TTL 的
`tweet_id -> source_run_id`、作者和归因窗口，不复制推文正文。点赞与评论在 Tweet MySQL 业务写入的
同一事务内追加 Outbox，由 Canal 投递到 `twitter.events`；Agent 使用独立队列消费，失败按 1/2/4 秒
退避并在三次后进入 DLQ。归因窗口默认 7 天，可用 `AGENT_PROFILE_CONTENT_ATTRIBUTION_WINDOW`
调整。窗口内首个非作者点赞或评论记录 `content_engaged=true`；自赞、窗口外事件、普通推文和重复投递
均不增加样本。没有互动仍不构造负样本，转发尚未纳入该信号。`AGENT_PROFILE_EXPERIMENTS_ENABLED=false`
会同时关闭映射创建和消费者。`response_accepted` 的明确拒绝动作仍待后续设计。

### 内容互动 DLQ 检查与重放

专用死信队列为 `queue.agent.profile.content-engagement.dlq.v1`。运维命令不会声明或修复
拓扑；队列不存在时直接失败，避免误连环境后创建空队列掩盖部署问题。默认模式只取出有界批次、
校验后重新入队，不发布消息：

```powershell
go run ./cmd/agent-profile-dlq-replay --limit 20
```

执行重放必须显式提供操作人和变更原因：

```powershell
$env:DLQ_REPLAY_OPERATOR='on-call@example'
go run ./cmd/agent-profile-dlq-replay --execute --limit 20 --max-replays 1 --reason 'INC-42 verified downstream recovery'
```

- 单批最多 100 条，单条累计重放上限最多配置为 10；生产建议保持默认 1。
- 只接受 `tweet.liked.agent-profile.dlq` 与 `comment.created.agent-profile.dlq`，并在发布前
  重新解析和校验事件；毒消息、超大消息和达到上限的消息继续留在 DLQ。
- 重放会保留 Trace/Correlation Header，清除旧消费重试次数并增加独立重放计数。只有
  `twitter.events` 的 Publisher Confirm 成功后才 ACK 原消息；ACK 失败报告为
  `acknowledgement_uncertain`，下游仍以事件 ID、Run 和归因记录幂等处理重复投递。
- JSON 报告只包含消息 SHA-256、固定事件类型、固定错误码、操作人和原因 SHA-256，不输出
  点赞用户、评论正文、Prompt 或推文内容。执行时应把标准输出纳入受控变更单/审计采集。
- 检查模式会把取出的消息重新入队，RabbitMQ 不承诺维持 DLQ 的原始物理顺序；同一队列禁止
  并行运行多个重放命令。先检查、修复下游并确认积压，再使用小批量执行。

任一护栏越界时，控制器通过 Release revision CAS 将候选基点设为 `0`；Release 已被其他
管理员修改时实验进入 `superseded`，不会覆盖新配置。达到目标样本且护栏通过时只进入
`passed`，不会自动把候选提升为 stable。`AGENT_PROFILE_EXPERIMENT_RECONCILE_INTERVAL` 默认
30 秒；`AGENT_PROFILE_EXPERIMENTS_ENABLED=false` 可关闭 Recorder、协调器与管理入口。

运行指标只证明可靠性、延迟和成本；业务结果信号只证明外部事件是否发生，两者都不代表答案
正确或内容质量。语义质量仍需固定数据集 Eval。禁止把 Prompt/响应正文塞入实验观测集合或
Prometheus Label。

## 离线质量发布门禁

`AGENT_PROFILE_EVAL_EVIDENCE_REQUIRED=true` 时，编辑者提交发布审批必须附带
`agent-task-eval` 生成的 MinIO Object Lock 回执。管理路径按精确 `version_id` 读取报告并校验：

- HMAC-SHA256 签名与受信 Key ID；
- 数据集、执行配置和完整报告 SHA-256；
- `runtime_live` 与目标 Profile ID/Version；
- `passed` Gate、至少 50 个 Case、零执行错误、零越权写成功、零虚构工具结果；
- 审批类用例存在时 100% 通过；
- MinIO 远端仍保持不短于回执的有效 `COMPLIANCE` 保留期。

提交申请、批准发布和失败恢复都会重新验真。Mongo 只保存对象定位、摘要指标与验证时间，运行时
请求不读取 MinIO。将开关恢复为 `false` 可立即回退到原审批策略，不修改既有 Profile/Release。

进一步设置 `AGENT_PROFILE_EVAL_CONTENT_SIGNOFF_REQUIRED=true` 时，回执必须指向
`agent-task-content-qualified-evidence/v1`，而不是裸报告。管理路径会分别使用报告 HMAC Key 和
独立 Content Signoff HMAC Key 验证内嵌报告、逐 Case Signoff 绑定及
`external_human + asserted_external + approved`；配置绑定 Judge 不能通过该门禁。Agent Service
不持有 Review Bundle AES Key，也不解密人工复核正文。该开关依赖基础证据开关；将它单独恢复为
`false` 可兼容旧裸报告回执，但不会删除或改写已归档资格对象。

## 启动与回滚

- `AGENT_PROFILE_STORE_ENABLED=true`：默认行为，启动时确保 Mongo 索引并加载所有
  已发布版本和 Release；非法持久化目录会阻止服务启动。
- `AGENT_PROFILE_STORE_ENABLED=false`：应急回滚，只使用内置目录和
  `AGENT_PROFILE_RELEASES`，不读取持久化 Profile 集合。
- 将候选基点设为 `0` 可把新请求切回 stable。已开始的请求不受影响。
- `AGENT_PROFILE_EXPERIMENTS_ENABLED=false`：关闭实验观测、自动评估和实验 API，不改变
  当前 Release；需要同时止损时由管理员将候选基点设为 `0`。
- `AGENT_PROFILE_EVAL_EVIDENCE_REQUIRED=false`：关闭离线证据强制门禁并停止启动时 MinIO 依赖；
  双人审批、revision/snapshot 绑定和运行指标实验仍保持不变。
- `AGENT_PROFILE_EVAL_CONTENT_SIGNOFF_REQUIRED=false`：只回滚外部人工 Signoff 强制要求，基础
  WORM 报告门禁是否启用仍由 `AGENT_PROFILE_EVAL_EVIDENCE_REQUIRED` 决定。

## 当前边界

本增量已提供 `/agent/profiles` 管理界面、持久化项目级角色绑定、运行指标与可选业务结果实验
门禁、自动 CAS 回滚和离线 Eval 归档回执发布门禁。以下能力仍未完成：

- 组织目录/统一授权服务集成与外部变更单联动；
- 受控生产环境中的真实固定模型 WORM 基线、Assist 发布样本/回滚验收，以及负向与互动产品事件源；
- Profile 删除、归档和引用关系治理。

后续管理 API 必须调用应用层 Manager，不能让请求热路径或 Transport 直接读写
MongoDB，也不能就地修改已发布快照。
