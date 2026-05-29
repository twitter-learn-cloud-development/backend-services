package cache

import (
	"context"
	"log"
	"twitter-clone/pkg/config"

	"github.com/go-redis/redis/v8"
)

// StartConfigListener 启动自举 (GET) 与热更新订阅广播 (PubSub) 的双保险监听器，解决 K8s 脑裂与冷启动问题
func StartConfigListener(ctx context.Context, rdb *redis.Client) {
	configKey := "system:cache:dynamic_config"
	pubsubChannel := "channel:dynamic-cache-config"

	// 1. 🎯 启动自举 (Self-Bootstrap)：解决新建 Pod 或重启 Pod 的配置落后问题
	initPayload, err := rdb.Get(ctx, configKey).Bytes()
	if err == nil {
		if err := config.ReloadConfig(initPayload); err != nil {
			log.Printf("⚠️ [Bootstrap] Failed to load config from Redis: %v, using default", err)
		} else {
			log.Printf("✅ [Bootstrap] Dynamic config loaded successfully: %+v", config.GetCurrentConfig())
		}
	} else {
		log.Printf("ℹ️ [Bootstrap] No dynamic config found in Redis, using built-in default config")
	}

	// 2. 监听后续的热更新广播
	pubsub := rdb.Subscribe(ctx, pubsubChannel)
	go func() {
		defer pubsub.Close()
		
		// 确保收到订阅成功的消息
		_, err := pubsub.Receive(ctx)
		if err != nil {
			log.Printf("🚨 [Hot Reload] Failed to subscribe to channel: %v", err)
			return
		}
		log.Printf("✅ [Hot Reload] Subscribed to Redis dynamic config channel: %s", pubsubChannel)

		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-pubsub.Channel():
				if !ok {
					return
				}
				log.Printf("🔥 [Hot Reload] Received new tuning config from AIOps Agent")
				if err := config.ReloadConfig([]byte(msg.Payload)); err != nil {
					log.Printf("🚨 [Guardrail Blocked] Invalid config rejected: %v", err)
				} else {
					log.Printf("🛡️ [Tuning Applied] Cache configs dynamically updated! Current: %+v", config.GetCurrentConfig())
				}
			}
		}
	}()
}
