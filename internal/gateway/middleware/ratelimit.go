package middleware

import (
	"fmt"
	"net/http"
	"os"
	"time"

	consts "twitter-clone/internal/gateway/internal/consts"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/joho/godotenv"
)

// RateLimitMiddlewareFixedWindow creates the middleware with default Lua script for atomicity
func RateLimitMiddlewareFixedWindow(rdb *redis.Client, limit int, window time.Duration) gin.HandlerFunc {
	// Lua script for Fixed Window counter
	// KEYS[1]: key
	// ARGV[1]: limit
	// ARGV[2]: window (seconds)
	// Returns: 1 if allowed, 0 if blocked
	script := `
		local key = KEYS[1]
		local limit = tonumber(ARGV[1])
		local window = tonumber(ARGV[2])

		local current = redis.call("INCR", key)
		if current == 1 then
			redis.call("EXPIRE", key, window)
		end

		if current > limit then
			return 0
		end
		return 1
	`

	return func(c *gin.Context) {
		// 🎯 压测环境且携带压测万能令牌，直接放行，避免限流拦截，把并发压力传导至 Sentinel 熔断层
		authHeader := c.GetHeader("Authorization")
		if err := godotenv.Load(); err != nil {
			fmt.Println("Error loading .env file:", err)
		}
		if os.Getenv("APP_ENV") == consts.TestAppEnv && authHeader == consts.TestToken {
			c.Next()
			return
		}

		ip := c.ClientIP()
		key := fmt.Sprintf("rate_limit:%s", ip)

		val, err := rdb.Eval(c, script, []string{key}, limit, int(window.Seconds())).Int()

		// 🛠️ DEBUG LOG: Print detailed info to console
		fmt.Printf("🔒 RATELIMIT DEBUG | IP: %s | Key: %s | Limit: %d | Window: %ds | Redis Result: %d | Err: %v\n",
			ip, key, limit, int(window.Seconds()), val, err)

		if err != nil {
			// Fail open
			fmt.Println("⚠️ RATELIMIT ERROR: Redis Eval failed, allowing request.")
			c.Next()
			return
		}

		if val == 0 {
			fmt.Println("🛑 RATELIMIT BLOCKED: Request denied for", ip)
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error":       "too many requests",
				"retry_after": int(window.Seconds()),
			})
			return
		}

		fmt.Println("✅ RATELIMIT ALLOWED: Request allowed for", ip)
		c.Next()
	}
}

// RateLimitMiddlewareSlidingWindow creates the middleware with default Lua script for atomicity
func RateLimitMiddlewareSlidingWindow(rdb *redis.Client, limit int, window time.Duration) gin.HandlerFunc {
	// Lua script for Sliding Window counter
	// KEYS[1]: key
	// ARGV[1]: limit
	// ARGV[2]: window (seconds)
	// Returns: 1 if allowed, 0 if blocked
	script := `
		local key = KEYS[1]
		local limit = tonumber(ARGV[1])
		local window = tonumber(ARGV[2])
		local now = tonumber(ARGV[3])
		local min_score = now - window  -- 窗口开始时间

		-- 清理过期记录
		redis.call("ZREMRANGEBYSCORE", key, 0, min_score)

		-- 统计窗口内请求数
		local current = redis.call("ZCARD", key)

		if current >= limit then
			return 0
		end

		-- 插入本次请求（score=时间戳, member=时间戳+随机数保证唯一）
		redis.call("ZADD", key, now, now .. "-" .. math.random(100000))

		-- 设置key过期（避免内存泄漏）
		redis.call("EXPIRE", key, window)

		return 1
	`

	return func(c *gin.Context) {
		// 🎯 压测环境且携带压测万能令牌，直接放行，避免限流拦截，把并发压力传导至 Sentinel 熔断层
		authHeader := c.GetHeader("Authorization")
		if err := godotenv.Load(); err != nil {
			fmt.Println("Error loading .env file:", err)
		}
		if os.Getenv("APP_ENV") == consts.TestAppEnv && authHeader == consts.TestToken {
			c.Next()
			return
		}

		ip := c.ClientIP()
		key := fmt.Sprintf("rate_limit:%s", ip)

		now := time.Now().UnixNano() / int64(time.Millisecond)
		val, err := rdb.Eval(c, script, []string{key}, limit, window.Milliseconds(), now).Int()

		// 🛠️ DEBUG LOG: Print detailed info to console
		fmt.Printf("🔒 RATELIMIT DEBUG | IP: %s | Key: %s | Limit: %d | Window: %ds | Redis Result: %d | Err: %v\n",
			ip, key, limit, int(window.Seconds()), val, err)

		if err != nil {
			// Fail open
			fmt.Println("⚠️ RATELIMIT ERROR: Redis Eval failed, allowing request.")
			c.Next()
			return
		}

		if val == 0 {
			fmt.Println("🛑 RATELIMIT BLOCKED: Request denied for", ip)
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error":       "too many requests",
				"retry_after": int(window.Seconds()),
			})
			return
		}

		fmt.Println("✅ RATELIMIT ALLOWED: Request allowed for", ip)
		c.Next()
	}
}
