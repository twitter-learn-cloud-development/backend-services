package mcp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	tweetv1 "twitter-clone/api/tweet/v1"
	userv1 "twitter-clone/api/user/v1"
	mcpSecurity "twitter-clone/internal/module/agent/mcp/security"
	"twitter-clone/internal/module/agent/mcp/tools"
	agentWebSearch "twitter-clone/internal/module/agent/websearch"
	"twitter-clone/pkg/ai"
	"twitter-clone/pkg/es"
	"twitter-clone/pkg/qdrant"
	platformTrace "twitter-clone/pkg/trace"

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
	webSearch    agentWebSearch.Provider
	pageReader   agentWebSearch.PageReader
	authToken    string
	mu           sync.Mutex
	sseServer    *server.SSEServer
}

type Option func(*MCPServer)

func WithAuthToken(token string) Option {
	return func(server *MCPServer) {
		server.authToken = strings.TrimSpace(token)
	}
}

func WithWebSearchProvider(provider agentWebSearch.Provider) Option {
	return func(server *MCPServer) {
		server.webSearch = provider
	}
}

func WithPageReader(reader agentWebSearch.PageReader) Option {
	return func(server *MCPServer) {
		server.pageReader = reader
	}
}

func NewMCPServer(esClient *es.Client, qdrantClient *qdrant.Client, aiClient *ai.Client, reranker ai.Reranker, tweetClient tweetv1.TweetServiceClient, userClient userv1.UserServiceClient, model string, options ...Option) *MCPServer {
	mcpServer := &MCPServer{
		esClient:     esClient,
		qdrantClient: qdrantClient,
		aiClient:     aiClient,
		reranker:     reranker,
		tweetClient:  tweetClient,
		userClient:   userClient,
		model:        model,
	}
	for _, option := range options {
		if option != nil {
			option(mcpServer)
		}
	}
	return mcpServer
}

func (s *MCPServer) Start(addr string) error {
	if err := validateInternalAddress(addr); err != nil {
		return err
	}
	authenticator, err := mcpSecurity.NewAuthenticator(s.authToken)
	if err != nil {
		return fmt.Errorf("configure MCP authentication: %w", err)
	}
	srv := server.NewMCPServer(
		"twitter-agent-mcp",
		"1.0.0",
		server.WithToolCapabilities(true),
		server.WithToolHandlerMiddleware(authenticator.ToolMiddleware()),
	)

	// 注册所有 Tools
	tools.RegisterSearchTweets(srv, s.aiClient, s.esClient, s.qdrantClient, s.reranker, s.tweetClient, s.model)
	tools.RegisterHybridSearchTweets(srv, s.aiClient, s.esClient, s.qdrantClient, s.reranker, s.tweetClient, s.model)
	tools.RegisterCreateTweet(srv, s.tweetClient)
	tools.RegisterGetUserTweets(srv, s.tweetClient)
	tools.RegisterGetTweetsByIds(srv, s.tweetClient)
	tools.RegisterSearchUsers(srv, s.userClient)
	tools.RegisterWebSearch(srv, s.webSearch)
	tools.RegisterPageRead(srv, s.pageReader)

	// 启动 HTTP SSE 模式
	baseURL := addr
	if strings.HasPrefix(baseURL, "0.0.0.0:") {
		baseURL = "127.0.0.1:" + strings.TrimPrefix(baseURL, "0.0.0.0:")
	}
	transport := &http.Server{Addr: addr, ReadHeaderTimeout: 5 * time.Second}
	sseServer := server.NewSSEServer(
		srv,
		server.WithBaseURL(fmt.Sprintf("http://%s", baseURL)),
		server.WithHTTPServer(transport),
	)
	transport.Handler = platformTrace.HTTPServerMiddleware(
		authenticator.HTTPMiddleware(sseServer),
		"agent.mcp.http",
		nil,
	)
	s.mu.Lock()
	s.sseServer = sseServer
	s.mu.Unlock()
	if err := sseServer.Start(addr); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("mcp server start failed: %w", err)
	}
	return nil
}

func (s *MCPServer) Shutdown(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	sseServer := s.sseServer
	s.mu.Unlock()
	if sseServer == nil {
		return nil
	}
	return sseServer.Shutdown(ctx)
}

func validateInternalAddress(addr string) error {
	host, _, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return fmt.Errorf("invalid MCP server address: %w", err)
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("MCP server must bind to a loopback address, got %q", host)
	}
	return nil
}
