package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"twitter-clone/internal/domain"
	"twitter-clone/internal/events"
	"twitter-clone/internal/infrastructure/mq"
	tweetCache "twitter-clone/internal/module/tweet/cache"
)

const (
	// QueueTweetFanout 推文扇出队列
	QueueTweetFanout = "queue.tweet.fanout"

	// QueueTweetDelete 推文删除队列
	QueueTweetDelete = "queue.tweet.delete"

	// ConsumerName 消费者名称
	ConsumerName = "timeline-worker"

	// PrefetchCount 预取数量（限流）
	PrefetchCount = 10

	// MaxRetries 最大重试次数
	MaxRetries = 3
)

// TimelineConsumer Timeline 消费者
type TimelineConsumer struct {
	mq            *mq.RabbitMQ
	followRepo    domain.FollowRepository
	timelineCache *tweetCache.TimelineCache
}

// NewTimelineConsumer 创建 Timeline 消费者
func NewTimelineConsumer(
	mqClient *mq.RabbitMQ,
	followRepo domain.FollowRepository,
	timelineCache *tweetCache.TimelineCache,
) (*TimelineConsumer, error) {
	if err := mqClient.DeclareExchange("twitter.events", "topic", true); err != nil {
		return nil, fmt.Errorf("failed to declare exchange: %w", err)
	}
	log.Println("✅ Exchange declared: twitter.events")
	// 声明队列
	if _, err := mqClient.DeclareQueue(QueueTweetFanout, true); err != nil {
		return nil, fmt.Errorf("failed to declare fanout queue: %w", err)
	}

	if _, err := mqClient.DeclareQueue(QueueTweetDelete, true); err != nil {
		return nil, fmt.Errorf("failed to declare delete queue: %w", err)
	}

	// 绑定队列到 Exchange
	if err := mqClient.BindQueue(QueueTweetFanout, "tweet.created", "twitter.events"); err != nil {
		return nil, fmt.Errorf("failed to bind fanout queue: %w", err)
	}

	if err := mqClient.BindQueue(QueueTweetDelete, "tweet.deleted", "twitter.events"); err != nil {
		return nil, fmt.Errorf("failed to bind delete queue: %w", err)
	}

	// 设置 QoS（每次只处理 N 条消息）
	if err := mqClient.SetQoS(PrefetchCount); err != nil {
		return nil, fmt.Errorf("failed to set qos: %w", err)
	}

	log.Println("✅ Timeline consumer initialized")

	return &TimelineConsumer{
		mq:            mqClient,
		followRepo:    followRepo,
		timelineCache: timelineCache,
	}, nil
}

// Start 启动消费者
func (c *TimelineConsumer) Start(ctx context.Context) error {
	// 启动扇出消费者
	go c.consumeFanout(ctx)

	// 启动删除消费者
	go c.consumeDelete(ctx)

	log.Println("🚀 Timeline consumer started")

	// 阻塞主线程
	<-ctx.Done()

	log.Println("⏹️  Timeline consumer stopped")
	return nil
}

// consumeFanout 消费推文创建事件（扇出）
func (c *TimelineConsumer) consumeFanout(ctx context.Context) {
	messages, err := c.mq.Consume(QueueTweetFanout, ConsumerName+"-fanout")
	if err != nil {
		log.Printf("❌ Failed to consume fanout queue: %v", err)
		return
	}

	log.Println("📥 Listening for tweet.created events...")

	for {
		select {
		case <-ctx.Done():
			return

		case msg, ok := <-messages:
			if !ok {
				log.Println("⚠️  Fanout message channel closed, reconnecting...")
				time.Sleep(5 * time.Second)
				messages, _ = c.mq.Consume(QueueTweetFanout, ConsumerName+"-fanout")
				continue
			}

			c.handleFanoutMessage(msg)
		}
	}
}

// handleFanoutMessage 处理扇出消息
func (c *TimelineConsumer) handleFanoutMessage(msg amqp.Delivery) {
	// 解析事件
	var event events.TweetCreatedEvent
	if err := json.Unmarshal(msg.Body, &event); err != nil {
		log.Printf("❌ Failed to unmarshal fanout event: %v", err)
		msg.Nack(false, false) // 不重试
		return
	}

	log.Printf("📨 Received: tweet.created (tweet_id=%d, author_id=%d)", event.TweetID, event.AuthorID)

	// 执行扇出
	if err := c.fanoutToFollowers(event.AuthorID, event.TweetID); err != nil {
		log.Printf("❌ Fanout failed: %v", err)

		// 重试逻辑
		retryCount := getRetryCount(msg.Headers)
		if retryCount < MaxRetries {
			log.Printf("🔄 Retrying... (attempt %d/%d)", retryCount+1, MaxRetries)
			msg.Nack(false, true) // 重新入队
		} else {
			log.Printf("💀 Max retries exceeded, discarding message")
			msg.Nack(false, false) // 丢弃
		}
		return
	}

	// 确认消息
	if err := msg.Ack(false); err != nil {
		log.Printf("❌ Failed to ack message: %v", err)
	}

	log.Printf("✅ Fanout completed: tweet_id=%d", event.TweetID)
}

// fanoutToFollowers 扇出到粉丝
func (c *TimelineConsumer) fanoutToFollowers(authorID uint64, tweetID uint64) error {
	ctx := context.Background()

	// 1. 获取活跃粉丝列表（限制 1000）
	followerIDs, err := c.followRepo.GetActiveFollowers(ctx, authorID, 1000)
	if err != nil {
		return fmt.Errorf("failed to get followers: %w", err)
	}

	if len(followerIDs) == 0 {
		log.Printf("ℹ️  No followers for user %d", authorID)
		return nil
	}

	log.Printf("📤 Fanout to %d followers...", len(followerIDs))

	// 2. 分批推送（每批 100 个）
	batchSize := 100
	for i := 0; i < len(followerIDs); i += batchSize {
		end := i + batchSize
		if end > len(followerIDs) {
			end = len(followerIDs)
		}

		batch := followerIDs[i:end]

		// 批量添加到 Timeline
		if err := c.timelineCache.BatchAddToTimeline(ctx, batch, tweetID); err != nil {
			log.Printf("⚠️  Failed to fanout batch %d-%d: %v", i, end, err)
			continue
		}

		log.Printf("✅ Fanout batch %d-%d completed", i, end)

		// 避免 Redis 压力过大
		if end < len(followerIDs) {
			time.Sleep(10 * time.Millisecond)
		}
	}

	return nil
}

// consumeDelete 消费推文删除事件
func (c *TimelineConsumer) consumeDelete(ctx context.Context) {
	messages, err := c.mq.Consume(QueueTweetDelete, ConsumerName+"-delete")
	if err != nil {
		log.Printf("❌ Failed to consume delete queue: %v", err)
		return
	}

	log.Println("📥 Listening for tweet.deleted events...")

	for {
		select {
		case <-ctx.Done():
			return

		case msg, ok := <-messages:
			if !ok {
				log.Println("⚠️  Delete message channel closed, reconnecting...")
				time.Sleep(5 * time.Second)
				messages, _ = c.mq.Consume(QueueTweetDelete, ConsumerName+"-delete")
				continue
			}

			c.handleDeleteMessage(msg)
		}
	}
}

// handleDeleteMessage 处理删除消息
func (c *TimelineConsumer) handleDeleteMessage(msg amqp.Delivery) {
	// 解析事件
	var event events.TweetDeletedEvent
	if err := json.Unmarshal(msg.Body, &event); err != nil {
		log.Printf("❌ Failed to unmarshal delete event: %v", err)
		msg.Nack(false, false)
		return
	}

	log.Printf("📨 Received: tweet.deleted (tweet_id=%d, author_id=%d)", event.TweetID, event.AuthorID)

	// 执行删除
	if err := c.removeFromFollowersTimeline(event.AuthorID, event.TweetID); err != nil {
		log.Printf("❌ Remove failed: %v", err)
		msg.Nack(false, true) // 重新入队
		return
	}

	// 确认消息
	msg.Ack(false)
	log.Printf("✅ Remove completed: tweet_id=%d", event.TweetID)
}

// removeFromFollowersTimeline 从粉丝 Timeline 删除
func (c *TimelineConsumer) removeFromFollowersTimeline(authorID uint64, tweetID uint64) error {
	ctx := context.Background()

	followerIDs, err := c.followRepo.GetActiveFollowers(ctx, authorID, 1000)
	if err != nil {
		return fmt.Errorf("failed to get followers: %w", err)
	}

	if len(followerIDs) > 0 {
		return c.timelineCache.BatchRemoveFromTimeline(ctx, followerIDs, tweetID)
	}

	return nil
}

// getRetryCount 获取重试次数
func getRetryCount(headers amqp.Table) int {
	if headers == nil {
		return 0
	}

	if count, ok := headers["x-retry-count"].(int32); ok {
		return int(count)
	}

	return 0
}
