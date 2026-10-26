[CmdletBinding()]
param()
Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
$repositoryRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot ".."))
$fixtureRoot = Join-Path $repositoryRoot (".codex-tmp/deployment-evidence-self-test-" + [Guid]::NewGuid().ToString("N"))
$evidenceRoot = Join-Path $fixtureRoot "evidence"
$subject = Join-Path $PSScriptRoot "production-acceptance-record.ps1"
$planFile = Join-Path $repositoryRoot "deploy/acceptance/production-launch-checks.json"
$manifestFile = Join-Path $fixtureRoot "release.json"
$snapshotFile = Join-Path $fixtureRoot "environment.json"
$recordFile = Join-Path $fixtureRoot "record.json"
$keyFile = Join-Path $fixtureRoot "fixture-key.bin"
$source = "1" * 40
$cases = 0
function Write-FixtureJson([string]$Path, [object]$Value) { [IO.File]::WriteAllText($Path, ($Value | ConvertTo-Json -Depth 30), [Text.UTF8Encoding]::new($false)) }
function File-Hash([string]$Path) { return (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant() }
function Expect-Failure([scriptblock]$Action, [string]$Pattern) {
    try { & $Action | Out-Null; throw "negative deployment fixture unexpectedly succeeded" }
    catch { if ($_.Exception.Message -notmatch $Pattern) { throw } }
    $script:cases++
}
try {
    New-Item -ItemType Directory -Path $evidenceRoot -Force | Out-Null
    $key = [byte[]]::new(32); [Security.Cryptography.RandomNumberGenerator]::Fill($key); [IO.File]::WriteAllBytes($keyFile, $key)
    $catalogFile = Join-Path $repositoryRoot "deploy/release/catalog.json"
    $catalog = Get-Content -LiteralPath $catalogFile -Raw | ConvertFrom-Json
    $catalogHash = File-Hash $catalogFile
    $imageEnvironment = [ordered]@{}
    $images = @($catalog.include | ForEach-Object {
        $pinned = "registry.invalid/fixture/$($_.service)@sha256:$("a" * 64)"
        $imageEnvironment[$_.image_env] = $pinned
        [ordered]@{ service = $_.service; source_sha = $source; catalog_sha256 = $catalogHash
            repository = "registry.invalid/fixture/$($_.service)"; digest = "sha256:$("a" * 64)"; published = $true
            builder_image = "fixture-builder@sha256:$("b" * 64)"; runtime_image = "fixture-runtime@sha256:$("c" * 64)" }
    })
    # These are synthetic published/live statements in an isolated fixture only.
    # They are never genuine CI, registry or deployment evidence.
    $manifest = [ordered]@{ version = 1; source_sha = $source; catalog_sha256 = $catalogHash
        images_published = $true; deployment_accepted = $false; images = $images
        checks = @(@("go_test", "go_vet", "go_race", "package_build", "web_build", "web_test", "mobile_analyze", "mobile_test", "production_config") | ForEach-Object {
            [ordered]@{ kind = $_; source_sha = $source; passed = $true; evidence_sha256 = "d" * 64 }
        }); inputs = @(@{ path = "fixture-only"; sha256 = "e" * 64 }); image_environment = $imageEnvironment }
    Write-FixtureJson $manifestFile $manifest
    $manifestHash = File-Hash $manifestFile
    $snapshot = [ordered]@{ schema = "twitter-clone.deployment-environment.v1"; deployment_id = "fixture-deployment"; environment = "staging"
        source_sha = $source; release_manifest_sha256 = $manifestHash; configuration_revision = "fixture-config-v1"
        secret_revision = "fixture-reference-v1"; schema_revision = "fixture-schema-v1"; previous_release_manifest_sha256 = "f" * 64
        images = $imageEnvironment; infrastructure_images = @{ mysql = "mysql@sha256:$("b" * 64)" }
        data_deletion_enabled = $false; token_revocation_supported = $false }
    Write-FixtureJson $snapshotFile $snapshot
    $common = @{ RecordFile = $recordFile; PlanFile = $planFile; SigningKeyFile = $keyFile; SigningKeyID = "fixture-key-v2"
        AllowRepositoryFiles = $true; ReleaseManifestFile = $manifestFile; ExpectedReleaseManifestSHA256 = $manifestHash
        ExpectedSourceSHA = $source; EnvironmentSnapshotFile = $snapshotFile; ExpectedEnvironmentSnapshotSHA256 = File-Hash $snapshotFile; Environment = "staging" }
    $bad = $common.Clone(); $bad.ExpectedSourceSHA = "2" * 40
    Expect-Failure { & $subject -Mode Initialize @bad -ReleaseID "fixture-release" -Owner "fixture-owner" } "manifest identity"
    $manifest.images_published = $false; Write-FixtureJson $manifestFile $manifest
    $bad = $common.Clone(); $bad.ExpectedReleaseManifestSHA256 = File-Hash $manifestFile
    Expect-Failure { & $subject -Mode Initialize @bad -ReleaseID "fixture-release" -Owner "fixture-owner" } "candidate"
    $manifest.images_published = $true; Write-FixtureJson $manifestFile $manifest
    $snapshot.data_deletion_enabled = $true; Write-FixtureJson $snapshotFile $snapshot
    $bad = $common.Clone(); $bad.ExpectedEnvironmentSnapshotSHA256 = File-Hash $snapshotFile
    Expect-Failure { & $subject -Mode Initialize @bad -ReleaseID "fixture-release" -Owner "fixture-owner" } "must remain disabled"
    $snapshot.data_deletion_enabled = $false; Write-FixtureJson $snapshotFile $snapshot
    $snapshot.token_revocation_supported = $true; Write-FixtureJson $snapshotFile $snapshot
    $bad = $common.Clone(); $bad.ExpectedEnvironmentSnapshotSHA256 = File-Hash $snapshotFile
    Expect-Failure { & $subject -Mode Initialize @bad -ReleaseID "fixture-release" -Owner "fixture-owner" } "must remain disabled"
    $snapshot.token_revocation_supported = $false; Write-FixtureJson $snapshotFile $snapshot
    $firstEnv = [string]$catalog.include[0].image_env
    $savedImage = $snapshot.images[$firstEnv]; $snapshot.images[$firstEnv] = "registry.invalid/other@sha256:$("b" * 64)"
    Write-FixtureJson $snapshotFile $snapshot
    $bad = $common.Clone(); $bad.ExpectedEnvironmentSnapshotSHA256 = File-Hash $snapshotFile
    Expect-Failure { & $subject -Mode Initialize @bad -ReleaseID "fixture-release" -Owner "fixture-owner" } "deployed image differs"
    $snapshot.images[$firstEnv] = $savedImage; Write-FixtureJson $snapshotFile $snapshot
    & $subject -Mode Initialize @common -ReleaseID "fixture-release" -Owner "fixture-owner" | Out-Null
    & $subject -Mode Verify @common | Out-Null
    Expect-Failure { & $subject -Mode Verify @common -RequireReleaseReady } "not release-ready"
    $writer = [IO.File]::Open($recordFile + ".lock", [IO.FileMode]::OpenOrCreate, [IO.FileAccess]::ReadWrite, [IO.FileShare]::None)
    try { Expect-Failure { & $subject -Mode SetCheck @common -CheckID "l5.auth_session" -CheckStatus blocked -Reason "fixture dependency unavailable" } "locked by another writer" }
    finally { $writer.Dispose() }
    $bad = $common.Clone(); $bad.Remove("ExpectedEnvironmentSnapshotSHA256")
    Expect-Failure { & $subject -Mode Verify @bad } "invalid acceptance environment"
    Expect-Failure { & $subject -Mode SetCheck @common -EvidenceRoot $evidenceRoot -CheckID "l5.auth_session" -CheckStatus passed -Evidence "missing.txt" } "does not exist"
    $rawFile = Join-Path $evidenceRoot "raw.txt"
    [IO.File]::WriteAllText($rawFile, "synthetic fixture only; no live services accessed")
    $raw = [ordered]@{ path = "raw.txt"; bytes = (Get-Item -LiteralPath $rawFile).Length; sha256 = File-Hash $rawFile }
    Expect-Failure { & $subject -Mode SetCheck @common -EvidenceRoot $evidenceRoot -CheckID "l5.auth_session" -CheckStatus passed -Evidence "raw.txt" } "require a live observation"
    $observation = [ordered]@{ schema = "twitter-clone.deployment-observation.v1"; execution_mode = "live"; check_id = "l5.auth_session"; status = "passed"
        source_sha = $source; release_manifest_sha256 = $manifestHash; deployment_id = "fixture-deployment"; environment = "staging"
        environment_snapshot_sha256 = $common.ExpectedEnvironmentSnapshotSHA256; actor_ref = "fixture-actor"; observed_at_utc = [DateTime]::UtcNow.ToString("o"); evidence = @($raw) }
    $observationFile = Join-Path $evidenceRoot "observation.json"
    foreach ($change in @(
        @{ field = "check_id"; value = "l5.search_visibility"; error = "check identity" },
        @{ field = "execution_mode"; value = "offline"; error = "check identity" },
        @{ field = "deployment_id"; value = "other-deployment"; error = "binding mismatch" },
        @{ field = "source_sha"; value = ("2" * 40); error = "binding mismatch" },
        @{ field = "environment"; value = "production"; error = "binding mismatch" },
        @{ field = "release_manifest_sha256"; value = ("2" * 64); error = "binding mismatch" },
        @{ field = "environment_snapshot_sha256"; value = ("2" * 64); error = "binding mismatch" },
        @{ field = "observed_at_utc"; value = [DateTime]::UtcNow.AddHours(-25).ToString("o"); error = "stale or invalid" }
    )) {
        $saved = $observation[$change.field]; $observation[$change.field] = $change.value; Write-FixtureJson $observationFile $observation
        Expect-Failure { & $subject -Mode SetCheck @common -EvidenceRoot $evidenceRoot -CheckID "l5.auth_session" -CheckStatus passed -Evidence "raw.txt" -ObservationFile "observation.json" } $change.error
        $observation[$change.field] = $saved
    }
    $raw.sha256 = "2" * 64; Write-FixtureJson $observationFile $observation
    Expect-Failure { & $subject -Mode SetCheck @common -EvidenceRoot $evidenceRoot -CheckID "l5.auth_session" -CheckStatus passed -Evidence "raw.txt" -ObservationFile "observation.json" } "evidence digest mismatch"
    $raw.sha256 = File-Hash $rawFile
    Write-FixtureJson $observationFile $observation
    $goodObservation = [IO.File]::ReadAllText($observationFile)
    [IO.File]::WriteAllText($observationFile, $goodObservation.Replace('"execution_mode": "live"', '"execution_mode": "live", "execution_mode": "offline"'))
    Expect-Failure { & $subject -Mode SetCheck @common -EvidenceRoot $evidenceRoot -CheckID "l5.auth_session" -CheckStatus passed -Evidence "raw.txt" -ObservationFile "observation.json" } "duplicate acceptance JSON key"
    $plan = Get-Content -LiteralPath $planFile -Raw | ConvertFrom-Json
    foreach ($check in $plan.checks) {
        $observation.check_id = [string]$check.id
        $relative = "$($check.id).json"; Write-FixtureJson (Join-Path $evidenceRoot $relative) $observation
        & $subject -Mode SetCheck @common -EvidenceRoot $evidenceRoot -CheckID ([string]$check.id) -CheckStatus passed -Evidence "raw.txt" -ObservationFile $relative | Out-Null
    }
    $output = & $subject -Mode Verify @common -EvidenceRoot $evidenceRoot -RequireReleaseReady | Out-String
    if ($output -notmatch "required=31 passed=31" -or $output -notmatch "release_ready=true" -or $output -notmatch "deployment_accepted=false") { throw "bound fixture gate failed" }
    $signedRecord = [IO.File]::ReadAllText($recordFile)
    [IO.File]::WriteAllText($recordFile, $signedRecord.Replace('"release_binding": {', '"release_binding": { "signature": "unsigned-injection",'))
    Expect-Failure { & $subject -Mode Verify @common -EvidenceRoot $evidenceRoot } "signature mismatch"
    [IO.File]::WriteAllText($recordFile, $signedRecord)
    $bad = $common.Clone(); $bad.ExpectedReleaseManifestSHA256 = "2" * 64
    Expect-Failure { & $subject -Mode Verify @bad -EvidenceRoot $evidenceRoot } "digest mismatch"
    $changedObservation = Join-Path $evidenceRoot "l5.auth_session.json"
    [IO.File]::AppendAllText($changedObservation, " ")
    Expect-Failure { & $subject -Mode Verify @common -EvidenceRoot $evidenceRoot } "observation file digest mismatch"
    $observation.check_id = "l5.auth_session"; Write-FixtureJson $changedObservation $observation
    [IO.File]::AppendAllText($rawFile, " changed")
    Expect-Failure { & $subject -Mode Verify @common -EvidenceRoot $evidenceRoot } "evidence size mismatch|evidence hash mismatch"
    Write-Output "deployment_evidence_self_test=PASS checks=31 negative_cases=$cases synthetic_only=true"
} finally {
    if (Test-Path -LiteralPath $fixtureRoot) {
        $resolved = [IO.Path]::GetFullPath($fixtureRoot)
        $allowed = [IO.Path]::GetFullPath((Join-Path $repositoryRoot ".codex-tmp")) + [IO.Path]::DirectorySeparatorChar
        if (-not $resolved.StartsWith($allowed, [StringComparison]::OrdinalIgnoreCase)) { throw "fixture cleanup escaped workspace" }
        Remove-Item -LiteralPath $resolved -Recurse -Force
    }
}
