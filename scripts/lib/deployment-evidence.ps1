# Pure local validation shared by the signed acceptance record and its fixtures.
Set-StrictMode -Version Latest

function Assert-AcceptanceFile {
    param([string]$Path, [long]$MaximumBytes = 1MB)
    $absolute = [IO.Path]::GetFullPath($Path)
    $item = Get-Item -LiteralPath $absolute -Force
    if ($item.PSIsContainer -or $item.Length -le 0 -or $item.Length -gt $MaximumBytes) { throw "acceptance file is not bounded and nonempty" }
    $ancestor = $item
    while ($null -ne $ancestor) {
        if (($ancestor.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { throw "acceptance file reparse path rejected" }
        $ancestor = if ($ancestor -is [IO.FileInfo]) { $ancestor.Directory } else { $ancestor.Parent }
    }
    return $absolute
}

function Read-AcceptanceJson {
    param([string]$Path)
    $absolute = Assert-AcceptanceFile $Path
    $body = [IO.File]::ReadAllText($absolute)
    $options = [System.Text.Json.JsonDocumentOptions]::new()
    $options.MaxDepth = 32
    $document = [System.Text.Json.JsonDocument]::Parse($body, $options)
    function Assert-UniqueJsonKeys([System.Text.Json.JsonElement]$Node) {
        if ($Node.ValueKind -eq [System.Text.Json.JsonValueKind]::Object) {
            $seen = [Collections.Generic.HashSet[string]]::new([StringComparer]::OrdinalIgnoreCase)
            foreach ($entry in $Node.EnumerateObject()) {
                if (-not $seen.Add($entry.Name)) { throw "duplicate acceptance JSON key" }
                Assert-UniqueJsonKeys $entry.Value
            }
        } elseif ($Node.ValueKind -eq [System.Text.Json.JsonValueKind]::Array) {
            foreach ($entry in $Node.EnumerateArray()) { Assert-UniqueJsonKeys $entry }
        }
    }
    try { Assert-UniqueJsonKeys $document.RootElement } finally { $document.Dispose() }
    $arguments = @{ InputObject = $body; Depth = 32 }
    if ((Get-Command ConvertFrom-Json).Parameters.ContainsKey("DateKind")) { $arguments.DateKind = "String" }
    return ConvertFrom-Json @arguments
}

function Assert-AcceptanceFields {
    param([object]$Value, [string[]]$Names)
    if ($null -eq $Value -or $Value -isnot [pscustomobject]) { throw "acceptance JSON object required" }
    $actual = @($Value.PSObject.Properties.Name)
    if ($actual.Count -ne $Names.Count) { throw "acceptance JSON fields mismatch" }
    foreach ($name in $actual) { if ($name -cnotin $Names) { throw "unknown acceptance JSON field" } }
}

function Assert-AcceptanceHash {
    param([string]$Value, [string]$Name = "digest", [int]$Length = 64)
    if ($Value -cnotmatch "^[a-f0-9]{$Length}$" -or $Value -eq ("0" * $Length)) { throw "invalid acceptance $Name" }
}

function Assert-PinnedAcceptanceImage {
    param([string]$Value)
    if ($Value -cnotmatch '^[a-z0-9][a-z0-9._:/-]{0,255}@sha256:([a-f0-9]{64})$' -or $Matches[1] -eq ("0" * 64)) {
        throw "acceptance image must be immutable"
    }
}

function Get-DeploymentBinding {
    param([string]$ManifestFile, [string]$ExpectedManifestHash, [string]$ExpectedSource,
        [string]$SnapshotFile, [string]$ExpectedSnapshotHash, [string]$DeploymentEnvironment)
    Assert-AcceptanceHash $ExpectedManifestHash "release manifest digest"
    Assert-AcceptanceHash $ExpectedSnapshotHash "environment snapshot digest"
    Assert-AcceptanceHash $ExpectedSource "source SHA" 40
    $manifestPath = Assert-AcceptanceFile $ManifestFile
    $snapshotPath = Assert-AcceptanceFile $SnapshotFile
    if ((Get-FileHash -LiteralPath $manifestPath -Algorithm SHA256).Hash.ToLowerInvariant() -cne $ExpectedManifestHash -or
        (Get-FileHash -LiteralPath $snapshotPath -Algorithm SHA256).Hash.ToLowerInvariant() -cne $ExpectedSnapshotHash) { throw "reviewed deployment artifact digest mismatch" }
    $manifest = Read-AcceptanceJson $manifestPath
    Assert-AcceptanceFields $manifest @("version", "source_sha", "catalog_sha256", "images_published", "deployment_accepted", "images", "checks", "inputs", "image_environment")
    if ($manifest.version -ne 1 -or $manifest.source_sha -cne $ExpectedSource -or
        $manifest.images_published -isnot [bool] -or -not $manifest.images_published -or
        $manifest.deployment_accepted -isnot [bool] -or $manifest.deployment_accepted) { throw "candidate or invalid release manifest identity" }
    $catalogFile = Join-Path $PSScriptRoot "../../deploy/release/catalog.json"
    $catalog = Read-AcceptanceJson $catalogFile
    if ($manifest.catalog_sha256 -cne (Get-FileHash -LiteralPath $catalogFile -Algorithm SHA256).Hash.ToLowerInvariant()) { throw "release catalog digest mismatch" }
    $images = @($manifest.images)
    $environmentNames = @($manifest.image_environment.PSObject.Properties.Name)
    if ($images.Count -ne @($catalog.include).Count -or $environmentNames.Count -ne $images.Count) { throw "release image matrix mismatch" }
    for ($i = 0; $i -lt $images.Count; $i++) {
        $image = $images[$i]; $component = $catalog.include[$i]
        Assert-AcceptanceFields $image @("service", "source_sha", "catalog_sha256", "repository", "digest", "published", "builder_image", "runtime_image")
        if ($image.service -cne $component.service -or $image.source_sha -cne $ExpectedSource -or
            $image.catalog_sha256 -cne $manifest.catalog_sha256 -or $image.published -isnot [bool] -or -not $image.published -or
            $component.image_env -cnotin $environmentNames) { throw "release component identity mismatch" }
        $pinned = "{0}@{1}" -f $image.repository, $image.digest
        Assert-PinnedAcceptanceImage $pinned
        Assert-PinnedAcceptanceImage ([string]$image.builder_image)
        Assert-PinnedAcceptanceImage ([string]$image.runtime_image)
        if ($manifest.image_environment.($component.image_env) -cne $pinned) { throw "release image environment mismatch" }
    }
    $checkNames = @("go_test", "go_vet", "go_race", "package_build", "web_build", "web_test", "mobile_analyze", "mobile_test", "production_config")
    if (@($manifest.checks).Count -ne $checkNames.Count -or @($manifest.inputs).Count -eq 0 -or @($manifest.inputs).Count -gt 256) { throw "release input or check matrix mismatch" }
    for ($i = 0; $i -lt $checkNames.Count; $i++) {
        $check = $manifest.checks[$i]
        Assert-AcceptanceFields $check @("kind", "source_sha", "passed", "evidence_sha256")
        if ($check.kind -cne $checkNames[$i] -or $check.source_sha -cne $ExpectedSource -or $check.passed -isnot [bool] -or -not $check.passed) { throw "release check identity mismatch" }
        Assert-AcceptanceHash ([string]$check.evidence_sha256)
    }
    $snapshot = Read-AcceptanceJson $snapshotPath
    Assert-AcceptanceFields $snapshot @("schema", "deployment_id", "environment", "source_sha", "release_manifest_sha256", "configuration_revision", "secret_revision", "schema_revision", "previous_release_manifest_sha256", "images", "infrastructure_images", "data_deletion_enabled", "token_revocation_supported")
    if ($snapshot.schema -cne "twitter-clone.deployment-environment.v1" -or $snapshot.environment -cne $DeploymentEnvironment -or
        $snapshot.source_sha -cne $ExpectedSource -or $snapshot.release_manifest_sha256 -cne $ExpectedManifestHash) { throw "deployment environment identity mismatch" }
    foreach ($field in @("deployment_id", "environment", "configuration_revision", "secret_revision", "schema_revision")) { Assert-SafeIdentifier ([string]$snapshot.$field) $field }
    Assert-AcceptanceHash ([string]$snapshot.previous_release_manifest_sha256)
    foreach ($field in @("data_deletion_enabled", "token_revocation_supported")) {
        if ($snapshot.$field -isnot [bool] -or $snapshot.$field) { throw "unsupported deployment capability must remain disabled" }
    }
    Assert-AcceptanceFields $snapshot.images $environmentNames
    foreach ($name in $environmentNames) {
        if ($snapshot.images.$name -cne $manifest.image_environment.$name) { throw "deployed image differs from release manifest" }
    }
    $infrastructure = @($snapshot.infrastructure_images.PSObject.Properties)
    if ($infrastructure.Count -eq 0 -or $infrastructure.Count -gt 32) { throw "infrastructure image matrix is not bounded" }
    foreach ($image in $infrastructure) {
        Assert-SafeIdentifier $image.Name "infrastructure component"
        Assert-PinnedAcceptanceImage ([string]$image.Value)
    }
    return [pscustomobject][ordered]@{
        release_manifest_sha256 = $ExpectedManifestHash; source_sha = $ExpectedSource
        deployment_id = [string]$snapshot.deployment_id; environment = $DeploymentEnvironment
        environment_snapshot_sha256 = $ExpectedSnapshotHash
        previous_release_manifest_sha256 = [string]$snapshot.previous_release_manifest_sha256
    }
}

function Assert-DeploymentObservation {
    param([object]$Observation, [object]$Binding, [string]$Check, [object[]]$Evidence, [string]$CreatedAt)
    Assert-AcceptanceFields $Observation @("schema", "execution_mode", "check_id", "status", "source_sha", "release_manifest_sha256", "deployment_id", "environment", "environment_snapshot_sha256", "actor_ref", "observed_at_utc", "evidence")
    if ($Observation.schema -cne "twitter-clone.deployment-observation.v1" -or $Observation.execution_mode -cne "live" -or
        $Observation.check_id -cne $Check -or $Observation.status -cne "passed") { throw "observation check identity mismatch" }
    foreach ($name in @("source_sha", "release_manifest_sha256", "deployment_id", "environment", "environment_snapshot_sha256")) {
        if ($Observation.$name -cne $Binding.$name) { throw "observation deployment binding mismatch" }
    }
    Assert-SafeIdentifier ([string]$Observation.actor_ref) "observation actor"
    $observed = [DateTimeOffset]::MinValue; $created = [DateTimeOffset]::MinValue
    $style = [Globalization.DateTimeStyles]::RoundtripKind
    if (-not [DateTimeOffset]::TryParse([string]$Observation.observed_at_utc, [Globalization.CultureInfo]::InvariantCulture, $style, [ref]$observed) -or
        -not [DateTimeOffset]::TryParse($CreatedAt, [Globalization.CultureInfo]::InvariantCulture, $style, [ref]$created) -or
        $observed.Offset -ne [TimeSpan]::Zero -or $observed -lt $created.AddMinutes(-5) -or
        $observed -lt [DateTimeOffset]::UtcNow.AddHours(-24) -or $observed -gt [DateTimeOffset]::UtcNow.AddMinutes(5)) { throw "observation time is stale or invalid" }
    $items = @($Observation.evidence)
    if ($items.Count -ne $Evidence.Count -or $items.Count -eq 0) { throw "observation evidence matrix mismatch" }
    for ($i = 0; $i -lt $items.Count; $i++) {
        Assert-AcceptanceFields $items[$i] @("path", "bytes", "sha256")
        if ($items[$i].path -cne $Evidence[$i].path -or $items[$i].bytes -ne $Evidence[$i].bytes -or $items[$i].sha256 -cne $Evidence[$i].sha256) { throw "observation evidence digest mismatch" }
    }
}
