package handler

import (
	"context"
	"log"
	"sync"

	"github.com/alibaba/sentinel-golang/core/circuitbreaker"
	"twitter-clone/pkg/k8s"
)

type SelfHealer struct {
	mu           sync.RWMutex
	baseRules    []*circuitbreaker.Rule          // 系统自带的基础保底规则
	dynamicRules map[string]*circuitbreaker.Rule // AI 动态注入的规则
	allowList    map[string]bool                 // 🎯 绝对白名单，防止 AI 幻觉“杀”全站
	k8sClient    *k8s.K8sClient                  // 🎯 新增的 K8s client，用于灰度流控自愈
}

func NewSelfHealer(base []*circuitbreaker.Rule) *SelfHealer {
	k8sCli, err := k8s.NewK8sClient()
	if err != nil {
		log.Printf("⚠️  [SelfHealer] Failed to initialize K8s client: %v. Kubernetes self-healing features will be disabled.", err)
	}

	return &SelfHealer{
		baseRules:    base,
		dynamicRules: make(map[string]*circuitbreaker.Rule),
		allowList: map[string]bool{
			"GET:/api/v1/feeds": true, // 仅允许对特定的高压读接口熔断
		},
		k8sClient: k8sCli,
	}
}

// InjectCircuitBreaker 安全注入 AI 指定 of 熔断规则
func (s *SelfHealer) InjectCircuitBreaker(resource string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 1. 🎯 幻觉拦截：白名单强校验
	if !s.allowList[resource] {
		log.Printf("🚨 [SelfHealer Security Blocked] AI attempted to break unauthorized resource: %s", resource)
		return
	}

	// 2. 构造高频错误率熔断规则 (例如：错误率>50% 且 1秒内请求量>10 则熔断 10秒)
	newRule := &circuitbreaker.Rule{
		Resource:         resource,
		Strategy:         circuitbreaker.ErrorRatio,
		RetryTimeoutMs:   10000,
		MinRequestAmount: 10,
		StatIntervalMs:   1000,
		Threshold:        0.5,
	}

	s.dynamicRules[resource] = newRule

	// 3. 🎯 规则合并：防止 LoadRules 清空已有防线
	allRules := make([]*circuitbreaker.Rule, 0, len(s.baseRules)+len(s.dynamicRules))
	allRules = append(allRules, s.baseRules...)
	for _, r := range s.dynamicRules {
		allRules = append(allRules, r)
	}

	// 整体重载
	_, err := circuitbreaker.LoadRules(allRules)
	if err == nil {
		log.Printf("🛡️ [SelfHealer Activated] Dynamic circuit breaker applied to: %s", resource)
	} else {
		log.Printf("❌ [SelfHealer Failed] Failed to load rules: %v", err)
	}
}

// GetDefaultBaseRules 获取系统初始基础规则副本
func GetDefaultBaseRules() []*circuitbreaker.Rule {
	return []*circuitbreaker.Rule{
		{
			Resource:         "grpc:tweet-service",
			Strategy:         circuitbreaker.ErrorRatio,
			RetryTimeoutMs:   3000,
			MinRequestAmount: 10,
			StatIntervalMs:   1000,
			Threshold:        0.5,
		},
		{
			Resource:         "grpc:user-service",
			Strategy:         circuitbreaker.SlowRequestRatio,
			RetryTimeoutMs:   3000,
			MinRequestAmount: 10,
			StatIntervalMs:   1000,
			MaxAllowedRtMs:   500,
			Threshold:        0.5,
		},
	}
}

// GlobalSelfHealer 全局自愈实例
var GlobalSelfHealer = NewSelfHealer(GetDefaultBaseRules())

// UpdateVirtualServiceTraffic 安全地修改 Istio 灰度流量权重
func (s *SelfHealer) UpdateVirtualServiceTraffic(ctx context.Context, vsName string, v1Weight, v2Weight int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 1. 🎯 幻觉拦截：白名单强校验，只允许修改特定的虚拟服务权重
	if vsName != "tweet-service-vs" {
		log.Printf("🚨 [SelfHealer Security Blocked] AI attempted to modify unauthorized VirtualService: %s", vsName)
		return
	}

	if s.k8sClient == nil {
		log.Printf("⚠️ [SelfHealer] K8s dynamic client not initialized, skipping VS patch")
		return
	}

	// 2. 调用 K8sClient 执行带乐观锁重试的更新
	err := s.k8sClient.UpdateVirtualServiceTrafficWeight(ctx, vsName, v1Weight, v2Weight)
	if err != nil {
		log.Printf("❌ [SelfHealer Failed] Failed to update VirtualService %s: %v", vsName, err)
	} else {
		log.Printf("🛡️ [SelfHealer Success] Istio traffic weights successfully adjusted for %s (v1: %d, v2: %d)", vsName, v1Weight, v2Weight)
	}
}
