package handler

import (
	"testing"

	sentinel "github.com/alibaba/sentinel-golang/api"
	"github.com/alibaba/sentinel-golang/core/circuitbreaker"
	"github.com/alibaba/sentinel-golang/core/config"
)

func TestSelfHealer(t *testing.T) {
	// 1. 初始化最小配置 of Sentinel
	conf := config.NewDefaultConfig()
	conf.Sentinel.App.Name = "test-healer"
	_ = sentinel.InitWithConfig(conf) // 忽略可能重复初始化产生的 error

	// 2. 初始化 Base 规则
	baseRule := &circuitbreaker.Rule{
		Resource:         "grpc:test-base",
		Strategy:         circuitbreaker.ErrorRatio,
		RetryTimeoutMs:   3000,
		MinRequestAmount: 10,
		StatIntervalMs:   1000,
		Threshold:        0.5,
	}

	healer := NewSelfHealer([]*circuitbreaker.Rule{baseRule})

	// 用例 1: 注入不在白名单的资源 (如 GET:/api/v1/users/me 或 /*)
	healer.InjectCircuitBreaker("GET:/api/v1/users/me")
	rules := circuitbreaker.GetRules()
	
	// 检查该非法资源没有被加载到 Sentinel
	for _, r := range rules {
		if r.Resource == "GET:/api/v1/users/me" {
			t.Fatalf("security violation: resource not in allowlist was successfully injected")
		}
	}

	// 用例 2: 注入在白名单内的资源 (GET:/api/v1/feeds)
	healer.InjectCircuitBreaker("GET:/api/v1/feeds")
	rules = circuitbreaker.GetRules()

	// 检查白名单资源成功加载，并且保底规则 "grpc:test-base" 依然存在（没有被全量替换清空）
	foundBase := false
	foundDynamic := false
	for _, r := range rules {
		if r.Resource == "grpc:test-base" {
			foundBase = true
		}
		if r.Resource == "GET:/api/v1/feeds" {
			foundDynamic = true
		}
	}

	if !foundBase {
		t.Errorf("critical rule missing: baseRules were wiped out by LoadRules!")
	}
	if !foundDynamic {
		t.Errorf("failed to load dynamic rule: GET:/api/v1/feeds not found")
	}
}
