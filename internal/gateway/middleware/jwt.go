package middleware

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// JWTClaims JWT 声明
type JWTClaims struct {
	UserID uint64 `json:"user_id"`
	jwt.RegisteredClaims
}

// JWTMiddleware JWT 认证中间件
type JWTMiddleware struct {
	secret string
	expire time.Duration
	mu     sync.RWMutex // 🔒 读写锁，保护 secret
}

// NewJWTMiddleware 创建 JWT 中间件
func NewJWTMiddleware(secret string, expire time.Duration) *JWTMiddleware {
	return &JWTMiddleware{
		secret: secret,
		expire: expire,
	}
}

// SetSecret 🔒 动态更新 Secret (热更新)
func (m *JWTMiddleware) SetSecret(newSecret string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.secret = newSecret
	// log.Println("🔐 JWT Secret updated successfully") // Optional log
}

// GetSecret 🔒 获取当前 Secret
func (m *JWTMiddleware) GetSecret() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.secret
}

// GenerateToken 生成 JWT Token
func (m *JWTMiddleware) GenerateToken(userID uint64) (string, error) {
	claims := JWTClaims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(m.expire)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	// 使用当前的 Secret 签名
	return token.SignedString([]byte(m.GetSecret()))
}

// AuthRequired 需要认证的中间件
func (m *JWTMiddleware) AuthRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 从 Header 中获取 Token
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "missing authorization header",
			})
			c.Abort()
			return
		}

		// 检查格式：Bearer <token>
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "invalid authorization header format",
			})
			c.Abort()
			return
		}

		tokenString := parts[1]

		// 解析 Token
		token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			// 🔒 获取当前的 Secret 用于验证
			return []byte(m.GetSecret()), nil
		})

		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": fmt.Sprintf("invalid token: %v", err),
			})
			c.Abort()
			return
		}

		if claims, ok := token.Claims.(*JWTClaims); ok && token.Valid {
			// 将 UserID 存入上下文
			c.Set("user_id", claims.UserID)
			c.Next()
		} else {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "invalid token claims",
			})
			c.Abort()
			return
		}
	}
}

// GetUserID 从上下文获取 UserID
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
