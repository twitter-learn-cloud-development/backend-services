# Twitter Clone - gRPC Services Test Script
# PowerShell Version with proper JSON escaping

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "Testing Twitter Clone - gRPC Services" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

# Check if services are running
Write-Host "Checking service status..." -ForegroundColor Yellow
Write-Host ""

$services = @(
    @{Name="User Service"; Port=9091},
    @{Name="Tweet Service"; Port=9092},
    @{Name="Follow Service"; Port=9093}
)

foreach ($service in $services) {
    $result = Test-NetConnection -ComputerName localhost -Port $service.Port -WarningAction SilentlyContinue
    if ($result.TcpTestSucceeded) {
        Write-Host "OK $($service.Name) (Port $($service.Port))" -ForegroundColor Green
    } else {
        Write-Host "X $($service.Name) (Port $($service.Port)) - NOT RUNNING" -ForegroundColor Red
    }
}

Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "Test 1: User Service - Register" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan

$registerJson = "{`"username`":`"alice`",`"email`":`"alice@example.com`",`"password`":`"password123`"}"
Write-Host "Request: $registerJson" -ForegroundColor Gray
Write-Host ""

$result = grpcurl -plaintext -d $registerJson localhost:9091 user.v1.UserService/Register 2>&1 | Out-String
Write-Host $result

# Extract user_id
if ($result -match '"id":\s*"(\d+)"') {
    $userId = $matches[1]
    Write-Host "OK User registered! User ID: $userId" -ForegroundColor Green
} else {
    Write-Host "WARNING: Cannot extract User ID, using default" -ForegroundColor Yellow
    $userId = "1234567890"
}

Write-Host ""
Read-Host "Press Enter to continue..."

Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "Test 2: User Service - Login" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan

$loginJson = "{`"email`":`"alice@example.com`",`"password`":`"password123`"}"
Write-Host "Request: $loginJson" -ForegroundColor Gray
Write-Host ""

$result = grpcurl -plaintext -d $loginJson localhost:9091 user.v1.UserService/Login 2>&1 | Out-String
Write-Host $result

Read-Host "Press Enter to continue..."

Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "Test 3: Tweet Service - Create Tweet" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan

$createTweetJson = "{`"user_id`":$userId,`"content`":`"Hello from gRPC!`",`"media_urls`":[]}"
Write-Host "Request: $createTweetJson" -ForegroundColor Gray
Write-Host ""

$result = grpcurl -plaintext -d $createTweetJson localhost:9092 tweet.v1.TweetService/CreateTweet 2>&1 | Out-String
Write-Host $result

# Extract tweet_id
if ($result -match '"id":\s*"(\d+)"') {
    $tweetId = $matches[1]
    Write-Host "OK Tweet created! Tweet ID: $tweetId" -ForegroundColor Green
} else {
    Write-Host "WARNING: Cannot extract Tweet ID, using default" -ForegroundColor Yellow
    $tweetId = "9876543210"
}

Write-Host ""
Read-Host "Press Enter to continue..."

Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "Test 4: Tweet Service - Get Tweet" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan

$getTweetJson = "{`"tweet_id`":$tweetId}"
Write-Host "Request: $getTweetJson" -ForegroundColor Gray
Write-Host ""

$result = grpcurl -plaintext -d $getTweetJson localhost:9092 tweet.v1.TweetService/GetTweet 2>&1 | Out-String
Write-Host $result

Read-Host "Press Enter to continue..."

Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "Test 5: Register second user (Bob)" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan

$registerBobJson = "{`"username`":`"bob`",`"email`":`"bob@example.com`",`"password`":`"password123`"}"
Write-Host "Request: $registerBobJson" -ForegroundColor Gray
Write-Host ""

$result = grpcurl -plaintext -d $registerBobJson localhost:9091 user.v1.UserService/Register 2>&1 | Out-String
Write-Host $result

if ($result -match '"id":\s*"(\d+)"') {
    $bobId = $matches[1]
    Write-Host "OK Bob registered! User ID: $bobId" -ForegroundColor Green
} else {
    Write-Host "WARNING: Cannot extract Bob User ID, using default" -ForegroundColor Yellow
    $bobId = "1234567891"
}

Write-Host ""
Read-Host "Press Enter to continue..."

Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "Test 6: Follow Service - Bob follows Alice" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan

$followJson = "{`"follower_id`":$bobId,`"followee_id`":$userId}"
Write-Host "Request: $followJson" -ForegroundColor Gray
Write-Host ""

$result = grpcurl -plaintext -d $followJson localhost:9093 follow.v1.FollowService/Follow 2>&1 | Out-String
Write-Host $result

Read-Host "Press Enter to continue..."

Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "Test 7: Follow Service - Check following status" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan

$isFollowingJson = "{`"follower_id`":$bobId,`"followee_id`":$userId}"
Write-Host "Request: $isFollowingJson" -ForegroundColor Gray
Write-Host ""

$result = grpcurl -plaintext -d $isFollowingJson localhost:9093 follow.v1.FollowService/IsFollowing 2>&1 | Out-String
Write-Host $result

Read-Host "Press Enter to continue..."

Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "Test 8: Tweet Service - Bob views Feeds" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan

Write-Host "Waiting 3 seconds for Consumer..." -ForegroundColor Yellow
Start-Sleep -Seconds 3

$getFeedsJson = "{`"user_id`":$bobId,`"cursor`":0,`"limit`":20}"
Write-Host "Request: $getFeedsJson" -ForegroundColor Gray
Write-Host ""

$result = grpcurl -plaintext -d $getFeedsJson localhost:9092 tweet.v1.TweetService/GetFeeds 2>&1 | Out-String
Write-Host $result

Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "Test 9: Follow Service - Get follow stats" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan

$getStatsJson = "{`"user_id`":$userId}"
Write-Host "Request: $getStatsJson" -ForegroundColor Gray
Write-Host ""

$result = grpcurl -plaintext -d $getStatsJson localhost:9093 follow.v1.FollowService/GetFollowStats 2>&1 | Out-String
Write-Host $result

Write-Host ""
Write-Host "========================================" -ForegroundColor Green
Write-Host "All tests completed!" -ForegroundColor Green
Write-Host "========================================" -ForegroundColor Green
Write-Host ""
Write-Host "Test Summary:" -ForegroundColor Cyan
Write-Host "  User Service: Register, Login tested" -ForegroundColor Green
Write-Host "  Tweet Service: Create, Get, Feeds tested" -ForegroundColor Green
Write-Host "  Follow Service: Follow, Check, Stats tested" -ForegroundColor Green
Write-Host ""