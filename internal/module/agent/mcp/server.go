package mcp

import (
	"context"
	"fmt"

	"strconv"
	"twitter-clone/pkg/ai"
	"twitter-clone/pkg/es"

	tweetv1 "twitter-clone/api/tweet/v1"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type MCPServer struct {
	esClient    *es.Client
	aiClient    *ai.Client
	tweetClient tweetv1.TweetServiceClient
	model       string
}

func NewMCPServer(esClient *es.Client, aiClient *ai.Client, tweetClient tweetv1.TweetServiceClient, model string) *MCPServer {
	return &MCPServer{
		esClient:    esClient,
		aiClient:    aiClient,
		tweetClient: tweetClient,
		model:       model,
	}
}

func (s *MCPServer) Start(addr string) error {
	srv := server.NewMCPServer(
		"twitter-agent-mcp",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	// 注册所有 Tools
	s.registerSearchTweets(srv)
	s.registerHybridSearchTweets(srv)
	s.registerCreateTweet(srv)

	// 启动 HTTP SSE 模式
	httpServer := server.NewSSEServer(srv, server.WithBaseURL(fmt.Sprintf("http://%s", addr)))
	if err := httpServer.Start(addr); err != nil {
		return fmt.Errorf("mcp server start failed: %w", err)
	}
	return nil
}

// registerSearchTweets 注册语义搜索推文工具
func (s *MCPServer) registerSearchTweets(srv *server.MCPServer) {
	tool := mcp.NewTool("search_tweets_by_semantic",
		mcp.WithDescription("根据用户输入的语义描述，搜索最相关的推文列表"),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("用户的搜索描述，例如：最近很火的健身博主、关于 AI 的推文"),
		),
		mcp.WithNumber("size",
			mcp.Description("返回结果数量，默认 5，最大 20"),
		),
	)

	srv.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := req.Params.Arguments.(map[string]any)
		if !ok {
			return mcp.NewToolResultError("参数解析失败"), nil
		}

		query, ok := args["query"].(string)
		if !ok || query == "" {
			return mcp.NewToolResultError("query 参数不能为空"), nil
		}

		size := 5
		if s, ok := args["size"].(float64); ok && s > 0 {
			size = int(s)
			if size > 20 {
				size = 20
			}
		}

		// 1. 把用户问题向量化
		vector, err := s.aiClient.GetEmbedding(ctx, query, s.model)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("embedding failed: %v", err)), nil
		}

		// 2. kNN 语义搜索
		tweets, err := s.esClient.SearchTweetsByVector(ctx, vector, size)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("search failed: %v", err)), nil
		}

		if len(tweets) == 0 {
			return mcp.NewToolResultText("没有找到相关推文"), nil
		}

		// 3. 格式化结果给 LLM
		result := fmt.Sprintf("找到 %d 条相关推文：\n\n", len(tweets))
		for i, t := range tweets {
			result += fmt.Sprintf("%d. [推文ID: %s] [用户ID: %s]\n内容: %s\n发布时间: %d\n点赞数: %d\n\n",
				i+1, t.ID, t.UserID, t.Content, t.CreatedAt, t.LikeCount)
		}

		return mcp.NewToolResultText(result), nil
	})
}

// registerHybridSearchTweets 注册混合搜索推文工具
func (s *MCPServer) registerHybridSearchTweets(srv *server.MCPServer) {
	tool := mcp.NewTool("hybrid_search_tweets",
		mcp.WithDescription("混合搜索：同时基于关键词和语义向量搜索推文，结果更精准"),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("搜索关键词或语义描述"),
		),
		mcp.WithNumber("size",
			mcp.Description("返回结果数量，默认 5，最大 20"),
		),
	)

	srv.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := req.Params.Arguments.(map[string]any)
		if !ok {
			return mcp.NewToolResultError("参数解析失败"), nil
		}

		query, ok := args["query"].(string)
		if !ok || query == "" {
			return mcp.NewToolResultError("query 参数不能为空"), nil
		}

		size := 5
		if s, ok := args["size"].(float64); ok && s > 0 {
			size = int(s)
			if size > 20 {
				size = 20
			}
		}

		// 1. 向量化
		vector, err := s.aiClient.GetEmbedding(ctx, query, s.model)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("embedding failed: %v", err)), nil
		}

		// 2. 混合搜索
		tweets, err := s.esClient.HybridSearchTweets(ctx, query, vector, size)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("hybrid search failed: %v", err)), nil
		}

		if len(tweets) == 0 {
			return mcp.NewToolResultText("没有找到相关推文"), nil
		}

		result := fmt.Sprintf("混合搜索找到 %d 条推文：\n\n", len(tweets))
		for i, t := range tweets {
			result += fmt.Sprintf("%d. [推文ID: %s] [用户ID: %s]\n内容: %s\n发布时间: %d\n点赞数: %d\n\n",
				i+1, t.ID, t.UserID, t.Content, t.CreatedAt, t.LikeCount)
		}

		return mcp.NewToolResultText(result), nil
	})
}

func (s *MCPServer) registerCreateTweet(srv *server.MCPServer) {
	tool := mcp.NewTool("create_tweet",
		mcp.WithDescription("代替用户发布一条推文"),
		mcp.WithString("user_id",
			mcp.Required(),
			mcp.Description("发推的用户ID"),
		),
		mcp.WithString("content",
			mcp.Required(),
			mcp.Description("推文内容，不超过280字"),
		),
	)

	srv.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := req.Params.Arguments.(map[string]any)
		if !ok {
			return mcp.NewToolResultError("参数解析失败"), nil
		}

		content, ok := args["content"].(string)
		if !ok || content == "" {
			return mcp.NewToolResultError("content 不能为空"), nil
		}

		userIDStr, ok := args["user_id"].(string)
		if !ok || userIDStr == "" {
			return mcp.NewToolResultError("user_id 不能为空"), nil
		}

		userID, err := strconv.ParseUint(userIDStr, 10, 64)
		if err != nil {
			return mcp.NewToolResultError("user_id 格式错误"), nil
		}

		resp, err := s.tweetClient.CreateTweet(ctx, &tweetv1.CreateTweetRequest{
			UserId:  userID,
			Content: content,
		})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("发推失败: %v", err)), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("发推成功！推文ID: %d，内容: %s", resp.Tweet.Id, resp.Tweet.Content)), nil
	})
}
