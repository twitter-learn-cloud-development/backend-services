package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"twitter-clone/internal/domain"
	agentdeletion "twitter-clone/internal/module/agent/deletion"
)

type subjectDeletionRepoFake struct {
	inventory     domain.TweetSubjectInventory
	authoredIDs   []uint64
	operation     *domain.TweetSubjectDeletionOperation
	deleteCalls   int
	leaveResidual bool
}

func (fake *subjectDeletionRepoFake) GetOperation(context.Context, string) (*domain.TweetSubjectDeletionOperation, error) {
	if fake.operation == nil {
		return nil, domain.ErrTweetSubjectDeletionNotFound
	}
	copy := *fake.operation
	return &copy, nil
}

func (fake *subjectDeletionRepoFake) CreateOperation(_ context.Context, operation *domain.TweetSubjectDeletionOperation) error {
	if fake.operation != nil {
		return domain.ErrTweetSubjectDeletionExists
	}
	copy := *operation
	fake.operation = &copy
	return nil
}

func (fake *subjectDeletionRepoFake) ObserveSubject(context.Context, uint64) (domain.TweetSubjectInventory, error) {
	return fake.inventory, nil
}

func (fake *subjectDeletionRepoFake) ListAuthoredTweetIDs(context.Context, uint64, int) ([]uint64, error) {
	return append([]uint64(nil), fake.authoredIDs...), nil
}

func (fake *subjectDeletionRepoFake) DeleteOwnedData(context.Context, uint64, []uint64) error {
	fake.deleteCalls++
	if fake.leaveResidual {
		fake.inventory = domain.TweetSubjectInventory{Likes: 1}
	} else {
		fake.inventory = domain.TweetSubjectInventory{}
	}
	return nil
}

type subjectDeletionOutboxFake struct {
	events []*domain.OutboxEvent
}

func (fake *subjectDeletionOutboxFake) Create(context.Context, *domain.OutboxEvent) error {
	return errors.New("unexpected non-idempotent outbox create")
}

func (fake *subjectDeletionOutboxFake) CreateIdempotent(_ context.Context, event *domain.OutboxEvent) (bool, error) {
	copy := *event
	fake.events = append(fake.events, &copy)
	return true, nil
}

func (fake *subjectDeletionOutboxFake) DeleteExpired(context.Context, int64) error { return nil }

func TestDeleteTweetSubjectDataCommitsReceiptAndReplaysWithoutSideEffects(t *testing.T) {
	repo := &subjectDeletionRepoFake{
		inventory: domain.TweetSubjectInventory{
			AuthoredTweets: 2, AuthoredComments: 3, Likes: 4,
			Bookmarks: 5, Retweets: 6, PollVotes: 7,
		},
		authoredIDs: []uint64{101, 102},
	}
	outbox := &subjectDeletionOutboxFake{}
	service := &TweetService{
		deleteSubjectRepo: repo, outboxEventRepo: outbox, uow: new(MockUOWManager),
		subjectWriteFenceRepo: AllowTweetSubjectWrites(),
	}
	command := validTweetSubjectDeletionCommand()

	outcome, err := service.DeleteTweetSubjectData(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Replayed || outcome.After.Total() != 0 || outcome.Before.AuthoredTweets != 2 {
		t.Fatalf("unexpected first outcome: %+v", outcome)
	}
	if repo.deleteCalls != 1 || len(outbox.events) != 2 || repo.operation == nil {
		t.Fatalf("side effects delete=%d events=%d operation=%v", repo.deleteCalls, len(outbox.events), repo.operation != nil)
	}
	for _, event := range outbox.events {
		if event.EventType != "TWEET_DELETED" || event.DedupKey == nil {
			t.Fatalf("unexpected cleanup event: %+v", event)
		}
	}

	replayed, err := service.DeleteTweetSubjectData(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	if !replayed.Replayed || replayed.ReceiptDigest != outcome.ReceiptDigest {
		t.Fatalf("unexpected replay outcome: %+v", replayed)
	}
	if repo.deleteCalls != 1 || len(outbox.events) != 2 {
		t.Fatalf("replay repeated side effects delete=%d events=%d", repo.deleteCalls, len(outbox.events))
	}
}

func TestDeleteTweetSubjectDataRejectsConflictingReplay(t *testing.T) {
	repo := &subjectDeletionRepoFake{inventory: domain.TweetSubjectInventory{}, authoredIDs: nil}
	service := &TweetService{
		deleteSubjectRepo: repo, outboxEventRepo: &subjectDeletionOutboxFake{}, uow: new(MockUOWManager),
		subjectWriteFenceRepo: AllowTweetSubjectWrites(),
	}
	command := validTweetSubjectDeletionCommand()
	if _, err := service.DeleteTweetSubjectData(context.Background(), command); err != nil {
		t.Fatal(err)
	}
	command.GovernanceDigest = sha256String("different-governance-decision")
	if _, err := service.DeleteTweetSubjectData(context.Background(), command); !errors.Is(err, ErrSubjectDeletionConflict) {
		t.Fatalf("conflicting replay error = %v, want %v", err, ErrSubjectDeletionConflict)
	}
}

func TestDeleteTweetSubjectDataRejectsResiduals(t *testing.T) {
	repo := &subjectDeletionRepoFake{
		inventory:     domain.TweetSubjectInventory{Likes: 2},
		leaveResidual: true,
	}
	service := &TweetService{
		deleteSubjectRepo: repo, outboxEventRepo: &subjectDeletionOutboxFake{}, uow: new(MockUOWManager),
		subjectWriteFenceRepo: AllowTweetSubjectWrites(),
	}
	if _, err := service.DeleteTweetSubjectData(context.Background(), validTweetSubjectDeletionCommand()); !errors.Is(err, ErrSubjectDeletionResiduals) {
		t.Fatalf("residual error = %v, want %v", err, ErrSubjectDeletionResiduals)
	}
	if repo.operation != nil {
		t.Fatal("residual deletion persisted a completion receipt")
	}
}

func TestDeleteTweetSubjectDataRequiresInstalledWriteFence(t *testing.T) {
	repo := &subjectDeletionRepoFake{inventory: domain.TweetSubjectInventory{Likes: 1}}
	fenceRepo := AllowTweetSubjectWrites()
	fenceRepo.requireErr = domain.ErrTweetSubjectWriteFenceMissing
	service := &TweetService{
		deleteSubjectRepo: repo, outboxEventRepo: &subjectDeletionOutboxFake{}, uow: new(MockUOWManager),
		subjectWriteFenceRepo: fenceRepo,
	}

	_, err := service.DeleteTweetSubjectData(context.Background(), validTweetSubjectDeletionCommand())

	if !errors.Is(err, ErrSubjectWriteFenceMissing) {
		t.Fatalf("missing fence error = %v, want %v", err, ErrSubjectWriteFenceMissing)
	}
	if repo.deleteCalls != 0 || repo.operation != nil {
		t.Fatal("subject deletion executed before the write fence was installed")
	}
}

func TestDeleteTweetSubjectDataRejectsUnboundedTransaction(t *testing.T) {
	ids := make([]uint64, tweetSubjectDeletionMaxAuthoredIDs+1)
	for index := range ids {
		ids[index] = uint64(index + 1)
	}
	repo := &subjectDeletionRepoFake{inventory: domain.TweetSubjectInventory{AuthoredTweets: int64(len(ids))}, authoredIDs: ids}
	service := &TweetService{
		deleteSubjectRepo: repo, outboxEventRepo: &subjectDeletionOutboxFake{}, uow: new(MockUOWManager),
		subjectWriteFenceRepo: AllowTweetSubjectWrites(),
	}
	if _, err := service.DeleteTweetSubjectData(context.Background(), validTweetSubjectDeletionCommand()); !errors.Is(err, ErrSubjectDeletionTooLarge) {
		t.Fatalf("large deletion error = %v, want %v", err, ErrSubjectDeletionTooLarge)
	}
	if repo.deleteCalls != 0 || repo.operation != nil {
		t.Fatal("unbounded deletion executed side effects")
	}
}

func TestDeleteTweetSubjectDataRejectsExpiredCommand(t *testing.T) {
	service := &TweetService{}
	command := validTweetSubjectDeletionCommand()
	command.Deadline = time.Now().Add(-time.Second)
	if _, err := service.DeleteTweetSubjectData(context.Background(), command); !errors.Is(err, ErrSubjectDeletionInvalid) {
		t.Fatalf("expired command error = %v, want %v", err, ErrSubjectDeletionInvalid)
	}
}

func TestTweetSubjectDeletionReceiptMatchesAgentContract(t *testing.T) {
	completedAt := time.Date(2026, 9, 25, 9, 10, 11, 12000000, time.UTC)
	before := domain.TweetSubjectInventory{
		AuthoredTweets: 1, AuthoredComments: 2, Likes: 3,
		Bookmarks: 4, Retweets: 5, PollVotes: 6,
	}
	serviceOutcome := newTweetSubjectDeletionOutcome(
		sha256String("operation"), before, domain.TweetSubjectInventory{}, completedAt,
	)
	agentOutcome, err := agentdeletion.NewTweetSubjectDeletionOutcome(
		serviceOutcome.OperationKey,
		agentdeletion.TweetSubjectInventory{
			AuthoredTweets: before.AuthoredTweets, AuthoredComments: before.AuthoredComments,
			Likes: before.Likes, Bookmarks: before.Bookmarks,
			Retweets: before.Retweets, PollVotes: before.PollVotes,
		},
		agentdeletion.TweetSubjectInventory{},
		serviceOutcome.CompletedAt,
	)
	if err != nil {
		t.Fatal(err)
	}
	if serviceOutcome.ReceiptDigest != agentOutcome.ReceiptDigest {
		t.Fatalf("receipt digest mismatch service=%s agent=%s", serviceOutcome.ReceiptDigest, agentOutcome.ReceiptDigest)
	}
}

func validTweetSubjectDeletionCommand() TweetSubjectDeletionCommand {
	const userID uint64 = 42
	return TweetSubjectDeletionCommand{
		SchemaVersion: domain.TweetSubjectDeletionSchemaV1,
		OperationKey:  sha256String("operation"), UserID: userID,
		SubjectDigest:    tweetSubjectDigest(userID),
		GovernanceDigest: sha256String("governance-decision"),
		Deadline:         time.Now().Add(time.Minute),
	}
}
