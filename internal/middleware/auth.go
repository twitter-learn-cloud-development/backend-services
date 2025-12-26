package middleware

import (
	"github.com/gin-gonic/gin"
	"strings"
	"twitter-clone/internal/module/user/service"
)

// AuthMiddleware 认证中间件
func AuthMiddleware(jwtConfig *service.JWTConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		//1.从Header获取Token
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(401, gin.H{"error": "missing authorization header"})
			c.Abort()
		}

		//2.解析Bearer Token
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.JSON(401, gin.H{"error": "invalid authorization header format"})
			c.Abort()
			return
		}

		tokenString := parts[1]

		//3.验证Token
		claims, err := service.ParseToken(jwtConfig, tokenString)
		if err != nil {
			c.JSON(401, gin.H{"error": "invalid or expired token"})
			c.Abort()
			return
		}

		//4.将用户信息存为上下文
		c.Set("user_id", claims.UserID)
		c.Set("username", claims.Username)
		c.Set("email", claims.Email)

		//5.继续处理请求
		c.Next()
	}
}
