package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"regexp"
	"time"

	"twitter-clone/pkg/es"
	"twitter-clone/pkg/logger"
	"twitter-clone/pkg/qdrant"

	"twitter-clone/pkg/ai"

	"github.com/go-redis/redis/v8"
	amqp "github.com/rabbitmq/amqp091-go"
	"go.uber.org/zap"

	"twitter-clone/internal/domain"
	"twitter-clone/internal/events"
	"twitter-clone/internal/infrastructure/mq"
	tweetCache "twitter-clone/internal/module/tweet/cache"
	snowflake "twitter-clone/pkg/pkg/snowflake"
)

const (
	// ExchangeEvents 业务事件交换机
	ExchangeEvents = "twitter.events"

	// ExchangeRetry 重试交换机
	ExchangeRetry = "retry.events.exchange"

	// ExchangeDLX 死信交换机
	ExchangeDLX = "dlx.events.exchange"

	// QueueTweetFanout 推文扇出队列
	QueueTweetFanout = "queue.tweet.fanout"

	// QueueTweetFanoutRetry 推文扇出重试队列
	QueueTweetFanoutRetry = "queue.tweet.fanout.retry"

	// QueueTweetFanoutDLQ 推文扇出死信队列
	QueueTweetFanoutDLQ = "queue.tweet.fanout.dlq"

	// QueueTweetDelete 推文删除队列
	QueueTweetDelete = "queue.tweet.delete"

	// QueueTweetDeleteRetry 推文删除重试队列
	QueueTweetDeleteRetry = "queue.tweet.delete.retry"

	// QueueTweetDeleteDLQ 推文删除死信队列
	QueueTweetDeleteDLQ = "queue.tweet.delete.dlq"

	// QueueTweetRisk 🆕 推文风控旁路队列
	QueueTweetRisk = "queue.tweet.risk"

	// RoutingKeyTweetCreated 正常发推路由键
	RoutingKeyTweetCreated = "tweet.created"

	// RoutingKeyTweetDeleted 正常删推路由键
	RoutingKeyTweetDeleted = "tweet.deleted"

	// RoutingKeyTweetRisk 🆕 推文风控旁路路由键
	RoutingKeyTweetRisk = "risk.checking"

	// CelebrityMinFollowers 大V粉丝数判定阈值
	CelebrityMinFollowers = 5000

	// ConsumerName 消费者名称
	ConsumerName = "timeline-worker"

	// PrefetchCount 预取数量（限流）
	PrefetchCount = 10

	// MaxRetries 最大重试次数
	MaxRetries = 3
)

// TimelineConsumer Timeline 消费者
type TimelineConsumer struct {
	mq             *mq.RabbitMQ
	followRepo     domain.FollowRepository
	timelineCache  *tweetCache.TimelineCache
	redisClient    *redis.Client
	esClient       *es.Client
	qdrantClient   *qdrant.Client // 🆕 注入 Qdrant 客户端
	aiClient       *ai.Client
	outboxRepo     domain.OutboxRepository // 🆕 注入 Outbox 仓储
	hashtagBatcher *HashtagBatcher         // 🆕 注入 Hashtag 批量计数缓冲器
}

// NewTimelineConsumer 创建 Timeline 消费者
func NewTimelineConsumer(
	mqClient *mq.RabbitMQ,
	followRepo domain.FollowRepository,
	timelineCache *tweetCache.TimelineCache,
	redisClient *redis.Client,
	esClient *es.Client,
	qdrantClient *qdrant.Client, // 🆕 注入 Qdrant 客户端
	aiClient *ai.Client,
	outboxRepo domain.OutboxRepository, // 🆕 注入 Outbox 仓储
) (*TimelineConsumer, error) {
	// 1. 声明 Exchanges
	if err := mqClient.DeclareExchange(ExchangeEvents, "topic", true); err != nil {
		return nil, fmt.Errorf("failed to declare events exchange: %w", err)
	}
	if err := mqClient.DeclareExchange(ExchangeRetry, "topic", true); err != nil {
		return nil, fmt.Errorf("failed to declare retry exchange: %w", err)
	}
	if err := mqClient.DeclareExchange(ExchangeDLX, "topic", true); err != nil {
		return nil, fmt.Errorf("failed to declare dlx exchange: %w", err)
	}
	log.Println("✅ Exchanges declared: events, retry, dlx")

	// 2. 声明业务队列
	if _, err := mqClient.DeclareQueue(QueueTweetFanout, true); err != nil {
		return nil, fmt.Errorf("failed to declare fanout queue: %w", err)
	}
	if _, err := mqClient.DeclareQueue(QueueTweetDelete, true); err != nil {
		return nil, fmt.Errorf("failed to declare delete queue: %w", err)
	}
	if _, err := mqClient.DeclareQueue(QueueTweetRisk, true); err != nil {
		return nil, fmt.Errorf("failed to declare risk queue: %w", err)
	}

	// 3. 声明重试队列（配置 Dead Letter 参数以在 TTL 到期时重新发回到业务队列）
	fanoutRetryArgs := amqp.Table{
		"x-dead-letter-exchange":    ExchangeEvents,
		"x-dead-letter-routing-key": RoutingKeyTweetCreated,
	}
	if _, err := mqClient.DeclareQueueWithArgs(QueueTweetFanoutRetry, true, fanoutRetryArgs); err != nil {
		return nil, fmt.Errorf("failed to declare fanout retry queue: %w", err)
	}

	deleteRetryArgs := amqp.Table{
		"x-dead-letter-exchange":    ExchangeEvents,
		"x-dead-letter-routing-key": RoutingKeyTweetDeleted,
	}
	if _, err := mqClient.DeclareQueueWithArgs(QueueTweetDeleteRetry, true, deleteRetryArgs); err != nil {
		return nil, fmt.Errorf("failed to declare delete retry queue: %w", err)
	}

	// 4. 声明死信队列（DLQ）
	if _, err := mqClient.DeclareQueue(QueueTweetFanoutDLQ, true); err != nil {
		return nil, fmt.Errorf("failed to declare fanout dlq: %w", err)
	}
	if _, err := mqClient.DeclareQueue(QueueTweetDeleteDLQ, true); err != nil {
		return nil, fmt.Errorf("failed to declare delete dlq: %w", err)
	}
	log.Println("✅ Queues declared: business, retry, dlq, risk")

	// 5. 绑定正常业务队列
	if err := mqClient.BindQueue(QueueTweetFanout, RoutingKeyTweetCreated, ExchangeEvents); err != nil {
		return nil, fmt.Errorf("failed to bind fanout queue: %w", err)
	}
	if err := mqClient.BindQueue(QueueTweetDelete, RoutingKeyTweetDeleted, ExchangeEvents); err != nil {
		return nil, fmt.Errorf("failed to bind delete queue: %w", err)
	}
	if err := mqClient.BindQueue(QueueTweetRisk, RoutingKeyTweetRisk, ExchangeEvents); err != nil {
		return nil, fmt.Errorf("failed to bind risk queue: %w", err)
	}

	// 6. 绑定重试队列
	if err := mqClient.BindQueue(QueueTweetFanoutRetry, RoutingKeyTweetCreated+".retry", ExchangeRetry); err != nil {
		return nil, fmt.Errorf("failed to bind fanout retry queue: %w", err)
	}
	if err := mqClient.BindQueue(QueueTweetDeleteRetry, RoutingKeyTweetDeleted+".retry", ExchangeRetry); err != nil {
		return nil, fmt.Errorf("failed to bind delete retry queue: %w", err)
	}

	// 7. 绑定死信队列（DLQ）
	if err := mqClient.BindQueue(QueueTweetFanoutDLQ, RoutingKeyTweetCreated+".dlq", ExchangeDLX); err != nil {
		return nil, fmt.Errorf("failed to bind fanout dlq: %w", err)
	}
	if err := mqClient.BindQueue(QueueTweetDeleteDLQ, RoutingKeyTweetDeleted+".dlq", ExchangeDLX); err != nil {
		return nil, fmt.Errorf("failed to bind delete dlq: %w", err)
	}
	log.Println("✅ Bindings created successfully")

	// 设置 QoS（每次只处理 N 条消息）
	if err := mqClient.SetQoS(PrefetchCount); err != nil {
		return nil, fmt.Errorf("failed to set qos: %w", err)
	}

	log.Println("✅ Timeline consumer initialized")

	hashtagBatcher := NewHashtagBatcher(redisClient, 500*time.Millisecond)

	return &TimelineConsumer{
		mq:             mqClient,
		followRepo:     followRepo,
		timelineCache:  timelineCache,
		redisClient:    redisClient,
		esClient:       esClient,
		qdrantClient:   qdrantClient, // 🆕 注入 Qdrant
		aiClient:       aiClient,
		outboxRepo:     outboxRepo, // 🆕 注入 Outbox 仓储
		hashtagBatcher: hashtagBatcher,
	}, nil
}

// Start 启动消费者
func (c *TimelineConsumer) Start(ctx context.Context) error {
	// 🆕 启动 hashtag 批量收集器
	c.hashtagBatcher.Start()

	// 启动扇出消费者
	go c.consumeFanout(ctx)

	// 启动删除消费者
	go c.consumeDelete(ctx)

	// 🆕 启动事务发件箱（Outbox）对账补偿协程
	go c.StartOutboxWorker(ctx)

	log.Println("🚀 Timeline consumer, Outbox worker and HashtagBatcher started")

	// 阻塞主线程
	<-ctx.Done()

	// 🆕 优雅停止 hashtag 收集器并强制刷写缓存落盘
	c.hashtagBatcher.Stop()

	log.Println("⏹️  Timeline consumer and Outbox worker stopped")
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
				log.Println("channel closed, reconnecting...")
				for {
					time.Sleep(5 * time.Second)
					newMsgs, err := c.mq.Consume(QueueTweetFanout, ConsumerName+"-fanout")
					if err == nil {
						messages = newMsgs
						break // 重连成功才退出循环
					}
					log.Printf("reconnect failed: %v, retrying...", err)
				}
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
		msg.Nack(false, false) // 格式错误直接丢弃
		return
	}

	log.Printf("📨 Received: tweet.created (tweet_id=%d, author_id=%d)", event.TweetID, event.AuthorID)

	// 执行扇出
	if err := c.fanoutToFollowers(event.AuthorID, event.TweetID); err != nil {
		log.Printf("❌ Fanout failed: %v", err)
		c.handleFailure(msg, RoutingKeyTweetCreated)
		return
	}

	// 确认消息
	if err := msg.Ack(false); err != nil {
		log.Printf("❌ Failed to ack message: %v", err)
	}

	// 提取并更新 Hashtags 用于热门话题
	go c.processHashtags(context.Background(), event.Content)

	// 🆕 将 ES 向量同步操作作为 Outbox 任务持久化写入数据库，保障高可用与最终一致性
	payloadBytes, err := json.Marshal(event)
	if err != nil {
		log.Printf("❌ Failed to marshal ES outbox payload: %v", err)
	} else {
		id, err := snowflake.GenerateID()
		if err != nil {
			return
		}

		task := &domain.OutboxTask{
			ID:         id,
			TaskType:   "sync_es",
			Payload:    string(payloadBytes),
			Status:     domain.OutboxStatusPending,
			MaxRetries: 5,
		}
		if err := c.outboxRepo.Create(context.Background(), task); err != nil {
			log.Printf("❌ Failed to create ES sync outbox task: %v", err)
		} else {
			log.Printf("📦 ES sync outbox task created: task_id=%d", task.ID)
		}
	}

	// 🆕 异步广播发帖到风控旁路检测队列
	go func() {
		riskCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := c.mq.Publish(riskCtx, ExchangeEvents, RoutingKeyTweetRisk, msg.Body); err != nil {
			log.Printf("❌ Failed to broadcast to risk queue: %v", err)
		} else {
			log.Printf("⚡ Broadcasted to risk queue for tweet_id=%d", event.TweetID)
		}
	}()

	log.Printf("✅ Fanout completed: tweet_id=%d", event.TweetID)
}

// processHashtags 提取 Hashtags 并更新 Redis ZSet
func (c *TimelineConsumer) processHashtags(ctx context.Context, content string) {
	// 正则匹配 #hashtag
	re := regexp.MustCompile(`#(\w+)`)
	matches := re.FindAllStringSubmatch(content, -1)

	if len(matches) == 0 {
		return
	}

	for _, match := range matches {
		if len(match) > 1 {
			tag := match[1]
			// 🆕 将高频热 Key 写入改为本地内存计数累加，避开单点 CPU 排他锁竞争
			c.hashtagBatcher.Add(tag)
		}
	}
	log.Printf("🔥 Buffered trending topics locally: %v", matches)
}

// fanoutToFollowers 扇出到粉丝
func (c *TimelineConsumer) fanoutToFollowers(authorID uint64, tweetID uint64) error {
	ctx := context.Background()

	// 1. 检查是否为大V (获取发推人粉丝数)
	isCelebrity, err := c.timelineCache.IsCelebrity(ctx, authorID)
	if err != nil {
		log.Printf("⚠️  Failed to check celebrity status for user %d: %v", authorID, err)
		// 如果 Redis 出错，降级走普通写扩散流程，不影响正常发布
	} else if isCelebrity {
		log.Printf("📢 [Celebrity Push Avoided] Author %d is a celebrity. Skipping write-diffusion fanout.", authorID)
		// 🆕 将推文写入大V个人时间线缓存，以供粉丝拉取使用 (L2 Pull 缓存)
		if cacheErr := c.timelineCache.AddToUserTimeline(ctx, authorID, tweetID); cacheErr != nil {
			log.Printf("⚠️  Failed to add to celebrity timeline cache for user %d: %v", authorID, cacheErr)
		}
		return nil // 略过写扩散，大V通过拉模式（读扩散）提供数据
	}

	// 2. 获取粉丝列表（限制大V判定阈值 5000，普通博主的全部粉丝都能覆盖且无雪崩风险）
	followerIDs, err := c.followRepo.GetActiveFollowers(ctx, authorID, CelebrityMinFollowers)
	if err != nil {
		return fmt.Errorf("failed to get followers: %w", err)
	}

	if len(followerIDs) == 0 {
		log.Printf("ℹ️  No followers for user %d", authorID)
		return nil
	}

	log.Printf("📤 Fanout to %d followers...", len(followerIDs))

	// 3. 分批推送（每批 100 个）
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
		c.handleFailure(msg, RoutingKeyTweetDeleted)
		return
	}

	// 确认消息
	if err := msg.Ack(false); err != nil {
		log.Printf("❌ Failed to ack message: %v", err)
	}
	// 从 ES 删除
	go func() {
		if c.esClient == nil {
			return
		}
		tweetID := fmt.Sprintf("%d", event.TweetID)
		if err := c.esClient.DeleteTweet(context.Background(), tweetID); err != nil {
			log.Printf("⚠️ Failed to delete tweet from ES: %v", err)
		}
	}()
	log.Printf("✅ Remove completed: tweet_id=%d", event.TweetID)
}

// removeFromFollowersTimeline 从粉丝 Timeline 删除，并失效 L1/L2 缓存
func (c *TimelineConsumer) removeFromFollowersTimeline(authorID uint64, tweetID uint64) error {
	ctx := context.Background()

	// 🆕 异步广播失效该推文的 L1/L2 缓存，以及大V的个人时间线
	go func() {
		invalidCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := c.timelineCache.InvalidateBaseTweet(invalidCtx, tweetID); err != nil {
			log.Printf("⚠️  Failed to globally invalidate tweet cache: %v", err)
		}
		_ = c.timelineCache.RemoveFromUserTimeline(invalidCtx, authorID, tweetID)
	}()

	// 1. 检查是否为大V
	isCelebrity, err := c.timelineCache.IsCelebrity(ctx, authorID)
	if err != nil {
		log.Printf("⚠️  Failed to check celebrity status for user %d on delete: %v", authorID, err)
	} else if isCelebrity {
		log.Printf("📢 [Celebrity Delete Skip] Author %d is a celebrity. Skipping delete-diffusion.", authorID)
		return nil // 大V没有写扩散过，直接略过
	}

	// 2. 获取全部粉丝 (限制为 CelebrityMinFollowers 5000)
	followerIDs, err := c.followRepo.GetActiveFollowers(ctx, authorID, CelebrityMinFollowers)
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

	// rabbitmq Header 解出来可能是 int32 或 int，这里做一个防御性类型断言
	if count, ok := headers["x-retry-count"].(int32); ok {
		return int(count)
	}
	if count, ok := headers["x-retry-count"].(int); ok {
		return count
	}

	return 0
}

// handleFailure 消息消费失败通用处理函数：实现指数退避重试或路由到死信队列
func (c *TimelineConsumer) handleFailure(msg amqp.Delivery, routingKeySuffix string) {
	ctx := context.Background()
	retryCount := getRetryCount(msg.Headers)

	// 初始化/复制 Headers
	headers := msg.Headers
	if headers == nil {
		headers = amqp.Table{}
	}

	if retryCount < MaxRetries {
		delaySeconds := 1 << retryCount // 1s, 2s, 4s...
		headers["x-retry-count"] = int32(retryCount + 1)

		log.Printf("🔄 Retrying message (attempt %d/%d) in %ds. RoutingKey: %s.retry", retryCount+1, MaxRetries, delaySeconds, routingKeySuffix)

		err := c.mq.GetChannel().PublishWithContext(
			ctx,
			ExchangeRetry,
			routingKeySuffix+".retry",
			false, // mandatory
			false, // immediate
			amqp.Publishing{
				ContentType:  msg.ContentType,
				DeliveryMode: amqp.Persistent,
				Headers:      headers,
				Body:         msg.Body,
				Expiration:   fmt.Sprintf("%d", delaySeconds*1000), // TTL in ms (string)
				Timestamp:    time.Now(),
			},
		)
		if err != nil {
			log.Printf("❌ Failed to publish retry message: %v", err)
			// 万一重试队列发布失败，则 requeue 退回原队列，防止丢数据
			msg.Nack(false, true)
			return
		}
	} else {
		log.Printf("💀 Max retries (%d) exceeded, routing to DLQ: %s.dlq", MaxRetries, routingKeySuffix)

		err := c.mq.GetChannel().PublishWithContext(
			ctx,
			ExchangeDLX,
			routingKeySuffix+".dlq",
			false,
			false,
			amqp.Publishing{
				ContentType:  msg.ContentType,
				DeliveryMode: amqp.Persistent,
				Headers:      headers,
				Body:         msg.Body,
				Timestamp:    time.Now(),
			},
		)
		if err != nil {
			log.Printf("❌ Failed to publish to DLQ: %v. Message will be silently lost!", err)
			msg.Nack(false, true) // 退回原队列挂起
			return
		}

		// 结构化日志报警
		logger.Error(ctx, "Message exceeded max retries and routed to DLQ",
			zap.String("routing_key", msg.RoutingKey),
			zap.Int("retry_count", retryCount),
			zap.ByteString("message_body", msg.Body),
		)
	}

	// 无论是发送到重试队列还是死信队列，成功后都需要对原消息进行确认（从当前队列移出）
	if err := msg.Ack(false); err != nil {
		log.Printf("❌ Failed to ack handled failure message: %v", err)
	}
}

// StartOutboxWorker 启动发件箱后台对账守护协程
func (c *TimelineConsumer) StartOutboxWorker(ctx context.Context) {
	log.Println("🚀 Outbox worker daemon started")
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("⏹️  Outbox worker daemon stopped")
			return
		case <-ticker.C:
			c.processOutboxTasks(ctx)
		}
	}
}

// processOutboxTasks 批量处理待同步的发件箱任务
func (c *TimelineConsumer) processOutboxTasks(ctx context.Context) {
	tasks, err := c.outboxRepo.GetPendingTasks(ctx, 10)
	if err != nil {
		log.Printf("⚠️  Outbox worker: failed to query pending tasks: %v", err)
		return
	}

	if len(tasks) == 0 {
		return
	}

	log.Printf("📦 Outbox worker: processing %d pending tasks...", len(tasks))

	for _, task := range tasks {
		if task.TaskType != "sync_es" {
			log.Printf("⚠️  Outbox worker: unknown task type %s for task %d", task.TaskType, task.ID)
			continue
		}

		var event events.TweetCreatedEvent
		if err := json.Unmarshal([]byte(task.Payload), &event); err != nil {
			log.Printf("❌ Outbox worker: failed to unmarshal payload for task %d: %v", task.ID, err)
			// 格式非法直接物理删除，避免卡死通道
			_ = c.outboxRepo.Delete(ctx, task.ID)
			continue
		}

		// 同步执行 ES 写入
		err = c.executeESIndex(ctx, &event)
		if err != nil {
			log.Printf("❌ Outbox worker: failed to sync ES for task %d: %v", task.ID, err)
			task.Retries++
			task.ErrorMsg = err.Error()
			if task.Retries >= task.MaxRetries {
				task.Status = domain.OutboxStatusFailed
				log.Printf("🚨 Outbox worker: task %d reached max retries (%d), marked as Failed", task.ID, task.MaxRetries)
			} else {
				task.Status = domain.OutboxStatusFailed // status=2 触发指数级退避
			}

			if updateErr := c.outboxRepo.Update(ctx, task); updateErr != nil {
				log.Printf("❌ Outbox worker: failed to update task %d status: %v", task.ID, updateErr)
			}
		} else {
			log.Printf("✅ Outbox worker: successfully synced ES for task %d, deleting task", task.ID)
			// 执行成功直接物理删除记录，释放数据库表空间
			if deleteErr := c.outboxRepo.Delete(ctx, task.ID); deleteErr != nil {
				log.Printf("❌ Outbox worker: failed to delete task %d: %v", task.ID, deleteErr)
			}
		}
	}
}

// executeESIndex 核心向量计算与 ES 索引逻辑
func (c *TimelineConsumer) executeESIndex(ctx context.Context, event *events.TweetCreatedEvent) error {
	embeddingData, err := c.aiClient.GetEmbedding(ctx, event.Content, os.Getenv("LM_STUDIO_MODEL_EMBEDDING"))
	if err != nil {
		return fmt.Errorf("failed to get embedding: %w", err)
	}

	doc := es.TweetDocument{
		ID:            fmt.Sprintf("%d", event.TweetID),
		UserID:        fmt.Sprintf("%d", event.AuthorID),
		ParentID:      fmt.Sprintf("%d", event.ParentID),
		Content:       "Document: " + event.Content,
		ContentVector: nil, // 🎯 设为 nil，释放 ES 堆内存与 HNSW 索引压力
		Type:          event.Type,
		VisibleType:   event.VisibleType,
		CreatedAt:     event.CreatedAt,
		LikeCount:     0,
		DeletedAt:     0,
	}

	if c.esClient == nil {
		log.Println("⚠️ Elasticsearch client is nil. Skip indexing.")
		return nil
	}
	if err := c.esClient.IndexTweet(ctx, doc); err != nil {
		return fmt.Errorf("failed to index tweet in ES: %w", err)
	}

	// 🎯 写入 Qdrant (HNSW)
	if c.qdrantClient != nil {
		payload := map[string]interface{}{
			"user_id":      fmt.Sprintf("%d", event.AuthorID),
			"parent_id":    fmt.Sprintf("%d", event.ParentID),
			"content":      "Document: " + event.Content,
			"type":         event.Type,
			"visible_type": event.VisibleType,
			"created_at":   event.CreatedAt,
		}
		if err := c.qdrantClient.UpsertPoint(ctx, "tweets", event.TweetID, embeddingData, payload); err != nil {
			return fmt.Errorf("failed to upsert point to Qdrant: %w", err)
		}
		log.Printf("✅ Synced vector to Qdrant successfully: tweet_id=%d", event.TweetID)
	}

	return nil
}
