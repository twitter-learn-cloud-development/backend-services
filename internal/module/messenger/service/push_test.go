package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
	messengerv1 "twitter-clone/api/messenger/v1"
	"twitter-clone/internal/domain"
)

func pushCounterValue(t *testing.T, counter prometheus.Counter) float64 {
	t.Helper()
	var metric dto.Metric
	require.NoError(t, counter.Write(&metric))
	return metric.GetCounter().GetValue()
}

type sendRepository struct {
	domain.MessageRepository
	stored *domain.Message
	err    error
}

func (r *sendRepository) Create(_ context.Context, m *domain.Message) error {
	if r.err == nil {
		r.stored = m
	}
	return r.err
}

func TestStoredMessageDoesNotBecomeRetryableWhenHintFails(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1, DialTimeout: 50 * time.Millisecond, ReadTimeout: 50 * time.Millisecond, WriteTimeout: 50 * time.Millisecond})
	defer client.Close()
	repo := &sendRepository{}
	svc := NewMessengerService(repo, client)
	svc.generateID = func() (uint64, error) { return 9007199254740999, nil }
	server.Close()
	before := pushCounterValue(t, messagePushResults.WithLabelValues("error"))
	start := time.Now()
	response, err := svc.SendMessage(context.Background(), &messengerv1.SendMessageRequest{SenderId: 1, ReceiverId: 2, Content: "private fixture text"})
	require.NoError(t, err)
	require.Equal(t, uint64(9007199254740999), response.Message.Id)
	require.NotNil(t, repo.stored)
	require.Less(t, time.Since(start), 3*time.Second)
	require.Equal(t, before+2, pushCounterValue(t, messagePushResults.WithLabelValues("error")))
}

func TestHintPreservesStringsAndNoSubscribersAreObservable(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()
	repo := &sendRepository{}
	svc := NewMessengerService(repo, client)
	svc.generateID = func() (uint64, error) { return 9007199254740999, nil }
	ctx := context.Background()
	subscription := client.Subscribe(ctx, "notifications:user:2")
	defer subscription.Close()
	_, err := subscription.Receive(ctx)
	require.NoError(t, err)
	before := pushCounterValue(t, messagePushResults.WithLabelValues("no_subscriber"))
	_, err = svc.SendMessage(ctx, &messengerv1.SendMessageRequest{SenderId: 1, ReceiverId: 2, Content: "fixture"})
	require.NoError(t, err)
	readCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	hint, err := subscription.ReceiveMessage(readCtx)
	require.NoError(t, err)
	require.Contains(t, hint.Payload, `"id":"9007199254740999"`)
	require.Equal(t, before+1, pushCounterValue(t, messagePushResults.WithLabelValues("no_subscriber")))
	repo.err = errors.New("storage unavailable")
	_, err = svc.SendMessage(ctx, &messengerv1.SendMessageRequest{SenderId: 1, ReceiverId: 2, Content: "fixture"})
	require.Error(t, err)
}
