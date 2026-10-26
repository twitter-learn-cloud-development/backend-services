package service

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/stretchr/testify/mock"
	"twitter-clone/internal/domain"
)

func TestGetPublicTweetsUsesCurrentBatchAuthority(t *testing.T) {
	repo := new(MockTweetRepository)
	repo.On("GetByIDs", mock.Anything, []uint64{3, 1, 2, 4, 5, 6}).Return([]*domain.Tweet{
		{ID: 1, UserID: 10, Content: "current"},
		{ID: 2, UserID: 10, VisibleType: domain.VisibleShadowban},
		{ID: 3, UserID: 10, Content: "current first"},
		{ID: 4, UserID: 10, DeletedAt: 123},
		{ID: 5, UserID: 10, VisibleType: domain.VisiblePrivate},
		{ID: 6, UserID: 10, VisibleType: domain.VisibleFollows},
		{ID: 99, UserID: 10}, nil,
	}, nil).Once()
	svc := &TweetService{repo: repo}
	got, err := svc.GetPublicTweets(context.Background(), []uint64{3, 1, 2, 4, 5, 6, 3})
	if err != nil || len(got) != 2 || got[0].ID != 3 || got[1].ID != 1 || got[1].Content != "current" {
		t.Fatalf("public authority result = %+v, %v", got, err)
	}
	repo.AssertExpectations(t)
}

func TestGetPublicTweetsRejectsInvalidInputBeforeReading(t *testing.T) {
	for _, ids := range [][]uint64{nil, {0}, make([]uint64, 101)} {
		_, err := (&TweetService{}).GetPublicTweets(context.Background(), ids)
		if !errors.Is(err, ErrInvalidPublicTweetCandidates) {
			t.Fatalf("invalid batch accepted: %v", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := (&TweetService{}).GetPublicTweets(ctx, []uint64{1})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel ignored: %v", err)
	}
}

func TestGetPublicTweetsPropagatesAuthorityFailure(t *testing.T) {
	repo := new(MockTweetRepository)
	failure := errors.New("database unavailable")
	repo.On("GetByIDs", mock.Anything, []uint64{1}).Return(nil, failure).Once()
	got, err := (&TweetService{repo: repo}).GetPublicTweets(context.Background(), []uint64{1})
	if got != nil || !errors.Is(err, failure) {
		t.Fatalf("authority failure masked: %+v %v", got, err)
	}
}

func TestGetPublicTweetsBoundaryBatch(t *testing.T) {
	ids := make([]uint64, MaxPublicTweetCandidates)
	for i := range ids {
		ids[i] = uint64(i + 1)
	}
	repo := new(MockTweetRepository)
	repo.On("GetByIDs", mock.Anything, ids).Return([]*domain.Tweet{}, nil).Once()
	got, err := (&TweetService{repo: repo}).GetPublicTweets(context.Background(), ids)
	if err != nil || !reflect.DeepEqual(got, []*domain.Tweet{}) {
		t.Fatalf("boundary: %+v %v", got, err)
	}
	repo.AssertExpectations(t)
}
