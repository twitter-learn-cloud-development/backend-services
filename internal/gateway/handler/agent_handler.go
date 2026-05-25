package handler

import (
	"context"
	"net/http"
	"strconv"
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

// ConfirmPublishRequest 确认发布请求
type ConfirmPublishRequest struct {
	Content string `json:"content" binding:"required"`
}

// MultiAgentPublishRequest 多 Agent 写推文请求
type MultiAgentPublishRequest struct {
	Domain            string   `json:"domain" binding:"required"`
	AuthorUserID      uint64   `json:"author_user_id" binding:"required"`
	StyleRatio        float32  `json:"style_ratio" binding:"required"`
	ReferenceTweetIDs []uint64 `json:"reference_tweet_ids"`
	Content           string   `json:"content" binding:"required"`
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

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
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

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
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

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
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

// ConfirmPublishTwitter 模式三第二阶段：确认发布推文
// POST /api/v1/agent/confirm
func (h *AgentHandler) ConfirmPublishTwitter(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req ConfirmPublishRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	resp, err := h.agentClient.ConfirmPublishTwitter(ctx, &aiAgentv1.ConfirmPublishTwitterRequest{
		UserId:  userID,
		Content: req.Content,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"response": resp.Response,
		"tweet_id": resp.TweetId,
	})
}

// MultiAgentPublishTwitter 模式四：多 Agent 协作写推文
// POST /api/v1/agent/multi
func (h *AgentHandler) MultiAgentPublishTwitter(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req MultiAgentPublishRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 120*time.Second)
	defer cancel()

	resp, err := h.agentClient.MultiAgentPublishTwitter(ctx, &aiAgentv1.MultiAgentPublishTwitterRequest{
		UserId:            userID,
		Domain:            req.Domain,
		AuthorUserId:      req.AuthorUserID,
		StyleRatio:        req.StyleRatio,
		ReferenceTweetIds: req.ReferenceTweetIDs,
		Content:           req.Content,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"response": resp.Response,
	})
}

// ========================== 对话与历史 ==========================

// GetRepositoryDialogue 获取历史对话列表
// GET /api/v1/agent/dialogues?page=1&page_size=20
func (h *AgentHandler) GetRepositoryDialogue(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.agentClient.GetRepositoryDialogue(ctx, &aiAgentv1.GetRepositoryDialogueRequest{
		UserId: userID,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetDialogueDetail 获取特定对话的消息记录
// GET /api/v1/agent/dialogues/:id/messages
func (h *AgentHandler) GetDialogueDetail(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	dialogueIDStr := c.Param("id")
	// 注意这里前端可能会传 hex 字符串，但由于 pb 定义是 uint64，我们在 service 层做了兼容
	dialogueID, err := strconv.ParseUint(dialogueIDStr, 10, 64)
	if err != nil {
		// 如果前端传的是 mongo 的 hex string，尝试兼容传递
		// 暂时为了 pb 兼容，如果解析失败传 0，后端在 GetDialogueDetail 中处理
		// 实际上由于我们之前的修改，gRPC proto 中的 dialogue_id 是 uint64，但我们其实也可以把 hex string 传给 proto 里的其他字段。
		// 这里的处理：我们将 dialogue_id 作为 uint64 传输，如果是 hex string 无法解析，这里会报错。
		// 因为我们现在采用的是后 8 bytes 截取的伪 ObjectID 方案，所以前端可以传 uint64。
		// 在这里强行解析为 uint64 即可。
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid dialogue_id format"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.agentClient.GetDialogueDetail(ctx, &aiAgentv1.GetDialogueDetailRequest{
		UserId:     userID,
		DialogueId: dialogueID,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// ========================== 模型与文件 ==========================

// GetModelDetailedInformation 获取可用模型及支持的文件类型
// GET /api/v1/agent/models
func (h *AgentHandler) GetModelDetailedInformation(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.agentClient.GetModelDetailedInformation(ctx, &aiAgentv1.GetModelDetailedInformationRequest{
		UserId: userID,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// AnalysisFiles 解析上传的文件
// POST /api/v1/agent/files/analysis
func (h *AgentHandler) AnalysisFiles(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	fileKindIDStr := c.PostForm("file_kind_id")
	fileKindID, _ := strconv.ParseUint(fileKindIDStr, 10, 64)
	if fileKindID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing or invalid file_kind_id"})
		return
	}

	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing file"})
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "open file failed"})
		return
	}
	defer file.Close()

	fileContent := make([]byte, fileHeader.Size)
	if _, err := file.Read(fileContent); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "read file failed"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	resp, err := h.agentClient.AnalysisFiles(ctx, &aiAgentv1.AnalysisFilesRequest{
		UserId:      userID,
		FileKindId:  fileKindID,
		FileContent: fileContent,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}
