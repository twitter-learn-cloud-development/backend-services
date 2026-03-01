package handler

import (
	"context"

	"github.com/stretchr/testify/mock"
	"google.golang.org/grpc"

	tweetv1 "twitter-clone/api/tweet/v1"
	userv1 "twitter-clone/api/user/v1"
)

// MockUserServiceClient 模拟用户服务客户端
type MockUserServiceClient struct {
	mock.Mock
}

func (m *MockUserServiceClient) Register(ctx context.Context, in *userv1.RegisterRequest, opts ...grpc.CallOption) (*userv1.RegisterResponse, error) {
	args := m.Called(ctx, in)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*userv1.RegisterResponse), args.Error(1)
}

func (m *MockUserServiceClient) Login(ctx context.Context, in *userv1.LoginRequest, opts ...grpc.CallOption) (*userv1.LoginResponse, error) {
	args := m.Called(ctx, in)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*userv1.LoginResponse), args.Error(1)
}

func (m *MockUserServiceClient) GetProfile(ctx context.Context, in *userv1.GetProfileRequest, opts ...grpc.CallOption) (*userv1.GetProfileResponse, error) {
	args := m.Called(ctx, in)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*userv1.GetProfileResponse), args.Error(1)
}

func (m *MockUserServiceClient) UpdateProfile(ctx context.Context, in *userv1.UpdateProfileRequest, opts ...grpc.CallOption) (*userv1.UpdateProfileResponse, error) {
	args := m.Called(ctx, in)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*userv1.UpdateProfileResponse), args.Error(1)
}

func (m *MockUserServiceClient) ChangePassword(ctx context.Context, in *userv1.ChangePasswordRequest, opts ...grpc.CallOption) (*userv1.ChangePasswordResponse, error) {
	args := m.Called(ctx, in)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*userv1.ChangePasswordResponse), args.Error(1)
}

// MockTweetServiceClient 模拟推文服务客户端
type MockTweetServiceClient struct {
	mock.Mock
}

func (m *MockTweetServiceClient) CreateTweet(ctx context.Context, in *tweetv1.CreateTweetRequest, opts ...grpc.CallOption) (*tweetv1.CreateTweetResponse, error) {
	args := m.Called(ctx, in)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*tweetv1.CreateTweetResponse), args.Error(1)
}

func (m *MockTweetServiceClient) GetTweet(ctx context.Context, in *tweetv1.GetTweetRequest, opts ...grpc.CallOption) (*tweetv1.GetTweetResponse, error) {
	args := m.Called(ctx, in)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*tweetv1.GetTweetResponse), args.Error(1)
}

func (m *MockTweetServiceClient) GetUserTimeline(ctx context.Context, in *tweetv1.GetUserTimelineRequest, opts ...grpc.CallOption) (*tweetv1.GetUserTimelineResponse, error) {
	args := m.Called(ctx, in)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*tweetv1.GetUserTimelineResponse), args.Error(1)
}

func (m *MockTweetServiceClient) DeleteTweet(ctx context.Context, in *tweetv1.DeleteTweetRequest, opts ...grpc.CallOption) (*tweetv1.DeleteTweetResponse, error) {
	args := m.Called(ctx, in)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*tweetv1.DeleteTweetResponse), args.Error(1)
}

func (m *MockTweetServiceClient) LikeTweet(ctx context.Context, in *tweetv1.LikeTweetRequest, opts ...grpc.CallOption) (*tweetv1.LikeTweetResponse, error) {
	args := m.Called(ctx, in)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*tweetv1.LikeTweetResponse), args.Error(1)
}

func (m *MockTweetServiceClient) UnlikeTweet(ctx context.Context, in *tweetv1.UnlikeTweetRequest, opts ...grpc.CallOption) (*tweetv1.UnlikeTweetResponse, error) {
	args := m.Called(ctx, in)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*tweetv1.UnlikeTweetResponse), args.Error(1)
}

func (m *MockTweetServiceClient) CreateComment(ctx context.Context, in *tweetv1.CreateCommentRequest, opts ...grpc.CallOption) (*tweetv1.CreateCommentResponse, error) {
	args := m.Called(ctx, in)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*tweetv1.CreateCommentResponse), args.Error(1)
}

func (m *MockTweetServiceClient) DeleteComment(ctx context.Context, in *tweetv1.DeleteCommentRequest, opts ...grpc.CallOption) (*tweetv1.DeleteCommentResponse, error) {
	args := m.Called(ctx, in)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*tweetv1.DeleteCommentResponse), args.Error(1)
}

func (m *MockTweetServiceClient) GetTweetComments(ctx context.Context, in *tweetv1.GetTweetCommentsRequest, opts ...grpc.CallOption) (*tweetv1.GetTweetCommentsResponse, error) {
	args := m.Called(ctx, in)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*tweetv1.GetTweetCommentsResponse), args.Error(1)
}

func (m *MockTweetServiceClient) GetFeeds(ctx context.Context, in *tweetv1.GetFeedsRequest, opts ...grpc.CallOption) (*tweetv1.GetFeedsResponse, error) {
	args := m.Called(ctx, in)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*tweetv1.GetFeedsResponse), args.Error(1)
}

func (m *MockTweetServiceClient) SearchTweets(ctx context.Context, in *tweetv1.SearchTweetsRequest, opts ...grpc.CallOption) (*tweetv1.SearchTweetsResponse, error) {
	args := m.Called(ctx, in)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*tweetv1.SearchTweetsResponse), args.Error(1)
}

func (m *MockTweetServiceClient) GetTrendingTopics(ctx context.Context, in *tweetv1.GetTrendingTopicsRequest, opts ...grpc.CallOption) (*tweetv1.GetTrendingTopicsResponse, error) {
	args := m.Called(ctx, in)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*tweetv1.GetTrendingTopicsResponse), args.Error(1)
}
