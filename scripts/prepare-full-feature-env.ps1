[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$SourceEnv,
    [Parameter(Mandatory = $true)]
    [string]$OutputPath
)

$ErrorActionPreference = 'Stop'
$repoRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$source = [System.IO.Path]::GetFullPath((Join-Path (Get-Location).Path $SourceEnv))
$target = [System.IO.Path]::GetFullPath((Join-Path (Get-Location).Path $OutputPath))
$rootPrefix = $repoRoot + [System.IO.Path]::DirectorySeparatorChar

if (-not $source.StartsWith($rootPrefix, [System.StringComparison]::OrdinalIgnoreCase) -or
    -not $target.StartsWith($rootPrefix, [System.StringComparison]::OrdinalIgnoreCase)) {
    throw 'SourceEnv and OutputPath must remain inside the repository.'
}
if (-not (Test-Path -LiteralPath $source -PathType Leaf)) {
    throw 'Source deployment .env is missing.'
}
if (Test-Path -LiteralPath $target) {
    throw 'OutputPath already exists; refusing to overwrite credentials.'
}

$lines = [System.IO.File]::ReadAllLines($source)
$values = @{}
foreach ($line in $lines) {
    if ($line -match '^([A-Z][A-Z0-9_]*)=(.*)$') {
        if ($values.ContainsKey($Matches[1])) {
            throw "Duplicate environment variable: $($Matches[1])"
        }
        $values[$Matches[1]] = $Matches[2]
    }
}

foreach ($name in @(
    'GATEWAY_ALLOWED_ORIGINS', 'DB_PASSWORD', 'MONGO_PASSWORD',
    'MINIO_USER', 'MINIO_PASSWORD', 'DASHSCOPE_API_KEY',
    'LM_STUDIO_API_URL', 'LM_STUDIO_API_TOKEN', 'TWEET_INTERNAL_SERVICE_TOKEN'
)) {
    if ([string]::IsNullOrWhiteSpace($values[$name])) {
        throw "Required deployment variable is missing: $name"
    }
}
foreach ($name in @('AGENT_PROVIDER_CONFIG_MASTER_KEY', 'AGENT_RUN_CHECKPOINT_MASTER_KEY')) {
    try {
        $decoded = [Convert]::FromBase64String($values[$name])
    } catch {
        throw "$name must be a base64-encoded 32-byte key."
    }
    if ($decoded.Length -ne 32) {
        throw "$name must be a base64-encoded 32-byte key."
    }
}
if ($values['AGENT_PROVIDER_CONFIG_MASTER_KEY'] -eq $values['AGENT_RUN_CHECKPOINT_MASTER_KEY']) {
    throw 'Provider and checkpoint encryption keys must be independent.'
}

# Enable implemented user-facing paths while preserving independent governance.
# External MCP still requires a user connection; publishing still requires
# authorization/approval. Destructive retention and direct Profile publishing
# are intentionally not part of this profile.
$features = [ordered]@{
    AGENT_EXTERNAL_MCP_ENABLED = 'true'
    AGENT_EXTERNAL_MCP_PROJECT_SCOPE_ENABLED = 'true'
    AGENT_EXTERNAL_MCP_HEALTH_CHECK_ENABLED = 'true'
    AGENT_WORKFLOW_AS_TOOL_ENABLED = 'true'
    AGENT_SKILL_CATALOG_ENABLED = 'true'
    AGENT_EXTENSION_CATALOG_ENABLED = 'true'
    AGENT_EXTENSION_MARKETPLACE_ENABLED = 'true'
    AGENT_RECOVERABLE_RUNS_ENABLED = 'true'
    AGENT_UNIFIED_APPROVAL_RECOVERY_ENABLED = 'true'
    AGENT_PLANNING_EXECUTE_ENABLED = 'true'
    AGENT_PLANNING_EXECUTE_CANARY_PERCENT = '100'
    AGENT_PLANNING_EXECUTE_PLATFORM_SEARCH_ENABLED = 'true'
    AGENT_PLANNING_EXECUTE_WEB_RESEARCH_ENABLED = 'true'
    AGENT_PLANNING_EXECUTE_EXTERNAL_MCP_READ_ENABLED = 'true'
    AGENT_PLANNING_EXECUTE_EXTERNAL_MCP_WRITE_ENABLED = 'true'
    AGENT_PLANNING_EXECUTE_WORKFLOW_READ_ENABLED = 'true'
    AGENT_GOAL_RUNTIME_ENABLED = 'true'
    AGENT_GOAL_RUNTIME_PLATFORM_SEARCH_SHADOW_ENABLED = 'true'
    AGENT_GOAL_RUNTIME_WEB_RESEARCH_SHADOW_ENABLED = 'true'
    AGENT_GOAL_RUNTIME_GROUNDED_DRAFT_SHADOW_ENABLED = 'true'
    AGENT_GOAL_RUNTIME_RESEARCH_DRAFT_SHADOW_ENABLED = 'true'
    AGENT_TASK_TEMPLATES_ENABLED = 'true'
    AGENT_MULTI_AGENT_PLANNER_ENABLED = 'true'
    AGENT_MULTI_AGENT_EXECUTION_ENABLED = 'true'
    AGENT_WEB_SEARCH_ENABLED = 'true'
    AGENT_WEB_PAGE_READ_ENABLED = 'true'
    AGENT_RUNTIME_V2_MODES = 'all'
}

$output = New-Object System.Collections.Generic.List[string]
foreach ($line in $lines) {
    if ($line -match '^([A-Z][A-Z0-9_]*)=' -and $features.Contains($Matches[1])) {
        continue
    }
    $output.Add($line)
}
$output.Add('')
$output.Add('# Full-feature deployment profile; rollback by restoring the previous .env.')
foreach ($name in $features.Keys) {
    $output.Add("$name=$($features[$name])")
}

$directory = [System.IO.Path]::GetDirectoryName($target)
[System.IO.Directory]::CreateDirectory($directory) | Out-Null
$text = ($output.ToArray() -join "`n") -replace "`r", ''
[System.IO.File]::WriteAllText($target, $text + "`n", (New-Object System.Text.UTF8Encoding($false)))
Write-Output "Created full-feature deployment .env at $target with existing credentials preserved."
