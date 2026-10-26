[CmdletBinding()]
param(
    [ValidateSet("status", "start", "stop", "logs")]
    [string]$Action = "status",
    [ValidateRange(1, 1000)]
    [int]$Tail = 100,
    [switch]$Follow
)

$ErrorActionPreference = "Stop"
$containerName = "twitter-clone-support-agent-1"

docker info --format '{{.ServerVersion}}' *> $null
if ($LASTEXITCODE -ne 0) {
    throw "Docker Desktop Linux Engine is not running."
}

docker container inspect $containerName --format '{{.Id}}' *> $null
if ($LASTEXITCODE -ne 0) {
    throw "The joint Live Support Agent container does not exist. Run scripts/run_live_acceptance.ps1 for first-time setup."
}

switch ($Action) {
    "status" {
        docker ps --all --filter "name=^/$containerName$" --format '{{.Names}} {{.Status}} {{.Ports}}'
    }
    "start" {
        foreach ($dependency in @("mongodb", "qdrant", "twitter-clone-auth-service-1", "twitter-clone-gateway-1")) {
            $state = docker container inspect $dependency --format '{{.State.Status}}' 2>$null
            if ($LASTEXITCODE -ne 0 -or $state -ne "running") {
                throw "Required dependency $dependency is not running. Start the joint stack before starting Support Agent."
            }
        }
        $state = docker container inspect $containerName --format '{{.State.Status}}'
        if ($state -eq "running") {
            Write-Output "$containerName is already running."
        }
        else {
            docker start $containerName
            if ($LASTEXITCODE -ne 0) {
                throw "Failed to start $containerName."
            }
        }
    }
    "stop" {
        $state = docker container inspect $containerName --format '{{.State.Status}}'
        if ($state -ne "running") {
            Write-Output "$containerName is already stopped."
        }
        else {
            docker stop --timeout 30 $containerName
            if ($LASTEXITCODE -ne 0) {
                throw "Failed to stop $containerName."
            }
        }
    }
    "logs" {
        $arguments = @("logs", "--tail", "$Tail")
        if ($Follow) {
            $arguments += "--follow"
        }
        $arguments += $containerName
        docker @arguments
        if ($LASTEXITCODE -ne 0) {
            throw "Failed to read $containerName logs."
        }
    }
}
