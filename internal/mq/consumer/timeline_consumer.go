package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"twitter-clone/pkg/es"
	"twitter-clone/pkg/qdrant"

	"twitter-clone/pkg/ai"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"

	"twitter-clone/internal/domain"
	"twitter-clone/internal/events"
	"twitter-clone/internal/infrastructure/mq"
	tweetCache "twitter-clone/internal/module/tweet/cache"
	snowflake "twitter-clone/pkg/pkg/snowflake"
)

const (
	// ExchangeEvents 业务事件交换机
	ExchangeEvents = "twitter.events"

	// ExchangeDLX 死信交换机
	ExchangeDLX = "dlx.events.exchange"

	// QueueTweetFanout 推文扇出队列
	QueueTweetFanout = "queue.tweet.fanout"

	// QueueTweetFanoutDLQ 推文扇出死信队列
	QueueTweetFanoutDLQ = "queue.tweet.fanout.dlq"

	// QueueTweetDelete 推文删除队列
	QueueTweetDelete = "queue.tweet.delete"

	// QueueTweetDeleteDLQ 推文删除死信队列
	QueueTweetDeleteDLQ = "queue.tweet.delete.dlq"

	QueueTweetModerationCleanup    = "queue.tweet.moderation.cleanup"
	QueueTweetModerationCleanupDLQ = "queue.tweet.moderation.cleanup.dlq"

	// RoutingKeyTweetCreated 正常发推路由键
	RoutingKeyTweetCreated = "tweet.created"

	// RoutingKeyTweetDeleted 正常删推路由键
	RoutingKeyTweetDeleted = "tweet.deleted"

	RoutingKeyTweetModerated = "tweet.moderated"

	// CelebrityMinFollowers 大V粉丝数判定阈值
	CelebrityMinFollowers = 5000

	// ConsumerName 消费者名称
	ConsumerName = "timeline-worker"

	// PrefetchCount 预取数量（限流）
	PrefetchCount = 10

	// MaxRetries 最大重试次数
	MaxRetries = 3

	// QueueTweetLiked 🆕 推文点赞队列
	QueueTweetLiked = "queue.tweet.liked"

	// QueueCommentCreated 🆕 评论创建队列
	QueueCommentCreated = "queue.comment.created"

	// RoutingKeyTweetLiked 🆕 推文点赞路由键
	RoutingKeyTweetLiked = "tweet.liked"

	// RoutingKeyCommentCreated 🆕 评论创建路由键
	RoutingKeyCommentCreated = "comment.created"
)

const (
	syncESOutboxTaskType     = "sync_es"
	syncESOutboxDedupVersion = "v1"
	outboxSuccessRetention   = 72 * time.Hour
	outboxCleanupInterval    = time.Minute
	outboxCleanupBatchSize   = 1000
	outboxCleanupMaxBatches  = 10
	outboxWorkerInterval     = 5 * time.Second
	outboxClaimBatchSize     = 10
	outboxRecoveryBatchSize  = 100
	outboxLeaseDuration      = 90 * time.Second
	outboxTaskTimeout        = 60 * time.Second
	outboxFinalizeTimeout    = 5 * time.Second
	tweetCreatedStageTimeout = 30 * time.Second
)

// TimelineConsumer Timeline 消费者
type TimelineConsumer struct {
	mq                   *mq.RabbitMQ
	followRepo           domain.FollowRepository
	followerPager        domain.FollowerPageRepository
	timelineCache        *tweetCache.TimelineCache
	redisClient          *redis.Client
	esClient             *es.Client
	qdrantClient         *qdrant.Client // 🆕 注入 Qdrant 客户端
	aiClient             *ai.Client
	outboxRepo           domain.OutboxRepository // 🆕 注入 Outbox 仓储
	hashtagBatcher       *HashtagBatcher         // 🆕 注入 Hashtag 批量计数缓冲器
	trendsProcessor      *TrendsProcessor        // 🆕 注入趋势话题处理器
	moderationObserver   ModerationCleanupObserver
	tweetCreatedObserver TweetCreatedObserver
	outboxObserver       OutboxWorkerObserver
	trendProjector       *tweetCreatedTrendProjector
	newOutboxTaskID      func() (uint64, error)
	newOutboxLeaseToken  func() string
	outboxWorkerID       string
	outboxNow            func() time.Time
	searchSyncExecutor   func(context.Context, *events.TweetCreatedEvent) error
	failureRouter        *timelineFailureRouter
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
	trendsProcessor *TrendsProcessor, // 🆕 注入趋势话题处理器
	failurePublisher timelineFailurePublisher,
) (*TimelineConsumer, error) {
	followerPager, ok := followRepo.(domain.FollowerPageRepository)
	if !ok {
		return nil, fmt.Errorf("follow repository does not support stable follower pagination")
	}
	failureRouter, err := newTimelineFailureRouter(failurePublisher)
	if err != nil {
		return nil, err
	}

	// 1. 声明 Exchanges
	if err := mqClient.DeclareExchange(ExchangeEvents, "topic", true); err != nil {
		return nil, fmt.Errorf("failed to declare events exchange: %w", err)
	}
	if err := mqClient.DeclareExchange(ExchangeDLX, "topic", true); err != nil {
		return nil, fmt.Errorf("failed to declare dlx exchange: %w", err)
	}
	log.Println("✅ Exchanges declared: events, dlx")

	// 2. 声明业务队列
	if _, err := mqClient.DeclareQueue(QueueTweetFanout, true); err != nil {
		return nil, fmt.Errorf("failed to declare fanout queue: %w", err)
	}
	if _, err := mqClient.DeclareQueue(QueueTweetDelete, true); err != nil {
		return nil, fmt.Errorf("failed to declare delete queue: %w", err)
	}
	if _, err := mqClient.DeclareQueue(QueueTweetModerationCleanup, true); err != nil {
		return nil, fmt.Errorf("failed to declare moderation cleanup queue: %w", err)
	}
	if _, err := mqClient.DeclareQueue(QueueTweetLiked, true); err != nil {
		return nil, fmt.Errorf("failed to declare liked queue: %w", err)
	}
	if _, err := mqClient.DeclareQueue(QueueCommentCreated, true); err != nil {
		return nil, fmt.Errorf("failed to declare comment queue: %w", err)
	}

	// 3. 声明死信队列（DLQ）
	if _, err := mqClient.DeclareQueue(QueueTweetFanoutDLQ, true); err != nil {
		return nil, fmt.Errorf("failed to declare fanout dlq: %w", err)
	}
	if _, err := mqClient.DeclareQueue(QueueTweetDeleteDLQ, true); err != nil {
		return nil, fmt.Errorf("failed to declare delete dlq: %w", err)
	}
	if _, err := mqClient.DeclareQueue(QueueTweetModerationCleanupDLQ, true); err != nil {
		return nil, fmt.Errorf("failed to declare moderation cleanup dlq: %w", err)
	}
	log.Println("✅ Queues declared: business, retry, dlq, liked, comment")

	// 4. 绑定正常业务队列
	if err := mqClient.BindQueue(QueueTweetFanout, RoutingKeyTweetCreated, ExchangeEvents); err != nil {
		return nil, fmt.Errorf("failed to bind fanout queue: %w", err)
	}
	if err := mqClient.BindQueue(QueueTweetDelete, RoutingKeyTweetDeleted, ExchangeEvents); err != nil {
		return nil, fmt.Errorf("failed to bind delete queue: %w", err)
	}
	if err := mqClient.BindQueue(QueueTweetModerationCleanup, RoutingKeyTweetModerated, ExchangeEvents); err != nil {
		return nil, fmt.Errorf("failed to bind moderation cleanup queue: %w", err)
	}
	if err := mqClient.BindQueue(QueueTweetLiked, RoutingKeyTweetLiked, ExchangeEvents); err != nil {
		return nil, fmt.Errorf("failed to bind liked queue: %w", err)
	}
	if err := mqClient.BindQueue(QueueCommentCreated, RoutingKeyCommentCreated, ExchangeEvents); err != nil {
		return nil, fmt.Errorf("failed to bind comment queue: %w", err)
	}

	// 5. 绑定死信队列（DLQ）
	if err := mqClient.BindQueue(QueueTweetFanoutDLQ, RoutingKeyTweetCreated+".dlq", ExchangeDLX); err != nil {
		return nil, fmt.Errorf("failed to bind fanout dlq: %w", err)
	}
	if err := mqClient.BindQueue(QueueTweetDeleteDLQ, RoutingKeyTweetDeleted+".dlq", ExchangeDLX); err != nil {
		return nil, fmt.Errorf("failed to bind delete dlq: %w", err)
	}
	if err := mqClient.BindQueue(QueueTweetModerationCleanupDLQ, RoutingKeyTweetModerated+".dlq", ExchangeDLX); err != nil {
		return nil, fmt.Errorf("failed to bind moderation cleanup dlq: %w", err)
	}
	if err := DeclareTimelineRecoveryTopology(mqClient); err != nil {
		return nil, err
	}
	log.Println("✅ Bindings created successfully")

	// 设置 QoS（每次只处理 N 条消息）
	if err := mqClient.SetQoS(PrefetchCount); err != nil {
		return nil, fmt.Errorf("failed to set qos: %w", err)
	}

	log.Println("✅ Timeline consumer initialized")

	hashtagBatcher := NewHashtagBatcher(redisClient, 500*time.Millisecond)

	return &TimelineConsumer{
		mq:                   mqClient,
		followRepo:           followRepo,
		followerPager:        followerPager,
		timelineCache:        timelineCache,
		redisClient:          redisClient,
		esClient:             esClient,
		qdrantClient:         qdrantClient, // 🆕 注入 Qdrant
		aiClient:             aiClient,
		outboxRepo:           outboxRepo, // 🆕 注入 Outbox 仓储
		hashtagBatcher:       hashtagBatcher,
		trendsProcessor:      trendsProcessor,
		moderationObserver:   noopModerationCleanupObserver{},
		tweetCreatedObserver: noopTweetCreatedObserver{},
		outboxObserver:       noopOutboxWorkerObserver{},
		trendProjector:       newTweetCreatedTrendProjector(redisClient),
		newOutboxTaskID:      snowflake.GenerateID,
		newOutboxLeaseToken:  uuid.NewString,
		outboxWorkerID:       newTimelineOutboxWorkerID(),
		outboxNow:            time.Now,
		failureRouter:        failureRouter,
	}, nil
}

func (c *TimelineConsumer) SetTweetCreatedObserver(observer TweetCreatedObserver) {
	if observer == nil {
		c.tweetCreatedObserver = noopTweetCreatedObserver{}
		return
	}
	c.tweetCreatedObserver = observer
}

func (c *TimelineConsumer) SetOutboxWorkerObserver(observer OutboxWorkerObserver) {
	if observer == nil {
		c.outboxObserver = noopOutboxWorkerObserver{}
		return
	}
	c.outboxObserver = observer
}

// Start 启动消费者
func (c *TimelineConsumer) Start(ctx context.Context) error {
	// 🆕 启动 hashtag 批量收集器
	c.hashtagBatcher.Start()

	// 🆕 启动 Redis 趋势话题时间衰减及清理协程（分布式锁防雪崩）
	go c.startTrendsDecayWorker(ctx)

	// 启动扇出消费者
	go c.consumeFanout(ctx)

	// 启动删除消费者
	go c.consumeDelete(ctx)

	go c.consumeModerationCleanup(ctx)

	// 🆕 监听点赞与评论事件以计算热度分值
	go c.consumeTweetLiked(ctx)
	go c.consumeCommentCreated(ctx)

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
	event, err := decodeTimelineTweetCreatedEvent(msg.Body)
	if err != nil {
		log.Printf("❌ Failed to unmarshal fanout event: %v", err)
		disposition := c.handlePermanentFailure(msg, RoutingKeyTweetCreated)
		c.observeTweetCreatedStage(tweetCreatedStageAck, string(disposition))
		return
	}

	log.Printf("📨 Received: tweet.created (tweet_id=%d, author_id=%d)", event.TweetID, event.AuthorID)
	ctx, cancel := context.WithTimeout(context.Background(), tweetCreatedStageTimeout)
	defer cancel()

	if err := c.fanoutToFollowers(ctx, event.AuthorID, event.TweetID); err != nil {
		c.observeTweetCreatedStage(tweetCreatedStageFanout, "failed")
		log.Printf("❌ Fanout failed: %v", err)
		disposition := c.handleFailure(msg, RoutingKeyTweetCreated)
		c.observeTweetCreatedStage(tweetCreatedStageAck, string(disposition))
		return
	}
	c.observeTweetCreatedStage(tweetCreatedStageFanout, "applied")

	trendApplied, err := c.processHashtags(ctx, event)
	if err != nil {
		c.observeTweetCreatedStage(tweetCreatedStageTrends, "failed")
		log.Printf("❌ Trend projection failed: %v", err)
		disposition := c.handleFailure(msg, RoutingKeyTweetCreated)
		c.observeTweetCreatedStage(tweetCreatedStageAck, string(disposition))
		return
	}
	if trendApplied {
		c.observeTweetCreatedStage(tweetCreatedStageTrends, "applied")
	} else {
		c.observeTweetCreatedStage(tweetCreatedStageTrends, "duplicate")
	}

	outboxCreated, err := c.enqueueSearchSync(ctx, event)
	if err != nil {
		c.observeTweetCreatedStage(tweetCreatedStageOutbox, "failed")
		log.Printf("❌ Failed to enqueue search sync: %v", err)
		disposition := c.handleFailure(msg, RoutingKeyTweetCreated)
		c.observeTweetCreatedStage(tweetCreatedStageAck, string(disposition))
		return
	}
	if outboxCreated {
		c.observeTweetCreatedStage(tweetCreatedStageOutbox, "applied")
	} else {
		c.observeTweetCreatedStage(tweetCreatedStageOutbox, "duplicate")
	}

	if err := msg.Ack(false); err != nil {
		c.observeTweetCreatedStage(tweetCreatedStageAck, "acknowledgement_uncertain")
		log.Printf("❌ Failed to ack message: %v", err)
		return
	}
	c.observeTweetCreatedStage(tweetCreatedStageAck, "completed")

	log.Printf("✅ Fanout completed: tweet_id=%d", event.TweetID)
}

// processHashtags 提取并给发帖增加热度，同时建立推文到实体的 48小时 Redis TTL 预映射
func (c *TimelineConsumer) processHashtags(ctx context.Context, event events.TweetCreatedEvent) (bool, error) {
	if c.trendProjector == nil {
		return false, fmt.Errorf("tweet created trend projector is unavailable")
	}
	var topics map[string]int64
	if c.trendsProcessor == nil {
		topics = extractFallbackTweetTopics(event.Content)
	} else {
		topics = c.trendsProcessor.ExtractTopics(event.Content)
	}
	return c.trendProjector.Project(ctx, event.TweetID, event.AuthorID, topics)
}

func (c *TimelineConsumer) enqueueSearchSync(ctx context.Context, event events.TweetCreatedEvent) (bool, error) {
	if c.outboxRepo == nil {
		return false, fmt.Errorf("outbox repository is unavailable")
	}
	if c.newOutboxTaskID == nil {
		return false, fmt.Errorf("outbox task ID generator is unavailable")
	}
	payloadBytes, err := json.Marshal(event)
	if err != nil {
		return false, fmt.Errorf("marshal search sync outbox payload: %w", err)
	}
	taskID, err := c.newOutboxTaskID()
	if err != nil {
		return false, fmt.Errorf("generate search sync outbox task ID: %w", err)
	}
	dedupKey := fmt.Sprintf("timeline:%s:tweet:%d:%s", syncESOutboxTaskType, event.TweetID, syncESOutboxDedupVersion)
	task := &domain.OutboxTask{
		ID:         taskID,
		DedupKey:   &dedupKey,
		TaskType:   syncESOutboxTaskType,
		Payload:    string(payloadBytes),
		Status:     domain.OutboxStatusPending,
		MaxRetries: 5,
	}
	created, err := c.outboxRepo.CreateIdempotent(ctx, task)
	if err != nil {
		return false, fmt.Errorf("create idempotent search sync outbox task: %w", err)
	}
	return created, nil
}

func (c *TimelineConsumer) observeTweetCreatedStage(stage, result string) {
	observer := c.tweetCreatedObserver
	if observer == nil {
		observer = noopTweetCreatedObserver{}
	}
	observer.ObserveStage(stage, result)
}

// consumeTweetLiked 监听推文点赞事件
func (c *TimelineConsumer) consumeTweetLiked(ctx context.Context) {
	messages, err := c.mq.Consume(QueueTweetLiked, ConsumerName+"-liked")
	if err != nil {
		log.Printf("❌ Failed to consume liked queue: %v", err)
		return
	}
	log.Println("📥 Listening for tweet.liked events...")
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-messages:
			if !ok {
				time.Sleep(5 * time.Second)
				messages, _ = c.mq.Consume(QueueTweetLiked, ConsumerName+"-liked")
				continue
			}
			c.handleTweetLikedMessage(msg)
		}
	}
}

// handleTweetLikedMessage 处理点赞消息并触发算分
func (c *TimelineConsumer) handleTweetLikedMessage(msg amqp.Delivery) {
	var event events.TweetLikedEvent
	if err := json.Unmarshal(msg.Body, &event); err != nil {
		log.Printf("❌ Failed to unmarshal liked event: %v", err)
		msg.Nack(false, false)
		return
	}

	_ = msg.Ack(false)
	go c.processEngagement(context.Background(), event.TweetID, event.UserID, 2) // 点赞权重 W_l = 2
}

// consumeCommentCreated 监听评论创建事件
func (c *TimelineConsumer) consumeCommentCreated(ctx context.Context) {
	messages, err := c.mq.Consume(QueueCommentCreated, ConsumerName+"-comment")
	if err != nil {
		log.Printf("❌ Failed to consume comment queue: %v", err)
		return
	}
	log.Println("📥 Listening for comment.created events...")
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-messages:
			if !ok {
				time.Sleep(5 * time.Second)
				messages, _ = c.mq.Consume(QueueCommentCreated, ConsumerName+"-comment")
				continue
			}
			c.handleCommentCreatedMessage(msg)
		}
	}
}

// handleCommentCreatedMessage 处理评论消息并触发算分
func (c *TimelineConsumer) handleCommentCreatedMessage(msg amqp.Delivery) {
	var event events.CommentCreatedEvent
	if err := json.Unmarshal(msg.Body, &event); err != nil {
		log.Printf("❌ Failed to unmarshal comment event: %v", err)
		msg.Nack(false, false)
		return
	}

	_ = msg.Ack(false)
	go c.processEngagement(context.Background(), event.TweetID, event.UserID, 5) // 评论权重 W_c = 5
}

// processEngagement 结合 48h TTL 映射与 1h 用户限频防刷进行多维加权算分
func (c *TimelineConsumer) processEngagement(ctx context.Context, tweetID uint64, userID uint64, eventWeight int64) {
	tagsKey := fmt.Sprintf("tweet_tags:%d", tweetID)
	tagsStr, err := c.redisClient.Get(ctx, tagsKey).Result()
	if err == redis.Nil {
		// TTL 过期或无此映射，不计入热搜（老帖子防刷与截断）
		return
	} else if err != nil {
		log.Printf("⚠️  Failed to query tweet tags from Redis: %v", err)
		return
	}

	tags := strings.Split(tagsStr, ",")
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}

		// 限频防刷：同一个用户针对同一个实体词，1 小时内最多计前 3 次互动
		limitKey := fmt.Sprintf("lock:user_tag_count:%d:%s", userID, tag)
		count, err := c.redisClient.Incr(ctx, limitKey).Result()
		if err != nil {
			log.Printf("⚠️  Failed to increment user tag limit: %v", err)
			continue
		}

		if count == 1 {
			_ = c.redisClient.Expire(ctx, limitKey, 1*time.Hour)
		}

		if count > 3 {
			continue
		}

		c.hashtagBatcher.AddWithScore(tag, eventWeight)
	}
}

// startTrendsDecayWorker 启动后台 Ticker 周期运行衰减
func (c *TimelineConsumer) startTrendsDecayWorker(ctx context.Context) {
	log.Println("🚀 Trends decay worker started")
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("⏹️  Trends decay worker stopped")
			return
		case <-ticker.C:
			c.decayTrends(ctx)
		}
	}
}

// decayTrends 执行分布式锁保护的时间衰减与长尾词即时截断，避免雪崩衰减与内存膨胀
func (c *TimelineConsumer) decayTrends(ctx context.Context) {
	decayCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// 1. 获取分布式锁，防止多副本重复衰减（雪崩衰减）。锁有效期为 50 秒。
	lockKey := "lock:trends_decay"
	success, err := c.redisClient.SetNX(decayCtx, lockKey, "1", 50*time.Second).Result()
	if err != nil {
		log.Printf("⚠️  Failed to acquire trends decay lock: %v", err)
		return
	}
	if !success {
		// 抢锁失败说明其他副本已在此分钟内执行过衰减
		return
	}

	log.Println("🔒 Acquired trends decay lock, executing decay and cleanup...")

	// 2. 使用 ZINTERSTORE 对整个 trends:global 乘以权重系数 0.95 进行指数衰减
	store := redis.ZStore{
		Keys:    []string{"trends:global"},
		Weights: []float64{0.95},
	}

	err = c.redisClient.ZInterStore(decayCtx, "trends:global", &store).Err()
	if err != nil {
		log.Printf("⚠️  Failed to decay trends:global ZSet: %v", err)
		return
	}

	// 3. 顺手物理裁剪长尾词，仅保留前 100 名，防止 Redis 内存膨胀
	err = c.redisClient.ZRemRangeByRank(decayCtx, "trends:global", 0, -101).Err()
	if err != nil {
		log.Printf("⚠️  Failed to clean up long-tail trends: %v", err)
	}
}

// fanoutToFollowers 扇出到粉丝
func (c *TimelineConsumer) fanoutToFollowers(ctx context.Context, authorID uint64, tweetID uint64) error {
	// 1. 检查是否为大V (获取发推人粉丝数)
	isCelebrity, err := c.timelineCache.IsCelebrity(ctx, authorID)
	if err != nil {
		log.Printf("⚠️  Failed to check celebrity status for user %d: %v", authorID, err)
		// 如果 Redis 出错，降级走普通写扩散流程，不影响正常发布
	} else if isCelebrity {
		log.Printf("📢 [Celebrity Push Avoided] Author %d is a celebrity. Skipping write-diffusion fanout.", authorID)
		// 🆕 将推文写入大V个人时间线缓存，以供粉丝拉取使用 (L2 Pull 缓存)
		if cacheErr := c.timelineCache.AddToUserTimeline(ctx, authorID, tweetID); cacheErr != nil {
			return fmt.Errorf("add celebrity tweet to user timeline: %w", cacheErr)
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
			return fmt.Errorf("fanout batch %d-%d: %w", i, end, err)
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
	event, err := decodeTimelineTweetDeletedEvent(msg.Body)
	if err != nil {
		log.Printf("❌ Failed to unmarshal delete event: %v", err)
		c.handlePermanentFailure(msg, RoutingKeyTweetDeleted)
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

// handleFailure 消息消费失败通用处理函数：实现指数退避重试或路由到死信队列
func (c *TimelineConsumer) handleFailure(msg amqp.Delivery, routingKeySuffix string) failureDisposition {
	if c.failureRouter == nil {
		log.Printf("timeline failure router unavailable: routing_key=%s", routingKeySuffix)
		time.Sleep(time.Second)
		if err := msg.Nack(false, true); err != nil {
			log.Printf("timeline failure fallback requeue failed: error=%v", err)
		}
		return failureDispositionRequeued
	}
	return c.failureRouter.route(msg, routingKeySuffix, false)
}

func (c *TimelineConsumer) handlePermanentFailure(msg amqp.Delivery, routingKeySuffix string) failureDisposition {
	if c.failureRouter == nil {
		return c.handleFailure(msg, routingKeySuffix)
	}
	return c.failureRouter.route(msg, routingKeySuffix, true)
}

// StartOutboxWorker 启动发件箱后台对账守护协程
func (c *TimelineConsumer) StartOutboxWorker(ctx context.Context) {
	log.Println("🚀 Outbox worker daemon started")
	workerTicker := time.NewTicker(outboxWorkerInterval)
	cleanupTicker := time.NewTicker(outboxCleanupInterval)
	defer workerTicker.Stop()
	defer cleanupTicker.Stop()
	c.processOutboxTasks(ctx)

	for {
		select {
		case <-ctx.Done():
			log.Println("⏹️  Outbox worker daemon stopped")
			return
		case <-workerTicker.C:
			c.processOutboxTasks(ctx)
		case <-cleanupTicker.C:
			c.cleanupCompletedOutboxTasks(ctx)
		}
	}
}

// processOutboxTasks recovers expired attempts, atomically claims work, and executes the batch concurrently.
func (c *TimelineConsumer) processOutboxTasks(ctx context.Context) {
	if c.outboxRepo == nil {
		log.Println("Outbox worker: repository is unavailable")
		c.observeOutbox(outboxOperationClaim, "failed", 1)
		return
	}

	now := c.outboxCurrentTime().UnixMilli()
	recovery, err := c.outboxRepo.RecoverExpiredClaims(ctx, now, outboxRecoveryBatchSize)
	if err != nil {
		log.Printf("Outbox worker: failed to recover expired claims: %v", err)
		c.observeOutbox(outboxOperationRecover, "failed", 1)
		return
	}
	c.observeOutbox(outboxOperationRecover, "retryable", int(recovery.Retryable))
	c.observeOutbox(outboxOperationRecover, "exhausted", int(recovery.Exhausted))

	claimedAt := c.outboxCurrentTime()
	tasks, err := c.outboxRepo.Claim(ctx, domain.OutboxClaimRequest{
		LeaseOwner:          c.outboxWorkerIdentity(),
		LeaseToken:          c.outboxLeaseToken(),
		ClaimedAtUnixMilli:  claimedAt.UnixMilli(),
		LeaseUntilUnixMilli: claimedAt.Add(outboxLeaseDuration).UnixMilli(),
		Limit:               outboxClaimBatchSize,
	})
	if err != nil {
		log.Printf("Outbox worker: failed to claim tasks: %v", err)
		c.observeOutbox(outboxOperationClaim, "failed", 1)
		return
	}

	if len(tasks) == 0 {
		c.observeOutbox(outboxOperationClaim, "empty", 1)
		return
	}
	c.observeOutbox(outboxOperationClaim, "claimed", len(tasks))

	log.Printf("Outbox worker: processing %d claimed tasks", len(tasks))

	var workers sync.WaitGroup
	for _, task := range tasks {
		claimedTask := task
		workers.Add(1)
		go func() {
			defer workers.Done()
			c.processClaimedOutboxTask(ctx, claimedTask)
		}()
	}
	workers.Wait()
}

func (c *TimelineConsumer) processClaimedOutboxTask(ctx context.Context, task *domain.OutboxTask) {
	if task == nil {
		c.observeOutbox(outboxOperationExecute, "failed", 1)
		return
	}
	if task.TaskType != syncESOutboxTaskType {
		log.Printf("Outbox worker: unknown task type %s for task %d", task.TaskType, task.ID)
		c.observeOutbox(outboxOperationExecute, "failed", 1)
		c.failOutboxClaim(ctx, task, "unknown outbox task type", true)
		return
	}

	var event events.TweetCreatedEvent
	if err := json.Unmarshal([]byte(task.Payload), &event); err != nil {
		log.Printf("Outbox worker: invalid payload for task %d: %v", task.ID, err)
		c.observeOutbox(outboxOperationExecute, "failed", 1)
		c.failOutboxClaim(ctx, task, "invalid outbox payload", true)
		return
	}

	taskCtx, cancel := context.WithTimeout(ctx, outboxTaskTimeout)
	executor := c.executeESIndex
	if c.searchSyncExecutor != nil {
		executor = c.searchSyncExecutor
	}
	err := executor(taskCtx, &event)
	cancel()
	if err != nil {
		log.Printf("Outbox worker: search sync failed for task %d: %v", task.ID, err)
		c.observeOutbox(outboxOperationExecute, "failed", 1)
		c.failOutboxClaim(ctx, task, err.Error(), task.Retries >= task.MaxRetries)
		return
	}

	c.observeOutbox(outboxOperationExecute, "succeeded", 1)
	c.completeOutboxClaim(ctx, task)
}

func (c *TimelineConsumer) completeOutboxClaim(parent context.Context, task *domain.OutboxTask) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), outboxFinalizeTimeout)
	defer cancel()
	committed, err := c.outboxRepo.CompleteClaim(ctx, domain.OutboxClaimCompletion{
		TaskID:               task.ID,
		LeaseOwner:           task.LeaseOwner,
		LeaseToken:           task.LeaseToken,
		CompletedAtUnixMilli: c.outboxCurrentTime().UnixMilli(),
	})
	if err != nil {
		log.Printf("Outbox worker: failed to complete claim for task %d: %v", task.ID, err)
		c.observeOutbox(outboxOperationFinalize, "failed", 1)
		return
	}
	if !committed {
		log.Printf("Outbox worker: ignored stale completion for task %d", task.ID)
		c.observeOutbox(outboxOperationFinalize, "stale", 1)
		return
	}
	c.observeOutbox(outboxOperationFinalize, "succeeded", 1)
	log.Printf("Outbox worker: successfully synced search indexes for task %d", task.ID)
}

func (c *TimelineConsumer) failOutboxClaim(parent context.Context, task *domain.OutboxTask, reason string, terminal bool) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), outboxFinalizeTimeout)
	defer cancel()
	released, err := c.outboxRepo.FailClaim(ctx, domain.OutboxClaimFailure{
		TaskID:            task.ID,
		LeaseOwner:        task.LeaseOwner,
		LeaseToken:        task.LeaseToken,
		FailedAtUnixMilli: c.outboxCurrentTime().UnixMilli(),
		ErrorMsg:          reason,
		Terminal:          terminal,
	})
	if err != nil {
		log.Printf("Outbox worker: failed to release claim for task %d: %v", task.ID, err)
		c.observeOutbox(outboxOperationFinalize, "failed", 1)
		return
	}
	if !released {
		log.Printf("Outbox worker: ignored stale failure for task %d", task.ID)
		c.observeOutbox(outboxOperationFinalize, "stale", 1)
		return
	}
	c.observeOutbox(outboxOperationFinalize, "released", 1)
}

func (c *TimelineConsumer) cleanupCompletedOutboxTasks(ctx context.Context) {
	if c.outboxRepo == nil {
		c.observeOutbox(outboxOperationCleanup, "failed", 1)
		return
	}
	cutoff := c.outboxCurrentTime().Add(-outboxSuccessRetention).UnixMilli()
	var total int64
	for batch := 0; batch < outboxCleanupMaxBatches; batch++ {
		deleted, err := c.outboxRepo.DeleteCompletedBefore(ctx, cutoff, outboxCleanupBatchSize)
		if err != nil {
			log.Printf("⚠️  Outbox worker: failed to clean completed receipts: %v", err)
			c.observeOutbox(outboxOperationCleanup, "failed", 1)
			return
		}
		total += deleted
		if deleted < outboxCleanupBatchSize {
			break
		}
	}
	if total > 0 {
		c.observeOutbox(outboxOperationCleanup, "deleted", int(total))
		log.Printf("🧹 Outbox worker: deleted %d expired success receipts", total)
		return
	}
	c.observeOutbox(outboxOperationCleanup, "empty", 1)
}

func (c *TimelineConsumer) outboxWorkerIdentity() string {
	if strings.TrimSpace(c.outboxWorkerID) == "" {
		c.outboxWorkerID = newTimelineOutboxWorkerID()
	}
	return c.outboxWorkerID
}

func (c *TimelineConsumer) outboxLeaseToken() string {
	if c.newOutboxLeaseToken == nil {
		return uuid.NewString()
	}
	return c.newOutboxLeaseToken()
}

func (c *TimelineConsumer) outboxCurrentTime() time.Time {
	if c.outboxNow == nil {
		return time.Now()
	}
	return c.outboxNow()
}

func (c *TimelineConsumer) observeOutbox(operation, result string, count int) {
	if c.outboxObserver == nil {
		return
	}
	c.outboxObserver.ObserveOutbox(operation, result, count)
}

func newTimelineOutboxWorkerID() string {
	if configured := strings.TrimSpace(os.Getenv("TIMELINE_OUTBOX_WORKER_ID")); configured != "" {
		return boundedOutboxWorkerID(configured)
	}
	hostname, err := os.Hostname()
	if err != nil || strings.TrimSpace(hostname) == "" {
		hostname = "unknown-host"
	}
	identity := fmt.Sprintf("%s:%s:%d:%s", ConsumerName, hostname, os.Getpid(), uuid.NewString())
	return boundedOutboxWorkerID(identity)
}

func boundedOutboxWorkerID(value string) string {
	const maxBytes = 191
	value = strings.TrimSpace(value)
	if len(value) <= maxBytes {
		return value
	}
	end := maxBytes
	for end > 0 && !utf8.RuneStart(value[end]) {
		end--
	}
	return value[:end]
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
