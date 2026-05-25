package tools

import (
	"context"
	"fmt"

	userv1 "twitter-clone/api/user/v1"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RegisterSearchUsers 注册搜索用户工具
func RegisterSearchUsers(srv *server.MCPServer, userClient userv1.UserServiceClient) {
	tool := mcp.NewTool("search_users",
		mcp.WithDescription("根据关键词搜索用户（博主），获取用户的基本信息"),
		mcp.WithString("keyword",
			mcp.Required(),
			mcp.Description("搜索用户的关键词，例如：张三、健身、AI"),
		),
		mcp.WithNumber("limit",
			mcp.Description("返回结果数量，默认 5，最大 20"),
		),
	)

	srv.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := req.Params.Arguments.(map[string]any)
		if !ok {
			return mcp.NewToolResultError("参数解析失败"), nil
		}

		keyword, ok := args["keyword"].(string)
		if !ok || keyword == "" {
			return mcp.NewToolResultError("keyword 参数不能为空"), nil
		}

		limit := int32(5)
		if l, ok := args["limit"].(float64); ok && l > 0 {
			limit = int32(l)
			if limit > 20 {
				limit = 20
			}
		}

		resp, err := userClient.SearchUsers(ctx, &userv1.SearchUsersRequest{
			Keyword: keyword,
			Limit:   limit,
		})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("搜索用户失败: %v", err)), nil
		}

		if len(resp.Users) == 0 {
			return mcp.NewToolResultText("没有找到相关用户"), nil
		}

		result := fmt.Sprintf("找到 %d 个相关用户：\n\n", len(resp.Users))
		for i, u := range resp.Users {
			result += fmt.Sprintf("%d. [用户ID: %d] 用户名: %s\n简介: %s\n\n",
				i+1, u.Id, u.Username, u.Bio)
		}

		return mcp.NewToolResultText(result), nil
	})
}
