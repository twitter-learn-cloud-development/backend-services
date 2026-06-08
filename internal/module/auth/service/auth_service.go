package service

import (
	"crypto/rsa"
	"encoding/base64"
	"math/big"
)

// JWKey 代表单个公钥的 JWK 格式
type JWKey struct {
	Kty string `json:"kty"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// JWKSResponse 代表标准的 JWKS 返回体
type JWKSResponse struct {
	Keys []JWKey `json:"keys"`
}

// GetPublicJWKS 纯业务/数学逻辑：将私钥转为标准的 JWKS 结构体
func GetPublicJWKS(privateKey *rsa.PrivateKey, kid string) JWKSResponse {
	publicKey := &privateKey.PublicKey

	nStr := base64.RawURLEncoding.EncodeToString(publicKey.N.Bytes())
	eBytes := big.NewInt(int64(publicKey.E)).Bytes()
	eStr := base64.RawURLEncoding.EncodeToString(eBytes)

	return JWKSResponse{
		Keys: []JWKey{
			{
				Kty: "RSA",
				Use: "sig",
				Alg: "RS256",
				Kid: kid,
				N:   nStr,
				E:   eStr,
			},
		},
	}
}
