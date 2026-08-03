package repository

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ========================== MongoDB 集合名 ==========================

const (
	CollectionDialogues             = "dialogues"
	CollectionMessages              = "dialogue_messages"
	CollectionWorkflows             = "agent_workflows"
	CollectionWorkflowRevisions     = "agent_workflow_revisions"
	CollectionRuns                  = "agent_workflow_runs"
	CollectionWorkflowStateEvents   = "agent_workflow_state_events"
	CollectionWorkflowSnapshots     = "agent_workflow_state_snapshots"
	CollectionWorkflowCompensations = "agent_workflow_compensations"
	CollectionProviderConfigs       = "agent_provider_configs"
	CollectionAgentProjects         = "agent_projects"
	CollectionMCPConnections        = "agent_mcp_connections"
	CollectionMCPToolSnapshots      = "agent_mcp_tool_snapshots"
)

// ========================== 对话模式枚举 ==========================

type DialogueMode int32

const (
	ModeChat     DialogueMode = 1 // 模式一：直接 AI 对话
	ModeConsult  DialogueMode = 2 // 模式二：语义搜索推文（RAG）
	ModeAssist   DialogueMode = 3 // 模式三：AI 辅助写推文
	ModeMulti    DialogueMode = 4 // 模式四：多 Agent 协作写推文
	ModeWorkflow DialogueMode = 5 // 模式五：用户自定义工作流
)

// ========================== 数据模型 ==========================

// Dialogue 对话会话
// 对应 MongoDB 集合：dialogues
// 一个用户可以有多个对话会话，每个会话包含多条消息
type Dialogue struct {
	ID                     primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	UserID                 uint64             `bson:"user_id"       json:"user_id"`
	Title                  string             `bson:"title"         json:"title"` // 对话标题，取自首条用户消息的前30字
	Mode                   DialogueMode       `bson:"mode"          json:"mode"`  // 对话模式
	SummaryVersion         int                `bson:"summary_version,omitempty" json:"summary_version,omitempty"`
	SummarizedMessageCount int64              `bson:"summarized_message_count,omitempty" json:"summarized_message_count,omitempty"`
	SummaryStatus          string             `bson:"summary_status,omitempty" json:"-"`
	SummaryLeaseToken      string             `bson:"summary_lease_token,omitempty" json:"-"`
	SummaryLeaseUntil      time.Time          `bson:"summary_lease_until,omitempty" json:"-"`
	SummaryUpdatedAt       time.Time          `bson:"summary_updated_at,omitempty" json:"summary_updated_at,omitempty"`
	CreatedAt              time.Time          `bson:"created_at"    json:"created_at"`
	UpdatedAt              time.Time          `bson:"updated_at"    json:"updated_at"`
}

// MessageRole 消息角色
type MessageRole string

const (
	RoleUser      MessageRole = "user"
	RoleAssistant MessageRole = "assistant"
	RoleSystem    MessageRole = "system"
	RoleTool      MessageRole = "tool"
)

// DialogueMessage 对话中的单条消息
// 对应 MongoDB 集合：dialogue_messages
// 通过 dialogue_id 关联到 Dialogue
type DialogueMessage struct {
	ID         primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	DialogueID primitive.ObjectID `bson:"dialogue_id"   json:"dialogue_id"`
	UserID     uint64             `bson:"user_id"       json:"user_id"`
	Role       MessageRole        `bson:"role"          json:"role"` // user / assistant / system / tool
	Content    string             `bson:"content"       json:"content"`
	ToolName   string             `bson:"tool_name,omitempty"   json:"tool_name,omitempty"`     // 仅 role=tool 时有值
	ToolCallID string             `bson:"tool_call_id,omitempty" json:"tool_call_id,omitempty"` // 仅 role=tool 时有值
	Metadata   map[string]any     `bson:"metadata,omitempty"    json:"metadata,omitempty"`      // 扩展字段：存推文结果等附加数据
	CreatedAt  time.Time          `bson:"created_at"    json:"created_at"`
}

// WorkflowDefinition 保存用户通过前端画布创建的工作流 DSL。
type WorkflowDefinition struct {
	ID                    primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	UserID                uint64             `bson:"user_id" json:"user_id"`
	Name                  string             `bson:"name" json:"name"`
	DSLJSON               string             `bson:"dsl_json" json:"dsl_json"`
	CurrentRevisionID     primitive.ObjectID `bson:"current_revision_id,omitempty" json:"current_revision_id,omitempty"`
	CurrentRevisionNumber int64              `bson:"current_revision_number,omitempty" json:"current_revision_number,omitempty"`
	CurrentDSLHash        string             `bson:"current_dsl_hash,omitempty" json:"current_dsl_hash,omitempty"`
	CreatedAt             time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt             time.Time          `bson:"updated_at" json:"updated_at"`
}

// WorkflowRunRecord 保存一次工作流运行的输入、输出和错误信息。
type WorkflowRunRecord struct {
	ID                     primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	WorkflowID             primitive.ObjectID `bson:"workflow_id" json:"workflow_id"`
	WorkflowRevisionID     primitive.ObjectID `bson:"workflow_revision_id,omitempty" json:"workflow_revision_id,omitempty"`
	WorkflowRevisionNumber int64              `bson:"workflow_revision_number,omitempty" json:"workflow_revision_number,omitempty"`
	UserID                 uint64             `bson:"user_id" json:"user_id"`
	InvocationSource       string             `bson:"invocation_source,omitempty" json:"invocation_source,omitempty"`
	ParentRunID            string             `bson:"parent_run_id,omitempty" json:"parent_run_id,omitempty"`
	ParentActionID         string             `bson:"parent_action_id,omitempty" json:"parent_action_id,omitempty"`
	Status                 string             `bson:"status" json:"status"`
	InputJSON              string             `bson:"input_json" json:"input_json"`
	OutputJSON             string             `bson:"output_json" json:"output_json"`
	CheckpointJSON         string             `bson:"checkpoint_json,omitempty" json:"checkpoint_json,omitempty"`
	WaitingNodeID          string             `bson:"waiting_node_id,omitempty" json:"waiting_node_id,omitempty"`
	ApprovalRequestID      primitive.ObjectID `bson:"approval_request_id,omitempty" json:"approval_request_id,omitempty"`
	ResumeToken            string             `bson:"resume_token,omitempty" json:"resume_token,omitempty"` // Deprecated: 仅兼容旧文档，新运行不得写入明文令牌。
	ResumeTokenHash        string             `bson:"resume_token_hash,omitempty" json:"-"`
	ResumeAttemptID        string             `bson:"resume_attempt_id,omitempty" json:"-"`
	ResumeGrantIssuedAt    time.Time          `bson:"resume_grant_issued_at,omitempty" json:"resume_grant_issued_at,omitempty"`
	ResumeGrantExpiresAt   time.Time          `bson:"resume_grant_expires_at,omitempty" json:"resume_grant_expires_at,omitempty"`
	Revision               int64              `bson:"revision" json:"revision"`
	StateVersion           int64              `bson:"state_version,omitempty" json:"state_version,omitempty"`
	NodeExecutions         int                `bson:"node_executions" json:"node_executions"`
	InputTokens            int                `bson:"input_tokens" json:"input_tokens"`
	OutputTokens           int                `bson:"output_tokens" json:"output_tokens"`
	TotalTokens            int                `bson:"total_tokens" json:"total_tokens"`
	UsageEstimated         bool               `bson:"usage_estimated" json:"usage_estimated"`
	EstimatedCostMicros    int64              `bson:"estimated_cost_micros" json:"estimated_cost_micros"`
	CostEstimated          bool               `bson:"cost_estimated" json:"cost_estimated"`
	PricingVersion         string             `bson:"pricing_version,omitempty" json:"pricing_version,omitempty"`
	MaxSteps               int                `bson:"max_steps" json:"max_steps"`
	MaxTotalTokens         int                `bson:"max_total_tokens" json:"max_total_tokens"`
	MaxEstimatedCostMicros int64              `bson:"max_estimated_cost_micros" json:"max_estimated_cost_micros"`
	AccountingVersion      string             `bson:"accounting_version,omitempty" json:"accounting_version,omitempty"`
	ErrorMessage           string             `bson:"error_message,omitempty" json:"error_message,omitempty"`
	CancelRequestedAt      time.Time          `bson:"cancel_requested_at,omitempty" json:"cancel_requested_at,omitempty"`
	CancelReason           string             `bson:"cancel_reason,omitempty" json:"cancel_reason,omitempty"`
	CanceledAt             time.Time          `bson:"canceled_at,omitempty" json:"canceled_at,omitempty"`
	StartedAt              time.Time          `bson:"started_at" json:"started_at"`
	SuspendedAt            time.Time          `bson:"suspended_at,omitempty" json:"suspended_at,omitempty"`
	FinishedAt             time.Time          `bson:"finished_at" json:"finished_at"`
}

// GenerateTitle 根据用户首条消息生成对话标题
// 截取前 30 个 rune，超出部分用 ... 替代
func GenerateTitle(content string) string {
	runes := []rune(content)
	if len(runes) > 30 {
		return string(runes[:30]) + "..."
	}
	return string(runes)
}
