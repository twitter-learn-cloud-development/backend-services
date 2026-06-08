package config

import (
	"fmt"
	"log"
	"os"
	"strconv"

	consts "twitter-clone/internal/gateway/internal/consts"

	sentinel "github.com/alibaba/sentinel-golang/api"
	"github.com/alibaba/sentinel-golang/core/circuitbreaker"
	"github.com/alibaba/sentinel-golang/core/config"
	"github.com/joho/godotenv"
)

// InitSentinel initializes Sentinel and loads circuit breaker rules
func InitSentinel() {
	if err := godotenv.Load(); err != nil {
		fmt.Println("Error loading .env file:", err)
	}
	// 1. Initialize Sentinel Configuration
	conf := config.NewDefaultConfig()
	// Set App Name (displayed in Dashboard)
	appName := getEnv("SENTINEL_APP_NAME", consts.AppName)
	conf.Sentinel.App.Name = appName
	// Set Log Dir
	logDir := getEnv("SENTINEL_LOG_DIR", consts.LogDir)
	conf.Sentinel.Log.Dir = logDir

	// Use Environment Variables for Transport Config (avoiding struct field issues)
	dashboardAddr := getEnv("SENTINEL_DASHBOARD_ADDR", consts.DashboardAddress)
	os.Setenv("SENTINEL_DASHBOARD_ADDR", dashboardAddr)
	transportPort := getEnv("SENTINEL_TRANSPORT_PORT", strconv.Itoa(consts.TransportPort))
	os.Setenv("SENTINEL_TRANSPORT_PORT", transportPort)
	// Also set App Name via Env for consistency
	os.Setenv("SENTINEL_APP_NAME", appName)
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
			Resource:         consts.ResourceTweetService,
			Strategy:         circuitbreaker.ErrorRatio,
			RetryTimeoutMs:   consts.TweetRetryTimeoutMs,
			MinRequestAmount: consts.TweetMinRequestAmount,
			StatIntervalMs:   consts.TweetStatIntervalMs,
			Threshold:        consts.TweetThreshold,
		},
		// Rule 2: Protect Gateway from User Service slow responses
		// Strategy: SlowRequestRatio (if 50% of requests > 500ms, break)
		&circuitbreaker.Rule{
			Resource:         consts.ResourceUserService,
			Strategy:         circuitbreaker.SlowRequestRatio,
			RetryTimeoutMs:   consts.UserRetryTimeoutMs,
			MinRequestAmount: consts.UserMinRequestAmount,
			StatIntervalMs:   consts.UserStatIntervalMs,
			MaxAllowedRtMs:   consts.UserMaxAllowedRtMs,
			Threshold:        consts.UserThreshold,
		},
	})

	if err != nil {
		log.Fatalf("❌ Failed to load Sentinel rules: %+v", err)
	}
	log.Println("✅ Sentinel rules loaded")
}
