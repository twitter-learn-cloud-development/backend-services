# 外部 MCP 生产验收与故障演练

> 状态：验收框架已实现；真实第三方与生产 Kubernetes 证据待受控执行  
> 入口：`cmd/agent-mcp-acceptance`、`cmd/agent-mcp-conformance`

## 1. 定位与真实性边界

`agent-mcp-acceptance` 是显式运行的运维验收命令，不进入 Agent Service 请求热路径。它复用生产 `EndpointPolicy`、受限 HTTP Client、MCP SDK Adapter 和有界 Session Pool，对指定远程 MCP Server 执行：

1. 凭据引用加载。
2. MCP `ping`。
3. Tool Discovery 与 Schema Catalog 摘要。
4. 已声明只读的工具调用。
5. 可选的声明式幂等写入重放与只读状态核验。
6. 可选的 Projected Secret 文件轮换观察。

本地 `agent-mcp-conformance` 只绑定回环地址，用于验证协议和验收器本身。它不是第三方 Server，也不是生产网络、多副本或代理行为的证据。

验收器只能证明运行时观察到的响应一致性或状态计数。第三方的 `idempotentHint` 和项目扩展元数据仍是远端声明，不能据此声称跨系统严格 exactly-once。

## 2. 安全不变量

- 网络调用必须显式提供 `--allow-live`。
- 写探针必须同时存在于配置并显式提供 `--allow-write`。
- Bearer Token 只能来自环境变量或只读文件，不能写入 JSON 配置。
- 所有非回环端点都要求 HTTPS；只有配置中显式允许的回环地址可使用 HTTP。
- 探针参数递归拒绝 Token、Secret、Password、Cookie 等凭据型字段。
- 报告只保存 Endpoint、Config、Catalog、Schema 和结果的 SHA-256，不保存 URL、Token、原始工具结果或错误正文。
- 可用独立 HMAC-SHA256 密钥签名报告；生产 Job 默认要求签名。
- Kubernetes Job 使用 `backoffLimit: 0`，避免 Job Controller 自动重放写探针。
- 凭据轮换只证明新凭据摘要建立了新 Session Identity；命令退出时关闭池，不伪造单身份远端撤销证明。

## 3. 配置契约

示例位于：

`internal/module/agent/mcp/acceptance/testdata/conformance_config.example.json`

配置 Schema 固定为 `agent-mcp-acceptance-config/v1`。核心字段：

| 字段 | 说明 |
|------|------|
| `target` | 报告中的稳定目标标识，不是 URL |
| `transport` | `streamable_http` 或 `sse` |
| `endpoint` | 真实端点，仅以哈希进入报告 |
| `allowed_hosts` | Endpoint Policy 的显式主机允许列表 |
| `auth` | `none`，或只引用环境变量/文件的 `bearer` |
| `read_probe` | 必须命中远端声明为只读且非破坏性的工具 |
| `idempotency_probe` | 可选；要求 `idempotentHint=true` 和必填字符串幂等键元数据 |
| `state_verification` | 可选；使用只读工具检查同一稳定键的可观察副作用计数 |

真实第三方只读验收应移除 `idempotency_probe`，且不要提供 `--allow-write`。

## 4. 本地协议验收

终端一启动回环 Conformance Server。该命令是常驻服务，看到监听信息后应保持运行，结束时按 `Ctrl+C`：

```powershell
$env:AGENT_MCP_CONFORMANCE_TOKEN='0123456789abcdef0123456789abcdef'
go run ./cmd/agent-mcp-conformance
```

终端二运行一次验收并写入不可覆盖的签名报告：

```powershell
$env:AGENT_MCP_CONFORMANCE_TOKEN='0123456789abcdef0123456789abcdef'
$env:AGENT_MCP_ACCEPTANCE_INTEGRITY_KEY='abcdef0123456789abcdef0123456789'
$env:AGENT_MCP_ACCEPTANCE_INTEGRITY_KEY_ID='local-v1'

go run ./cmd/agent-mcp-acceptance `
  --allow-live `
  --allow-write `
  --require-complete `
  --require-signed `
  --config internal/module/agent/mcp/acceptance/testdata/conformance_config.example.json `
  --out tmp/agent-mcp-acceptance/local-report.json
```

单独验证已有报告：

```powershell
go run ./cmd/agent-mcp-acceptance `
  --verify-report tmp/agent-mcp-acceptance/local-report.json
```

`--out` 目标已存在时命令拒绝覆盖。需要新证据时使用新的不可变文件名，不要删除或改写原报告冒充连续证据。

## 5. 证据等级

| `evidence_level` | 含义 | 不代表 |
|------------------|------|--------|
| `protocol` | MCP Ping 成功 | 工具业务语义正确 |
| `schema_catalog` | Discovery 成功且 Schema 可摘要 | Snapshot 已经人工审核 |
| `read_result_digest` | 只读调用成功并生成结果摘要 | 外部数据真实、完整 |
| `response_consistency` | 相同稳定键两次响应或回执一致 | 远端只产生一次副作用 |
| `observable_state` | 额外只读状态工具返回预期副作用计数 | 分布式严格 exactly-once |
| `projected_secret_rotation` | 文件凭据变化后新身份 Ping 成功 | 旧 Token 已在远端撤销 |

报告状态：

- `passed`：所有已配置步骤通过。
- `partial`：配置了写探针但未提供 `--allow-write` 等显式授权，步骤被跳过。
- `failed`：至少一个步骤失败；报告只记录固定错误码。

## 6. Kubernetes Job

Helm 默认不渲染验收 Job。启用前必须准备：

1. 不含凭据的严格 JSON `configJSON`。
2. 可选的现有凭据 Secret；轮换观察必须使用整个 Secret Volume，不得使用 `subPath`。
3. 报告 HMAC Key 与 Key ID 的现有 Secret。
4. 对写探针的独立变更审批；只读验收保持 `allowWrite=false`。

关键 Values：

```yaml
services:
  agentService:
    externalMCP:
      acceptance:
        enabled: true
        configJSON: |
          {
            "schema_version": "agent-mcp-acceptance-config/v1",
            "target": "staging-third-party",
            "transport": "streamable_http",
            "endpoint": "https://mcp.example.com/mcp",
            "allowed_hosts": ["mcp.example.com"],
            "auth": {
              "type": "bearer",
              "bearer_token_file": "/var/run/secrets/agent-mcp-acceptance/token"
            },
            "read_probe": {
              "tool": "health_read",
              "arguments": {}
            }
          }
        credentialExistingSecret: "agent-mcp-acceptance-credential"
        integrityExistingSecret: "agent-mcp-acceptance-integrity"
        allowWrite: false
        expectCredentialRotation: false
        requireComplete: true
        requireSigned: true
```

Job 使用固定非 root UID/GID、只读根文件系统、关闭 ServiceAccount Token、移除 Linux Capabilities，并设置运行时限和完成后 TTL。报告默认输出到 Pod stdout，生产归档应由受控日志/对象存储流程完成；模板本身不把证据自动写入业务数据库。

## 7. 受控演练矩阵

| 场景 | 操作 | 通过条件 |
|------|------|----------|
| 只读第三方 | 不配置写探针 | Ping、Discovery、只读调用与签名报告通过 |
| Tool Schema 漂移 | 第三方升级工具 Schema | Catalog Hash 变化；生产 Connection 仍要求重新审核 Snapshot |
| 调用超时 | 将探针超时设为小于远端延迟 | 非零退出，报告错误码为 `timeout`，无错误正文 |
| 凭据轮换 | 更新整个 Projected Secret Volume | 新 Credential Identity Ping 成功，报告不包含旧/新 Token |
| 旧凭据撤销 | 在第三方控制面撤销旧 Token | 使用旧 Token 的独立负向探针失败；不能只靠本命令推断 |
| 幂等重放 | 经审批启用写探针 | 同一稳定键响应一致；有状态查询时副作用计数等于配置值 |
| 多副本故障 | 并发运行只读验收，并在调用中重启 Agent/MCP 代理 | 无无界 Session/Goroutine；失败有界且可重跑 |
| Registry Version 漂移 | 更新 Registry 版本但不 CAS 采纳 | Agent Service 运行路径 fail-closed，重新保存并重审后才恢复 |

## 8. 当前待执行项

- 使用一个真实公网或企业第三方 MCP Server 生成并人工复核签名报告。
- 在 Kubernetes 受控命名空间执行 Projected Secret 轮换和旧 Token 撤销负向测试。
- 注入代理空闲断连、DNS/证书错误、Pod 重启和多副本竞争。
- 通过真实项目 Owner/Editor/Viewer 与 User Service 链路验证成员撤销即时生效。
- 对允许写入的第三方逐项确认其状态查询或业务回执，不能只信任注解。
