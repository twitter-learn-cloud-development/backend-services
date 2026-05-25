package service

import (
	"context"
	"fmt"
	"go.uber.org/zap"
	"twitter-clone/pkg/logger"
)

// AlignCelebrityCache 全局对账与校准大V缓存 (定时任务或维护端调用)
func (s *FollowService) AlignCelebrityCache(ctx context.Context) error {
	logger.Info(ctx, "🔄 [Sync Job] Starting celebrity cache alignment...")

	// 1. 从关系库查询粉丝数超过 5000 的大V ID 列表
	dbCelebrities, err := s.repo.GetCelebrities(ctx, CelebrityPromoThreshold)
	if err != nil {
		return fmt.Errorf("failed to get celebrities from DB: %w", err)
	}

	logger.Info(ctx, "🔄 [Sync Job] Loaded celebrities from DB", zap.Int("count", len(dbCelebrities)))

	// 2. 差分校准 global:celebrities 缓存集合
	if err := s.timelineCache.SyncGlobalCelebrities(ctx, dbCelebrities); err != nil {
		return fmt.Errorf("failed to sync global celebrities: %w", err)
	}
	logger.Info(ctx, "✅ [Sync Job] Global celebrities synchronized successfully")

	// 3. 校准大V的粉丝关联缓存
	logger.Info(ctx, "🔄 [Sync Job] Starting celebrity-follower relationship alignment...")
	for _, celebrityID := range dbCelebrities {
		// 获取这名大V当前数据库中的全量粉丝
		followerIDs, err := s.repo.GetFollowers(ctx, celebrityID, 0, CelebrityPromoThreshold+50000)
		if err != nil {
			logger.Warn(ctx, "⚠️ [Sync Job] Failed to get followers for celebrity", zap.Error(err), zap.Uint64("celebrity_id", celebrityID))
			continue
		}

		if len(followerIDs) > 0 {
			// 批量校准，将被关注者大V批量关联加入至全量粉丝的 user:celebrities:ID 缓存中
			if err := s.timelineCache.BatchAddCelebrityFollowees(ctx, followerIDs, celebrityID); err != nil {
				logger.Warn(ctx, "⚠️ [Sync Job] Failed to batch sync celebrity followers", zap.Error(err), zap.Uint64("celebrity_id", celebrityID))
			}
		}
	}

	logger.Info(ctx, "✅ [Sync Job] Celebrity cache alignment finished successfully")
	return nil
}
