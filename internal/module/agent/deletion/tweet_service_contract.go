package deletion

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
)

const TweetSubjectDeletionContractV1 = "tweet-subject-deletion/v1"

type TweetSubjectInventory struct {
	AuthoredTweets   int64 `json:"authored_tweets"`
	AuthoredComments int64 `json:"authored_comments"`
	Likes            int64 `json:"likes"`
	Bookmarks        int64 `json:"bookmarks"`
	Retweets         int64 `json:"retweets"`
	PollVotes        int64 `json:"poll_votes"`
}

type TweetSubjectDeletionCommand struct {
	SchemaVersion    string    `json:"schema_version"`
	OperationKey     string    `json:"operation_key"`
	SubjectDigest    string    `json:"subject_digest"`
	UserID           uint64    `json:"-"`
	GovernanceDigest string    `json:"governance_digest"`
	Deadline         time.Time `json:"deadline"`
}

type TweetSubjectDeletionOutcome struct {
	SchemaVersion string                `json:"schema_version"`
	OperationKey  string                `json:"operation_key"`
	ReceiptDigest string                `json:"receipt_digest"`
	Before        TweetSubjectInventory `json:"before"`
	After         TweetSubjectInventory `json:"after"`
	Replayed      bool                  `json:"replayed"`
	CompletedAt   time.Time             `json:"completed_at"`
}

func NewTweetSubjectDeletionOutcome(
	operationKey string,
	before TweetSubjectInventory,
	after TweetSubjectInventory,
	completedAt time.Time,
) (TweetSubjectDeletionOutcome, error) {
	outcome := TweetSubjectDeletionOutcome{
		SchemaVersion: TweetSubjectDeletionContractV1,
		OperationKey:  strings.ToLower(strings.TrimSpace(operationKey)),
		Before:        before,
		After:         after,
		CompletedAt:   completedAt.UTC(),
	}
	outcome.ReceiptDigest = tweetSubjectDeletionOutcomeDigest(outcome)
	if err := ValidateTweetSubjectDeletionOutcome(outcome, operationKey); err != nil {
		return TweetSubjectDeletionOutcome{}, err
	}
	return outcome, nil
}

// TweetSubjectDeletionClient is the internal service boundary required by the
// data-deletion worker. The adapter must recheck governance immediately before
// the call; the Tweet service owns its SQL transaction, outbox/cache cleanup
// and persistent receipt bound to that short-lived governance decision.
type TweetSubjectDeletionClient interface {
	ObserveSubjectContent(context.Context, uint64) (TweetSubjectInventory, error)
	DeleteSubjectContent(context.Context, TweetSubjectDeletionCommand) (TweetSubjectDeletionOutcome, error)
}

func TweetSubjectDeletionScopeDigest() string {
	return DeletionScopeDigest(
		"tweet.authored_tweets", "tweet.authored_comments", "tweet.likes",
		"tweet.bookmarks", "tweet.retweets", "tweet.poll_votes",
	)
}

func ValidateTweetSubjectInventory(inventory TweetSubjectInventory) error {
	values := []int64{inventory.AuthoredTweets, inventory.AuthoredComments, inventory.Likes, inventory.Bookmarks, inventory.Retweets, inventory.PollVotes}
	for _, value := range values {
		if value < 0 {
			return errors.New("tweet subject inventory contains a negative count")
		}
	}
	return nil
}

func TweetSubjectInventoryTotal(inventory TweetSubjectInventory) int64 {
	return inventory.AuthoredTweets + inventory.AuthoredComments + inventory.Likes + inventory.Bookmarks + inventory.Retweets + inventory.PollVotes
}

func ValidateTweetSubjectDeletionCommand(command TweetSubjectDeletionCommand, now time.Time) error {
	if command.SchemaVersion != TweetSubjectDeletionContractV1 || !isSHA256(command.OperationKey) ||
		command.UserID == 0 || command.SubjectDigest != DeletionSubjectDigest(command.UserID) ||
		!isSHA256(command.GovernanceDigest) || now.IsZero() || !command.Deadline.After(now) ||
		command.Deadline.Sub(now) > maxStageLease {
		return errors.New("tweet subject deletion command is invalid")
	}
	return nil
}

func ValidateTweetSubjectDeletionOutcome(outcome TweetSubjectDeletionOutcome, operationKey string) error {
	if outcome.SchemaVersion != TweetSubjectDeletionContractV1 || outcome.OperationKey != strings.ToLower(strings.TrimSpace(operationKey)) ||
		!isSHA256(outcome.OperationKey) || !isSHA256(outcome.ReceiptDigest) || outcome.CompletedAt.IsZero() ||
		outcome.ReceiptDigest != tweetSubjectDeletionOutcomeDigest(outcome) {
		return errors.New("tweet subject deletion outcome identity is invalid")
	}
	if err := ValidateTweetSubjectInventory(outcome.Before); err != nil {
		return err
	}
	if err := ValidateTweetSubjectInventory(outcome.After); err != nil {
		return err
	}
	if TweetSubjectInventoryTotal(outcome.After) != 0 {
		return ErrDeletionResidualsRemain
	}
	return nil
}

func tweetSubjectDeletionOutcomeDigest(outcome TweetSubjectDeletionOutcome) string {
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
	return sha256Hex(strings.Join(parts, "\x00"))
}
