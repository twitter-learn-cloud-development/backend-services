package service

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
	"go.temporal.io/sdk/activity"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"

	tweetv1 "twitter-clone/api/tweet/v1"
	"twitter-clone/pkg/ai"
	"twitter-clone/pkg/es"
	"twitter-clone/pkg/qdrant"
)

type tweetActivityClient interface {
	CreateTweet(context.Context, *tweetv1.CreateTweetRequest, ...grpc.CallOption) (*tweetv1.CreateTweetResponse, error)
	GetAuthorPostingStats(context.Context, *tweetv1.GetAuthorPostingStatsRequest, ...grpc.CallOption) (*tweetv1.GetAuthorPostingStatsResponse, error)
	ApplyTweetModeration(context.Context, *tweetv1.ApplyTweetModerationRequest, ...grpc.CallOption) (*tweetv1.ApplyTweetModerationResponse, error)
}

type AgentActivities struct {
	redisClient      *redis.Client
	esClient         *es.Client
	qdrantClient     *qdrant.Client
	aiClient         *ai.Client
	tweetClient      tweetActivityClient
	embeddingModel   string
	chatModelCheap   string
	chatModelPremium string
	botUserID        uint64
}

func NewAgentActivities(
	redisClient *redis.Client,
	esClient *es.Client,
	qdrantClient *qdrant.Client,
	aiClient *ai.Client,
	tweetClient tweetActivityClient,
	embeddingModel string,
	chatModelCheap string,
	chatModelPremium string,
	botUserID uint64,
) *AgentActivities {
	return &AgentActivities{
		redisClient:      redisClient,
		esClient:         esClient,
		qdrantClient:     qdrantClient,
		aiClient:         aiClient,
		tweetClient:      tweetClient,
		embeddingModel:   embeddingModel,
		chatModelCheap:   chatModelCheap,
		chatModelPremium: chatModelPremium,
		botUserID:        botUserID,
	}
}

// CheckSpamFrequencyActivity 频率风控检测
func (a *AgentActivities) CheckSpamFrequencyActivity(ctx context.Context, authorID uint64) (bool, error) {
	stats, err := a.tweetClient.GetAuthorPostingStats(ctx, &tweetv1.GetAuthorPostingStatsRequest{
		AuthorId:        authorID,
		LookbackSeconds: int64((10 * time.Minute) / time.Second),
	})
	if err != nil {
		return false, fmt.Errorf("failed to get author posting stats: %w", err)
	}
	if stats == nil {
		return false, fmt.Errorf("failed to get author posting stats: empty response")
	}
	if stats.SampleCount < 2 {
		return false, nil
	}
	interval := stats.LatestCreatedAt - stats.PreviousCreatedAt
	if interval < 0 || interval >= 5000 {
		return false, nil
	}
	return true, nil
}

// QdrantSearchSimilarityActivity 语义相似度风控检测
func (a *AgentActivities) QdrantSearchSimilarityActivity(ctx context.Context, content string, authorID uint64) (bool, error) {
	if a.qdrantClient == nil {
		return false, nil
	}

	embedding, err := a.aiClient.GetEmbedding(ctx, content, a.embeddingModel)
	if err != nil {
		return false, fmt.Errorf("failed to generate embedding: %w", err)
	}

	results, err := a.qdrantClient.Search(ctx, "tweets", embedding, 10)
	if err != nil {
		return false, fmt.Errorf("failed to search Qdrant: %w", err)
	}

	for _, res := range results {
		authorIDStr, _ := res.Payload["user_id"].(string)
		if authorIDStr == fmt.Sprintf("%d", authorID) {
			if res.Score >= 0.85 {
				log.Printf("🔍 Similarity match found: score=%.4f (threshold=0.85) with historical tweet_id=%s", res.Score, res.ID)
				return true, nil
			}
		}
	}

	return false, nil
}

// ExecuteShadowbanActivity 影子封禁并对粉丝的 Timeline 进行原子清洗
func (a *AgentActivities) ExecuteShadowbanActivity(ctx context.Context, tweetID uint64, authorID uint64) error {
	result, err := a.tweetClient.ApplyTweetModeration(ctx, &tweetv1.ApplyTweetModerationRequest{
		TweetId:    tweetID,
		AuthorId:   authorID,
		Action:     tweetv1.TweetModerationAction_TWEET_MODERATION_ACTION_SHADOWBAN,
		ReasonCode: "agent_risk_control",
	})
	if err != nil {
		return fmt.Errorf("failed to apply tweet moderation: %w", err)
	}
	if result == nil {
		return fmt.Errorf("failed to apply tweet moderation: empty response")
	}
	log.Printf(
		"Tweet moderation accepted: tweet_id=%d applied=%t cleanup_queued=%t timelines_cleaned=%d",
		tweetID,
		result.Applied,
		result.CleanupQueued,
		result.TimelinesCleaned,
	)
	return nil
}

// GetHottestTopicActivity 从 Redis 获取最高热度的话题
func (a *AgentActivities) GetHottestTopicActivity(ctx context.Context) (string, error) {
	res, err := a.redisClient.ZRevRangeWithScores(ctx, "trends:global", 0, 0).Result()
	if err != nil {
		return "", fmt.Errorf("failed to fetch trending topics from Redis: %w", err)
	}
	if len(res) == 0 {
		return "", nil
	}
	return res[0].Member.(string), nil
}

// ParallelRetrieveActivity 并行检索 ES 和 Qdrant 中该话题相关的推文文本
func (a *AgentActivities) ParallelRetrieveActivity(ctx context.Context, topic string) ([]string, error) {
	if a.esClient == nil && a.qdrantClient == nil {
		return []string{"AIGC is the future", "Artificial intelligence is booming"}, nil
	}

	var qdrantResults []string
	var esResults []string

	retrieveCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	g, gCtx := errgroup.WithContext(retrieveCtx)

	// Qdrant 向量路
	g.Go(func() error {
		if a.qdrantClient == nil {
			return nil
		}
		embedding, err := a.aiClient.GetEmbedding(gCtx, topic, a.embeddingModel)
		if err != nil {
			log.Printf("⚠️ Qdrant pathway failed to get embedding: %v", err)
			return err
		}

		results, err := a.qdrantClient.Search(gCtx, "tweets", embedding, 10)
		if err != nil {
			log.Printf("⚠️ Qdrant pathway search failed: %v", err)
			return err
		}

		for _, res := range results {
			if content, ok := res.Payload["content"].(string); ok {
				content = strings.TrimPrefix(content, "Document: ")
				qdrantResults = append(qdrantResults, content)
			}
		}
		return nil
	})

	// ES 关键词路
	g.Go(func() error {
		if a.esClient == nil {
			return nil
		}
		docs, err := a.esClient.SearchTweets(gCtx, topic, 1, 10)
		if err != nil {
			log.Printf("⚠️ ES pathway search failed: %v", err)
			return err
		}
		for _, doc := range docs {
			esResults = append(esResults, doc.Content)
		}
		return nil
	})

	if err := g.Wait(); err != nil {
		return nil, err
	}

	unique := make(map[string]bool)
	var merged []string

	for _, text := range qdrantResults {
		cleaned := strings.TrimSpace(text)
		if cleaned != "" && !unique[cleaned] {
			unique[cleaned] = true
			merged = append(merged, cleaned)
		}
	}
	for _, text := range esResults {
		cleaned := strings.TrimSpace(text)
		if cleaned != "" && !unique[cleaned] {
			unique[cleaned] = true
			merged = append(merged, cleaned)
		}
	}

	finalResults := make([]string, len(merged))
	copy(finalResults, merged)

	return finalResults, nil
}

// GenerateSummaryActivity 舆情总结，带开销降级路由与心跳汇报
func (a *AgentActivities) GenerateSummaryActivity(ctx context.Context, topic string, tweets []string) (string, error) {
	systemPrompt := "你是一个舆情追踪播报姬，负责撰写本平台的每日热点速递。"

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("请针对热点话题“#%s”撰写一篇大约 200 字的热点快报评论，语言需要活泼、幽默，符合社交媒体特点。请基于以下平台用户推文内容，提炼主要观点，禁止凭空捏造事实。必须携带“#%s”标签。\n\n参考推文：\n", topic, topic))

	maxItems := 15
	if len(tweets) < maxItems {
		maxItems = len(tweets)
	}

	for i := 0; i < maxItems; i++ {
		sb.WriteString(fmt.Sprintf("- %s\n", tweets[i]))
	}

	// 注入 Temporal 心跳上报逻辑
	// 顶级写法：利用高阶函数做限流，每隔 2 秒或累积一定内容才上报一次
	lastHeartbeat := time.Now()
	onProgress := func(chunk string) {
		if time.Since(lastHeartbeat) > 2*time.Second {
			activity.RecordHeartbeat(ctx, chunk)
			lastHeartbeat = time.Now()
		}
	}

	summaryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	summary, err := a.aiClient.GetChatCompletionWithRouting(
		summaryCtx,
		systemPrompt,
		sb.String(),
		a.chatModelCheap,
		a.chatModelPremium,
		"Medium",
		onProgress,
	)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(summary), nil
}

// PublishTweetActivity 发布生成的快报
func (a *AgentActivities) PublishTweetActivity(ctx context.Context, summary string) (uint64, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var idempotencyKey string
	if activity.IsActivity(ctx) {
		info := activity.GetInfo(ctx)
		// 结合 Workflow ID 和 Activity ID 作为唯一的幂等键
		idempotencyKey = fmt.Sprintf("idempotency:publish_tweet:%s:%s", info.WorkflowExecution.ID, info.ActivityID)
	}

	// 1. 如果有幂等键，先尝试从 Redis 中获取已发布的 Tweet ID
	if idempotencyKey != "" && a.redisClient != nil {
		cachedTweetIDStr, err := a.redisClient.Get(reqCtx, idempotencyKey).Result()
		if err == nil && cachedTweetIDStr != "" {
			var cachedTweetID uint64
			if _, errScan := fmt.Sscanf(cachedTweetIDStr, "%d", &cachedTweetID); errScan == nil {
				log.Printf("ℹ️ Idempotency hit: Tweet already published previously. Key: %s, Returned TweetID: %d",
					idempotencyKey, cachedTweetID)
				return cachedTweetID, nil
			}
		}
	}

	createReq := &tweetv1.CreateTweetRequest{
		UserId:         a.botUserID,
		Content:        summary,
		IdempotencyKey: idempotencyKey,
	}

	resp, err := a.tweetClient.CreateTweet(reqCtx, createReq)
	if err != nil {
		return 0, fmt.Errorf("failed to publish trending tweet via tweetClient: %w", err)
	}

	// 2. 发布成功后，将生成的 TweetID 写入 Redis 缓存（缓存 24 小时以防重试）
	if idempotencyKey != "" && a.redisClient != nil {
		errSet := a.redisClient.Set(reqCtx, idempotencyKey, fmt.Sprintf("%d", resp.Tweet.Id), 24*time.Hour).Err()
		if errSet != nil {
			log.Printf("⚠️ Failed to cache idempotency key in Redis: %v", errSet)
		} else {
			log.Printf("💾 Idempotency key cached in Redis: %s -> %d", idempotencyKey, resp.Tweet.Id)
		}
	}

	log.Printf("✅ Trending report published successfully via Activity! TweetID: %d", resp.Tweet.Id)
	return resp.Tweet.Id, nil
}
