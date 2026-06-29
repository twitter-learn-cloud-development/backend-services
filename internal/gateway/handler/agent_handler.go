package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	aiAgentv1 "twitter-clone/api/aiAgent/v1"
	"twitter-clone/internal/gateway/middleware"
)

// parseInterfaceToUint64 辅助函数：将 interface{} 兼容转换为 uint64
func parseInterfaceToUint64(val interface{}) (uint64, error) {
	if val == nil {
		return 0, nil
	}
	switch v := val.(type) {
	case float64:
		return uint64(v), nil
	case string:
		if v == "" || v == "0" {
			return 0, nil
		}
		if len(v) == 24 {
			return 0, nil
		}
		return strconv.ParseUint(v, 10, 64)
	default:
		return 0, strconv.ErrSyntax
	}
}

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
	Content     string      `json:"content" binding:"required"`
	DialogueID  interface{} `json:"dialogue_id"`
	DialogueKey string      `json:"dialogue_key"`
	ModelKindID interface{} `json:"model_kind_id"`
}

// ConfirmPublishRequest 确认发布请求
type ConfirmPublishRequest struct {
	Content string `json:"content" binding:"required"`
}

// MultiAgentPublishRequest 多 Agent 写推文请求
type MultiAgentPublishRequest struct {
	Domain            string   `json:"domain" binding:"required"`
	AuthorUserID      string   `json:"author_user_id" binding:"required"`
	StyleRatio        float32  `json:"style_ratio" binding:"required"`
	ReferenceTweetIDs []string `json:"reference_tweet_ids"`
	DialogueKey       string   `json:"dialogue_key"`
	Content           string   `json:"content" binding:"required"`
}

type WorkflowSaveRequest struct {
	Name    string          `json:"name" binding:"required"`
	DSLJSON string          `json:"dsl_json"`
	DSL     json.RawMessage `json:"dsl"`
}

type WorkflowRunRequest struct {
	InputJSON string          `json:"input_json"`
	Input     json.RawMessage `json:"input"`
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

	var dialogueID uint64
	if req.DialogueID != nil {
		var err error
		dialogueID, err = parseInterfaceToUint64(req.DialogueID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid dialogue_id format"})
			return
		}
	}

	var modelKindID uint64
	if req.ModelKindID != nil {
		var err error
		modelKindID, err = parseInterfaceToUint64(req.ModelKindID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid model_kind_id format"})
			return
		}
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	resp, err := h.agentClient.CallApiOfAi(ctx, &aiAgentv1.CallApiOfAiRequest{
		UserId:      userID,
		ModelKindId: modelKindID,
		MainContent: &aiAgentv1.MainContent{
			UserId:      userID,
			DialogueId:  dialogueID,
			DialogueKey: req.DialogueKey,
			Content:     req.Content,
		},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"response":     resp.Response,
		"dialogue_key": resp.DialogueKey,
	})
}

// ConsultContentRequest 推文查询请求
type ConsultContentRequest struct {
	Content     string      `json:"content" binding:"required"`
	DialogueID  interface{} `json:"dialogue_id"`
	DialogueKey string      `json:"dialogue_key"`
	ModelKindID interface{} `json:"model_kind_id"`
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

	var dialogueID uint64
	if req.DialogueID != nil {
		var err error
		dialogueID, err = parseInterfaceToUint64(req.DialogueID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid dialogue_id format"})
			return
		}
	}

	var modelKindID uint64
	if req.ModelKindID != nil {
		var err error
		modelKindID, err = parseInterfaceToUint64(req.ModelKindID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid model_kind_id format"})
			return
		}
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	resp, err := h.agentClient.ConsultContent(ctx, &aiAgentv1.ConsultContentRequest{
		UserId:      userID,
		ModelKindId: modelKindID,
		MainContent: &aiAgentv1.MainContent{
			UserId:      userID,
			DialogueId:  dialogueID,
			DialogueKey: req.DialogueKey,
			Content:     req.Content,
		},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	tweetList := make([]gin.H, len(resp.TweetList))
	for i, t := range resp.TweetList {
		tweetList[i] = gin.H{
			"tweet_id": strconv.FormatUint(t.TweetId, 10), // 👈 无损转换为字符串输出
			"url":      t.Url,
			"summary":  t.Summary,
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"response":     resp.Response,
		"tweet_list":   tweetList,
		"dialogue_key": resp.DialogueKey,
	})
}

// AssistPublishRequest 协作写推文请求
type AssistPublishRequest struct {
	Content     string      `json:"content" binding:"required"`
	DialogueID  interface{} `json:"dialogue_id"`
	DialogueKey string      `json:"dialogue_key"`
	ModelKindID interface{} `json:"model_kind_id"`
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

	var dialogueID uint64
	if req.DialogueID != nil {
		var err error
		dialogueID, err = parseInterfaceToUint64(req.DialogueID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid dialogue_id format"})
			return
		}
	}

	var modelKindID uint64
	if req.ModelKindID != nil {
		var err error
		modelKindID, err = parseInterfaceToUint64(req.ModelKindID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid model_kind_id format"})
			return
		}
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	resp, err := h.agentClient.AssistPublishTwitter(ctx, &aiAgentv1.AssistPublishTwitterRequest{
		UserId:      userID,
		ModelKindId: modelKindID,
		MainContent: &aiAgentv1.MainContent{
			UserId:      userID,
			DialogueId:  dialogueID,
			DialogueKey: req.DialogueKey,
			Content:     req.Content,
		},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 🎯 转换 Tweet 内的 ID 避免 JS 精度截断
	tweetList := make([]gin.H, len(resp.TweetList))
	for i, t := range resp.TweetList {
		tweetList[i] = gin.H{
			"id":            strconv.FormatUint(t.Id, 10),
			"user_id":       strconv.FormatUint(t.UserId, 10),
			"content":       t.Content,
			"media_urls":    t.MediaUrls,
			"type":          t.Type,
			"visible_type":  t.VisibleType,
			"created_at":    t.CreatedAt,
			"updated_at":    t.UpdatedAt,
			"like_count":    t.LikeCount,
			"comment_count": t.CommentCount,
			"share_count":   t.ShareCount,
			"is_liked":      t.IsLiked,
			"parent_id":     strconv.FormatUint(t.ParentId, 10),
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"response":     resp.Response,
		"tweet_list":   tweetList,
		"dialogue_key": resp.DialogueKey,
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
		"tweet_id": strconv.FormatUint(resp.TweetId, 10),
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

	// 解析 author_user_id 字符串为 uint64
	authorUserID, err := strconv.ParseUint(req.AuthorUserID, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid author_user_id format"})
		return
	}

	// 解析 reference_tweet_ids 字符串数组为 uint64 数组
	var refTweetIDs []uint64
	if len(req.ReferenceTweetIDs) > 0 {
		refTweetIDs = make([]uint64, len(req.ReferenceTweetIDs))
		for i, idStr := range req.ReferenceTweetIDs {
			id, err := strconv.ParseUint(idStr, 10, 64)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid reference_tweet_id format"})
				return
			}
			refTweetIDs[i] = id
		}
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 120*time.Second)
	defer cancel()

	resp, err := h.agentClient.MultiAgentPublishTwitter(ctx, &aiAgentv1.MultiAgentPublishTwitterRequest{
		UserId:            userID,
		Domain:            req.Domain,
		AuthorUserId:      authorUserID,
		StyleRatio:        req.StyleRatio,
		ReferenceTweetIds: refTweetIDs,
		DialogueKey:       req.DialogueKey,
		Content:           req.Content,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"response":     resp.Response,
		"dialogue_key": resp.DialogueKey,
	})
}

// CreateWorkflow 保存自定义工作流
// POST /api/v1/agent/workflows
func (h *AgentHandler) CreateWorkflow(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req WorkflowSaveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	dslJSON, ok := normalizeRawJSON(req.DSLJSON, req.DSL)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing dsl or dsl_json"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	resp, err := h.agentClient.CreateWorkflow(ctx, &aiAgentv1.CreateWorkflowRequest{
		UserId:  userID,
		Name:    req.Name,
		DslJson: dslJSON,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"workflow": workflowDetailToJSON(resp.Workflow),
	})
}

// UpdateWorkflow 更新自定义工作流
// PUT /api/v1/agent/workflows/:id
func (h *AgentHandler) UpdateWorkflow(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	workflowID := c.Param("id")
	var req WorkflowSaveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	dslJSON, ok := normalizeRawJSON(req.DSLJSON, req.DSL)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing dsl or dsl_json"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	resp, err := h.agentClient.UpdateWorkflow(ctx, &aiAgentv1.UpdateWorkflowRequest{
		UserId:     userID,
		WorkflowId: workflowID,
		Name:       req.Name,
		DslJson:    dslJSON,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"workflow": workflowDetailToJSON(resp.Workflow),
	})
}

// ListWorkflows 获取当前用户工作流列表
// GET /api/v1/agent/workflows
func (h *AgentHandler) ListWorkflows(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 50 {
		pageSize = 20
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.agentClient.ListWorkflows(ctx, &aiAgentv1.ListWorkflowsRequest{
		UserId:   userID,
		Page:     uint32(page),
		PageSize: uint32(pageSize),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	workflows := make([]gin.H, 0, len(resp.Workflows))
	for _, workflow := range resp.Workflows {
		workflows = append(workflows, gin.H{
			"workflow_id": workflow.WorkflowId,
			"user_id":     strconv.FormatUint(workflow.UserId, 10),
			"name":        workflow.Name,
			"created_at":  workflow.CreatedAt,
			"updated_at":  workflow.UpdatedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"workflows": workflows,
		"total":     resp.Total,
	})
}

// GetWorkflow 获取单个工作流 DSL
// GET /api/v1/agent/workflows/:id
func (h *AgentHandler) GetWorkflow(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.agentClient.GetWorkflow(ctx, &aiAgentv1.GetWorkflowRequest{
		UserId:     userID,
		WorkflowId: c.Param("id"),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"workflow": workflowDetailToJSON(resp.Workflow),
	})
}

// RunWorkflow 执行工作流
// POST /api/v1/agent/workflows/:id/run
func (h *AgentHandler) RunWorkflow(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req WorkflowRunRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	inputJSON, ok := normalizeRawJSON(req.InputJSON, req.Input)
	if !ok && (req.InputJSON != "" || len(req.Input) > 0) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid input or input_json"})
		return
	}
	if inputJSON == "" {
		inputJSON = "{}"
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 120*time.Second)
	defer cancel()

	resp, err := h.agentClient.RunWorkflow(ctx, &aiAgentv1.RunWorkflowRequest{
		UserId:     userID,
		WorkflowId: c.Param("id"),
		InputJson:  inputJSON,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"run":          workflowRunToJSON(resp.Run),
		"dialogue_key": resp.DialogueKey,
		"response":     resp.Response,
	})
}

// GetWorkflowRun 获取工作流运行记录
// GET /api/v1/agent/workflow-runs/:id
func (h *AgentHandler) GetWorkflowRun(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.agentClient.GetWorkflowRun(ctx, &aiAgentv1.GetWorkflowRunRequest{
		UserId: userID,
		RunId:  c.Param("id"),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"run": workflowRunToJSON(resp.Run),
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

	dialogues := make([]gin.H, len(resp.RepositoryDialogueList))
	for i, d := range resp.RepositoryDialogueList {
		dialogueKey := d.DialogueKey
		if dialogueKey == "" {
			dialogueKey = strconv.FormatUint(d.Id, 10)
		}
		dialogues[i] = gin.H{
			"id":           dialogueKey,
			"legacy_id":    strconv.FormatUint(d.Id, 10),
			"dialogue_key": dialogueKey,
			"user_id":      strconv.FormatUint(d.UserId, 10),
			"title":        d.Title,
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"code":                     resp.Code,
		"msg":                      resp.Msg,
		"repository_dialogue_list": dialogues,
	})
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
	dialogueID, err := strconv.ParseUint(dialogueIDStr, 10, 64)
	dialogueKey := ""
	if err != nil {
		dialogueID = 0
		dialogueKey = dialogueIDStr
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.agentClient.GetDialogueDetail(ctx, &aiAgentv1.GetDialogueDetailRequest{
		UserId:      userID,
		DialogueId:  dialogueID,
		DialogueKey: dialogueKey,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	messages := make([]gin.H, len(resp.Messages))
	for i, m := range resp.Messages {
		messages[i] = gin.H{
			"id":           strconv.FormatUint(m.Id, 10),
			"user_id":      strconv.FormatUint(m.UserId, 10),
			"dialogue_id":  strconv.FormatUint(m.DialogueId, 10),
			"dialogue_key": m.DialogueKey,
			"question":     m.Question,
			"response":     m.Response,
			"role":         m.Role,
			"content":      m.Content,
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"code":     resp.Code,
		"msg":      resp.Msg,
		"messages": messages,
	})
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

// AnalyzeAlert 告警根因分析透传
func (h *AgentHandler) AnalyzeAlert(ctx context.Context, req *aiAgentv1.AnalyzeAlertRequest) (*aiAgentv1.AnalyzeAlertResponse, error) {
	return h.agentClient.AnalyzeAlert(ctx, req)
}

func normalizeRawJSON(jsonString string, raw json.RawMessage) (string, bool) {
	if jsonString != "" {
		if !json.Valid([]byte(jsonString)) {
			return "", false
		}
		return jsonString, true
	}
	if len(raw) == 0 {
		return "", false
	}
	if !json.Valid(raw) {
		return "", false
	}
	return string(raw), true
}

func workflowDetailToJSON(workflow *aiAgentv1.WorkflowDetail) gin.H {
	if workflow == nil {
		return gin.H{}
	}

	var dsl any
	if workflow.DslJson != "" {
		_ = json.Unmarshal([]byte(workflow.DslJson), &dsl)
	}
	return gin.H{
		"workflow_id": workflow.WorkflowId,
		"user_id":     strconv.FormatUint(workflow.UserId, 10),
		"name":        workflow.Name,
		"dsl":         dsl,
		"dsl_json":    workflow.DslJson,
		"created_at":  workflow.CreatedAt,
		"updated_at":  workflow.UpdatedAt,
	}
}

func workflowRunToJSON(run *aiAgentv1.WorkflowRun) gin.H {
	if run == nil {
		return gin.H{}
	}

	var input any
	var output any
	if run.InputJson != "" {
		_ = json.Unmarshal([]byte(run.InputJson), &input)
	}
	if run.OutputJson != "" {
		_ = json.Unmarshal([]byte(run.OutputJson), &output)
	}
	return gin.H{
		"run_id":        run.RunId,
		"workflow_id":   run.WorkflowId,
		"user_id":       strconv.FormatUint(run.UserId, 10),
		"status":        run.Status,
		"input":         input,
		"input_json":    run.InputJson,
		"output":        output,
		"output_json":   run.OutputJson,
		"error_message": run.ErrorMessage,
		"started_at":    run.StartedAt,
		"finished_at":   run.FinishedAt,
	}
}
