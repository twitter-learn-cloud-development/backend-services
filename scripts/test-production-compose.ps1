[CmdletBinding()]
param()

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$repositoryRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$fixtureRoot = Join-Path $repositoryRoot ('.codex-tmp/production-compose-' + [Guid]::NewGuid().ToString('N'))
$null = New-Item -ItemType Directory -Path $fixtureRoot
$subject = Join-Path $PSScriptRoot 'validate-production-compose.ps1'
$values = @{}
foreach ($rawLine in Get-Content -LiteralPath (Join-Path $repositoryRoot 'deploy/compose/production.env.example')) {
    if ($rawLine.StartsWith('#') -or [string]::IsNullOrWhiteSpace($rawLine)) { continue }
    $parts = $rawLine.Split('=', 2)
    $values[$parts[0]] = $parts[1]
}
foreach ($name in @($values.Keys)) {
    if ($name.EndsWith('_IMAGE')) {
        $values[$name] = 'registry.release.invalid/' + $name.ToLowerInvariant().Replace('_', '-') + '@sha256:' + ('1' * 64)
    } elseif ($name.EndsWith('_MASTER_KEY')) {
        $values[$name] = [Convert]::ToBase64String([Security.Cryptography.RandomNumberGenerator]::GetBytes(32))
    } elseif ($values[$name] -match 'REPLACE_WITH') {
        $values[$name] = [Convert]::ToHexString([Security.Cryptography.RandomNumberGenerator]::GetBytes(32)).ToLowerInvariant()
    }
}
$values['JWT_ISSUER'] = 'https://auth.release.invalid'
$values['MINIO_PUBLIC_URL'] = 'https://media.release.invalid'
$values['PUBLIC_BASE_URL'] = 'https://app.release.invalid'
$values['PUBLIC_HOST'] = 'app.release.invalid'
$values['GATEWAY_ALLOWED_ORIGINS'] = 'https://app.release.invalid'
$values['AGENT_LLM_BASE_URL'] = 'https://llm.release.invalid/v1'
$values['AGENT_PROCESS_ROLE'] = 'all'
$values['AGENT_SHARED_ADMISSION_ENABLED'] = 'false'

$rsa = [Security.Cryptography.RSA]::Create(2048)
try {
    $request = [Security.Cryptography.X509Certificates.CertificateRequest]::new('CN=app.release.invalid', $rsa, [Security.Cryptography.HashAlgorithmName]::SHA256, [Security.Cryptography.RSASignaturePadding]::Pkcs1)
    $certificate = $request.CreateSelfSigned([DateTimeOffset]::UtcNow.AddDays(-1), [DateTimeOffset]::UtcNow.AddDays(60))
    $values['TLS_CERTIFICATE_FILE'] = Join-Path $fixtureRoot 'fixture.crt'
    $values['TLS_PRIVATE_KEY_FILE'] = Join-Path $fixtureRoot 'fixture.key'
    [IO.File]::WriteAllText($values['TLS_CERTIFICATE_FILE'], $certificate.ExportCertificatePem())
    [IO.File]::WriteAllText($values['TLS_PRIVATE_KEY_FILE'], $rsa.ExportPkcs8PrivateKeyPem())
    $certificate.Dispose()
} finally { $rsa.Dispose() }
$values['ALERTMANAGER_WEBHOOK_TOKEN_FILE'] = Join-Path $fixtureRoot 'fixture-token'
[IO.File]::WriteAllText($values['ALERTMANAGER_WEBHOOK_TOKEN_FILE'], [Convert]::ToHexString([Security.Cryptography.RandomNumberGenerator]::GetBytes(32)))

function Invoke-Case {
    param([hashtable]$CaseValues, [string]$ExpectedFailure = '')
    $envPath = Join-Path $fixtureRoot ([Guid]::NewGuid().ToString('N') + '.env')
    $lines = @($CaseValues.Keys | Sort-Object | ForEach-Object { '{0}={1}' -f $_, $CaseValues[$_] })
    [IO.File]::WriteAllLines($envPath, [string[]]$lines, [Text.UTF8Encoding]::new($false))
    try {
        & $subject -EnvFile $envPath -AllowRepositoryEnvFile | Out-Null
        if ($ExpectedFailure) { throw 'fixture unexpectedly passed' }
    } catch {
        if (-not $ExpectedFailure -or $_.Exception.Message -notmatch $ExpectedFailure) { throw }
    }
}

# Explicit env files do not override process environment. Remove only Compose
# inputs for the fixture and restore them without displaying their values.
$originalEnvironment = @{}
$variableNames = @($values.Keys)
foreach ($path in @('docker-compose.yaml', 'docker-compose.production.yaml')) {
    $text = [IO.File]::ReadAllText((Join-Path $repositoryRoot $path))
    foreach ($match in [regex]::Matches($text, '\$\{([A-Z][A-Z0-9_]*)')) { $variableNames += $match.Groups[1].Value }
}
try {
    foreach ($name in @($variableNames | Sort-Object -Unique)) {
        $originalEnvironment[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
        Remove-Item -LiteralPath ('Env:' + $name) -ErrorAction SilentlyContinue
    }
    Invoke-Case $values
    $bad = $values.Clone(); $bad['GATEWAY_IMAGE'] = 'registry.release.invalid/gateway:latest'
    Invoke-Case $bad 'immutable|sha256'
    $bad = $values.Clone(); $bad['GATEWAY_IMAGE'] = 'registry.release.invalid/gateway@sha256:' + ('0' * 64)
    Invoke-Case $bad 'template digest'
    $bad = $values.Clone(); $bad['TWEET_SERVICE_DB_USER'] = $bad['USER_SERVICE_DB_USER']
    Invoke-Case $bad 'distinct'
    $bad = $values.Clone(); $bad['AGENT_SERVICE_DB_PASSWORD'] = ''
    Invoke-Case $bad 'AGENT_SERVICE_DB_PASSWORD'
    $worker = $values.Clone(); $worker['AGENT_PROCESS_ROLE'] = 'worker'; $worker['AGENT_SERVICE_DB_USER'] = ''; $worker['AGENT_SERVICE_DB_PASSWORD'] = ''
    Invoke-Case $worker
    $bad = $values.Clone(); $bad['AGENT_PROCESS_ROLE'] = 'unknown'
    Invoke-Case $bad 'AGENT_PROCESS_ROLE'
    $bad = $values.Clone(); $bad['AGENT_SHARED_ADMISSION_ENABLED'] = 'true'; $bad['AGENT_RUNTIME_V2_MODES'] = 'chat'
    Invoke-Case $bad 'Runtime v2'
    $bad = $values.Clone(); $bad['AGENT_PLANNING_EXECUTE_ENABLED'] = 'true'
    Invoke-Case $bad 'recoverable'
    'production_compose_fixture=PASS cases=9 live_dependencies=false'
} finally {
    foreach ($name in $originalEnvironment.Keys) { [Environment]::SetEnvironmentVariable($name, $originalEnvironment[$name], 'Process') }
}
