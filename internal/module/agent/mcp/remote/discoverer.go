package remote

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	agentModel "twitter-clone/internal/module/agent/model"
	platformTrace "twitter-clone/pkg/trace"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

type SDKDiscoverer struct {
	endpointPolicy *agentModel.EndpointPolicy
	httpTimeout    time.Duration
	pool           *clientPool
	sessionFactory remoteSessionFactory
}

type SDKDiscovererOption func(*sdkDiscovererOptions)

type sdkDiscovererOptions struct {
	poolConfig     ClientPoolConfig
	poolObserver   PoolObserver
	sessionFactory remoteSessionFactory
}

func WithClientPool(config ClientPoolConfig) SDKDiscovererOption {
	return func(options *sdkDiscovererOptions) { options.poolConfig = config }
}

func WithPoolObserver(observer PoolObserver) SDKDiscovererOption {
	return func(options *sdkDiscovererOptions) { options.poolObserver = observer }
}

func withRemoteSessionFactory(factory remoteSessionFactory) SDKDiscovererOption {
	return func(options *sdkDiscovererOptions) { options.sessionFactory = factory }
}

func NewSDKDiscoverer(
	endpointPolicy *agentModel.EndpointPolicy,
	timeout time.Duration,
	options ...SDKDiscovererOption,
) *SDKDiscoverer {
	if endpointPolicy == nil {
		endpointPolicy = agentModel.NewEndpointPolicy()
	}
	if timeout <= 0 {
		timeout = defaultDiscoveryTime
	}
	config := sdkDiscovererOptions{}
	for _, option := range options {
		if option != nil {
			option(&config)
		}
	}
	discoverer := &SDKDiscoverer{endpointPolicy: endpointPolicy, httpTimeout: timeout}
	discoverer.sessionFactory = config.sessionFactory
	if discoverer.sessionFactory == nil {
		discoverer.sessionFactory = discoverer.openSession
	}
	if config.poolConfig.Enabled {
		discoverer.pool = newClientPool(config.poolConfig, discoverer.sessionFactory, config.poolObserver)
	}
	return discoverer
}

func (discoverer *SDKDiscoverer) Discover(ctx context.Context, request DiscoveryRequest) ([]mcp.Tool, error) {
	if discoverer == nil {
		return nil, errors.New("external MCP discoverer is unavailable")
	}
	mcpClient, release, err := discoverer.acquire(ctx, request)
	if err != nil {
		return nil, err
	}
	reusable := false
	defer func() { release(reusable) }()
	result, err := mcpClient.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return nil, fmt.Errorf("list external MCP tools: %w", err)
	}
	if result == nil {
		return nil, errors.New("external MCP server returned an empty tool list response")
	}
	reusable = true
	return result.Tools, nil
}

func (discoverer *SDKDiscoverer) Call(
	ctx context.Context,
	request DiscoveryRequest,
	toolName string,
	arguments map[string]interface{},
) (*mcp.CallToolResult, error) {
	if discoverer == nil {
		return nil, errors.New("external MCP caller is unavailable")
	}
	mcpClient, release, err := discoverer.acquire(ctx, request)
	if err != nil {
		return nil, err
	}
	reusable := false
	defer func() { release(reusable) }()
	result, err := mcpClient.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{
		Name: toolName, Arguments: arguments,
	}})
	if err != nil {
		return nil, fmt.Errorf("call external MCP tool: %w", err)
	}
	reusable = true
	return result, nil
}

func (discoverer *SDKDiscoverer) Ping(ctx context.Context, request DiscoveryRequest) error {
	if discoverer == nil {
		return errors.New("external MCP health prober is unavailable")
	}
	mcpClient, release, err := discoverer.acquire(ctx, request)
	if err != nil {
		return err
	}
	reusable := false
	defer func() { release(reusable) }()
	if err := mcpClient.Ping(ctx); err != nil {
		return fmt.Errorf("ping external MCP server: %w", err)
	}
	reusable = true
	return nil
}

func (discoverer *SDKDiscoverer) Invalidate(request DiscoveryRequest) {
	if discoverer != nil && discoverer.pool != nil {
		discoverer.pool.Invalidate(request)
	}
}

func (discoverer *SDKDiscoverer) Prune() {
	if discoverer != nil && discoverer.pool != nil {
		discoverer.pool.Prune()
	}
}

func (discoverer *SDKDiscoverer) Close() error {
	if discoverer == nil || discoverer.pool == nil {
		return nil
	}
	return discoverer.pool.Close()
}

func (discoverer *SDKDiscoverer) acquire(
	ctx context.Context,
	request DiscoveryRequest,
) (remoteSession, func(bool), error) {
	if discoverer.pool == nil {
		session, err := discoverer.sessionFactory(ctx, request)
		if err != nil {
			return nil, nil, err
		}
		return session, func(bool) { _ = session.Close() }, nil
	}
	lease, err := discoverer.pool.Acquire(ctx, request)
	if err != nil {
		return nil, nil, err
	}
	return lease.Client(), lease.Release, nil
}

func (discoverer *SDKDiscoverer) openSession(
	ctx context.Context,
	request DiscoveryRequest,
) (remoteSession, error) {
	if err := discoverer.endpointPolicy.Validate(request.Endpoint, "external-mcp"); err != nil {
		return nil, err
	}
	restrictedClient := agentModel.NewRestrictedHTTPClient(discoverer.endpointPolicy, "external-mcp")
	restrictedClient.Timeout = discoverer.httpTimeout
	httpClient := platformTrace.InstrumentHTTPClient(restrictedClient, "agent.external_mcp.http", nil)
	headers := make(map[string]string, 1)
	if token := strings.TrimSpace(request.BearerToken); token != "" {
		headers["Authorization"] = "Bearer " + token
	}
	mcpClient, err := newRemoteClient(request.Transport, request.Endpoint, headers, httpClient)
	if err != nil {
		return nil, err
	}
	if err := mcpClient.Start(ctx); err != nil {
		_ = mcpClient.Close()
		return nil, fmt.Errorf("start external MCP client: %w", err)
	}
	if _, err := mcpClient.Initialize(ctx, mcp.InitializeRequest{}); err != nil {
		_ = mcpClient.Close()
		return nil, fmt.Errorf("initialize external MCP server: %w", err)
	}
	return mcpClient, nil
}

func newRemoteClient(
	transportName string,
	endpoint string,
	headers map[string]string,
	httpClient *http.Client,
) (*client.Client, error) {
	switch transportName {
	case TransportStreamableHTTP:
		return client.NewStreamableHttpClient(
			endpoint,
			transport.WithHTTPHeaders(headers),
			transport.WithHTTPBasicClient(httpClient),
		)
	case TransportSSE:
		return client.NewSSEMCPClient(
			endpoint,
			client.WithHeaders(headers),
			client.WithHTTPClient(httpClient),
		)
	default:
		return nil, fmt.Errorf("unsupported external MCP transport %q", transportName)
	}
}
