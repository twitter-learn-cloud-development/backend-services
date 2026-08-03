package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

const ReActCheckpointVersion = "react.v1"

// RunCheckpoint is infrastructure-agnostic Runtime state. Tool definitions
// are deliberately excluded: callers must resolve and authorize the current
// tool catalog again before every resume.
type RunCheckpoint struct {
	Version                 string            `json:"version"`
	Context                 RunContext        `json:"context"`
	Model                   string            `json:"model"`
	Messages                []Message         `json:"messages"`
	Steps                   []Step            `json:"steps"`
	PendingAction           Action            `json:"pending_action"`
	PendingResumeKind       ResumeKind        `json:"pending_resume_kind,omitempty"`
	PendingToolContinuation *ToolContinuation `json:"pending_tool_continuation,omitempty"`
	PendingApprovalID       string            `json:"pending_approval_id,omitempty"`
	Usage                   TokenUsage        `json:"usage"`
}

type ResumeRequest struct {
	Checkpoint    RunCheckpoint
	HumanResponse string
	ApprovalID    string
	ResumeToken   string
	Tools         []ToolDefinition
}

// ResumableAgentRunner is optional so compatibility runners can continue to
// implement AgentRunner without pretending to support durable recovery.
type ResumableAgentRunner interface {
	AgentRunner
	Resume(context.Context, ResumeRequest) (RunResult, error)
}

func NewRunCheckpoint(request RunRequest, result RunResult) (RunCheckpoint, error) {
	checkpoint := RunCheckpoint{
		Version:                 ReActCheckpointVersion,
		Context:                 result.Context,
		Model:                   strings.TrimSpace(request.Model),
		Messages:                cloneMessages(result.Messages),
		Steps:                   cloneSteps(result.Steps),
		Usage:                   result.Usage,
		PendingResumeKind:       result.PendingResumeKind,
		PendingToolContinuation: cloneToolContinuationPointer(result.PendingToolContinuation),
		PendingApprovalID:       strings.TrimSpace(result.ApprovalID),
	}
	if result.PendingAction != nil {
		checkpoint.PendingAction = cloneActions([]Action{*result.PendingAction})[0]
	}
	if checkpoint.PendingResumeKind == "" {
		switch {
		case checkpoint.PendingAction.Type == ActionAskHuman:
			checkpoint.PendingResumeKind = ResumeKindHumanResponse
		case checkpoint.PendingAction.Type == ActionToolCall && checkpoint.PendingApprovalID != "":
			checkpoint.PendingResumeKind = ResumeKindToolApproval
		}
	}
	if err := ValidateRunCheckpoint(checkpoint); err != nil {
		return RunCheckpoint{}, err
	}
	return checkpoint, nil
}

func ValidateRunCheckpoint(checkpoint RunCheckpoint) error {
	if checkpoint.Version != ReActCheckpointVersion {
		return errors.New("unsupported agent runtime checkpoint version")
	}
	if strings.TrimSpace(checkpoint.Context.RunID) == "" || checkpoint.Context.UserID == 0 {
		return errors.New("agent runtime checkpoint identity is incomplete")
	}
	if strings.TrimSpace(checkpoint.Model) == "" {
		return errors.New("agent runtime checkpoint model is required")
	}
	switch checkpoint.PendingAction.Type {
	case ActionAskHuman:
		if checkpoint.PendingResumeKind != "" &&
			checkpoint.PendingResumeKind != ResumeKindHumanResponse {
			return errors.New("agent runtime human checkpoint resume kind is invalid")
		}
		if strings.TrimSpace(checkpoint.PendingAction.ID) == "" || strings.TrimSpace(checkpoint.PendingAction.Content) == "" {
			return errors.New("agent runtime checkpoint pending human action is incomplete")
		}
		if strings.TrimSpace(checkpoint.PendingApprovalID) != "" {
			return errors.New("agent runtime human checkpoint cannot contain an approval id")
		}
		if checkpoint.PendingToolContinuation != nil {
			return errors.New("agent runtime direct human checkpoint cannot contain a tool continuation")
		}
	case ActionToolCall:
		if strings.TrimSpace(checkpoint.PendingAction.ID) == "" ||
			strings.TrimSpace(checkpoint.PendingAction.Name) == "" {
			return errors.New("agent runtime checkpoint pending tool action is incomplete")
		}
		switch checkpoint.PendingResumeKind {
		case ResumeKindHumanResponse:
			if strings.TrimSpace(checkpoint.PendingApprovalID) != "" {
				return errors.New("agent runtime suspended tool checkpoint cannot contain an approval id")
			}
			if err := validateToolContinuation(checkpoint.PendingToolContinuation); err != nil {
				return err
			}
		case ResumeKindDelegatedToolApproval:
			approvalID := strings.TrimSpace(checkpoint.PendingApprovalID)
			if approvalID == "" {
				return errors.New("agent runtime delegated approval checkpoint is incomplete")
			}
			if err := validateToolContinuation(checkpoint.PendingToolContinuation); err != nil {
				return err
			}
			if checkpoint.PendingToolContinuation.ResumeKind != ResumeKindDelegatedToolApproval ||
				strings.TrimSpace(checkpoint.PendingToolContinuation.ApprovalID) != approvalID {
				return errors.New("agent runtime delegated approval continuation binding is invalid")
			}
		case "", ResumeKindToolApproval:
			if strings.TrimSpace(checkpoint.PendingApprovalID) == "" {
				return errors.New("agent runtime checkpoint pending approval action is incomplete")
			}
			if checkpoint.PendingToolContinuation != nil {
				return errors.New("agent runtime approval checkpoint cannot contain a tool continuation")
			}
		default:
			return errors.New("agent runtime tool checkpoint resume kind is invalid")
		}
	default:
		return errors.New("agent runtime checkpoint is not resumable")
	}
	if len(checkpoint.Messages) == 0 || len(checkpoint.Steps) == 0 {
		return errors.New("agent runtime checkpoint execution history is empty")
	}
	lastAssistant, found := latestAssistantMessage(checkpoint.Messages)
	if !found || !messageContainsAction(lastAssistant, checkpoint.PendingAction.ID, checkpoint.PendingAction.Type) {
		return errors.New("agent runtime checkpoint pending action is not paired with the last assistant message")
	}
	lastStep := checkpoint.Steps[len(checkpoint.Steps)-1]
	if !actionsContain(lastStep.Actions, checkpoint.PendingAction.ID, checkpoint.PendingAction.Type) {
		return errors.New("agent runtime checkpoint pending action is not paired with the last step")
	}
	if checkpoint.PendingAction.Type == ActionToolCall && !hasFailedObservation(lastStep.Observations, checkpoint.PendingAction.ID) {
		return errors.New("agent runtime approval checkpoint has no failed pending observation")
	}
	return nil
}

func latestAssistantMessage(messages []Message) (Message, bool) {
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role == RoleAssistant {
			return messages[index], true
		}
	}
	return Message{}, false
}

func hasFailedObservation(observations []Observation, actionID string) bool {
	for _, observation := range observations {
		if observation.ActionID == actionID && observation.IsError {
			return true
		}
	}
	return false
}

func messageContainsAction(message Message, actionID string, actionType ActionType) bool {
	return actionsContain(message.Actions, actionID, actionType)
}

func actionsContain(actions []Action, actionID string, actionType ActionType) bool {
	for _, action := range actions {
		if action.ID == actionID && action.Type == actionType {
			return true
		}
	}
	return false
}

func cloneRunCheckpoint(checkpoint RunCheckpoint) RunCheckpoint {
	checkpoint.Messages = cloneMessages(checkpoint.Messages)
	checkpoint.Steps = cloneSteps(checkpoint.Steps)
	checkpoint.PendingAction = cloneActions([]Action{checkpoint.PendingAction})[0]
	checkpoint.PendingToolContinuation = cloneToolContinuationPointer(checkpoint.PendingToolContinuation)
	return checkpoint
}

func validateToolContinuation(continuation *ToolContinuation) error {
	if continuation == nil ||
		strings.TrimSpace(continuation.Version) == "" ||
		strings.TrimSpace(continuation.Prompt) == "" ||
		len(continuation.State) == 0 ||
		string(continuation.State) == "null" {
		return errors.New("agent runtime suspended tool continuation is incomplete")
	}
	if !json.Valid(continuation.State) {
		return errors.New("agent runtime suspended tool continuation state is invalid")
	}
	switch continuation.ResumeKind {
	case "", ResumeKindHumanResponse:
		if strings.TrimSpace(continuation.ApprovalID) != "" {
			return errors.New("agent runtime human tool continuation cannot contain an approval id")
		}
	case ResumeKindDelegatedToolApproval:
		if strings.TrimSpace(continuation.ApprovalID) == "" {
			return errors.New("agent runtime delegated approval continuation is incomplete")
		}
	default:
		return errors.New("agent runtime tool continuation resume kind is invalid")
	}
	return nil
}

func cloneToolContinuationPointer(continuation *ToolContinuation) *ToolContinuation {
	if continuation == nil {
		return nil
	}
	cloned := cloneToolContinuation(*continuation)
	return &cloned
}

func cloneToolContinuation(continuation ToolContinuation) ToolContinuation {
	continuation.State = cloneRawMessage(continuation.State)
	return continuation
}

func cloneSteps(steps []Step) []Step {
	cloned := make([]Step, len(steps))
	for index, step := range steps {
		cloned[index] = step
		cloned[index].Actions = cloneActions(step.Actions)
		cloned[index].Observations = cloneObservations(step.Observations)
	}
	return cloned
}

func cloneObservations(observations []Observation) []Observation {
	cloned := make([]Observation, len(observations))
	for index, observation := range observations {
		cloned[index] = observation
		cloned[index].StructuredContent = cloneRawMessage(observation.StructuredContent)
	}
	return cloned
}
