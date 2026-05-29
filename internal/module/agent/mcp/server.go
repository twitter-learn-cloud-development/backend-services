package mcp

import (
	"fmt"
	"strings"

	tweetv1 "twitter-clone/api/tweet/v1"
	userv1 "twitter-clone/api/user/v1"
	"twitter-clone/internal/module/agent/mcp/tools"
	"twitter-clone/pkg/ai"
	"twitter-clone/pkg/es"
	"twitter-clone/pkg/qdrant"

	"github.com/mark3labs/mcp-go/server"
)

type MCPServer struct {
	esClient     *es.Client
	qdrantClient *qdrant.Client
	aiClient     *ai.Client
	reranker     ai.Reranker
	tweetClient  tweetv1.TweetServiceClient
	userClient   userv1.UserServiceClient
	model        string
}

func NewMCPServer(esClient *es.Client, qdrantClient *qdrant.Client, aiClient *ai.Client, reranker ai.Reranker, tweetClient tweetv1.TweetServiceClient, userClient userv1.UserServiceClient, model string) *MCPServer {
	return &MCPServer{
		esClient:     esClient,
		qdrantClient: qdrantClient,
		aiClient:     aiClient,
		reranker:     reranker,
		tweetClient:  tweetClient,
		userClient:   userClient,
		model:        model,
	}
}

func (s *MCPServer) Start(addr string) error {
	srv := server.NewMCPServer(
		"twitter-agent-mcp",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	// 注册所有 Tools
	tools.RegisterSearchTweets(srv, s.aiClient, s.esClient, s.qdrantClient, s.reranker, s.tweetClient, s.model)
	tools.RegisterHybridSearchTweets(srv, s.aiClient, s.esClient, s.qdrantClient, s.reranker, s.tweetClient, s.model)
	tools.RegisterCreateTweet(srv, s.tweetClient)
	tools.RegisterGetUserTweets(srv, s.tweetClient)
	tools.RegisterGetTweetsByIds(srv, s.tweetClient)
	tools.RegisterSearchUsers(srv, s.userClient)

	// 启动 HTTP SSE 模式
	baseURL := addr
	if strings.HasPrefix(baseURL, "0.0.0.0:") {
		baseURL = "127.0.0.1:" + strings.TrimPrefix(baseURL, "0.0.0.0:")
	}
	httpServer := server.NewSSEServer(srv, server.WithBaseURL(fmt.Sprintf("http://%s", baseURL)))
	if err := httpServer.Start(addr); err != nil {
		return fmt.Errorf("mcp server start failed: %w", err)
	}
	return nil
}

