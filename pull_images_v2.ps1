# Updated mirror list
$mirrors = @(
    "docker.nju.edu.cn",
    "hub-mirror.c.163.com",
    "mirror.baidubce.com",
    "dockerproxy.com"
)

$images = @(
    "bitnami/mysql:9.1.0-debian-11-r0",
    "bitnami/rabbitmq:3.11.0-debian-11-r0"
    "bitnami/mysql:latest",
    "bitnami/rabbitmq:latest"
)

# Function to try pulling from mirrors
function Try-Pull {
    param ($imageName)
    
    foreach ($mirror in $mirrors) {
        $mirrorImg = "$mirror/$imageName"
        $targetImg = "docker.io/$imageName"
        
        Write-Host "Trying $mirror..." -ForegroundColor Cyan
        docker pull $mirrorImg
        
        if ($?) {
            Write-Host "Success! Tagging..." -ForegroundColor Green
            docker tag $mirrorImg $targetImg
            docker rmi $mirrorImg
            return $true
        }
    }
    return $false
}

foreach ($img in $images) {
    if (-not (Try-Pull $img)) {
        Write-Host "Failed to pull $img from all mirrors." -ForegroundColor Red
    }
}
