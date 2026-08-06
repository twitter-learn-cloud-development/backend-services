package service

import (
	"context"
	"errors"
	"log"
	"time"

	"twitter-clone/internal/module/agent/profile"
)

const DefaultProfileCatalogSyncInterval = 30 * time.Second

type ProfileCatalogSynchronizer struct {
	manager       *ProfileCatalogManager
	bus           profile.CatalogChangeBus
	syncInterval  time.Duration
	retryInterval time.Duration
}

func NewProfileCatalogSynchronizer(
	manager *ProfileCatalogManager,
	bus profile.CatalogChangeBus,
	syncInterval time.Duration,
) (*ProfileCatalogSynchronizer, error) {
	if manager == nil || bus == nil {
		return nil, errors.New("profile catalog manager and change bus are required")
	}
	if syncInterval <= 0 {
		syncInterval = DefaultProfileCatalogSyncInterval
	}
	return &ProfileCatalogSynchronizer{
		manager:       manager,
		bus:           bus,
		syncInterval:  syncInterval,
		retryInterval: time.Second,
	}, nil
}

func (s *ProfileCatalogSynchronizer) Run(ctx context.Context) {
	if s == nil || ctx == nil {
		return
	}
	ticker := time.NewTicker(s.syncInterval)
	retry := time.NewTimer(0)
	defer ticker.Stop()
	defer retry.Stop()

	var subscription profile.CatalogChangeSubscription
	var events <-chan profile.CatalogChangeEvent
	var subscriptionErrors <-chan error
	defer func() {
		if subscription != nil {
			_ = subscription.Close()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-retry.C:
			if subscription != nil {
				continue
			}
			created, err := s.bus.SubscribeCatalogChanges(ctx)
			if err != nil {
				log.Printf("profile catalog change subscription failed: %v", err)
				retry.Reset(s.retryInterval)
				continue
			}
			subscription = created
			events = created.Events()
			subscriptionErrors = created.Errors()
		case <-ticker.C:
			s.reload(ctx, "periodic anti-entropy")
		case _, ok := <-events:
			if !ok {
				_ = subscription.Close()
				subscription = nil
				events = nil
				subscriptionErrors = nil
				retry.Reset(s.retryInterval)
				continue
			}
			s.reload(ctx, "catalog change event")
		case err, ok := <-subscriptionErrors:
			if !ok {
				subscriptionErrors = nil
				continue
			}
			if err != nil {
				log.Printf("profile catalog change event ignored: %v", err)
			}
		}
	}
}

func (s *ProfileCatalogSynchronizer) reload(ctx context.Context, reason string) {
	if err := s.manager.Reload(ctx); err != nil {
		log.Printf("profile catalog reload failed after %s: %v", reason, err)
	}
}
