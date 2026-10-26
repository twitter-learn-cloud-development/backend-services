package repository

import (
	"context"
	"fmt"

	"gorm.io/gorm"
	"twitter-clone/internal/domain"
)

var _ domain.FollowingFeedRepository = (*tweetRepo)(nil)

func (r *tweetRepo) followingFeedQuery(ctx context.Context, viewerID, before uint64, limit int) *gorm.DB {
	// Read-only join within the current shared social DB. Follow owns writes;
	// a future separate store must implement this port with a governed projection.
	q := r.db.WithContext(ctx).Model(&domain.Tweet{}).
		Where("tweets.deleted_at = 0").
		Where("tweets.user_id = ? OR (tweets.visible_type IN ? AND EXISTS (SELECT 1 FROM follows f WHERE f.follower_id = ? AND f.followee_id = tweets.user_id AND f.deleted_at = 0))", viewerID, []int{domain.VisiblePublic, domain.VisibleFollows}, viewerID)
	if before > 0 {
		q = q.Where("tweets.id < ?", before)
	}
	return q.Order("tweets.id DESC").Limit(limit)
}

func (r *tweetRepo) ListFollowingFeed(ctx context.Context, viewerID, before uint64, limit int) ([]*domain.Tweet, error) {
	if viewerID == 0 || limit < 1 || limit > 1000 {
		return nil, fmt.Errorf("invalid following feed request")
	}
	rows := make([]*domain.Tweet, 0)
	if err := r.followingFeedQuery(ctx, viewerID, before, limit).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("read current following feed: %w", err)
	}
	return rows, nil
}

// ExplainFollowingFeed reuses the production query with bound parameters.
// The explicit acceptance CLI filters the JSON plan before persisting it.
func (r *tweetRepo) ExplainFollowingFeed(ctx context.Context, viewerID, before uint64, limit int) (string, error) {
	if viewerID == 0 || limit < 1 || limit > 1000 {
		return "", fmt.Errorf("invalid following feed explain request")
	}
	var rows []*domain.Tweet
	query := r.followingFeedQuery(ctx, viewerID, before, limit).Session(&gorm.Session{DryRun: true}).Find(&rows)
	if query.Error != nil {
		return "", query.Error
	}
	var plan string
	if err := r.db.WithContext(ctx).Raw("EXPLAIN FORMAT=JSON "+query.Statement.SQL.String(), query.Statement.Vars...).Row().Scan(&plan); err != nil {
		return "", fmt.Errorf("explain current following feed: %w", err)
	}
	return plan, nil
}
