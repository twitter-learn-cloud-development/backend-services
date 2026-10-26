[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [ValidateSet("Initialize", "SetCheck", "Verify")]
    [string]$Mode,

    [Parameter(Mandatory = $true)]
    [string]$RecordFile,

    [string]$PlanFile = (Join-Path $PSScriptRoot "../deploy/acceptance/production-launch-checks.json"),
    [string]$EvidenceRoot,
    [string]$ReleaseID,
    [string]$Environment,
    [string]$Owner,
    [string]$CheckID,
    [string]$CheckStatus,
    [string[]]$Evidence = @(),
    [string]$Reason = "",
    [string]$ReleaseManifestFile,
    [string]$ExpectedReleaseManifestSHA256,
    [string]$ExpectedSourceSHA,
    [string]$EnvironmentSnapshotFile,
    [string]$ExpectedEnvironmentSnapshotSHA256,
    [string]$ObservationFile,

    [Parameter(Mandatory = $true)]
    [string]$SigningKeyFile,

    [Parameter(Mandatory = $true)]
    [string]$SigningKeyID,

    [switch]$RequireReleaseReady,
    [switch]$AllowRepositoryFiles
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$repositoryRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot ".."))
$planPath = [IO.Path]::GetFullPath($PlanFile)
$recordPath = [IO.Path]::GetFullPath($RecordFile)
$keyPath = [IO.Path]::GetFullPath($SigningKeyFile)
$allowedStatuses = @("not_run", "passed", "failed", "blocked")
$maximumEvidenceFiles = 8
$maximumEvidenceBytes = 50MB
$maximumReasonLength = 500
. (Join-Path $PSScriptRoot "lib/deployment-evidence.ps1")

function Assert-SafeIdentifier {
    param([string]$Value, [string]$Name)

    if ([string]::IsNullOrWhiteSpace($Value) -or $Value -notmatch "^[A-Za-z0-9][A-Za-z0-9._:-]{2,127}$") {
        throw "$Name must contain 3-128 safe identifier characters"
    }
}

function Assert-ExternalPath {
    param([string]$Path, [string]$Name, [ValidateSet("Leaf", "Container", "Any")] [string]$Kind = "Any")

    $resolved = [IO.Path]::GetFullPath($Path)
    $relative = [IO.Path]::GetRelativePath($repositoryRoot, $resolved)
    $outside = [IO.Path]::IsPathRooted($relative) -or $relative -eq ".." -or $relative.StartsWith(".." + [IO.Path]::DirectorySeparatorChar)
    if (-not $AllowRepositoryFiles -and -not $outside) {
        throw "$Name must live outside the repository"
    }
    if ($Kind -eq "Leaf" -and -not (Test-Path -LiteralPath $resolved -PathType Leaf)) {
        throw "$Name does not reference a file"
    }
    if ($Kind -eq "Container" -and -not (Test-Path -LiteralPath $resolved -PathType Container)) {
        throw "$Name does not reference a directory"
    }
    return $resolved
}

function Read-SigningKey {
    $resolved = Assert-ExternalPath $keyPath "SigningKeyFile" "Leaf"
    [void](Assert-AcceptanceFile $resolved 4096)
    $bytes = [IO.File]::ReadAllBytes($resolved)
    if ($bytes.Length -lt 32 -or $bytes.Length -gt 4096) {
        throw "SigningKeyFile must contain 32-4096 bytes"
    }
    return $bytes
}

function ConvertTo-CanonicalNode {
    param([object]$Value)

    if ($null -eq $Value) { return $null }
    if ($Value -is [string] -or $Value -is [ValueType]) { return $Value }
    if ($Value -is [Collections.IDictionary]) {
        $ordered = [ordered]@{}
        foreach ($key in @($Value.Keys | Sort-Object)) {
            $ordered[[string]$key] = ConvertTo-CanonicalNode $Value[$key]
        }
        return $ordered
    }
    if ($Value -is [Collections.IEnumerable]) {
        return @($Value | ForEach-Object { ConvertTo-CanonicalNode $_ })
    }
    $object = [ordered]@{}
    foreach ($property in @($Value.PSObject.Properties | Sort-Object Name)) {
        $object[$property.Name] = ConvertTo-CanonicalNode $property.Value
    }
    return $object
}

function Get-RecordSignature {
    param([object]$Record, [byte[]]$Key)

    # Only the root signature is excluded. Nested fields named signature remain
    # authenticated, rather than providing an unsigned metadata escape hatch.
    $unsigned = [ordered]@{}
    foreach ($property in $Record.PSObject.Properties) {
        if ($property.Name -cne "signature") { $unsigned[$property.Name] = $property.Value }
    }
    $payload = ConvertTo-CanonicalNode $unsigned | ConvertTo-Json -Compress -Depth 30
    $hmac = [Security.Cryptography.HMACSHA256]::new($Key)
    try {
        return [Convert]::ToHexString($hmac.ComputeHash([Text.Encoding]::UTF8.GetBytes($payload))).ToLowerInvariant()
    } finally {
        $hmac.Dispose()
    }
}

function Read-Plan {
    if (-not (Test-Path -LiteralPath $planPath -PathType Leaf)) {
        throw "acceptance plan does not exist"
    }
    $plan = Read-AcceptanceJson $planPath
    if ($plan.schema -ne "twitter-clone.production-launch-plan.v1") {
        throw "unsupported acceptance plan schema"
    }
    $seen = @{}
    foreach ($check in @($plan.checks)) {
        Assert-SafeIdentifier ([string]$check.id) "plan check id"
        if ($seen.ContainsKey([string]$check.id)) { throw "duplicate plan check id" }
        if ([string]$check.stage -notin @("L3", "L4", "L5")) { throw "invalid plan stage" }
        if ([string]::IsNullOrWhiteSpace([string]$check.title)) { throw "plan check title is empty" }
        if ($check.required -isnot [bool]) { throw "plan check required must be boolean" }
        $seen[[string]$check.id] = $true
    }
    if ($seen.Count -eq 0 -or $seen.Count -gt 64) { throw "acceptance plan check matrix is not bounded" }
    return $plan
}

function Write-SignedRecord {
    param([object]$Record, [byte[]]$Key)

    # Normalize PowerShell dictionaries and JSON-loaded objects to one shape
    # before signing so the signature survives a write/read round trip.
    $Record.signature = $null
    $normalized = ($Record | ConvertTo-Json -Depth 30 | ConvertFrom-Json)
    $normalized.signature = [ordered]@{
        key_id = $SigningKeyID
        algorithm = "hmac-sha256"
        value = Get-RecordSignature $normalized $Key
    }
    $parent = Split-Path -Parent $recordPath
    if (-not (Test-Path -LiteralPath $parent -PathType Container)) {
        throw "record parent directory does not exist"
    }
    $ancestor = Get-Item -LiteralPath $parent -Force
    while ($null -ne $ancestor) {
        if (($ancestor.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { throw "acceptance output reparse path rejected" }
        $ancestor = $ancestor.Parent
    }
    $body = [Text.Encoding]::UTF8.GetBytes(($normalized | ConvertTo-Json -Depth 30))
    if ($body.Length -gt 1MB) { throw "acceptance record exceeds size bound" }
    $temporary = $recordPath + "." + [Guid]::NewGuid().ToString("N") + ".tmp"
    $stream = [IO.File]::Open($temporary, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::None)
    try { $stream.Write($body, 0, $body.Length); $stream.Flush($true) } finally { $stream.Dispose() }
    # Exclusive initialization never overwrites an existing signed record.
    [IO.File]::Move($temporary, $recordPath, ($Mode -ne "Initialize"))
}

function Read-VerifiedRecord {
    param([object]$Plan, [byte[]]$Key)

    if (-not (Test-Path -LiteralPath $recordPath -PathType Leaf)) { throw "acceptance record does not exist" }
    $record = Read-AcceptanceJson $recordPath
    if ($record.schema -cnotin @("twitter-clone.production-launch-record.v1", "twitter-clone.production-launch-record.v2")) { throw "unsupported acceptance record schema" }
    if ($record.plan_sha256 -ne (Get-FileHash -LiteralPath $planPath -Algorithm SHA256).Hash.ToLowerInvariant()) {
        throw "acceptance plan digest mismatch"
    }
    Assert-AcceptanceFields $record.signature @("key_id", "algorithm", "value")
    if ($record.signature.key_id -cne $SigningKeyID -or $record.signature.algorithm -cne "hmac-sha256") {
        throw "acceptance signature identity mismatch"
    }
    $expected = Get-RecordSignature $record $Key
    $expectedBytes = [Convert]::FromHexString($expected)
    try { $actualBytes = [Convert]::FromHexString([string]$record.signature.value) } catch { throw "acceptance signature is malformed" }
    if ($actualBytes.Length -ne $expectedBytes.Length -or
        [Security.Cryptography.CryptographicOperations]::FixedTimeEquals($expectedBytes, $actualBytes) -ne $true) {
        throw "acceptance record signature mismatch"
    }
    Assert-SafeIdentifier ([string]$record.release_id) "record release id"
    Assert-SafeIdentifier ([string]$record.environment) "record environment"
    Assert-SafeIdentifier ([string]$record.owner) "record owner"
    if ($record.schema -ceq "twitter-clone.production-launch-record.v2") {
        $binding = Get-DeploymentBinding $ReleaseManifestFile $ExpectedReleaseManifestSHA256 $ExpectedSourceSHA $EnvironmentSnapshotFile $ExpectedEnvironmentSnapshotSHA256 $Environment
        $actualBinding = ConvertTo-CanonicalNode $record.release_binding | ConvertTo-Json -Compress -Depth 30
        $expectedBinding = ConvertTo-CanonicalNode $binding | ConvertTo-Json -Compress -Depth 30
        if ($actualBinding -cne $expectedBinding -or $record.environment -cne $Environment) { throw "signed deployment binding mismatch" }
    }

    $planByID = @{}
    foreach ($planCheck in @($Plan.checks)) { $planByID[[string]$planCheck.id] = $planCheck }
    $planIDs = @($planByID.Keys)
    $recordChecks = @($record.checks)
    if ($recordChecks.Count -ne $planIDs.Count) { throw "acceptance record check count mismatch" }
    $seen = @{}
    foreach ($check in $recordChecks) {
        $id = [string]$check.id
        if ($id -notin $planIDs -or $seen.ContainsKey($id)) { throw "unknown or duplicate record check" }
        if ([string]$check.status -notin $allowedStatuses) { throw "invalid record check status" }
        $planCheck = $planByID[$id]
        if ([string]$check.stage -ne [string]$planCheck.stage -or
            [string]$check.title -ne [string]$planCheck.title -or
            [bool]$check.required -ne [bool]$planCheck.required) {
            throw "record check metadata differs from the acceptance plan: $id"
        }
        $evidenceItems = @($check.evidence)
        if ($evidenceItems.Count -gt $maximumEvidenceFiles) { throw "record evidence exceeds limit" }
        if ($check.status -eq "passed" -and $evidenceItems.Count -eq 0) {
            throw "passed record check has no evidence: $id"
        }
        if ($check.status -in @("failed", "blocked") -and [string]::IsNullOrWhiteSpace([string]$check.reason)) {
            throw "$($check.status) record check has no reason: $id"
        }
        if ($check.status -eq "not_run" -and ($evidenceItems.Count -gt 0 -or -not [string]::IsNullOrWhiteSpace([string]$check.reason))) {
            throw "not_run record check contains a result: $id"
        }
        if ($record.schema -ceq "twitter-clone.production-launch-record.v2") {
            if ($check.status -eq "passed" -and $null -eq $check.observation) { throw "passed check requires a live observation" }
            if ($check.status -ne "passed" -and $null -ne $check.observation) { throw "only passed checks may contain observations" }
        }
        $seen[$id] = $true
    }
    return $record
}

function Get-EvidenceMetadata {
    param([string[]]$RelativePaths)

    if ($RelativePaths.Count -gt $maximumEvidenceFiles) { throw "a check may reference at most $maximumEvidenceFiles evidence files" }
    $root = Assert-ExternalPath $EvidenceRoot "EvidenceRoot" "Container"
    $seen = @{}
    return @($RelativePaths | ForEach-Object {
        $relative = [string]$_
        if ([string]::IsNullOrWhiteSpace($relative) -or [IO.Path]::IsPathRooted($relative)) {
            throw "evidence path must be relative"
        }
        $path = [IO.Path]::GetFullPath((Join-Path $root $relative))
        $normalized = [IO.Path]::GetRelativePath($root, $path).Replace("\", "/")
        if ($normalized.StartsWith("..") -or $seen.ContainsKey($normalized)) { throw "evidence path escapes the root or is duplicated" }
        if (-not (Test-Path -LiteralPath $path -PathType Leaf)) { throw "evidence file does not exist: $normalized" }
        $file = Get-Item -LiteralPath $path
        [void](Assert-AcceptanceFile $path $maximumEvidenceBytes)
        if ($file.Length -le 0 -or $file.Length -gt $maximumEvidenceBytes) { throw "evidence file size is outside the allowed range: $normalized" }
        $seen[$normalized] = $true
        [ordered]@{
            path = $normalized
            bytes = $file.Length
            sha256 = (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToLowerInvariant()
        }
    })
}

function Assert-EvidenceIntegrity {
    param([object]$Record)

    $requiresRoot = @($Record.checks | ForEach-Object { @($_.evidence) } | Where-Object { $null -ne $_ }).Count -gt 0
    if (-not $requiresRoot) { return }
    $root = Assert-ExternalPath $EvidenceRoot "EvidenceRoot" "Container"
    foreach ($check in @($Record.checks)) {
        foreach ($item in @($check.evidence)) {
            $path = [IO.Path]::GetFullPath((Join-Path $root ([string]$item.path)))
            $relative = [IO.Path]::GetRelativePath($root, $path)
            if ($relative.StartsWith("..") -or -not (Test-Path -LiteralPath $path -PathType Leaf)) {
                throw "recorded evidence is missing or unsafe: $($item.path)"
            }
            $file = Get-Item -LiteralPath $path
            [void](Assert-AcceptanceFile $path $maximumEvidenceBytes)
            if ($file.Length -ne [int64]$item.bytes) { throw "recorded evidence size mismatch: $($item.path)" }
            $hash = (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToLowerInvariant()
            if ($hash -ne ([string]$item.sha256).ToLowerInvariant()) { throw "recorded evidence hash mismatch: $($item.path)" }
        }
        if ($Record.schema -ceq "twitter-clone.production-launch-record.v2" -and $check.status -eq "passed") {
            $metadata = @(Get-EvidenceMetadata @([string]$check.observation.path))
            if ($metadata.Count -ne 1 -or $metadata[0].bytes -ne $check.observation.bytes -or $metadata[0].sha256 -cne $check.observation.sha256) { throw "observation file digest mismatch" }
            $observation = Read-AcceptanceJson (Join-Path $root $metadata[0].path)
            Assert-DeploymentObservation $observation $Record.release_binding ([string]$check.id) @($check.evidence) ([string]$Record.created_at_utc)
        }
    }
}

Assert-SafeIdentifier $SigningKeyID "SigningKeyID"
$plan = Read-Plan
$key = Read-SigningKey
[void](Assert-ExternalPath $recordPath "RecordFile")

$recordLock = $null
if ($Mode -eq "SetCheck") {
    $lockPath = $recordPath + ".lock"
    $ancestor = Get-Item -LiteralPath (Split-Path -Parent $recordPath) -Force
    while ($null -ne $ancestor) {
        if (($ancestor.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { throw "acceptance output reparse path rejected" }
        $ancestor = $ancestor.Parent
    }
    if (Test-Path -LiteralPath $lockPath) {
        $lockItem = Get-Item -LiteralPath $lockPath -Force
        if ($lockItem.PSIsContainer -or ($lockItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { throw "acceptance lock path is unsafe" }
    }
    try { $recordLock = [IO.File]::Open($lockPath, [IO.FileMode]::OpenOrCreate, [IO.FileAccess]::ReadWrite, [IO.FileShare]::None) }
    catch { throw "acceptance record is locked by another writer" }
}
try {
if ($Mode -eq "Initialize") {
    if (Test-Path -LiteralPath $recordPath) { throw "acceptance record already exists" }
    Assert-SafeIdentifier $ReleaseID "ReleaseID"
    Assert-SafeIdentifier $Environment "Environment"
    Assert-SafeIdentifier $Owner "Owner"
    $binding = $null
    if ($ReleaseManifestFile -or $ExpectedReleaseManifestSHA256 -or $ExpectedSourceSHA -or $EnvironmentSnapshotFile -or $ExpectedEnvironmentSnapshotSHA256) {
        $binding = Get-DeploymentBinding $ReleaseManifestFile $ExpectedReleaseManifestSHA256 $ExpectedSourceSHA $EnvironmentSnapshotFile $ExpectedEnvironmentSnapshotSHA256 $Environment
    }
    $now = [DateTime]::UtcNow.ToString("o")
    $checks = @($plan.checks | ForEach-Object {
        [ordered]@{
            id = [string]$_.id
            stage = [string]$_.stage
            title = [string]$_.title
            required = [bool]$_.required
            status = "not_run"
            reason = ""
            evidence = @()
            updated_at_utc = $null
        }
    })
    $record = [pscustomobject][ordered]@{
        schema = "twitter-clone.production-launch-record.v1"
        release_id = $ReleaseID
        environment = $Environment
        owner = $Owner
        plan_sha256 = (Get-FileHash -LiteralPath $planPath -Algorithm SHA256).Hash.ToLowerInvariant()
        created_at_utc = $now
        updated_at_utc = $now
        revision = 1
        checks = $checks
        signature = $null
    }
    if ($null -ne $binding) {
        $record.schema = "twitter-clone.production-launch-record.v2"
        $record | Add-Member -NotePropertyName release_binding -NotePropertyValue $binding
        foreach ($check in $checks) { $check["observation"] = $null }
    }
    Write-SignedRecord $record $key
    Write-Output ("production_acceptance=INITIALIZED release_id={0} checks={1}" -f $ReleaseID, $checks.Count)
    exit 0
}

$record = Read-VerifiedRecord $plan $key

if ($Mode -eq "SetCheck") {
    Assert-SafeIdentifier $CheckID "CheckID"
    if ($CheckStatus -notin $allowedStatuses) { throw "CheckStatus must be not_run, passed, failed or blocked" }
    if ($Reason.Length -gt $maximumReasonLength) { throw "Reason exceeds $maximumReasonLength characters" }
    if ($Reason -match "(?i)(api[_ -]?key|authorization|bearer|password|secret|token)\s*[:=]") {
        throw "Reason appears to contain secret material"
    }
    if ($CheckStatus -eq "passed" -and $Evidence.Count -eq 0) { throw "passed checks require evidence" }
    if ($CheckStatus -in @("failed", "blocked") -and [string]::IsNullOrWhiteSpace($Reason)) {
        throw "$CheckStatus checks require a reason"
    }
    if ($CheckStatus -eq "not_run" -and ($Evidence.Count -gt 0 -or $Reason.Length -gt 0)) {
        throw "not_run checks cannot contain evidence or a reason"
    }
    $matching = @($record.checks | Where-Object { $_.id -eq $CheckID })
    if ($matching.Count -ne 1) { throw "unknown acceptance check: $CheckID" }
    $metadata = if ($Evidence.Count -gt 0) { Get-EvidenceMetadata $Evidence } else { @() }
    if ($record.schema -ceq "twitter-clone.production-launch-record.v2") {
        if ($CheckStatus -eq "passed") {
            if ([string]::IsNullOrWhiteSpace($ObservationFile)) { throw "passed checks require a live observation file" }
            $observationMetadata = @(Get-EvidenceMetadata @($ObservationFile))
            if ($observationMetadata[0].path -cin @($metadata | ForEach-Object { $_.path })) { throw "observation must be separate from raw evidence" }
            $observation = Read-AcceptanceJson (Join-Path $EvidenceRoot $observationMetadata[0].path)
            Assert-DeploymentObservation $observation $record.release_binding $CheckID @($metadata) ([string]$record.created_at_utc)
            $matching[0].observation = $observationMetadata[0]
        } else {
            if ($ObservationFile) { throw "only passed checks may contain observations" }
            $matching[0].observation = $null
        }
    }
    $matching[0].status = $CheckStatus
    $matching[0].reason = $Reason.Trim()
    $matching[0].evidence = @($metadata)
    $matching[0].updated_at_utc = [DateTime]::UtcNow.ToString("o")
    $record.revision = [int64]$record.revision + 1
    $record.updated_at_utc = [DateTime]::UtcNow.ToString("o")
    Write-SignedRecord $record $key
    Write-Output ("production_acceptance=UPDATED check={0} status={1} revision={2}" -f $CheckID, $CheckStatus, $record.revision)
    exit 0
}

Assert-EvidenceIntegrity $record
$required = @($record.checks | Where-Object { $_.required -eq $true })
$passed = @($required | Where-Object { $_.status -eq "passed" }).Count
$failed = @($required | Where-Object { $_.status -eq "failed" }).Count
$blocked = @($required | Where-Object { $_.status -eq "blocked" }).Count
$notRun = @($required | Where-Object { $_.status -eq "not_run" }).Count
$complete = $passed -eq $required.Count
$ready = $complete -and $record.schema -ceq "twitter-clone.production-launch-record.v2"
$recordHash = (Get-FileHash -LiteralPath $recordPath -Algorithm SHA256).Hash.ToLowerInvariant()
Write-Output ("production_acceptance=VERIFIED release_id={0} revision={1} required={2} passed={3} failed={4} blocked={5} not_run={6} release_ready={7} record_sha256={8}" -f $record.release_id, $record.revision, $required.Count, $passed, $failed, $blocked, $notRun, $ready.ToString().ToLowerInvariant(), $recordHash)
Write-Output ("checks_complete={0} release_bound={1} deployment_accepted=false" -f $complete.ToString().ToLowerInvariant(), ($record.schema -ceq "twitter-clone.production-launch-record.v2").ToString().ToLowerInvariant())
if ($RequireReleaseReady -and -not $ready) {
    throw "production acceptance is not release-ready"
}
} finally {
    if ($null -ne $recordLock) { $recordLock.Dispose() }
}
