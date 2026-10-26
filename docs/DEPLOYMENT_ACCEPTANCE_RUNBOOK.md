# M8-R3 部署验收与证据门禁

日期：2026-10-15。用户范围：本轮完成验收工具与离线验证；真实 staging/恢复/故障旅程尚未执行。模块整体保持Partial，工具验证结果见PROJECT_PROGRESS。

| 功能模块 | 轮次/子项 | 实施及验收 |
|---|---|---|
| 发布身份 | M8-R3-A | v2签名记录绑定经独立审核的发布清单SHA256、源码SHA、部署ID与环境快照SHA256；候选镜像、版本或配置漂移拒绝 |
| 现场观察 | M8-R3-B | 每个passed检查都提供独立live观察回执，绑定检查ID/部署/版本/环境/操作角色/发生时间与原始证据摘要；缺项、复用、过期及篡改拒绝 |
| 用户旅程 | M8-R3-C | 保留L3/L4/L5既有26项，增加认证、搜索可见性、Feed/私信、Schema owner、共享准入5项；真实操作须使用指定staging身份/依赖与批准范围 |
| 收口与回退 | M8-R3-D | v1记录仍可核验但不能通过新的发布门禁；v2工具离线测试/CI/文档接线；未提供真实环境证据时整体保持Partial |

架构：复用现有production-acceptance-record的HMAC/状态和证据文件模型，追加纯本地的身份与观察校验。不会部署、启动依赖、恢复数据库、发送告警或调用Provider/MCP。发布清单始终保留`deployment_accepted=false`；完整证据门禁通过只证明受信任签名者提交的材料一致，不证明材料内容真实或实际部署已获批准。

兼容/迁移：不修改HTTP/Proto/Schema。已有v1记录不重签/覆盖为v2；新部署使用新文件初始化v2，旧记录保留审计。计划追加检查会改变plan摘要，旧记录需按原计划核验，不能静默迁移Passed。回退应用前停止/排空不允许混跑的owner，保留表/事件/位点/预算/备份，不删除卷，不恢复启动DDL或旧鉴权竞态。

风险：期望摘要必须来自独立审核的CI/发布渠道，不能从待验证文件自己计算后当作信任来源。HMAC持有者可以伪造陈述，密钥保管/权限和真实日志/读回必须另审；环境快照只放公开版本与Secret引用版本，不包含Secret、JWT、DSN或其可离线猜测的明文摘要。离线夹具不是现场验收。

## 1. 发布与环境身份

先在匹配发布SHA的干净Checkout执行M8-R1验证，确认清单源于受信任CI、完整12镜像/9检查已发布，不能把手填digest或本机夹具当作CI来源。公开清单可放`.codex-tmp`，签名密钥、记录和原始现场证据放仓库外、仅允许操作方写入的目录。

```powershell
go run ./cmd/release-manifest --verify-manifest=.codex-tmp/release-manifest.json --require-published
```

v2再次检查经审核的清单字节摘要、source/catalog、12镜像/9检查、基础镜像digest与image_environment。该步骤不访问Registry，也不替代上面的源码/输入漂移验证。环境快照采用以下结构，`images`必须复制完整12项`image_environment`，基础设施镜像同样按部署实际值固定digest：

```json
{
  "schema": "twitter-clone.deployment-environment.v1",
  "deployment_id": "staging-20261015-run01",
  "environment": "staging",
  "source_sha": "<reviewed-40-character-source-sha>",
  "release_manifest_sha256": "<reviewed-64-character-manifest-hash>",
  "configuration_revision": "staging-config-r1",
  "secret_revision": "vault-reference-version-r1",
  "schema_revision": "reviewed-owner-schema-r1",
  "previous_release_manifest_sha256": "<reviewed-previous-release-hash>",
  "images": {"<all-12-image-env-names>": "<repository@sha256:actual-digest>"},
  "infrastructure_images": {"mysql": "<repository@sha256:actual-digest>"},
  "data_deletion_enabled": false,
  "token_revocation_supported": false
}
```

这是不具备可用摘要的格式示例，不能直接作为验收输入。配置/Secret/Schema版本均为公开的受控引用，不是Secret本身的哈希；快照必须由实际环境读回后审核，复制发布清单到快照不能证明镜像已运行。前版清单摘要只绑定回退目标，实际拉取/启动/恢复仍需独立证据。

签名者分别取得审核过的两个摘要及source SHA。每次Initialize/SetCheck/Verify都显式提供这些期望值，不能只信任记录自报身份：

```powershell
$bound = @{
  RecordFile = 'E:\release-evidence\staging-20261015\acceptance.json'
  SigningKeyFile = 'E:\release-secrets\acceptance-hmac.bin'
  SigningKeyID = 'staging-acceptance-key-v2'
  ReleaseManifestFile = '.codex-tmp/release-manifest.json'
  ExpectedReleaseManifestSHA256 = '<independently-reviewed-manifest-sha256>'
  ExpectedSourceSHA = '<reviewed-source-sha>'
  EnvironmentSnapshotFile = 'E:\release-evidence\staging-20261015\environment.json'
  ExpectedEnvironmentSnapshotSHA256 = '<independently-reviewed-environment-sha256>'
  Environment = 'staging'
}
./scripts/production-acceptance-record.ps1 -Mode Initialize @bound -ReleaseID 'release-20261015-r1' -Owner 'staging-operator'
```

工具不生成真实密钥、不抓取环境或Secret、不替用户建仓库外目录。先准备既有32–4096字节HMAC密钥和受控目录，不在对话/命令行放密钥内容。`AllowRepositoryFiles`只供隔离的合成夹具，不用于真实验收。

## 2. 每项现场观察

原始日志、Trace、后端读回和截图需先脱敏。对每一项检查单独形成观察JSON，不能复用其他check_id的回执；一个观察最多引用8份、每份不超过50MiB的非空原始证据，路径相对EvidenceRoot，不允许逃逸或reparse/symlink父路径。观察文件单独保存，不能充当自身原始证据。JSON/记录/计划不超过1MiB、JSON深度32且拒绝重复键；计划最多64项。

```json
{
  "schema": "twitter-clone.deployment-observation.v1",
  "execution_mode": "live",
  "check_id": "l5.auth_session",
  "status": "passed",
  "source_sha": "<reviewed-source-sha>",
  "release_manifest_sha256": "<reviewed-manifest-sha256>",
  "deployment_id": "staging-20261015-run01",
  "environment": "staging",
  "environment_snapshot_sha256": "<reviewed-environment-sha256>",
  "actor_ref": "fixture-account-reference",
  "observed_at_utc": "<actual-UTC-ISO8601-time>",
  "evidence": [{"path":"auth-session-redacted.log","bytes":123,"sha256":"<actual-evidence-sha256>"}]
}
```

`actor_ref`使用测试账号或受控角色引用，不录入密码、Token或邮箱。`execution_mode=live`表示这份陈述来自目标部署的实际观察，离线测试不可据此补造现场成功。观察时间须在当前24小时内、不得早于记录创建5分钟之前或晚于当前5分钟；24小时后需重新执行并记录，避免沿用陈旧旅程。原始证据字段/顺序/长度/摘要必须与`-Evidence`一致，工具核验身份与字节完整性，不解读原始日志的业务正确性。

```powershell
$evidenceRoot = 'E:\release-evidence\staging-20261015\evidence'
./scripts/production-acceptance-record.ps1 -Mode SetCheck @bound -EvidenceRoot $evidenceRoot -CheckID 'l5.auth_session' -CheckStatus passed -Evidence 'auth-session-redacted.log' -ObservationFile 'auth-session-observation.json'
./scripts/production-acceptance-record.ps1 -Mode Verify @bound -EvidenceRoot $evidenceRoot -RequireReleaseReady
```

`failed/blocked`写明确原因，可以携带失败材料，但不携带passed观察；`not_run`不能带结果。SetCheck使用同记录旁的文件锁，另一写者持锁时明确拒绝；初始化不覆盖已有记录，临时输出使用随机名。记录/HMAC/观察/原始证据任一变动、材料缺失或部署身份变化都会阻断Verify。文件锁不是分布式审批/RBAC，存储目录需受操作系统权限保护；IO故障留下的临时文件保留排查，不自动覆盖或删除。

v1仍允许按其原计划核验/更新，但即使全部Passed，也输出`release_ready=false`、`release_bound=false`；不能通过`RequireReleaseReady`。v2全部31项通过才可能输出`release_ready=true`，同时仍输出`deployment_accepted=false`。前者仅表示本地证据门禁完整，最终部署/流量批准需要外部审核。

## 3. 旅程与实际证据

既有26项完整目录见`deploy/acceptance/production-launch-checks.json`。新增项按以下条件验收；不得为了凑齐31项把未完成代码、关闭能力或故障演练写成Passed。

| 检查ID | 现场操作及应有证据 | 当前边界 |
|---|---|---|
| l5.auth_session | Web/Mobile登录、退出、旧401/迟到响应与改密成功/错误密码/不确定回执；双端读回和脱敏网络证据 | 本地退出，JWT到期前仍有效；不得写成全设备撤销已实现 |
| l5.search_visibility | 自有fixture的Public/Follows/Private/Shadowban/删除，MCP/RAG候选到正文输出前排除，索引延迟及服务失败也不得旧正文兜底 | 索引是派生状态，必须读当前事实；写入/删除测试数据须另有授权 |
| l5.feed_messaging | 两端多页/空页/重复/深历史，断线消息补拉与已读边界；固定ID读回和执行计划 | M2-R3完整容量仍Partial；M4-R2代码/离线Implemented，真实恢复/跨端/容量未Accepted，不能替它们补造现场通过 |
| l5.schema_ownership | 指定owner迁移/旧表漂移拒绝、运行账号不能DDL或越权，API/Worker角色隔离 | 真实账号/DDL/权限需单独批准；schema-migrate默认离线 |
| l5.shared_admission | 多副本并发/预算、Redis丢失或重启、租约失效取消，指标/运行与配置版本读回 | 开关默认关闭；不能把Redis租约等同Provider取消或真实账单封顶 |
| l4.retention_deletion | 留存策略读回与不支持删除的拒绝/审计 | 检查描述已澄清：数据删除继续关闭，不要求开启未验收删除 |

备份/恢复先用`production-backup-manifest.ps1 -Mode Verify`验证既有七类备份摘要，再在指定恢复目标执行隔离恢复并读回；manifest完整不证明可恢复。`l4.rollback`同时提供前版发布清单、环境快照、兼容迁移声明、owner停止/排空及恢复后读回。`l3/l5`真实Provider/MCP/审批/撤权演练沿各专项手册和固定身份/费用授权；TLS/告警须真实浏览器和目标接收者证据，不能用本机curl或配置文件代替。

本轮没有启动服务、联网探测staging、调用Provider、执行SQL/数据恢复、发送告警或发布/部署。后续执行前按environment_context核对所需软件、模型、端口、用途和时长，并确认目标与授权范围。

## 4. 离线验证与当前结论

```powershell
./scripts/test-deployment-evidence.ps1
./scripts/test-production-acceptance-record.ps1
go test ./internal/releaseplan ./cmd/release-manifest -count=1
go vet ./internal/releaseplan ./cmd/release-manifest
go test -race ./internal/releaseplan ./cmd/release-manifest -count=1
```

CI production-config任务接入新v2夹具；Go/Web/Mobile业务契约未变，本轮无需重复前端工具链。正式Actions/Registry/部署证据仍未验收。当前工作区有大量未提交内容，必须先由用户提交/审核出精确源码版本，再执行M8-R1清单流程；不得为生成清单自动提交或清理现有修改。

2026-10-15最终本机结果：四脚本AST、31项v2/24拒绝用例、31项v1兼容且不可release-ready、发布工具Test/Vet/Race、12服务CLI矩阵和CI接线通过。CLI曾因新shell未指定GOCACHE访问默认缓存被拒，显式使用repo/tmp/go-build-cache后通过；没有绕过审批或把权限失败当业务失败，见ISSUES246。当前系统上的路径夹具通过不等同真实Linux Actions或staging运行。
