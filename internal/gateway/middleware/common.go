package middleware

import (
	"time"
	"twitter-clone/pkg/logger"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// CORS 跨域中间件
func CORS() gin.HandlerFunc {
	policy, err := OriginPolicyFromEnvironment()
	if err != nil {
		panic("invalid gateway origin policy: " + err.Error())
	}
	return CORSWithPolicy(policy)
}

func CORSWithPolicy(policy *OriginPolicy) gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" {
			if policy == nil || !policy.Allows(origin) {
				c.AbortWithStatus(403)
				return
			}
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Credentials", "true")
			c.Header("Vary", "Origin")
			c.Header("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, Accept, Origin, Cache-Control, X-Requested-With")
			c.Header("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE, PATCH")
		}

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}

// Logger 日志中间件
func Logger() gin.HandlerFunc {
	return func(c *gin.Context) {
		startTime := time.Now()

		// 处理请求
		c.Next()

		// 计算延迟
		latency := time.Since(startTime)

		// 获取状态码
		statusCode := c.Writer.Status()

		// 获取客户端 IP
		clientIP := c.ClientIP()

		// 获取请求方法和路径
		method := c.Request.Method
		path := c.Request.URL.Path

		// 打印结构化日志 (JSON + TraceID)
		logger.Info(c.Request.Context(), "Request",
			zap.Int("status", statusCode),
			zap.String("method", method),
			zap.String("path", path),
			zap.String("ip", clientIP),
			zap.Duration("latency", latency),
		)
	}
}

// ErrorHandler 统一错误处理
func ErrorHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		// 检查是否有错误
		if len(c.Errors) > 0 {
			err := c.Errors.Last()
			logger.Error(c.Request.Context(), "Request Error", zap.Error(err.Err))

			c.JSON(-1, gin.H{
				"error": err.Error(),
			})
		}
	}
}
