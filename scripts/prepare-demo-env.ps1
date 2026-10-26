param(
    [Parameter(Mandatory = $true)]
    [string]$OutputPath,
    [Parameter(Mandatory = $true)]
    [string]$PublicOrigin,
    [string]$SourceEnv,
    [switch]$AugmentExisting
)

$ErrorActionPreference = 'Stop'

if ([string]::IsNullOrWhiteSpace($SourceEnv)) {
    $SourceEnv = Join-Path $PSScriptRoot '..\.env'
}

$repoRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$target = [System.IO.Path]::GetFullPath((Join-Path (Get-Location).Path $OutputPath))
if (-not $target.StartsWith($repoRoot + [System.IO.Path]::DirectorySeparatorChar, [System.StringComparison]::OrdinalIgnoreCase)) {
    throw 'OutputPath must remain inside the repository.'
}
if ((Test-Path -LiteralPath $target) -and -not $AugmentExisting) {
    throw 'OutputPath already exists; refusing to overwrite credentials.'
}

$originUri = $null
if (-not [System.Uri]::TryCreate($PublicOrigin, [System.UriKind]::Absolute, [ref]$originUri) -or
    $originUri.Scheme -notin @('http', 'https') -or
    $originUri.AbsolutePath -ne '/' -or
    $originUri.Query -ne '' -or
    $originUri.Fragment -ne '' -or
    $originUri.UserInfo -ne '' -or
    $PublicOrigin.EndsWith('/')) {
    throw 'PublicOrigin must be an exact HTTP(S) origin without a path or trailing slash.'
}

if (-not (Test-Path -LiteralPath $SourceEnv)) {
    throw 'Source .env is missing; supply DashScope key locally before generating this file.'
}
$apiKey = $null
foreach ($line in [System.IO.File]::ReadAllLines([System.IO.Path]::GetFullPath($SourceEnv))) {
    if ($line -match '^\s*DASHSCOPE_API_KEY\s*=\s*(.*?)\s*$') {
        $apiKey = $Matches[1].Trim('"', "'", ' ')
    }
}
if ([string]::IsNullOrWhiteSpace($apiKey) -or $apiKey -notmatch '^[A-Za-z0-9._-]+$') {
    throw 'Source .env has no valid DashScope API key; refusing to create an incomplete file.'
}

function New-RandomBytes([int]$count) {
    $bytes = New-Object byte[] $count
    $rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
    try { $rng.GetBytes($bytes) } finally { $rng.Dispose() }
    return ,$bytes
}

function New-Password {
    return [Convert]::ToBase64String((New-RandomBytes 32)).TrimEnd('=').Replace('+', '-').Replace('/', '_')
}

if (Test-Path -LiteralPath $target) {
    $existing = @{}
    foreach ($line in [System.IO.File]::ReadAllLines($target)) {
        if ($line -match '^([A-Z_]+)=(.*)$') {
            $existing[$Matches[1]] = $Matches[2]
        }
    }
    if ($existing['GATEWAY_ALLOWED_ORIGINS'] -ne $PublicOrigin -or [string]::IsNullOrWhiteSpace($existing['DASHSCOPE_API_KEY'])) {
        throw 'Existing deployment .env has a different origin or no DashScope key; refusing to modify it.'
    }
    $serverApiKey = $existing['DASHSCOPE_API_KEY']
    $settings = [ordered]@{
        TWEET_INTERNAL_SERVICE_TOKEN = New-Password
        LM_STUDIO_API_URL = 'https://dashscope.aliyuncs.com/compatible-mode/v1'
        LM_STUDIO_API_TOKEN = $serverApiKey
        LM_STUDIO_MODEL_CHAT = 'qwen-plus'
        LM_STUDIO_MODEL_EMBEDDING = 'text-embedding-v3'
        AGENT_SESSION_SUMMARY_MODEL = 'qwen-plus'
    }
    $added = New-Object System.Collections.Generic.List[string]
    foreach ($name in $settings.Keys) {
        if (-not $existing.ContainsKey($name)) {
            $added.Add("$name=$($settings[$name])")
        }
    }
    if ($added.Count -gt 0) {
        [System.IO.File]::AppendAllLines($target, [string[]]$added, (New-Object System.Text.UTF8Encoding($false)))
    }
    Write-Output "Updated deployment .env at $target without changing existing credentials. Added $($added.Count) settings."
    return
}

$dbPassword = New-Password
$mongoPassword = New-Password
$minioPassword = New-Password
$tweetToken = New-Password
$minioUser = 'minio_' + ([BitConverter]::ToString((New-RandomBytes 8)).Replace('-', '').ToLowerInvariant())
$providerKey = [Convert]::ToBase64String((New-RandomBytes 32))
$checkpointKey = [Convert]::ToBase64String((New-RandomBytes 32))

$lines = @(
    '# Deployment credentials: keep private; do not commit or share in chat.'
    "GATEWAY_ALLOWED_ORIGINS=$PublicOrigin"
    'MINIO_PUBLIC_URL=/media'
    "DB_PASSWORD=$dbPassword"
    "MONGO_PASSWORD=$mongoPassword"
    "MINIO_USER=$minioUser"
    "MINIO_PASSWORD=$minioPassword"
    "DASHSCOPE_API_KEY=$apiKey"
    "TWEET_INTERNAL_SERVICE_TOKEN=$tweetToken"
    'LM_STUDIO_API_URL=https://dashscope.aliyuncs.com/compatible-mode/v1'
    "LM_STUDIO_API_TOKEN=$apiKey"
    'LM_STUDIO_MODEL_CHAT=qwen-plus'
    'LM_STUDIO_MODEL_EMBEDDING=text-embedding-v3'
    'AGENT_SESSION_SUMMARY_MODEL=qwen-plus'
    'AGENT_PROVIDER_CONFIG_ACTIVE_KEY_ID=v1'
    "AGENT_PROVIDER_CONFIG_MASTER_KEY=$providerKey"
    'AGENT_RUN_CHECKPOINT_ACTIVE_KEY_ID=v1'
    "AGENT_RUN_CHECKPOINT_MASTER_KEY=$checkpointKey"
)

[System.IO.File]::WriteAllLines($target, [string[]]$lines, (New-Object System.Text.UTF8Encoding($false)))
Write-Output "Created deployment .env at $target with fresh infrastructure credentials and the local DashScope key."
