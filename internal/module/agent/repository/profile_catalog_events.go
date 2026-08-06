package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/go-redis/redis/v8"

	"twitter-clone/internal/module/agent/profile"
)

type RedisProfileCatalogChangeBus struct {
	client  *redis.Client
	channel string
}

func NewRedisProfileCatalogChangeBus(client *redis.Client, channel string) (*RedisProfileCatalogChangeBus, error) {
	if client == nil {
		return nil, errors.New("profile catalog change redis client is required")
	}
	channel = strings.TrimSpace(channel)
	if channel == "" {
		return nil, errors.New("profile catalog change channel is required")
	}
	return &RedisProfileCatalogChangeBus{client: client, channel: channel}, nil
}

func (b *RedisProfileCatalogChangeBus) PublishCatalogChange(ctx context.Context, event profile.CatalogChangeEvent) error {
	if b == nil || b.client == nil {
		return errors.New("profile catalog change bus is unavailable")
	}
	if err := event.Validate(); err != nil {
		return err
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode profile catalog change event: %w", err)
	}
	if err := b.client.Publish(ctx, b.channel, payload).Err(); err != nil {
		return fmt.Errorf("publish profile catalog change event: %w", err)
	}
	return nil
}

func (b *RedisProfileCatalogChangeBus) SubscribeCatalogChanges(ctx context.Context) (profile.CatalogChangeSubscription, error) {
	if b == nil || b.client == nil {
		return nil, errors.New("profile catalog change bus is unavailable")
	}
	if ctx == nil {
		return nil, errors.New("profile catalog change context is required")
	}
	pubsub := b.client.Subscribe(ctx, b.channel)
	if _, err := pubsub.Receive(ctx); err != nil {
		_ = pubsub.Close()
		return nil, fmt.Errorf("subscribe profile catalog changes: %w", err)
	}
	subCtx, cancel := context.WithCancel(ctx)
	subscription := &redisProfileCatalogSubscription{
		pubsub: pubsub,
		cancel: cancel,
		events: make(chan profile.CatalogChangeEvent),
		errors: make(chan error, 1),
	}
	go subscription.consume(subCtx)
	return subscription, nil
}

type redisProfileCatalogSubscription struct {
	pubsub    *redis.PubSub
	cancel    context.CancelFunc
	events    chan profile.CatalogChangeEvent
	errors    chan error
	closeOnce sync.Once
}

func (s *redisProfileCatalogSubscription) Events() <-chan profile.CatalogChangeEvent { return s.events }
func (s *redisProfileCatalogSubscription) Errors() <-chan error                      { return s.errors }

func (s *redisProfileCatalogSubscription) Close() error {
	var closeErr error
	s.closeOnce.Do(func() {
		s.cancel()
		closeErr = s.pubsub.Close()
	})
	return closeErr
}

func (s *redisProfileCatalogSubscription) consume(ctx context.Context) {
	defer close(s.events)
	defer close(s.errors)
	channel := s.pubsub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case message, ok := <-channel:
			if !ok {
				s.reportError(errors.New("profile catalog change subscription closed"))
				return
			}
			event, err := decodeProfileCatalogChange([]byte(message.Payload))
			if err != nil {
				s.reportError(err)
				continue
			}
			select {
			case s.events <- event:
			case <-ctx.Done():
				return
			}
		}
	}
}

func (s *redisProfileCatalogSubscription) reportError(err error) {
	select {
	case s.errors <- err:
	default:
	}
}

func decodeProfileCatalogChange(payload []byte) (profile.CatalogChangeEvent, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var event profile.CatalogChangeEvent
	if err := decoder.Decode(&event); err != nil {
		return profile.CatalogChangeEvent{}, fmt.Errorf("decode profile catalog change event: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return profile.CatalogChangeEvent{}, errors.New("decode profile catalog change event: trailing JSON value")
	}
	if err := event.Validate(); err != nil {
		return profile.CatalogChangeEvent{}, fmt.Errorf("validate profile catalog change event: %w", err)
	}
	return event, nil
}
