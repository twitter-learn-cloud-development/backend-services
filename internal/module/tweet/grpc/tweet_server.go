package grpc

import (
	"context"
	"log"

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
	log.Printf("gRPC: CreateTweet - user_id=%d, content=%s", req.UserId, req.Content)

	// 调用 Service 层
	tweet, err := s.svc.CreateTweet(ctx, req.UserId, req.Content, req.MediaUrls)
	if err != nil {
		log.Printf("❌ CreateTweet error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to create tweet: %v", err)
	}

	// 转换为 Protobuf 消息
	return &tweetv1.CreateTweetResponse{
		Tweet: domainTweetToProto(tweet),
	}, nil
}

// GetTweet 获取推文详情
func (s *TweetServer) GetTweet(ctx context.Context, req *tweetv1.GetTweetRequest) (*tweetv1.GetTweetResponse, error) {
	log.Printf("gRPC: GetTweet - tweet_id=%d", req.TweetId)

	tweet, err := s.svc.GetTweet(ctx, req.TweetId)
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
func (s *TweetServer) GetUserTimeline(ctx context.Context, req *tweetv1.GetUserTimelineRequest) (*tweetv1.GetUserTimelineResponse, error) {
	log.Printf("gRPC: GetUserTimeline - user_id=%d, cursor=%d, limit=%d", req.UserId, req.Cursor, req.Limit)

	tweets, nextCursor, hasMore, err := s.svc.GetUserTimeline(ctx, req.UserId, req.Cursor, int(req.Limit))
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

// domainTweetToProto 将 Domain Tweet 转换为 Protobuf Tweet
func domainTweetToProto(tweet *domain.Tweet) *tweetv1.Tweet {
	return &tweetv1.Tweet{
		Id:           tweet.ID,
		UserId:       tweet.UserID,
		Content:      tweet.Content,
		MediaUrls:    tweet.MediaURLs,
		Type:         int32(tweet.Type),
		VisibleType:  int32(tweet.VisibleType),
		CreatedAt:    tweet.CreatedAt,
		UpdatedAt:    tweet.UpdatedAt,
		LikeCount:    int32(tweet.LikeCount),
		CommentCount: int32(tweet.CommentCount),
		ShareCount:   int32(tweet.ShareCount),
		IsLiked:      tweet.IsLiked,
	}
}
