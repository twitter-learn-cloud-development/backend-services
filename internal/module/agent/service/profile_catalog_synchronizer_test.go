package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"twitter-clone/internal/module/agent/profile"
)

func TestProfileCatalogSynchronizerReloadsOnChangeEvent(t *testing.T) {
	repo := newFakeProfileCatalogRepository()
	resolver := newTestAtomicProfileResolver(t, nil)
	manager, err := NewProfileCatalogManager(repo, resolver, nil)
	if err != nil {
		t.Fatalf("NewProfileCatalogManager() error = %v", err)
	}
	bus := newFakeProfileChangeBus()
	synchronizer, err := NewProfileCatalogSynchronizer(manager, bus, time.Hour)
	if err != nil {
		t.Fatalf("NewProfileCatalogSynchronizer() error = %v", err)
	}

	candidate := testManagedProfile("custom.remote", "v1")
	record := fakePublishedProfileRecord(t, candidate)
	repo.mu.Lock()
	repo.versions[profileVersionKey(candidate.ID, candidate.Version)] = record
	repo.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go synchronizer.Run(ctx)
	bus.waitSubscribed(t)
	bus.subscription.events <- profile.CatalogChangeEvent{
		Schema:               profile.CatalogChangeSchemaV1,
		OperationID:          "remote-operation",
		ProfileID:            candidate.ID,
		VersionRevision:      2,
		OccurredAtUnixMillis: time.Now().UnixMilli(),
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		selected, resolveErr := resolver.Resolve(context.Background(), candidate.ID, profile.SelectionSubject{UserID: 7})
		if resolveErr == nil && selected.Version == candidate.Version {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("remote profile catalog change was not activated")
}

type fakeProfileChangeBus struct {
	mu           sync.Mutex
	subscription *fakeProfileChangeSubscription
	subscribed   chan struct{}
}

func newFakeProfileChangeBus() *fakeProfileChangeBus {
	return &fakeProfileChangeBus{subscribed: make(chan struct{})}
}

func (b *fakeProfileChangeBus) PublishCatalogChange(context.Context, profile.CatalogChangeEvent) error {
	return nil
}

func (b *fakeProfileChangeBus) SubscribeCatalogChanges(context.Context) (profile.CatalogChangeSubscription, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.subscription == nil {
		b.subscription = &fakeProfileChangeSubscription{
			events: make(chan profile.CatalogChangeEvent, 1),
			errors: make(chan error, 1),
		}
		close(b.subscribed)
	}
	return b.subscription, nil
}

func (b *fakeProfileChangeBus) waitSubscribed(t *testing.T) {
	t.Helper()
	select {
	case <-b.subscribed:
	case <-time.After(time.Second):
		t.Fatal("synchronizer did not subscribe")
	}
}

type fakeProfileChangeSubscription struct {
	events chan profile.CatalogChangeEvent
	errors chan error
	once   sync.Once
}

func (s *fakeProfileChangeSubscription) Events() <-chan profile.CatalogChangeEvent { return s.events }
func (s *fakeProfileChangeSubscription) Errors() <-chan error                      { return s.errors }
func (s *fakeProfileChangeSubscription) Close() error {
	s.once.Do(func() {
		close(s.events)
		close(s.errors)
	})
	return nil
}
