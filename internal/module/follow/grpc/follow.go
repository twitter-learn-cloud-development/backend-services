package grpc

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"go.uber.org/zap"

	followv1 "twitter-clone/api/follow/v1"
	"twitter-clone/internal/module/follow/service"
	"twitter-clone/pkg/logger"
)

// FollowServer gRPC 服务器
type FollowServer struct {
	followv1.UnimplementedFollowServiceServer
	svc *service.FollowService
}

// NewFollowServer 创建 Follow gRPC 服务器
func NewFollowServer(svc *service.FollowService) *FollowServer {
	return &FollowServer{svc: svc}
}

// Follow 关注用户
func (s *FollowServer) Follow(ctx context.Context, req *followv1.FollowRequest) (*followv1.FollowResponse, error) {
	logger.Info(ctx, "gRPC: Follow", zap.Uint64("follower_id", req.FollowerId), zap.Uint64("followee_id", req.FolloweeId))

	err := s.svc.Follow(ctx, req.FollowerId, req.FolloweeId)
	if err != nil {
		logger.Error(ctx, "❌ Follow error", zap.Error(err))
		return nil, status.Errorf(codes.Internal, "failed to follow: %v", err)
	}

	return &followv1.FollowResponse{
		Message: "followed successfully",
	}, nil
}

// Unfollow 取消关注
func (s *FollowServer) Unfollow(ctx context.Context, req *followv1.UnfollowRequest) (*followv1.UnfollowResponse, error) {
	logger.Info(ctx, "gRPC: Unfollow", zap.Uint64("follower_id", req.FollowerId), zap.Uint64("followee_id", req.FolloweeId))

	err := s.svc.Unfollow(ctx, req.FollowerId, req.FolloweeId)
	if err != nil {
		logger.Error(ctx, "❌ Unfollow error", zap.Error(err))
		return nil, status.Errorf(codes.Internal, "failed to unfollow: %v", err)
	}

	return &followv1.UnfollowResponse{
		Message: "unfollowed successfully",
	}, nil
}

// IsFollowing 检查是否关注
func (s *FollowServer) IsFollowing(ctx context.Context, req *followv1.IsFollowingRequest) (*followv1.IsFollowingResponse, error) {
	logger.Info(ctx, "gRPC: IsFollowing", zap.Uint64("follower_id", req.FollowerId), zap.Uint64("followee_id", req.FolloweeId))

	isFollowing, err := s.svc.IsFollowing(ctx, req.FollowerId, req.FolloweeId)
	if err != nil {
		logger.Error(ctx, "❌ IsFollowing error", zap.Error(err))
		return nil, status.Errorf(codes.Internal, "failed to check following status: %v", err)
	}

	return &followv1.IsFollowingResponse{
		IsFollowing: isFollowing,
	}, nil
}

// GetFollowers 获取粉丝列表
func (s *FollowServer) GetFollowers(ctx context.Context, req *followv1.GetFollowersRequest) (*followv1.GetFollowersResponse, error) {
	logger.Info(ctx, "gRPC: GetFollowers", zap.Uint64("user_id", req.UserId), zap.Uint64("cursor", req.Cursor), zap.Int32("limit", req.Limit))

	followerIDs, nextCursor, hasMore, err := s.svc.GetFollowers(ctx, req.UserId, req.Cursor, int(req.Limit))
	if err != nil {
		logger.Error(ctx, "❌ GetFollowers error", zap.Error(err))
		return nil, status.Errorf(codes.Internal, "failed to get followers: %v", err)
	}

	return &followv1.GetFollowersResponse{
		FollowerIds: followerIDs,
		NextCursor:  nextCursor,
		HasMore:     hasMore,
	}, nil
}

// GetFollowees 获取关注列表
func (s *FollowServer) GetFollowees(ctx context.Context, req *followv1.GetFolloweesRequest) (*followv1.GetFolloweesResponse, error) {
	logger.Info(ctx, "gRPC: GetFollowees", zap.Uint64("user_id", req.UserId), zap.Uint64("cursor", req.Cursor), zap.Int32("limit", req.Limit))

	followeeIDs, nextCursor, hasMore, err := s.svc.GetFollowees(ctx, req.UserId, req.Cursor, int(req.Limit))
	if err != nil {
		logger.Error(ctx, "❌ GetFollowees error", zap.Error(err))
		return nil, status.Errorf(codes.Internal, "failed to get followees: %v", err)
	}

	return &followv1.GetFolloweesResponse{
		FolloweeIds: followeeIDs,
		NextCursor:  nextCursor,
		HasMore:     hasMore,
	}, nil
}

// GetFollowStats 获取关注统计
func (s *FollowServer) GetFollowStats(ctx context.Context, req *followv1.GetFollowStatsRequest) (*followv1.GetFollowStatsResponse, error) {
	logger.Info(ctx, "gRPC: GetFollowStats", zap.Uint64("user_id", req.UserId))

	followerCount, followeeCount, err := s.svc.GetFollowStats(ctx, req.UserId)
	if err != nil {
		logger.Error(ctx, "❌ GetFollowStats error", zap.Error(err))
		return nil, status.Errorf(codes.Internal, "failed to get follow stats: %v", err)
	}

	return &followv1.GetFollowStatsResponse{
		FollowerCount: followerCount,
		FolloweeCount: followeeCount,
	}, nil
}
