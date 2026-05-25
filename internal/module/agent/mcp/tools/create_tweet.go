package tools

import (
	"context"
	"fmt"
	"strconv"

	tweetv1 "twitter-clone/api/tweet/v1"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RegisterCreateTweet 注册创建推文工具
func RegisterCreateTweet(srv *server.MCPServer, tweetClient tweetv1.TweetServiceClient) {
	tool := mcp.NewTool("create_tweet",
		mcp.WithDescription("代替用户发布一条推文"),
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

		resp, err := tweetClient.CreateTweet(ctx, &tweetv1.CreateTweetRequest{
			UserId:  userID,
			Content: content,
		})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("发推失败: %v", err)), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("发推成功！推文ID: %d，内容: %s", resp.Tweet.Id, resp.Tweet.Content)), nil
	})
}
