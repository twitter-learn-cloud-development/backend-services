# ==============================================================================
# generate-deploy-compose.ps1
# 生成服务器部署用的「自包含」compose 文件。
#
# 背景: 原 docker-compose.yaml 里 11 个业务服务只写了 build:、没有 image:，
# `docker compose up` 会在服务器上重新构建，而不是使用已经 docker load 进去的镜像。
# 本脚本把这 11 个服务的 `build:` 块替换为 `image: twitter-clone/<服务>:<版本>`，
# 其余内容（18 个第三方服务、网络、卷、环境变量插值）原样保留。
#
# 用法:
#   powershell -ExecutionPolicy Bypass -File ./scripts/generate-deploy-compose.ps1
#   powershell ... -Version 20260926-2 -OutputPath docker-compose.deploy.yaml
# ==============================================================================
[CmdletBinding()]
param(
    [string]$Version = (Get-Date -Format 'yyyyMMdd') + '-1',
    [string]$BaseCompose = 'docker-compose.yaml',
    [string]$OutputPath = 'docker-compose.deploy.yaml'
)

$ErrorActionPreference = 'Stop'

$RepoRoot = Split-Path -Parent $PSScriptRoot
Set-Location $RepoRoot

# 需要把 build: 换成 image: 的业务服务（与 export-images.ps1 的 $AppImages 保持一致）
$AppServices = @(
    'auth-service'
    'user-service'
    'tweet-service'
    'follow-service'
    'notification-service'
    'messenger-service'
    'canal-relay'
    'consumer'
    'agent-service'
    'gateway'
    'frontend'
)

function Write-LfNoBom {
    param([string]$Path, [string[]]$Lines)
    # 先按 LF 合并，再把内嵌 CRLF 折叠为 LF，最后删除游离 CR（不能把 CR 替换成 LF，否则行数翻倍）
    $text = ($Lines -join "`n")
    $text = ($text -replace "`r`n", "`n") -replace "`r", ""
    $text += "`n"
    [System.IO.File]::WriteAllText($Path, $text, (New-Object System.Text.UTF8Encoding($false)))
}

if (-not (Test-Path $BaseCompose)) { throw "找不到基础 compose 文件: $BaseCompose" }

$raw = [System.IO.File]::ReadAllText((Resolve-Path $BaseCompose).Path)
$lines = $raw -split "`r`n|`n"

$out = New-Object System.Collections.Generic.List[string]
$out.Add('# ==============================================================================')
$out.Add('# 服务器部署用 compose —— 由 scripts/generate-deploy-compose.ps1 自动生成，请勿手工修改')
$out.Add('# 版本: ' + $Version)
$out.Add('#')
$out.Add('# 与 docker-compose.yaml 内容一致，唯一区别是 11 个业务服务由 build: 改为 image:，')
$out.Add('# 直接使用已 docker load 的镜像。服务器因此不需要源码、Go/Node 工具链和外网。')
$out.Add('#')
$out.Add('# 用法:')
$out.Add('#   docker compose -f docker-compose.deploy.yaml up -d')
$out.Add('#   （上传到服务器后也可直接重命名为 docker-compose.yaml，再 docker compose up -d）')
$out.Add('#')
$out.Add('# 前提:')
$out.Add('#   1. 同目录已执行 bash load-images.sh 导入镜像，并准备好 .env（compose 会自动读取）')
$out.Add('#   2. 下面的镜像 tag 默认是 ' + $Version + '，升级版本时用 APP_IMAGE_TAG 覆盖:')
$out.Add('#      APP_IMAGE_TAG=' + $Version + '-2 docker compose -f docker-compose.deploy.yaml up -d')
$out.Add('# ==============================================================================')
$out.Add('')

$replaced = 0
$i = 0
while ($i -lt $lines.Count) {
    $line = $lines[$i]
    if ($line -eq '      - GATEWAY_ALLOWED_ORIGINS=${GATEWAY_ALLOWED_ORIGINS:-}') {
        $out.Add('      - GATEWAY_ALLOWED_ORIGINS=${GATEWAY_ALLOWED_ORIGINS:?GATEWAY_ALLOWED_ORIGINS is required}')
    } elseif ($line -eq '      - TWEET_INTERNAL_SERVICE_TOKEN=${TWEET_INTERNAL_SERVICE_TOKEN:-}') {
        $out.Add('      - TWEET_INTERNAL_SERVICE_TOKEN=${TWEET_INTERNAL_SERVICE_TOKEN:?TWEET_INTERNAL_SERVICE_TOKEN is required}')
    } else {
        $out.Add($line)
    }
    $i++

    $svc = $null
    foreach ($name in $AppServices) {
        if ($line -eq "  ${name}:") { $svc = $name; break }
    }
    if (-not $svc) { continue }

    # 业务服务下面紧跟的应是一个 build: 块（build: 行 + 若干 6 空格缩进的子键）
    if ($i -lt $lines.Count -and $lines[$i] -eq '    build:') {
        $i++
        while ($i -lt $lines.Count -and $lines[$i] -match '^      \S') { $i++ }
        $out.Add('    image: twitter-clone/' + $svc + ':${APP_IMAGE_TAG:-' + $Version + '}')
        $replaced++
    }
}

# 自校验: 替换数量不符说明基础 compose 结构变了，宁可报错也不要生成错误的部署文件
if ($replaced -ne $AppServices.Count) {
    throw "预期替换 $($AppServices.Count) 个服务，实际只替换 $replaced 个。$BaseCompose 的结构可能已变化，请检查。"
}

Write-LfNoBom -Path $OutputPath -Lines $out

Write-Host "已生成 $OutputPath"
Write-Host "  业务服务 build -> image 替换: $replaced 个"
Write-Host "  镜像 tag: twitter-clone/<服务>:`${APP_IMAGE_TAG:-$Version}"
