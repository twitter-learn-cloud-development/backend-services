package grpc

import (
	"context"
	"fmt"
	"log"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	aiAgentv1 "twitter-clone/api/aiAgent/v1"
	"twitter-clone/internal/module/agent/repository"
	"twitter-clone/internal/module/agent/service"
)

// AgentServer gRPC 服务器
type AgentServer struct {
	aiAgentv1.UnimplementedAiAgentServiceServer
	svc *service.AgentService
}

// NewAgentServer 创建 Agent gRPC 服务器
func NewAgentServer(svc *service.AgentService) *AgentServer {
	return &AgentServer{svc: svc}
}

// CallApiOfAi 模式一：直接调用 AI 对话
func (s *AgentServer) CallApiOfAi(ctx context.Context, req *aiAgentv1.CallApiOfAiRequest) (*aiAgentv1.CallApiOfAiResponse, error) {
	log.Printf("gRPC: CallApiOfAi - user_id=%d, dialogue_id=%d", req.UserId, req.MainContent.DialogueId)

	result, err := s.svc.CallApiOfAi(ctx, req.UserId, req.MainContent.DialogueId, req.MainContent.DialogueKey, req.MainContent.Content)
	if err != nil {
		log.Printf("❌ CallApiOfAi error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to call ai: %v", err)
	}

	return &aiAgentv1.CallApiOfAiResponse{
		Code:        200,
		Msg:         "success",
		Response:    result.Response,
		DialogueKey: result.DialogueID,
	}, nil
}

// ConsultContent 模式二：通过对话查询相关推文和作者
func (s *AgentServer) ConsultContent(ctx context.Context, req *aiAgentv1.ConsultContentRequest) (*aiAgentv1.ConsultContentResponse, error) {
	log.Printf("gRPC: ConsultContent - user_id=%d, dialogue_id=%d", req.UserId, req.MainContent.DialogueId)

	result, err := s.svc.ConsultContent(ctx, req.UserId, req.MainContent.DialogueId, req.MainContent.DialogueKey, req.MainContent.Content)
	if err != nil {
		log.Printf("❌ ConsultContent error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to consult content: %v", err)
	}

	protoTweetList := make([]*aiAgentv1.TweetResult, len(result.Tweets))
	for i, t := range result.Tweets {
		protoTweetList[i] = &aiAgentv1.TweetResult{
			TweetId: t.TweetID,
			Url:     t.URL,
			Summary: t.Summary,
		}
	}

	return &aiAgentv1.ConsultContentResponse{
		Code:        200,
		Msg:         "success",
		Response:    result.Response,
		TweetList:   protoTweetList,
		DialogueKey: result.DialogueID,
	}, nil
}

// AssistPublishTwitter 模式三：协助构建推文
func (s *AgentServer) AssistPublishTwitter(ctx context.Context, req *aiAgentv1.AssistPublishTwitterRequest) (*aiAgentv1.AssistPublishTwitterResponse, error) {
	log.Printf("gRPC: AssistPublishTwitter - user_id=%d, dialogue_id=%d", req.UserId, req.MainContent.DialogueId)

	result, err := s.svc.AssistPublishTwitter(ctx, req.UserId, req.MainContent.DialogueId, req.MainContent.DialogueKey, req.MainContent.Content)
	if err != nil {
		log.Printf("❌ AssistPublishTwitter error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to assist publish twitter: %v", err)
	}
	return &aiAgentv1.AssistPublishTwitterResponse{
		Code:        200,
		Msg:         "success",
		Response:    result.Response,
		DialogueKey: result.DialogueID,
	}, nil
}

// ConfirmPublishTwitter 模式三第二阶段：确认发布推文
func (s *AgentServer) ConfirmPublishTwitter(ctx context.Context, req *aiAgentv1.ConfirmPublishTwitterRequest) (*aiAgentv1.ConfirmPublishTwitterResponse, error) {
	log.Printf("gRPC: ConfirmPublishTwitter - user_id=%d", req.UserId)

	result, err := s.svc.ConfirmPublishTwitter(ctx, req.UserId, req.Content)
	if err != nil {
		log.Printf("❌ ConfirmPublishTwitter error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to confirm publish twitter: %v", err)
	}

	return &aiAgentv1.ConfirmPublishTwitterResponse{
		Code:     200,
		Msg:      "success",
		Response: result,
	}, nil
}

// GetRepositoryDialogue 获取历史对话列表
func (s *AgentServer) GetRepositoryDialogue(ctx context.Context, req *aiAgentv1.GetRepositoryDialogueRequest) (*aiAgentv1.GetRepositoryDialogueResponse, error) {
	log.Printf("gRPC: GetRepositoryDialogue - user_id=%d", req.UserId)

	dialogues, _, err := s.svc.ListDialogues(ctx, req.UserId, 1, 50)
	if err != nil {
		log.Printf("❌ GetRepositoryDialogue error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to get dialogues: %v", err)
	}

	protoDialogues := make([]*aiAgentv1.RepositoryDialogue, len(dialogues))
	for i, d := range dialogues {
		protoDialogues[i] = &aiAgentv1.RepositoryDialogue{
			Id:          dialogueObjectIDToUint64(d.ID),
			UserId:      d.UserID,
			Title:       d.Title,
			DialogueKey: d.ID.Hex(),
		}
	}

	return &aiAgentv1.GetRepositoryDialogueResponse{
		Code:                   200,
		Msg:                    "success",
		RepositoryDialogueList: protoDialogues,
	}, nil
}

// GetDialogueDetail 获取某个历史对话的详细消息记录
func (s *AgentServer) GetDialogueDetail(ctx context.Context, req *aiAgentv1.GetDialogueDetailRequest) (*aiAgentv1.GetDialogueDetailResponse, error) {
	log.Printf("gRPC: GetDialogueDetail - user_id=%d, dialogue_id=%d", req.UserId, req.DialogueId)

	// 将 uint64 dialogue_id 转回 hex 格式
	dialogueIDHex := req.DialogueKey
	if dialogueIDHex == "" {
		dialogueIDHex = uint64ToObjectIDHex(req.DialogueId)
	}

	messages, err := s.svc.GetDialogueMessages(ctx, req.UserId, dialogueIDHex)
	if err != nil {
		log.Printf("❌ GetDialogueDetail error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to get dialogue detail: %v", err)
	}

	protoMessages := make([]*aiAgentv1.RepositoryContentList, len(messages))
	for i, m := range messages {
		question := ""
		response := ""
		if m.Role == "user" {
			question = m.Content
		} else if m.Role == "assistant" {
			response = m.Content
		}

		protoMessages[i] = &aiAgentv1.RepositoryContentList{
			Id:          dialogueObjectIDToUint64(m.ID),
			UserId:      m.UserID,
			DialogueId:  dialogueObjectIDToUint64(m.DialogueID),
			Question:    question,
			Response:    response,
			DialogueKey: m.DialogueID.Hex(),
			Role:        string(m.Role),
			Content:     m.Content,
		}
	}

	return &aiAgentv1.GetDialogueDetailResponse{
		Code:     200,
		Msg:      "success",
		Messages: protoMessages,
	}, nil
}

// GetModelDetailedInformation 获取模型初始化详细信息
func (s *AgentServer) GetModelDetailedInformation(ctx context.Context, req *aiAgentv1.GetModelDetailedInformationRequest) (*aiAgentv1.GetModelDetailedInformationResponse, error) {
	log.Printf("gRPC: GetModelDetailedInformation - user_id=%d", req.UserId)

	models := s.svc.GetModelInfo()
	protoModels := make([]*aiAgentv1.ModelKind, len(models))
	for i, m := range models {
		fileKinds := make([]*aiAgentv1.FileKind, len(m.FileKinds))
		for j, fk := range m.FileKinds {
			fileKinds[j] = &aiAgentv1.FileKind{
				Id:   fk.ID,
				Name: fk.Name,
			}
		}
		protoModels[i] = &aiAgentv1.ModelKind{
			Id:           m.ID,
			Name:         m.Name,
			Description:  m.Description,
			MaxTokens:    m.MaxTokens,
			FileKindList: fileKinds,
		}
	}

	return &aiAgentv1.GetModelDetailedInformationResponse{
		Code:          200,
		Msg:           "success",
		ModelKindList: protoModels,
	}, nil
}

// AnalysisFiles 解析前端文件（PDF/TXT/图片）
func (s *AgentServer) AnalysisFiles(ctx context.Context, req *aiAgentv1.AnalysisFilesRequest) (*aiAgentv1.AnalysisFilesResponse, error) {
	log.Printf("gRPC: AnalysisFiles - user_id=%d, file_kind_id=%d, file_size=%d", req.UserId, req.FileKindId, len(req.FileContent))

	result, err := s.svc.AnalysisFile(ctx, req.UserId, req.FileKindId, req.FileContent)
	if err != nil {
		log.Printf("❌ AnalysisFiles error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to analysis file: %v", err)
	}

	return &aiAgentv1.AnalysisFilesResponse{
		Code:          200,
		Msg:           "success",
		ParsedContent: result.ParsedContent,
		FileKey:       result.FileKey,
	}, nil
}

// MultiAgentPublishTwitter 模式四：多 Agent 协作写推文
func (s *AgentServer) MultiAgentPublishTwitter(ctx context.Context, req *aiAgentv1.MultiAgentPublishTwitterRequest) (*aiAgentv1.MultiAgentPublishTwitterResponse, error) {
	log.Printf("gRPC: MultiAgentPublishTwitter - user_id=%d, domain=%s", req.UserId, req.Domain)

	result, err := s.svc.MultiAgentPublishTwitter(ctx, req.UserId, req.Domain, req.AuthorUserId, req.StyleRatio, req.ReferenceTweetIds, req.DialogueKey, req.Content)
	if err != nil {
		log.Printf("❌ MultiAgentPublishTwitter error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to multi agent publish twitter: %v", err)
	}

	return &aiAgentv1.MultiAgentPublishTwitterResponse{
		Code:        200,
		Msg:         "success",
		Response:    result.Response,
		DialogueKey: result.DialogueID,
	}, nil
}

// AnalyzeAlert 告警分析根因诊断
func (s *AgentServer) AnalyzeAlert(ctx context.Context, req *aiAgentv1.AnalyzeAlertRequest) (*aiAgentv1.AnalyzeAlertResponse, error) {
	log.Printf("gRPC: AnalyzeAlert received")

	report, structuredRca, err := s.svc.AnalyzeAlert(ctx, req.AlertPayload, req.ErrorLogs)
	if err != nil {
		log.Printf("❌ AnalyzeAlert error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to analyze alert: %v", err)
	}

	return &aiAgentv1.AnalyzeAlertResponse{
		Code:           200,
		Msg:            "success",
		AnalysisReport: report,
		StructuredRca:  structuredRca,
	}, nil
}

func (s *AgentServer) CreateWorkflow(ctx context.Context, req *aiAgentv1.CreateWorkflowRequest) (*aiAgentv1.CreateWorkflowResponse, error) {
	log.Printf("gRPC: CreateWorkflow - user_id=%d, name=%s", req.UserId, req.Name)

	workflow, err := s.svc.CreateWorkflow(ctx, req.UserId, req.Name, req.DslJson)
	if err != nil {
		log.Printf("❌ CreateWorkflow error: %v", err)
		return nil, status.Errorf(codes.InvalidArgument, "failed to create workflow: %v", err)
	}

	return &aiAgentv1.CreateWorkflowResponse{
		Code:     200,
		Msg:      "success",
		Workflow: workflowToProto(workflow),
	}, nil
}

func (s *AgentServer) UpdateWorkflow(ctx context.Context, req *aiAgentv1.UpdateWorkflowRequest) (*aiAgentv1.UpdateWorkflowResponse, error) {
	log.Printf("gRPC: UpdateWorkflow - user_id=%d, workflow_id=%s", req.UserId, req.WorkflowId)

	workflow, err := s.svc.UpdateWorkflow(ctx, req.UserId, req.WorkflowId, req.Name, req.DslJson)
	if err != nil {
		log.Printf("❌ UpdateWorkflow error: %v", err)
		return nil, status.Errorf(codes.InvalidArgument, "failed to update workflow: %v", err)
	}

	return &aiAgentv1.UpdateWorkflowResponse{
		Code:     200,
		Msg:      "success",
		Workflow: workflowToProto(workflow),
	}, nil
}

func (s *AgentServer) ListWorkflows(ctx context.Context, req *aiAgentv1.ListWorkflowsRequest) (*aiAgentv1.ListWorkflowsResponse, error) {
	log.Printf("gRPC: ListWorkflows - user_id=%d", req.UserId)

	workflows, total, err := s.svc.ListWorkflows(ctx, req.UserId, int(req.Page), int(req.PageSize))
	if err != nil {
		log.Printf("❌ ListWorkflows error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to list workflows: %v", err)
	}

	protoWorkflows := make([]*aiAgentv1.WorkflowSummary, 0, len(workflows))
	for _, workflow := range workflows {
		protoWorkflows = append(protoWorkflows, workflowSummaryToProto(workflow))
	}

	return &aiAgentv1.ListWorkflowsResponse{
		Code:      200,
		Msg:       "success",
		Workflows: protoWorkflows,
		Total:     total,
	}, nil
}

func (s *AgentServer) GetWorkflow(ctx context.Context, req *aiAgentv1.GetWorkflowRequest) (*aiAgentv1.GetWorkflowResponse, error) {
	log.Printf("gRPC: GetWorkflow - user_id=%d, workflow_id=%s", req.UserId, req.WorkflowId)

	workflow, err := s.svc.GetWorkflow(ctx, req.UserId, req.WorkflowId)
	if err != nil {
		log.Printf("❌ GetWorkflow error: %v", err)
		return nil, status.Errorf(codes.NotFound, "failed to get workflow: %v", err)
	}

	return &aiAgentv1.GetWorkflowResponse{
		Code:     200,
		Msg:      "success",
		Workflow: workflowToProto(workflow),
	}, nil
}

func (s *AgentServer) RunWorkflow(ctx context.Context, req *aiAgentv1.RunWorkflowRequest) (*aiAgentv1.RunWorkflowResponse, error) {
	log.Printf("gRPC: RunWorkflow - user_id=%d, workflow_id=%s", req.UserId, req.WorkflowId)

	result, err := s.svc.RunWorkflow(ctx, req.UserId, req.WorkflowId, req.InputJson)
	if err != nil {
		log.Printf("❌ RunWorkflow error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to run workflow: %v", err)
	}

	return &aiAgentv1.RunWorkflowResponse{
		Code:        200,
		Msg:         "success",
		Run:         workflowRunToProto(result.Run),
		DialogueKey: result.DialogueKey,
		Response:    result.Response,
	}, nil
}

func (s *AgentServer) GetWorkflowRun(ctx context.Context, req *aiAgentv1.GetWorkflowRunRequest) (*aiAgentv1.GetWorkflowRunResponse, error) {
	log.Printf("gRPC: GetWorkflowRun - user_id=%d, run_id=%s", req.UserId, req.RunId)

	run, err := s.svc.GetWorkflowRun(ctx, req.UserId, req.RunId)
	if err != nil {
		log.Printf("❌ GetWorkflowRun error: %v", err)
		return nil, status.Errorf(codes.NotFound, "failed to get workflow run: %v", err)
	}

	return &aiAgentv1.GetWorkflowRunResponse{
		Code: 200,
		Msg:  "success",
		Run:  workflowRunToProto(run),
	}, nil
}

// ========================== 辅助函数 ==========================

func workflowSummaryToProto(workflow *repository.WorkflowDefinition) *aiAgentv1.WorkflowSummary {
	return &aiAgentv1.WorkflowSummary{
		WorkflowId: workflow.ID.Hex(),
		UserId:     workflow.UserID,
		Name:       workflow.Name,
		CreatedAt:  workflow.CreatedAt.Unix(),
		UpdatedAt:  workflow.UpdatedAt.Unix(),
	}
}

func workflowToProto(workflow *repository.WorkflowDefinition) *aiAgentv1.WorkflowDetail {
	return &aiAgentv1.WorkflowDetail{
		WorkflowId: workflow.ID.Hex(),
		UserId:     workflow.UserID,
		Name:       workflow.Name,
		DslJson:    workflow.DSLJSON,
		CreatedAt:  workflow.CreatedAt.Unix(),
		UpdatedAt:  workflow.UpdatedAt.Unix(),
	}
}

func workflowRunToProto(run *repository.WorkflowRunRecord) *aiAgentv1.WorkflowRun {
	return &aiAgentv1.WorkflowRun{
		RunId:        run.ID.Hex(),
		WorkflowId:   run.WorkflowID.Hex(),
		UserId:       run.UserID,
		Status:       run.Status,
		InputJson:    run.InputJSON,
		OutputJson:   run.OutputJSON,
		ErrorMessage: run.ErrorMessage,
		StartedAt:    run.StartedAt.Unix(),
		FinishedAt:   run.FinishedAt.Unix(),
	}
}

// dialogueObjectIDToUint64 将 MongoDB ObjectID 转为 uint64
// 由于 proto 中 dialogue_id 定义为 uint64，这里取 ObjectID 的后 8 字节作为 uint64
// 注意：这是一个有损转换，仅用于 gRPC 层的兼容适配
// 后续前端完善后建议改为直接传 string 类型的 hex ID
func dialogueObjectIDToUint64(oid interface{ Hex() string }) uint64 {
	hex := oid.Hex()
	if len(hex) < 16 {
		return 0
	}
	// 取 ObjectID hex 的后 16 位字符（8字节），转为 uint64
	var result uint64
	for _, c := range hex[len(hex)-16:] {
		result <<= 4
		switch {
		case c >= '0' && c <= '9':
			result |= uint64(c - '0')
		case c >= 'a' && c <= 'f':
			result |= uint64(c - 'a' + 10)
		}
	}
	return result
}

// uint64ToObjectIDHex 将 uint64 dialogue_id 转回 ObjectID hex 格式
// 这是 dialogueObjectIDToUint64 的逆操作（有损，前 8 字节补零）
func uint64ToObjectIDHex(id uint64) string {
	return fmt.Sprintf("%08x%016x", 0, id)
}
