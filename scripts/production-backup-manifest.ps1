[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [ValidateSet("Create", "Verify")]
    [string]$Mode,

    [Parameter(Mandatory = $true)]
    [string]$BackupDirectory,

    [string]$SnapshotID
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$requiredArtifacts = @(
    "mysql.sql.gz",
    "mongo.archive.gz",
    "redis.rdb",
    "qdrant.snapshot",
    "elasticsearch.snapshot-manifest.json",
    "minio.inventory.json",
    "deployment-metadata.json"
)
$root = [IO.Path]::GetFullPath($BackupDirectory)
$manifestPath = Join-Path $root "backup-manifest.json"

function Assert-SafeArtifactPath {
    param([string]$Root, [string]$RelativePath)

    if ([IO.Path]::IsPathRooted($RelativePath)) {
        throw "backup artifact path must be relative"
    }
    $candidate = [IO.Path]::GetFullPath((Join-Path $Root $RelativePath))
    $relative = [IO.Path]::GetRelativePath($Root, $candidate)
    if ($relative.StartsWith("..")) {
        throw "backup artifact escapes the backup directory"
    }
    return $candidate
}

if (-not (Test-Path -LiteralPath $root -PathType Container)) {
    throw "backup directory does not exist: $root"
}

if ($Mode -eq "Create") {
    if ([string]::IsNullOrWhiteSpace($SnapshotID) -or $SnapshotID -notmatch "^[A-Za-z0-9][A-Za-z0-9._-]{2,63}$") {
        throw "SnapshotID must be 3-64 safe characters"
    }
    $items = foreach ($relativePath in $requiredArtifacts) {
        $path = Assert-SafeArtifactPath $root $relativePath
        if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
            throw "required backup artifact is missing: $relativePath"
        }
        $file = Get-Item -LiteralPath $path
        if ($file.Length -le 0) {
            throw "backup artifact is empty: $relativePath"
        }
        [ordered]@{
            path = $relativePath
            bytes = $file.Length
            sha256 = (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToLowerInvariant()
        }
    }
    $manifest = [ordered]@{
        schema = "twitter-clone.production-backup.v1"
        snapshot_id = $SnapshotID
        created_at_utc = [DateTime]::UtcNow.ToString("o")
        artifacts = @($items)
    }
    $temporaryPath = $manifestPath + ".tmp"
    [IO.File]::WriteAllText(
        $temporaryPath,
        ($manifest | ConvertTo-Json -Depth 5),
        [Text.UTF8Encoding]::new($false)
    )
    Move-Item -LiteralPath $temporaryPath -Destination $manifestPath -Force
    Write-Output ("backup_manifest=CREATED snapshot_id={0} artifacts={1}" -f $SnapshotID, $items.Count)
    exit 0
}

if (-not (Test-Path -LiteralPath $manifestPath -PathType Leaf)) {
    throw "backup manifest is missing"
}
$manifest = Get-Content -LiteralPath $manifestPath -Raw | ConvertFrom-Json
if ($manifest.schema -ne "twitter-clone.production-backup.v1") {
    throw "unsupported backup manifest schema"
}
$manifestItems = @($manifest.artifacts)
if ($manifestItems.Count -ne $requiredArtifacts.Count) {
    throw "backup manifest artifact count is invalid"
}
$seen = @{}
foreach ($item in $manifestItems) {
    $relativePath = [string]$item.path
    if ($requiredArtifacts -notcontains $relativePath -or $seen.ContainsKey($relativePath)) {
        throw "backup manifest contains an unknown or duplicate artifact"
    }
    $seen[$relativePath] = $true
    $path = Assert-SafeArtifactPath $root $relativePath
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
        throw "backup artifact is missing: $relativePath"
    }
    $file = Get-Item -LiteralPath $path
    if ($file.Length -ne [int64]$item.bytes) {
        throw "backup artifact size mismatch: $relativePath"
    }
    $actualHash = (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actualHash -ne ([string]$item.sha256).ToLowerInvariant()) {
        throw "backup artifact hash mismatch: $relativePath"
    }
}
foreach ($required in $requiredArtifacts) {
    if (-not $seen.ContainsKey($required)) {
        throw "backup manifest is missing required artifact: $required"
    }
}
Write-Output ("backup_manifest=VERIFIED snapshot_id={0} artifacts={1}" -f $manifest.snapshot_id, $manifestItems.Count)
