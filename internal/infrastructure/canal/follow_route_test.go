package canal

import (
	"github.com/stretchr/testify/require"
	"testing"
	"twitter-clone/internal/events"
)

func TestFollowOutboxRoutesMatchDurableRelationQueueBindings(t *testing.T) {
	for kind, key := range map[string]string{events.OutboxEventTypeUserFollowed: "user.followed", events.OutboxEventTypeUserUnfollowed: "user.unfollowed"} {
		route, ok := OutboxRoute(kind)
		require.True(t, ok)
		require.Equal(t, "twitter.events", route.Exchange)
		require.Equal(t, key, route.RoutingKey)
	}
}
