package es

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/elastic/go-elasticsearch/v8"
	"twitter-clone/pkg/logger"
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

var defaultClient *Client

// Init 初始化全局 ElasticSearch 客户端
func Init() error {
	addressesStr := os.Getenv("ES_ADDRESSES")
	if addressesStr == "" {
		addressesStr = "http://localhost:9200" // 默认本地单机
	}

	cfg := Config{
		Addresses: strings.Split(addressesStr, ","),
		Username:  os.Getenv("ES_USERNAME"),
		Password:  os.Getenv("ES_PASSWORD"),
		CloudID:   os.Getenv("ES_CLOUD_ID"),
		APIKey:    os.Getenv("ES_API_KEY"),
	}

	client, err := NewClient(cfg)
	if err != nil {
		return err
	}

	defaultClient = client
	logger.Info("ElasticSearch client initialized successfully", zap.Strings("addresses", cfg.Addresses))
	return nil
}

// GetClient 获取全局 ElasticSearch 客户端
func GetClient() *Client {
	if defaultClient == nil {
		logger.Fatal("ElasticSearch client not initialized. Please call es.Init() first")
	}
	return defaultClient
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
	
	if !pingResp.IsSuccess() {
		return nil, fmt.Errorf("elasticsearch ping failed, status: %d", pingResp.StatusCode)
	}

	return &Client{TypedClient: client}, nil
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
		logger.Info("ElasticSearch index already exists", zap.String("index", indexName))
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
	
	if !resp.IsSuccess() {
		return fmt.Errorf("create index failed with status: %d", resp.StatusCode)
	}
	
	logger.Info("ElasticSearch index created", zap.String("index", indexName))
	return nil
}

// DeleteIndex 删除索引
func (c *Client) DeleteIndex(ctx context.Context, indexName string) error {
	resp, err := c.Indices.Delete(indexName).Do(ctx)
	if err != nil {
		return err
	}
	if !resp.IsSuccess() {
		return fmt.Errorf("delete index failed with status: %d", resp.StatusCode)
	}
	return nil
}
