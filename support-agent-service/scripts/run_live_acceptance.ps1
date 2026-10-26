[CmdletBinding()]
param(
    [string]$TwitterRepo,
    [string]$SupportRepo,
    [switch]$StopSupportAgentAfter
)

$ErrorActionPreference = "Stop"

function Read-DotEnv {
    param([string]$Path)
    $result = @{}
    foreach ($line in Get-Content -LiteralPath $Path) {
        $trimmed = $line.Trim()
        if ($trimmed -eq "" -or $trimmed.StartsWith("#") -or -not $trimmed.Contains("=")) {
            continue
        }
        $parts = $trimmed.Split("=", 2)
        $result[$parts[0].Trim()] = $parts[1].Trim()
    }
    return $result
}

function New-EphemeralSecret {
    param([int]$ByteCount = 48)
    $bytes = New-Object byte[] $ByteCount
    $generator = [Security.Cryptography.RandomNumberGenerator]::Create()
    try {
        $generator.GetBytes($bytes)
    }
    finally {
        $generator.Dispose()
    }
    return [Convert]::ToBase64String($bytes).TrimEnd("=").Replace("+", "-").Replace("/", "_")
}

function Wait-HttpReady {
    param(
        [string]$Url,
        [string]$Name,
        [int]$Attempts = 120
    )
    for ($attempt = 1; $attempt -le $Attempts; $attempt++) {
        try {
            $response = Invoke-WebRequest -UseBasicParsing -Uri $Url -TimeoutSec 3
            if ($response.StatusCode -eq 200) {
                Write-Host "$Name ready (HTTP $($response.StatusCode))."
                return
            }
        }
        catch {
            if ($attempt -eq $Attempts) {
                throw "$Name did not become ready at $Url."
            }
        }
        Start-Sleep -Seconds 2
    }
    throw "$Name did not become ready at $Url."
}

$dockerInfo = docker info --format '{{.ServerVersion}}' 2>$null
if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($dockerInfo)) {
    throw "Docker Desktop Linux Engine is not running. Start Docker Desktop, wait for Engine running, and rerun."
}

if ([string]::IsNullOrWhiteSpace($SupportRepo)) {
    $SupportRepo = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot "..")).Path
}
if ([string]::IsNullOrWhiteSpace($TwitterRepo)) {
    $TwitterRepo = (Resolve-Path -LiteralPath (Join-Path $SupportRepo "..")).Path
}

$twitterEnvPath = Join-Path $TwitterRepo ".env"
$twitterComposePath = Join-Path $TwitterRepo "docker-compose.yaml"
$liveComposePath = Join-Path $SupportRepo "compose.live.yaml"
foreach ($path in @($twitterEnvPath, $twitterComposePath, $liveComposePath)) {
    if (-not (Test-Path -LiteralPath $path)) {
        throw "Required Live acceptance file is missing: $path"
    }
}

$dotenv = Read-DotEnv -Path $twitterEnvPath
$dashscopeKey = [string]$dotenv["DASHSCOPE_API_KEY"]
if ([string]::IsNullOrWhiteSpace($dashscopeKey)) {
    throw "DASHSCOPE_API_KEY is not configured in the Go repository's Git-ignored .env."
}
$mongoPassword = [string]$dotenv["MONGO_PASSWORD"]
if ([string]::IsNullOrWhiteSpace($mongoPassword)) {
    throw "MONGO_PASSWORD is not configured."
}

$runId = (Get-Date -Format "yyyyMMddHHmmss") + "-" + ([Guid]::NewGuid().ToString("N").Substring(0, 8))
$env:LIVE_TEST_RUN_ID = $runId
$env:LIVE_USER_JWT_ISSUER = "twitter-clone-auth"
$env:LIVE_USER_JWT_AUDIENCE = "twitter-clone-users"
$env:LIVE_CHAT_MODEL = if ([string]::IsNullOrWhiteSpace($env:LIVE_CHAT_MODEL)) { "qwen-plus" } else { $env:LIVE_CHAT_MODEL }
$env:LIVE_EMBEDDING_MODEL = "text-embedding-v4"
$env:SUPPORT_QUERY_JWT_SECRET = New-EphemeralSecret
$env:AUDIT_HMAC_KEY = New-EphemeralSecret
$env:DASHSCOPE_API_KEY = $dashscopeKey
$env:DASHSCOPE_API_URL = if ([string]::IsNullOrWhiteSpace([string]$dotenv["DASHSCOPE_API_URL"])) {
    "https://dashscope.aliyuncs.com/compatible-mode/v1"
} else {
    [string]$dotenv["DASHSCOPE_API_URL"]
}
$encodedMongoPassword = [Uri]::EscapeDataString($mongoPassword)
$env:SUPPORT_AGENT_MONGODB_URI = "mongodb://root:$encodedMongoPassword@mongodb:27017/support_agent_live?authSource=admin"
$env:SUPPORT_AGENT_MODEL_BASE_URL = $env:DASHSCOPE_API_URL
$env:SUPPORT_AGENT_MODEL_API_KEY = $dashscopeKey
$env:SUPPORT_AGENT_EMBEDDING_MODEL_NAME = $env:LIVE_EMBEDDING_MODEL
$env:SUPPORT_AGENT_EMBEDDING_DIMENSIONS = "1536"
$env:SUPPORT_AGENT_QDRANT_URL = "http://127.0.0.1:6333"

$composeArgs = @(
    "compose",
    "-p", "twitter-clone",
    "--env-file", $twitterEnvPath,
    "-f", $twitterComposePath,
    "-f", $liveComposePath
)

Write-Host "Starting joint Live stack. Provider/Mongo credentials come from the Go repository's Git-ignored .env; generated service/audit secrets remain process/container environment values."
docker @composeArgs up -d --build support-agent
if ($LASTEXITCODE -ne 0) {
    throw "Docker Compose failed to start the joint Live stack."
}

Wait-HttpReady -Url "http://127.0.0.1:8081/.well-known/jwks.json" -Name "Auth JWKS"
Wait-HttpReady -Url "http://127.0.0.1:9638/health" -Name "Gateway"
Wait-HttpReady -Url "http://127.0.0.1:6333/readyz" -Name "Qdrant"
Wait-HttpReady -Url "http://127.0.0.1:8000/health/ready" -Name "Support Agent"

Push-Location $SupportRepo
try {
    $qdrantOutput = uv run python -m scripts.live_qdrant_probe
    if ($LASTEXITCODE -ne 0) {
        throw "Qdrant/embedding Live probe failed."
    }
    $qdrantJson = $qdrantOutput | Where-Object { $_ -match '^\{' } | Select-Object -Last 1
    $qdrantResult = $qdrantJson | ConvertFrom-Json

    $acceptanceOutput = uv run python -m scripts.live_acceptance
    if ($LASTEXITCODE -ne 0) {
        throw "Joint API Live acceptance failed."
    }
    $acceptanceJson = $acceptanceOutput | Where-Object { $_ -match '^\{' } | Select-Object -Last 1
    $acceptanceResult = $acceptanceJson | ConvertFrom-Json
}
finally {
    Pop-Location
}

$supportLogs = docker @composeArgs logs --no-color --since 15m support-agent
$auditCount = @($supportLogs | Select-String -Pattern 'capability_call_completed|support_run_completed').Count
$traceExported = $false
try {
    $services = Invoke-RestMethod -Uri "http://127.0.0.1:16686/api/services" -TimeoutSec 5
    $traceExported = @($services.data) -contains "Support Agent Service"
}
catch {
    $traceExported = $false
}

$report = [ordered]@{
    status = if ($traceExported -and $auditCount -gt 0) { "passed" } else { "partial" }
    run_id = $runId
    completed_at = (Get-Date).ToUniversalTime().ToString("o")
    external_dependencies = [ordered]@{
        docker_engine = "passed"
        mongodb = "passed"
        qdrant = $qdrantResult
        dashscope_chat_and_embeddings = "passed"
    }
    api_acceptance = $acceptanceResult
    observability = [ordered]@{
        audit_events_observed = $auditCount
        jaeger_service_observed = $traceExported
    }
    persisted_test_data = [ordered]@{
        isolated_users = 2
        sample_tweets = 1
        credentials_persisted = $false
        qdrant_probe_document_removed = $true
    }
    secrets = [ordered]@{
        repository_files_written = $false
        values_printed = $false
        lifecycle = "provider/Mongo credentials from Git-ignored Go .env; generated service/audit secrets in process/container environment"
    }
}

$reportDirectory = Join-Path $SupportRepo "dist\live-acceptance"
New-Item -ItemType Directory -Path $reportDirectory -Force | Out-Null
$reportPath = Join-Path $reportDirectory "live-acceptance-$runId.json"
$report | ConvertTo-Json -Depth 10 | Set-Content -LiteralPath $reportPath -Encoding UTF8
Write-Host "Live acceptance report: $reportPath"
Write-Output ($report | ConvertTo-Json -Depth 10 -Compress)

if ($StopSupportAgentAfter) {
    docker @composeArgs stop support-agent
}

if ($report.status -ne "passed") {
    exit 2
}
