# Agent L3/L4 上线验收 Runbook

> 状态：代码与 Helm 入口已具备；真实第三方 MCP、真实 Workflow、进程重启和部署平台证据尚未执行。
> 适用范围：公开演示和小规模 Beta，不包含 Marketplace、并行 Multi-Agent 或新执行 Profile。

## 1. 上线停止线

以下证据全部齐备前，不得将 L3/L4 标记为完成：

- Agent 使用不可变镜像标签，镜像同时包含 agent-service、agent-mcp-acceptance 与 agent-mcp-conformance。
- Checkpoint 主密钥、MCP 凭据与验收报告签名密钥只通过 Kubernetes Secret 文件交付。
- 一个真实第三方 MCP 只读探针生成并通过人工复核的签名报告。
- 一个不可变 Workflow Publication 完成父子 Run、输出摘要、超时和失败传播验证。
- Approval、Resume、成员撤销、发布停用和超时演练均 fail-closed。
- Planning-Execute、External MCP、Workflow-as-Tool 可分别回滚。

## 2. 离线预检

在不访问公网和第三方服务时执行：

~~~powershell
helm lint deploy/helm/twitter-clone
helm template twitter-clone deploy/helm/twitter-clone -f deploy/helm/twitter-clone/values-agent-staging.example.yaml
go test ./cmd/agent-service ./internal/module/agent/mcp/acceptance ./cmd/agent-mcp-acceptance ./cmd/agent-mcp-conformance -count=1
~~~

确认渲染结果包含所有 AGENT_PLANNING_EXECUTE_*、AGENT_SESSION_SUMMARY_* 与 AGENT_RUNTIME_V2_MODES 配置。api/all 角色使用原生 gRPC Health Check；worker 角色使用 metrics 端口 TCP 探针。关闭时 gRPC Health 必须先切换为 NOT_SERVING。

## 3. Secret 交付

只允许从受控文件创建 Secret，不在命令行、Values、ConfigMap 或日志中写入明文：

~~~powershell
kubectl -n <namespace> create secret generic agent-checkpoint-key --from-file=key=<checkpoint-key-file>
kubectl -n <namespace> create secret generic agent-mcp-acceptance-credential --from-file=token=<token-file>
kubectl -n <namespace> create secret generic agent-mcp-acceptance-integrity --from-file=key=<report-signing-key-file>
~~~

## 4. MCP 真实验收

1. 先启用只读探针，使用唯一报告文件名和已有 PVC 保存报告。
2. 人工核对报告签名、Target、Catalog Hash、调用次数和固定错误码；报告不得包含 URL、凭据或原始工具正文。
3. 再启用需要逐次审批的幂等写探针；必须配置状态读回或权威业务回执。
4. 执行旧凭据撤销、项目成员撤销、DNS/证书错误、超时和多副本竞争演练。
5. Job 固定 backoffLimit=0；失败只能人工分析后创建新的 Job，禁止控制器自动重放写探针。

PVC 仅是报告交付介质，不等价于 WORM/不可篡改存储。验收后还需由部署平台复制到具备保留策略的对象存储或审计系统。

## 5. Workflow-as-Tool 真实验收

1. 创建只读演示 Workflow，保存不可变 Revision 后发布。
2. 绑定精确 Skill ID/Version，并从 Agent 入口执行 skill.run。
3. 核对 Publication Revision、Workflow DSL Hash、父 Run/Action、权威 Child Run、输出 JSON 摘要和成功状态。
4. 停用发布后再次调用，必须在子 Workflow 启动前失败。
5. 分别注入节点超时和失败，确认状态、错误码和父子链路一致。

## 6. 灰度顺序

1. 开启 Recoverable Run 与统一审批恢复。
2. 开启 Planning-Execute 主开关，将稳定用户灰度设为 1% 到 5%。
3. 依次开启平台只读工具、External MCP Read、Workflow Read。
4. 观察完成率、验证失败率、审批挂起量、预算拒绝量、延迟和远端错误码。
5. 只在前述证据稳定后开启 External MCP Write；Tweet 发布继续使用显式确认路径。

禁止一次性打开全部路由，也禁止用模型 FinalAnswer 代替现实完成证据。

## 7. 回滚

按以下顺序关闭，且不删除运行证据：

1. 关闭具体 Planning 路由开关。
2. 将 Planning canary 调为 0。
3. 分别关闭 External MCP 与 Workflow-as-Tool。
4. 清空 Runtime V2 Modes，回退兼容路径。

回滚时保留 Runs、Checkpoints、Approvals、Publications、审计日志与验收报告，便于复盘。

## 8. 证据清单

- 镜像 digest、Chart 版本和脱敏 Values 摘要。
- api/all 与 worker 健康探针记录。
- MCP 签名报告、旧凭据撤销结果和远端副作用计数。
- Workflow 发布/DSL/输出摘要与父子 Run 链路。
- Approval/Resume 一次性令牌和幂等重放结果。
- 超时、证书、DNS、成员撤销、发布停用和 Pod 故障演练。
- 灰度期间的低基数指标与告警截图。

只有真实部署证据可以关闭 L3；Helm 渲染、单元测试和回环 Conformance 只能证明实现入口存在。

所有 L3/L4/L5 证据应通过 `scripts/production-acceptance-record.ps1` 登记到 HMAC 签名记录，并在发布前使用 `-RequireReleaseReady` 验证。固定 26 项目录、外部签名密钥、脱敏附件和篡改检测操作见 `docs/production_launch_acceptance.md`。记录工具不执行 Live Check，也不能把离线夹具转换为生产证据。

## 9. Compose/Host 生产基线

单机演示或小规模 Beta 使用 `docker-compose.production.yaml`，并在启动前执行 `scripts/validate-production-compose.ps1`。完整命令、镜像 digest、仓库外 Secret/TLS 文件、唯一公网 Edge 入口和回滚要求见 `docs/production_compose_deployment.md`；备份恢复与保留边界分别见 `docs/production_backup_restore.md`、`docs/data_retention_policy.md`。

预检通过只证明配置结构满足静态门禁；只有真实镜像 clean-host 启动、健康检查、重启保留状态和回滚演练完成后，才能关闭 L4 对应验收项。
