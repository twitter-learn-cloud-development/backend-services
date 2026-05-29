package config

import (
	"encoding/json"
	"fmt"
	"sync/atomic"
)

// DynamicCacheConfig 复合配置结构体
type DynamicCacheConfig struct {
	L1CacheTTLSeconds int `json:"l1_cache_ttl_seconds"`
	L2CacheTTLSeconds int `json:"l2_cache_ttl_seconds"`
	PreloadDepth      int `json:"preload_depth"`
}

// 🎯 全局原子指针，确保千万并发下读取配置的绝对安全且无锁开销
var globalCacheConfig atomic.Pointer[DynamicCacheConfig]

// init 提供一个极度保守的默认降级配置
func init() {
	defaultConfig := &DynamicCacheConfig{
		L1CacheTTLSeconds: 5,
		L2CacheTTLSeconds: 300,
		PreloadDepth:      2,
	}
	globalCacheConfig.Store(defaultConfig)
}

// GetCurrentConfig 获取当前生效的配置快照 (微秒级)
func GetCurrentConfig() *DynamicCacheConfig {
	return globalCacheConfig.Load()
}

// ReloadConfig 热重载校验与原子替换 (Agent 护栏)
func ReloadConfig(payload []byte) error {
	var newCfg DynamicCacheConfig
	if err := json.Unmarshal(payload, &newCfg); err != nil {
		return err
	}

	// 🎯 严格的安全护栏 (Guardrails) - 防止 AI 幻觉“自杀”
	if newCfg.L1CacheTTLSeconds < 1 || newCfg.L1CacheTTLSeconds > 3600 {
		return fmt.Errorf("invalid L1 TTL: %d", newCfg.L1CacheTTLSeconds)
	}
	if newCfg.L2CacheTTLSeconds < 1 || newCfg.L2CacheTTLSeconds > 86400 {
		return fmt.Errorf("invalid L2 TTL: %d", newCfg.L2CacheTTLSeconds)
	}
	if newCfg.PreloadDepth < 0 || newCfg.PreloadDepth > 10 {
		return fmt.Errorf("invalid PreloadDepth: %d", newCfg.PreloadDepth)
	}

	// 原子替换整个结构体指针，杜绝部分更新引起的幻读
	globalCacheConfig.Store(&newCfg)
	return nil
}
