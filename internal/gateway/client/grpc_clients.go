package client

import (
	"context"
	"fmt"
	"log"

	_ "github.com/mbobakov/grpc-consul-resolver" // Import Consul Resolver
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	followv1 "twitter-clone/api/follow/v1"
	messengerv1 "twitter-clone/api/messenger/v1"
	tweetv1 "twitter-clone/api/tweet/v1"
	userv1 "twitter-clone/api/user/v1"

	sentinel "github.com/alibaba/sentinel-golang/api"
)

type GRPCClients struct {
	UserClient      userv1.UserServiceClient
	TweetClient     tweetv1.TweetServiceClient
	FollowClient    followv1.FollowServiceClient
	MessengerClient messengerv1.MessengerServiceClient

	userConn      *grpc.ClientConn
	tweetConn     *grpc.ClientConn
	followConn    *grpc.ClientConn
	messengerConn *grpc.ClientConn
}

func NewGRPCClients(consulAddr string) (*GRPCClients, error) {
	clients := &GRPCClients{}

	// 定义服务发现解析器 Scheme (consul://<consulAddr>/<serviceName>)
	// 使用 round_robin 负载均衡策略
	serviceConfig := `{"loadBalancingPolicy": "round_robin"}`

	// 🔍 添加 OpenTelemetry Client Interceptor (使用 StatsHandler 以获得更好支持)
	otelInterceptor := grpc.WithStatsHandler(otelgrpc.NewClientHandler())

	// 1. 连接 User Service
	userTarget := fmt.Sprintf("consul://%s/user-service?healthy=true", consulAddr)
	userConn, err := grpc.NewClient(userTarget,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(serviceConfig),
		otelInterceptor,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create user service client: %v", err)
	}
	clients.userConn = userConn
	// Wrap with Circuit Breaker
	originalUserClient := userv1.NewUserServiceClient(userConn)
	clients.UserClient = &ProtectedUserClient{UserServiceClient: originalUserClient}
	log.Printf("✅ Gateway connected to User Service info (Target: %s)", userTarget)

	// 2. 连接 Tweet Service
	tweetTarget := fmt.Sprintf("consul://%s/tweet-service?healthy=true", consulAddr)
	tweetConn, err := grpc.NewClient(tweetTarget,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(serviceConfig),
		otelInterceptor,
	)
	if err != nil {
		userConn.Close()
		return nil, fmt.Errorf("failed to create tweet service client: %v", err)
	}
	clients.tweetConn = tweetConn
	// Wrap with Circuit Breaker
	originalTweetClient := tweetv1.NewTweetServiceClient(tweetConn)
	clients.TweetClient = &ProtectedTweetClient{TweetServiceClient: originalTweetClient}
	log.Printf("✅ Gateway connected to Tweet Service info (Target: %s)", tweetTarget)

	// 3. 连接 Follow Service
	followTarget := fmt.Sprintf("consul://%s/follow-service?healthy=true", consulAddr)
	followConn, err := grpc.NewClient(followTarget,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(serviceConfig),
		otelInterceptor,
	)
	if err != nil {
		userConn.Close()
		tweetConn.Close()
		return nil, fmt.Errorf("failed to create follow service client: %v", err)
	}
	clients.followConn = followConn
	clients.FollowClient = followv1.NewFollowServiceClient(followConn)
	log.Printf("✅ Gateway connected to Follow Service info (Target: %s)", followTarget)

	// 4. 连接 Messenger Service
	messengerTarget := fmt.Sprintf("consul://%s/messenger-service?healthy=true", consulAddr)
	messengerConn, err := grpc.NewClient(messengerTarget,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(serviceConfig),
		otelInterceptor,
	)
	if err != nil {
		userConn.Close()
		tweetConn.Close()
		followConn.Close()
		return nil, fmt.Errorf("failed to create messenger service client: %v", err)
	}
	clients.messengerConn = messengerConn
	clients.MessengerClient = messengerv1.NewMessengerServiceClient(messengerConn)
	log.Printf("✅ Gateway connected to Messenger Service info (Target: %s)", messengerTarget)

	return clients, nil
}

func (c *GRPCClients) Close() {
	if c.userConn != nil {
		c.userConn.Close()
	}
	if c.tweetConn != nil {
		c.tweetConn.Close()
	}
	if c.followConn != nil {
		c.followConn.Close()
	}
	if c.messengerConn != nil {
		c.messengerConn.Close()
	}
}

// =============================================================================
// Protected Clients (Decorator Pattern)
// =============================================================================

// ProtectedTweetClient wraps TweetServiceClient with Sentinel
type ProtectedTweetClient struct {
	tweetv1.TweetServiceClient
}

// GetTweet overrides the default GetTweet with Circuit Breaking
func (c *ProtectedTweetClient) GetTweet(ctx context.Context, in *tweetv1.GetTweetRequest, opts ...grpc.CallOption) (*tweetv1.GetTweetResponse, error) {
	entry, blockError := sentinel.Entry("grpc:tweet-service")
	if blockError != nil {
		log.Printf("🔥 Circuit Breaker BLOCKED: grpc:tweet-service | Reason: %v", blockError)
		return nil, fmt.Errorf("service overloaded (Circuit Breaker Open)")
	}
	defer entry.Exit()

	resp, err := c.TweetServiceClient.GetTweet(ctx, in, opts...)

	if err != nil {
		sentinel.TraceError(entry, err)
	}
	return resp, err
}

// ProtectedUserClient wraps UserServiceClient with Sentinel
type ProtectedUserClient struct {
	userv1.UserServiceClient
}

// GetProfile overrides the default GetProfile with Circuit Breaking
func (c *ProtectedUserClient) GetProfile(ctx context.Context, in *userv1.GetProfileRequest, opts ...grpc.CallOption) (*userv1.GetProfileResponse, error) {
	entry, blockError := sentinel.Entry("grpc:user-service")
	if blockError != nil {
		log.Printf("🔥 Circuit Breaker BLOCKED: grpc:user-service | Reason: %v", blockError)
		return nil, fmt.Errorf("service overloaded (Circuit Breaker Open)")
	}
	defer entry.Exit()

	resp, err := c.UserServiceClient.GetProfile(ctx, in, opts...)

	if err != nil {
		sentinel.TraceError(entry, err)
	}
	return resp, err
}
