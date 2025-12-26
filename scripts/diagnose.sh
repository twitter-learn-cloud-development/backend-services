#!/bin/bash

echo "=========================================="
echo "🔍 Twitter Clone 诊断工具"
echo "=========================================="

# 配置
DB_USER="root"
DB_PASS="your_password"  # 改成你的密码
DB_NAME="twitter"
BASE_URL="http://localhost:8080/api/v1"

echo -e "\n1️⃣  检查数据库连接..."
mysql -u$DB_USER -p$DB_PASS -e "SELECT 'Database OK' as status;" 2>/dev/null
if [ $? -eq 0 ]; then
    echo "✅ 数据库连接正常"
else
    echo "❌ 数据库连接失败"
    exit 1
fi

echo -e "\n2️⃣  检查用户表数据..."
echo "现有用户："
mysql -u$DB_USER -p$DB_PASS $DB_NAME -e "SELECT id, username, email FROM users WHERE deleted_at = 0;" 2>/dev/null

USER_COUNT=$(mysql -u$DB_USER -p$DB_PASS $DB_NAME -se "SELECT COUNT(*) FROM users WHERE deleted_at = 0;" 2>/dev/null)
if [ "$USER_COUNT" -eq 0 ]; then
    echo "⚠️  警告：没有用户数据"
fi

echo -e "\n3️⃣  检查推文表数据..."
echo "现有推文："
mysql -u$DB_USER -p$DB_PASS $DB_NAME -e "SELECT id, user_id, LEFT(content, 50) as content FROM tweets WHERE deleted_at = 0 LIMIT 5;" 2>/dev/null

ZERO_USERID_COUNT=$(mysql -u$DB_USER -p$DB_PASS $DB_NAME -se "SELECT COUNT(*) FROM tweets WHERE user_id = 0;" 2>/dev/null)
if [ "$ZERO_USERID_COUNT" -gt 0 ]; then
    echo "❌ 发现 $ZERO_USERID_COUNT 条推文的 user_id 为 0！"
else
    echo "✅ 所有推文的 user_id 都正常"
fi

echo -e "\n4️⃣  测试 API 可用性..."
HEALTH=$(curl -s $BASE_URL/../health | jq -r '.status' 2>/dev/null)
if [ "$HEALTH" = "ok" ]; then
    echo "✅ API 服务正常"
else
    echo "❌ API 服务异常"
fi

echo -e "\n=========================================="
echo "诊断完成"
echo "=========================================="