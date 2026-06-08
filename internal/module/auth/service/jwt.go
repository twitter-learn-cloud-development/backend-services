package service

import (
	"crypto/rsa"
	"fmt"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type JWTConfig struct {
	Expiration time.Duration
	Kid        string
	PrivateKey *rsa.PrivateKey
}

// DefaultJWTConfig 默认JWT配置
func DefaultJWTConfig() *JWTConfig {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		secret = "your secret key"
	}
	return &JWTConfig{
		Expiration: 7 * 24 * time.Hour,
	}
}

// UserCliaims 用户JWT Claims
type UserClaims struct {
	UserID   uint64 `json:"user_id"`
	Username string `json:"username"`
	Email    string `json:"email"`
	jwt.RegisteredClaims
}

// GenerateToken 生成JWT Token
func GenerateToken(config *JWTConfig, privateKey *rsa.PrivateKey, kid string, userID uint64, username string, email string) (string, error) {
	claims := &UserClaims{
		UserID:   userID,
		Username: username,
		Email:    email,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(config.Expiration)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
		},
	}

	//创建Token
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)

	//Header里明确指定kid编号
	token.Header["kid"] = kid

	//签名
	tokenString, err := token.SignedString(privateKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}

	return tokenString, nil

}
