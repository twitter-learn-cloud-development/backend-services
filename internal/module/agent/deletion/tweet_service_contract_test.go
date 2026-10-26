package deletion

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestTweetSubjectDeletionContractRequiresZeroResidualAndHidesUserID(t *testing.T) {
	now := time.Date(2026, 9, 25, 20, 0, 0, 0, time.UTC)
	command := TweetSubjectDeletionCommand{
		SchemaVersion: TweetSubjectDeletionContractV1, OperationKey: sha256Hex("operation"),
		SubjectDigest: DeletionSubjectDigest(42), UserID: 42, GovernanceDigest: sha256Hex("governance"),
		Deadline: now.Add(time.Minute),
	}
	if err := ValidateTweetSubjectDeletionCommand(command, now); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(command)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"user_id"`) {
		t.Fatalf("command leaked raw user identity: %s", encoded)
	}
	outcome, err := NewTweetSubjectDeletionOutcome(
		command.OperationKey,
		TweetSubjectInventory{AuthoredTweets: 2, Likes: 3},
		TweetSubjectInventory{},
		now.Add(time.Second),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateTweetSubjectDeletionOutcome(outcome, command.OperationKey); err != nil {
		t.Fatal(err)
	}
	outcome.After.Likes = 1
	outcome.ReceiptDigest = tweetSubjectDeletionOutcomeDigest(outcome)
	if err := ValidateTweetSubjectDeletionOutcome(outcome, command.OperationKey); !errors.Is(err, ErrDeletionResidualsRemain) {
		t.Fatalf("residual validation error = %v", err)
	}
}
