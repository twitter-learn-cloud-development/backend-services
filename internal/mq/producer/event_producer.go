package producer

import (
	"context"
	"fmt"
	"log"

	"twitter-clone/internal/events"
	"twitter-clone/internal/infrastructure/mq"
)

const (
	// ExchangeName Exchange 名称
	ExchangeName = "twitter.events"

	// ExchangeType Exchange 类型
	ExchangeType = "topic"

	// RoutingKeyTweetCreated 推文创建路由键
	RoutingKeyTweetCreated = "tweet.created"

	// RoutingKeyTweetDeleted 推文删除路由键
	RoutingKeyTweetDeleted = "tweet.deleted"

	// RoutingKeyUserFollowed 用户关注路由键
	RoutingKeyUserFollowed = "user.followed"

	// RoutingKeyUserUnfollowed 用户取关路由键
	RoutingKeyUserUnfollowed = "user.unfollowed"
)

// EventProducer 事件生产者
type EventProducer struct {
	mq *mq.RabbitMQ
}

// NewEventProducer 创建事件生产者
func NewEventProducer(mqClient *mq.RabbitMQ) (*EventProducer, error) {
	// 声明 Exchange
	if err := mqClient.DeclareExchange(ExchangeName, ExchangeType, true); err != nil {
		return nil, fmt.Errorf("failed to declare exchange: %w", err)
	}

	log.Printf("✅ Exchange declared: %s (type: %s)", ExchangeName, ExchangeType)

	return &EventProducer{mq: mqClient}, nil
}

// PublishTweetCreated 发布推文创建事件
func (p *EventProducer) PublishTweetCreated(ctx context.Context, event *events.TweetCreatedEvent) error {
	body, err := event.ToJSON()
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	if err := p.mq.Publish(ctx, ExchangeName, RoutingKeyTweetCreated, body); err != nil {
		return fmt.Errorf("failed to publish tweet created event: %w", err)
	}

	log.Printf("📤 Published: tweet.created (tweet_id=%d, author_id=%d)", event.TweetID, event.AuthorID)
	return nil
}

// PublishTweetDeleted 发布推文删除事件
func (p *EventProducer) PublishTweetDeleted(ctx context.Context, event *events.TweetDeletedEvent) error {
	body, err := event.ToJSON()
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	if err := p.mq.Publish(ctx, ExchangeName, RoutingKeyTweetDeleted, body); err != nil {
		return fmt.Errorf("failed to publish tweet deleted event: %w", err)
	}

	log.Printf("📤 Published: tweet.deleted (tweet_id=%d, author_id=%d)", event.TweetID, event.AuthorID)
	return nil
}

// PublishUserFollowed 发布用户关注事件
func (p *EventProducer) PublishUserFollowed(ctx context.Context, event *events.UserFollowedEvent) error {
	body, err := event.ToJSON()
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	if err := p.mq.Publish(ctx, ExchangeName, RoutingKeyUserFollowed, body); err != nil {
		return fmt.Errorf("failed to publish user followed event: %w", err)
	}

	log.Printf("📤 Published: user.followed (follower_id=%d, followee_id=%d)", event.FollowerID, event.FolloweeID)
	return nil
}

// PublishUserUnfollowed 发布用户取关事件
func (p *EventProducer) PublishUserUnfollowed(ctx context.Context, event *events.UserUnfollowedEvent) error {
	body, err := event.ToJSON()
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	if err := p.mq.Publish(ctx, ExchangeName, RoutingKeyUserUnfollowed, body); err != nil {
		return fmt.Errorf("failed to publish user unfollowed event: %w", err)
	}

	log.Printf("📤 Published: user.unfollowed (follower_id=%d, followee_id=%d)", event.FollowerID, event.FolloweeID)
	return nil
}
