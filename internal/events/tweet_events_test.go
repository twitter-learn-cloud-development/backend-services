package events

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTweetModeratedEventValidatesStableIdentity(t *testing.T) {
	event := NewTweetModeratedEvent(900, 42, TweetModerationShadowban, 1234)
	require.NoError(t, event.Validate())
	require.Equal(t, "tweet-moderated:v1:900:shadowban", event.EventKey)

	event.EventKey = "tweet-moderated:v1:901:shadowban"
	require.Error(t, event.Validate())
}
