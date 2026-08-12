package runtime

import (
	"context"
	"encoding/json"
	"time"
)

type MessageRole string

const (
	RoleSystem    MessageRole = "system"
	RoleDeveloper MessageRole = "developer"
	RoleUser      MessageRole = "user"
	RoleAssistant MessageRole = "assistant"
	RoleTool      MessageRole = "tool"
)

type ActionType string

const (
	ActionFinalAnswer ActionType = "final_answer"
	ActionToolCall    ActionType = "tool_call"
	ActionRAGSearch   ActionType = "rag_search"
	ActionAskHuman    ActionType = "ask_human"
)

type RunStatus string

const (
	RunStatusRunning          RunStatus = "running"
	RunStatusCompleted        RunStatus = "completed"
	RunStatusAwaitingHuman    RunStatus = "awaiting_human"
	RunStatusApprovalRequired RunStatus = "approval_required"
	RunStatusFailed           RunStatus = "failed"
)

type ResumeKind string

const (
	ResumeKindHumanResponse         ResumeKind = "human_response"
	ResumeKindToolApproval          ResumeKind = "tool_approval"
	ResumeKindDelegatedToolApproval ResumeKind = "delegated_tool_approval"
)

type ToolCategory string

const (
	ToolCategoryRead  ToolCategory = "read"
	ToolCategoryWrite ToolCategory = "write"
	ToolCategoryRisky ToolCategory = "risky"
)

type Message struct {
	Role       MessageRole
	Content    string
	Name       string
	ToolCallID string
	Actions    []Action
}

type Action struct {
	ID        string
	Type      ActionType
	Name      string
	Arguments json.RawMessage
	Content   string
}

type Observation struct {
	ActionID          string
	Name              string
	Content           string
	StructuredContent json.RawMessage
	IsError           bool
}

type ToolDefinition struct {
	Name             string
	Description      string
	InputSchema      json.RawMessage
	Category         ToolCategory
	RequiresApproval bool
}

func (t ToolDefinition) ApprovalRequired() bool {
	return t.RequiresApproval || t.Category == ToolCategoryWrite || t.Category == ToolCategoryRisky
}

type TokenUsage struct {
	InputTokens         int    `json:"input_tokens"`
	OutputTokens        int    `json:"output_tokens"`
	TotalTokens         int    `json:"total_tokens"`
	Estimated           bool   `json:"estimated"`
	EstimatedCostMicros int64  `json:"estimated_cost_micros"`
	CostEstimated       bool   `json:"cost_estimated"`
	PricingVersion      string `json:"pricing_version,omitempty"`
}

func (u *TokenUsage) Add(other TokenUsage) {
	u.InputTokens += other.InputTokens
	u.OutputTokens += other.OutputTokens
	u.TotalTokens += other.TotalTokens
	u.Estimated = u.Estimated || other.Estimated
	u.EstimatedCostMicros += other.EstimatedCostMicros
	u.CostEstimated = u.CostEstimated || other.CostEstimated
	switch {
	case u.PricingVersion == "":
		u.PricingVersion = other.PricingVersion
	case other.PricingVersion == "" || u.PricingVersion == other.PricingVersion:
	default:
		u.PricingVersion = "mixed"
	}
}

type Budget struct {
	MaxSteps               int
	MaxInputTokens         int
	MaxOutputTokens        int
	MaxTotalTokens         int
	MaxEstimatedCostMicros int64
	Timeout                time.Duration
	Deadline               time.Time
}

type RunContext struct {
	RunID                 string
	ParentRunID           string
	RoleID                string
	StrategyPlanDigest    string
	WorkflowID            string
	UserID                uint64
	Mode                  Mode
	AgentProfileID        string
	AgentProfileVersion   string
	PromptTemplateID      string
	PromptTemplateVersion string
	StartedAt             time.Time
	Budget                Budget
}

type RunRequest struct {
	Context           RunContext
	Model             string
	Messages          []Message
	Tools             []ToolDefinition
	InitialToolChoice ToolChoice
}

type ToolChoice string

const (
	ToolChoiceAuto     ToolChoice = "auto"
	ToolChoiceRequired ToolChoice = "required"
	ToolChoiceNone     ToolChoice = "none"
)

func (choice ToolChoice) Valid() bool {
	switch choice {
	case "", ToolChoiceAuto, ToolChoiceRequired, ToolChoiceNone:
		return true
	default:
		return false
	}
}

type Step struct {
	Index        int
	RoleID       string
	StartedAt    time.Time
	FinishedAt   time.Time
	Model        string
	Provider     string
	Actions      []Action
	Observations []Observation
	Usage        TokenUsage
	ModelRouting *ModelRoutingTrace
}

type RunResult struct {
	Context                 RunContext
	Status                  RunStatus
	FinalAnswer             string
	Messages                []Message
	Steps                   []Step
	PendingAction           *Action
	PendingResumeKind       ResumeKind
	PendingToolContinuation *ToolContinuation
	ApprovalID              string
	Usage                   TokenUsage
}

type ModelRequest struct {
	Context         RunContext
	StepIndex       int
	Model           string
	Messages        []Message
	Tools           []ToolDefinition
	ToolChoice      ToolChoice
	MaxOutputTokens int
}

type ModelResponse struct {
	Message      Message
	Actions      []Action
	Usage        TokenUsage
	Model        string
	Provider     string
	ModelRouting *ModelRoutingTrace
}

type ToolCall struct {
	RunContext RunContext
	ActionID   string
	Name       string
	Arguments  json.RawMessage
}

type ToolResult struct {
	Content           string
	StructuredContent json.RawMessage
}

// ToolContinuation is opaque Runtime state owned by a resumable Tool adapter.
// It is persisted only inside the caller's encrypted Run checkpoint.
type ToolContinuation struct {
	Version    string          `json:"version"`
	Prompt     string          `json:"prompt"`
	ResumeKind ResumeKind      `json:"resume_kind,omitempty"`
	ApprovalID string          `json:"approval_id,omitempty"`
	State      json.RawMessage `json:"state"`
}

type ToolResumeRequest struct {
	Call          ToolCall
	Continuation  ToolContinuation
	HumanResponse string
	ApprovalID    string
	ResumeToken   string
}

type RAGQuery struct {
	RunContext RunContext
	ActionID   string
	Name       string
	Arguments  json.RawMessage
}

type RAGResult struct {
	Content string
}

type ModelClient interface {
	Complete(ctx context.Context, request ModelRequest) (ModelResponse, error)
}

type CostEstimate struct {
	Micros         int64
	PricingVersion string
}

// CostEstimator keeps provider pricing outside Runtime while allowing the
// runner to reserve and enforce a conservative run-level monetary budget.
type CostEstimator interface {
	EstimateCost(model string, usage TokenUsage) (CostEstimate, error)
}

// TokenCounter provides replaceable request/response estimates. Providers may
// return exact usage; the runner only uses these estimates when exact usage is
// unavailable and for pre-call budget admission.
type TokenCounter interface {
	CountText(text string) int
	CountMessages(messages []Message) int
	EstimateRequest(request ModelRequest) TokenUsage
	EstimateResponse(response ModelResponse) TokenUsage
}

type ToolExecutor interface {
	Execute(ctx context.Context, call ToolCall) (ToolResult, error)
}

// ResumableToolExecutor resumes a previously suspended ToolCall without
// replaying the initial execution or any earlier Agent step.
type ResumableToolExecutor interface {
	ToolExecutor
	ResumeTool(context.Context, ToolResumeRequest) (ToolResult, error)
}

// ApprovalToolExecutor is an explicit trust boundary for approval-gated tools.
// A plain ToolExecutor can never execute write/risky actions merely because a
// caller included them in the model-visible catalog.
type ApprovalToolExecutor interface {
	ToolExecutor
	ExecuteApprovalGated(ctx context.Context, call ToolCall) (ToolResult, error)
}

type RAGSearcher interface {
	Search(ctx context.Context, query RAGQuery) (RAGResult, error)
}

type AgentRunner interface {
	Run(ctx context.Context, request RunRequest) (RunResult, error)
}
