[CmdletBinding()]
param()

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$repositoryRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot ".."))
$fixtureRoot = Join-Path $repositoryRoot (".codex-tmp/production-acceptance-self-test-" + [Guid]::NewGuid().ToString("N"))
$evidenceRoot = Join-Path $fixtureRoot "evidence"
$recordFile = Join-Path $fixtureRoot "record.json"
$keyFile = Join-Path $fixtureRoot "key.bin"
$planFile = Join-Path $repositoryRoot "deploy/acceptance/production-launch-checks.json"
$fixturePlanFile = Join-Path $fixtureRoot "production-launch-checks.json"
$subject = Join-Path $PSScriptRoot "production-acceptance-record.ps1"

function Invoke-ExpectedFailure {
    param([scriptblock]$Action, [string]$Pattern, [string]$Name)

    try {
        & $Action | Out-Null
        throw "$Name unexpectedly succeeded"
    } catch {
        if ($_.Exception.Message -notmatch $Pattern) { throw }
    }
}

$common = @{
    RecordFile = $recordFile
    SigningKeyFile = $keyFile
    SigningKeyID = "self-test-key-v1"
    PlanFile = $fixturePlanFile
    AllowRepositoryFiles = $true
}

try {
    New-Item -ItemType Directory -Force -Path $evidenceRoot | Out-Null
    $key = New-Object byte[] 32
    [Security.Cryptography.RandomNumberGenerator]::Fill($key)
    [IO.File]::WriteAllBytes($keyFile, $key)
    Copy-Item -LiteralPath $planFile -Destination $fixturePlanFile
    $evidenceFile = Join-Path $evidenceRoot "offline-check.txt"
    [IO.File]::WriteAllText($evidenceFile, "offline acceptance self-test evidence", [Text.UTF8Encoding]::new($false))

    & $subject -Mode Initialize @common -ReleaseID "self-test-release" -Environment "staging" -Owner "self-test-owner" | Out-Null
    & $subject -Mode Verify @common | Out-Null

    Invoke-ExpectedFailure {
        & $subject -Mode Verify @common -RequireReleaseReady
    } "not release-ready" "incomplete release gate"

    Invoke-ExpectedFailure {
        & $subject -Mode SetCheck @common -CheckID "l4.compose_preflight" -CheckStatus passed
    } "require evidence" "evidence-less pass"

    $plan = Get-Content -LiteralPath $planFile -Raw | ConvertFrom-Json
    foreach ($check in @($plan.checks)) {
        & $subject -Mode SetCheck @common -EvidenceRoot $evidenceRoot -CheckID ([string]$check.id) -CheckStatus passed -Evidence "offline-check.txt" | Out-Null
    }
    $readyOutput = (& $subject -Mode Verify @common -EvidenceRoot $evidenceRoot | Out-String)
    $count = @($plan.checks).Count
    if ($readyOutput -notmatch "required=$count passed=$count" -or $readyOutput -notmatch "checks_complete=true release_bound=false" -or $readyOutput -notmatch "release_ready=false") {
        throw "legacy checks must remain readable without passing the bound release gate"
    }
    Invoke-ExpectedFailure { & $subject -Mode Verify @common -EvidenceRoot $evidenceRoot -RequireReleaseReady } "not release-ready" "unbound legacy release gate"

    $originalRecord = [IO.File]::ReadAllText($recordFile)
    [IO.File]::WriteAllText($recordFile, $originalRecord.Replace("self-test-release", "self-test-release-tampered"), [Text.UTF8Encoding]::new($false))
    Invoke-ExpectedFailure {
        & $subject -Mode Verify @common -EvidenceRoot $evidenceRoot
    } "signature mismatch" "record tamper check"
    [IO.File]::WriteAllText($recordFile, $originalRecord, [Text.UTF8Encoding]::new($false))

    $originalPlan = [IO.File]::ReadAllText($fixturePlanFile)
    [IO.File]::WriteAllText($fixturePlanFile, $originalPlan.Replace("MCP protocol conformance completed", "MCP protocol conformance completed tampered"), [Text.UTF8Encoding]::new($false))
    Invoke-ExpectedFailure {
        & $subject -Mode Verify @common -EvidenceRoot $evidenceRoot
    } "plan digest mismatch" "plan tamper check"
    [IO.File]::WriteAllText($fixturePlanFile, $originalPlan, [Text.UTF8Encoding]::new($false))

    [IO.File]::AppendAllText($evidenceFile, "tampered")
    Invoke-ExpectedFailure {
        & $subject -Mode Verify @common -EvidenceRoot $evidenceRoot
    } "size mismatch|hash mismatch" "evidence tamper check"

    Write-Output "production_acceptance_self_test=PASS checks=$count legacy_release_ready=false"
} finally {
    if (Test-Path -LiteralPath $fixtureRoot) {
        $resolved = [IO.Path]::GetFullPath($fixtureRoot)
        $temporaryRoot = [IO.Path]::GetFullPath((Join-Path $repositoryRoot ".codex-tmp"))
        if (-not $resolved.StartsWith($temporaryRoot.TrimEnd([IO.Path]::DirectorySeparatorChar) + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) {
            throw "self-test cleanup path escaped .codex-tmp"
        }
        Remove-Item -LiteralPath $resolved -Recurse -Force
    }
}
