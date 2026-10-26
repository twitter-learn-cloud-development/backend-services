package repository

import (
	"errors"
	"testing"
	"time"

	"twitter-clone/internal/domain"
)

func TestValidateRequestedTweetSubjectWriteFence(t *testing.T) {
	digest := tweetFenceSHA256("binding")
	fence := &domain.TweetSubjectWriteFence{
		UserID: 42, SchemaVersion: domain.TweetSubjectWriteFenceSchemaV1, Fenced: true,
		RequestID: "delete/request-7", SubjectDigest: tweetFenceSubjectDigest(42),
		PlanDigest: digest, CapabilityDigest: digest, GovernanceDigest: digest,
		EnforcedAt: time.Now().UTC().UnixMilli(),
	}
	if err := validateRequestedTweetSubjectWriteFence(fence); err != nil {
		t.Fatal(err)
	}

	fence.SubjectDigest = tweetFenceSubjectDigest(43)
	if err := validateRequestedTweetSubjectWriteFence(fence); !errors.Is(err, domain.ErrTweetSubjectWriteFenceCorrupt) {
		t.Fatalf("validateRequestedTweetSubjectWriteFence() error = %v", err)
	}
}

func TestValidatePersistedTweetSubjectWriteFenceFailsClosedOnCorruption(t *testing.T) {
	digest := tweetFenceSHA256("binding")
	fence := &domain.TweetSubjectWriteFence{
		UserID: 42, SchemaVersion: domain.TweetSubjectWriteFenceSchemaV1, Fenced: true,
		RequestID: "delete/request-7", SubjectDigest: tweetFenceSubjectDigest(42),
		PlanDigest: digest, CapabilityDigest: digest, GovernanceDigest: digest,
		FenceRevision: 1, EnforcedAt: time.Now().UTC().UnixMilli(),
	}
	if err := validatePersistedTweetSubjectWriteFence(fence); err != nil {
		t.Fatal(err)
	}

	fence.CapabilityDigest = "not-a-digest"
	if err := validatePersistedTweetSubjectWriteFence(fence); !errors.Is(err, domain.ErrTweetSubjectWriteFenceCorrupt) {
		t.Fatalf("validatePersistedTweetSubjectWriteFence() error = %v", err)
	}
}
