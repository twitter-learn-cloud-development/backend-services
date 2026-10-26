package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"twitter-clone/internal/domain"
	"twitter-clone/internal/events"
)

const (
	tweetSubjectDeletionMaxLease       = 15 * time.Minute
	tweetSubjectDeletionMaxAuthoredIDs = 1000
	tweetSubjectDigestSchema           = "agent-data-deletion-subject/v1"
)

type TweetSubjectDeletionCommand struct {
	SchemaVersion    string
	OperationKey     string
	UserID           uint64
	SubjectDigest    string
	GovernanceDigest string
	Deadline         time.Time
}

type TweetSubjectDeletionOutcome struct {
	SchemaVersion string
	OperationKey  string
	ReceiptDigest string
	Before        domain.TweetSubjectInventory
	After         domain.TweetSubjectInventory
	Replayed      bool
	CompletedAt   time.Time
}

func (s *TweetService) ObserveTweetSubjectData(ctx context.Context, userID uint64) (domain.TweetSubjectInventory, error) {
	if userID == 0 {
		return domain.TweetSubjectInventory{}, ErrSubjectDeletionInvalid
	}
	if s.deleteSubjectRepo == nil {
		return domain.TweetSubjectInventory{}, ErrSubjectDeletionUnavailable
	}
	return s.deleteSubjectRepo.ObserveSubject(ctx, userID)
}

// DeleteTweetSubjectData hard-deletes subject-owned Tweet records and commits
// projection cleanup events plus a content-free idempotency receipt atomically.
// The caller must recheck legal-hold governance immediately before this call;
// the short-lived governance digest is bound into the durable receipt.
func (s *TweetService) DeleteTweetSubjectData(
	ctx context.Context,
	command TweetSubjectDeletionCommand,
) (TweetSubjectDeletionOutcome, error) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	command.OperationKey = strings.ToLower(strings.TrimSpace(command.OperationKey))
	command.SubjectDigest = strings.ToLower(strings.TrimSpace(command.SubjectDigest))
	command.GovernanceDigest = strings.ToLower(strings.TrimSpace(command.GovernanceDigest))
	if err := validateTweetSubjectDeletionCommand(command, now); err != nil {
		return TweetSubjectDeletionOutcome{}, err
	}
	if s.deleteSubjectRepo == nil || s.subjectWriteFenceRepo == nil || s.uow == nil || s.outboxEventRepo == nil {
		return TweetSubjectDeletionOutcome{}, ErrSubjectDeletionUnavailable
	}
	if operation, err := s.deleteSubjectRepo.GetOperation(ctx, command.OperationKey); err == nil {
		return replayTweetSubjectDeletion(operation, command)
	} else if !errors.Is(err, domain.ErrTweetSubjectDeletionNotFound) {
		return TweetSubjectDeletionOutcome{}, err
	}

	var outcome TweetSubjectDeletionOutcome
	var authoredTweetIDs []uint64
	err := s.uow.Do(ctx, func(txCtx context.Context) error {
		if fenceErr := s.requireSubjectWriteFence(txCtx, command.UserID); fenceErr != nil {
			return fenceErr
		}
		if operation, getErr := s.deleteSubjectRepo.GetOperation(txCtx, command.OperationKey); getErr == nil {
			var replayErr error
			outcome, replayErr = replayTweetSubjectDeletion(operation, command)
			return replayErr
		} else if !errors.Is(getErr, domain.ErrTweetSubjectDeletionNotFound) {
			return getErr
		}

		var listErr error
		authoredTweetIDs, listErr = s.deleteSubjectRepo.ListAuthoredTweetIDs(
			txCtx, command.UserID, tweetSubjectDeletionMaxAuthoredIDs+1,
		)
		if listErr != nil {
			return listErr
		}
		if len(authoredTweetIDs) > tweetSubjectDeletionMaxAuthoredIDs {
			return ErrSubjectDeletionTooLarge
		}
		before, observeErr := s.deleteSubjectRepo.ObserveSubject(txCtx, command.UserID)
		if observeErr != nil {
			return observeErr
		}
		if deleteErr := s.deleteSubjectRepo.DeleteOwnedData(txCtx, command.UserID, authoredTweetIDs); deleteErr != nil {
			return deleteErr
		}
		after, observeErr := s.deleteSubjectRepo.ObserveSubject(txCtx, command.UserID)
		if observeErr != nil {
			return observeErr
		}
		if after.Total() != 0 {
			return ErrSubjectDeletionResiduals
		}

		for _, tweetID := range authoredTweetIDs {
			payload, marshalErr := json.Marshal(events.TweetDeletedEvent{TweetID: tweetID, AuthorID: command.UserID})
			if marshalErr != nil {
				return fmt.Errorf("marshal tweet subject deletion event: %w", marshalErr)
			}
			dedupKey := fmt.Sprintf("tweet-subject-deleted:v1:%s:%d", command.OperationKey, tweetID)
			if _, createErr := s.outboxEventRepo.CreateIdempotent(txCtx, &domain.OutboxEvent{
				EventType: events.OutboxEventTypeTweetDeleted,
				Payload:   string(payload), DedupKey: &dedupKey, CreatedAt: now.UnixMilli(),
			}); createErr != nil {
				return fmt.Errorf("enqueue tweet subject deletion cleanup: %w", createErr)
			}
		}

		outcome = newTweetSubjectDeletionOutcome(command.OperationKey, before, after, now)
		operation := operationFromTweetSubjectDeletionOutcome(command, outcome)
		if createErr := s.deleteSubjectRepo.CreateOperation(txCtx, operation); createErr != nil {
			return createErr
		}
		return nil
	})
	if errors.Is(err, domain.ErrTweetSubjectDeletionExists) {
		operation, getErr := s.deleteSubjectRepo.GetOperation(ctx, command.OperationKey)
		if getErr != nil {
			return TweetSubjectDeletionOutcome{}, getErr
		}
		return replayTweetSubjectDeletion(operation, command)
	}
	if err != nil {
		return TweetSubjectDeletionOutcome{}, err
	}

	for _, tweetID := range authoredTweetIDs {
		if s.l1Cache != nil {
			_ = s.l1Cache.Delete(fmt.Sprintf("tweet:base:%d", tweetID))
		}
		if s.timelineCache != nil {
			_ = s.timelineCache.InvalidateBaseTweet(ctx, tweetID)
		}
	}
	return outcome, nil
}

func validateTweetSubjectDeletionCommand(command TweetSubjectDeletionCommand, now time.Time) error {
	if command.SchemaVersion != domain.TweetSubjectDeletionSchemaV1 || command.UserID == 0 ||
		!isLowerSHA256(command.OperationKey) || !isLowerSHA256(command.SubjectDigest) ||
		command.SubjectDigest != tweetSubjectDigest(command.UserID) || !isLowerSHA256(command.GovernanceDigest) ||
		now.IsZero() || !command.Deadline.After(now) || command.Deadline.Sub(now) > tweetSubjectDeletionMaxLease {
		return ErrSubjectDeletionInvalid
	}
	return nil
}

func replayTweetSubjectDeletion(
	operation *domain.TweetSubjectDeletionOperation,
	command TweetSubjectDeletionCommand,
) (TweetSubjectDeletionOutcome, error) {
	if operation == nil || operation.OperationKey != command.OperationKey ||
		operation.SubjectDigest != command.SubjectDigest || operation.GovernanceDigest != command.GovernanceDigest {
		return TweetSubjectDeletionOutcome{}, ErrSubjectDeletionConflict
	}
	outcome := TweetSubjectDeletionOutcome{
		SchemaVersion: domain.TweetSubjectDeletionSchemaV1,
		OperationKey:  operation.OperationKey, ReceiptDigest: operation.ReceiptDigest,
		Before: operation.BeforeInventory(), After: operation.AfterInventory(),
		Replayed: true, CompletedAt: time.UnixMilli(operation.CompletedAt).UTC(),
	}
	if outcome.After.Total() != 0 || outcome.ReceiptDigest != tweetSubjectDeletionReceiptDigest(outcome) {
		return TweetSubjectDeletionOutcome{}, ErrSubjectDeletionConflict
	}
	return outcome, nil
}

func newTweetSubjectDeletionOutcome(
	operationKey string,
	before domain.TweetSubjectInventory,
	after domain.TweetSubjectInventory,
	completedAt time.Time,
) TweetSubjectDeletionOutcome {
	outcome := TweetSubjectDeletionOutcome{
		SchemaVersion: domain.TweetSubjectDeletionSchemaV1,
		OperationKey:  operationKey, Before: before, After: after,
		CompletedAt: completedAt.UTC().Truncate(time.Millisecond),
	}
	outcome.ReceiptDigest = tweetSubjectDeletionReceiptDigest(outcome)
	return outcome
}

func operationFromTweetSubjectDeletionOutcome(
	command TweetSubjectDeletionCommand,
	outcome TweetSubjectDeletionOutcome,
) *domain.TweetSubjectDeletionOperation {
	return &domain.TweetSubjectDeletionOperation{
		OperationKey: command.OperationKey, SubjectDigest: command.SubjectDigest,
		GovernanceDigest: command.GovernanceDigest,
		BeforeTweets:     outcome.Before.AuthoredTweets, BeforeComments: outcome.Before.AuthoredComments,
		BeforeLikes: outcome.Before.Likes, BeforeBookmarks: outcome.Before.Bookmarks,
		BeforeRetweets: outcome.Before.Retweets, BeforePollVotes: outcome.Before.PollVotes,
		AfterTweets: outcome.After.AuthoredTweets, AfterComments: outcome.After.AuthoredComments,
		AfterLikes: outcome.After.Likes, AfterBookmarks: outcome.After.Bookmarks,
		AfterRetweets: outcome.After.Retweets, AfterPollVotes: outcome.After.PollVotes,
		ReceiptDigest: outcome.ReceiptDigest, CompletedAt: outcome.CompletedAt.UnixMilli(),
		CreatedAt: outcome.CompletedAt.UnixMilli(),
	}
}

func tweetSubjectDeletionReceiptDigest(outcome TweetSubjectDeletionOutcome) string {
	counts := []int64{
		outcome.Before.AuthoredTweets, outcome.Before.AuthoredComments, outcome.Before.Likes,
		outcome.Before.Bookmarks, outcome.Before.Retweets, outcome.Before.PollVotes,
		outcome.After.AuthoredTweets, outcome.After.AuthoredComments, outcome.After.Likes,
		outcome.After.Bookmarks, outcome.After.Retweets, outcome.After.PollVotes,
	}
	parts := []string{outcome.SchemaVersion, outcome.OperationKey}
	for _, count := range counts {
		parts = append(parts, strconv.FormatInt(count, 10))
	}
	parts = append(parts, outcome.CompletedAt.UTC().Format(time.RFC3339Nano))
	return sha256String(strings.Join(parts, "\x00"))
}

func tweetSubjectDigest(userID uint64) string {
	return sha256String(tweetSubjectDigestSchema + "\x00" + strconv.FormatUint(userID, 10))
}

func sha256String(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func isLowerSHA256(value string) bool {
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
