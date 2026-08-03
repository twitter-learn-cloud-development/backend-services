package engine

import (
	"fmt"

	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

type WorkflowCheckpoint struct {
	CurrentNodeID    string                            `json:"current_node_id"`
	Blackboard       map[string]map[string]interface{} `json:"blackboard"`
	StateVersion     uint64                            `json:"state_version,omitempty"`
	Traces           []NodeTrace                       `json:"traces"`
	SuspendedAt      int64                             `json:"suspended_at"`
	Reason           string                            `json:"reason,omitempty"`
	ResumeToken      string                            `json:"resume_token,omitempty"`
	Metadata         map[string]interface{}            `json:"metadata,omitempty"`
	RetryCurrentNode bool                              `json:"retry_current_node,omitempty"`
	Budget           agentRuntime.BudgetSnapshot       `json:"budget,omitempty"`
}

type WorkflowSuspension struct {
	NodeID      string                 `json:"node_id"`
	Reason      string                 `json:"reason,omitempty"`
	ResumeToken string                 `json:"resume_token,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

type SuspensionError struct {
	Suspension WorkflowSuspension
	Cause      error
}

func (e *SuspensionError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func (e *SuspensionError) Error() string {
	if e == nil {
		return "workflow suspended"
	}
	if e.Suspension.Reason != "" {
		return fmt.Sprintf("workflow suspended at node %s: %s", e.Suspension.NodeID, e.Suspension.Reason)
	}
	return fmt.Sprintf("workflow suspended at node %s", e.Suspension.NodeID)
}

func NewSuspensionError(nodeID, reason, resumeToken string, metadata map[string]interface{}) error {
	return NewSuspensionErrorWithCause(nodeID, reason, resumeToken, metadata, nil)
}

func NewSuspensionErrorWithCause(nodeID, reason, resumeToken string, metadata map[string]interface{}, cause error) error {
	return &SuspensionError{
		Suspension: WorkflowSuspension{
			NodeID:      nodeID,
			Reason:      reason,
			ResumeToken: resumeToken,
			Metadata:    metadata,
		},
		Cause: cause,
	}
}
