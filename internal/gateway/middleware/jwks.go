package middleware

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"twitter-clone/internal/module/auth/service" // 引入引入刚才对齐的 UserClaims

	"github.com/MicahParks/keyfunc/v3"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

type GatewayAuthMiddleware struct {
	jwksKeyfunc keyfunc.Keyfunc // 内部自带缓存和异步更新的公钥管理器
}

func NewGatewayAuthMiddleware(authServiceJWKSUrl string) (*GatewayAuthMiddleware, error) {
	// 🔴 JWKS 会在底层自动处理公钥的热更新和动态拉取，你的 RWMutex 读写锁从此光荣退休！
	kf, err := keyfunc.NewDefault([]string{authServiceJWKSUrl})
	if err != nil {
		return nil, fmt.Errorf("failed to create JWKS keyfunc: %w", err)
	}
	return &GatewayAuthMiddleware{jwksKeyfunc: kf}, nil
}

// ParseTokenViaJWKS 核心的纯 JWKS 解密算法
func (m *GatewayAuthMiddleware) ParseTokenViaJWKS(tokenString string) (*service.UserClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &service.UserClaims{}, func(token *jwt.Token) (interface{}, error) {
		// 🔴 算法校验更替为非对称的 RS256
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		// 根据 kid 自动匹配获取内存缓存里的 *rsa.PublicKey
		return m.jwksKeyfunc.Keyfunc(token)
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*service.UserClaims); ok && token.Valid {
		return claims, nil
	}

	return nil, fmt.Errorf("invalid token claims")
}

// AuthRequired 🔴 替换你原先的强认证门禁（保留了混沌测试逻辑）
func (m *GatewayAuthMiddleware) AuthRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authorization header"})
			c.Abort()
			return
		}

		// 🎯 🎉 完美继承：你原先写得极漂亮的混沌压测虚拟环境隔离防护！
		if os.Getenv("APP_ENV") == "chaos_testing" && authHeader == "Bearer CHAOS_MOCK_UNIVERSAL_TOKEN_999" {
			c.Set("user_id", uint64(999999))
			c.Next()
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid authorization header format"})
			c.Abort()
			return
		}

		// 调用 JWKS 解析
		claims, err := m.ParseTokenViaJWKS(parts[1])
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": fmt.Sprintf("invalid token: %v", err)})
			c.Abort()
			return
		}

		// 统一写入上下文，以便业务层捞取
		c.Set("user_id", claims.UserID)
		c.Next()
	}
}

// AuthOptional 🔴 替换你原先的可选认证门禁（有 token 就解，没有就当游客静默放行）
func (m *GatewayAuthMiddleware) AuthOptional() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.Next()
			return
		}

		// 🎯 🎉 混沌压测万能 Token 同样在可选门禁中保留
		if os.Getenv("APP_ENV") == "chaos_testing" && authHeader == "Bearer CHAOS_MOCK_UNIVERSAL_TOKEN_999" {
			c.Set("user_id", uint64(999999))
			c.Next()
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.Next()
			return
		}

		claims, err := m.ParseTokenViaJWKS(parts[1])
		if err == nil {
			c.Set("user_id", claims.UserID)
		}
		c.Next()
	}
}

// GetUserID 🔴 完美保留此全局辅助函数，业务层捞取用户 ID 的习惯完全不需要改
func GetUserID(c *gin.Context) (uint64, bool) {
	userID, exists := c.Get("user_id")
	if !exists {
		return 0, false
	}
	if id, ok := userID.(uint64); ok {
		return id, true
	}
	return 0, false
}
