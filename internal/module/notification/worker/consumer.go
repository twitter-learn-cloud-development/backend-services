package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/go-redis/redis/v8"
	amqp "github.com/rabbitmq/amqp091-go"

	"twitter-clone/internal/domain"
	"twitter-clone/internal/events"
	"twitter-clone/internal/infrastructure/mq"
)

// Consumer 负责消费事件并创建通知
type Consumer struct {
	mq       *mq.RabbitMQ
	repo     domain.NotificationRepository
	redis    *redis.Client
	queue    string
	exchange string
}

// NewConsumer 创建消费者
func NewConsumer(mq *mq.RabbitMQ, repo domain.NotificationRepository, rdb *redis.Client) *Consumer {
	return &Consumer{
		mq:       mq,
		repo:     repo,
		redis:    rdb,
		queue:    "notification.events",
		exchange: "twitter.events",
	}
}

// Start 启动消费者
func (c *Consumer) Start(ctx context.Context) error {
	// 1. 声明 Queue
	_, err := c.mq.GetChannel().QueueDeclare(
		c.queue, // name
		true,    // durable
		false,   // delete when unused
		false,   // exclusive
		false,   // no-wait
		nil,     // arguments
	)
	if err != nil {
		return fmt.Errorf("failed to declare queue: %w", err)
	}

	// 2. 绑定 Queue 到 Exchange (Topic)
	routingKeys := []string{
		"tweet.liked",
		"comment.created",
		"user.followed",
	}
	for _, key := range routingKeys {
		if err := c.mq.GetChannel().QueueBind(
			c.queue,    // queue name
			key,        // routing key
			c.exchange, // exchange
			false,
			nil,
		); err != nil {
			return fmt.Errorf("failed to bind queue key=%s: %w", key, err)
		}
	}

	// 3. 消费消息
	msgs, err := c.mq.GetChannel().Consume(
		c.queue, // queue
		"",      // consumer
		true,    // auto-ack
		false,   // exclusive
		false,   // no-local
		false,   // no-wait
		nil,     // args
	)
	if err != nil {
		return fmt.Errorf("failed to consume: %w", err)
	}

	go func() {
		for d := range msgs {
			if err := c.handleMessage(ctx, d); err != nil {
				log.Printf("❌ Failed to handle message: %v", err)
			}
		}
	}()

	log.Println("✅ Notification Consumer started")
	return nil
}

// handleMessage 处理消息
func (c *Consumer) handleMessage(ctx context.Context, d amqp.Delivery) error {
	switch d.RoutingKey {
	case "tweet.liked":
		return c.handleTweetLiked(ctx, d.Body)
	case "comment.created":
		return c.handleCommentCreated(ctx, d.Body)
	case "user.followed":
		return c.handleUserFollowed(ctx, d.Body)
	default:
		return nil
	}
}

// handleTweetLiked 处理点赞事件
func (c *Consumer) handleTweetLiked(ctx context.Context, body []byte) error {
	var event events.TweetLikedEvent
	if err := json.Unmarshal(body, &event); err != nil {
		return err
	}

	// 自己给自己点赞不通知
	if event.UserID == event.TweetUser {
		return nil
	}

	notification := &domain.Notification{
		UserID:   event.TweetUser,
		ActorID:  event.UserID,
		Type:     domain.NotificationTypeLike,
		TargetID: event.TweetID,
		Content:  "", // 点赞无需内容
	}

	return c.createAndPush(ctx, notification)
}

// handleCommentCreated 处理评论事件
func (c *Consumer) handleCommentCreated(ctx context.Context, body []byte) error {
	var event events.CommentCreatedEvent
	if err := json.Unmarshal(body, &event); err != nil {
		return err
	}

	// 自己给自己评论不通知
	if event.UserID == event.TweetUser {
		return nil
	}

	notification := &domain.Notification{
		UserID:   event.TweetUser,
		ActorID:  event.UserID,
		Type:     domain.NotificationTypeComment,
		TargetID: event.TweetID, // 关联推文 ID
		Content:  event.Content,
	}

	return c.createAndPush(ctx, notification)
}

// handleUserFollowed 处理关注事件
func (c *Consumer) handleUserFollowed(ctx context.Context, body []byte) error {
	var event events.UserFollowedEvent
	if err := json.Unmarshal(body, &event); err != nil {
		return err
	}

	notification := &domain.Notification{
		UserID:   event.FolloweeID,
		ActorID:  event.FollowerID,
		Type:     domain.NotificationTypeFollow,
		TargetID: event.FollowerID, // 关联关注者 ID
		Content:  "",
	}

	return c.createAndPush(ctx, notification)
}

// createAndPush 创建通知并推送
func (c *Consumer) createAndPush(ctx context.Context, n *domain.Notification) error {
	// 1. 保存到 DB
	if err := c.repo.Create(ctx, n); err != nil {
		return err
	}

	// 2. 推送到 Redis Pub/Sub (用于 WebSocket 实时通知)
	channel := fmt.Sprintf("notifications:user:%d", n.UserID)
	msg, _ := json.Marshal(n)

	if err := c.redis.Publish(ctx, channel, msg).Err(); err != nil {
		log.Printf("⚠️ Failed to publish to redis: %v", err)
	} else {
		log.Printf("📢 Notification pushed to user %d via Redis", n.UserID)
	}

	return nil
}
