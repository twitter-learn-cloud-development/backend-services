# Production Launch Acceptance Record

该记录把 L3/L4/L5 的上线声明转换为可验证清单。它不执行测试、部署或外部调用；操作者先完成动作，再把脱敏证据文件登记进记录。离线单测、截图或“看起来正常”不能替代对应 Live Check。

## 1. 安全边界

- Record、签名密钥和 Evidence Root 默认必须位于仓库外。
- Record 只保存 Check 状态、简短原因、相对证据路径、字节数和 SHA-256，不保存 Token、Cookie、Prompt、响应正文或数据库转储。
- 记录使用外部 HMAC-SHA256 Key 签名；修改 JSON、计划或证据文件后验证会失败。
- HMAC 证明持有同一部署密钥的人签发了当前记录，不等于 WORM。最终证据包仍应复制到受保留策略保护的私有对象存储。
- `passed` 必须至少绑定一个非空证据文件；`failed/blocked` 必须写原因；`not_run` 不得伪装证据。

## 2. 创建密钥与记录

~~~powershell
$root = "$env:USERPROFILE\twitter-clone-acceptance\release-2026-09"
New-Item -ItemType Directory -Force -Path "$root\evidence" | Out-Null
$key = New-Object byte[] 32
[Security.Cryptography.RandomNumberGenerator]::Fill($key)
[IO.File]::WriteAllBytes("$root\acceptance.hmac", $key)

./scripts/production-acceptance-record.ps1 `
  -Mode Initialize `
  -RecordFile "$root\acceptance.json" `
  -SigningKeyFile "$root\acceptance.hmac" `
  -SigningKeyID "launch-hmac-v1" `
  -ReleaseID "release-2026-09" `
  -Environment "staging" `
  -Owner "platform-owner"
~~~

密钥由部署账户独占，禁止提交仓库、粘贴聊天或作为命令行值传递。轮换 Key 时创建新的 Release Record，不覆盖旧记录。

## 3. 登记检查结果

证据文件先进行人工脱敏，再放入 Evidence Root。示例：

~~~powershell
./scripts/production-acceptance-record.ps1 `
  -Mode SetCheck `
  -RecordFile "$root\acceptance.json" `
  -EvidenceRoot "$root\evidence" `
  -Evidence "compose-preflight.txt","compose-ps.txt" `
  -SigningKeyFile "$root\acceptance.hmac" `
  -SigningKeyID "launch-hmac-v1" `
  -CheckID "l4.clean_host_health" `
  -CheckStatus passed
~~~

失败或外部条件缺失必须如实登记：

~~~powershell
./scripts/production-acceptance-record.ps1 `
  -Mode SetCheck `
  -RecordFile "$root\acceptance.json" `
  -SigningKeyFile "$root\acceptance.hmac" `
  -SigningKeyID "launch-hmac-v1" `
  -CheckID "l3.mcp_read_live" `
  -CheckStatus blocked `
  -Reason "No approved staging MCP endpoint"
~~~

Reason 不得包含 Endpoint Credential、Authorization Header、用户正文或原始错误响应。

## 4. 验证与发布门禁

~~~powershell
./scripts/production-acceptance-record.ps1 `
  -Mode Verify `
  -RecordFile "$root\acceptance.json" `
  -EvidenceRoot "$root\evidence" `
  -SigningKeyFile "$root\acceptance.hmac" `
  -SigningKeyID "launch-hmac-v1"
~~~

日常 Verify 会报告 Required/Passed/Failed/Blocked/Not Run。发布前使用严格门禁：

~~~powershell
./scripts/production-acceptance-record.ps1 `
  -Mode Verify `
  -RecordFile "$root\acceptance.json" `
  -EvidenceRoot "$root\evidence" `
  -SigningKeyFile "$root\acceptance.hmac" `
  -SigningKeyID "launch-hmac-v1" `
  -RequireReleaseReady
~~~

只有 26 项 Required Check 全部为 `passed` 且证据文件哈希一致时，命令才以成功状态退出。真实发布还需人工确认 Record Owner、证据内容、目标域名和流量扩张窗口。

## 5. 固定旅程证据

`l5.fixed_journey` 必须在同一隔离账号和 Release 上完成：

1. 多轮对话，并在上下文压缩后核对既有事实。
2. 站内与公网搜索，核对结构化引用可打开且归属正确。
3. 基于引用生成草稿，不允许混入评测元数据或无来源事实。
4. 触发一个明确审批动作；审批前副作用为零。
5. 批准后只执行一次发布或选定幂等写操作。
6. 查看 Run、Plan 摘要、Tool、Usage、Verifier 和 Sanitized Error。
7. 对一个挂起 Run 执行 Resume，确认不重复已成功副作用。

建议证据包含脱敏的 Run Inspection JSON、审批审计摘要、目标系统后状态摘要、相关 Dashboard 截图与 Feature Flag 快照。禁止收集完整 Prompt、模型正文、Credential 或生产用户数据。
