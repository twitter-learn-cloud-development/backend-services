package config

import (
	"fmt"
	"log"
	"time"

	"github.com/hashicorp/consul/api"
)

// ConsulConfigClient Encapsulates Consul Client
type ConsulConfigClient struct {
	client *api.Client
}

// NewConsulConfigClient Creates a new Consul configuration client
func NewConsulConfigClient(address string) (*ConsulConfigClient, error) {
	config := api.DefaultConfig()
	config.Address = address
	client, err := api.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create consul client: %w", err)
	}
	return &ConsulConfigClient{client: client}, nil
}

// GetConfig gets a configuration value (string)
func (c *ConsulConfigClient) GetConfig(key string) (string, error) {
	kv := c.client.KV()
	pair, _, err := kv.Get(key, nil)
	if err != nil {
		return "", err
	}
	if pair == nil {
		return "", fmt.Errorf("config key not found: %s", key)
	}
	return string(pair.Value), nil
}

// WatchConfig watches for configuration changes and executes the callback
func (c *ConsulConfigClient) WatchConfig(key string, onChange func(string)) {
	go func() {
		kv := c.client.KV()
		var lastIndex uint64

		for {
			opts := &api.QueryOptions{
				WaitIndex: lastIndex,
				WaitTime:  10 * time.Minute, // Blocking query max wait time
			}

			pair, meta, err := kv.Get(key, opts)
			if err != nil {
				log.Printf("❌ [Consul] Error watching key %s: %v", key, err)
				time.Sleep(5 * time.Second) // Failure backoff
				continue
			}

			if pair == nil {
				log.Printf("⚠️ [Consul] Key %s deleted or not found during watch", key)
				time.Sleep(5 * time.Second)
				// If deleted, we might want to keep watching or handle it.
				// For now, retry loop.
				if meta != nil {
					lastIndex = meta.LastIndex
				}
				continue
			}

			// If index changed, update and notify
			if meta.LastIndex > lastIndex {
				log.Printf("🔄 [Consul] Config changed: %s", key)
				lastIndex = meta.LastIndex
				onChange(string(pair.Value))
			}
		}
	}()
}
