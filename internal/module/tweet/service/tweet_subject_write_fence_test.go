package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"twitter-clone/internal/domain"
	"twitter-clone/internal/events"
)

func TestEnforceTweetSubjectWriteFenceBindsRequest(t *testing.T) {
	repo := AllowTweetSubjectWrites()
	repo.enforceRevision = 3
	repo.enforceReplay = true
	service := &TweetService{subjectWriteFenceRepo: repo, uow: new(MockUOWManager)}
	now := time.Now().UTC()
	digest := sha256String("binding")

	outcome, err := service.EnforceTweetSubjectWriteFence(context.Background(), TweetSubjectWriteFenceCommand{
		SchemaVersion: domain.TweetSubjectWriteFenceSchemaV1,
		RequestID:     "delete/request-7", UserID: 42, SubjectDigest: tweetSubjectDigest(42),
		PlanDigest: digest, CapabilityDigest: digest, GovernanceDigest: digest,
		Deadline: now.Add(time.Minute),
	})

	assert.NoError(t, err)
	assert.Equal(t, "delete/request-7", outcome.RequestID)
	assert.Equal(t, int64(3), outcome.FenceRevision)
	assert.True(t, outcome.Replayed)
	assert.False(t, outcome.EnforcedAt.IsZero())
}

func TestEnforceTweetSubjectWriteFenceRejectsWrongSubjectBinding(t *testing.T) {
	service := &TweetService{subjectWriteFenceRepo: AllowTweetSubjectWrites(), uow: new(MockUOWManager)}
	digest := sha256String("binding")

	_, err := service.EnforceTweetSubjectWriteFence(context.Background(), TweetSubjectWriteFenceCommand{
		SchemaVersion: domain.TweetSubjectWriteFenceSchemaV1,
		RequestID:     "delete/request-8", UserID: 42, SubjectDigest: tweetSubjectDigest(43),
		PlanDigest: digest, CapabilityDigest: digest, GovernanceDigest: digest,
		Deadline: time.Now().UTC().Add(time.Minute),
	})

	assert.ErrorIs(t, err, ErrSubjectWriteFenceInvalid)
}

func TestPersistTweetLikedStopsBeforeMutationWhenSubjectIsFenced(t *testing.T) {
	likeRepo := new(MockLikeRepository)
	outboxRepo := new(MockOutboxEventRepository)
	fenceRepo := AllowTweetSubjectWrites()
	fenceRepo.authorizeErr = domain.ErrTweetSubjectWriteFenced
	service := &TweetService{
		likeRepo: likeRepo, outboxEventRepo: outboxRepo, uow: new(MockUOWManager),
		subjectWriteFenceRepo: fenceRepo,
	}

	err := service.persistTweetLiked(context.Background(), &events.TweetLikedEvent{
		TweetID: 9001, UserID: 77, TweetUser: 42, OccurredAtUnixMS: 1700000000000,
	})

	assert.ErrorIs(t, err, ErrSubjectWriteFenced)
	assert.Equal(t, []uint64{77}, fenceRepo.AuthorizedIDs())
	likeRepo.AssertNotCalled(t, "Like", mock.Anything, mock.Anything, mock.Anything)
	outboxRepo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
}

func TestPersistCommentCreatedFailsClosedWithoutFenceRepository(t *testing.T) {
	service := &TweetService{
		commentRepo:     new(MockCommentRepository),
		outboxEventRepo: new(MockOutboxEventRepository),
		uow:             new(MockUOWManager),
	}
	err := service.persistCommentCreated(context.Background(), &domain.Comment{
		TweetID: 9001, UserID: 77, Content: "blocked",
	}, 42)
	assert.ErrorIs(t, err, ErrSubjectWriteFenceUnavailable)
}

func TestAuthorizeSubjectWriteWrapsFenceStoreFailureAsUnavailable(t *testing.T) {
	fenceRepo := AllowTweetSubjectWrites()
	fenceRepo.authorizeErr = errors.New("database unavailable")
	service := &TweetService{subjectWriteFenceRepo: fenceRepo}

	err := service.authorizeSubjectWrite(context.Background(), 42)

	assert.ErrorIs(t, err, ErrSubjectWriteFenceUnavailable)
}
