package service

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"twitter-clone/internal/domain"
)

type currentFeedFacts struct {
	domain.TweetRepository
	domain.FollowRepository
	mu         sync.Mutex
	rows       []*domain.Tweet
	follows    map[uint64]bool
	pages      int
	rebuilds   int
	checks     int
	pageErr    error
	rebuildErr error
	followErr  error
	afterPage  func()
}

func (f *currentFeedFacts) ListFollowingFeed(ctx context.Context, viewer, before uint64, limit int) ([]*domain.Tweet, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	if limit == 1000 {
		f.rebuilds++
	} else {
		f.pages++
	}
	err := f.pageErr
	if limit == 1000 {
		err = f.rebuildErr
	}
	if err != nil {
		f.mu.Unlock()
		return nil, err
	}
	rows := make([]*domain.Tweet, 0)
	for _, row := range f.rows {
		if row.DeletedAt != 0 || (before > 0 && row.ID >= before) {
			continue
		}
		if row.UserID == viewer || (f.follows[row.UserID] && (row.VisibleType == domain.VisiblePublic || row.VisibleType == domain.VisibleFollows)) {
			rows = append(rows, row)
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID > rows[j].ID })
	if len(rows) > limit {
		rows = rows[:limit]
	}
	hook := f.afterPage
	f.afterPage = nil
	f.mu.Unlock()
	if hook != nil {
		hook()
	}
	return rows, nil
}
func (f *currentFeedFacts) BatchIsFollowing(ctx context.Context, viewer uint64, ids []uint64) (map[uint64]bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checks++
	if f.followErr != nil {
		return nil, f.followErr
	}
	result := make(map[uint64]bool, len(ids))
	for _, id := range ids {
		result[id] = f.follows[id]
	}
	return result, nil
}
func currentFeedFixture(t *testing.T) (*TweetService, *currentFeedFacts) {
	t.Helper()
	s, _, _ := feedServiceFixture(t)
	f := &currentFeedFacts{follows: make(map[uint64]bool)}
	s.repo = f
	s.followRepo = f
	return s, f
}

func TestFeedRecoversEmptyAndEvictedInboxWithoutLosingNewFanout(t *testing.T) {
	s, f := currentFeedFixture(t)
	f.follows[2] = true
	f.rows = []*domain.Tweet{{ID: 100, UserID: 2}, {ID: 99, UserID: 2}}
	ctx := context.Background()
	// Concurrent/new fanout is already present when recovery merges the snapshot.
	require.NoError(t, s.timelineCache.AddToTimeline(ctx, 1, 101))
	rows, _, _, err := s.GetFeeds(ctx, 1, 0, 20)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	ids, err := s.timelineCache.GetTimeline(ctx, 1, 0, 20)
	require.NoError(t, err)
	require.Equal(t, []uint64{101, 100, 99}, ids)
	require.Equal(t, 1, f.rebuilds)
	_, _, _, err = s.GetFeeds(ctx, 1, 0, 20)
	require.NoError(t, err)
	require.Equal(t, 1, f.rebuilds)
	require.NoError(t, s.timelineCache.ClearTimeline(ctx, 1))
	_, _, _, err = s.GetFeeds(ctx, 1, 0, 20)
	require.NoError(t, err)
	require.Equal(t, 2, f.rebuilds)

	empty, emptyFacts := currentFeedFixture(t)
	for i := 0; i < 3; i++ {
		rows, _, _, err = empty.GetFeeds(ctx, 1, 0, 20)
		require.NoError(t, err)
		require.Empty(t, rows)
	}
	require.Equal(t, 1, emptyFacts.rebuilds)
}

func TestFeedCurrentRelationsCoverAllAuthorsAndDeepHistory(t *testing.T) {
	s, f := currentFeedFixture(t)
	for i := 0; i < 1205; i++ {
		author := uint64(2 + i%205)
		f.follows[author] = true
		f.rows = append(f.rows, &domain.Tweet{ID: uint64(5000 - i), UserID: author, VisibleType: domain.VisibleFollows})
	}
	// No celebrity association set, no author ZSet, no legacy ID enumeration.
	rows, next, more, err := s.GetFeeds(context.Background(), 1, 0, 100)
	require.NoError(t, err)
	require.Len(t, rows, 100)
	require.True(t, more)
	require.EqualValues(t, 4901, next)
	rows, next, more, err = s.GetFeeds(context.Background(), 1, 4000, 100)
	require.NoError(t, err)
	require.Len(t, rows, 100)
	require.EqualValues(t, 3999, rows[0].ID)
	require.True(t, more)
	require.EqualValues(t, 3900, next)
	require.Equal(t, 1, f.rebuilds)
	// Old cached content and new author tweets are both excluded after unfollow.
	f.mu.Lock()
	f.follows[206] = false
	f.mu.Unlock()
	rows, _, _, err = s.GetFeeds(context.Background(), 1, 0, 100)
	require.NoError(t, err)
	for _, row := range rows {
		require.NotEqualValues(t, 206, row.UserID)
	}
}

func TestFeedRevocationBetweenCandidateReadAndBatchCheck(t *testing.T) {
	s, f := currentFeedFixture(t)
	f.follows[2] = true
	f.follows[3] = true
	f.rows = []*domain.Tweet{{ID: 100, UserID: 2, VisibleType: domain.VisibleFollows}, {ID: 99, UserID: 3}, {ID: 98, UserID: 1, VisibleType: domain.VisiblePrivate}, {ID: 97, UserID: 4}, {ID: 96, UserID: 3, VisibleType: domain.VisibleShadowban}}
	f.afterPage = func() { f.mu.Lock(); f.follows[2] = false; f.mu.Unlock() }
	rows, _, _, err := s.GetFeeds(context.Background(), 1, 0, 20)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.EqualValues(t, 99, rows[0].ID)
	require.EqualValues(t, 98, rows[1].ID)
	require.Equal(t, 1, f.checks)
	f.followErr = errors.New("private relation SQL")
	rows, next, more, err := s.GetFeeds(context.Background(), 1, 0, 20)
	require.Error(t, err)
	require.Nil(t, rows)
	require.Zero(t, next)
	require.False(t, more)
}

func TestFeedProjectionFailureDoesNotConvertAuthorityFailureToEmptySuccess(t *testing.T) {
	s, f := currentFeedFixture(t)
	f.follows[2] = true
	f.rows = []*domain.Tweet{{ID: 100, UserID: 2}}
	f.rebuildErr = errors.New("rebuild failure")
	rows, _, _, err := s.GetFeeds(context.Background(), 1, 0, 20)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, 1, f.rebuilds)
	_, _, _, err = s.GetFeeds(context.Background(), 1, 0, 20)
	require.NoError(t, err)
	require.Equal(t, 2, f.rebuilds)
	// Redis is optional for permission/completeness. No hot-loop Redis retry.
	s.timelineCache = nil
	rows, _, _, err = s.GetFeeds(context.Background(), 1, 0, 20)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	f.pageErr = errors.New("authority outage")
	rows, next, more, err := s.GetFeeds(context.Background(), 1, 0, 20)
	require.Error(t, err)
	require.Nil(t, rows)
	require.Zero(t, next)
	require.False(t, more)
}

func TestFeedConcurrentColdRecoveryHasOneProjectionOwner(t *testing.T) {
	s, f := currentFeedFixture(t)
	f.follows[2] = true
	f.rows = []*domain.Tweet{{ID: 100, UserID: 2}}
	var wg sync.WaitGroup
	results := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _, _, err := s.GetFeeds(context.Background(), 1, 0, 20); results <- err }()
	}
	wg.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err)
	}
	require.Equal(t, 1, f.rebuilds)
	require.Equal(t, 16, f.pages)
}

func TestFeedFailsClosedWithoutCurrentReadPorts(t *testing.T) {
	s, _ := currentFeedFixture(t)
	s.repo = &struct{ domain.TweetRepository }{}
	_, _, _, err := s.GetFeeds(context.Background(), 1, 0, 20)
	require.Error(t, err)
	s, f := currentFeedFixture(t)
	s.followRepo = nil
	_, _, _, err = s.GetFeeds(context.Background(), 1, 0, 20)
	require.Error(t, err)
	require.Zero(t, f.pages)
}
