package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
	"twitter-clone/internal/domain"
	"twitter-clone/internal/module/tweet/cache"
)

type feedRepo struct {
	domain.TweetRepository
	rows       map[uint64]*domain.Tweet
	readErr    error
	pullLimits []int
	hydrated   int
}

func (r *feedRepo) ListFollowingFeed(ctx context.Context, viewerID, before uint64, limit int) ([]*domain.Tweet, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.pullLimits = append(r.pullLimits, limit)
	if r.readErr != nil {
		return nil, r.readErr
	}
	rows := []*domain.Tweet{}
	// Deliberately include records changing visibility after candidate scope,
	// to retain scanner's defensive filtering regression tests.
	for _, row := range r.rows {
		if before == 0 || row.ID < before {
			rows = append(rows, row)
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID > rows[j].ID })
	if len(rows) > limit {
		rows = rows[:limit]
	}
	return rows, nil
}

func (r *feedRepo) GetByIDs(ctx context.Context, ids []uint64) ([]*domain.Tweet, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r.readErr != nil {
		return nil, r.readErr
	}
	r.hydrated += len(ids)
	result := []*domain.Tweet{}
	for i := len(ids) - 1; i >= 0; i-- {
		if row := r.rows[ids[i]]; row != nil {
			result = append(result, row)
		}
	}
	return result, nil
}
func (r *feedRepo) GetFeeds(ctx context.Context, users []uint64, before uint64, limit int) ([]*domain.Tweet, error) {
	r.pullLimits = append(r.pullLimits, limit)
	if r.readErr != nil {
		return nil, r.readErr
	}
	allowed := map[uint64]bool{}
	for _, id := range users {
		allowed[id] = true
	}
	rows := []*domain.Tweet{}
	for _, row := range r.rows {
		if allowed[row.UserID] && row.DeletedAt == 0 && row.VisibleType == domain.VisiblePublic && (before == 0 || row.ID < before) {
			rows = append(rows, row)
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID > rows[j].ID })
	if len(rows) > limit {
		rows = rows[:limit]
	}
	return rows, nil
}
func (r *feedRepo) ListByUserID(context.Context, uint64, uint64, int) ([]*domain.Tweet, error) {
	return nil, errors.New("rebuild failed")
}

type feedFollowRepo struct{ domain.FollowRepository }

func (*feedFollowRepo) GetFollowees(context.Context, uint64, uint64, int) ([]uint64, error) {
	return []uint64{2, 3}, nil
}

func (*feedFollowRepo) BatchIsFollowing(_ context.Context, _ uint64, ids []uint64) (map[uint64]bool, error) {
	result := map[uint64]bool{}
	for _, id := range ids {
		result[id] = id == 2 || id == 3
	}
	return result, nil
}

type feedLikes struct{ domain.LikeRepository }

func (*feedLikes) BatchGetLikeCounts(context.Context, []uint64) (map[uint64]int64, error) {
	return nil, nil
}
func (*feedLikes) BatchIsLiked(context.Context, uint64, []uint64) (map[uint64]bool, error) {
	return nil, nil
}

type feedRetweets struct{ domain.RetweetRepository }

func (*feedRetweets) BatchGetRetweetCounts(context.Context, []uint64) (map[uint64]int64, error) {
	return nil, nil
}
func (*feedRetweets) BatchIsRetweeted(context.Context, uint64, []uint64) (map[uint64]bool, error) {
	return nil, nil
}

type feedBookmarks struct{ domain.BookmarkRepository }

func (*feedBookmarks) BatchIsBookmarked(context.Context, uint64, []uint64) (map[uint64]bool, error) {
	return nil, nil
}

type feedComments struct{ domain.CommentRepository }

func (*feedComments) BatchGetCommentCounts(context.Context, []uint64) (map[uint64]int64, error) {
	return nil, nil
}

type feedPolls struct{ domain.PollRepository }

func (*feedPolls) GetByTweetIDs(context.Context, []uint64) (map[uint64]*domain.Poll, error) {
	return nil, nil
}

func feedServiceFixture(t *testing.T) (*TweetService, *feedRepo, *redis.Client) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	r := &feedRepo{rows: map[uint64]*domain.Tweet{}}
	s := &TweetService{repo: r, followRepo: &feedFollowRepo{}, timelineCache: cache.NewTimelineCache(client), likeRepo: &feedLikes{}, retweetRepo: &feedRetweets{}, bookmarkRepo: &feedBookmarks{}, commentRepo: &feedComments{}, pollRepo: &feedPolls{}}
	return s, r, client
}
func seedFeed(t *testing.T, s *TweetService, r *feedRepo, count int, base uint64) {
	t.Helper()
	for i := 0; i < count; i++ {
		id := base - uint64(i)
		r.rows[id] = &domain.Tweet{ID: id, UserID: 2, CreatedAt: 1000}
		require.NoError(t, s.timelineCache.AddToTimeline(context.Background(), 1, id))
	}
}

func TestFeedNormalPagesExactLargeIDsAndConcurrentInsert(t *testing.T) {
	s, r, _ := feedServiceFixture(t)
	const base = uint64(9007199254741099)
	seedFeed(t, s, r, 35, base)
	var all []uint64
	cursor := uint64(0)
	for page := 0; page < 4; page++ {
		rows, next, more, err := s.GetFeeds(context.Background(), 1, cursor, 10)
		require.NoError(t, err)
		for _, row := range rows {
			all = append(all, row.ID)
		}
		if page == 0 {
			require.True(t, more)
			seedFeed(t, s, r, 1, base+100)
		}
		if !more {
			require.Zero(t, next)
			break
		}
		require.NotZero(t, next)
		if cursor > 0 {
			require.Less(t, next, cursor)
		}
		cursor = next
	}
	require.Len(t, all, 35)
	for i, id := range all {
		require.Equal(t, base-uint64(i), id)
	}
}

func TestFeedOneHundredLookaheadAndPull(t *testing.T) {
	for _, pull := range []bool{false, true} {
		t.Run(fmt.Sprint(pull), func(t *testing.T) {
			s, r, _ := feedServiceFixture(t)
			seedFeed(t, s, r, 101, 500)
			if pull {
				s.timelineCache = nil
			}
			rows, next, more, err := s.GetFeeds(context.Background(), 1, 0, 100)
			require.NoError(t, err)
			require.Len(t, rows, 100)
			require.True(t, more)
			require.EqualValues(t, 401, next)
			rows, next, more, err = s.GetFeeds(context.Background(), 1, next, 100)
			require.NoError(t, err)
			require.Len(t, rows, 1)
			require.EqualValues(t, 400, rows[0].ID)
			require.False(t, more)
			require.Zero(t, next)
			if pull {
				require.Equal(t, []int{101, 101}, r.pullLimits)
			}
		})
	}
}

func TestFeedFilteringFillsPageWithoutConsumingUndeliveredIDs(t *testing.T) {
	s, r, _ := feedServiceFixture(t)
	seedFeed(t, s, r, 180, 500)
	for id := uint64(500); id >= 391; id-- {
		r.rows[id].VisibleType = domain.VisibleShadowban
	}
	delete(r.rows, 390)
	r.rows[389].DeletedAt = 1
	r.rows[388].VisibleType = domain.VisiblePrivate
	r.rows[387].VisibleType = domain.VisibleFollows
	rows, next, more, err := s.GetFeeds(context.Background(), 1, 0, 20)
	require.NoError(t, err)
	require.Len(t, rows, 20)
	require.True(t, more)
	require.EqualValues(t, 387, rows[0].ID)
	require.EqualValues(t, 368, next)
	rows, _, _, err = s.GetFeeds(context.Background(), 1, next, 20)
	require.NoError(t, err)
	require.EqualValues(t, 367, rows[0].ID)
}

func TestFeedEmptyFilteredPageAdvancesAtScanBudget(t *testing.T) {
	s, r, client := feedServiceFixture(t)
	// Bypass cache trimming to exercise a retained history > the scan budget.
	for id := uint64(2000); id >= 901; id-- {
		r.rows[id] = &domain.Tweet{ID: id, UserID: 2, VisibleType: domain.VisibleShadowban}
		require.NoError(t, client.ZAdd(context.Background(), "timeline:1", &redis.Z{Score: float64(id), Member: id}).Err())
	}
	rows, next, more, err := s.GetFeeds(context.Background(), 1, 0, 20)
	require.NoError(t, err)
	require.Empty(t, rows)
	require.True(t, more)
	require.EqualValues(t, 1001, next)
	require.Equal(t, []int{101, 101, 101, 101, 101, 101, 101, 101, 101, 101, 1000}, r.pullLimits)
	r.rows[950].VisibleType = domain.VisiblePublic
	rows, next, more, err = s.GetFeeds(context.Background(), 1, next, 20)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.EqualValues(t, 950, rows[0].ID)
	require.False(t, more)
	require.Zero(t, next)
}

func TestFeedStaleCelebrityAssociationsDoNotLimitCurrentRead(t *testing.T) {
	s, r, client := feedServiceFixture(t)
	seedFeed(t, s, r, 10, 100)
	require.NoError(t, client.SAdd(context.Background(), "user:celebrities:1", 3).Err())
	require.NoError(t, s.timelineCache.RebuildUserTimeline(context.Background(), 3, []uint64{105, 100, 95, 80}))
	for _, id := range []uint64{105, 100, 95, 80} {
		r.rows[id] = &domain.Tweet{ID: id, UserID: 3}
	}
	rows, _, _, err := s.GetFeeds(context.Background(), 1, 0, 100)
	require.NoError(t, err)
	require.Len(t, rows, 12)
	require.EqualValues(t, 105, rows[0].ID)
	// Losing old celebrity association initialization does not lose this author.
	require.NoError(t, client.Del(context.Background(), "user_timeline:3:initialized").Err())
	rows, _, _, err = s.GetFeeds(context.Background(), 1, 0, 100)
	require.NoError(t, err)
	require.Len(t, rows, 12)
	require.NotEmpty(t, r.pullLimits)
}

func TestFeedRejectsBrokenPagesCancellationAndBodyFailure(t *testing.T) {
	s, r, _ := feedServiceFixture(t)
	seedFeed(t, s, r, 2, 100)
	r.readErr = errors.New("SQL private content")
	rows, next, more, err := s.GetFeeds(context.Background(), 1, 0, 20)
	require.Error(t, err)
	require.Nil(t, rows)
	require.Zero(t, next)
	require.False(t, more)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, _, err = s.GetFeeds(ctx, 1, 0, 20)
	require.ErrorIs(t, err, context.Canceled)
	for _, ids := range [][]uint64{{100, 100}, {100, 101}, {0}} {
		_, _, _, err = s.scanFeedPage(context.Background(), 1, 0, 20, func(context.Context, uint64, int) (feedCandidates, error) { return feedCandidates{ids: ids}, nil })
		require.Error(t, err)
	}
	_, _, _, err = s.GetFeeds(context.Background(), 0, 0, 20)
	require.ErrorIs(t, err, ErrInvalidAuthorID)
	for input, want := range map[int]int{-1: 20, 0: 20, 1: 1, 100: 100, 101: 100} {
		require.Equal(t, want, feedPageLimit(input))
	}
}
