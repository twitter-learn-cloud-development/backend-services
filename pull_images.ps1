# Images to pull
$images = @(
    "bitnami/mysql:latest", 
    "bitnami/rabbitmq:latest",
    "jaegertracing/all-in-one:latest",
    "bitnami/os-shell:latest" 
)

# Registry mirror
$mirror = "docker.m.daocloud.io"

foreach ($img in $images) {
    $mirrorImg = "$mirror/$img"
    $targetImg = "docker.io/$img"
    
    Write-Host "Pulling $mirrorImg..."
    docker pull $mirrorImg
    
    if ($?) {
        Write-Host "Tagging as $targetImg..."
        docker tag $mirrorImg $targetImg
        
        Write-Host "Cleaning up $mirrorImg..."
        docker rmi $mirrorImg
    } else {
        Write-Host "Failed to pull $mirrorImg" -ForegroundColor Red
    }
}

# Special handling for strict versions found in logs if 'latest' doesn't work
# But since we set version: "*" in Chart.yaml, it might be fetching latest stable
# The logs showed: docker.io/bitnami/mysql:9.4.0-debian-12-r1
# Let's try to pull that specific one too just in case
$specificImages = @(
    "bitnami/mysql:9.1.0-debian-11-r0",
    "bitnami/rabbitmq:3.11.0-debian-11-r0",
    "jaegertracing/all-in-one:1.37"
)

foreach ($img in $specificImages) {
    $mirrorImg = "$mirror/$img"
    $targetImg = "docker.io/$img"
    
     Write-Host "Pulling specific $mirrorImg..."
    docker pull $mirrorImg
    
    if ($?) {
        Write-Host "Tagging as $targetImg..."
        docker tag $mirrorImg $targetImg
    }
}
