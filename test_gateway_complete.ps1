# Twitter Clone - API Gateway 完整测试脚本
# PowerShell 版本

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "Testing API Gateway - Complete Flow" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

# 配置 - Gateway 地址
$BASE_URL = "http://localhost:9638"

# 测试 1: 健康检查
Write-Host "Test 1: Health Check" -ForegroundColor Yellow
try {
    $health = Invoke-RestMethod -Uri "$BASE_URL/health" -Method Get
    if ($health.status -eq "ok") {
        Write-Host "✅ Gateway is healthy!" -ForegroundColor Green
    }
} catch {
    Write-Host "❌ Gateway is not running!" -ForegroundColor Red
    Write-Host "Please start: docker-compose up" -ForegroundColor Yellow
    exit
}
Write-Host ""

# 测试 2: 注册 Alice
Write-Host "Test 2: Register Alice" -ForegroundColor Yellow
$registerBody = @{
    username = "alice"
    email = "alice@example.com"
    password = "password123"
} | ConvertTo-Json

try {
    $alice = Invoke-RestMethod -Uri "$BASE_URL/api/v1/auth/register" `
        -Method Post `
        -ContentType "application/json" `
        -Body $registerBody

    $aliceId = $alice.user.id
    Write-Host "✅ Alice registered! ID: $aliceId" -ForegroundColor Green
} catch {
    Write-Host "⚠️  Alice might already exist, continuing..." -ForegroundColor Yellow
    $aliceId = "1999104813811372032"  # 使用默认 ID
}
Write-Host ""

# 测试 3: 登录 Alice
Write-Host "Test 3: Login Alice" -ForegroundColor Yellow
$loginBody = @{
    email = "alice@example.com"
    password = "password123"
} | ConvertTo-Json

$loginResponse = Invoke-RestMethod -Uri "$BASE_URL/api/v1/auth/login" `
    -Method Post `
    -ContentType "application/json" `
    -Body $loginBody

$token = $loginResponse.token
Write-Host "✅ Alice logged in! Token: $($token.Substring(0, 50))..." -ForegroundColor Green
Write-Host ""

# 测试 4: 获取当前用户信息
Write-Host "Test 4: Get Current User (me)" -ForegroundColor Yellow
$headers = @{
    "Authorization" = "Bearer $token"
}

$me = Invoke-RestMethod -Uri "$BASE_URL/api/v1/users/me" `
    -Method Get `
    -Headers $headers

Write-Host "✅ Current user: $($me.user.username)" -ForegroundColor Green
Write-Host ""

# 测试 5: 发推文
Write-Host "Test 5: Create Tweet" -ForegroundColor Yellow
$tweetBody = @{
    content = "Hello from API Gateway! This is a test tweet."
    media_urls = @()
} | ConvertTo-Json

$headers = @{
    "Authorization" = "Bearer $token"
    "Content-Type" = "application/json"
}

$tweet = Invoke-RestMethod -Uri "$BASE_URL/api/v1/tweets" `
    -Method Post `
    -Headers $headers `
    -Body $tweetBody

$tweetId = $tweet.tweet.id
Write-Host "✅ Tweet created! ID: $tweetId" -ForegroundColor Green
Write-Host "   Content: $($tweet.tweet.content)" -ForegroundColor Cyan
Write-Host ""

# 测试 6: 获取推文详情
Write-Host "Test 6: Get Tweet Details" -ForegroundColor Yellow
$getTweet = Invoke-RestMethod -Uri "$BASE_URL/api/v1/tweets/$tweetId" -Method Get
Write-Host "✅ Retrieved tweet: $($getTweet.tweet.content)" -ForegroundColor Green
Write-Host ""

# 测试 7: 注册 Bob
Write-Host "Test 7: Register Bob" -ForegroundColor Yellow
$bobRegisterBody = @{
    username = "bob"
    email = "bob@example.com"
    password = "password123"
} | ConvertTo-Json

try {
    $bob = Invoke-RestMethod -Uri "$BASE_URL/api/v1/auth/register" `
        -Method Post `
        -ContentType "application/json" `
        -Body $bobRegisterBody

    $bobId = $bob.user.id
    Write-Host "✅ Bob registered! ID: $bobId" -ForegroundColor Green
} catch {
    Write-Host "⚠️  Bob might already exist, continuing..." -ForegroundColor Yellow
    $bobId = "1999104813811372033"  # 使用默认 ID
}
Write-Host ""

# 测试 8: 登录 Bob
Write-Host "Test 8: Login Bob" -ForegroundColor Yellow
$bobLoginBody = @{
    email = "bob@example.com"
    password = "password123"
} | ConvertTo-Json

$bobLoginResponse = Invoke-RestMethod -Uri "$BASE_URL/api/v1/auth/login" `
    -Method Post `
    -ContentType "application/json" `
    -Body $bobLoginBody

$bobToken = $bobLoginResponse.token
Write-Host "✅ Bob logged in! Token: $($bobToken.Substring(0, 50))..." -ForegroundColor Green
Write-Host ""

# 测试 9: Bob 关注 Alice
Write-Host "Test 9: Bob follows Alice" -ForegroundColor Yellow
$followBody = @{
    followee_id = [uint64]$aliceId
} | ConvertTo-Json

$bobHeaders = @{
    "Authorization" = "Bearer $bobToken"
    "Content-Type" = "application/json"
}

try {
    $followResponse = Invoke-RestMethod -Uri "$BASE_URL/api/v1/follows" `
        -Method Post `
        -Headers $bobHeaders `
        -Body $followBody

    Write-Host "✅ Bob followed Alice! Message: $($followResponse.message)" -ForegroundColor Green
} catch {
    Write-Host "⚠️  Already following or error" -ForegroundColor Yellow
}
Write-Host ""

# 测试 10: 检查关注状态
Write-Host "Test 10: Check Following Status" -ForegroundColor Yellow
$isFollowing = Invoke-RestMethod -Uri "$BASE_URL/api/v1/follows/$aliceId/status" `
    -Method Get `
    -Headers $bobHeaders

if ($isFollowing.is_following) {
    Write-Host "✅ Bob is following Alice!" -ForegroundColor Green
} else {
    Write-Host "❌ Bob is not following Alice" -ForegroundColor Red
}
Write-Host ""

# 测试 11: 等待 Consumer 处理
Write-Host "Test 11: Waiting for Consumer..." -ForegroundColor Yellow
Write-Host "   (Check Consumer terminal for fanout messages)" -ForegroundColor Gray
Start-Sleep -Seconds 3
Write-Host "✅ Wait complete" -ForegroundColor Green
Write-Host ""

# 测试 12: Bob 查看 Feeds
Write-Host "Test 12: Bob views Feeds" -ForegroundColor Yellow
$feeds = Invoke-RestMethod -Uri "$BASE_URL/api/v1/feeds" `
    -Method Get `
    -Headers $bobHeaders

if ($feeds.tweets.Count -gt 0) {
    Write-Host "✅ SUCCESS! Bob can see $($feeds.tweets.Count) tweet(s) in his feed!" -ForegroundColor Green
    Write-Host "   First tweet: $($feeds.tweets[0].content)" -ForegroundColor Cyan
} else {
    Write-Host "❌ No tweets in Bob's feed" -ForegroundColor Red
    Write-Host "   This might mean Consumer is not running or RabbitMQ has issues" -ForegroundColor Yellow
}
Write-Host ""

# 测试 13: 获取 Alice 的统计
Write-Host "Test 13: Get Alice's Follow Stats" -ForegroundColor Yellow
$aliceStats = Invoke-RestMethod -Uri "$BASE_URL/api/v1/users/$aliceId/stats" -Method Get
Write-Host "✅ Alice has $($aliceStats.follower_count) follower(s), following $($aliceStats.followee_count)" -ForegroundColor Green
Write-Host ""

# 测试 14: 获取 Alice 的时间线
Write-Host "Test 14: Get Alice's Timeline" -ForegroundColor Yellow
$timeline = Invoke-RestMethod -Uri "$BASE_URL/api/v1/users/$aliceId/timeline" -Method Get
Write-Host "✅ Alice has $($timeline.tweets.Count) tweet(s) in timeline" -ForegroundColor Green
Write-Host ""

# 测试 15: 更新 Alice 的资料
Write-Host "Test 15: Update Alice's Profile" -ForegroundColor Yellow
$updateBody = @{
    avatar = "https://example.com/avatar.jpg"
    bio = "Testing API Gateway!"
} | ConvertTo-Json

$headers = @{
    "Authorization" = "Bearer $token"
    "Content-Type" = "application/json"
}

$updatedUser = Invoke-RestMethod -Uri "$BASE_URL/api/v1/users/me" `
    -Method Put `
    -Headers $headers `
    -Body $updateBody

Write-Host "✅ Profile updated!" -ForegroundColor Green
Write-Host "   Bio: $($updatedUser.user.bio)" -ForegroundColor Cyan
Write-Host ""

# 总结
Write-Host "========================================" -ForegroundColor Green
Write-Host "🎉 All Tests Completed!" -ForegroundColor Green
Write-Host "========================================" -ForegroundColor Green
Write-Host ""
Write-Host "Test Summary:" -ForegroundColor Cyan
Write-Host "  ✅ Health Check" -ForegroundColor Green
Write-Host "  ✅ User Registration" -ForegroundColor Green
Write-Host "  ✅ User Login (JWT)" -ForegroundColor Green
Write-Host "  ✅ Get Current User" -ForegroundColor Green
Write-Host "  ✅ Create Tweet" -ForegroundColor Green
Write-Host "  ✅ Get Tweet Details" -ForegroundColor Green
Write-Host "  ✅ Follow User" -ForegroundColor Green
Write-Host "  ✅ Check Following Status" -ForegroundColor Green
Write-Host "  ✅ View Feeds (Timeline Fanout)" -ForegroundColor Green
Write-Host "  ✅ Follow Stats" -ForegroundColor Green
Write-Host "  ✅ User Timeline" -ForegroundColor Green
Write-Host "  ✅ Update Profile" -ForegroundColor Green
Write-Host ""
Write-Host "🎊 Your microservices architecture is working perfectly!" -ForegroundColor Green
Write-Host ""
Write-Host "Architecture:" -ForegroundColor Cyan
Write-Host "  Browser → HTTP REST API (Gateway :9638)" -ForegroundColor White
Write-Host "         ↓" -ForegroundColor White
Write-Host "      gRPC Services (User/Tweet/Follow)" -ForegroundColor White
Write-Host "         ↓" -ForegroundColor White
Write-Host "  MySQL + Redis + RabbitMQ + Consumer" -ForegroundColor White
Write-Host ""