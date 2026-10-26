package consumer

import (
	"testing"
	"time"
)

func TestTweetCreatedWithinTrendWindow(t *testing.T) {
	now := time.UnixMilli(1790433251392)
	tests := []struct {
		name    string
		created int64
		want    bool
	}{
		{name: "recent", created: now.Add(-time.Minute).UnixMilli(), want: true},
		{name: "at boundary", created: now.Add(-globalTrendTTL).UnixMilli(), want: true},
		{name: "expired", created: now.Add(-globalTrendTTL - time.Millisecond).UnixMilli()},
		{name: "missing", created: 0},
		{name: "future", created: now.Add(6 * time.Minute).UnixMilli()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tweetCreatedWithinTrendWindow(tt.created, now); got != tt.want {
				t.Fatalf("got %t, want %t", got, tt.want)
			}
		})
	}
}
