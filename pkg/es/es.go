package es

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"twitter-clone/internal/domain"
	"twitter-clone/pkg/logger"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types"
	"go.uber.org/zap"
)

// Client 包装了原生的 elasticsearch client
type Client struct {
	*elasticsearch.TypedClient // 使用 v8 推荐的 TypedClient (强类型API)
}

// Config ElasticSearch 配置
type Config struct {
	Addresses []string
	Username  string
	Password  string
	CloudID   string
	APIKey    string
}

const TweetIndex = "tweets"

type TweetDocument struct {
	ID            string    `json:"id"` // uint64 转 string，ES 的 keyword 类型
	UserID        string    `json:"user_id"`
	ParentID      string    `json:"parent_id"`      // 区分推文和回复
	Content       string    `json:"content"`        // 全文检索核心字段
	ContentVector []float32 `json:"content_vector"` // 向量表示，用于相似度计算
	Type          int       `json:"type"`           // 0文本 1图片 2视频，用于过滤
	VisibleType   int       `json:"visible_type"`   // 0公开 1粉丝 2私密，搜索时过滤私密内容
	CreatedAt     int64     `json:"created_at"`     // 用于时间排序
	LikeCount     int       `json:"like_count"`     // 用于热度排序
	DeletedAt     int64     `json:"deleted_at"`     // 软删除过滤，deleted_at=0 才展示
}

const tweetMapping = `{
    "mappings": {
        "properties": {
            "id":           { "type": "keyword" },
            "user_id":      { "type": "keyword" },
            "parent_id":    { "type": "keyword" },
            "content": {
                "type":            "text",
                "analyzer":        "ik_max_word",
                "search_analyzer": "ik_smart"
            },
            "content_vector": {
                "type": "dense_vector",
                "dims": 1024,
                "index": true,
                "similarity": "cosine"
            },
            "type":         { "type": "integer" },
            "visible_type": { "type": "integer" },
            "created_at":   { "type": "long" },
            "like_count":   { "type": "integer" },
            "deleted_at":   { "type": "long" }
        }
    }
}`

var defaultClient *Client

// Init 初始化全局 ElasticSearch 客户端
func Init() error {
	addressesStr := GetEnv("ES_ADDRESSES", "http://localhost:9200")

	log.Printf("ES_ADDRESSES: %s", addressesStr)

	cfg := Config{
		Addresses: strings.Split(addressesStr, ","), // 支持多个地址，逗号分隔
		Username:  GetEnv("ES_USERNAME", ""),
		Password:  GetEnv("ES_PASSWORD", ""),
		CloudID:   GetEnv("ES_CLOUD_ID", ""),
		APIKey:    GetEnv("ES_API_KEY", ""),
	}

	client, err := NewClient(cfg)
	if err != nil {
		return err
	}

	defaultClient = client
	logger.Info(context.Background(), "ElasticSearch client initialized successfully", zap.Strings("addresses", cfg.Addresses))
	return nil
}

// GetClient 获取全局 ElasticSearch 客户端
func GetClient() *Client {
	if defaultClient == nil {
		logger.Fatal(context.Background(), "ElasticSearch client not initialized. Please call es.Init() first")
	}
	return defaultClient
}

// GetEnv 获取环境变量值，如果不存在则返回默认值
func GetEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

// NewClient 创建一个新的 ElasticSearch 客户端实例
func NewClient(cfg Config) (*Client, error) {
	esCfg := elasticsearch.Config{
		Addresses: cfg.Addresses,
		Username:  cfg.Username,
		Password:  cfg.Password,
		CloudID:   cfg.CloudID,
		APIKey:    cfg.APIKey,

		// 根据需要配置重试机制和传输层
		RetryOnStatus: []int{502, 503, 504, 429},
		MaxRetries:    3,
		Transport: &http.Transport{
			MaxIdleConnsPerHost:   10,
			ResponseHeaderTimeout: time.Second * 5,
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, // 开发环境可跳过证书验证
			},
		},
	}

	// 初始化 v8 的 TypedClient，提供更友好的 Go Struct 风格的 API
	client, err := elasticsearch.NewTypedClient(esCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create elasticsearch client: %w", err)
	}

	// Ping 探活测试连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pingResp, err := client.Ping().Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to ping elasticsearch: %w", err)
	}

	if !pingResp {
		return nil, fmt.Errorf("elasticsearch ping failed, status: %t", pingResp)
	}

	return &Client{TypedClient: client}, nil
}

func FromTweet(tweet *domain.Tweet, contentVector []float32) TweetDocument {
	return TweetDocument{
		ID:            fmt.Sprintf("%d", tweet.ID),
		UserID:        fmt.Sprintf("%d", tweet.UserID),
		ParentID:      fmt.Sprintf("%d", tweet.ParentID),
		Content:       tweet.Content,
		ContentVector: contentVector,
		Type:          tweet.Type,
		VisibleType:   tweet.VisibleType,
		CreatedAt:     tweet.CreatedAt,
		LikeCount:     tweet.LikeCount,
		DeletedAt:     tweet.DeletedAt,
	}
}

// 创建索引
func (c *Client) CreateTweetIndex(ctx context.Context) error {
	return c.CreateIndexIfNotExists(ctx, TweetIndex, tweetMapping)
}

// CreateIndexIfNotExists 如果索引不存在则创建
// mappings 参数应为 JSON 字符串，例如：`{"properties": {"content": {"type": "text", "analyzer": "ik_max_word"}}}`
func (c *Client) CreateIndexIfNotExists(ctx context.Context, indexName string, mappings string) error {
	// 检查是否存在
	exists, err := c.Indices.Exists(indexName).IsSuccess(ctx)
	if err != nil {
		return fmt.Errorf("check index exists err: %w", err)
	}

	if exists {
		logger.Info(ctx, "ElasticSearch index already exists", zap.String("index", indexName))
		return nil
	}

	// 创建索引
	req := c.Indices.Create(indexName)
	if mappings != "" {
		req = req.Raw(strings.NewReader(mappings))
	}

	resp, err := req.Do(ctx)
	if err != nil {
		return fmt.Errorf("create index err: %w", err)
	}

	//拦截集群业务层面的失败
	if !resp.Acknowledged {
		return fmt.Errorf("create index failed: request not acknowledged by the cluster")
	}

	logger.Info(ctx, "ElasticSearch index created", zap.String("index", indexName))
	return nil
}

// DeleteIndex 删除索引
func (c *Client) DeleteIndex(ctx context.Context, indexName string) error {
	resp, err := c.Indices.Delete(indexName).Do(ctx)
	if err != nil {
		return err
	}
	if !resp.Acknowledged {
		return fmt.Errorf("delete index failed: request not acknowledged by the cluster")
	}

	return nil
}

// IndexTweet写入推文
func (c *Client) IndexTweet(ctx context.Context, doc TweetDocument) error {
	_, err := c.Index(TweetIndex).Id(doc.ID).Document(doc).Do(ctx)
	if err != nil {
		return fmt.Errorf("index tweet failed: %w", err)
	}
	return nil
}

// DeleteTweet删除推文
func (c *Client) DeleteTweet(ctx context.Context, id string) error {
	_, err := c.Delete(TweetIndex, id).Do(ctx)
	if err != nil {
		return fmt.Errorf("delete tweet failed: %w", err)
	}
	return nil
}

// SearchTweets 搜索推文
func (c *Client) SearchTweets(ctx context.Context, keyword string, page, size int) ([]TweetDocument, error) {
	from := (page - 1) * size

	resp, err := c.Search().
		Index(TweetIndex).
		From(from).
		Size(size).
		Query(&types.Query{
			Match: map[string]types.MatchQuery{
				"content": {Query: keyword},
			},
		}).
		Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("search tweets failed: %w", err)
	}

	var tweets []TweetDocument
	for _, hit := range resp.Hits.Hits {
		var tweet TweetDocument
		if err := json.Unmarshal(hit.Source_, &tweet); err != nil {
			continue
		}
		tweets = append(tweets, tweet)
	}
	return tweets, nil
}

// SearchTweetsByVector 纯向量语义检索
func (c *Client) SearchTweetsByVector(ctx context.Context, queryVector []float32, size int) ([]TweetDocument, error) {
	// 声明局部变量，以便获取它们的内存地址指针
	k := size
	numCandidates := size * 10
	resp, err := c.Search().
		Index(TweetIndex).
		Knn(types.KnnSearch{ // 使用 types.KnnSearch 的值类型
			Field:         "content_vector", // 指定向量字段
			QueryVector:   queryVector,      // 传入从 Jina 模型获取的用户提问向量
			K:             &k,               // 最终返回的最相似文档数
			NumCandidates: &numCandidates,   // 候选队列大小，通常设置为 K 的 10-50 倍，越大越准但越慢
		}).
		Size(size).
		Do(ctx)

	if err != nil {
		return nil, fmt.Errorf("vector search failed: %w", err)
	}

	var tweets []TweetDocument
	for _, hit := range resp.Hits.Hits {
		var tweet TweetDocument
		if err := json.Unmarshal(hit.Source_, &tweet); err != nil {
			continue
		}
		tweets = append(tweets, tweet)
	}
	return tweets, nil
}

// HybridSearchTweets 混合检索：同时基于关键词和语义向量
func (c *Client) HybridSearchTweets(ctx context.Context, keyword string, queryVector []float32, size int) ([]TweetDocument, error) {
	// 声明局部变量，以便获取它们的内存地址指针
	k := size
	numCandidates := size * 10
	resp, err := c.Search().
		Index(TweetIndex).
		// 1. 向量语义检索部分
		Knn(types.KnnSearch{
			Field:         "content_vector",
			QueryVector:   queryVector,
			K:             &k,
			NumCandidates: &numCandidates,
			Boost:         Float32Ptr(0.6), // 权重配置：给语义相似度 0.6 的权重
		}).
		// 2. 传统文本检索部分
		Query(&types.Query{
			Match: map[string]types.MatchQuery{
				"content": {
					Query: keyword,
					Boost: Float32Ptr(0.4), // 权重配置：给关键词匹配 0.4 的权重
				},
			},
		}).
		Size(size).
		Do(ctx)

	if err != nil {
		return nil, fmt.Errorf("hybrid search failed: %w", err)
	}

	var tweets []TweetDocument
	for _, hit := range resp.Hits.Hits {
		var tweet TweetDocument
		if err := json.Unmarshal(hit.Source_, &tweet); err != nil {
			continue
		}
		tweets = append(tweets, tweet)
	}
	return tweets, nil
}

// Float32Ptr 辅助函数，用于将 float32 转换为指针，供 typedAPI 使用
func Float32Ptr(v float32) *float32 {
	return &v
}
