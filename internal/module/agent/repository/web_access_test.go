package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	agentWebSearch "twitter-clone/internal/module/agent/websearch"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
)

func TestRedisWebCacheRoundTripAndTTL(t *testing.T) {
	t.Parallel()

	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cache, err := NewRedisWebCache(client, "test:web")
	if err != nil {
		t.Fatalf("NewRedisWebCache() error = %v", err)
	}
	if err := cache.Set(context.Background(), "key", []byte(`{"value":1}`), time.Minute); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	payload, found, err := cache.Get(context.Background(), "key")
	if err != nil || !found || string(payload) != `{"value":1}` {
		t.Fatalf("Get() = %q, %t, %v", payload, found, err)
	}
	server.FastForward(2 * time.Minute)
	_, found, err = cache.Get(context.Background(), "key")
	if err != nil || found {
		t.Fatalf("expired Get() found=%t error=%v", found, err)
	}
}

func TestRedisWebAccessGovernorEnforcesUserAndRunBudgets(t *testing.T) {
	t.Parallel()

	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	governor, err := NewRedisWebAccessGovernor(client, RedisWebAccessGovernorConfig{
		UserWindow: time.Minute, UserMaxRequests: 2,
		RunTTL: time.Hour, RunMaxRequests: 2, RunMaxCostMicros: 150,
	})
	if err != nil {
		t.Fatalf("NewRedisWebAccessGovernor() error = %v", err)
	}
	request := agentWebSearch.AdmissionRequest{
		Subject:   agentWebSearch.AccessSubject{UserID: 7, RunID: "run-1"},
		Operation: agentWebSearch.AccessOperationSearch, CostMicros: 75,
	}
	if err := governor.Admit(context.Background(), request); err != nil {
		t.Fatalf("first Admit() error = %v", err)
	}
	if err := governor.Admit(context.Background(), request); err != nil {
		t.Fatalf("second Admit() error = %v", err)
	}
	if err := governor.Admit(context.Background(), request); !errors.Is(err, agentWebSearch.ErrAccessRateLimited) {
		t.Fatalf("third Admit() error = %v", err)
	}

	costGovernor, err := NewRedisWebAccessGovernor(client, RedisWebAccessGovernorConfig{
		Prefix: "test:cost", UserWindow: time.Minute, UserMaxRequests: 10,
		RunTTL: time.Hour, RunMaxRequests: 10, RunMaxCostMicros: 100,
	})
	if err != nil {
		t.Fatalf("NewRedisWebAccessGovernor() error = %v", err)
	}
	if err := costGovernor.Admit(context.Background(), request); err != nil {
		t.Fatalf("cost Admit() error = %v", err)
	}
	if err := costGovernor.Admit(context.Background(), request); !errors.Is(err, agentWebSearch.ErrAccessBudgetExceeded) {
		t.Fatalf("cost limit error = %v", err)
	}
}

func TestRedisWebAccessGovernorRequiresIdentityAndFailsClosed(t *testing.T) {
	t.Parallel()

	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	governor, err := NewRedisWebAccessGovernor(client, RedisWebAccessGovernorConfig{})
	if err != nil {
		t.Fatalf("NewRedisWebAccessGovernor() error = %v", err)
	}
	if err := governor.Admit(context.Background(), agentWebSearch.AdmissionRequest{}); !errors.Is(err, agentWebSearch.ErrAccessIdentityRequired) {
		t.Fatalf("identity error = %v", err)
	}
	_ = client.Close()
	err = governor.Admit(context.Background(), agentWebSearch.AdmissionRequest{
		Subject:   agentWebSearch.AccessSubject{UserID: 7, RunID: "run-1"},
		Operation: agentWebSearch.AccessOperationSearch,
	})
	if !errors.Is(err, agentWebSearch.ErrAccessGovernor) {
		t.Fatalf("closed Redis error = %v", err)
	}
}

func TestRedisWebAccessGovernorNormalizesSubMillisecondWindows(t *testing.T) {
	t.Parallel()

	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	governor, err := NewRedisWebAccessGovernor(client, RedisWebAccessGovernorConfig{
		UserWindow: time.Nanosecond,
		RunTTL:     time.Nanosecond,
	})
	if err != nil {
		t.Fatalf("NewRedisWebAccessGovernor() error = %v", err)
	}
	if governor.userWindow != DefaultWebUserWindow || governor.runTTL != DefaultWebRunTTL {
		t.Fatalf("normalized windows = user:%s run:%s", governor.userWindow, governor.runTTL)
	}
	if err := governor.Admit(context.Background(), agentWebSearch.AdmissionRequest{
		Subject:   agentWebSearch.AccessSubject{UserID: 7, RunID: "run-sub-millisecond"},
		Operation: agentWebSearch.AccessOperationSearch,
	}); err != nil {
		t.Fatalf("Admit() error = %v", err)
	}
}

func TestRedisWebCacheRejectsSubMillisecondTTL(t *testing.T) {
	t.Parallel()

	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cache, err := NewRedisWebCache(client, "test:web:ttl")
	if err != nil {
		t.Fatalf("NewRedisWebCache() error = %v", err)
	}
	if err := cache.Set(context.Background(), "key", []byte(`{"value":1}`), time.Nanosecond); err == nil {
		t.Fatal("Set() accepted a sub-millisecond TTL")
	}
}
