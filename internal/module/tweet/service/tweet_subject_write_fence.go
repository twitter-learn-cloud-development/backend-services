package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"twitter-clone/internal/domain"
)

const tweetSubjectWriteFenceMaxLease = 15 * time.Minute

type TweetSubjectWriteFenceCommand struct {
	SchemaVersion    string
	RequestID        string
	UserID           uint64
	SubjectDigest    string
	PlanDigest       string
	CapabilityDigest string
	GovernanceDigest string
	Deadline         time.Time
}

type TweetSubjectWriteFenceOutcome struct {
	SchemaVersion string
	RequestID     string
	FenceRevision int64
	Replayed      bool
	EnforcedAt    time.Time
}

func (s *TweetService) EnforceTweetSubjectWriteFence(
	ctx context.Context,
	command TweetSubjectWriteFenceCommand,
) (TweetSubjectWriteFenceOutcome, error) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	command.RequestID = strings.TrimSpace(command.RequestID)
	command.SubjectDigest = strings.ToLower(strings.TrimSpace(command.SubjectDigest))
	command.PlanDigest = strings.ToLower(strings.TrimSpace(command.PlanDigest))
	command.CapabilityDigest = strings.ToLower(strings.TrimSpace(command.CapabilityDigest))
	command.GovernanceDigest = strings.ToLower(strings.TrimSpace(command.GovernanceDigest))
	if err := validateTweetSubjectWriteFenceCommand(command, now); err != nil {
		return TweetSubjectWriteFenceOutcome{}, err
	}
	if s.subjectWriteFenceRepo == nil || s.uow == nil {
		return TweetSubjectWriteFenceOutcome{}, ErrSubjectWriteFenceUnavailable
	}

	var persisted *domain.TweetSubjectWriteFence
	var replayed bool
	err := s.uow.Do(ctx, func(txCtx context.Context) error {
		var err error
		persisted, replayed, err = s.subjectWriteFenceRepo.EnforceSubjectWriteFence(txCtx, &domain.TweetSubjectWriteFence{
			UserID: command.UserID, SchemaVersion: command.SchemaVersion, Fenced: true,
			RequestID: command.RequestID, SubjectDigest: command.SubjectDigest,
			PlanDigest: command.PlanDigest, CapabilityDigest: command.CapabilityDigest,
			GovernanceDigest: command.GovernanceDigest, EnforcedAt: now.UnixMilli(),
		})
		return err
	})
	if err != nil {
		return TweetSubjectWriteFenceOutcome{}, err
	}
	return TweetSubjectWriteFenceOutcome{
		SchemaVersion: persisted.SchemaVersion, RequestID: persisted.RequestID,
		FenceRevision: persisted.FenceRevision, Replayed: replayed,
		EnforcedAt: time.UnixMilli(persisted.EnforcedAt).UTC(),
	}, nil
}

func (s *TweetService) authorizeSubjectWrite(ctx context.Context, userID uint64) error {
	if s.subjectWriteFenceRepo == nil || userID == 0 {
		return ErrSubjectWriteFenceUnavailable
	}
	err := s.subjectWriteFenceRepo.AuthorizeSubjectWrite(ctx, userID)
	if err == nil || errors.Is(err, ErrSubjectWriteFenced) || errors.Is(err, ErrSubjectWriteFenceCorrupt) {
		return err
	}
	return fmt.Errorf("%w: %v", ErrSubjectWriteFenceUnavailable, err)
}

func (s *TweetService) requireSubjectWriteFence(ctx context.Context, userID uint64) error {
	if s.subjectWriteFenceRepo == nil || userID == 0 {
		return ErrSubjectWriteFenceUnavailable
	}
	err := s.subjectWriteFenceRepo.RequireSubjectWriteFence(ctx, userID)
	if err == nil || errors.Is(err, ErrSubjectWriteFenceMissing) || errors.Is(err, ErrSubjectWriteFenceCorrupt) {
		return err
	}
	return fmt.Errorf("%w: %v", ErrSubjectWriteFenceUnavailable, err)
}

func validateTweetSubjectWriteFenceCommand(command TweetSubjectWriteFenceCommand, now time.Time) error {
	if command.SchemaVersion != domain.TweetSubjectWriteFenceSchemaV1 || command.UserID == 0 ||
		!isTweetSubjectWriteFenceIdentity(command.RequestID) ||
		command.SubjectDigest != tweetSubjectDigest(command.UserID) ||
		!isLowerSHA256(command.PlanDigest) || !isLowerSHA256(command.CapabilityDigest) ||
		!isLowerSHA256(command.GovernanceDigest) || now.IsZero() || !command.Deadline.After(now) ||
		command.Deadline.Sub(now) > tweetSubjectWriteFenceMaxLease {
		return ErrSubjectWriteFenceInvalid
	}
	return nil
}

func isTweetSubjectWriteFenceIdentity(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || strings.ContainsRune("_-./", character) {
			continue
		}
		return false
	}
	return true
}
