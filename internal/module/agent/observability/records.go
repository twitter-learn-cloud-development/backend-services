package observability

import (
	"context"
	"time"
)

const (
	SourceWorkflow = "workflow"
	SourceRuntime  = "runtime"
)

type TokenUsage struct {
	InputTokens         int    `bson:"input_tokens,omitempty" json:"input_tokens,omitempty"`
	OutputTokens        int    `bson:"output_tokens,omitempty" json:"output_tokens,omitempty"`
	TotalTokens         int    `bson:"total_tokens,omitempty" json:"total_tokens,omitempty"`
	Estimated           bool   `bson:"estimated,omitempty" json:"estimated,omitempty"`
	EstimatedCostMicros int64  `bson:"estimated_cost_micros,omitempty" json:"estimated_cost_micros,omitempty"`
	CostEstimated       bool   `bson:"cost_estimated,omitempty" json:"cost_estimated,omitempty"`
	PricingVersion      string `bson:"pricing_version,omitempty" json:"pricing_version,omitempty"`
}

type BudgetSnapshot struct {
	MaxSteps               int   `bson:"max_steps,omitempty" json:"max_steps,omitempty"`
	MaxTotalTokens         int   `bson:"max_total_tokens,omitempty" json:"max_total_tokens,omitempty"`
	MaxEstimatedCostMicros int64 `bson:"max_estimated_cost_micros,omitempty" json:"max_estimated_cost_micros,omitempty"`
	ConsumedSteps          int   `bson:"consumed_steps,omitempty" json:"consumed_steps,omitempty"`
	ConsumedTokens         int   `bson:"consumed_tokens,omitempty" json:"consumed_tokens,omitempty"`
	ConsumedCostMicros     int64 `bson:"consumed_cost_micros,omitempty" json:"consumed_cost_micros,omitempty"`
}

type RunRecord struct {
	RecordID            string         `bson:"record_id" json:"record_id"`
	RunID               string         `bson:"run_id" json:"run_id"`
	WorkflowID          string         `bson:"workflow_id,omitempty" json:"workflow_id,omitempty"`
	UserID              uint64         `bson:"user_id" json:"user_id"`
	Source              string         `bson:"source" json:"source"`
	AgentProfileID      string         `bson:"agent_profile_id,omitempty" json:"agent_profile_id,omitempty"`
	AgentProfileVersion string         `bson:"agent_profile_version,omitempty" json:"agent_profile_version,omitempty"`
	Mode                string         `bson:"mode,omitempty" json:"mode,omitempty"`
	Strategy            string         `bson:"strategy,omitempty" json:"strategy,omitempty"`
	Status              string         `bson:"status" json:"status"`
	ErrorClass          string         `bson:"error_class,omitempty" json:"error_class,omitempty"`
	Usage               TokenUsage     `bson:"usage,omitempty" json:"usage,omitempty"`
	Budget              BudgetSnapshot `bson:"budget,omitempty" json:"budget,omitempty"`
	StartedAt           time.Time      `bson:"started_at" json:"started_at"`
	FinishedAt          time.Time      `bson:"finished_at,omitempty" json:"finished_at,omitempty"`
	DurationMS          int64          `bson:"duration_ms,omitempty" json:"duration_ms,omitempty"`
	UpdatedAt           time.Time      `bson:"updated_at" json:"updated_at"`
}

type StepRecord struct {
	RecordID     string    `bson:"record_id" json:"record_id"`
	RunID        string    `bson:"run_id" json:"run_id"`
	WorkflowID   string    `bson:"workflow_id,omitempty" json:"workflow_id,omitempty"`
	UserID       uint64    `bson:"user_id" json:"user_id"`
	Source       string    `bson:"source" json:"source"`
	StepID       string    `bson:"step_id" json:"step_id"`
	ParentStepID string    `bson:"parent_step_id,omitempty" json:"parent_step_id,omitempty"`
	Sequence     int       `bson:"sequence" json:"sequence"`
	StepType     string    `bson:"step_type" json:"step_type"`
	Name         string    `bson:"name,omitempty" json:"name,omitempty"`
	Status       string    `bson:"status" json:"status"`
	Attempt      int       `bson:"attempt,omitempty" json:"attempt,omitempty"`
	MaxAttempts  int       `bson:"max_attempts,omitempty" json:"max_attempts,omitempty"`
	ErrorClass   string    `bson:"error_class,omitempty" json:"error_class,omitempty"`
	StartedAt    time.Time `bson:"started_at,omitempty" json:"started_at,omitempty"`
	FinishedAt   time.Time `bson:"finished_at,omitempty" json:"finished_at,omitempty"`
	DurationMS   int64     `bson:"duration_ms,omitempty" json:"duration_ms,omitempty"`
	UpdatedAt    time.Time `bson:"updated_at" json:"updated_at"`
}

type LLMCallRecord struct {
	RecordID               string     `bson:"record_id" json:"record_id"`
	RunID                  string     `bson:"run_id" json:"run_id"`
	WorkflowID             string     `bson:"workflow_id,omitempty" json:"workflow_id,omitempty"`
	UserID                 uint64     `bson:"user_id" json:"user_id"`
	Source                 string     `bson:"source" json:"source"`
	StepID                 string     `bson:"step_id" json:"step_id"`
	Sequence               int        `bson:"sequence" json:"sequence"`
	Model                  string     `bson:"model,omitempty" json:"model,omitempty"`
	Provider               string     `bson:"provider,omitempty" json:"provider,omitempty"`
	Status                 string     `bson:"status" json:"status"`
	ErrorClass             string     `bson:"error_class,omitempty" json:"error_class,omitempty"`
	PromptHash             string     `bson:"prompt_hash,omitempty" json:"prompt_hash,omitempty"`
	PromptLength           int        `bson:"prompt_length,omitempty" json:"prompt_length,omitempty"`
	PromptTemplateID       string     `bson:"prompt_template_id,omitempty" json:"prompt_template_id,omitempty"`
	PromptTemplateVersion  string     `bson:"prompt_template_version,omitempty" json:"prompt_template_version,omitempty"`
	PromptSample           string     `bson:"prompt_sample,omitempty" json:"prompt_sample,omitempty"`
	PromptSampleStatus     string     `bson:"prompt_sample_status,omitempty" json:"prompt_sample_status,omitempty"`
	CompletionHash         string     `bson:"completion_hash,omitempty" json:"completion_hash,omitempty"`
	CompletionLength       int        `bson:"completion_length,omitempty" json:"completion_length,omitempty"`
	CompletionSample       string     `bson:"completion_sample,omitempty" json:"completion_sample,omitempty"`
	CompletionSampleStatus string     `bson:"completion_sample_status,omitempty" json:"completion_sample_status,omitempty"`
	ContentSamplePolicy    string     `bson:"content_sample_policy,omitempty" json:"content_sample_policy,omitempty"`
	Usage                  TokenUsage `bson:"usage,omitempty" json:"usage,omitempty"`
	StartedAt              time.Time  `bson:"started_at" json:"started_at"`
	FinishedAt             time.Time  `bson:"finished_at" json:"finished_at"`
	DurationMS             int64      `bson:"duration_ms" json:"duration_ms"`
	UpdatedAt              time.Time  `bson:"updated_at" json:"updated_at"`
}

type ToolCallRecord struct {
	RecordID          string    `bson:"record_id" json:"record_id"`
	RunID             string    `bson:"run_id" json:"run_id"`
	WorkflowID        string    `bson:"workflow_id,omitempty" json:"workflow_id,omitempty"`
	UserID            uint64    `bson:"user_id" json:"user_id"`
	Source            string    `bson:"source" json:"source"`
	StepID            string    `bson:"step_id" json:"step_id"`
	Sequence          int       `bson:"sequence" json:"sequence"`
	ToolName          string    `bson:"tool_name" json:"tool_name"`
	Category          string    `bson:"category,omitempty" json:"category,omitempty"`
	Status            string    `bson:"status" json:"status"`
	ErrorClass        string    `bson:"error_class,omitempty" json:"error_class,omitempty"`
	Attempts          int       `bson:"attempts,omitempty" json:"attempts,omitempty"`
	ArgumentsHash     string    `bson:"arguments_hash,omitempty" json:"arguments_hash,omitempty"`
	ArgumentsLength   int       `bson:"arguments_length,omitempty" json:"arguments_length,omitempty"`
	OutputHash        string    `bson:"output_hash,omitempty" json:"output_hash,omitempty"`
	OutputLength      int       `bson:"output_length,omitempty" json:"output_length,omitempty"`
	OutputStorage     string    `bson:"output_storage,omitempty" json:"output_storage,omitempty"`
	OutputReference   string    `bson:"output_reference,omitempty" json:"output_reference,omitempty"`
	OutputContentType string    `bson:"output_content_type,omitempty" json:"output_content_type,omitempty"`
	StartedAt         time.Time `bson:"started_at" json:"started_at"`
	FinishedAt        time.Time `bson:"finished_at" json:"finished_at"`
	DurationMS        int64     `bson:"duration_ms" json:"duration_ms"`
	UpdatedAt         time.Time `bson:"updated_at" json:"updated_at"`
}

type TraceBundle struct {
	Run       *RunRecord       `json:"run,omitempty"`
	Steps     []StepRecord     `json:"steps"`
	LLMCalls  []LLMCallRecord  `json:"llm_calls"`
	ToolCalls []ToolCallRecord `json:"tool_calls"`
}

type Recorder interface {
	RecordRun(ctx context.Context, record RunRecord) error
	RecordStep(ctx context.Context, record StepRecord) error
	RecordLLMCall(ctx context.Context, record LLMCallRecord) error
	RecordToolCall(ctx context.Context, record ToolCallRecord) error
}

type Reader interface {
	GetTraceBundle(ctx context.Context, userID uint64, runID string) (*TraceBundle, error)
}

type NoopRecorder struct{}

func (NoopRecorder) RecordRun(context.Context, RunRecord) error           { return nil }
func (NoopRecorder) RecordStep(context.Context, StepRecord) error         { return nil }
func (NoopRecorder) RecordLLMCall(context.Context, LLMCallRecord) error   { return nil }
func (NoopRecorder) RecordToolCall(context.Context, ToolCallRecord) error { return nil }
