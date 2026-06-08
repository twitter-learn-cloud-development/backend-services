package auth

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"time"
	"twitter-clone/internal/module/auth/service"
)

// RawConsulConfig 用来映射 Consul 里最原始的字符串配置
type RawConsulConfig struct {
	Expiration string `mapstructure:"expiration"`
	Kid        string `mapstructure:"kid"`
	PrivateKey string `mapstructure:"private_key"`
}

// ProvideJWTConfig 通过 Wire 注入给 AuthService
func ProvideJWTConfig(raw *RawConsulConfig) (*service.JWTConfig, error) {
	// 1. 解析时间
	duration, err := time.ParseDuration(raw.Expiration)
	if err != nil {
		duration = 2 * time.Hour // 默认2小时
	}

	// 2. 🔴 关键步骤：把明文的 PEM 字符串解析为 *rsa.PrivateKey 对象
	block, _ := pem.Decode([]byte(raw.PrivateKey))
	if block == nil {
		return nil, errors.New("failed to parse PEM block containing the private key")
	}

	// 支持 PKCS#1 或 PKCS#8 格式的私钥解析
	privKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		// 如果不是 PKCS#1，尝试 PKCS#8
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("failed to parse rsa private key: %w", err)
		}
		var ok bool
		privKey, ok = key.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("not an rsa private key")
		}
	}

	// 3. 组装成最终的高级 JWTConfig 依赖返回给 Wire
	return &service.JWTConfig{
		Expiration: duration,
		Kid:        raw.Kid,
		PrivateKey: privKey,
	}, nil
}
