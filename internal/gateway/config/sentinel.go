package config

import (
	"log"
	"os"

	sentinel "github.com/alibaba/sentinel-golang/api"
	"github.com/alibaba/sentinel-golang/core/circuitbreaker"
	"github.com/alibaba/sentinel-golang/core/config"
)

// InitSentinel initializes Sentinel and loads circuit breaker rules
func InitSentinel() {
	// 1. Initialize Sentinel Configuration
	conf := config.NewDefaultConfig()
	// Set App Name (displayed in Dashboard)
	// Set App Name (displayed in Dashboard)
	conf.Sentinel.App.Name = "gateway"
	// Set Log Dir
	conf.Sentinel.Log.Dir = "/tmp/sentinel/logs"

	// Use Environment Variables for Transport Config (avoiding struct field issues)
	os.Setenv("SENTINEL_DASHBOARD_ADDR", "sentinel:8080")
	os.Setenv("SENTINEL_TRANSPORT_PORT", "8719")
	// Also set App Name via Env for consistency
	os.Setenv("SENTINEL_APP_NAME", "gateway")
	err := sentinel.InitWithConfig(conf)
	if err != nil {
		log.Fatalf("❌ Failed to initialize Sentinel: %+v", err)
	}
	log.Println("✅ Sentinel initialized (Connected to Dashboard at sentinel:8080)")

	// 2. Load Circuit Breaker Rules
	loadRules()
}

func loadRules() {
	_, err := circuitbreaker.LoadRules([]*circuitbreaker.Rule{
		// Rule 1: Protect Gateway from Tweet Service failures
		// Strategy: ErrorRatio (if 50% of requests fail, break)
		&circuitbreaker.Rule{
			Resource:         "grpc:tweet-service",
			Strategy:         circuitbreaker.ErrorRatio,
			RetryTimeoutMs:   3000, // Wait 3s before retry (Half-Open)
			MinRequestAmount: 10,   // Min 10 requests to trigger
			StatIntervalMs:   1000, // 1s window
			Threshold:        0.5,  // 50% Error Rate
		},
		// Rule 2: Protect Gateway from User Service slow responses
		// Strategy: SlowRequestRatio (if 50% of requests > 500ms, break)
		&circuitbreaker.Rule{
			Resource:         "grpc:user-service",
			Strategy:         circuitbreaker.SlowRequestRatio,
			RetryTimeoutMs:   3000,
			MinRequestAmount: 10,
			StatIntervalMs:   1000,
			MaxAllowedRtMs:   500, // Max 500ms allowed
			Threshold:        0.5, // 50% Slow Requests
		},
	})

	if err != nil {
		log.Fatalf("❌ Failed to load Sentinel rules: %+v", err)
	}
	log.Println("✅ Sentinel rules loaded")
}
