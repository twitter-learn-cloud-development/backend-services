package service

import (
	"context"
	"fmt"
	"testing"

	"twitter-clone/internal/domain"
)

// Measures scanner allocations/budget only; Fake source has O(1) page seeks.
// No MySQL/Redis/network/SQL execution-plan throughput claim is made.
func BenchmarkFeedScannerBudget(b *testing.B) {
	for _, hidden := range []bool{false, true} {
		b.Run(fmt.Sprintf("hidden_%t", hidden), func(b *testing.B) {
			s := &TweetService{repo: &feedRepo{}, followRepo: &feedFollowRepo{}, likeRepo: &feedLikes{}, retweetRepo: &feedRetweets{}, bookmarkRepo: &feedBookmarks{}, commentRepo: &feedComments{}, pollRepo: &feedPolls{}}
			calls := 0
			fetch := func(_ context.Context, before uint64, count int) (feedCandidates, error) {
				calls++
				if before == 0 {
					before = 1000001
				}
				page := feedCandidates{tweets: map[uint64]*domain.Tweet{}}
				for i := 0; i < count; i++ {
					id := before - uint64(i) - 1
					visibility := domain.VisiblePublic
					if hidden {
						visibility = domain.VisibleShadowban
					}
					page.ids = append(page.ids, id)
					page.tweets[id] = &domain.Tweet{ID: id, UserID: 2, VisibleType: visibility}
				}
				return page, nil
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _, _, err := s.scanFeedPage(context.Background(), 1, 0, 20, fetch)
				if err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(calls)/float64(b.N), "candidate_batches/op")
		})
	}
}

func TestFeedCapacityQueryAndAuthorizationBudgets(t *testing.T) {
	s := &TweetService{followRepo: &feedFollowRepo{}, likeRepo: &feedLikes{}, retweetRepo: &feedRetweets{}, bookmarkRepo: &feedBookmarks{}, commentRepo: &feedComments{}, pollRepo: &feedPolls{}}
	queries := 0
	_, next, more, err := s.scanFeedPage(context.Background(), 1, 0, 100, func(_ context.Context, before uint64, count int) (feedCandidates, error) {
		queries++
		if count != 101 {
			t.Fatalf("unbounded count %d", count)
		}
		if before == 0 {
			before = 1000001
		}
		page := feedCandidates{tweets: map[uint64]*domain.Tweet{}}
		for i := 0; i < count; i++ {
			id := before - uint64(i) - 1
			page.ids = append(page.ids, id)
			page.tweets[id] = &domain.Tweet{ID: id, UserID: 2, VisibleType: domain.VisibleShadowban}
		}
		return page, nil
	})
	if err != nil || !more || queries != 10 || next != 999001 {
		t.Fatalf("budget %d %d %t %v", queries, next, more, err)
	}
}
