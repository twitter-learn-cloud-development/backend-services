package repository

import (
	"context"
	"fmt"

	"gorm.io/gorm"
	"twitter-clone/internal/domain"
)

var (
	_ domain.FollowerPageRepository = (*followRepo)(nil)
	_ domain.FolloweePageRepository = (*followRepo)(nil)
)

func (r *followRepo) followerPageQuery(ctx context.Context, userID uint64, cursor uint64, limit int) *gorm.DB {
	query := r.db.WithContext(ctx).Model(&domain.Follow{}).
		Where("followee_id = ? AND deleted_at = 0", userID)
	if cursor > 0 {
		query = query.Where("id < ?", cursor)
	}
	return query.Order("id DESC").Limit(limit + 1)
}

func (r *followRepo) followeePageQuery(ctx context.Context, userID uint64, cursor uint64, limit int) *gorm.DB {
	query := r.db.WithContext(ctx).Model(&domain.Follow{}).
		Where("follower_id = ? AND deleted_at = 0", userID)
	if cursor > 0 {
		query = query.Where("id < ?", cursor)
	}
	return query.Order("id DESC").Limit(limit + 1)
}

// ListFolloweePage is a bounded internal page port. Service independently
// applies the public maximum of 100; no lookahead row is lost to that cap.
func (r *followRepo) ListFolloweePage(ctx context.Context, userID uint64, cursor uint64, limit int) (domain.FolloweePage, error) {
	if limit <= 0 {
		limit = 500
	}
	if limit > 1000 {
		limit = 1000
	}
	var follows []domain.Follow
	if err := r.followeePageQuery(ctx, userID, cursor, limit).Find(&follows).Error; err != nil {
		return domain.FolloweePage{}, fmt.Errorf("failed to list followee page: %w", err)
	}
	return buildFolloweePage(follows, limit), nil
}

func buildFolloweePage(follows []domain.Follow, limit int) domain.FolloweePage {
	hasMore := limit > 0 && len(follows) > limit
	if hasMore {
		follows = follows[:limit]
	}
	ids := make([]uint64, len(follows))
	for i, follow := range follows {
		ids[i] = follow.FolloweeID
	}
	var nextCursor uint64
	if hasMore && len(follows) > 0 {
		nextCursor = follows[len(follows)-1].ID
	}
	return domain.FolloweePage{FolloweeIDs: ids, NextCursor: nextCursor, HasMore: hasMore}
}
