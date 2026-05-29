package service

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
	"golang.org/x/sync/errgroup"

	tweetv1 "twitter-clone/api/tweet/v1"
	"twitter-clone/pkg/ai"
	"twitter-clone/pkg/es"
	"twitter-clone/pkg/qdrant"
)

type TrendingReporter struct {
	redisClient    *redis.Client
	esClient       *es.Client
	qdrantClient   *qdrant.Client
	aiClient       *ai.Client
	tweetClient    tweetv1.TweetServiceClient
	embeddingModel string
	chatModel      string
	botUserID      uint64
}

func NewTrendingReporter(
	redisClient *redis.Client,
	esClient *es.Client,
	qdrantClient *qdrant.Client,
	aiClient *ai.Client,
	tweetClient tweetv1.TweetServiceClient,
	embeddingModel string,
	chatModel string,
	botUserID uint64,
) *TrendingReporter {
	return &TrendingReporter{
		redisClient:    redisClient,
		esClient:       esClient,
		qdrantClient:   qdrantClient,
		aiClient:       aiClient,
		tweetClient:    tweetClient,
		embeddingModel: embeddingModel,
		chatModel:      chatModel,
		botUserID:      botUserID,
	}
}

// Start 启动后台定时舆情监视哨兵
func (t *TrendingReporter) Start(ctx context.Context, interval time.Duration) {
	log.Printf("🤖 Trending reporter background task started with interval %v", interval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("⏹️ Stopping trending reporter...")
			return
		case <-ticker.C:
			t.reportTrending(ctx)
		}
	}
}

func (t *TrendingReporter) reportTrending(ctx context.Context) {
	// 1. 抢占分布式排他锁，防止多实例重复生成发帖
	lockKey := "lock:trending_reporter"
	locked, err := t.redisClient.SetNX(ctx, lockKey, "locked", 30*time.Second).Result()
	if err != nil {
		log.Printf("⚠️ Redis error during SetNX for lock:trending_reporter: %v", err)
		return
	}
	if !locked {
		// 未能抢到分布式锁，静默退出
		return
	}
	log.Println("🔒 Grabbed lock:trending_reporter, executing hot topic tracking...")

	defer func() {
		t.redisClient.Del(ctx, lockKey)
	}()

	// 2. 从 Redis ZSet 读取最高热度的话题
	res, err := t.redisClient.ZRevRangeWithScores(ctx, "trends:global", 0, 0).Result()
	if err != nil {
		log.Printf("❌ Failed to fetch trending topics: %v", err)
		return
	}
	if len(res) == 0 {
		log.Println("ℹ️ No trending topics found in Redis trends:global")
		return
	}

	trendingTopic := res[0].Member.(string)
	log.Printf("🔥 Current hottest topic detected: #%s (score: %.1f)", trendingTopic, res[0].Score)

	// 3. 并行双路召回：从 ES 和 Qdrant 中并行检索该话题下的推文文本
	retrievedTweets, err := t.parallelRetrieve(ctx, trendingTopic)
	if err != nil {
		log.Printf("❌ Failed to parallel retrieve: %v", err)
		return
	}
	if len(retrievedTweets) == 0 {
		log.Printf("ℹ️ No relevant tweets found for topic #%s. Skip reporting.", trendingTopic)
		return
	}

	// 4. 调用大模型生成摘要
	summary, err := t.generateSummary(ctx, trendingTopic, retrievedTweets)
	if err != nil {
		log.Printf("❌ Failed to generate summary: %v", err)
		return
	}

	log.Printf("📝 Generated summary for #%s: %s", trendingTopic, summary)

	// 5. 官方发帖助理调用 tweet-service 接口发帖
	reqCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	createReq := &tweetv1.CreateTweetRequest{
		UserId:  t.botUserID, // 官方播报姬 UserID (e.g. 100)
		Content: summary,
	}

	resp, err := t.tweetClient.CreateTweet(reqCtx, createReq)
	if err != nil {
		log.Printf("❌ Failed to publish trending tweet: %v", err)
		return
	}

	log.Printf("✅ Trending report published successfully! TweetID: %d", resp.Tweet.Id)
}

func (t *TrendingReporter) parallelRetrieve(ctx context.Context, topic string) ([]string, error) {
	if t.esClient == nil && t.qdrantClient == nil {
		return []string{"AIGC is the future", "Artificial intelligence is booming"}, nil
	}

	var qdrantResults []string
	var esResults []string

	// 使用 errgroup 保证并发安全与超时控制 (3s 超时)
	retrieveCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	g, gCtx := errgroup.WithContext(retrieveCtx)

	// Qdrant 向量路
	g.Go(func() error {
		if t.qdrantClient == nil {
			return nil
		}
		embedding, err := t.aiClient.GetEmbedding(gCtx, topic, t.embeddingModel)
		if err != nil {
			log.Printf("⚠️ Qdrant pathway failed to get embedding: %v", err)
			return nil // 降级：局部失败不阻断全局
		}

		results, err := t.qdrantClient.Search(gCtx, "tweets", embedding, 10)
		if err != nil {
			log.Printf("⚠️ Qdrant pathway search failed: %v", err)
			return nil
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
		if t.esClient == nil {
			return nil
		}
		docs, err := t.esClient.SearchTweets(gCtx, topic, 1, 10)
		if err != nil {
			log.Printf("⚠️ ES pathway search failed: %v", err)
			return nil
		}
		for _, doc := range docs {
			esResults = append(esResults, doc.Content)
		}
		return nil
	})

	if err := g.Wait(); err != nil {
		return nil, err
	}

	// 融合去重与深拷贝
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

	// 🆕 显式重新声明以确保隔离内存，防范潜在的 slice 底层数组竞态
	finalResults := make([]string, len(merged))
	copy(finalResults, merged)

	return finalResults, nil
}

func (t *TrendingReporter) generateSummary(ctx context.Context, topic string, tweets []string) (string, error) {
	systemPrompt := "你是一个舆情追踪播报姬，负责撰写本平台的每日热点速递。"

	// 拼接输入推文列表并进行剪枝 (控制上下文长度)
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("请针对热点话题“#%s”撰写一篇大约 200 字的热点快报评论，语言需要活泼、幽默，符合社交媒体特点。请基于以下平台用户推文内容，提炼主要观点，禁止凭空捏造事实。必须携带“#%s”标签。\n\n参考推文：\n", topic, topic))

	maxItems := 15
	if len(tweets) < maxItems {
		maxItems = len(tweets)
	}

	for i := 0; i < maxItems; i++ {
		sb.WriteString(fmt.Sprintf("- %s\n", tweets[i]))
	}

	summaryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	summary, err := t.aiClient.GetChatCompletion(summaryCtx, systemPrompt, sb.String(), t.chatModel)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(summary), nil
}
