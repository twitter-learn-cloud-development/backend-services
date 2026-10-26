[CmdletBinding()]
param(
    [ValidateSet("start", "status")]
    [string]$Action = "start",
    [switch]$RestoreStack
)

$ErrorActionPreference = "Stop"
$demoRoot = Join-Path (Split-Path $PSScriptRoot -Parent) "frontend"
$baseContainers = @("twitter-clone-mysql-1", "twitter-clone-redis-1", "twitter-clone-rabbitmq-1", "twitter-clone-consul-1", "mongodb", "qdrant", "minio-server", "twitter-clone-jaeger-1")
$applicationContainers = @("twitter-clone-user-service-1", "twitter-clone-tweet-service-1", "twitter-clone-follow-service-1", "twitter-clone-auth-service-1", "twitter-clone-gateway-1", "twitter-clone-frontend-1", "twitter-clone-support-agent-1")

function Start-ExistingContainer([string]$Name) {
    $state = docker container inspect $Name --format '{{.State.Status}}' 2>$null
    if ($LASTEXITCODE -ne 0) { throw "Required existing container is missing: $Name. Complete the local joint-stack setup first." }
    if ($state -ne "running") {
        docker start $Name
        if ($LASTEXITCODE -ne 0) { throw "Failed to start $Name" }
    }
}

function Wait-ContainerReady([string]$Name) {
    $deadline = [DateTime]::UtcNow.AddSeconds(45)
    do {
        $state = docker container inspect $Name --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' 2>$null
        if ($LASTEXITCODE -eq 0 -and $state -in @("healthy", "running")) { return }
        Start-Sleep -Milliseconds 500
    } while ([DateTime]::UtcNow -lt $deadline)
    throw "$Name did not become ready within 45 seconds. Inspect its logs before continuing."
}

function Get-EndpointState([string]$Url) {
    try {
        $response = Invoke-WebRequest -UseBasicParsing -Uri $Url -TimeoutSec 3
        if ($response.StatusCode -eq 200) { return "available" }
        return "HTTP $($response.StatusCode)"
    }
    catch { return "unavailable" }
}

if ($RestoreStack) {
    if ($Action -ne "start") { throw "-RestoreStack can only be used with start." }
    docker info --format '{{.ServerVersion}}' *> $null
    if ($LASTEXITCODE -ne 0) { throw "Docker Desktop Linux Engine is not running." }
    foreach ($name in $baseContainers) { Start-ExistingContainer $name }
    foreach ($name in $baseContainers) { Wait-ContainerReady $name }
    foreach ($name in $applicationContainers) { Start-ExistingContainer $name }
    Wait-ContainerReady "twitter-clone-support-agent-1"
}

Write-Output "Demo:    $(Get-EndpointState 'http://127.0.0.1:5174/')"
Write-Output "Support: $(Get-EndpointState 'http://127.0.0.1:8000/health/ready')"
Write-Output "Gateway: $(Get-EndpointState 'http://127.0.0.1:9638/health')"
if ($Action -eq "status") { return }
if ((Get-EndpointState 'http://127.0.0.1:5174/') -eq "available") {
    Write-Output "Demo is already running: http://127.0.0.1:5174/"
    return
}

Push-Location $demoRoot
try {
    if (-not (Test-Path -LiteralPath (Join-Path $demoRoot "node_modules"))) {
        npm ci --no-audit --no-fund
        if ($LASTEXITCODE -ne 0) { throw "Frontend dependency install failed." }
    }
    npm run dev
    if ($LASTEXITCODE -ne 0) { throw "Frontend process exited with an error." }
}
finally { Pop-Location }
