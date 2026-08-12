package tools

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	tweetv1 "twitter-clone/api/tweet/v1"
	agentEvidence "twitter-clone/internal/module/agent/evidence"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RegisterGetUserTweets 注册获取用户历史推文工具
func RegisterGetUserTweets(srv *server.MCPServer, tweetClient tweetv1.TweetServiceClient) {
	tool := mcp.NewTool("get_user_tweets",
		mcp.WithDescription("获取指定用户的历史推文，用于分析该用户的写作风格"),
		mcp.WithString("user_id",
			mcp.Required(),
			mcp.Description("目标用户的ID"),
		),
		mcp.WithNumber("limit",
			mcp.Description("获取推文数量，默认10，最大50"),
		),
	)

	srv.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := req.Params.Arguments.(map[string]any)
		if !ok {
			return mcp.NewToolResultError("参数解析失败"), nil
		}

		userIDStr, ok := args["user_id"].(string)
		if !ok || userIDStr == "" {
			return mcp.NewToolResultError("user_id 不能为空"), nil
		}

		userID, err := strconv.ParseUint(userIDStr, 10, 64)
		if err != nil {
			return mcp.NewToolResultError("user_id 格式错误"), nil
		}

		limit := int32(10)
		if l, ok := args["limit"].(float64); ok && l > 0 {
			limit = int32(l)
			if limit > 50 {
				limit = 50
			}
		}

		resp, err := tweetClient.GetUserTimeline(ctx, &tweetv1.GetUserTimelineRequest{
			UserId: userID,
			Limit:  limit,
		})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("获取推文失败: %v", err)), nil
		}

		if len(resp.Tweets) == 0 {
			return mcp.NewToolResultText("该用户暂无推文"), nil
		}

		result := fmt.Sprintf("获取到用户 %d 的 %d 条推文：\n\n", userID, len(resp.Tweets))
		for i, t := range resp.Tweets {
			result += fmt.Sprintf("%d. %s\n", i+1, t.Content)
		}

		return mcp.NewToolResultText(result), nil
	})
}

// RegisterGetTweetsByIds 注册通过ID列表获取推文工具
func RegisterGetTweetsByIds(srv *server.MCPServer, tweetClient tweetv1.TweetServiceClient) {
	tool := mcp.NewTool("get_tweets_by_ids",
		mcp.WithDescription("根据推文ID列表获取指定推文内容，用于参考特定推文风格"),
		mcp.WithString("tweet_ids",
			mcp.Required(),
			mcp.Description("推文ID列表，逗号分隔，例如：123,456,789"),
		),
	)

	srv.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := req.Params.Arguments.(map[string]any)
		if !ok {
			return mcp.NewToolResultError("参数解析失败"), nil
		}

		tweetIDsStr, ok := args["tweet_ids"].(string)
		if !ok || tweetIDsStr == "" {
			return mcp.NewToolResultError("tweet_ids 不能为空"), nil
		}

		result := ""
		tweets := make([]*tweetv1.Tweet, 0)
		for _, idStr := range strings.Split(tweetIDsStr, ",") {
			idStr = strings.TrimSpace(idStr)
			tweetID, err := strconv.ParseUint(idStr, 10, 64)
			if err != nil {
				continue
			}

			resp, err := tweetClient.GetTweet(ctx, &tweetv1.GetTweetRequest{
				TweetId: tweetID,
			})
			if err != nil {
				continue
			}
			if resp == nil || resp.Tweet == nil || resp.Tweet.Id == 0 {
				continue
			}
			tweets = append(tweets, resp.Tweet)

			result += fmt.Sprintf("推文ID: %d\n内容: %s\n\n", resp.Tweet.Id, resp.Tweet.Content)
		}

		if result == "" {
			return mcp.NewToolResultText("未找到任何推文"), nil
		}

		return mcp.NewToolResultStructured(newPlatformTweetDetailEvidence(tweets), result), nil
	})
}

func newPlatformTweetDetailEvidence(tweets []*tweetv1.Tweet) agentEvidence.PlatformTweetDetailResult {
	items := make([]agentEvidence.PlatformTweetSearchEvidence, 0, len(tweets))
	for _, tweet := range tweets {
		if tweet == nil || tweet.Id == 0 {
			continue
		}
		items = append(items, agentEvidence.PlatformTweetSearchEvidence{
			TweetID:   strconv.FormatUint(tweet.Id, 10),
			Content:   tweet.Content,
			CreatedAt: tweet.CreatedAt,
		})
	}
	return agentEvidence.PlatformTweetDetailResult{
		Schema: agentEvidence.PlatformTweetDetailSchema,
		Items:  items,
	}
}
