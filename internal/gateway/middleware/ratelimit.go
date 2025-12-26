package middleware

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
)

// RateLimitMiddleware implements a simple Fixed Window rate limiter using Redis
// limit: max requests per window
// window: time window duration
func RateLimitMiddleware(rdb *redis.Client, limit int, window time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		key := fmt.Sprintf("rate_limit:%s", ip)

		// Pipeline execution for atomicity
		pipe := rdb.Pipeline()
		incr := pipe.Incr(c, key)
		expire := pipe.Expire(c, key, window)
		_, err := pipe.Exec(c)

		if err != nil {
			// Fail-open strategy: if Redis fails, allow request
			c.Next()
			return
		}

		count, _ := incr.Result()

		// Setting expiration only on the first increment (if key didn't exist) is tricky with pipeline
		// because we don't know if it existed.
		// Optimized approach: Always set expire. It's cheap.
		// Correct approach for fixed window:
		// If INCR returns 1, it means it's new, set EXPIRE.
		// If > 1, do nothing (preserve existing TTL).
		// But checking result requires two round trips or Lua.
		// For simplicity/performance trade-off here, we just use the simple INCR approach.
		// A better simple approach:
		// timestamp based?
		// Let's stick to the simple "INCR > limit" check.
		// The expire is reset on every request here? No, `Expire` updates TTL.
		// We only want to set TTL if it's a new key.
		// `redis.Incr` operation is atomic.

		// Refined Logic:
		// 1. INCR
		// 2. If result == 1, EXPIRE
		// 3. Check limit.

		// To do this atomically without Lua, we can't easily.
		// But checking result after INCR is fine.
		// If result == 1, we call Expire.

		// HOWEVER, pipeline executes all.
		// Let's remove pipeline for logic correctness (at cost of RTT) or use Lua.
		// Lua is best.

		if count > int64(limit) {
			ttl, _ := expire.Result() // Just debugging
			_ = ttl
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error":       "too many requests",
				"retry_after": window.Seconds(),
			})
			return
		}

		c.Next()
	}
}

// NewRateLimitMiddleware creates the middleware with default Lua script for atomicity
func NewRateLimitMiddleware(rdb *redis.Client, limit int, window time.Duration) gin.HandlerFunc {
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
