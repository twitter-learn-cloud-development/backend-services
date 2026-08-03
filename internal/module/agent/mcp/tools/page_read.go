package tools

import (
	"context"

	agentEvidence "twitter-clone/internal/module/agent/evidence"
	agentWebSearch "twitter-clone/internal/module/agent/websearch"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const PageReadToolName = "page_read"

func RegisterPageRead(srv *server.MCPServer, reader agentWebSearch.PageReader) {
	if srv == nil || reader == nil {
		return
	}
	tool := mcp.NewTool(
		PageReadToolName,
		mcp.WithOutputSchema[agentEvidence.WebPageResult](),
		mcp.WithDescription("Read bounded visible text from a public web page returned by web search."),
		mcp.WithString(
			"url",
			mcp.Required(),
			mcp.Description("Absolute public HTTP(S) page URL. Private hosts, redirects and credentials are rejected."),
		),
	)
	srv.AddTool(tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		arguments, ok := request.Params.Arguments.(map[string]any)
		if !ok {
			return mcp.NewToolResultError("invalid page_read arguments"), nil
		}
		rawURL, ok := arguments["url"].(string)
		if !ok {
			return mcp.NewToolResultError("url is required"), nil
		}
		result, err := reader.Read(ctx, agentWebSearch.PageRequest{
			URL: rawURL, Subject: webAccessSubject(arguments),
		})
		if err != nil {
			return mcp.NewToolResultError("web page request failed"), nil
		}
		return mcp.NewToolResultStructured(
			result,
			agentWebSearch.FormatPageForModel(result),
		), nil
	})
}
