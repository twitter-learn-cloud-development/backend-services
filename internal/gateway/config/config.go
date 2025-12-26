package config

import (
	"os"
	"time"
)

type GatewayConfig struct {
	Port      string
	JWTSecret string
	JWTExpire time.Duration
	Consul    ConsulConfig
}

type ConsulConfig struct {
	Address string
}

func LoadGatewayConfig() *GatewayConfig {
	return &GatewayConfig{
		Port:      getEnv("GATEWAY_PORT", "8080"),
		JWTSecret: getEnv("JWT_SECRET", "your-secret-key-change-this-in-production"),
		JWTExpire: 24 * time.Hour,
		Consul: ConsulConfig{
			Address: getEnv("CONSUL_HOST", "localhost") + ":" + getEnv("CONSUL_PORT", "8500"),
		},
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
