package tools

import (
	"context"
	"strconv"
	"strings"

	agentEvidence "twitter-clone/internal/module/agent/evidence"
	agentWebSearch "twitter-clone/internal/module/agent/websearch"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const WebSearchToolName = "web_search"

func RegisterWebSearch(srv *server.MCPServer, provider agentWebSearch.Provider) {
	if srv == nil || provider == nil {
		return
	}
	tool := mcp.NewTool(
		WebSearchToolName,
		mcp.WithOutputSchema[agentEvidence.WebSearchResult](),
		mcp.WithDescription("Search the public web and return normalized, citable source records."),
		mcp.WithString(
			"query",
			mcp.Required(),
			mcp.Description("Public web search query, up to 400 characters and 50 words."),
		),
		mcp.WithNumber(
			"count",
			mcp.Description("Number of results to return, from 1 to 10. Defaults to 5."),
		),
	)
	srv.AddTool(tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		arguments, ok := request.Params.Arguments.(map[string]any)
		if !ok {
			return mcp.NewToolResultError("invalid web_search arguments"), nil
		}
		query, ok := arguments["query"].(string)
		if !ok {
			return mcp.NewToolResultError("query is required"), nil
		}
		limit := agentWebSearch.DefaultResultLimit
		if value, ok := arguments["count"].(float64); ok {
			limit = int(value)
		}
		subject := webAccessSubject(arguments)
		result, err := provider.Search(ctx, agentWebSearch.Request{
			Query: query, Limit: limit, Subject: subject,
			ProviderConfigID: webProviderConfigID(arguments),
		})
		if err != nil {
			return mcp.NewToolResultError("web search provider request failed"), nil
		}
		return mcp.NewToolResultStructured(
			result,
			agentWebSearch.FormatForModel(result),
		), nil
	})
}

func webAccessSubject(arguments map[string]any) agentWebSearch.AccessSubject {
	return agentWebSearch.AccessSubject{
		UserID: webAccessUint64(arguments[agentWebSearch.InternalUserIDArgument]),
		RunID:  webAccessString(arguments[agentWebSearch.InternalRunIDArgument]),
	}
}

func webProviderConfigID(arguments map[string]any) string {
	return strings.TrimSpace(webAccessString(arguments[agentWebSearch.InternalProviderConfigIDArgument]))
}

func webAccessString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return ""
	}
}

func webAccessUint64(value any) uint64 {
	switch typed := value.(type) {
	case uint64:
		return typed
	case int:
		if typed > 0 {
			return uint64(typed)
		}
	case float64:
		if typed > 0 {
			return uint64(typed)
		}
	case string:
		parsed, _ := strconv.ParseUint(typed, 10, 64)
		return parsed
	}
	return 0
}
