package service

import (
	"context"
	"encoding/json"
	"fmt"

	"twitter-clone/pkg/logger"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/sashabaranov/go-openai"
	"go.uber.org/zap"
)

type TweetResult struct {
	TweetID uint64
	URL     string
	Summary string
}

// AgentService AI Agent 服务
type AgentService struct {
	llmClient *openai.Client // 对话模型客户端
	mcpClient *client.Client // MCP Client，连接 MCP Server
	chatModel string         // 对话模型名称
	mcpAddr   string         // MCP Server 地址
}

// NewAgentService 创建 Agent 服务
func NewAgentService(
	llmBaseURL string,
	llmAPIKey string,
	chatModel string,
	mcpAddr string,
) *AgentService {
	config := openai.DefaultConfig(llmAPIKey)
	config.BaseURL = llmBaseURL

	return &AgentService{
		llmClient: openai.NewClientWithConfig(config),
		chatModel: chatModel,
		mcpAddr:   mcpAddr,
	}
}

// initMCPClient 初始化 MCP Client 并获取 Tools
func (s *AgentService) initMCPClient(ctx context.Context) (*client.Client, []mcp.Tool, error) {
	mcpClient, err := client.NewSSEMCPClient(fmt.Sprintf("http://%s/sse", s.mcpAddr))
	if err != nil {
		return nil, nil, fmt.Errorf("create mcp client failed: %w", err)
	}

	if err := mcpClient.Start(ctx); err != nil {
		return nil, nil, fmt.Errorf("mcp client start failed: %w", err)
	}

	// 初始化握手
	if _, err := mcpClient.Initialize(ctx, mcp.InitializeRequest{}); err != nil {
		return nil, nil, fmt.Errorf("mcp initialize failed: %w", err)
	}

	// 获取所有可用 Tools
	toolsResp, err := mcpClient.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return nil, nil, fmt.Errorf("list tools failed: %w", err)
	}

	return mcpClient, toolsResp.Tools, nil
}

// CallApiOfAi 模式一：直接调用 AI 对话，不使用 MCP Tools
func (s *AgentService) CallApiOfAi(ctx context.Context, userID uint64, dialogueID uint64, content string) (string, error) {
	resp, err := s.llmClient.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: s.chatModel,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleSystem,
				Content: "你是一个专业的推特助手，请用简洁友好的方式回答用户问题。",
			},
			{
				Role:    openai.ChatMessageRoleUser,
				Content: content,
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("llm call failed: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("empty response from llm")
	}

	return resp.Choices[0].Message.Content, nil
}

// ConsultContent 模式二：通过 MCP Tool 搜索推文和用户
func (s *AgentService) ConsultContent(ctx context.Context, userID uint64, dialogueID uint64, content string) (string, []TweetResult, error) {
	mcpClient, tools, err := s.initMCPClient(ctx)
	if err != nil {
		return "", nil, fmt.Errorf("init mcp client failed: %w", err)
	}
	defer mcpClient.Close()

	// 2. 把 MCP Tools 转换成 OpenAI Function Calling 格式
	openaiTools := mcpToolsToOpenAI(tools)

	// 3. 构建初始消息
	messages := []openai.ChatCompletionMessage{
		{
			Role:    openai.ChatMessageRoleSystem,
			Content: "你是一个推特内容助手。当用户想搜索推文或博主时，你必须调用对应的工具来查询真实数据，不要凭空捏造结果。",
		},
		{
			Role:    openai.ChatMessageRoleUser,
			Content: content,
		},
	}

	// 4. ReAct 循环：LLM 决策 → 调 Tool → 把结果喂回 LLM → 直到 LLM 不再调 Tool
	for i := 0; i < 5; i++ { // 最多循环 5 次，防止死循环
		resp, err := s.llmClient.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
			Model:    s.chatModel,
			Messages: messages,
			Tools:    openaiTools,
		})
		if err != nil {
			return "", nil, fmt.Errorf("llm call failed: %w", err)
		}

		if len(resp.Choices) == 0 {
			return "", nil, fmt.Errorf("empty response from llm")
		}

		choice := resp.Choices[0]

		// 5. LLM 不再调 Tool，直接返回最终回答
		if choice.FinishReason != openai.FinishReasonToolCalls {
			return choice.Message.Content, nil, nil
		}

		// 6. LLM 要调 Tool，执行它
		messages = append(messages, choice.Message)

		for _, toolCall := range choice.Message.ToolCalls {
			logger.Info(ctx, "mcp tool call", zap.String("tool", toolCall.Function.Name), zap.String("args", toolCall.Function.Arguments))

			// 解析参数
			var args map[string]any
			if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
				return "", nil, fmt.Errorf("parse tool args failed: %w", err)
			}

			// 调用 MCP Server 执行 Tool
			toolResult, err := mcpClient.CallTool(ctx, mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name:      toolCall.Function.Name,
					Arguments: args,
				},
			})
			if err != nil {
				return "", nil, fmt.Errorf("call tool failed: %w", err)
			}

			// 提取 Tool 返回的文本结果
			resultText := ""
			for _, c := range toolResult.Content {
				if textContent, ok := c.(mcp.TextContent); ok {
					resultText += textContent.Text
				}
			}

			// 把 Tool 结果追加到消息历史
			messages = append(messages, openai.ChatCompletionMessage{
				Role:       openai.ChatMessageRoleTool,
				Content:    resultText,
				ToolCallID: toolCall.ID,
			})
		}
	}

	return "", nil, fmt.Errorf("max iterations reached without final answer")
}

// mcpToolsToOpenAI 把 MCP Tools 格式转换成 OpenAI Function Calling 格式
func mcpToolsToOpenAI(tools []mcp.Tool) []openai.Tool {
	openaiTools := make([]openai.Tool, 0, len(tools))
	for _, t := range tools {
		// 把 MCP Tool 的 InputSchema 转成 openai 需要的 map
		schemaBytes, _ := json.Marshal(t.InputSchema)
		var schemaMap map[string]any
		_ = json.Unmarshal(schemaBytes, &schemaMap)

		openaiTools = append(openaiTools, openai.Tool{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  schemaMap,
			},
		})
	}
	return openaiTools
}

func (s *AgentService) AssistPublishTwitter(ctx context.Context, userID uint64, dialogueID uint64, content string) (string, error) {
	// 让 LLM 生成三版推文草稿，不需要调 Tool
	prompt := fmt.Sprintf(`用户想发一条推文，他的想法是："%s"

请帮他生成3个不同风格的推文草稿，要求：
1. 每条不超过280字
2. 风格各异（比如：正式版、轻松版、热点版）
3. 用以下格式输出：

【草稿一】（正式版）
内容...

【草稿二】（轻松版）
内容...

【草稿三】（热点版）
内容...`, content)

	resp, err := s.llmClient.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: s.chatModel,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleSystem,
				Content: "你是一个专业的推特文案助手，擅长写出吸引人的推文。",
			},
			{
				Role:    openai.ChatMessageRoleUser,
				Content: prompt,
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("llm call failed: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("empty response from llm")
	}

	return resp.Choices[0].Message.Content, nil
}
