package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"twitter-clone/internal/domain"
	"twitter-clone/internal/events"
	"twitter-clone/internal/module/tweet/cache"
	"twitter-clone/pkg/logger"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"
)

const (
	// MaxContentLength 推文最大长度
	MaxContentLength = 280

	// MaxMediaCount 最大媒体数量
	MaxMediaCount = 4
)

// EventProducer 事件生产者接口
type EventProducer interface {
	PublishTweetCreated(ctx context.Context, event *events.TweetCreatedEvent) error
	PublishTweetDeleted(ctx context.Context, event *events.TweetDeletedEvent) error
	PublishTweetLiked(ctx context.Context, event *events.TweetLikedEvent) error
	PublishCommentCreated(ctx context.Context, event *events.CommentCreatedEvent) error
}

// TweetService 推文服务（带消息队列）
type TweetService struct {
	repo          domain.TweetRepository
	followRepo    domain.FollowRepository
	likeRepo      domain.LikeRepository
	commentRepo   domain.CommentRepository
	pollRepo      domain.PollRepository
	bookmarkRepo  domain.BookmarkRepository
	retweetRepo   domain.RetweetRepository
	timelineCache *cache.TimelineCache
	eventProducer EventProducer
	l1Cache       *cache.L1Cache
	requestGroup  singleflight.Group
}

// NewTweetService 创建推文服务
func NewTweetService(
	repo domain.TweetRepository,
	followRepo domain.FollowRepository,
	likeRepo domain.LikeRepository,
	commentRepo domain.CommentRepository,
	pollRepo domain.PollRepository,
	bookmarkRepo domain.BookmarkRepository,
	retweetRepo domain.RetweetRepository,
	timelineCache *cache.TimelineCache,
	eventProducer EventProducer,
	l1Cache *cache.L1Cache,
) *TweetService {
	return &TweetService{
		repo:          repo,
		followRepo:    followRepo,
		likeRepo:      likeRepo,
		commentRepo:   commentRepo,
		pollRepo:      pollRepo,
		bookmarkRepo:  bookmarkRepo,
		retweetRepo:   retweetRepo,
		timelineCache: timelineCache,
		eventProducer: eventProducer,
		l1Cache:       l1Cache,
	}
}

// CreateTweet 发布推文（使用消息队列）
func (s *TweetService) CreateTweet(ctx context.Context, userID uint64, content string, mediaURLs []string, parentID uint64, pollOptions []string, pollDuration int32) (*domain.Tweet, error) {
	// 🔍 启动 Span
	tr := otel.Tracer("tweet-service")
	ctx, span := tr.Start(ctx, "TweetService.CreateTweet")
	defer span.End()

	span.SetAttributes(attribute.Int64("user.id", int64(userID)))

	// 1. 参数验证
	if err := s.validateContent(content); err != nil {
		span.RecordError(err)
		return nil, err
	}

	if err := s.validateMediaURLs(mediaURLs); err != nil {
		span.RecordError(err)
		return nil, err
	}

	// 1.5 验证父推文是否存在 (如果是回复)
	if parentID > 0 {
		_, err := s.GetBaseTweetWithCache(ctx, parentID)
		if err != nil {
			return nil, ErrTweetNotFound
		}
	}

	// 2. 确定推文类型
	tweetType := s.determineTweetType(mediaURLs)

	// 3. 构建推文对象
	tweet := &domain.Tweet{
		UserID:      userID,
		ParentID:    parentID,
		Content:     strings.TrimSpace(content),
		MediaURLs:   mediaURLs,
		Type:        tweetType,
		VisibleType: domain.VisiblePublic,
	}

	// 4. 保存到数据库
	// 🔍 DB Span
	dbCtx, dbSpan := tr.Start(ctx, "TweetRepo.Create")
	if err := s.repo.Create(dbCtx, tweet); err != nil {
		dbSpan.RecordError(err)
		dbSpan.End()
		return nil, fmt.Errorf("failed to create tweet: %w", err)
	}
	dbSpan.End()

	// 4.5 保存投票 (如果有)
	if len(pollOptions) >= 2 {
		poll := &domain.Poll{
			TweetID:   tweet.ID,
			Question:  content, // 默认问题为推文内容 (或者可以独立)
			CreatedAt: tweet.CreatedAt,
			EndTime:   tweet.CreatedAt + int64(pollDuration)*60*1000, // 分钟转毫秒
		}
		for _, opt := range pollOptions {
			poll.Options = append(poll.Options, domain.PollOption{
				Text: opt,
			})
		}
		if err := s.pollRepo.Create(ctx, poll); err != nil {
			logger.Error(ctx, "failed to create poll", zap.Error(err))
			// 这种情况下是否回滚推文？
			// MVP: 记录错误，不回滚，前端可能显示不完整
		} else {
			// 关联 poll 到返回的 tweet 对象，以便前端立即显示
			tweet.Poll = poll
		}
	}

	logger.Info(ctx, "✅ Tweet created", zap.Uint64("tweet_id", tweet.ID), zap.Uint64("user_id", tweet.UserID), zap.Uint64("parent_id", parentID))

	// 5. 🆕 发送消息到 MQ（异步扇出）
	event := &events.TweetCreatedEvent{
		TweetID:  tweet.ID,
		AuthorID: tweet.UserID,
		Content:  tweet.Content,
		Type:     tweet.Type,
		// TODO: Add ParentID to event if needed for notification service
	}

	// 🔥 关键改进：不再使用 Goroutine，而是发送到消息队列
	// 🔍 MQ Span
	mqCtx, mqSpan := tr.Start(ctx, "MQProducer.Publish")
	if err := s.eventProducer.PublishTweetCreated(mqCtx, event); err != nil {
		// ⚠️ 即使发送失败，推文也已保存，记录错误即可
		mqSpan.RecordError(err)
		logger.Error(mqCtx, "⚠️  Failed to publish tweet created event", zap.Error(err))
	}
	mqSpan.End()

	return tweet, nil
}

// DeleteTweet 删除推文（使用消息队列）并失效多级缓存
func (s *TweetService) DeleteTweet(ctx context.Context, tweetID uint64, userID uint64) error {
	// 1. 查询推文
	tweet, err := s.repo.GetByID(ctx, tweetID)
	if err != nil {
		return ErrTweetNotFound
	}

	// 2. 权限检查
	if tweet.UserID != userID {
		return ErrUnauthorized
	}

	// 3. 执行数据库删除
	if err := s.repo.Delete(ctx, tweetID); err != nil {
		return fmt.Errorf("failed to delete tweet: %w", err)
	}

	// 4. 失效本地与全局 L1/L2 缓存
	s.InvalidateTweetCache(ctx, tweetID)

	// 5. 发送删除事件到 MQ
	event := &events.TweetDeletedEvent{
		TweetID:  tweetID,
		AuthorID: userID,
	}

	if err := s.eventProducer.PublishTweetDeleted(ctx, event); err != nil {
		logger.Error(ctx, "⚠️  Failed to publish tweet deleted event", zap.Error(err))
	}

	return nil
}

// GetTweet 获取推文详情 (带多级缓存与 singleflight 并发穿透防御)
func (s *TweetService) GetTweet(ctx context.Context, tweetID uint64, requestingUserID uint64) (*domain.Tweet, error) {
	// 1. 获取基础推文信息 (多级缓存 + singleflight)
	baseTweet, err := s.GetBaseTweetWithCache(ctx, tweetID)
	if err != nil {
		return nil, ErrTweetNotFound
	}

	// 2. 影子封禁过滤：如果是影子封禁的推文，且请求者不是作者本人，则返回推文不存在
	if baseTweet.VisibleType == domain.VisibleShadowban && baseTweet.UserID != requestingUserID {
		return nil, ErrTweetNotFound
	}

	// 3. 克隆一份以防止并发修改导致缓存数据污染
	tweet := &domain.Tweet{}
	*tweet = *baseTweet

	// 4. 获取点赞数
	likeCount, err := s.likeRepo.GetLikeCount(ctx, tweetID)
	if err == nil {
		tweet.LikeCount = int(likeCount)
	}

	// 5. 检查是否已点赞
	if requestingUserID > 0 {
		isLiked, err := s.likeRepo.IsLiked(ctx, requestingUserID, tweetID)
		if err == nil {
			tweet.IsLiked = isLiked
		}
	}

	// 6. 获取评论数
	commentCount, err := s.commentRepo.GetCommentCount(ctx, tweetID)
	if err == nil {
		tweet.CommentCount = int(commentCount)
	}

	// 7. 填充其他统计数据 (包括 Poll)
	s.populateTweetStats(ctx, []*domain.Tweet{tweet}, requestingUserID)

	return tweet, nil
}

// GetBaseTweetWithCache 带 L1/L2 缓存与 singleflight 并发穿透防御的基础推文获取
func (s *TweetService) GetBaseTweetWithCache(ctx context.Context, tweetID uint64) (*domain.Tweet, error) {
	key := fmt.Sprintf("tweet:base:%d", tweetID)

	// 1. 尝试 L1 本地内存缓存
	if s.l1Cache != nil {
		if data, err := s.l1Cache.Get(key); err == nil {
			var tweet domain.Tweet
			if json.Unmarshal(data, &tweet) == nil {
				return &tweet, nil
			}
		}
	}

	// 2. L1 击穿，使用 singleflight 拦截瞬间并发
	// DoChan 结合 select 做整体超时控制，防止协程永久阻塞导致假死
	ch := s.requestGroup.DoChan(key, func() (interface{}, error) {
		// 限制去底层（Redis L2 或 MySQL）查询的绝对超时时间
		fetchCtx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
		defer cancel()

		// 3. 尝试从 Redis L2 缓存获取
		tweet, err := s.timelineCache.GetBaseTweet(fetchCtx, tweetID)
		if err == nil && tweet != nil {
			// 回写 L1 缓存
			if s.l1Cache != nil {
				if data, err := json.Marshal(tweet); err == nil {
					_ = s.l1Cache.Set(key, data)
				}
			}
			return tweet, nil
		}

		// 4. Redis 也未命中，回源到 MySQL
		tweet, err = s.repo.GetByID(fetchCtx, tweetID)
		if err != nil {
			return nil, err
		}

		// 回写 Redis L2 缓存 (带防雪崩随机 TTL)
		_ = s.timelineCache.SetBaseTweet(fetchCtx, tweet)

		// 回写 L1 缓存
		if s.l1Cache != nil {
			if data, err := json.Marshal(tweet); err == nil {
				_ = s.l1Cache.Set(key, data)
			}
		}

		return tweet, nil
	})

	select {
	case res := <-ch:
		if res.Err != nil {
			return nil, res.Err
		}
		if val, ok := res.Val.(*domain.Tweet); ok {
			return val, nil
		}
		return nil, fmt.Errorf("unexpected type from singleflight: %T", res.Val)
	case <-ctx.Done(): // 上游客户端放弃，或者全局超时
		return nil, fmt.Errorf("singleflight timed out or cancelled: %w", ctx.Err())
	}
}

// GetBaseTweetsWithCache 批量获取基础推文信息 (多级缓存设计)
func (s *TweetService) GetBaseTweetsWithCache(ctx context.Context, tweetIDs []uint64) ([]*domain.Tweet, error) {
	if len(tweetIDs) == 0 {
		return []*domain.Tweet{}, nil
	}

	resultMap := make(map[uint64]*domain.Tweet, len(tweetIDs))
	var remainingIDs []uint64

	// 1. 尝试从 L1 缓存获取
	for _, id := range tweetIDs {
		key := fmt.Sprintf("tweet:base:%d", id)
		hit := false
		if s.l1Cache != nil {
			if data, err := s.l1Cache.Get(key); err == nil {
				var tweet domain.Tweet
				if json.Unmarshal(data, &tweet) == nil {
					resultMap[id] = &tweet
					hit = true
				}
			}
		}
		if !hit {
			remainingIDs = append(remainingIDs, id)
		}
	}

	// 2. 尝试从 L2 缓存 (Redis) 获取
	if len(remainingIDs) > 0 {
		l2Ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
		l2Map, err := s.timelineCache.MGetBaseTweets(l2Ctx, remainingIDs)
		cancel()
		if err == nil {
			for _, id := range remainingIDs {
				if tweet, ok := l2Map[id]; ok {
					resultMap[id] = tweet
					// 回写 L1
					if s.l1Cache != nil {
						if data, err := json.Marshal(tweet); err == nil {
							_ = s.l1Cache.Set(fmt.Sprintf("tweet:base:%d", id), data)
						}
					}
				}
			}
		}

		// 重新统计依然缺失的 IDs
		var stillMissingIDs []uint64
		for _, id := range remainingIDs {
			if _, ok := resultMap[id]; !ok {
				stillMissingIDs = append(stillMissingIDs, id)
			}
		}
		remainingIDs = stillMissingIDs
	}

	// 3. 回源到 MySQL
	if len(remainingIDs) > 0 {
		dbCtx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
		dbTweets, err := s.repo.GetByIDs(dbCtx, remainingIDs)
		cancel()
		if err != nil {
			return nil, fmt.Errorf("failed to batch get tweets from db: %w", err)
		}

		for _, tweet := range dbTweets {
			resultMap[tweet.ID] = tweet
			// 异步回写 L2 和 L1 缓存，不阻塞当前读请求
			go func(t *domain.Tweet) {
				writeCtx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
				defer cancel()
				_ = s.timelineCache.SetBaseTweet(writeCtx, t)
				if s.l1Cache != nil {
					if data, err := json.Marshal(t); err == nil {
						_ = s.l1Cache.Set(fmt.Sprintf("tweet:base:%d", t.ID), data)
					}
				}
			}(tweet)
		}
	}

	// 4. 组装结果，克隆防污染
	tweets := make([]*domain.Tweet, 0, len(tweetIDs))
	for _, id := range tweetIDs {
		if t, ok := resultMap[id]; ok {
			tc := *t
			tweets = append(tweets, &tc)
		}
	}

	return tweets, nil
}

// InvalidateTweetCache 全局失效并清除推文的 L1/L2 缓存
func (s *TweetService) InvalidateTweetCache(ctx context.Context, tweetID uint64) {
	// 调用 timelineCache 的广播删除接口
	_ = s.timelineCache.InvalidateBaseTweet(ctx, tweetID)
}

// GetUserTimeline 获取用户时间线（拉模式：合并原生与转发推文）
func (s *TweetService) GetUserTimeline(ctx context.Context, userID uint64, cursor uint64, limit int, requestingUserID uint64) ([]*domain.Tweet, uint64, bool, error) {
	// 🔍 启动 Span
	tr := otel.Tracer("tweet-service")
	ctx, span := tr.Start(ctx, "TweetService.GetUserTimeline")
	defer span.End()

	// 1. 参数验证
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	// 2. 分别获取用户发布的推文和转发记录（多获取 1 条用于 HasMore 判断）
	tweets, err := s.repo.ListByUserID(ctx, userID, cursor, limit+1)
	if err != nil {
		return nil, 0, false, fmt.Errorf("failed to get user timeline: %w", err)
	}

	retweets, err := s.retweetRepo.ListByUserID(ctx, userID, cursor, limit+1)
	if err != nil {
		return nil, 0, false, fmt.Errorf("failed to get user retweets: %w", err)
	}

	// 收集转发推文的原推文 IDs
	rtTweetIDs := make([]uint64, 0, len(retweets))
	for _, rt := range retweets {
		rtTweetIDs = append(rtTweetIDs, rt.TweetID)
	}

	// 批量查询原推文详情
	var originalTweets []*domain.Tweet
	if len(rtTweetIDs) > 0 {
		originalTweets, err = s.GetBaseTweetsWithCache(ctx, rtTweetIDs)
		if err != nil {
			return nil, 0, false, fmt.Errorf("failed to batch get retweet original tweets: %w", err)
		}
	}

	originalMap := make(map[uint64]*domain.Tweet)
	for _, t := range originalTweets {
		originalMap[t.ID] = t
	}

	// 3. 合并与深拷贝，并进行影子封禁过滤
	var allItems []*domain.Tweet

	for _, t := range tweets {
		// 影子封禁过滤：如果是影子封禁的推文，且请求者不是作者本人，则忽略
		if t.VisibleType == domain.VisibleShadowban && t.UserID != requestingUserID {
			continue
		}
		tc := *t
		tc.IsRetweetedDisplay = false
		tc.SortID = tc.ID
		allItems = append(allItems, &tc)
	}

	for _, rt := range retweets {
		if orig, ok := originalMap[rt.TweetID]; ok {
			// 影子封禁过滤：如果是影子封禁的推文，且请求者不是作者本人，则忽略该转发显示
			if orig.VisibleType == domain.VisibleShadowban && orig.UserID != requestingUserID {
				continue
			}
			tc := *orig
			tc.IsRetweetedDisplay = true
			tc.RetweetedAt = rt.CreatedAt
			tc.SortID = rt.ID
			allItems = append(allItems, &tc)
		}
	}

	// 4. 按 SortID 降序排序
	sort.Slice(allItems, func(i, j int) bool {
		return allItems[i].SortID > allItems[j].SortID
	})

	// 5. 截取并判断是否还有更多
	hasMore := len(allItems) > limit
	if hasMore {
		allItems = allItems[:limit]
	}

	// 6. 计算下一页游标 (用最后一个元素的 SortID)
	var nextCursor uint64
	if len(allItems) > 0 {
		nextCursor = allItems[len(allItems)-1].SortID
	}

	// 7. 填充统计数据 (点赞数、是否点赞、转发数、是否转发、是否收藏)
	s.populateTweetStats(ctx, allItems, requestingUserID)

	return allItems, nextCursor, hasMore, nil
}

// GetFeeds 获取关注流 (带推拉结合纯缓存优化与并发合并)
func (s *TweetService) GetFeeds(ctx context.Context, userID uint64, cursor uint64, limit int) ([]*domain.Tweet, uint64, bool, error) {
	// 参数验证
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	// 1. 获取当前用户关注的大V ID 集合
	celebrityIDs, err := s.timelineCache.GetCelebrityFollowees(ctx, userID)
	if err != nil {
		logger.Warn(ctx, "⚠️  Failed to get celebrity followees from redis", zap.Error(err))
		celebrityIDs = nil
	}

	var wg sync.WaitGroup
	var normalIDs []uint64
	var celebIDs []uint64
	var errNormal error
	var errCeleb error

	// 2. 并发拉取大V和普通用户的时间线
	wg.Add(2)
	go func() {
		defer wg.Done()
		// 普通时间线：直接走写扩散的 Redis ZSet
		normalIDs, errNormal = s.timelineCache.GetTimeline(ctx, userID, cursor, limit)
	}()

	go func() {
		defer wg.Done()
		// 大V时间线：走 Pipeline 并发拉取各大V的专属 ZSet (含自动重建机制)
		celebIDs, errCeleb = s.getCelebrityTimelineWithRebuild(ctx, celebrityIDs, cursor, limit)
	}()
	wg.Wait()

	// 降级判断：如果普通时间线读取失败，降级走拉模式
	if errNormal != nil {
		logger.Warn(ctx, "⚠️  Failed to get normal timeline from redis, falling back to pull mode", zap.Error(errNormal))
		return s.getFeedsByPull(ctx, userID, cursor, limit, userID)
	}
	if errCeleb != nil {
		logger.Warn(ctx, "⚠️  Failed to fetch celebrity timelines, proceeding without them", zap.Error(errCeleb))
	}

	// 3. 内存聚合与全局排序
	// 合并推文 ID
	mergedIDs := make([]uint64, 0, len(normalIDs)+len(celebIDs))
	uniqueMap := make(map[uint64]bool)

	for _, id := range normalIDs {
		if !uniqueMap[id] {
			uniqueMap[id] = true
			mergedIDs = append(mergedIDs, id)
		}
	}
	for _, id := range celebIDs {
		if !uniqueMap[id] {
			uniqueMap[id] = true
			mergedIDs = append(mergedIDs, id)
		}
	}

	// 按 ID 降序排列 (时间降序)
	sort.Slice(mergedIDs, func(i, j int) bool {
		return mergedIDs[i] > mergedIDs[j]
	})

	// 4. 判断是否还有更多，并裁切
	hasMore := len(mergedIDs) > limit
	if hasMore {
		mergedIDs = mergedIDs[:limit]
	}

	// 5. 批量拉取推文详情 (完全利用多级缓存，避免穿透 DB)
	tweets, err := s.GetBaseTweetsWithCache(ctx, mergedIDs)
	if err != nil {
		return nil, 0, false, fmt.Errorf("failed to get feeds tweets: %w", err)
	}

	// 6. 按照已排序的 mergedIDs 重新对 tweets 排序并影子过滤
	normalMap := make(map[uint64]*domain.Tweet, len(tweets))
	for _, tweet := range tweets {
		// 影子封禁过滤
		if tweet.VisibleType == domain.VisibleShadowban && tweet.UserID != userID {
			continue
		}
		normalMap[tweet.ID] = tweet
	}

	finalTweets := make([]*domain.Tweet, 0, len(mergedIDs))
	for _, tweetID := range mergedIDs {
		if tweet, ok := normalMap[tweetID]; ok {
			finalTweets = append(finalTweets, tweet)
		}
	}

	// 7. 计算下一页游标
	var nextCursor uint64
	if len(finalTweets) > 0 {
		nextCursor = finalTweets[len(finalTweets)-1].ID
	}

	// 8. 填充统计数据 (点赞、收藏等)
	s.populateTweetStats(ctx, finalTweets, userID)

	// 9. 【分页预热】异步启动预拉取下一页
	if hasMore && nextCursor > 0 {
		prewarmCtx := context.Background() // 使用独立的脱离主线程生命周期的 Context
		go s.prewarmNextPage(prewarmCtx, userID, nextCursor, limit, celebrityIDs)
	}

	logger.Info(ctx, "✅ Feeds loaded by cache hybrid mode", zap.Uint64("user_id", userID), zap.Int("count", len(finalTweets)))
	return finalTweets, nextCursor, hasMore, nil
}

// getCelebrityTimelineWithRebuild 获取大V的时间线推文 ID，包含防击穿的 ZSet 自动重建
func (s *TweetService) getCelebrityTimelineWithRebuild(ctx context.Context, celebrityIDs []uint64, cursor uint64, limit int) ([]uint64, error) {
	if len(celebrityIDs) == 0 {
		return []uint64{}, nil
	}

	results, missingIDs, err := s.timelineCache.PipelineGetCelebrityTweets(ctx, celebrityIDs, cursor, limit)
	if err != nil {
		logger.Warn(ctx, "⚠️  Failed to pipeline get celebrity tweets", zap.Error(err))
		// 降级，全部视作需要重建或走 DB
		missingIDs = celebrityIDs
		results = make(map[uint64][]uint64)
	}

	// 如果有大V缺失缓存，使用 singleflight 拦截，防止高并发打穿 DB
	if len(missingIDs) > 0 {
		var wg sync.WaitGroup
		var mu sync.Mutex
		for _, id := range missingIDs {
			wg.Add(1)
			go func(celebID uint64) {
				defer wg.Done()
				key := fmt.Sprintf("rebuild:celeb:%d", celebID)

				// DoChan 结合 select 加上超时防御，防止协程饥饿
				ch := s.requestGroup.DoChan(key, func() (interface{}, error) {
					rebuildCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
					defer cancel()

					// 查 DB 获取该大V最新的 1000 条推文 (符合拍板建议)
					tweets, err := s.repo.ListByUserID(rebuildCtx, celebID, 0, 1000)
					if err != nil {
						return nil, fmt.Errorf("failed to fetch celebrity tweets from DB: %w", err)
					}

					tweetIDs := make([]uint64, len(tweets))
					for i, t := range tweets {
						tweetIDs[i] = t.ID
					}

					// 重建 ZSet 缓存
					err = s.timelineCache.RebuildUserTimeline(rebuildCtx, celebID, tweetIDs)
					if err != nil {
						logger.Warn(rebuildCtx, "⚠️  Failed to rebuild celebrity timeline cache", zap.Uint64("celeb_id", celebID), zap.Error(err))
					}
					return tweetIDs, nil
				})

				select {
				case res := <-ch:
					if res.Err == nil && res.Val != nil {
						if ids, ok := res.Val.([]uint64); ok {
							// 根据当前游标过滤出符合分页条件的部分
							var filteredIDs []uint64
							for _, tweetID := range ids {
								// cursor > 0 时只拉取比 cursor 小的
								if cursor > 0 && tweetID >= cursor {
									continue
								}
								filteredIDs = append(filteredIDs, tweetID)
								if len(filteredIDs) >= limit {
									break
								}
							}
							mu.Lock()
							results[celebID] = filteredIDs
							mu.Unlock()
						}
					}
				case <-ctx.Done():
					// 上游取消或超时
					return
				}
			}(id)
		}
		wg.Wait()
	}

	// 将所有大V的推文 ID 聚合并扁平化
	var allCelebTweetIDs []uint64
	for _, ids := range results {
		allCelebTweetIDs = append(allCelebTweetIDs, ids...)
	}

	return allCelebTweetIDs, nil
}

// prewarmNextPage 异步预热下一页推文详情 (L1/L2 缓存热身)
func (s *TweetService) prewarmNextPage(ctx context.Context, userID uint64, nextCursor uint64, limit int, celebrityIDs []uint64) {
	// 加上最大 2 秒的超时限制，防止预热逻辑挂死
	prewarmCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	var normalIDs []uint64
	var celebIDs []uint64

	wg.Add(2)
	go func() {
		defer wg.Done()
		// 获取下一页写扩散推文 ID
		normalIDs, _ = s.timelineCache.GetTimeline(prewarmCtx, userID, nextCursor, limit)
	}()

	go func() {
		defer wg.Done()
		// 获取下一页大V推文 ID (不带 rebuild，仅取当前 L2，预热无需强一致)
		if len(celebrityIDs) > 0 {
			results, _, _ := s.timelineCache.PipelineGetCelebrityTweets(prewarmCtx, celebrityIDs, nextCursor, limit)
			for _, ids := range results {
				celebIDs = append(celebIDs, ids...)
			}
		}
	}()
	wg.Wait()

	// 合并去重并截取
	mergedIDs := make([]uint64, 0, len(normalIDs)+len(celebIDs))
	uniqueMap := make(map[uint64]bool)
	for _, id := range normalIDs {
		if !uniqueMap[id] {
			uniqueMap[id] = true
			mergedIDs = append(mergedIDs, id)
		}
	}
	for _, id := range celebIDs {
		if !uniqueMap[id] {
			uniqueMap[id] = true
			mergedIDs = append(mergedIDs, id)
		}
	}

	if len(mergedIDs) > limit {
		mergedIDs = mergedIDs[:limit]
	}

	if len(mergedIDs) == 0 {
		return
	}

	logger.Info(prewarmCtx, "🔥 Pre-warming next page feeds", zap.Uint64("user_id", userID), zap.Int("count", len(mergedIDs)))
	_, _ = s.GetBaseTweetsWithCache(prewarmCtx, mergedIDs)
}

// getFeedsByPull 拉模式获取 Feeds（降级方案）
func (s *TweetService) getFeedsByPull(ctx context.Context, userID uint64, cursor uint64, limit int, requestingUserID uint64) ([]*domain.Tweet, uint64, bool, error) {
	// 1. 获取关注列表
	followeeIDs, err := s.followRepo.GetFollowees(ctx, userID, 0, 1000)
	if err != nil {
		return nil, 0, false, fmt.Errorf("failed to get followees: %w", err)
	}

	if len(followeeIDs) == 0 {
		return []*domain.Tweet{}, 0, false, nil
	}

	// 2. 查询这些人的推文
	tweets, err := s.repo.GetFeeds(ctx, followeeIDs, cursor, limit+1)
	if err != nil {
		return nil, 0, false, fmt.Errorf("failed to get feeds: %w", err)
	}

	// 3. 判断是否还有更多
	hasMore := len(tweets) > limit
	if hasMore {
		tweets = tweets[:limit]
	}

	// 4. 计算下一页游标
	var nextCursor uint64
	if hasMore && len(tweets) > 0 {
		nextCursor = tweets[len(tweets)-1].ID
	}

	// 5. 填充统计数据
	s.populateTweetStats(ctx, tweets, requestingUserID)

	logger.Warn(ctx, "⚠️  Feeds loaded by pull mode", zap.Uint64("user_id", userID), zap.Int("count", len(tweets)))

	return tweets, nextCursor, hasMore, nil
}

// ========== 私有辅助方法 ==========

func (s *TweetService) validateContent(content string) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return ErrInvalidContent
	}

	// 使用 rune 计数（支持中文等 Unicode 字符）
	if len([]rune(content)) > MaxContentLength {
		return ErrContentTooLong
	}

	return nil
}

func (s *TweetService) validateMediaURLs(mediaURLs []string) error {
	if len(mediaURLs) > MaxMediaCount {
		return ErrTooManyMedia
	}

	for _, mediaURL := range mediaURLs {
		if mediaURL == "" {
			continue
		}

		parsedURL, err := url.Parse(mediaURL)
		if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
			return ErrInvalidMediaURL
		}

		if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
			return ErrInvalidMediaURL
		}
	}

	return nil
}

func (s *TweetService) determineTweetType(mediaURLs []string) int {
	if len(mediaURLs) == 0 {
		return domain.TweetTypeText
	}

	for _, mediaURL := range mediaURLs {
		lower := strings.ToLower(mediaURL)
		if strings.HasSuffix(lower, ".mp4") ||
			strings.HasSuffix(lower, ".mov") ||
			strings.HasSuffix(lower, ".avi") {
			return domain.TweetTypeVideo
		}
	}

	return domain.TweetTypeImage
}

// populateTweetStats 批量填充推文统计数据 (点赞数、是否点赞、转发数、是否转发、是否收藏)
func (s *TweetService) populateTweetStats(ctx context.Context, tweets []*domain.Tweet, requestingUserID uint64) {
	if len(tweets) == 0 {
		return
	}

	tweetIDs := make([]uint64, len(tweets))
	for i, tweet := range tweets {
		tweetIDs[i] = tweet.ID
	}

	// 1. 批量获取点赞数
	likeCounts, err := s.likeRepo.BatchGetLikeCounts(ctx, tweetIDs)
	if err != nil {
		logger.Warn(ctx, "failed to batch get like counts", zap.Error(err))
	} else {
		for _, tweet := range tweets {
			if count, ok := likeCounts[tweet.ID]; ok {
				tweet.LikeCount = int(count)
			}
		}
	}

	// 2. 批量获取转发数
	retweetCounts, err := s.retweetRepo.BatchGetRetweetCounts(ctx, tweetIDs)
	if err != nil {
		logger.Warn(ctx, "failed to batch get retweet counts", zap.Error(err))
	} else {
		for _, tweet := range tweets {
			if count, ok := retweetCounts[tweet.ID]; ok {
				tweet.ShareCount = int(count)
			}
		}
	}

	// 3. 批量检查是否点赞、转发、收藏 (仅当 requestingUserID > 0)
	if requestingUserID > 0 {
		// 点赞状态
		likedMap, err := s.likeRepo.BatchIsLiked(ctx, requestingUserID, tweetIDs)
		if err != nil {
			logger.Warn(ctx, "failed to batch check like status", zap.Error(err))
		} else {
			for _, tweet := range tweets {
				if isLiked, ok := likedMap[tweet.ID]; ok {
					tweet.IsLiked = isLiked
				}
			}
		}

		// 转发状态
		retweetedMap, err := s.retweetRepo.BatchIsRetweeted(ctx, requestingUserID, tweetIDs)
		if err != nil {
			logger.Warn(ctx, "failed to batch check retweet status", zap.Error(err))
		} else {
			for _, tweet := range tweets {
				if isRetweeted, ok := retweetedMap[tweet.ID]; ok {
					tweet.IsRetweeted = isRetweeted
				}
			}
		}

		// 收藏（书签）状态
		bookmarkedMap, err := s.bookmarkRepo.BatchIsBookmarked(ctx, requestingUserID, tweetIDs)
		if err != nil {
			logger.Warn(ctx, "failed to batch check bookmark status", zap.Error(err))
		} else {
			for _, tweet := range tweets {
				if isBookmarked, ok := bookmarkedMap[tweet.ID]; ok {
					tweet.IsBookmarked = isBookmarked
				}
			}
		}
	}

	// 4. 批量获取评论数
	commentCounts, err := s.commentRepo.BatchGetCommentCounts(ctx, tweetIDs)
	if err != nil {
		logger.Warn(ctx, "failed to batch get comment counts", zap.Error(err))
	} else {
		for _, tweet := range tweets {
			if count, ok := commentCounts[tweet.ID]; ok {
				tweet.CommentCount = int(count)
			}
		}
	}

	// 5. 批量获取投票信息
	pollMap, err := s.pollRepo.GetByTweetIDs(ctx, tweetIDs)
	if err != nil {
		logger.Warn(ctx, "failed to batch get polls", zap.Error(err))
	} else if len(pollMap) > 0 {
		// 批量检查用户投票状态
		var votedMap map[uint64]uint64
		if requestingUserID > 0 {
			votedMap, _ = s.pollRepo.GetVotesByTweetIDs(ctx, tweetIDs, requestingUserID)
		}

		for _, tweet := range tweets {
			if poll, ok := pollMap[tweet.ID]; ok {
				// 计算总票数和过期状态
				now := time.Now().UnixMilli()
				poll.IsExpired = now > poll.EndTime

				var totalVotes int
				for i := range poll.Options {
					totalVotes += poll.Options[i].VoteCount
				}

				// 计算百分比
				if totalVotes > 0 {
					for i := range poll.Options {
						poll.Options[i].Percentage = float32(poll.Options[i].VoteCount) / float32(totalVotes) * 100
					}
				}
				poll.TotalVotes = totalVotes
				logger.Info(ctx, "Populated poll stats",
					zap.Uint64("poll_id", poll.ID),
					zap.Int("total_votes", totalVotes),
					zap.Int("option_count", len(poll.Options)))

				// 用户投票状态
				if votedOptionID, ok := votedMap[tweet.ID]; ok {
					poll.IsVoted = true
					poll.VotedOptionID = votedOptionID
				}

				tweet.Poll = poll
			}
		}
	}
}

// VotePoll 投票
func (s *TweetService) VotePoll(ctx context.Context, userID, pollID, optionID uint64) (*domain.Poll, error) {
	// 1. 尝试投票
	vote := &domain.PollVote{
		PollID:    pollID,
		OptionID:  optionID,
		UserID:    userID,
		CreatedAt: time.Now().UnixMilli(),
	}

	var finalVotedOptionID uint64

	err := s.pollRepo.Vote(ctx, vote)
	if err != nil {
		// 2. 如果失败，检查是否是因为已经投过票
		existingVote, checkErr := s.pollRepo.GetVote(ctx, pollID, userID)
		if checkErr == nil && existingVote != nil {
			// 用户已经投过票，视为成功（幂等），返回最新数据
			logger.Info(ctx, "user already voted", zap.Uint64("user_id", userID), zap.Uint64("poll_id", pollID))
			finalVotedOptionID = existingVote.OptionID
		} else {
			// 其他错误
			return nil, fmt.Errorf("failed to vote: %w", err)
		}
	} else {
		finalVotedOptionID = optionID
	}

	// 3. 返回最新的 Poll 数据
	poll, err := s.pollRepo.GetByID(ctx, pollID)
	if err != nil {
		return nil, fmt.Errorf("failed to get updated poll: %w", err)
	}

	// 4. 填充用户的投票状态 (GetByID 不会填充 IsVoted，因为它不知道是哪个用户)
	poll.IsVoted = true
	poll.VotedOptionID = finalVotedOptionID

	// 5. 计算百分比
	var totalVotes int
	for i := range poll.Options {
		totalVotes += poll.Options[i].VoteCount
	}
	if totalVotes > 0 {
		for i := range poll.Options {
			poll.Options[i].Percentage = float32(poll.Options[i].VoteCount) / float32(totalVotes) * 100
		}
	}
	poll.TotalVotes = totalVotes
	poll.IsExpired = time.Now().UnixMilli() > poll.EndTime

	return poll, nil
}

// LikeTweet 点赞推文
func (s *TweetService) LikeTweet(ctx context.Context, userID, tweetID uint64) (int64, error) {
	// 1. 检查推文是否存在
	tweet, err := s.GetBaseTweetWithCache(ctx, tweetID)
	if err != nil {
		return 0, ErrTweetNotFound
	}

	// 2. 数据库点赞 (幂等)
	if err := s.likeRepo.Like(ctx, userID, tweetID); err != nil {
		return 0, fmt.Errorf("failed to like tweet: %w", err)
	}

	// 3. (可选)Redis 计数 +1
	// 这里简化处理，直接查库获取最新数量，或者依赖 Redis 计数器
	// 也可以发送 MQ 消息去更新计数

	// 4. 获取最新点赞数
	count, err := s.likeRepo.GetLikeCount(ctx, tweetID)
	if err != nil {
		logger.Warn(ctx, "failed to get like count", zap.Error(err))
		return 0, nil
	}

	// 5. 发送点赞事件 (用于通知系统)
	event := &events.TweetLikedEvent{
		TweetID:   tweetID,
		UserID:    userID,
		TweetUser: tweet.UserID,
	}
	if err := s.eventProducer.PublishTweetLiked(ctx, event); err != nil {
		logger.Warn(ctx, "failed to publish tweet liked event", zap.Error(err))
	}

	return count, nil
}

// UnlikeTweet 取消点赞
func (s *TweetService) UnlikeTweet(ctx context.Context, userID, tweetID uint64) (int64, error) {
	// 1. 数据库取消点赞
	if err := s.likeRepo.Unlike(ctx, userID, tweetID); err != nil {
		return 0, fmt.Errorf("failed to unlike tweet: %w", err)
	}

	// 2. 获取最新点赞数
	count, err := s.likeRepo.GetLikeCount(ctx, tweetID)
	if err != nil {
		return 0, nil
	}

	return count, nil
}

// ==================== 评论相关 ====================

// CreateComment 发布评论
func (s *TweetService) CreateComment(ctx context.Context, userID, tweetID uint64, content string, parentID uint64) (*domain.Comment, error) {
	// 1. 验证推文是否存在
	tweet, err := s.GetBaseTweetWithCache(ctx, tweetID)
	if err != nil {
		return nil, ErrTweetNotFound
	}

	comment := &domain.Comment{
		UserID:   userID,
		TweetID:  tweetID,
		Content:  content,
		ParentID: parentID,
	}

	// 2. 创建评论
	if err := s.commentRepo.Create(ctx, comment); err != nil {
		return nil, err
	}

	// 3. 发送事件
	event := &events.CommentCreatedEvent{
		CommentID: comment.ID,
		TweetID:   tweetID,
		UserID:    userID,
		Content:   content,
		TweetUser: tweet.UserID, // 推文作者 ID
		ParentID:  parentID,
	}
	if err := s.eventProducer.PublishCommentCreated(ctx, event); err != nil {
		logger.Warn(ctx, "failed to publish comment created event", zap.Error(err))
	}

	return comment, nil
}

// DeleteComment 删除评论
func (s *TweetService) DeleteComment(ctx context.Context, commentID, userID uint64) error {
	// 1. 获取评论
	comment, err := s.commentRepo.GetByID(ctx, commentID)
	if err != nil {
		return err
	}

	// 2. 权限检查
	if comment.UserID != userID {
		return ErrUnauthorized
	}

	// 3. 删除
	return s.commentRepo.Delete(ctx, commentID)
}

// GetTweetComments 获取推文评论列表
func (s *TweetService) GetTweetComments(ctx context.Context, tweetID uint64, cursor uint64, limit int) ([]*domain.Comment, uint64, bool, error) {
	// 1. 参数验证
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	// 2. 获取列表
	comments, err := s.commentRepo.ListByTweetID(ctx, tweetID, cursor, limit+1)
	if err != nil {
		return nil, 0, false, err
	}

	// 3. 判断更多
	hasMore := len(comments) > limit
	if hasMore {
		comments = comments[:limit]
	}

	// 4. 计算游标
	var nextCursor uint64
	if hasMore && len(comments) > 0 {
		nextCursor = comments[len(comments)-1].ID
	}

	return comments, nextCursor, hasMore, nil
}

// SearchTweets 搜索推文
func (s *TweetService) SearchTweets(ctx context.Context, query string, cursor uint64, limit int, requestingUserID uint64) ([]*domain.Tweet, uint64, bool, error) {
	// 1. 参数验证
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return []*domain.Tweet{}, 0, false, nil
	}

	// 2. 搜索
	tweets, err := s.repo.Search(ctx, query, cursor, limit+1)
	if err != nil {
		return nil, 0, false, fmt.Errorf("failed to search tweets: %w", err)
	}

	// 3. 判断更多
	hasMore := len(tweets) > limit
	if hasMore {
		tweets = tweets[:limit]
	}

	// 4. 计算游标
	var nextCursor uint64
	if hasMore && len(tweets) > 0 {
		nextCursor = tweets[len(tweets)-1].ID
	}

	// 5. 填充统计数据 (点赞/评论等)
	s.populateTweetStats(ctx, tweets, requestingUserID)

	return tweets, nextCursor, hasMore, nil
}

// GetTrendingTopics 获取热门话题
func (s *TweetService) GetTrendingTopics(ctx context.Context, limit int) ([]*domain.TrendingTopic, error) {
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}

	return s.timelineCache.GetTrendingTopics(ctx, limit)
}

// ListTweets 获取全站最新推文
func (s *TweetService) ListTweets(ctx context.Context, cursor uint64, limit int, requestingUserID uint64) ([]*domain.Tweet, uint64, bool, error) {
	// 1. 参数验证
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	// 2. 从数据库拉取
	tweets, err := s.repo.ListAll(ctx, cursor, limit+1)
	if err != nil {
		return nil, 0, false, fmt.Errorf("failed to list all tweets: %w", err)
	}

	// 3. 判断是否还有更多
	hasMore := len(tweets) > limit
	if hasMore {
		tweets = tweets[:limit]
	}

	// 4. 计算下一页游标
	var nextCursor uint64
	if hasMore && len(tweets) > 0 {
		nextCursor = tweets[len(tweets)-1].ID
	}

	// 5. 填充统计数据
	s.populateTweetStats(ctx, tweets, requestingUserID)

	return tweets, nextCursor, hasMore, nil
}

// GetTweetReplies 获取推文回复
func (s *TweetService) GetTweetReplies(ctx context.Context, tweetID uint64, cursor uint64, limit int, requestingUserID uint64) ([]*domain.Tweet, uint64, bool, error) {
	// 1. 获取回复
	tweets, nextCursor, err := s.repo.GetReplies(ctx, tweetID, cursor, limit)
	if err != nil {
		return nil, 0, false, err
	}

	// 2. 丰富数据
	s.populateTweetStats(ctx, tweets, requestingUserID)

	return tweets, nextCursor, nextCursor > 0, nil
}

// BookmarkTweet 添加书签
func (s *TweetService) BookmarkTweet(ctx context.Context, userID, tweetID uint64) error {
	tr := otel.Tracer("tweet-service")
	ctx, span := tr.Start(ctx, "TweetService.BookmarkTweet")
	defer span.End()

	_, err := s.GetBaseTweetWithCache(ctx, tweetID)
	if err != nil {
		return fmt.Errorf("tweet not found: %w", err)
	}

	bookmark := &domain.Bookmark{
		UserID:  userID,
		TweetID: tweetID,
	}
	return s.bookmarkRepo.Create(ctx, bookmark)
}

// UnbookmarkTweet 取消书签
func (s *TweetService) UnbookmarkTweet(ctx context.Context, userID, tweetID uint64) error {
	tr := otel.Tracer("tweet-service")
	ctx, span := tr.Start(ctx, "TweetService.UnbookmarkTweet")
	defer span.End()

	return s.bookmarkRepo.Delete(ctx, userID, tweetID)
}

// GetUserBookmarks 获取用户收藏列表 (分页)
func (s *TweetService) GetUserBookmarks(ctx context.Context, userID uint64, cursor uint64, limit int) ([]*domain.Tweet, uint64, bool, error) {
	tr := otel.Tracer("tweet-service")
	ctx, span := tr.Start(ctx, "TweetService.GetUserBookmarks")
	defer span.End()

	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	bookmarks, err := s.bookmarkRepo.List(ctx, userID, cursor, limit+1)
	if err != nil {
		return nil, 0, false, fmt.Errorf("failed to list bookmarks: %w", err)
	}

	hasMore := len(bookmarks) > limit
	if hasMore {
		bookmarks = bookmarks[:limit]
	}

	var tweetIDs []uint64
	for _, b := range bookmarks {
		tweetIDs = append(tweetIDs, b.TweetID)
	}

	var tweets []*domain.Tweet
	if len(tweetIDs) > 0 {
		tweets, err = s.GetBaseTweetsWithCache(ctx, tweetIDs)
		if err != nil {
			return nil, 0, false, fmt.Errorf("failed to get bookmarked tweets: %w", err)
		}
	}

	tweetMap := make(map[uint64]*domain.Tweet)
	for _, t := range tweets {
		tweetMap[t.ID] = t
	}

	var sortedTweets []*domain.Tweet
	for _, b := range bookmarks {
		if t, ok := tweetMap[b.TweetID]; ok {
			tc := *t
			tc.IsBookmarked = true
			sortedTweets = append(sortedTweets, &tc)
		}
	}

	s.populateTweetStats(ctx, sortedTweets, userID)

	var nextCursor uint64
	if len(bookmarks) > 0 {
		nextCursor = bookmarks[len(bookmarks)-1].ID
	}

	return sortedTweets, nextCursor, hasMore, nil
}

// RetweetTweet 转发推文
func (s *TweetService) RetweetTweet(ctx context.Context, userID, tweetID uint64) (int64, error) {
	tr := otel.Tracer("tweet-service")
	ctx, span := tr.Start(ctx, "TweetService.RetweetTweet")
	defer span.End()

	_, err := s.GetBaseTweetWithCache(ctx, tweetID)
	if err != nil {
		return 0, fmt.Errorf("tweet not found: %w", err)
	}

	if err := s.retweetRepo.Create(ctx, userID, tweetID); err != nil {
		return 0, err
	}

	count, err := s.retweetRepo.GetRetweetCount(ctx, tweetID)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// UnretweetTweet 取消转发
func (s *TweetService) UnretweetTweet(ctx context.Context, userID, tweetID uint64) (int64, error) {
	tr := otel.Tracer("tweet-service")
	ctx, span := tr.Start(ctx, "TweetService.UnretweetTweet")
	defer span.End()

	if err := s.retweetRepo.Delete(ctx, userID, tweetID); err != nil {
		return 0, err
	}

	count, err := s.retweetRepo.GetRetweetCount(ctx, tweetID)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// GetUserLikes 获取用户点赞的推文列表
func (s *TweetService) GetUserLikes(ctx context.Context, userID uint64, cursor uint64, limit int, requestingUserID uint64) ([]*domain.Tweet, uint64, bool, error) {
	tr := otel.Tracer("tweet-service")
	ctx, span := tr.Start(ctx, "TweetService.GetUserLikes")
	defer span.End()

	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	likes, err := s.likeRepo.ListByUserID(ctx, userID, cursor, limit+1)
	if err != nil {
		return nil, 0, false, fmt.Errorf("failed to list user likes: %w", err)
	}

	hasMore := len(likes) > limit
	if hasMore {
		likes = likes[:limit]
	}

	var tweetIDs []uint64
	for _, l := range likes {
		tweetIDs = append(tweetIDs, l.TweetID)
	}

	var tweets []*domain.Tweet
	if len(tweetIDs) > 0 {
		tweets, err = s.GetBaseTweetsWithCache(ctx, tweetIDs)
		if err != nil {
			return nil, 0, false, fmt.Errorf("failed to get liked tweets: %w", err)
		}
	}

	tweetMap := make(map[uint64]*domain.Tweet)
	for _, t := range tweets {
		tweetMap[t.ID] = t
	}

	var sortedTweets []*domain.Tweet
	for _, l := range likes {
		if t, ok := tweetMap[l.TweetID]; ok {
			tc := *t
			sortedTweets = append(sortedTweets, &tc)
		}
	}

	s.populateTweetStats(ctx, sortedTweets, requestingUserID)

	var nextCursor uint64
	if len(likes) > 0 {
		nextCursor = likes[len(likes)-1].ID
	}

	return sortedTweets, nextCursor, hasMore, nil
}

// GetUserReplies 获取用户回复的推文列表
func (s *TweetService) GetUserReplies(ctx context.Context, userID uint64, cursor uint64, limit int, requestingUserID uint64) ([]*domain.Tweet, uint64, bool, error) {
	tr := otel.Tracer("tweet-service")
	ctx, span := tr.Start(ctx, "TweetService.GetUserReplies")
	defer span.End()

	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	tweets, err := s.repo.ListRepliesByUserID(ctx, userID, cursor, limit+1)
	if err != nil {
		return nil, 0, false, fmt.Errorf("failed to list user replies: %w", err)
	}

	hasMore := len(tweets) > limit
	if hasMore {
		tweets = tweets[:limit]
	}

	s.populateTweetStats(ctx, tweets, requestingUserID)

	var nextCursor uint64
	if len(tweets) > 0 {
		nextCursor = tweets[len(tweets)-1].ID
	}

	return tweets, nextCursor, hasMore, nil
}

// GetUserMedia 获取用户媒体推文列表
func (s *TweetService) GetUserMedia(ctx context.Context, userID uint64, cursor uint64, limit int, requestingUserID uint64) ([]*domain.Tweet, uint64, bool, error) {
	tr := otel.Tracer("tweet-service")
	ctx, span := tr.Start(ctx, "TweetService.GetUserMedia")
	defer span.End()

	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	tweets, err := s.repo.ListMediaByUserID(ctx, userID, cursor, limit+1)
	if err != nil {
		return nil, 0, false, fmt.Errorf("failed to list user media: %w", err)
	}

	hasMore := len(tweets) > limit
	if hasMore {
		tweets = tweets[:limit]
	}

	s.populateTweetStats(ctx, tweets, requestingUserID)

	var nextCursor uint64
	if len(tweets) > 0 {
		nextCursor = tweets[len(tweets)-1].ID
	}

	return tweets, nextCursor, hasMore, nil
}
