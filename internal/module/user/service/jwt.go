package service

import (
	"fmt"
	"github.com/golang-jwt/jwt/v5"
	"time"
)

type JWTConfig struct {
	Secret     []byte
	Expiration time.Duration
}

// DefaultJWTConfig 默认JWT配置
func DefaultJWTConfig() *JWTConfig {
	return &JWTConfig{
		Secret:     []byte("your secret key"),
		Expiration: 7 * 24 * time.Hour,
	}
}

// UserCliaims 用户JWT Claims
type UserClaims struct {
	UserID   uint64 `test_data:"user_id"`
	Username string `test_data:"username"`
	Email    string `test_data:"email"`
	jwt.RegisteredClaims
}

// GenerateToken 生成JWT Token
func GenerateToken(config *JWTConfig, userID uint64, username string, email string) (string, error) {
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
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	//签名
	tokenString, err := token.SignedString(config.Secret)
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}

	return tokenString, nil

}

// ParseToken解析JWT Token
func ParseToken(config *JWTConfig, tokenString string) (*UserClaims, error) {
	//解析Token
	token, err := jwt.ParseWithClaims(tokenString, &UserClaims{}, func(token *jwt.Token) (interface{}, error) {
		//验证签名方法
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return config.Secret, nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	//提取Claims
	if claims, ok := token.Claims.(*UserClaims); ok && token.Valid {
		return claims, nil
	}

	return nil, fmt.Errorf("invalid token")
}
