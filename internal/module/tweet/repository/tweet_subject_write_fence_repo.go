package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"twitter-clone/internal/domain"
	"twitter-clone/internal/pkg/database/uow"
)

type tweetSubjectWriteFenceRepo struct {
	db *gorm.DB
}

func NewTweetSubjectWriteFenceRepository(db *gorm.DB) domain.TweetSubjectWriteFenceRepository {
	return &tweetSubjectWriteFenceRepo{db: db}
}

func (r *tweetSubjectWriteFenceRepo) AuthorizeSubjectWrite(ctx context.Context, userID uint64) error {
	if r == nil || r.db == nil || userID == 0 {
		return domain.ErrTweetSubjectWriteFenceCorrupt
	}
	fence, err := r.lockSubjectFence(ctx, userID)
	if err != nil {
		return err
	}
	if !fence.Fenced {
		return nil
	}
	if err := validatePersistedTweetSubjectWriteFence(fence); err != nil {
		return err
	}
	return domain.ErrTweetSubjectWriteFenced
}

func (r *tweetSubjectWriteFenceRepo) RequireSubjectWriteFence(ctx context.Context, userID uint64) error {
	if r == nil || r.db == nil || userID == 0 {
		return domain.ErrTweetSubjectWriteFenceCorrupt
	}
	fence, err := r.lockSubjectFence(ctx, userID)
	if err != nil {
		return err
	}
	if !fence.Fenced {
		return domain.ErrTweetSubjectWriteFenceMissing
	}
	return validatePersistedTweetSubjectWriteFence(fence)
}

func (r *tweetSubjectWriteFenceRepo) EnforceSubjectWriteFence(
	ctx context.Context,
	requested *domain.TweetSubjectWriteFence,
) (*domain.TweetSubjectWriteFence, bool, error) {
	if r == nil || r.db == nil || requested == nil || requested.UserID == 0 {
		return nil, false, domain.ErrTweetSubjectWriteFenceCorrupt
	}
	if err := validateRequestedTweetSubjectWriteFence(requested); err != nil {
		return nil, false, err
	}
	current, err := r.lockSubjectFence(ctx, requested.UserID)
	if err != nil {
		return nil, false, err
	}
	if current.Fenced {
		if err := validatePersistedTweetSubjectWriteFence(current); err != nil {
			return nil, false, err
		}
		if current.RequestID != requested.RequestID || current.SubjectDigest != requested.SubjectDigest ||
			current.PlanDigest != requested.PlanDigest || current.CapabilityDigest != requested.CapabilityDigest {
			return nil, false, domain.ErrTweetSubjectWriteFenceConflict
		}
		return current, true, nil
	}

	now := requested.EnforcedAt
	if now == 0 {
		now = time.Now().UTC().UnixMilli()
	}
	updates := map[string]any{
		"fenced": true, "request_id": requested.RequestID,
		"subject_digest": requested.SubjectDigest, "plan_digest": requested.PlanDigest,
		"capability_digest": requested.CapabilityDigest, "governance_digest": requested.GovernanceDigest,
		"fence_revision": int64(1), "enforced_at": now, "updated_at": now,
	}
	db := uow.ExtractTx(ctx, r.db).WithContext(ctx)
	result := db.Model(&domain.TweetSubjectWriteFence{}).
		Where("user_id = ? AND fenced = ?", requested.UserID, false).Updates(updates)
	if result.Error != nil {
		return nil, false, fmt.Errorf("enforce tweet subject write fence: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return nil, false, domain.ErrTweetSubjectWriteFenceConflict
	}
	current.Fenced = true
	current.RequestID = requested.RequestID
	current.SubjectDigest = requested.SubjectDigest
	current.PlanDigest = requested.PlanDigest
	current.CapabilityDigest = requested.CapabilityDigest
	current.GovernanceDigest = requested.GovernanceDigest
	current.FenceRevision = 1
	current.EnforcedAt = now
	current.UpdatedAt = now
	return current, false, validatePersistedTweetSubjectWriteFence(current)
}

func (r *tweetSubjectWriteFenceRepo) lockSubjectFence(
	ctx context.Context,
	userID uint64,
) (*domain.TweetSubjectWriteFence, error) {
	db := uow.ExtractTx(ctx, r.db).WithContext(ctx)
	now := time.Now().UTC().UnixMilli()
	baseline := &domain.TweetSubjectWriteFence{
		UserID: userID, SchemaVersion: domain.TweetSubjectWriteFenceSchemaV1,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(baseline).Error; err != nil {
		return nil, fmt.Errorf("ensure tweet subject write fence anchor: %w", err)
	}
	var fence domain.TweetSubjectWriteFence
	if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("user_id = ?", userID).First(&fence).Error; err != nil {
		return nil, fmt.Errorf("lock tweet subject write fence: %w", err)
	}
	if fence.UserID != userID || fence.SchemaVersion != domain.TweetSubjectWriteFenceSchemaV1 {
		return nil, domain.ErrTweetSubjectWriteFenceCorrupt
	}
	return &fence, nil
}

func validatePersistedTweetSubjectWriteFence(fence *domain.TweetSubjectWriteFence) error {
	if fence == nil || fence.UserID == 0 || fence.SchemaVersion != domain.TweetSubjectWriteFenceSchemaV1 ||
		!fence.Fenced || !isTweetFenceIdentity(fence.RequestID) ||
		fence.SubjectDigest != tweetFenceSubjectDigest(fence.UserID) ||
		!isTweetFenceSHA256(fence.PlanDigest) || !isTweetFenceSHA256(fence.CapabilityDigest) ||
		!isTweetFenceSHA256(fence.GovernanceDigest) || fence.FenceRevision < 1 ||
		fence.EnforcedAt <= 0 {
		return domain.ErrTweetSubjectWriteFenceCorrupt
	}
	return nil
}

func validateRequestedTweetSubjectWriteFence(fence *domain.TweetSubjectWriteFence) error {
	if fence == nil || fence.UserID == 0 || fence.SchemaVersion != domain.TweetSubjectWriteFenceSchemaV1 ||
		!fence.Fenced || !isTweetFenceIdentity(fence.RequestID) ||
		fence.SubjectDigest != tweetFenceSubjectDigest(fence.UserID) ||
		!isTweetFenceSHA256(fence.PlanDigest) || !isTweetFenceSHA256(fence.CapabilityDigest) ||
		!isTweetFenceSHA256(fence.GovernanceDigest) || fence.EnforcedAt <= 0 {
		return domain.ErrTweetSubjectWriteFenceCorrupt
	}
	return nil
}

func tweetFenceSubjectDigest(userID uint64) string {
	return tweetFenceSHA256("agent-data-deletion-subject/v1" + "\x00" + strconv.FormatUint(userID, 10))
}

func tweetFenceSHA256(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func isTweetFenceSHA256(value string) bool {
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func isTweetFenceIdentity(value string) bool {
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

var _ domain.TweetSubjectWriteFenceRepository = (*tweetSubjectWriteFenceRepo)(nil)
