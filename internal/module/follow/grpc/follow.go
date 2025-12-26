package grpc

import (
	"context"
	"log"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	followv1 "twitter-clone/api/follow/v1"
	"twitter-clone/internal/module/follow/service"
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
	log.Printf("gRPC: Follow - follower_id=%d, followee_id=%d", req.FollowerId, req.FolloweeId)

	err := s.svc.Follow(ctx, req.FollowerId, req.FolloweeId)
	if err != nil {
		log.Printf("❌ Follow error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to follow: %v", err)
	}

	return &followv1.FollowResponse{
		Message: "followed successfully",
	}, nil
}

// Unfollow 取消关注
func (s *FollowServer) Unfollow(ctx context.Context, req *followv1.UnfollowRequest) (*followv1.UnfollowResponse, error) {
	log.Printf("gRPC: Unfollow - follower_id=%d, followee_id=%d", req.FollowerId, req.FolloweeId)

	err := s.svc.Unfollow(ctx, req.FollowerId, req.FolloweeId)
	if err != nil {
		log.Printf("❌ Unfollow error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to unfollow: %v", err)
	}

	return &followv1.UnfollowResponse{
		Message: "unfollowed successfully",
	}, nil
}

// IsFollowing 检查是否关注
func (s *FollowServer) IsFollowing(ctx context.Context, req *followv1.IsFollowingRequest) (*followv1.IsFollowingResponse, error) {
	log.Printf("gRPC: IsFollowing - follower_id=%d, followee_id=%d", req.FollowerId, req.FolloweeId)

	isFollowing, err := s.svc.IsFollowing(ctx, req.FollowerId, req.FolloweeId)
	if err != nil {
		log.Printf("❌ IsFollowing error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to check following status: %v", err)
	}

	return &followv1.IsFollowingResponse{
		IsFollowing: isFollowing,
	}, nil
}

// GetFollowers 获取粉丝列表
func (s *FollowServer) GetFollowers(ctx context.Context, req *followv1.GetFollowersRequest) (*followv1.GetFollowersResponse, error) {
	log.Printf("gRPC: GetFollowers - user_id=%d, cursor=%d, limit=%d", req.UserId, req.Cursor, req.Limit)

	followerIDs, nextCursor, hasMore, err := s.svc.GetFollowers(ctx, req.UserId, req.Cursor, int(req.Limit))
	if err != nil {
		log.Printf("❌ GetFollowers error: %v", err)
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
	log.Printf("gRPC: GetFollowees - user_id=%d, cursor=%d, limit=%d", req.UserId, req.Cursor, req.Limit)

	followeeIDs, nextCursor, hasMore, err := s.svc.GetFollowees(ctx, req.UserId, req.Cursor, int(req.Limit))
	if err != nil {
		log.Printf("❌ GetFollowees error: %v", err)
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
	log.Printf("gRPC: GetFollowStats - user_id=%d", req.UserId)

	followerCount, followeeCount, err := s.svc.GetFollowStats(ctx, req.UserId)
	if err != nil {
		log.Printf("❌ GetFollowStats error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to get follow stats: %v", err)
	}

	return &followv1.GetFollowStatsResponse{
		FollowerCount: followerCount,
		FolloweeCount: followeeCount,
	}, nil
}
