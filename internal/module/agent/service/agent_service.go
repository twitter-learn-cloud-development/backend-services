package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"twitter-clone/internal/module/agent/repository"
	"twitter-clone/pkg/logger"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/sashabaranov/go-openai"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.uber.org/zap"
)

// ========================== 常量 ==========================

const (
	// MaxContextMessages 多轮对话最多携带的历史消息数
	// 控制 token 消耗，20 条消息约覆盖最近 10 轮对话
	MaxContextMessages = 20
)

// ========================== 返回结构体 ==========================

type TweetResult struct {
	TweetID uint64
	URL     string
	Summary string
}

// ChatResult 统一的对话返回结构
type ChatResult struct {
	DialogueID string        // 对话会话 ID（十六进制字符串）
	Response   string        // AI 回复文本
	Tweets     []TweetResult // 推文搜索结果（模式二时有值）
}

// ========================== AgentService ==========================

// AgentService AI Agent 服务
type AgentService struct {
	llmClient *openai.Client               // 对话模型客户端
	repo      repository.AgentRepository   // 对话持久化仓储
	chatModel string                        // 对话模型名称
	mcpAddr   string                        // MCP Server 地址

	// 长连接与连接池复用
	mcpClient *client.Client
	mcpTools  []mcp.Tool
	mcpMu     sync.RWMutex
}

// NewAgentService 创建 Agent 服务
func NewAgentService(
	llmBaseURL string,
	llmAPIKey string,
	chatModel string,
	mcpAddr string,
	repo repository.AgentRepository,
) *AgentService {
	config := openai.DefaultConfig(llmAPIKey)
	config.BaseURL = llmBaseURL

	return &AgentService{
		llmClient: openai.NewClientWithConfig(config),
		chatModel: chatModel,
		mcpAddr:   mcpAddr,
		repo:      repo,
	}
}

// ========================== 对话上下文辅助方法 ==========================

// getOrCreateDialogue 获取已有对话或创建新对话
// dialogueIDHex 为空字符串时创建新对话，否则加载已有对话
func (s *AgentService) getOrCreateDialogue(ctx context.Context, userID uint64, dialogueIDHex string, firstMessage string, mode repository.DialogueMode) (*repository.Dialogue, error) {
	if dialogueIDHex != "" && dialogueIDHex != "0" {
		oid, err := primitive.ObjectIDFromHex(dialogueIDHex)
		if err != nil {
			return nil, fmt.Errorf("invalid dialogue_id: %w", err)
		}
		dialogue, err := s.repo.GetDialogue(ctx, oid)
		if err != nil {
			return nil, fmt.Errorf("get dialogue failed: %w", err)
		}
		// 验证对话归属
		if dialogue.UserID != userID {
			return nil, fmt.Errorf("dialogue does not belong to user %d", userID)
		}
		return dialogue, nil
	}

	// 创建新对话
	title := repository.GenerateTitle(firstMessage)
	dialogue, err := s.repo.CreateDialogue(ctx, userID, title, mode)
	if err != nil {
		return nil, fmt.Errorf("create dialogue failed: %w", err)
	}
	return dialogue, nil
}

// loadContextMessages 加载历史消息并转换为 OpenAI 格式
func (s *AgentService) loadContextMessages(ctx context.Context, dialogueID primitive.ObjectID) ([]openai.ChatCompletionMessage, error) {
	recentMsgs, err := s.repo.GetRecentMessages(ctx, dialogueID, MaxContextMessages)
	if err != nil {
		return nil, fmt.Errorf("load context messages failed: %w", err)
	}

	messages := make([]openai.ChatCompletionMessage, 0, len(recentMsgs))
	for _, msg := range recentMsgs {
		messages = append(messages, openai.ChatCompletionMessage{
			Role:    string(msg.Role),
			Content: msg.Content,
		})
	}
	return messages, nil
}

// saveUserAndAssistantMessages 保存用户问题和 AI 回复到 MongoDB
func (s *AgentService) saveUserAndAssistantMessages(ctx context.Context, dialogueID primitive.ObjectID, userID uint64, userContent string, assistantContent string, metadata map[string]any) error {
	msgs := []*repository.DialogueMessage{
		{
			DialogueID: dialogueID,
			UserID:     userID,
			Role:       repository.RoleUser,
			Content:    userContent,
		},
		{
			DialogueID: dialogueID,
			UserID:     userID,
			Role:       repository.RoleAssistant,
			Content:    assistantContent,
			Metadata:   metadata,
		},
	}

	if err := s.repo.SaveMessages(ctx, msgs); err != nil {
		return fmt.Errorf("save messages failed: %w", err)
	}

	// 更新对话的 updated_at
	if err := s.repo.TouchDialogue(ctx, dialogueID); err != nil {
		logger.Warn(ctx, "touch dialogue failed", zap.Error(err))
	}

	return nil
}

// ========================== 对话历史查询 ==========================

// ListDialogues 获取用户对话列表
func (s *AgentService) ListDialogues(ctx context.Context, userID uint64, page, pageSize int) ([]*repository.Dialogue, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 50 {
		pageSize = 20
	}
	return s.repo.ListDialogues(ctx, userID, page, pageSize)
}

// GetDialogueMessages 获取对话详细消息列表
func (s *AgentService) GetDialogueMessages(ctx context.Context, userID uint64, dialogueIDHex string) ([]*repository.DialogueMessage, error) {
	oid, err := primitive.ObjectIDFromHex(dialogueIDHex)
	if err != nil {
		return nil, fmt.Errorf("invalid dialogue_id: %w", err)
	}

	// 验证对话归属
	dialogue, err := s.repo.GetDialogue(ctx, oid)
	if err != nil {
		return nil, err
	}
	if dialogue.UserID != userID {
		return nil, fmt.Errorf("dialogue does not belong to user %d", userID)
	}

	return s.repo.GetMessages(ctx, oid)
}

// DeleteDialogue 删除对话
func (s *AgentService) DeleteDialogue(ctx context.Context, userID uint64, dialogueIDHex string) error {
	oid, err := primitive.ObjectIDFromHex(dialogueIDHex)
	if err != nil {
		return fmt.Errorf("invalid dialogue_id: %w", err)
	}
	return s.repo.DeleteDialogue(ctx, oid, userID)
}

// ========================== 模型信息 ==========================

// GetModelInfo 获取可用模型列表
func (s *AgentService) GetModelInfo() []ModelInfo {
	return GetAvailableModels()
}

// ========================== 文件解析 ==========================

// FileAnalysisResult 文件解析结果
type FileAnalysisResult struct {
	ParsedContent string // 解析出的文本内容
	FileKey       string // 存储 key，后续对话可通过此 key 引用
}

// AnalysisFile 解析上传的文件，提取文本内容并存入 MongoDB
// 存储后返回 file_key，用户可在后续对话中通过 file_key 引用文件内容
func (s *AgentService) AnalysisFile(ctx context.Context, userID uint64, fileKindID uint64, fileContent []byte) (*FileAnalysisResult, error) {
	// 1. 解析文件内容
	parsedText, err := ParseFile(fileKindID, fileContent)
	if err != nil {
		return nil, fmt.Errorf("parse file failed: %w", err)
	}

	// 2. 创建一个专门的对话来存储文件解析结果
	fileKindName := "未知文件"
	for _, fk := range SupportedFileKinds {
		if fk.ID == fileKindID {
			fileKindName = fk.Name
			break
		}
	}

	dialogue, err := s.repo.CreateDialogue(ctx, userID, fmt.Sprintf("[文件] %s", fileKindName), repository.ModeChat)
	if err != nil {
		return nil, fmt.Errorf("create file dialogue failed: %w", err)
	}

	// 3. 将解析结果作为 system 消息存入对话
	msg := &repository.DialogueMessage{
		DialogueID: dialogue.ID,
		UserID:     userID,
		Role:       repository.RoleSystem,
		Content:    fmt.Sprintf("用户上传了一个 %s 文件，以下是解析后的内容：\n\n%s", fileKindName, parsedText),
		Metadata: map[string]any{
			"file_kind_id":   fileKindID,
			"file_kind_name": fileKindName,
			"file_size":      len(fileContent),
		},
	}
	if err := s.repo.SaveMessage(ctx, msg); err != nil {
		return nil, fmt.Errorf("save file message failed: %w", err)
	}

	return &FileAnalysisResult{
		ParsedContent: parsedText,
		FileKey:       dialogue.ID.Hex(), // 使用对话 ID 作为 file_key，后续可直接加载此对话的上下文
	}, nil
}

// ========================== 模式一：直接 AI 对话 ==========================

// CallApiOfAi 模式一：直接调用 AI 对话，不使用 MCP Tools
// 支持多轮上下文：dialogueID 非空时加载历史消息
func (s *AgentService) CallApiOfAi(ctx context.Context, userID uint64, dialogueID uint64, content string) (*ChatResult, error) {
	// 1. 获取或创建对话（dialogueID 作为十六进制传入时需要适配，这里兼容旧的 uint64 传参）
	dialogueIDHex := ""
	if dialogueID > 0 {
		// 旧接口兼容：尝试将 uint64 当作 dialogueID 使用
		// 新接口应直接传 hex 字符串
		dialogueIDHex = fmt.Sprintf("%024x", dialogueID)
	}

	dialogue, err := s.getOrCreateDialogue(ctx, userID, dialogueIDHex, content, repository.ModeChat)
	if err != nil {
		return nil, err
	}

	// 2. 构建消息列表：system + 历史上下文 + 当前用户消息
	messages := []openai.ChatCompletionMessage{
		{
			Role:    openai.ChatMessageRoleSystem,
			Content: "你是一个专业的推特助手，请用简洁友好的方式回答用户问题。",
		},
	}

	// 加载历史上下文
	contextMsgs, err := s.loadContextMessages(ctx, dialogue.ID)
	if err != nil {
		logger.Warn(ctx, "load context failed, proceeding without history", zap.Error(err))
	} else {
		messages = append(messages, contextMsgs...)
	}

	// 追加当前用户消息
	messages = append(messages, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: content,
	})

	// 3. 调用 LLM
	resp, err := s.llmClient.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model:    s.chatModel,
		Messages: messages,
	})
	if err != nil {
		return nil, fmt.Errorf("llm call failed: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("empty response from llm")
	}

	aiResponse := resp.Choices[0].Message.Content

	// 4. 持久化消息
	if err := s.saveUserAndAssistantMessages(ctx, dialogue.ID, userID, content, aiResponse, nil); err != nil {
		logger.Error(ctx, "save messages failed", zap.Error(err))
	}

	return &ChatResult{
		DialogueID: dialogue.ID.Hex(),
		Response:   aiResponse,
	}, nil
}

// ========================== 模式二：RAG 语义搜索 ==========================

// ConsultContent 模式二：通过 MCP Tool 搜索推文和用户
// 使用 ReAct 循环：LLM 决策 → 调 Tool → 喂回结果 → 直到不再调 Tool
func (s *AgentService) ConsultContent(ctx context.Context, userID uint64, dialogueID uint64, content string) (*ChatResult, error) {
	dialogueIDHex := ""
	if dialogueID > 0 {
		dialogueIDHex = fmt.Sprintf("%024x", dialogueID)
	}

	dialogue, err := s.getOrCreateDialogue(ctx, userID, dialogueIDHex, content, repository.ModeConsult)
	if err != nil {
		return nil, err
	}

	// 1. 初始化 MCP Client
	mcpClient, tools, err := s.getOrInitMCPClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("init mcp client failed: %w", err)
	}

	// 2. 把 MCP Tools 转换成 OpenAI Function Calling 格式
	openaiTools := mcpToolsToOpenAI(tools)

	// 3. 构建初始消息（含历史上下文）
	messages := []openai.ChatCompletionMessage{
		{
			Role:    openai.ChatMessageRoleSystem,
			Content: "你是一个推特内容助手。当用户想搜索推文或博主时，你必须调用对应的工具来查询真实数据，不要凭空捏造结果。",
		},
	}

	// 加载历史上下文
	contextMsgs, err := s.loadContextMessages(ctx, dialogue.ID)
	if err != nil {
		logger.Warn(ctx, "load context failed", zap.Error(err))
	} else {
		messages = append(messages, contextMsgs...)
	}

	messages = append(messages, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: content,
	})

	// 4. ReAct 循环：LLM 决策 → 调 Tool → 把结果喂回 LLM → 直到 LLM 不再调 Tool
	for i := 0; i < 5; i++ { // 最多循环 5 次，防止死循环
		resp, err := s.llmClient.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
			Model:    s.chatModel,
			Messages: messages,
			Tools:    openaiTools,
		})
		if err != nil {
			return nil, fmt.Errorf("llm call failed: %w", err)
		}

		if len(resp.Choices) == 0 {
			return nil, fmt.Errorf("empty response from llm")
		}

		choice := resp.Choices[0]

		// 5. LLM 不再调 Tool，直接返回最终回答
		if choice.FinishReason != openai.FinishReasonToolCalls {
			aiResponse := choice.Message.Content

			// 持久化消息
			if err := s.saveUserAndAssistantMessages(ctx, dialogue.ID, userID, content, aiResponse, nil); err != nil {
				logger.Error(ctx, "save messages failed", zap.Error(err))
			}

			return &ChatResult{
				DialogueID: dialogue.ID.Hex(),
				Response:   aiResponse,
			}, nil
		}

		// 6. LLM 要调 Tool，执行它
		messages = append(messages, choice.Message)

		for _, toolCall := range choice.Message.ToolCalls {
			logger.Info(ctx, "mcp tool call", zap.String("tool", toolCall.Function.Name), zap.String("args", toolCall.Function.Arguments))

			// 解析参数
			var args map[string]any
			if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
				return nil, fmt.Errorf("parse tool args failed: %w", err)
			}

			// 调用 MCP Server 执行 Tool，并进行身份鉴权注入
			toolResult, err := s.callToolWithAuth(ctx, mcpClient, userID, mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name:      toolCall.Function.Name,
					Arguments: args,
				},
			})
			if err != nil {
				s.resetMCPClient() // 异常断连重置
				return nil, fmt.Errorf("call tool failed: %w", err)
			}

			// 提取 Tool 返回的文本结果
			resultText := extractTextFromToolResult(toolResult)

			// 把 Tool 结果追加到消息历史
			messages = append(messages, openai.ChatCompletionMessage{
				Role:       openai.ChatMessageRoleTool,
				Content:    resultText,
				ToolCallID: toolCall.ID,
			})
		}
	}

	return nil, fmt.Errorf("max iterations reached without final answer")
}

// ========================== 模式三：AI 辅助写推文 ==========================

// AssistPublishTwitter 模式三：让 LLM 生成推文草稿并在确认后调用工具发推 (ReAct)
func (s *AgentService) AssistPublishTwitter(ctx context.Context, userID uint64, dialogueID uint64, content string) (*ChatResult, error) {
	dialogueIDHex := ""
	if dialogueID > 0 {
		dialogueIDHex = fmt.Sprintf("%024x", dialogueID)
	}

	dialogue, err := s.getOrCreateDialogue(ctx, userID, dialogueIDHex, content, repository.ModeAssist)
	if err != nil {
		return nil, err
	}

	// 1. 初始化 MCP Client
	mcpClient, tools, err := s.getOrInitMCPClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("init mcp client failed: %w", err)
	}

	// 2. 把 MCP Tools 转换成 OpenAI Function Calling 格式
	openaiTools := mcpToolsToOpenAI(tools)

	// 3. 构建初始消息（含系统设定）
	systemPrompt := fmt.Sprintf(`你是一个专业的推特文案助手，当前服务于 user_id: %d。
1. 当用户想要发推且没有确定内容时，请帮他生成3个不同风格的推文草稿（不超过280字，可分正式版、轻松版、热点版）。
2. 当用户确认了某个草稿，或者明确要求发推时，请务必立刻调用 create_tweet 工具完成发布。调用时请传入当前的 user_id 以及要发布的推文 content。`, userID)

	messages := []openai.ChatCompletionMessage{
		{
			Role:    openai.ChatMessageRoleSystem,
			Content: systemPrompt,
		},
	}

	// 加载历史上下文
	contextMsgs, err := s.loadContextMessages(ctx, dialogue.ID)
	if err != nil {
		logger.Warn(ctx, "load context failed", zap.Error(err))
	} else {
		messages = append(messages, contextMsgs...)
	}

	messages = append(messages, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: content,
	})

	// 4. ReAct 循环
	for i := 0; i < 5; i++ {
		resp, err := s.llmClient.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
			Model:    s.chatModel,
			Messages: messages,
			Tools:    openaiTools,
		})
		if err != nil {
			return nil, fmt.Errorf("llm call failed: %w", err)
		}

		if len(resp.Choices) == 0 {
			return nil, fmt.Errorf("empty response from llm")
		}

		choice := resp.Choices[0]

		// 5. LLM 不再调 Tool，直接返回最终回答
		if choice.FinishReason != openai.FinishReasonToolCalls {
			aiResponse := choice.Message.Content

			// 持久化消息
			if err := s.saveUserAndAssistantMessages(ctx, dialogue.ID, userID, content, aiResponse, nil); err != nil {
				logger.Error(ctx, "save messages failed", zap.Error(err))
			}

			return &ChatResult{
				DialogueID: dialogue.ID.Hex(),
				Response:   aiResponse,
			}, nil
		}

		// 6. LLM 要调 Tool，执行它
		messages = append(messages, choice.Message)

		for _, toolCall := range choice.Message.ToolCalls {
			logger.Info(ctx, "mcp tool call", zap.String("tool", toolCall.Function.Name), zap.String("args", toolCall.Function.Arguments))

			var args map[string]any
			if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
				return nil, fmt.Errorf("parse tool args failed: %w", err)
			}

			// 调用 MCP Server 执行 Tool，并进行身份鉴权注入
			toolResult, err := s.callToolWithAuth(ctx, mcpClient, userID, mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name:      toolCall.Function.Name,
					Arguments: args,
				},
			})
			if err != nil {
				s.resetMCPClient() // 异常断连重置
				return nil, fmt.Errorf("call tool failed: %w", err)
			}

			resultText := extractTextFromToolResult(toolResult)

			messages = append(messages, openai.ChatCompletionMessage{
				Role:       openai.ChatMessageRoleTool,
				Content:    resultText,
				ToolCallID: toolCall.ID,
			})
		}
	}

	return nil, fmt.Errorf("max iterations reached without final answer")
}

// ConfirmPublishTwitter 模式三第二阶段：确认发布推文
func (s *AgentService) ConfirmPublishTwitter(ctx context.Context, userID uint64, content string) (string, error) {
	mcpClient, _, err := s.getOrInitMCPClient(ctx)
	if err != nil {
		return "", fmt.Errorf("init mcp client failed: %w", err)
	}

	// 直接调 create_tweet Tool，不经过 LLM，并进行身份鉴权注入
	toolResult, err := s.callToolWithAuth(ctx, mcpClient, userID, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "create_tweet",
			Arguments: map[string]any{
				"user_id": fmt.Sprintf("%d", userID),
				"content": content,
			},
		},
	})
	if err != nil {
		s.resetMCPClient() // 异常断连重置
		return "", fmt.Errorf("call create_tweet tool failed: %w", err)
	}

	resultText := extractTextFromToolResult(toolResult)
	return resultText, nil
}

// ========================== 模式四：多 Agent 协作写推文 ==========================

// MultiAgentPublishTwitter 模式四：多 Agent 协作写推文
func (s *AgentService) MultiAgentPublishTwitter(ctx context.Context, userID uint64, domain string, authorUserID uint64, styleRatio float32, referenceTweetIDs []uint64, content string) (*ChatResult, error) {

	// 模式四每次都创建新对话（独立的创作任务）
	dialogue, err := s.getOrCreateDialogue(ctx, userID, "", content, repository.ModeMulti)
	if err != nil {
		return nil, err
	}

	mcpClient, _, err := s.getOrInitMCPClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("init mcp client failed: %w", err)
	}

	// ======== Agent 1: Search Agent 查阅相关领域推文 ========
	searchResult, err := s.callToolWithAuth(ctx, mcpClient, userID, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "hybrid_search_tweets",
			Arguments: map[string]any{
				"query": domain,
				"size":  float64(5),
			},
		},
	})
	if err != nil {
		s.resetMCPClient() // 异常断连重置
		return nil, fmt.Errorf("search agent failed: %w", err)
	}
	domainTweets := extractTextFromToolResult(searchResult)

	// ======== Agent 2: Style Agent 分析作者风格 ========
	// 根据 style_ratio 计算读取推文数量，比如 0.7 对应读 35 条（最多50条）
	styleLimit := int(styleRatio * 50)
	if styleLimit < 1 {
		styleLimit = 1
	}

	styleResult, err := s.callToolWithAuth(ctx, mcpClient, userID, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "get_user_tweets",
			Arguments: map[string]any{
				"user_id": fmt.Sprintf("%d", authorUserID),
				"limit":   float64(styleLimit),
			},
		},
	})
	if err != nil {
		s.resetMCPClient() // 异常断连重置
		return nil, fmt.Errorf("style agent failed: %w", err)
	}
	authorTweets := extractTextFromToolResult(styleResult)

	// 获取用户指定的参考推文
	referenceTweets := ""
	if len(referenceTweetIDs) > 0 {
		ids := make([]string, len(referenceTweetIDs))
		for i, id := range referenceTweetIDs {
			ids[i] = fmt.Sprintf("%d", id)
		}
		refResult, err := s.callToolWithAuth(ctx, mcpClient, userID, mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name: "get_tweets_by_ids",
				Arguments: map[string]any{
					"tweet_ids": strings.Join(ids, ","),
				},
			},
		})
		if err == nil {
			referenceTweets = extractTextFromToolResult(refResult)
		} else {
			s.resetMCPClient() // 异常断连重置
		}
	}

	// ======== Agent 3: Writer Agent 综合生成推文 ========
	prompt := fmt.Sprintf(`你是一个专业的推文写作助手，现在需要综合以下信息写一条推文：

【用户要求】
%s

【领域参考推文】（来自 %s 领域的热门推文，供参考内容方向）
%s

【目标作者风格】（以下是目标作者的历史推文，请模仿其写作风格）
%s

【用户指定参考推文】（以下是用户特别指定的推文，请重点参考）
%s

请综合以上信息，生成3个推文草稿，要求：
1. 内容方向贴合用户要求和领域参考
2. 写作风格模仿目标作者
3. 每条不超过280字
4. 格式如下：

【草稿一】
内容...

【草稿二】
内容...

【草稿三】
内容...`,
		content, domain, domainTweets, authorTweets, referenceTweets)

	resp, err := s.llmClient.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: s.chatModel,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleSystem,
				Content: "你是一个专业的推文写作助手，擅长模仿不同作者的写作风格。",
			},
			{
				Role:    openai.ChatMessageRoleUser,
				Content: prompt,
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("writer agent failed: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("empty response from writer agent")
	}

	aiResponse := resp.Choices[0].Message.Content

	// 持久化消息，附带 metadata 记录调用参数
	metadata := map[string]any{
		"domain":              domain,
		"author_user_id":      authorUserID,
		"style_ratio":         styleRatio,
		"reference_tweet_ids": referenceTweetIDs,
	}
	if err := s.saveUserAndAssistantMessages(ctx, dialogue.ID, userID, content, aiResponse, metadata); err != nil {
		logger.Error(ctx, "save messages failed", zap.Error(err))
	}

	return &ChatResult{
		DialogueID: dialogue.ID.Hex(),
		Response:   aiResponse,
	}, nil
}

// ========================== MCP 辅助方法 ==========================

// getOrInitMCPClient 并发安全地获取已有的长连接客户端及缓存的 Tools 列表
func (s *AgentService) getOrInitMCPClient(ctx context.Context) (*client.Client, []mcp.Tool, error) {
	s.mcpMu.RLock()
	if s.mcpClient != nil {
		cli := s.mcpClient
		tools := s.mcpTools
		s.mcpMu.RUnlock()
		return cli, tools, nil
	}
	s.mcpMu.RUnlock()

	s.mcpMu.Lock()
	defer s.mcpMu.Unlock()

	// 双重检查
	if s.mcpClient != nil {
		return s.mcpClient, s.mcpTools, nil
	}

	logger.Info(ctx, "initializing MCP long-connection client", zap.String("addr", s.mcpAddr))
	mcpClient, err := client.NewSSEMCPClient(fmt.Sprintf("http://%s/sse", s.mcpAddr))
	if err != nil {
		return nil, nil, fmt.Errorf("create mcp client failed: %w", err)
	}

	if err := mcpClient.Start(ctx); err != nil {
		return nil, nil, fmt.Errorf("mcp client start failed: %w", err)
	}

	// 初始化握手
	if _, err := mcpClient.Initialize(ctx, mcp.InitializeRequest{}); err != nil {
		mcpClient.Close()
		return nil, nil, fmt.Errorf("mcp initialize failed: %w", err)
	}

	// 获取所有可用 Tools 并缓存
	toolsResp, err := mcpClient.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		mcpClient.Close()
		return nil, nil, fmt.Errorf("list tools failed: %w", err)
	}

	s.mcpClient = mcpClient
	s.mcpTools = toolsResp.Tools
	return s.mcpClient, s.mcpTools, nil
}

// resetMCPClient 清理失效的长连接客户端并置空，以便下次调用时重新握手
func (s *AgentService) resetMCPClient() {
	s.mcpMu.Lock()
	defer s.mcpMu.Unlock()
	if s.mcpClient != nil {
		s.mcpClient.Close()
		s.mcpClient = nil
		s.mcpTools = nil
	}
}

// callToolWithAuth 封装 CallTool 请求，强制在客户端注入与校验 user_id，实现身份鉴权隔离
func (s *AgentService) callToolWithAuth(ctx context.Context, mcpClient *client.Client, userID uint64, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args, ok := req.Params.Arguments.(map[string]any)
	if !ok {
		args = make(map[string]any)
	}

	// 针对敏感写操作 create_tweet，强制绑定当前用户的 userID，防止 LLM 参数注入越权
	if req.Params.Name == "create_tweet" {
		args["user_id"] = fmt.Sprintf("%d", userID)
	}

	req.Params.Arguments = args
	return mcpClient.CallTool(ctx, req)
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

// extractTextFromToolResult 提取 Tool 返回的文本内容
func extractTextFromToolResult(result *mcp.CallToolResult) string {
	text := ""
	for _, c := range result.Content {
		if textContent, ok := c.(mcp.TextContent); ok {
			text += textContent.Text
		}
	}
	return text
}
