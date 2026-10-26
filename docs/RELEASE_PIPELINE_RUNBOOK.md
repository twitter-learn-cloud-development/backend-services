# M8-R1 发布流水线与验证收口

> 2026-10-15 M8-R3后续：production-config接入v2部署证据夹具，验收目录现31项，签名记录绑定审核过的发布清单/source/环境摘要/部署ID及逐项现场观察。旧v1不再通过release-ready；发布manifest自身仍永远deployment_accepted=false。当前用户仅要求工具与离线验证，真实部署未验收；操作、兼容与风险见 `DEPLOYMENT_ACCEPTANCE_RUNBOOK.md`，下方M8-R1七/16/26项数字是当时验证快照。

日期：2026-10-13。状态：Implemented（代码与本机离线验证）；审批恢复后完整Go/Race、Web构建/7项测试及Flutter零分析问题/16项测试全部通过。真实GitHub Actions/镜像构建/Registry发布/部署未Accepted。

## 1. 功能模块—轮次

| 子项 | 实现 | 当前证据 |
|---|---|---|
| M8-R1-A 包级构建 | Makefile build/run均使用./cmd包目录，build覆盖全部cmd；10个Go服务Dockerfile按包编译，保留Agent MCP两个运维程序并追加admission-admin。 | 全仓编译、Windows包级构建与CGO=0 Linux/amd64包级构建通过；没有运行生成二进制。 |
| M8-R1-B 检查链路 | CI按Go.mod选择Go，Go Test/Vet/指定Race、全部cmd Linux构建；Web npm ci/build/test，Mobile固定Flutter3.44.2/lockfile/analyze/test；生产配置及验收记录夹具。 | 发布工具Test/Race、完整Go全仓/CI指定工程Race/全仓Vet、Web完整build/7测试、Flutter analyze零问题/16测试、Compose九项和26项验收记录自测通过。 |
| M8-R1-C 镜像矩阵 | deploy/release/catalog.json为12组件矩阵：10后台/接入服务、frontend、独立schema-migrate。PR/main默认仅候选OCI构建；仅main显式workflow_dispatch publish可上传仓库。 | JSON/YAML/Compose覆盖与包路径静态测试通过；真实12镜像构建、上游Actions与Registry身份未验收。 |
| M8-R1-D 发布证据 | release-manifest默认离线输出目录/矩阵，完整12镜像+9检查回执、Git真实干净版本、配置/锁文件/SQL与Schema目录摘要；只读Verify和require-published。 | Fake回执拒绝矩阵、文件漂移/错误版本/缺项/坏digest/重复键/候选/伪部署完成拒绝通过；CLI在当前脏工作区明确拒绝生成真实发布清单。 |

本轮不更改HTTP/Proto；Mobile修复分析发现的弃用API/死导入、相关await后页面有效性与Workflow对话框Context，真实应用烟测/迟到回调测试代替旧计数器模板。不执行SQL、真实Redis初始化或业务依赖启动，用户删除继续关闭。旧Helm values与tag型演示导出工具仍属于历史路径；本轮删除CI自动更新Helm/推Git逻辑，不以新Compose发布证据宣称Helm生产部署已收口。

## 2. 构建与CI契约

目录中12组件覆盖docker-compose.yaml所有11个build服务，并追加默认离线的一次性Schema Job；Agent的api/worker/all共用同一镜像，其角色依赖门禁沿用M7-R2。所有其他cmd管理/评测程序通过包级编译门禁，但并非全部打包进运行镜像；使用独立精确版本二进制时仍须遵守各自Live授权与运维手册。

- Go版本由go.mod（当前1.25.5）决定；容器可传BUILDER_IMAGE/RUNTIME_IMAGE，发布模式必须有效非零repository@sha256。开发构建保留Go1.25.5 Alpine/Alpine兼容tag，不能由此宣称构建完全可复现。
- Docker路径上下文的vendor由CI go mod vendor生成，Docker保持-mod=vendor/-trimpath/CGO=0；根.dockerignore排除Git、临时、.env/私钥/Agent本地配置，web独立上下文另有.dockerignore。不能因为存在忽略文件就把仓库视为凭据扫描已通过。
- Web统一使用既有package-lock.json与npm ci，保留pnpm-lock历史文件；新增npm test显式Node Type Stripping以兼容本机Node22.13，CI Node22。Docker EXPOSE修正为现有nginx实际5556，不改变nginx监听行为。
- CI Flutter固定3.44.2，与当前Dart3.12.2要求一致，pub get --enforce-lockfile后以--no-pub分析/测试。该门禁不构建或签发Android/iOS安装包。
- 质量门禁有超时且管道pipefail；失败不记录Passed。镜像矩阵从受校验目录生成，max-parallel=3、失败不取消其他构建；任何检查或组件失败，完整清单无法组装。

采用Docker显式Path context，使CI生成的vendor进入构建；候选使用OCI exporter，发布使用Registry exporter并读取实际构建digest。该能力以官方[build-push-action说明](https://github.com/docker/build-push-action#path-context)及[OCI exporter说明](https://docs.docker.com/build/exporters/oci-docker/)为设计依据，尚未在本仓库执行远端CI确认。

发布依赖Go/Node/Flutter、npm/Go/Flutter模块源、基础镜像、Alpine CA包源、Actions与Registry网络；固定基础digest、锁文件与源码并不覆盖apk仓库/动作可变tag或全部工具链供应链，不宣称完全hermetic或已完成SBOM/签名审计。发布前需固定组织认可的Actions版本/基础镜像与信任策略，另审镜像内容和架构。

## 3. 发布与回滚

默认push main/PR/workflow_dispatch publish=false不登录Registry、不上传镜像、不写Git或Helm、不部署。候选OCI文件仅在构建Job中校验，当前只上传digest回执，OCI tar不作为可下载发布产物保留；清单images_published=false且deployment_accepted=false。候选docker.io/twitter-clone仅作命名空间，不代表那些digest在Registry可拉取。

显式发布条件：main上的workflow_dispatch publish=true；设置DOCKER_NAMESPACE变量、DOCKER_USERNAME/DOCKER_PASSWORD凭据；每组件Builder/Runtime都必须提供审核过的非零digest。镜像使用源码SHA tag，不写latest；记录每个实际Registry输出digest，全部回执必须一致published状态。release环境应由仓库管理员配置审核/分支与Secret保护，workflow声明本身不证明组织批准已经设置；没有自动晋级或部署步骤。

1. 提交并审核源码；Git HEAD必须匹配传入SHA，tracked/untracked改动拒绝，忽略的CI日志/vendor不算源码变化。当前开发工作区有既有未提交变更，不为了生成清单而替用户提交或清理。
2. 干净精确版本执行完整CI。将运行记录、构建日志、回执/清单从同一次可信CI运行归档；这些回执是输入声明，不是签名证明，不能用手填假的digest/Passed代替真实构建测试。
3. 只有已发布清单才可映射image_environment到独立生产配置；11运行应用与Schema Job的digest齐备，但其余基础设施digest、真实Secret/证书、数据库账号/Schema仍须独立审核。不要把候选清单或生产模板零digest用于部署。
4. 在同源码Checkout只读verify --require-published；实际生产.env/Secret不读入发布清单，它只绑定模板/公开配置/迁移文件。真实部署配置身份、HMAC验收记录与证据仍按production-acceptance-record及M8-R3收口，九项fixture只证明预检实现。
5. 应用升级先满足各模块迁移/owner/队列/开关条件；独立schema-migrate Job默认离线，pipeline不会启动它或自动DDL。保留前版可信manifest/精确应用digest/配置身份与兼容说明，回滚前停止/排空不允许混跑的Owner；保留SQL结构、Outbox/位点、租约/预算及运行证据。

本轮manifest永远保持deployment_accepted=false。Registry写成功不证明镜像可拉取/内容正确、配置就绪或用户旅程成功。签名、Registry复验、真实clean-host/多副本/备份恢复与告警旅程归后续M8-R3。

## 4. 离线命令

```powershell
# 默认只读离线目录/矩阵，可在脏开发工作区查看。
go run ./cmd/release-manifest
go run ./cmd/release-manifest --matrix
# 发布输入预检，不构造Docker或Registry Client。
go run ./cmd/release-manifest --validate-bases --builder-image=<reviewed-go-image@sha256> --runtime-image=<reviewed-alpine-image@sha256> --published
# 以下用于可信CI的干净Checkout，不能用手填回执代替实际结果。
go run ./cmd/release-manifest --record-image=<component> --source-sha=<exact-head> --repository=<registry/namespace/component> --digest=<actual-build-sha256> --builder-image=<builder> --runtime-image=<runtime> --published --out=.codex-tmp/images/<component>.json
go run ./cmd/release-manifest --record-check=<required-kind> --source-sha=<exact-head> --evidence-file=<bounded-command-log> --out=.codex-tmp/checks/<kind>.json
go run ./cmd/release-manifest --assemble --source-sha=<exact-head> --receipts-dir=.codex-tmp/images --checks-dir=.codex-tmp/checks --out=.codex-tmp/release-manifest.json
go run ./cmd/release-manifest --verify-manifest=.codex-tmp/release-manifest.json --require-published
```

required-kind为go_test/go_vet/go_race/package_build/web_build/web_test/mobile_analyze/mobile_test/production_config。真实CI在命令成功后才记录检查，工具不会自行执行检查/构建/Registry写入。来源检查最多5秒、Git输出最多64KiB；JSON/证据每文件1MiB、目录最多64组件/64SQL，路径禁止逃逸/符号链接，JSON拒绝未知/重复键及尾随内容，写输出O_EXCL禁止覆盖已审清单。IO故障可能留下不完整输出，该文件必须重新审核，以新文件名输出，不自动删除或覆盖。

## 5. 本轮最终验证与外部验收

本机复验收口日期：2026-10-14；开发轮次起始日期保留2026-10-13。

已通过：目标releaseplan/CLI Test/Race/Vet、完整Go全仓与CI同范围工程Race、全仓Vet、全部cmd Windows/Linux包级编译、Web完整build/npm test七项、Flutter analyze零问题/test16项、实际docker compose config-only九项夹具、26项生产验收记录自测、YAML/目录覆盖/PS Parser与本轮格式。Fake镜像/检查回执仅用于结构与拒绝逻辑测试，不生成真实已发布证据。

历史阻塞：2026-10-13自动审批额度耗尽，用户选择恢复后复验；本次正常审批已通过，没有绕过审批。复验发现Mobile 39项warning/info与旧Widget模板初始化/计数器断言错误，按实际诊断修复并全部通过，不禁用Lint。登录/注册烟测以Mock Storage/ProviderScope启动，Dio拦截器禁止网络并断言零调用；详情页面卸载后迟到成功/失败均不触发UI异常。修复只覆盖本次已发现路径，不证明所有移动异步逻辑/真实接口已全面验收。

剩余外部验收：真实12镜像构建、clean-host锁安装/Actions/Registry发布、镜像拉取、生产Secret/权限与部署。本次没有安装或升级本机依赖/启动业务服务/发布/部署。精确命令见ARCHITECTURE_REMEDIATION_PLAN的M8-R1交付记录；GOCACHE使用repo缓存，旧tmp/go-tmp产物通过忽略tmp/go.mod隔离，新产物优先.codex-tmp。ISSUES244已Resolved（本机代码验证），下一代码轮次M8-R2，真实部署旅程归M8-R3。
