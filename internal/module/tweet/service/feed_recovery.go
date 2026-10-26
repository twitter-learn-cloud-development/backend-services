package service

import (
	"context"
	"time"

	"go.uber.org/zap"
	"twitter-clone/internal/domain"
	"twitter-clone/internal/module/tweet/cache"
	"twitter-clone/pkg/logger"
)

func (s *TweetService) recoverFeedProjection(ctx context.Context, userID uint64) {
	if s.timelineCache == nil || ctx.Err() != nil {
		return
	}
	cacheCtx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
	token, acquired, err := s.timelineCache.TryBeginFeedRecovery(cacheCtx, userID)
	cancel()
	if err != nil {
		logger.Warn(ctx, "feed projection unavailable; authoritative page retained")
		return
	}
	if !acquired {
		return
	}
	defer func() {
		releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
		defer releaseCancel()
		if err := s.timelineCache.ReleaseFeedRecovery(releaseCtx, userID, token); err != nil {
			logger.Warn(ctx, "feed projection lease release failed")
		}
	}()
	pager, ok := s.repo.(domain.FollowingFeedRepository)
	if !ok {
		return
	}
	rebuildCtx, rebuildCancel := context.WithTimeout(ctx, 2*time.Second)
	defer rebuildCancel()
	rows, err := pager.ListFollowingFeed(rebuildCtx, userID, 0, cache.TimelineMaxSize)
	if err != nil {
		logger.Warn(ctx, "feed projection rebuild failed")
		return
	}
	if len(rows) > cache.TimelineMaxSize {
		logger.Warn(ctx, "feed projection rebuild returned invalid page")
		return
	}
	ids := make([]uint64, 0, len(rows))
	var previous uint64
	for _, row := range rows {
		if row == nil || row.ID == 0 || row.UserID == 0 || row.DeletedAt != 0 || (previous > 0 && row.ID >= previous) {
			logger.Warn(ctx, "feed projection rebuild returned invalid page")
			return
		}
		ids = append(ids, row.ID)
		previous = row.ID
	}
	committed, err := s.timelineCache.CommitFeedRecovery(rebuildCtx, userID, token, ids)
	if err != nil {
		logger.Warn(ctx, "feed projection rebuild commit failed")
		return
	}
	logger.Info(ctx, "feed projection recovery finished", zap.Int("candidates", len(ids)), zap.Bool("committed", committed))
}
