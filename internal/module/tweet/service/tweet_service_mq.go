package service

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"twitter-clone/internal/domain"
	"twitter-clone/internal/events"
	"twitter-clone/internal/module/tweet/cache"
	"twitter-clone/pkg/logger"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"
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
	timelineCache *cache.TimelineCache
	eventProducer EventProducer
}

// NewTweetService 创建推文服务
func NewTweetService(
	repo domain.TweetRepository,
	followRepo domain.FollowRepository,
	likeRepo domain.LikeRepository,
	commentRepo domain.CommentRepository,
	pollRepo domain.PollRepository,
	timelineCache *cache.TimelineCache,
	eventProducer EventProducer,
) *TweetService {
	return &TweetService{
		repo:          repo,
		followRepo:    followRepo,
		likeRepo:      likeRepo,
		commentRepo:   commentRepo,
		pollRepo:      pollRepo,
		timelineCache: timelineCache,
		eventProducer: eventProducer,
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
		_, err := s.repo.GetByID(ctx, parentID)
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

// DeleteTweet 删除推文（使用消息队列）
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

	// 3. 执行删除
	if err := s.repo.Delete(ctx, tweetID); err != nil {
		return fmt.Errorf("failed to delete tweet: %w", err)
	}

	// 4. 🆕 发送删除事件到 MQ
	event := &events.TweetDeletedEvent{
		TweetID:  tweetID,
		AuthorID: userID,
	}

	if err := s.eventProducer.PublishTweetDeleted(ctx, event); err != nil {
		logger.Error(ctx, "⚠️  Failed to publish tweet deleted event", zap.Error(err))
	}

	return nil
}

// GetTweet 获取推文详情
func (s *TweetService) GetTweet(ctx context.Context, tweetID uint64, requestingUserID uint64) (*domain.Tweet, error) {
	tweet, err := s.repo.GetByID(ctx, tweetID)
	if err != nil {
		return nil, ErrTweetNotFound
	}

	// 1. 获取点赞数
	likeCount, err := s.likeRepo.GetLikeCount(ctx, tweetID)
	if err == nil {
		tweet.LikeCount = int(likeCount)
	}

	// 2. 检查是否已点赞
	if requestingUserID > 0 {
		isLiked, err := s.likeRepo.IsLiked(ctx, requestingUserID, tweetID)
		if err == nil {
			tweet.IsLiked = isLiked
		}
	}

	// 3. 获取评论数
	commentCount, err := s.commentRepo.GetCommentCount(ctx, tweetID)
	if err == nil {
		tweet.CommentCount = int(commentCount)
	}

	// 4. 填充其他统计数据 (包括 Poll)
	s.populateTweetStats(ctx, []*domain.Tweet{tweet}, requestingUserID)

	return tweet, nil
}

// GetUserTimeline 获取用户时间线（拉模式）
func (s *TweetService) GetUserTimeline(ctx context.Context, userID uint64, cursor uint64, limit int, requestingUserID uint64) ([]*domain.Tweet, uint64, bool, error) {
	// 1. 参数验证
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	// 2. 从数据库拉取
	tweets, err := s.repo.ListByUserID(ctx, userID, cursor, limit+1)
	if err != nil {
		return nil, 0, false, fmt.Errorf("failed to get user timeline: %w", err)
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

	// 5. 填充统计数据 (点赞数、是否点赞)
	s.populateTweetStats(ctx, tweets, requestingUserID)

	return tweets, nextCursor, hasMore, nil
}

// GetFeeds 获取关注流（推拉结合 + 消息队列）
func (s *TweetService) GetFeeds(ctx context.Context, userID uint64, cursor uint64, limit int) ([]*domain.Tweet, uint64, bool, error) {
	// 注意：GetFeeds 的 userID 就是 requestingUserID，因为查看的是自己的关注流
	requestingUserID := userID

	// 1. 参数验证
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	// 2. 从 Redis 获取 Timeline（推文 ID 列表）
	tweetIDs, err := s.timelineCache.GetTimeline(ctx, userID, cursor, limit+1)
	if err != nil {
		logger.Warn(ctx, "⚠️  Failed to get timeline from redis", zap.Error(err))
		// 降级：使用拉模式
		return s.getFeedsByPull(ctx, userID, cursor, limit, requestingUserID)
	}

	// 3. Redis 缓存为空，使用拉模式
	if len(tweetIDs) == 0 {
		logger.Info(ctx, "ℹ️  Timeline cache empty, using pull mode", zap.Uint64("user_id", userID))
		return s.getFeedsByPull(ctx, userID, cursor, limit, requestingUserID)
	}

	// 4. 判断是否还有更多
	hasMore := len(tweetIDs) > limit
	if hasMore {
		tweetIDs = tweetIDs[:limit]
	}

	// 5. 批量查询推文详情
	tweets, err := s.repo.GetByIDs(ctx, tweetIDs)
	if err != nil {
		return nil, 0, false, fmt.Errorf("failed to get tweets by ids: %w", err)
	}

	// 6. 按照 tweetIDs 的顺序排序
	tweetMap := make(map[uint64]*domain.Tweet, len(tweets))
	for _, tweet := range tweets {
		tweetMap[tweet.ID] = tweet
	}

	sortedTweets := make([]*domain.Tweet, 0, len(tweetIDs))
	for _, tweetID := range tweetIDs {
		if tweet, ok := tweetMap[tweetID]; ok {
			sortedTweets = append(sortedTweets, tweet)
		}
	}

	// 7. 计算下一页游标
	var nextCursor uint64
	if hasMore && len(sortedTweets) > 0 {
		nextCursor = sortedTweets[len(sortedTweets)-1].ID
	}

	// 8. 填充统计数据
	s.populateTweetStats(ctx, sortedTweets, requestingUserID)

	logger.Info(ctx, "✅ Feeds loaded from redis", zap.Uint64("user_id", userID), zap.Int("count", len(sortedTweets)))

	return sortedTweets, nextCursor, hasMore, nil
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

// populateTweetStats 批量填充推文统计数据 (点赞数、是否点赞)
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

	// 2. 批量检查是否点赞 (仅当 requestingUserID > 0)
	if requestingUserID > 0 {
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
	}

	// 3. 批量获取评论数
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
	// 4. 批量获取投票信息
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
	tweet, err := s.repo.GetByID(ctx, tweetID)
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
	tweet, err := s.repo.GetByID(ctx, tweetID)
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
func (s *TweetService) SearchTweets(ctx context.Context, query string, cursor uint64, limit int) ([]*domain.Tweet, uint64, bool, error) {
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
	// 搜索时 requestingUserID 暂不传递，或者需要从 ctx 获取 (如果需要 is_liked 状态)
	// 这里为了简化，暂不传 requestingUserID (is_liked = false)
	s.populateTweetStats(ctx, tweets, 0)

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
func (s *TweetService) ListTweets(ctx context.Context, cursor uint64, limit int) ([]*domain.Tweet, uint64, bool, error) {
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
	// 全站流不需要传 requestingUserID，除非我们需要显示当前用户是否点赞
	// 这里为了简单暂不传，如果需要，Controller 层需要解析 Token 并传入
	s.populateTweetStats(ctx, tweets, 0)

	return tweets, nextCursor, hasMore, nil
}

// GetTweetReplies 获取推文回复
func (s *TweetService) GetTweetReplies(ctx context.Context, tweetID uint64, cursor uint64, limit int) ([]*domain.Tweet, uint64, bool, error) {
	// 1. 获取回复
	tweets, nextCursor, err := s.repo.GetReplies(ctx, tweetID, cursor, limit)
	if err != nil {
		return nil, 0, false, err
	}

	// 2. 丰富数据
	s.populateTweetStats(ctx, tweets, 0)

	return tweets, nextCursor, nextCursor > 0, nil
}
