package canal

import "testing"

func TestTweetModeratedOutboxRoute(t *testing.T) {
	route, ok := routeRegistry["TWEET_MODERATED"]
	if !ok {
		t.Fatal("TWEET_MODERATED route is missing")
	}
	if route.Exchange != "twitter.events" || route.RoutingKey != "tweet.moderated" {
		t.Fatalf("unexpected moderation route: %+v", route)
	}
}
