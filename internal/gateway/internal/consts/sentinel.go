package consts

const (
	ResourceUserService         = "grpc:user-service"
	ResourceTweetService        = "grpc:tweet-service"
	ResourceFollowService       = "grpc:follow-service"
	ResourceNotificationService = "grpc:notification-service"
	ResourceBookmarkService     = "grpc:bookmark-service"
	ResourceMessengerService    = "grpc:messenger-service"
	ResourceAgentService        = "grpc:agent-service"
)

const (
	AppName          = "gateway"
	DashboardAddress = "sentinel:9124"
	TransportPort    = 8719
	LogDir           = "/tmp/sentinel/logs"
)

// 🐦 推文服务 (Tweet Service) - 对应你的 Rule 1
const (
	TweetRetryTimeoutMs   = 3000
	TweetMinRequestAmount = 10
	TweetStatIntervalMs   = 1000
	TweetThreshold        = 0.5
)

// 👤 用户服务 (User Service) - 对应你的 Rule 2 (慢请求策略)
const (
	UserRetryTimeoutMs   = 3000
	UserMinRequestAmount = 10
	UserStatIntervalMs   = 1000
	UserMaxAllowedRtMs   = 500
	UserThreshold        = 0.5
)

// 👥 关注服务 (Follow Service) - 图关系高频读写
const (
	FollowRetryTimeoutMs   = 3000
	FollowMinRequestAmount = 15 // 稍微提高门槛，过滤突发抖动
	FollowStatIntervalMs   = 1000
	FollowThreshold        = 0.4 // 错误率达到 40% 熔断
)

// 🔔 通知服务 (Notification Service) - 边缘异步服务，配置最宽松
const (
	NotificationRetryTimeoutMs   = 2000 // 恢复快，缩短重试等待
	NotificationMinRequestAmount = 5    // 流量较小，降低触发门槛
	NotificationStatIntervalMs   = 2000 // 统计窗口拉长到 2s
	NotificationThreshold        = 0.7  // 容忍度高，错误率到 70% 再断开
)

// 🔖 书签服务 (Bookmark Service) - 缓存率高的只读型服务
const (
	BookmarkRetryTimeoutMs   = 3000
	BookmarkMinRequestAmount = 10
	BookmarkStatIntervalMs   = 1000
	BookmarkThreshold        = 0.4
)

// 💬 聊天信使服务 (Messenger Service) - 实时长连接，对延迟敏感（建议走慢请求熔断）
const (
	MessengerRetryTimeoutMs   = 4000 // 给长连接断开重连留足时间
	MessengerMinRequestAmount = 10
	MessengerStatIntervalMs   = 1000
	MessengerMaxAllowedRtMs   = 200 // 实时聊天超过 200ms 就算慢请求
	MessengerThreshold        = 0.3 // 超过 30% 的请求慢了就果断熔断
)

// 🤖 AI 智能体服务 (Agent Service) - 计算密集型大模型服务，RT 极长
const (
	AgentRetryTimeoutMs   = 10000 // AI 恢复慢，等待 10s 再重试
	AgentMinRequestAmount = 5     // 吞吐低，5次请求开始计算
	AgentStatIntervalMs   = 5000  // 耗时长，统计窗口拉长到 5s
	AgentMaxAllowedRtMs   = 5000  // 大模型响应长，5000ms (5秒) 内都算正常
	AgentThreshold        = 0.5
)
