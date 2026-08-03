package grpc

import (
	"context"
	"errors"
	"log"
	"time"
	"unicode/utf8"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	tweetv1 "twitter-clone/api/tweet/v1"
	"twitter-clone/internal/domain"
	"twitter-clone/internal/module/tweet/service"
)

// TweetServer gRPC 服务器
type TweetServer struct {
	tweetv1.UnimplementedTweetServiceServer
	svc *service.TweetService
}

// NewTweetServer 创建 Tweet gRPC 服务器
func NewTweetServer(svc *service.TweetService) *TweetServer {
	return &TweetServer{svc: svc}
}

// CreateTweet 发布推文
func (s *TweetServer) CreateTweet(ctx context.Context, req *tweetv1.CreateTweetRequest) (*tweetv1.CreateTweetResponse, error) {
	log.Printf("gRPC: CreateTweet - user_id=%d, content_chars=%d, idempotent=%t", req.UserId, utf8.RuneCountInString(req.Content), req.IdempotencyKey != "")

	// 调用 Service 层
	tweet, err := s.svc.CreateTweetIdempotent(ctx, req.UserId, req.Content, req.MediaUrls, req.ParentId, req.PollOptions, req.PollDurationMinutes, req.IdempotencyKey)
	if err != nil {
		log.Printf("❌ CreateTweet error: %v", err)
		code := codes.Internal
		switch {
		case errors.Is(err, service.ErrInvalidContent), errors.Is(err, service.ErrContentTooLong),
			errors.Is(err, service.ErrInvalidMediaURL), errors.Is(err, service.ErrTooManyMedia),
			errors.Is(err, service.ErrInvalidIdempotencyKey):
			code = codes.InvalidArgument
		case errors.Is(err, service.ErrIdempotencyConflict):
			code = codes.AlreadyExists
		case errors.Is(err, service.ErrIdempotencyUnavailable):
			code = codes.FailedPrecondition
		}
		return nil, status.Errorf(code, "failed to create tweet: %v", err)
	}

	// 转换为 Protobuf 消息
	return &tweetv1.CreateTweetResponse{
		Tweet: domainTweetToProto(tweet),
	}, nil
}

// GetTweet 获取推文详情
func (s *TweetServer) GetTweet(ctx context.Context, req *tweetv1.GetTweetRequest) (*tweetv1.GetTweetResponse, error) {
	log.Printf("gRPC: GetTweet - tweet_id=%d", req.TweetId)

	tweet, err := s.svc.GetTweet(ctx, req.TweetId, req.RequestingUserId)
	if err != nil {
		log.Printf("❌ GetTweet error: %v", err)
		return nil, status.Errorf(codes.NotFound, "tweet not found: %v", err)
	}

	return &tweetv1.GetTweetResponse{
		Tweet: domainTweetToProto(tweet),
	}, nil
}

// DeleteTweet 删除推文
func (s *TweetServer) DeleteTweet(ctx context.Context, req *tweetv1.DeleteTweetRequest) (*tweetv1.DeleteTweetResponse, error) {
	log.Printf("gRPC: DeleteTweet - tweet_id=%d, user_id=%d", req.TweetId, req.UserId)

	err := s.svc.DeleteTweet(ctx, req.TweetId, req.UserId)
	if err != nil {
		log.Printf("❌ DeleteTweet error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to delete tweet: %v", err)
	}

	return &tweetv1.DeleteTweetResponse{
		Message: "tweet deleted successfully",
	}, nil
}

// GetUserTimeline 获取用户时间线
func (s *TweetServer) GetAuthorPostingStats(ctx context.Context, req *tweetv1.GetAuthorPostingStatsRequest) (*tweetv1.GetAuthorPostingStatsResponse, error) {
	if req.AuthorId == 0 {
		return nil, status.Error(codes.InvalidArgument, "author_id is required")
	}
	if req.LookbackSeconds > int64((24*time.Hour)/time.Second) {
		return nil, status.Error(codes.InvalidArgument, "lookback_seconds exceeds 24 hours")
	}

	stats, err := s.svc.GetAuthorPostingStats(ctx, req.AuthorId, time.Duration(req.LookbackSeconds)*time.Second)
	if err != nil {
		code := codes.Internal
		if errors.Is(err, service.ErrInvalidAuthorID) {
			code = codes.InvalidArgument
		}
		return nil, status.Errorf(code, "failed to get author posting stats: %v", err)
	}
	return &tweetv1.GetAuthorPostingStatsResponse{
		SampleCount:       int32(stats.SampleCount),
		LatestCreatedAt:   stats.LatestCreatedAt,
		PreviousCreatedAt: stats.PreviousCreatedAt,
	}, nil
}

func (s *TweetServer) ApplyTweetModeration(ctx context.Context, req *tweetv1.ApplyTweetModerationRequest) (*tweetv1.ApplyTweetModerationResponse, error) {
	if req.TweetId == 0 || req.AuthorId == 0 {
		return nil, status.Error(codes.InvalidArgument, "tweet_id and author_id are required")
	}
	if len(req.ReasonCode) > 128 {
		return nil, status.Error(codes.InvalidArgument, "reason_code exceeds 128 characters")
	}

	var action service.TweetModerationAction
	switch req.Action {
	case tweetv1.TweetModerationAction_TWEET_MODERATION_ACTION_SHADOWBAN:
		action = service.TweetModerationActionShadowban
	default:
		return nil, status.Error(codes.InvalidArgument, "unsupported moderation action")
	}

	result, err := s.svc.ApplyTweetModeration(ctx, req.TweetId, req.AuthorId, action)
	if err != nil {
		code := codes.Internal
		switch {
		case errors.Is(err, service.ErrInvalidTweetID), errors.Is(err, service.ErrInvalidAuthorID), errors.Is(err, service.ErrInvalidModerationAction):
			code = codes.InvalidArgument
		case errors.Is(err, service.ErrTweetNotFound):
			code = codes.NotFound
		case errors.Is(err, service.ErrUnauthorized):
			code = codes.PermissionDenied
		case errors.Is(err, service.ErrModerationUnavailable):
			code = codes.FailedPrecondition
		}
		return nil, status.Errorf(code, "failed to apply tweet moderation: %v", err)
	}

	log.Printf(
		"gRPC: ApplyTweetModeration - tweet_id=%d author_id=%d action=%s reason_code=%q applied=%t cleanup_queued=%t timelines_cleaned=%d",
		req.TweetId,
		req.AuthorId,
		req.Action.String(),
		req.ReasonCode,
		result.Applied,
		result.CleanupQueued,
		result.TimelinesCleaned,
	)
	return &tweetv1.ApplyTweetModerationResponse{
		Applied:          result.Applied,
		TimelinesCleaned: int32(result.TimelinesCleaned),
		CleanupQueued:    result.CleanupQueued,
	}, nil
}

func (s *TweetServer) GetUserTimeline(ctx context.Context, req *tweetv1.GetUserTimelineRequest) (*tweetv1.GetUserTimelineResponse, error) {
	log.Printf("gRPC: GetUserTimeline - user_id=%d, cursor=%d, limit=%d", req.UserId, req.Cursor, req.Limit)

	tweets, nextCursor, hasMore, err := s.svc.GetUserTimeline(ctx, req.UserId, req.Cursor, int(req.Limit), req.RequestingUserId)
	if err != nil {
		log.Printf("❌ GetUserTimeline error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to get user timeline: %v", err)
	}

	// 转换为 Protobuf 消息列表
	protoTweets := make([]*tweetv1.Tweet, len(tweets))
	for i, tweet := range tweets {
		protoTweets[i] = domainTweetToProto(tweet)
	}

	return &tweetv1.GetUserTimelineResponse{
		Tweets:     protoTweets,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	}, nil
}

// GetFeeds 获取关注流
func (s *TweetServer) GetFeeds(ctx context.Context, req *tweetv1.GetFeedsRequest) (*tweetv1.GetFeedsResponse, error) {
	log.Printf("gRPC: GetFeeds - user_id=%d, cursor=%d, limit=%d", req.UserId, req.Cursor, req.Limit)

	tweets, nextCursor, hasMore, err := s.svc.GetFeeds(ctx, req.UserId, req.Cursor, int(req.Limit))
	if err != nil {
		log.Printf("❌ GetFeeds error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to get feeds: %v", err)
	}

	// 转换为 Protobuf 消息列表
	protoTweets := make([]*tweetv1.Tweet, len(tweets))
	for i, tweet := range tweets {
		protoTweets[i] = domainTweetToProto(tweet)
	}

	return &tweetv1.GetFeedsResponse{
		Tweets:     protoTweets,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	}, nil
}

// LikeTweet 点赞推文
func (s *TweetServer) LikeTweet(ctx context.Context, req *tweetv1.LikeTweetRequest) (*tweetv1.LikeTweetResponse, error) {
	log.Printf("gRPC: LikeTweet - user_id=%d, tweet_id=%d", req.UserId, req.TweetId)

	count, err := s.svc.LikeTweet(ctx, req.UserId, req.TweetId)
	if err != nil {
		log.Printf("❌ LikeTweet error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to like tweet: %v", err)
	}

	return &tweetv1.LikeTweetResponse{
		LikeCount: int32(count),
	}, nil
}

// UnlikeTweet 取消点赞
func (s *TweetServer) UnlikeTweet(ctx context.Context, req *tweetv1.UnlikeTweetRequest) (*tweetv1.UnlikeTweetResponse, error) {
	log.Printf("gRPC: UnlikeTweet - user_id=%d, tweet_id=%d", req.UserId, req.TweetId)

	count, err := s.svc.UnlikeTweet(ctx, req.UserId, req.TweetId)
	if err != nil {
		log.Printf("❌ UnlikeTweet error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to unlike tweet: %v", err)
	}

	return &tweetv1.UnlikeTweetResponse{
		LikeCount: int32(count),
	}, nil
}

// VotePoll 投票
func (s *TweetServer) VotePoll(ctx context.Context, req *tweetv1.VotePollRequest) (*tweetv1.VotePollResponse, error) {
	log.Printf("gRPC: VotePoll - user_id=%d, poll_id=%d, option_id=%d", req.UserId, req.PollId, req.OptionId)

	poll, err := s.svc.VotePoll(ctx, req.UserId, req.PollId, req.OptionId)
	if err != nil {
		log.Printf("❌ VotePoll error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to vote poll: %v", err)
	}

	return &tweetv1.VotePollResponse{
		Poll: domainPollToProto(poll),
	}, nil
}

// ==================== 评论相关 ====================

// CreateComment 发布评论
func (s *TweetServer) CreateComment(ctx context.Context, req *tweetv1.CreateCommentRequest) (*tweetv1.CreateCommentResponse, error) {
	log.Printf("gRPC: CreateComment - user_id=%d, tweet_id=%d, content=%s", req.UserId, req.TweetId, req.Content)

	comment, err := s.svc.CreateComment(ctx, req.UserId, req.TweetId, req.Content, req.ParentId)
	if err != nil {
		log.Printf("❌ CreateComment error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to create comment: %v", err)
	}

	return &tweetv1.CreateCommentResponse{
		Comment: domainCommentToProto(comment),
	}, nil
}

// DeleteComment 删除评论
func (s *TweetServer) DeleteComment(ctx context.Context, req *tweetv1.DeleteCommentRequest) (*tweetv1.DeleteCommentResponse, error) {
	log.Printf("gRPC: DeleteComment - comment_id=%d, user_id=%d", req.CommentId, req.UserId)

	err := s.svc.DeleteComment(ctx, req.CommentId, req.UserId)
	if err != nil {
		log.Printf("❌ DeleteComment error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to delete comment: %v", err)
	}

	return &tweetv1.DeleteCommentResponse{
		Message: "comment deleted successfully",
	}, nil
}

// GetTweetComments 获取推文评论列表
func (s *TweetServer) GetTweetComments(ctx context.Context, req *tweetv1.GetTweetCommentsRequest) (*tweetv1.GetTweetCommentsResponse, error) {
	log.Printf("gRPC: GetTweetComments - tweet_id=%d, cursor=%d, limit=%d", req.TweetId, req.Cursor, req.Limit)

	comments, nextCursor, hasMore, err := s.svc.GetTweetComments(ctx, req.TweetId, req.Cursor, int(req.Limit))
	if err != nil {
		log.Printf("❌ GetTweetComments error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to get tweet comments: %v", err)
	}

	// 转换为 Protobuf 消息列表
	protoComments := make([]*tweetv1.Comment, len(comments))
	for i, comment := range comments {
		protoComments[i] = domainCommentToProto(comment)
	}

	return &tweetv1.GetTweetCommentsResponse{
		Comments:   protoComments,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	}, nil
}

// SearchTweets 搜索推文
func (s *TweetServer) SearchTweets(ctx context.Context, req *tweetv1.SearchTweetsRequest) (*tweetv1.SearchTweetsResponse, error) {
	log.Printf("gRPC: SearchTweets - query=%s, cursor=%d, limit=%d", req.Query, req.Cursor, req.Limit)

	tweets, nextCursor, hasMore, err := s.svc.SearchTweets(ctx, req.Query, req.Cursor, int(req.Limit), req.RequestingUserId)
	if err != nil {
		log.Printf("❌ SearchTweets error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to search tweets: %v", err)
	}

	// 转换为 Protobuf 消息列表
	protoTweets := make([]*tweetv1.Tweet, len(tweets))
	for i, tweet := range tweets {
		protoTweets[i] = domainTweetToProto(tweet)
	}

	return &tweetv1.SearchTweetsResponse{
		Tweets:     protoTweets,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	}, nil
}

// GetTrendingTopics 获取热门话题
func (s *TweetServer) GetTrendingTopics(ctx context.Context, req *tweetv1.GetTrendingTopicsRequest) (*tweetv1.GetTrendingTopicsResponse, error) {
	log.Printf("gRPC: GetTrendingTopics - limit=%d", req.Limit)

	topics, err := s.svc.GetTrendingTopics(ctx, int(req.Limit))
	if err != nil {
		log.Printf("❌ GetTrendingTopics error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to get trending topics: %v", err)
	}

	// 转换为 Protobuf 消息列表
	protoTopics := make([]*tweetv1.TrendingTopic, len(topics))
	for i, topic := range topics {
		protoTopics[i] = &tweetv1.TrendingTopic{
			Topic: topic.Topic,
			Score: topic.Score,
		}
	}

	return &tweetv1.GetTrendingTopicsResponse{
		Topics: protoTopics,
	}, nil
}

// ListTweets 获取推文列表（全站）
func (s *TweetServer) ListTweets(ctx context.Context, req *tweetv1.ListTweetsRequest) (*tweetv1.ListTweetsResponse, error) {
	log.Printf("gRPC: ListTweets - cursor=%d, limit=%d", req.Cursor, req.Limit)

	tweets, nextCursor, hasMore, err := s.svc.ListTweets(ctx, req.Cursor, int(req.Limit), req.RequestingUserId)
	if err != nil {
		log.Printf("❌ ListTweets error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to list tweets: %v", err)
	}

	protoTweets := make([]*tweetv1.Tweet, len(tweets))
	for i, tweet := range tweets {
		protoTweets[i] = domainTweetToProto(tweet)
	}

	return &tweetv1.ListTweetsResponse{
		Tweets:     protoTweets,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	}, nil
}

// domainCommentToProto 将 Domain Comment 转换为 Protobuf Comment
func domainCommentToProto(comment *domain.Comment) *tweetv1.Comment {
	return &tweetv1.Comment{
		Id:        comment.ID,
		UserId:    comment.UserID,
		TweetId:   comment.TweetID,
		Content:   comment.Content,
		CreatedAt: comment.CreatedAt,
		// 用户信息暂不填充，需 API 网关聚合
	}
}

// domainTweetToProto 将 Domain Tweet 转换为 Protobuf Tweet
func domainTweetToProto(tweet *domain.Tweet) *tweetv1.Tweet {
	return &tweetv1.Tweet{
		Id:                 tweet.ID,
		UserId:             tweet.UserID,
		Content:            tweet.Content,
		MediaUrls:          tweet.MediaURLs,
		Type:               int32(tweet.Type),
		VisibleType:        int32(tweet.VisibleType),
		CreatedAt:          tweet.CreatedAt,
		UpdatedAt:          tweet.UpdatedAt,
		LikeCount:          int32(tweet.LikeCount),
		CommentCount:       int32(tweet.CommentCount),
		ShareCount:         int32(tweet.ShareCount),
		IsLiked:            tweet.IsLiked,
		Poll:               domainPollToProto(tweet.Poll),
		IsBookmarked:       tweet.IsBookmarked,
		IsRetweeted:        tweet.IsRetweeted,
		IsRetweetedDisplay: tweet.IsRetweetedDisplay,
		RetweetedAt:        tweet.RetweetedAt,
		SortId:             tweet.SortID,
	}
}

func domainPollToProto(poll *domain.Poll) *tweetv1.Poll {
	if poll == nil {
		return nil
	}
	options := make([]*tweetv1.PollOption, len(poll.Options))
	for i, opt := range poll.Options {
		options[i] = &tweetv1.PollOption{
			Id:         opt.ID,
			PollId:     opt.PollID,
			Text:       opt.Text,
			VoteCount:  int32(opt.VoteCount),
			Percentage: opt.Percentage,
		}
	}
	log.Printf("Converting Poll to Proto: ID=%d, TotalVotes=%d", poll.ID, poll.TotalVotes)
	return &tweetv1.Poll{
		Id:            poll.ID,
		TweetId:       poll.TweetID,
		Question:      poll.Question,
		Options:       options,
		EndTime:       poll.EndTime,
		IsExpired:     poll.IsExpired,
		IsVoted:       poll.IsVoted,
		VotedOptionId: poll.VotedOptionID,
		TotalVotes:    int32(poll.TotalVotes),
	}
}

// GetTweetReplies 获取推文回复
func (s *TweetServer) GetTweetReplies(ctx context.Context, req *tweetv1.GetTweetRepliesRequest) (*tweetv1.GetTweetRepliesResponse, error) {
	log.Printf("gRPC: GetTweetReplies - tweet_id=%d, cursor=%d, limit=%d", req.TweetId, req.Cursor, req.Limit)

	tweets, nextCursor, hasMore, err := s.svc.GetTweetReplies(ctx, req.TweetId, req.Cursor, int(req.Limit), req.RequestingUserId)
	if err != nil {
		log.Printf("❌ GetTweetReplies error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to get tweet replies: %v", err)
	}

	protoTweets := make([]*tweetv1.Tweet, len(tweets))
	for i, tweet := range tweets {
		protoTweets[i] = domainTweetToProto(tweet)
	}

	return &tweetv1.GetTweetRepliesResponse{
		Replies:    protoTweets,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	}, nil
}

// BookmarkTweet 收藏推文
func (s *TweetServer) BookmarkTweet(ctx context.Context, req *tweetv1.BookmarkTweetRequest) (*tweetv1.BookmarkTweetResponse, error) {
	log.Printf("gRPC: BookmarkTweet - user_id=%d, tweet_id=%d", req.UserId, req.TweetId)

	err := s.svc.BookmarkTweet(ctx, req.UserId, req.TweetId)
	if err != nil {
		log.Printf("❌ BookmarkTweet error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to bookmark tweet: %v", err)
	}

	return &tweetv1.BookmarkTweetResponse{
		Message: "bookmarked successfully",
	}, nil
}

// UnbookmarkTweet 取消收藏
func (s *TweetServer) UnbookmarkTweet(ctx context.Context, req *tweetv1.UnbookmarkTweetRequest) (*tweetv1.UnbookmarkTweetResponse, error) {
	log.Printf("gRPC: UnbookmarkTweet - user_id=%d, tweet_id=%d", req.UserId, req.TweetId)

	err := s.svc.UnbookmarkTweet(ctx, req.UserId, req.TweetId)
	if err != nil {
		log.Printf("❌ UnbookmarkTweet error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to unbookmark tweet: %v", err)
	}

	return &tweetv1.UnbookmarkTweetResponse{
		Message: "unbookmarked successfully",
	}, nil
}

// GetUserBookmarks 获取用户收藏列表
func (s *TweetServer) GetUserBookmarks(ctx context.Context, req *tweetv1.GetUserBookmarksRequest) (*tweetv1.GetUserBookmarksResponse, error) {
	log.Printf("gRPC: GetUserBookmarks - user_id=%d, cursor=%d, limit=%d", req.UserId, req.Cursor, req.Limit)

	tweets, nextCursor, hasMore, err := s.svc.GetUserBookmarks(ctx, req.UserId, req.Cursor, int(req.Limit))
	if err != nil {
		log.Printf("❌ GetUserBookmarks error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to get bookmarks: %v", err)
	}

	protoTweets := make([]*tweetv1.Tweet, len(tweets))
	for i, tweet := range tweets {
		protoTweets[i] = domainTweetToProto(tweet)
	}

	return &tweetv1.GetUserBookmarksResponse{
		Tweets:     protoTweets,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	}, nil
}

// RetweetTweet 转发推文
func (s *TweetServer) RetweetTweet(ctx context.Context, req *tweetv1.RetweetTweetRequest) (*tweetv1.RetweetTweetResponse, error) {
	log.Printf("gRPC: RetweetTweet - user_id=%d, tweet_id=%d", req.UserId, req.TweetId)

	count, err := s.svc.RetweetTweet(ctx, req.UserId, req.TweetId)
	if err != nil {
		log.Printf("❌ RetweetTweet error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to retweet: %v", err)
	}

	return &tweetv1.RetweetTweetResponse{
		RetweetCount: count,
	}, nil
}

// UnretweetTweet 取消转发
func (s *TweetServer) UnretweetTweet(ctx context.Context, req *tweetv1.UnretweetTweetRequest) (*tweetv1.UnretweetTweetResponse, error) {
	log.Printf("gRPC: UnretweetTweet - user_id=%d, tweet_id=%d", req.UserId, req.TweetId)

	count, err := s.svc.UnretweetTweet(ctx, req.UserId, req.TweetId)
	if err != nil {
		log.Printf("❌ UnretweetTweet error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to unretweet: %v", err)
	}

	return &tweetv1.UnretweetTweetResponse{
		RetweetCount: count,
	}, nil
}

// GetUserLikes 获取用户点赞推文列表
func (s *TweetServer) GetUserLikes(ctx context.Context, req *tweetv1.GetUserLikesRequest) (*tweetv1.GetUserLikesResponse, error) {
	log.Printf("gRPC: GetUserLikes - user_id=%d, cursor=%d, limit=%d", req.UserId, req.Cursor, req.Limit)

	tweets, nextCursor, hasMore, err := s.svc.GetUserLikes(ctx, req.UserId, req.Cursor, int(req.Limit), req.RequestingUserId)
	if err != nil {
		log.Printf("❌ GetUserLikes error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to get user likes: %v", err)
	}

	protoTweets := make([]*tweetv1.Tweet, len(tweets))
	for i, tweet := range tweets {
		protoTweets[i] = domainTweetToProto(tweet)
	}

	return &tweetv1.GetUserLikesResponse{
		Tweets:     protoTweets,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	}, nil
}

// GetUserReplies 获取用户回复推文列表
func (s *TweetServer) GetUserReplies(ctx context.Context, req *tweetv1.GetUserRepliesRequest) (*tweetv1.GetUserRepliesResponse, error) {
	log.Printf("gRPC: GetUserReplies - user_id=%d, cursor=%d, limit=%d", req.UserId, req.Cursor, req.Limit)

	tweets, nextCursor, hasMore, err := s.svc.GetUserReplies(ctx, req.UserId, req.Cursor, int(req.Limit), req.RequestingUserId)
	if err != nil {
		log.Printf("❌ GetUserReplies error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to get user replies: %v", err)
	}

	protoTweets := make([]*tweetv1.Tweet, len(tweets))
	for i, tweet := range tweets {
		protoTweets[i] = domainTweetToProto(tweet)
	}

	return &tweetv1.GetUserRepliesResponse{
		Replies:    protoTweets,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	}, nil
}

// GetUserMedia 获取用户媒体推文列表
func (s *TweetServer) GetUserMedia(ctx context.Context, req *tweetv1.GetUserMediaRequest) (*tweetv1.GetUserMediaResponse, error) {
	log.Printf("gRPC: GetUserMedia - user_id=%d, cursor=%d, limit=%d", req.UserId, req.Cursor, req.Limit)

	tweets, nextCursor, hasMore, err := s.svc.GetUserMedia(ctx, req.UserId, req.Cursor, int(req.Limit), req.RequestingUserId)
	if err != nil {
		log.Printf("❌ GetUserMedia error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to get user media: %v", err)
	}

	protoTweets := make([]*tweetv1.Tweet, len(tweets))
	for i, tweet := range tweets {
		protoTweets[i] = domainTweetToProto(tweet)
	}

	return &tweetv1.GetUserMediaResponse{
		Tweets:     protoTweets,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	}, nil
}
