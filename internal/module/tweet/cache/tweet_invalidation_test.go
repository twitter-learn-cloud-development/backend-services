package cache

import (
	"context"
	"errors"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

type invalidationFailureHook struct {
	command   string
	failure   error
	published bool
}

func (h *invalidationFailureHook) BeforeProcess(ctx context.Context, cmd redis.Cmder) (context.Context, error) {
	if cmd.Name() == "publish" {
		h.published = true
	}
	if cmd.Name() == h.command {
		return ctx, h.failure
	}
	return ctx, nil
}
func (*invalidationFailureHook) AfterProcess(context.Context, redis.Cmder) error { return nil }
func (*invalidationFailureHook) BeforeProcessPipeline(ctx context.Context, _ []redis.Cmder) (context.Context, error) {
	return ctx, nil
}
func (*invalidationFailureHook) AfterProcessPipeline(context.Context, []redis.Cmder) error {
	return nil
}

func TestInvalidateBaseTweetObservesBothFailuresAndAttemptsBroadcast(t *testing.T) {
	for _, command := range []string{"", "del", "publish"} {
		t.Run("fail_"+command, func(t *testing.T) {
			server := miniredis.RunT(t)
			client := redis.NewClient(&redis.Options{Addr: server.Addr()})
			t.Cleanup(func() { _ = client.Close() })
			server.Set("tweet:base:17", "fixture")
			hook := &invalidationFailureHook{command: command, failure: errors.New("fixture invalidation failure")}
			client.AddHook(hook)
			cache := &TimelineCache{redis: client}
			err := cache.InvalidateBaseTweet(context.Background(), 17)
			if command == "" {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, hook.failure)
			}
			require.True(t, hook.published)
			require.Equal(t, command == "del", server.Exists("tweet:base:17"))
		})
	}
}
