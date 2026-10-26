package service

import (
	"crypto/rand"
	"crypto/rsa"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestGenerateTokenIncludesProductionClaims(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	config := &JWTConfig{
		Expiration: time.Hour,
		Kid:        "kid-test",
		PrivateKey: key,
		Issuer:     "https://auth.example.com",
		Audience:   "twitter-clone",
	}
	encoded, err := GenerateToken(config, key, config.Kid, 42, "user", "user@example.com")
	if err != nil {
		t.Fatal(err)
	}
	claims := &UserClaims{}
	token, err := jwt.ParseWithClaims(encoded, claims, func(*jwt.Token) (interface{}, error) {
		return &key.PublicKey, nil
	}, jwt.WithIssuer(config.Issuer), jwt.WithAudience(config.Audience), jwt.WithExpirationRequired())
	if err != nil {
		t.Fatal(err)
	}
	if !token.Valid || claims.Issuer != config.Issuer || len(claims.Audience) != 1 || claims.Audience[0] != config.Audience {
		t.Fatalf("unexpected claims: issuer=%q audience=%v", claims.Issuer, claims.Audience)
	}
}
