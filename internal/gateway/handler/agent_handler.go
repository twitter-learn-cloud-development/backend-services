package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	aiAgentv1 "twitter-clone/api/aiAgent/v1"
	"twitter-clone/internal/gateway/middleware"
)

// AgentHandler AI Agent 处理器
type AgentHandler struct {
	agentClient aiAgentv1.AiAgentServiceClient
}

// NewAgentHandler 创建 Agent 处理器
func NewAgentHandler(agentClient aiAgentv1.AiAgentServiceClient) *AgentHandler {
	return &AgentHandler{agentClient: agentClient}
}

// CallApiOfAiRequest 直接对话请求
type CallApiOfAiRequest struct {
	Content     string `json:"content" binding:"required"`
	DialogueID  uint64 `json:"dialogue_id"`
	ModelKindID uint64 `json:"model_kind_id"`
}

// CallApiOfAi 模式一：直接 AI 对话
// POST /api/v1/agent/chat
func (h *AgentHandler) CallApiOfAi(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req CallApiOfAiRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	resp, err := h.agentClient.CallApiOfAi(ctx, &aiAgentv1.CallApiOfAiRequest{
		UserId:      userID,
		ModelKindId: req.ModelKindID,
		MainContent: &aiAgentv1.MainContent{
			UserId:     userID,
			DialogueId: req.DialogueID,
			Content:    req.Content,
		},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"response": resp.Response,
	})
}

// ConsultContentRequest 推文查询请求
type ConsultContentRequest struct {
	Content     string `json:"content" binding:"required"`
	DialogueID  uint64 `json:"dialogue_id"`
	ModelKindID uint64 `json:"model_kind_id"`
}

// ConsultContent 模式二：语义搜索推文和作者
// POST /api/v1/agent/consult
func (h *AgentHandler) ConsultContent(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req ConsultContentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	resp, err := h.agentClient.ConsultContent(ctx, &aiAgentv1.ConsultContentRequest{
		UserId:      userID,
		ModelKindId: req.ModelKindID,
		MainContent: &aiAgentv1.MainContent{
			UserId:     userID,
			DialogueId: req.DialogueID,
			Content:    req.Content,
		},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	tweetList := make([]gin.H, len(resp.TweetList))
	for i, t := range resp.TweetList {
		tweetList[i] = gin.H{
			"tweet_id": t.TweetId,
			"url":      t.Url,
			"summary":  t.Summary,
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"response":   resp.Response,
		"tweet_list": tweetList,
	})
}

// AssistPublishRequest 协作写推文请求
type AssistPublishRequest struct {
	Content     string `json:"content" binding:"required"`
	DialogueID  uint64 `json:"dialogue_id"`
	ModelKindID uint64 `json:"model_kind_id"`
}

// AssistPublishTwitter 模式三：协助构建推文
// POST /api/v1/agent/assist
func (h *AgentHandler) AssistPublishTwitter(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req AssistPublishRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	resp, err := h.agentClient.AssistPublishTwitter(ctx, &aiAgentv1.AssistPublishTwitterRequest{
		UserId:      userID,
		ModelKindId: req.ModelKindID,
		MainContent: &aiAgentv1.MainContent{
			UserId:     userID,
			DialogueId: req.DialogueID,
			Content:    req.Content,
		},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"response":   resp.Response,
		"tweet_list": resp.TweetList,
	})
}
