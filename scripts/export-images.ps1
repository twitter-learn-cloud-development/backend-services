# ==============================================================================
# export-images.ps1
# 构建业务镜像 + 拉取第三方基础镜像，并逐个导出为独立 tar 包，便于离线传输到服务器。
#
# 用法:
#   powershell -ExecutionPolicy Bypass -File ./scripts/export-images.ps1
#   powershell ... -Version 20260926-2                      # 指定版本
#   powershell ... -SkipBuild -SkipPull                      # 仅重新导出本地已有镜像
#
# 产物: dist/release/<版本>/
#   images/<名称>.tar     每个镜像一个 tar
#   MANIFEST.txt          镜像引用 -> tar 文件 映射
#   SHA256SUMS.txt        tar 文件校验和
#   load-images.sh        服务器端一键 docker load
#   docker-compose.deploy.yaml  服务器端部署文件（业务服务 build 已换成 image:）
# ==============================================================================
[CmdletBinding()]
param(
    [string]$Version = (Get-Date -Format 'yyyyMMdd') + '-1',
    [string]$OutputRoot = 'dist/release',
    [string]$PublicOrigin = '',
    [switch]$SkipBuild,
    [switch]$SkipPull,
    [switch]$CoreOnly,
    [switch]$Resume
)

$ErrorActionPreference = 'Stop'

if ($PublicOrigin -and $PublicOrigin -notmatch '^https?://[A-Za-z0-9.-]+(:[0-9]{1,5})?$') {
    throw 'PublicOrigin must be an exact http(s) origin without a path or trailing slash'
}

# 项目根目录（脚本位于 scripts/ 下）
$RepoRoot = Split-Path -Parent $PSScriptRoot
Set-Location $RepoRoot

function Invoke-Docker {
    param([string[]]$Arguments, [string]$Step, [int]$Attempts = 3)
    for ($i = 1; $i -le $Attempts; $i++) {
        & docker @Arguments
        if ($LASTEXITCODE -eq 0) { return }
        if ($i -lt $Attempts) {
            # 镜像源偶发 403/超时，重试通常即可恢复
            Write-Warning "$Step 第 $i 次失败 (exit=$LASTEXITCODE)，8 秒后重试..."
            Start-Sleep -Seconds 8
        } else {
            throw "docker $($Arguments -join ' ') 执行失败 (exit=$LASTEXITCODE)，步骤: $Step"
        }
    }
}

function Get-SafeName {
    param([string]$ImageRef)
    return ($ImageRef -replace '[/:@]', '_')
}

# 以 LF 行尾 + 无 BOM 写出文本。
# Windows PowerShell 5.1 的 `Set-Content -Encoding UTF8` 会写入 BOM 与 CRLF，
# 那会破坏服务器端的 `sha256sum -c` 校验与 bash 脚本的执行。
function Write-LfNoBom {
    param([string]$Path, [string[]]$Lines)
    # 先按 LF 合并，再把内嵌的 CRLF 折叠为 LF，最后删除游离的 CR。
    # 注意：不能把 CR 替换成 LF，否则每个元素尾部会多出一个换行，导致行数翻倍。
    $text = ($Lines -join "`n")
    $text = ($text -replace "`r`n", "`n") -replace "`r", ""
    $text += "`n"
    $utf8NoBom = New-Object System.Text.UTF8Encoding($false)
    [System.IO.File]::WriteAllText($Path, $text, $utf8NoBom)
}

# 业务镜像: 名称 = dockerfile 相对路径 (相对于仓库根, 用 '.' 表示根上下文)
$AppImages = @(
    @{ Name = 'auth-service';         Dockerfile = 'deploy/docker/auth.dockerfile';         Context = '.' }
    @{ Name = 'user-service';         Dockerfile = 'deploy/docker/user.dockerfile';         Context = '.' }
    @{ Name = 'tweet-service';        Dockerfile = 'deploy/docker/tweet.dockerfile';        Context = '.' }
    @{ Name = 'follow-service';       Dockerfile = 'deploy/docker/follow.dockerfile';       Context = '.' }
    @{ Name = 'notification-service'; Dockerfile = 'deploy/docker/notification.dockerfile'; Context = '.' }
    @{ Name = 'messenger-service';    Dockerfile = 'deploy/docker/messenger.dockerfile';    Context = '.' }
    @{ Name = 'canal-relay';          Dockerfile = 'deploy/docker/canal-relay.dockerfile';  Context = '.' }
    @{ Name = 'consumer';             Dockerfile = 'deploy/docker/consumer.dockerfile';     Context = '.' }
    @{ Name = 'agent-service';        Dockerfile = 'deploy/docker/agent.dockerfile';        Context = '.' }
    @{ Name = 'gateway';              Dockerfile = 'deploy/docker/gateway.dockerfile';      Context = '.' }
    @{ Name = 'frontend';             Dockerfile = 'web/Dockerfile';                        Context = 'web' }
)

# 第三方基础镜像（与 docker-compose.yaml 保持一致）
$ThirdPartyImages = @(
    'mysql:8.0'
    'minio/minio:latest'
    'redis:alpine'
    'mongo:7.0'
    'elasticsearch:8.13.0'
    'qdrant/qdrant:latest'
    'rabbitmq:3-management'
    'consul:1.15'
    'jaegertracing/all-in-one:1.37'
    'prom/prometheus:v2.45.0'
    'kibana:8.13.0'
    'grafana/loki:2.9.0'
    'grafana/promtail:2.9.0'
    'grafana/grafana:10.0.0'
    'temporalio/auto-setup:1.24.0'
    'temporalio/ui:2.24.0'
    'bladex/sentinel-dashboard'
    'grafana/pyroscope:latest'
)

if ($CoreOnly) {
    $ThirdPartyImages = @(
        'mysql:8.0'
        'minio/minio:latest'
        'redis:alpine'
        'mongo:7.0'
        'elasticsearch:8.13.0'
        'qdrant/qdrant:latest'
        'rabbitmq:3-management'
        'consul:1.15'
        'temporalio/auto-setup:1.24.0'
    )
}

# 构建业务镜像所需的 Dockerfile 基础镜像（不导出，仅提前拉取以便尽早暴露网络问题）
$BuilderImages = @(
    'golang:1.25-alpine'
    'alpine:latest'
    'node:22-alpine'
    'nginx:alpine'
)

$ReleaseDir = Join-Path $RepoRoot (Join-Path $OutputRoot $Version)
$ImagesDir = Join-Path $ReleaseDir 'images'
if ((Test-Path -LiteralPath $ReleaseDir) -and -not $Resume) {
    throw "发布目录已存在，拒绝覆盖: $ReleaseDir"
}
New-Item -ItemType Directory -Force -Path $ImagesDir | Out-Null

$Manifest = New-Object System.Collections.Generic.List[string]
$Manifest.Add("# twitter-clone 离线镜像发布包")
$Manifest.Add("# version: $Version")
$Manifest.Add("# generated: $(Get-Date -Format o)")
$Manifest.Add("# profile: $(if ($CoreOnly) { 'core-only' } else { 'full' })")
$Manifest.Add("#")
$Manifest.Add("# 列: 镜像引用 <TAB> tar 文件 <TAB> 大小")
$Manifest.Add("# 服务端用: bash load-images.sh")
$Manifest.Add("")

function Export-Image {
    param([string]$ImageRef, [string]$SafeName)

    $tarPath = Join-Path $ImagesDir "$SafeName.tar"
    $existing = Get-Item -LiteralPath $tarPath -ErrorAction SilentlyContinue
    if ($Resume -and $existing -and $existing.Length -gt 0) {
        Write-Host "==> 复用已有归档 $ImageRef -> images/$SafeName.tar"
    } else {
        Write-Host "==> 导出 $ImageRef -> images/$SafeName.tar"
        Invoke-Docker -Arguments @('save', $ImageRef, '-o', $tarPath) -Step "save $ImageRef"
    }

    if (-not (Test-Path -LiteralPath $tarPath)) {
        throw "镜像导出后未找到归档: $tarPath"
    }

    $sizeMb = [math]::Round((Get-Item $tarPath).Length / 1MB, 1)
    if ($sizeMb -le 0) { throw "镜像归档为空: $tarPath" }
    # 字段之间必须只用单个 TAB：连续 TAB 在 bash 的 IFS=$'\t' 下会产生空字段。
    $Script:Manifest.Add("$ImageRef`timages/$SafeName.tar`t${sizeMb}MB")
}

# 拉取镜像，失败时若本地已存在则回退使用本地版本；否则抛出。
function Pull-ImageOrFallback {
    param([string]$ImageRef)
    Write-Host "==> 拉取 $ImageRef"
    try {
        Invoke-Docker -Arguments @('pull', $ImageRef) -Step "pull $ImageRef"
    } catch {
        & docker image inspect $ImageRef *> $null
        if ($LASTEXITCODE -eq 0) {
            Write-Warning "拉取 $ImageRef 失败，但本地已存在该镜像，将使用本地版本。"
        } else {
            throw
        }
    }
}

# ---------------------------------------------------------------------------
# 0. 预拉取构建用基础镜像
# ---------------------------------------------------------------------------
if (-not $SkipPull) {
    foreach ($img in $BuilderImages) {
        Pull-ImageOrFallback -ImageRef $img
    }
}

# ---------------------------------------------------------------------------
# 1. 构建业务镜像
# ---------------------------------------------------------------------------
foreach ($svc in $AppImages) {
    $tag = "twitter-clone/$($svc.Name):$Version"

    if ($SkipBuild) {
        Write-Host "==> 跳过构建 (SkipBuild): $tag"
    } else {
        Write-Host "==> 构建 $tag  (context=$($svc.Context), dockerfile=$($svc.Dockerfile))"
        # --provenance=false --sbom=false: 关闭 BuildKit 默认附加的 attestation，
        # 使镜像成为单 manifest 镜像，最大化服务器端 docker load 的兼容性。
        Invoke-Docker -Arguments @(
            'build',
            '--provenance=false',
            '--sbom=false',
            '-f', $svc.Dockerfile,
            '-t', $tag,
            $svc.Context
        ) -Step "build $($svc.Name)"
    }
}

# ---------------------------------------------------------------------------
# 2. 拉取第三方镜像
#   部分镜像在私有/国内镜像源上无法拉取（例如 minio），此时若本地已存在则回退使用本地镜像。
# ---------------------------------------------------------------------------
foreach ($img in $ThirdPartyImages) {
    if ($SkipPull) {
        Write-Host "==> 跳过拉取 (SkipPull): $img"
        continue
    }

    Pull-ImageOrFallback -ImageRef $img
}

# ---------------------------------------------------------------------------
# 3. 逐个导出
# ---------------------------------------------------------------------------
foreach ($svc in $AppImages) {
    Export-Image -ImageRef "twitter-clone/$($svc.Name):$Version" -SafeName "twitter-clone_$($svc.Name)_$Version"
}

foreach ($img in $ThirdPartyImages) {
    Export-Image -ImageRef $img -SafeName (Get-SafeName $img)
}

# ---------------------------------------------------------------------------
# 4. 清单 / 校验和 / 服务器加载脚本
# ---------------------------------------------------------------------------
$ManifestPath = Join-Path $ReleaseDir 'MANIFEST.txt'
Write-LfNoBom -Path $ManifestPath -Lines $Manifest

Write-Host "==> 生成 SHA256SUMS.txt"
$sums = Get-ChildItem -Path $ImagesDir -Filter *.tar | ForEach-Object {
    $hash = (Get-FileHash -Path $_.FullName -Algorithm SHA256).Hash.ToLower()
    "$hash  images/$($_.Name)"
}
Write-LfNoBom -Path (Join-Path $ReleaseDir 'SHA256SUMS.txt') -Lines $sums

$loadScript = @'
#!/usr/bin/env bash
# 服务器端一键导入镜像: bash load-images.sh
# 依赖: docker, bash；sha256sum 可选（用于校验完整性）
set -euo pipefail
cd "$(dirname "$0")"

if command -v sha256sum >/dev/null 2>&1 && [ -f SHA256SUMS.txt ]; then
  echo "==> 校验 tar 文件完整性"
  sha256sum -c SHA256SUMS.txt
fi

loaded=0
while IFS=$'\t' read -r image tarfile size; do
  case "$image" in ''|'#'*) continue ;; esac
  [ -z "${tarfile:-}" ] && continue
  echo "==> docker load -i $tarfile   ($image)"
  docker load -i "$tarfile"
  loaded=$((loaded + 1))
done < MANIFEST.txt

echo "==> 完成，共导入 $loaded 个镜像归档"
'@
Write-LfNoBom -Path (Join-Path $ReleaseDir 'load-images.sh') -Lines ($loadScript -split "`n")

# ---------------------------------------------------------------------------
# 5. 服务器端部署 compose
#    docker-compose.yaml 里 11 个业务服务只写了 build:、没有 image:，直接用
#    `docker compose up` 会在服务器上重新构建而不会使用已加载的镜像，
#    因此单独生成一份把 build: 换成 image: 的部署文件（见 generate-deploy-compose.ps1）。
# ---------------------------------------------------------------------------
& (Join-Path $PSScriptRoot 'generate-deploy-compose.ps1') `
    -Version $Version `
    -BaseCompose (Join-Path $RepoRoot 'docker-compose.yaml') `
    -OutputPath (Join-Path $ReleaseDir 'docker-compose.deploy.yaml')

if (-not $CoreOnly) {
    # Full Compose bind-mounts these files; image tar archives alone cannot
    # start the monitoring services on a clean host.
    foreach ($asset in @(
        'deploy/prometheus/prometheus.yml',
        'deploy/promtail/promtail-config.yaml'
    )) {
        $destination = Join-Path $ReleaseDir $asset
        New-Item -ItemType Directory -Force -Path (Split-Path -Parent $destination) | Out-Null
        Copy-Item -LiteralPath (Join-Path $RepoRoot $asset) -Destination $destination
    }
    $grafanaSource = Join-Path $RepoRoot 'deploy/grafana/provisioning'
    $grafanaDestination = Join-Path $ReleaseDir 'deploy/grafana/provisioning'
    New-Item -ItemType Directory -Force -Path (Split-Path -Parent $grafanaDestination) | Out-Null
    Copy-Item -LiteralPath $grafanaSource -Destination $grafanaDestination -Recurse

    Write-LfNoBom -Path (Join-Path $ReleaseDir 'README-FULL.md') -Lines @(
        '# Full deployment package',
        '',
        'This package contains all 11 application images, all 18 third-party images,',
        'and the non-secret monitoring bind-mount files required by Compose.',
        'Credentials are deliberately excluded. Upload the matching .env separately',
        'and keep a backup of the server .env and previous image tag for rollback.',
        '',
        'Verify archives and load images: bash load-images.sh',
        'Validate configuration: docker compose -f docker-compose.deploy.yaml config --quiet',
        'Start all services: docker compose -f docker-compose.deploy.yaml up -d',
        'Check status: docker compose -f docker-compose.deploy.yaml ps',
        '',
        'The full-feature .env enables Workflow-as-Tool, Skill Catalog, recoverable',
        'runs, Planning-Execute, approval recovery, external MCP and other implemented',
        'Agent paths. A real MCP connection, Web Search provider credential, and',
        'Marketplace administration still require separate user-owned configuration.',
        'Write actions remain subject to authorization, approval and tool policy.',
        'Direct Profile publishing, managed MCP credentials, Profile experiments,',
        'prompt sampling and destructive retention are not enabled by this profile.',
        'Planning-Execute at 100 percent can increase model cost; to roll back, restore',
        'the previous .env and image tag, then recreate agent-service and gateway.',
        'The generated Compose publishes infrastructure ports; restrict them with',
        'host firewall rules before exposing this deployment to the Internet.'
    )
}

if ($CoreOnly) {
    $demoNotes = @(
        '# 面试演示核心镜像包',
        '',
        '本包只包含业务服务及其基础依赖镜像，不包含监控/运维界面镜像。',
        '先执行 bash load-images.sh 导入镜像；不要直接对 docker-compose.deploy.yaml 执行无参数 up。',
        '目前的 Compose 仍含全部服务定义，并且原始端口映射不适合直接暴露到公网。',
        '在云服务器部署前，必须单独安全上传完整 .env；本包不包含任何密钥。',
        '限制数据库/中间件端口对公网开放，并单独处理 HTTPS。',
        '必填: GATEWAY_ALLOWED_ORIGINS 必须等于浏览器访问前端的完整 Origin（协议+主机+端口），',
        '例如 http://<公网IP>:5173；不要添加路径或结尾斜杠，也不要使用通配符。',
        '媒体: MINIO_PUBLIC_URL=/media（默认），前端 nginx 同源代理到内部 MinIO；无需开放 9000。',
        'TWEET_INTERNAL_SERVICE_TOKEN 必填，Agent、Gateway、Tweet 必须读取同一个值。',
        '无本地 LM Studio 时，显式设置 LM_STUDIO_API_URL、LM_STUDIO_API_TOKEN、',
        'LM_STUDIO_MODEL_CHAT、LM_STUDIO_MODEL_EMBEDDING 和 AGENT_SESSION_SUMMARY_MODEL。',
        '云端 Chat/Embedding 会产生模型费用；不能仅把本地 URL 改为云端 URL 而不配置 Token/模型。',
        '启动前检查: docker compose -f docker-compose.deploy.yaml config --quiet',
        '',
        '仅启动核心服务示例（仍需先完成上述公网安全配置）：',
        'docker compose -f docker-compose.deploy.yaml up -d mysql mongodb redis rabbitmq minio elasticsearch qdrant consul temporal auth-service user-service tweet-service follow-service messenger-service notification-service canal-relay consumer agent-service gateway frontend'
    )
    if ($PublicOrigin) {
        $demoNotes += @(
            '',
            "本次前端访问地址: $PublicOrigin",
            "完整 .env 应包含 GATEWAY_ALLOWED_ORIGINS=$PublicOrigin；不要用地址示例覆盖密钥。"
        )
    }
    Write-LfNoBom -Path (Join-Path $ReleaseDir 'README-DEMO.md') -Lines $demoNotes
}

if ($PublicOrigin) {
    Write-LfNoBom -Path (Join-Path $ReleaseDir 'DEPLOY-ADDRESS.env.example') -Lines @(
        '# Add these lines to the existing server .env; do not replace secrets.',
        "GATEWAY_ALLOWED_ORIGINS=$PublicOrigin",
        'MINIO_PUBLIC_URL=/media'
    )
}

Write-Host ""
Write-Host "完成。产物目录: $ReleaseDir"
Write-Host "  images/        共 $((Get-ChildItem $ImagesDir -Filter *.tar).Count) 个 tar"
Write-Host "  MANIFEST.txt"
Write-Host "  SHA256SUMS.txt"
Write-Host "  load-images.sh"
Write-Host "  docker-compose.deploy.yaml"
