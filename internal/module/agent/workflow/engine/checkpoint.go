package engine

import "fmt"

const NodeStatusSuspended = "suspended"

type WorkflowCheckpoint struct {
	CurrentNodeID string                            `json:"current_node_id"`
	Blackboard    map[string]map[string]interface{} `json:"blackboard"`
	Traces        []NodeTrace                       `json:"traces"`
	SuspendedAt   int64                             `json:"suspended_at"`
	Reason        string                            `json:"reason,omitempty"`
	ResumeToken   string                            `json:"resume_token,omitempty"`
}

type WorkflowSuspension struct {
	NodeID      string                 `json:"node_id"`
	Reason      string                 `json:"reason,omitempty"`
	ResumeToken string                 `json:"resume_token,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

type SuspensionError struct {
	Suspension WorkflowSuspension
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
	return &SuspensionError{
		Suspension: WorkflowSuspension{
			NodeID:      nodeID,
			Reason:      reason,
			ResumeToken: resumeToken,
			Metadata:    metadata,
		},
	}
}
