package client

import (
	"context"
	"fmt"
	"log"
	"os"

	_ "github.com/mbobakov/grpc-consul-resolver" // Import Consul Resolver
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	aiAgentv1 "twitter-clone/api/aiAgent/v1"
	followv1 "twitter-clone/api/follow/v1"
	messengerv1 "twitter-clone/api/messenger/v1"
	notificationv1 "twitter-clone/api/notification/v1"
	tweetv1 "twitter-clone/api/tweet/v1"
	userv1 "twitter-clone/api/user/v1"

	sentinel "github.com/alibaba/sentinel-golang/api"
)

type GRPCClients struct {
	UserClient         userv1.UserServiceClient
	TweetClient        tweetv1.TweetServiceClient
	FollowClient       followv1.FollowServiceClient
	MessengerClient    messengerv1.MessengerServiceClient
	AgentClient        aiAgentv1.AiAgentServiceClient
	NotificationClient notificationv1.NotificationServiceClient

	userConn         *grpc.ClientConn
	tweetConn        *grpc.ClientConn
	followConn       *grpc.ClientConn
	messengerConn    *grpc.ClientConn
	agentConn        *grpc.ClientConn
	notificationConn *grpc.ClientConn
}

func getServiceTarget(consulAddr, serviceName, envKey, defaultAddr string) string {
	if os.Getenv("USE_K8S_DNS") == "true" {
		addr := os.Getenv(envKey)
		if addr == "" {
			addr = defaultAddr
		}
		return fmt.Sprintf("dns:///%s", addr)
	}
	return fmt.Sprintf("consul://%s/%s?healthy=true", consulAddr, serviceName)
}

func NewGRPCClients(consulAddr string) (*GRPCClients, error) {
	clients := &GRPCClients{}

	// 定义服务发现解析器 Scheme
	// 使用 round_robin 负载均衡策略
	serviceConfig := `{"loadBalancingPolicy": "round_robin"}`

	// 🔍 添加 OpenTelemetry Client Interceptor (使用 StatsHandler 以获得更好支持)
	otelInterceptor := grpc.WithStatsHandler(otelgrpc.NewClientHandler())

	// 1. 连接 User Service
	userTarget := getServiceTarget(consulAddr, "user-service", "USER_SERVICE_ADDR", "twitter-clone-user:9091")
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
	tweetTarget := getServiceTarget(consulAddr, "tweet-service", "TWEET_SERVICE_ADDR", "twitter-clone-tweet:9092")
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
	followTarget := getServiceTarget(consulAddr, "follow-service", "FOLLOW_SERVICE_ADDR", "twitter-clone-follow:9093")
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
	messengerTarget := getServiceTarget(consulAddr, "messenger-service", "MESSENGER_SERVICE_ADDR", "twitter-clone-messenger:9094")
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

	// 5. 连接 Agent Service
	agentTarget := getServiceTarget(consulAddr, "agent-service", "AGENT_SERVICE_ADDR", "twitter-clone-agent:9095")
	agentConn, err := grpc.NewClient(agentTarget,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(serviceConfig),
		otelInterceptor,
	)
	if err != nil {
		userConn.Close()
		tweetConn.Close()
		followConn.Close()
		messengerConn.Close()
		return nil, fmt.Errorf("failed to create agent service client: %v", err)
	}
	clients.agentConn = agentConn
	clients.AgentClient = aiAgentv1.NewAiAgentServiceClient(agentConn)
	log.Printf("✅ Gateway connected to Agent Service (Target: %s)", agentTarget)

	// 6. 连接 Notification Service
	notificationTarget := getServiceTarget(consulAddr, "notification-service", "NOTIFICATION_SERVICE_ADDR", "twitter-clone-notification:9096")
	notificationConn, err := grpc.NewClient(notificationTarget,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(serviceConfig),
		otelInterceptor,
	)
	if err != nil {
		userConn.Close()
		tweetConn.Close()
		followConn.Close()
		messengerConn.Close()
		agentConn.Close()
		return nil, fmt.Errorf("failed to create notification service client: %v", err)
	}
	clients.notificationConn = notificationConn
	clients.NotificationClient = notificationv1.NewNotificationServiceClient(notificationConn)
	log.Printf("✅ Gateway connected to Notification Service (Target: %s)", notificationTarget)

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
	if c.agentConn != nil {
		c.agentConn.Close()
	}
	if c.notificationConn != nil {
		c.notificationConn.Close()
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
