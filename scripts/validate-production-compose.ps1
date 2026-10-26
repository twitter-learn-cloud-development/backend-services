[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$EnvFile,

    [switch]$AllowRepositoryEnvFile
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$repositoryRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot ".."))
$baseCompose = Join-Path $repositoryRoot "docker-compose.yaml"
$productionCompose = Join-Path $repositoryRoot "docker-compose.production.yaml"
$resolvedEnvFile = [IO.Path]::GetFullPath($EnvFile)

if (-not (Test-Path -LiteralPath $resolvedEnvFile -PathType Leaf)) {
    throw "production environment file does not exist: $resolvedEnvFile"
}

$relativeEnvPath = [IO.Path]::GetRelativePath($repositoryRoot, $resolvedEnvFile)
if (-not $AllowRepositoryEnvFile -and -not $relativeEnvPath.StartsWith("..")) {
    throw "production environment file must live outside the repository"
}

function Read-EnvFile {
    param([string]$Path)

    $result = @{}
    foreach ($rawLine in Get-Content -LiteralPath $Path) {
        $line = $rawLine.Trim()
        if ($line.Length -eq 0 -or $line.StartsWith("#")) {
            continue
        }
        $separator = $line.IndexOf("=")
        if ($separator -le 0) {
            throw "invalid environment entry; expected KEY=VALUE"
        }
        $key = $line.Substring(0, $separator).Trim()
        $value = $line.Substring($separator + 1).Trim()
        if ($result.ContainsKey($key)) {
            throw "duplicate environment key: $key"
        }
        $result[$key] = $value
    }
    return $result
}

function Require-Value {
    param([hashtable]$Values, [string]$Name)

    if (-not $Values.ContainsKey($Name) -or [string]::IsNullOrWhiteSpace($Values[$Name])) {
        throw "required environment key is missing: $Name"
    }
    $value = [string]$Values[$Name]
    if ($value -match "(?i)REPLACE_WITH|CHANGE_ME|CHANGEME|example\.com") {
        throw "environment key still contains a placeholder: $Name"
    }
    return $value
}

function Read-Bool {
    param([hashtable]$Values, [string]$Name, [bool]$Default = $false)

    if (-not $Values.ContainsKey($Name) -or [string]::IsNullOrWhiteSpace($Values[$Name])) {
        return $Default
    }
    $parsed = $false
    if (-not [bool]::TryParse(([string]$Values[$Name]), [ref]$parsed)) {
        throw "$Name must be true or false"
    }
    return $parsed
}

function Assert-MinimumLength {
    param([hashtable]$Values, [string]$Name, [int]$Minimum)

    $value = Require-Value $Values $Name
    if ($value.Length -lt $Minimum) {
        throw "$Name must contain at least $Minimum characters"
    }
    $knownWeak = @("guest", "admin", "password", "31415927", "twitter_clone_secret_key_2023")
    if ($knownWeak -contains $value.ToLowerInvariant()) {
        throw "$Name uses a known development credential"
    }
}

function Assert-Base64Key32 {
    param([hashtable]$Values, [string]$Name)

    $value = Require-Value $Values $Name
    try {
        $decoded = [Convert]::FromBase64String($value)
    } catch {
        throw "$Name must be valid base64"
    }
    if ($decoded.Length -ne 32) {
        throw "$Name must decode to exactly 32 bytes"
    }
}

function Assert-PublicHttpsUrl {
    param([hashtable]$Values, [string]$Name)

    $value = Require-Value $Values $Name
    $uri = $null
    if (-not [Uri]::TryCreate($value, [UriKind]::Absolute, [ref]$uri) -or $uri.Scheme -ne "https") {
        throw "$Name must be an absolute HTTPS URL"
    }
    if ($uri.Host -in @("localhost", "127.0.0.1", "host.docker.internal")) {
        throw "$Name cannot target a desktop or loopback host"
    }
}

function Assert-SecretFile {
    param(
        [hashtable]$Values,
        [string]$Name,
        [ValidateSet("certificate", "private_key", "token")]
        [string]$Kind
    )

    $path = [IO.Path]::GetFullPath((Require-Value $Values $Name))
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
        throw "$Name does not reference a readable file"
    }
    $relativePath = [IO.Path]::GetRelativePath($repositoryRoot, $path)
    if (-not $AllowRepositoryEnvFile -and -not $relativePath.StartsWith("..")) {
        throw "$Name must reference a file outside the repository"
    }
    $content = [IO.File]::ReadAllText($path).Trim()
    switch ($Kind) {
        "certificate" {
            $certificateMatch = [regex]::Match(
                $content,
                "-----BEGIN CERTIFICATE-----(?<body>[\s\S]+?)-----END CERTIFICATE-----"
            )
            if (-not $certificateMatch.Success) {
                throw "$Name is not a PEM certificate"
            }
            try {
                $certificateBytes = [Convert]::FromBase64String(
                    ($certificateMatch.Groups["body"].Value -replace "\s", "")
                )
                $certificate = [Security.Cryptography.X509Certificates.X509Certificate2]::new($certificateBytes)
            } catch {
                throw "$Name is not a valid X.509 certificate"
            }
            if ($certificate.NotAfter.ToUniversalTime() -le [DateTime]::UtcNow.AddDays(14)) {
                throw "$Name expires within 14 days"
            }
        }
        "private_key" {
            if ($content -notmatch "-----BEGIN (RSA |EC )?PRIVATE KEY-----") {
                throw "$Name is not an unencrypted PEM private key"
            }
        }
        "token" {
            if ($content.Length -lt 32) {
                throw "$Name must contain at least 32 characters"
            }
        }
    }
    return $path
}

function Get-OptionalProperty {
    param([object]$Object, [string]$Name)

    if ($null -eq $Object) {
        return $null
    }
    $property = $Object.PSObject.Properties[$Name]
    if ($null -eq $property) {
        return $null
    }
    return $property.Value
}

$values = Read-EnvFile $resolvedEnvFile

$agentProcessRole = 'all'
if ($values.ContainsKey('AGENT_PROCESS_ROLE') -and -not [string]::IsNullOrWhiteSpace($values['AGENT_PROCESS_ROLE'])) {
    $agentProcessRole = ([string]$values['AGENT_PROCESS_ROLE']).Trim().ToLowerInvariant()
}
if ($agentProcessRole -notin @('api', 'worker', 'all')) {
    throw 'AGENT_PROCESS_ROLE must be api, worker or all'
}
# Keep the rendered role identical to the role whose credentials are checked.
if ($values.ContainsKey('AGENT_PROCESS_ROLE') -and $values['AGENT_PROCESS_ROLE'] -cne $agentProcessRole) {
    throw 'production AGENT_PROCESS_ROLE must use its canonical lowercase value'
}
$sharedAdmissionEnabled = Read-Bool $values 'AGENT_SHARED_ADMISSION_ENABLED'
if ($sharedAdmissionEnabled -and $agentProcessRole -ne 'worker') {
    $runtimeModes = @()
    if ($values.ContainsKey('AGENT_RUNTIME_V2_MODES')) {
        $runtimeModes = @(([string]$values['AGENT_RUNTIME_V2_MODES']).Split(',') | ForEach-Object { $_.Trim().ToLowerInvariant() } | Sort-Object -Unique)
    }
    $allModes = @('assist', 'chat', 'consult', 'multi', 'workflow')
    if (-not ($runtimeModes.Count -eq 1 -and $runtimeModes[0] -eq 'all') -and
        ($runtimeModes.Count -ne 5 -or @(Compare-Object $allModes $runtimeModes).Count -ne 0)) {
        throw 'shared API admission requires Runtime v2 for all modes'
    }
}
$runtimeDatabaseAccounts = @{}
foreach ($rolePrefix in @('USER_SERVICE','TWEET_SERVICE','FOLLOW_SERVICE','MESSENGER_SERVICE','NOTIFICATION_SERVICE','CONSUMER','CANAL_RELAY','AGENT_SERVICE')) {
    if ($rolePrefix -eq 'AGENT_SERVICE' -and $agentProcessRole -eq 'worker') {
        continue
    }
    $roleAccount = Require-Value $values ($rolePrefix + '_DB_USER')
    if ($roleAccount -notmatch '^[a-zA-Z][a-zA-Z0-9_]{0,63}$' -or $roleAccount -ieq 'root' -or $runtimeDatabaseAccounts.ContainsKey($roleAccount)) {
        throw "runtime database account must be non-root and distinct: $rolePrefix"
    }
    if ($values.ContainsKey('DB_MIGRATOR_USER') -and $roleAccount -ieq $values['DB_MIGRATOR_USER']) {
        throw "runtime database account cannot be the migrator: $rolePrefix"
    }
    $runtimeDatabaseAccounts[$roleAccount] = $true
    Assert-MinimumLength $values ($rolePrefix + '_DB_PASSWORD') 20
}

$imageKeys = @(
    "EDGE_PROXY_IMAGE",
    "MYSQL_IMAGE", "MONGODB_IMAGE", "REDIS_IMAGE", "RABBITMQ_IMAGE",
    "MINIO_IMAGE", "ELASTICSEARCH_IMAGE", "QDRANT_IMAGE", "CONSUL_IMAGE",
    "TEMPORAL_IMAGE", "TEMPORAL_UI_IMAGE", "JAEGER_IMAGE", "PROMETHEUS_IMAGE", "ALERTMANAGER_IMAGE",
    "GRAFANA_IMAGE", "LOKI_IMAGE", "PROMTAIL_IMAGE", "KIBANA_IMAGE",
    "PYROSCOPE_IMAGE", "SENTINEL_IMAGE", "AUTH_SERVICE_IMAGE", "USER_SERVICE_IMAGE",
    "TWEET_SERVICE_IMAGE", "FOLLOW_SERVICE_IMAGE", "MESSENGER_SERVICE_IMAGE",
    "NOTIFICATION_SERVICE_IMAGE", "CANAL_RELAY_IMAGE", "CONSUMER_IMAGE",
    "AGENT_SERVICE_IMAGE", "GATEWAY_IMAGE", "FRONTEND_IMAGE"
)
foreach ($imageKey in $imageKeys) {
    $image = Require-Value $values $imageKey
    if ($image -notmatch "^[^@\s]+@sha256:[0-9a-fA-F]{64}$") {
        throw "$imageKey must use repository@sha256:<64 hex>"
    }
    $digest = $image.Substring($image.LastIndexOf(":") + 1)
    if ($digest -match "^0{64}$") {
        throw "$imageKey still contains the template digest"
    }
}

foreach ($name in @(
    "DB_ROOT_PASSWORD", "DB_PASSWORD", "MONGO_PASSWORD", "REDIS_PASSWORD",
    "MQ_PASSWORD", "MINIO_PASSWORD", "GRAFANA_ADMIN_PASSWORD"
)) {
    Assert-MinimumLength $values $name 20
}
foreach ($name in @("JWT_SECRET", "TWEET_INTERNAL_SERVICE_TOKEN")) {
    Assert-MinimumLength $values $name 32
}
[void](Require-Value $values "JWT_AUDIENCE")
Assert-PublicHttpsUrl $values "JWT_ISSUER"
Assert-Base64Key32 $values "AGENT_PROVIDER_CONFIG_MASTER_KEY"
Assert-Base64Key32 $values "AGENT_RUN_CHECKPOINT_MASTER_KEY"
Assert-PublicHttpsUrl $values "AGENT_LLM_BASE_URL"
Assert-PublicHttpsUrl $values "MINIO_PUBLIC_URL"
Assert-PublicHttpsUrl $values "PUBLIC_BASE_URL"
[void](Require-Value $values "DASHSCOPE_API_KEY")
[void](Require-Value $values "AGENT_CHAT_MODEL")
[void](Require-Value $values "AGENT_EMBEDDING_MODEL")
$publicBaseURL = [Uri](Require-Value $values "PUBLIC_BASE_URL")
$publicHost = Require-Value $values "PUBLIC_HOST"
if ($publicBaseURL.Host -ne $publicHost -or $publicBaseURL.AbsolutePath -ne "/" -or $publicBaseURL.Query -ne "") {
    throw "PUBLIC_BASE_URL must be the HTTPS origin for PUBLIC_HOST"
}
$allowedOrigins = @((Require-Value $values "GATEWAY_ALLOWED_ORIGINS").Split(",") | ForEach-Object { $_.Trim() })
if ($allowedOrigins -contains "*" -or $allowedOrigins -notcontains $publicBaseURL.GetLeftPart([UriPartial]::Authority)) {
    throw "GATEWAY_ALLOWED_ORIGINS must contain PUBLIC_BASE_URL and must not use a wildcard"
}
foreach ($origin in $allowedOrigins) {
    $originURI = $null
    if (-not [Uri]::TryCreate($origin, [UriKind]::Absolute, [ref]$originURI) -or
        $originURI.Scheme -ne "https" -or $originURI.AbsolutePath -ne "/" -or $originURI.Query -ne "") {
        throw "every production gateway origin must be an HTTPS origin"
    }
}
$edgeBindAddress = Require-Value $values "EDGE_BIND_ADDRESS"
$parsedBindAddress = $null
if (-not [Net.IPAddress]::TryParse($edgeBindAddress, [ref]$parsedBindAddress)) {
    throw "EDGE_BIND_ADDRESS must be an IP address"
}
$certificatePath = Assert-SecretFile $values "TLS_CERTIFICATE_FILE" "certificate"
$privateKeyPath = Assert-SecretFile $values "TLS_PRIVATE_KEY_FILE" "private_key"
try {
    $certificateWithKey = [Security.Cryptography.X509Certificates.X509Certificate2]::CreateFromPemFile(
        $certificatePath,
        $privateKeyPath
    )
    if (-not $certificateWithKey.HasPrivateKey) {
        throw "certificate has no private key"
    }
} catch {
    throw "TLS certificate and private key are invalid or do not match"
}
[void](Assert-SecretFile $values "ALERTMANAGER_WEBHOOK_TOKEN_FILE" "token")

$recoverable = Read-Bool $values "AGENT_RECOVERABLE_RUNS_ENABLED"
$approvalRecovery = Read-Bool $values "AGENT_UNIFIED_APPROVAL_RECOVERY_ENABLED"
$planning = Read-Bool $values "AGENT_PLANNING_EXECUTE_ENABLED"
$externalMCP = Read-Bool $values "AGENT_EXTERNAL_MCP_ENABLED"
$externalMCPRead = Read-Bool $values "AGENT_PLANNING_EXECUTE_EXTERNAL_MCP_READ_ENABLED"
$externalMCPWrite = Read-Bool $values "AGENT_PLANNING_EXECUTE_EXTERNAL_MCP_WRITE_ENABLED"
$workflow = Read-Bool $values "AGENT_WORKFLOW_AS_TOOL_ENABLED"
$workflowRead = Read-Bool $values "AGENT_PLANNING_EXECUTE_WORKFLOW_READ_ENABLED"
$webSearch = Read-Bool $values "AGENT_WEB_SEARCH_ENABLED"

if ($planning) {
    if (-not $recoverable) { throw "Planning-Execute requires recoverable runs" }
    $canary = 0
    if (-not [int]::TryParse($values["AGENT_PLANNING_EXECUTE_CANARY_PERCENT"], [ref]$canary) -or $canary -lt 1 -or $canary -gt 100) {
        throw "Planning-Execute canary must be between 1 and 100"
    }
}
if (($externalMCPRead -or $externalMCPWrite) -and (-not $planning -or -not $externalMCP)) {
    throw "External MCP Planning routes require Planning-Execute and External MCP"
}
if ($externalMCPWrite -and -not $approvalRecovery) {
    throw "External MCP write Planning requires unified approval recovery"
}
if ($workflowRead -and (-not $planning -or -not $workflow)) {
    throw "Workflow Planning requires Planning-Execute and Workflow-as-Tool"
}
if ($webSearch) {
    [void](Require-Value $values "AGENT_WEB_SEARCH_API_KEY")
}

$previousNativePreference = $null
if (Test-Path variable:PSNativeCommandUseErrorActionPreference) {
    $previousNativePreference = $PSNativeCommandUseErrorActionPreference
    $PSNativeCommandUseErrorActionPreference = $false
}
try {
    $configText = (& docker compose --env-file $resolvedEnvFile -f $baseCompose -f $productionCompose config --format json | Out-String)
    if ($LASTEXITCODE -ne 0) {
        throw "docker compose config failed"
    }
} finally {
    if ($null -ne $previousNativePreference) {
        $PSNativeCommandUseErrorActionPreference = $previousNativePreference
    }
}
$config = $configText | ConvertFrom-Json
$serviceProperties = @($config.services.PSObject.Properties)
if ($serviceProperties.Count -eq 0) {
    throw "rendered Compose config contains no services"
}

$criticalHealthServices = @(
    "edge-proxy", "mysql", "mongodb", "redis", "rabbitmq", "elasticsearch",
    "prometheus", "alertmanager",
    "auth-service", "user-service", "tweet-service", "follow-service",
    "messenger-service", "notification-service", "canal-relay", "consumer",
    "agent-service", "gateway", "frontend"
)
$publicServices = @("edge-proxy")
$allowedLoopbackEnvironment = @("MCP_SERVER_ADDR")

foreach ($property in $serviceProperties) {
    $name = $property.Name
    $service = $property.Value
    if ([string]$service.image -notmatch "^[^@\s]+@sha256:[0-9a-fA-F]{64}$") {
        throw "rendered service $name does not use an immutable image digest"
    }
    if ($null -ne (Get-OptionalProperty $service "build")) {
        throw "rendered production service $name still contains a local build"
    }
    if ([string]$service.restart -ne "unless-stopped") {
        throw "rendered production service $name must use restart=unless-stopped"
    }
    $deploy = Get-OptionalProperty $service "deploy"
    $resources = Get-OptionalProperty $deploy "resources"
    $limits = Get-OptionalProperty $resources "limits"
    if ($null -eq $limits) {
        throw "rendered production service $name has no resource limits"
    }
    if ($criticalHealthServices -contains $name -and $null -eq (Get-OptionalProperty $service "healthcheck")) {
        throw "critical service $name has no healthcheck"
    }
    $ports = Get-OptionalProperty $service "ports"
    if ($null -ne $ports) {
        if ($publicServices -notcontains $name) {
            throw "internal service $name publishes a host port"
        }
        foreach ($port in @($ports)) {
            if ([string]$port.host_ip -ne $edgeBindAddress) {
                throw "public service $name must bind to EDGE_BIND_ADDRESS"
            }
        }
    }
    $environment = Get-OptionalProperty $service "environment"
    if ($null -ne $environment) {
        foreach ($entry in $environment.PSObject.Properties) {
            $environmentValue = [string]$entry.Value
            if ($entry.Name -eq "MOCK_EMBEDDING" -and $environmentValue.ToLowerInvariant() -ne "false") {
                throw "$name enables MOCK_EMBEDDING"
            }
            if ($allowedLoopbackEnvironment -notcontains $entry.Name -and $environmentValue -match "(?i)localhost|127\.0\.0\.1|host\.docker\.internal") {
                throw "$name environment $($entry.Name) depends on a loopback or desktop host"
            }
            if ($environmentValue.ToLowerInvariant() -in @("guest", "admin", "31415927", "twitter_clone_secret_key_2023")) {
                throw "$name environment $($entry.Name) uses a known development credential"
            }
        }
    }
}

Write-Output ("production_compose_preflight=PASS services={0} images={1} public_host={2}" -f $serviceProperties.Count, $imageKeys.Count, $publicHost)
