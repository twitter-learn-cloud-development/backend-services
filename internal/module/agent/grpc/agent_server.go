package grpc

import (
	"context"
	"log"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	aiAgentv1 "twitter-clone/api/aiAgent/v1"
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

	response, err := s.svc.CallApiOfAi(ctx, req.UserId, req.MainContent.DialogueId, req.MainContent.Content)
	if err != nil {
		log.Printf("❌ CallApiOfAi error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to call ai: %v", err)
	}

	return &aiAgentv1.CallApiOfAiResponse{
		Code:     200,
		Msg:      "success",
		Response: response,
	}, nil
}

// ConsultContent 模式二：通过对话查询相关推文和作者
func (s *AgentServer) ConsultContent(ctx context.Context, req *aiAgentv1.ConsultContentRequest) (*aiAgentv1.ConsultContentResponse, error) {
	log.Printf("gRPC: ConsultContent - user_id=%d, dialogue_id=%d", req.UserId, req.MainContent.DialogueId)

	response, tweetResults, err := s.svc.ConsultContent(ctx, req.UserId, req.MainContent.DialogueId, req.MainContent.Content)
	if err != nil {
		log.Printf("❌ ConsultContent error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to consult content: %v", err)
	}

	protoTweetList := make([]*aiAgentv1.TweetResult, len(tweetResults))
	for i, t := range tweetResults {
		protoTweetList[i] = &aiAgentv1.TweetResult{
			TweetId: t.TweetID,
			Url:     t.URL,
			Summary: t.Summary,
		}
	}

	return &aiAgentv1.ConsultContentResponse{
		Code:      200,
		Msg:       "success",
		Response:  response,
		TweetList: protoTweetList,
	}, nil
}

// AssistPublishTwitter 模式三：协助构建推文
func (s *AgentServer) AssistPublishTwitter(ctx context.Context, req *aiAgentv1.AssistPublishTwitterRequest) (*aiAgentv1.AssistPublishTwitterResponse, error) {
	log.Printf("gRPC: AssistPublishTwitter - user_id=%d, dialogue_id=%d", req.UserId, req.MainContent.DialogueId)

	content, err := s.svc.AssistPublishTwitter(ctx, req.UserId, req.MainContent.DialogueId, req.MainContent.Content)
	if err != nil {
		log.Printf("❌ AssistPublishTwitter error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to assist publish twitter: %v", err)
	}
	return &aiAgentv1.AssistPublishTwitterResponse{
		Code:     200,
		Msg:      "success",
		Response: content,
	}, nil
}

// GetRepositoryDialogue 获取历史对话列表
func (s *AgentServer) GetRepositoryDialogue(ctx context.Context, req *aiAgentv1.GetRepositoryDialogueRequest) (*aiAgentv1.GetRepositoryDialogueResponse, error) {
	log.Printf("gRPC: GetRepositoryDialogue - user_id=%d", req.UserId)

	return &aiAgentv1.GetRepositoryDialogueResponse{
		Code: 200,
		Msg:  "coming soon",
	}, nil
}

// GetDialogueDetail 获取某个历史对话的详细消息记录
func (s *AgentServer) GetDialogueDetail(ctx context.Context, req *aiAgentv1.GetDialogueDetailRequest) (*aiAgentv1.GetDialogueDetailResponse, error) {
	log.Printf("gRPC: GetDialogueDetail - user_id=%d, dialogue_id=%d", req.UserId, req.DialogueId)

	return &aiAgentv1.GetDialogueDetailResponse{
		Code: 200,
		Msg:  "coming soon",
	}, nil
}

// GetModelDetailedInformation 获取模型初始化详细信息
func (s *AgentServer) GetModelDetailedInformation(ctx context.Context, req *aiAgentv1.GetModelDetailedInformationRequest) (*aiAgentv1.GetModelDetailedInformationResponse, error) {
	log.Printf("gRPC: GetModelDetailedInformation - user_id=%d", req.UserId)

	return &aiAgentv1.GetModelDetailedInformationResponse{
		Code: 200,
		Msg:  "coming soon",
	}, nil
}

// AnalysisFiles 解析前端文件
func (s *AgentServer) AnalysisFiles(ctx context.Context, req *aiAgentv1.AnalysisFilesRequest) (*aiAgentv1.AnalysisFilesResponse, error) {
	log.Printf("gRPC: AnalysisFiles - user_id=%d", req.UserId)

	return &aiAgentv1.AnalysisFilesResponse{
		Code: 200,
		Msg:  "coming soon",
	}, nil
}
