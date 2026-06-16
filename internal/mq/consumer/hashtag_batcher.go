package consumer

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
)

// HashtagBatcher controls in-memory buffering and batch flushing of hashtags to Redis ZSet
type HashtagBatcher struct {
	mu          sync.Mutex
	buffer      map[string]int64
	redisClient *redis.Client
	interval    time.Duration
	stopChan    chan struct{}
	wg          sync.WaitGroup
}

// NewHashtagBatcher initializes the batcher with a Redis client and flush interval
func NewHashtagBatcher(client *redis.Client, interval time.Duration) *HashtagBatcher {
	return &HashtagBatcher{
		buffer:      make(map[string]int64),
		redisClient: client,
		interval:    interval,
		stopChan:    make(chan struct{}),
	}
}

// Add appends a hashtag count to the local in-memory buffer
func (b *HashtagBatcher) Add(tag string) {
	b.AddWithScore(tag, 1)
}

// AddWithScore appends a hashtag with custom score weight to the local in-memory buffer
func (b *HashtagBatcher) AddWithScore(tag string, score int64) {
	b.mu.Lock()
	b.buffer[tag] += score
	b.mu.Unlock()
}

// Start spawns the background worker to periodically flush cache to Redis
func (b *HashtagBatcher) Start() {
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		ticker := time.NewTicker(b.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				b.Flush()
			case <-b.stopChan:
				b.Flush() // Last flush on graceful shutdown
				return
			}
		}
	}()
}

// Stop shuts down the background flusher gracefully
func (b *HashtagBatcher) Stop() {
	close(b.stopChan)
	b.wg.Wait()
}

// Flush performs the batch writes to Redis via Pipeline
func (b *HashtagBatcher) Flush() {
	b.mu.Lock()
	if len(b.buffer) == 0 {
		b.mu.Unlock()
		return
	}
	localCopy := b.buffer
	b.buffer = make(map[string]int64)
	b.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pipeline := b.redisClient.Pipeline()
	for tag, count := range localCopy {
		pipeline.ZIncrBy(ctx, "trends:global", float64(count), tag)
	}
	pipeline.Expire(ctx, "trends:global", 24*time.Hour)

	if _, err := pipeline.Exec(ctx); err != nil {
		log.Printf("⚠️  Failed to flush trending batch to Redis: %v", err)
	}
}
