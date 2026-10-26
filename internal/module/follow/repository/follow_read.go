package repository

import (
	"context"
	"fmt"

	"gorm.io/gorm"
	"twitter-clone/internal/domain"
)

var _ domain.FollowReadRepository = (*followRepo)(nil)

// This adapter exports only current relationship reads to other modules.
type followReadRepo struct{ inner *followRepo }

func NewFollowReadRepository(db *gorm.DB) domain.FollowReadRepository {
	return &followReadRepo{inner: &followRepo{db: db}}
}
func (r *followReadRepo) BatchIsFollowing(ctx context.Context, viewer uint64, ids []uint64) (map[uint64]bool, error) {
	return r.inner.BatchIsFollowing(ctx, viewer, ids)
}

func (r *followRepo) batchFollowingQuery(ctx context.Context, followerID uint64, ids []uint64) *gorm.DB {
	return r.db.WithContext(ctx).Model(&domain.Follow{}).Select("DISTINCT followee_id").
		Where("follower_id = ? AND deleted_at = 0 AND followee_id IN ?", followerID, ids)
}

func (r *followRepo) BatchIsFollowing(ctx context.Context, followerID uint64, ids []uint64) (map[uint64]bool, error) {
	if followerID == 0 || len(ids) > 100 {
		return nil, fmt.Errorf("invalid follow read batch")
	}
	result := make(map[uint64]bool, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	unique := make([]uint64, 0, len(ids))
	for _, id := range ids {
		if id == 0 {
			return nil, fmt.Errorf("invalid follow read batch")
		}
		if _, seen := result[id]; !seen {
			result[id] = false
			unique = append(unique, id)
		}
	}
	var rows []uint64
	if err := r.batchFollowingQuery(ctx, followerID, unique).Pluck("followee_id", &rows).Error; err != nil {
		return nil, fmt.Errorf("read current follows: %w", err)
	}
	for _, id := range rows {
		if _, ok := result[id]; ok {
			result[id] = true
		}
	}
	return result, nil
}
