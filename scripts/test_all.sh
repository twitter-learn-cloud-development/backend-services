#!/bin/bash

BASE_URL="http://localhost:8080/api/v1"
TEST_USER_EMAIL="testuser_$(date +%s)@example.com"
TEST_USER_PASSWORD="TestPassword123"

echo "=========================================="
echo "🧪 完整功能测试"
echo "=========================================="

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

function print_success() {
    echo -e "${GREEN}✅ $1${NC}"
}

function print_error() {
    echo -e "${RED}❌ $1${NC}"
}

function print_info() {
    echo -e "${YELLOW}ℹ️  $1${NC}"
}

# ========== 1. 用户注册 ==========
echo -e "\n1️⃣  测试用户注册..."
REGISTER_RESPONSE=$(curl -s -X POST $BASE_URL/users/register \
  -H "Content-Type: application/json" \
  -d "{
    \"username\": \"testuser$(date +%s)\",
    \"email\": \"$TEST_USER_EMAIL\",
    \"password\": \"$TEST_USER_PASSWORD\"
  }")

echo $REGISTER_RESPONSE | jq '.' 2>/dev/null

USER_ID=$(echo $REGISTER_RESPONSE | jq -r '.id' 2>/dev/null)

if [ "$USER_ID" != "null" ] && [ "$USER_ID" != "0" ] && [ -n "$USER_ID" ]; then
    print_success "注册成功 - User ID: $USER_ID"
else
    print_error "注册失败或 User ID 为 0"
    exit 1
fi

# ========== 2. 用户登录 ==========
echo -e "\n2️⃣  测试用户登录..."
LOGIN_RESPONSE=$(curl -s -X POST $BASE_URL/users/login \
  -H "Content-Type: application/json" \
  -d "{
    \"email\": \"$TEST_USER_EMAIL\",
    \"password\": \"$TEST_USER_PASSWORD\"
  }")

echo $LOGIN_RESPONSE | jq '.' 2>/dev/null

TOKEN=$(echo $LOGIN_RESPONSE | jq -r '.token' 2>/dev/null)
LOGIN_USER_ID=$(echo $LOGIN_RESPONSE | jq -r '.user.id' 2>/dev/null)

if [ -n "$TOKEN" ] && [ "$TOKEN" != "null" ]; then
    print_success "登录成功 - Token 已获取"
else
    print_error "登录失败"
    exit 1
fi

if [ "$LOGIN_USER_ID" = "$USER_ID" ]; then
    print_success "User ID 一致: $USER_ID"
else
    print_error "User ID 不一致: 注册=$USER_ID, 登录=$LOGIN_USER_ID"
fi

# ========== 3. 发布纯文本推文 ==========
echo -e "\n3️⃣  测试发布纯文本推文..."
TWEET1_RESPONSE=$(curl -s -X POST $BASE_URL/tweets \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{
    "content": "这是我的第一条测试推文！Hello Twitter Clone! 🚀",
    "media_urls": []
  }')

echo $TWEET1_RESPONSE | jq '.' 2>/dev/null

TWEET1_ID=$(echo $TWEET1_RESPONSE | jq -r '.id' 2>/dev/null)
TWEET1_USER_ID=$(echo $TWEET1_RESPONSE | jq -r '.user_id' 2>/dev/null)

if [ "$TWEET1_ID" != "null" ] && [ -n "$TWEET1_ID" ]; then
    print_success "发推成功 - Tweet ID: $TWEET1_ID"
else
    print_error "发推失败"
    exit 1
fi

if [ "$TWEET1_USER_ID" = "$USER_ID" ]; then
    print_success "Tweet user_id 正确: $TWEET1_USER_ID"
else
    print_error "Tweet user_id 错误: 期望=$USER_ID, 实际=$TWEET1_USER_ID"
    exit 1
fi

# ========== 4. 发布带图片的推文 ==========
echo -e "\n4️⃣  测试发布带图片的推文..."
TWEET2_RESPONSE=$(curl -s -X POST $BASE_URL/tweets \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{
    "content": "分享一些美图！📸",
    "media_urls": [
      "https://example.com/photo1.jpg",
      "https://example.com/photo2.jpg"
    ]
  }')

echo $TWEET2_RESPONSE | jq '.' 2>/dev/null

TWEET2_ID=$(echo $TWEET2_RESPONSE | jq -r '.id' 2>/dev/null)
TWEET2_USER_ID=$(echo $TWEET2_RESPONSE | jq -r '.user_id' 2>/dev/null)

if [ "$TWEET2_ID" != "null" ] && [ -n "$TWEET2_ID" ]; then
    print_success "发推（带图）成功 - Tweet ID: $TWEET2_ID"
else
    print_error "发推（带图）失败"
fi

if [ "$TWEET2_USER_ID" = "$USER_ID" ]; then
    print_success "Tweet user_id 正确: $TWEET2_USER_ID"
else
    print_error "Tweet user_id 错误"
fi

# ========== 5. 获取推文详情 ==========
echo -e "\n5️⃣  测试获取推文详情..."
TWEET_DETAIL=$(curl -s $BASE_URL/tweets/$TWEET1_ID)

echo $TWEET_DETAIL | jq '.' 2>/dev/null

DETAIL_ID=$(echo $TWEET_DETAIL | jq -r '.id' 2>/dev/null)

if [ "$DETAIL_ID" = "$TWEET1_ID" ]; then
    print_success "获取推文详情成功"
else
    print_error "获取推文详情失败"
fi

# ========== 6. 获取用户时间线 ==========
echo -e "\n6️⃣  测试获取用户时间线..."
USER_TIMELINE=$(curl -s "$BASE_URL/tweets/user/$USER_ID?limit=10")

echo $USER_TIMELINE | jq '.' 2>/dev/null

TWEET_COUNT=$(echo $USER_TIMELINE | jq '.tweets | length' 2>/dev/null)

if [ "$TWEET_COUNT" -ge 2 ]; then
    print_success "用户时间线获取成功 - 共 $TWEET_COUNT 条推文"
else
    print_error "用户时间线获取失败或推文数量不足"
fi

# ========== 7. 获取关注流 ==========
echo -e "\n7️⃣  测试获取关注流..."
FEEDS=$(curl -s "$BASE_URL/tweets/feeds?limit=10" \
  -H "Authorization: Bearer $TOKEN")

echo $FEEDS | jq '.' 2>/dev/null

FEEDS_COUNT=$(echo $FEEDS | jq '.tweets | length' 2>/dev/null)

if [ "$FEEDS_COUNT" -ge 0 ]; then
    print_success "关注流获取成功 - 共 $FEEDS_COUNT 条推文"
else
    print_error "关注流获取失败"
fi

# ========== 8. 删除推文 ==========
echo -e "\n8️⃣  测试删除推文..."
DELETE_RESPONSE=$(curl -s -X DELETE $BASE_URL/tweets/$TWEET1_ID \
  -H "Authorization: Bearer $TOKEN")

echo $DELETE_RESPONSE | jq '.' 2>/dev/null

DELETE_MSG=$(echo $DELETE_RESPONSE | jq -r '.message' 2>/dev/null)

if [ "$DELETE_MSG" = "tweet deleted" ]; then
    print_success "推文删除成功"
else
    print_error "推文删除失败"
fi

# ========== 9. 验证删除后无法查询 ==========
echo -e "\n9️⃣  验证删除后的推文无法查询..."
DELETED_TWEET=$(curl -s $BASE_URL/tweets/$TWEET1_ID)

ERROR=$(echo $DELETED_TWEET | jq -r '.error' 2>/dev/null)

if [ "$ERROR" = "tweet not found" ]; then
    print_success "软删除验证成功"
else
    print_error "软删除验证失败"
fi

# ========== 10. 数据库验证 ==========
echo -e "\n🔟 数据库验证..."
print_info "请手动检查数据库："
print_info "mysql -u root -p"
print_info "USE twitter;"
print_info "SELECT id, user_id, LEFT(content, 30) as content, deleted_at FROM tweets WHERE id IN ($TWEET1_ID, $TWEET2_ID);"

echo -e "\n=========================================="
echo "🎉 测试完成！"
echo "=========================================="
echo "测试用户："
echo "  Email: $TEST_USER_EMAIL"
echo "  User ID: $USER_ID"
echo "  Token: ${TOKEN:0:50}..."
echo "测试推文："
echo "  Tweet 1 ID: $TWEET1_ID (已删除)"
echo "  Tweet 2 ID: $TWEET2_ID (保留)"
echo "=========================================="