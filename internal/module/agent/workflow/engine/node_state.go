package engine

import "fmt"

const (
	NodeStatusPending   = "pending"
	NodeStatusRunning   = "running"
	NodeStatusRetrying  = "retrying"
	NodeStatusSuccess   = "success"
	NodeStatusFailed    = "failed"
	NodeStatusSkipped   = "skipped"
	NodeStatusSuspended = "suspended"
	NodeStatusCanceled  = "canceled"
	NodeStatusTimedOut  = "timed_out"
)

var nodeStateTransitions = map[string]map[string]struct{}{
	NodeStatusPending: {
		NodeStatusRunning:  {},
		NodeStatusSkipped:  {},
		NodeStatusCanceled: {},
		NodeStatusTimedOut: {},
	},
	NodeStatusRunning: {
		NodeStatusRetrying:  {},
		NodeStatusSuccess:   {},
		NodeStatusFailed:    {},
		NodeStatusSuspended: {},
		NodeStatusCanceled:  {},
		NodeStatusTimedOut:  {},
	},
	NodeStatusRetrying: {
		NodeStatusRunning:  {},
		NodeStatusFailed:   {},
		NodeStatusCanceled: {},
		NodeStatusTimedOut: {},
	},
	NodeStatusSuspended: {
		NodeStatusRunning:  {},
		NodeStatusSuccess:  {},
		NodeStatusCanceled: {},
	},
}

func validateNodeStateTransition(current, next string) error {
	if current == "" {
		current = NodeStatusPending
	}
	if current == next {
		return nil
	}
	allowed, exists := nodeStateTransitions[current]
	if !exists {
		return fmt.Errorf("node state %s is terminal", current)
	}
	if _, exists := allowed[next]; !exists {
		return fmt.Errorf("node state transition %s -> %s is not allowed", current, next)
	}
	return nil
}
