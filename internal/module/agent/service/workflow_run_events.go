package service

import (
	"context"
	"errors"
	"time"

	agentObservability "twitter-clone/internal/module/agent/observability"
)

const (
	defaultWorkflowEventHeartbeat    = 10 * time.Second
	defaultWorkflowEventStreamWindow = 2 * time.Minute
)

// WatchWorkflowRunEvents delivers bounded, resumable execution updates. Mongo
// remains the query source of truth; this method only transports redacted
// trace records from the configured event reader.
func (s *AgentService) WatchWorkflowRunEvents(
	ctx context.Context,
	userID uint64,
	runID string,
	afterCursor string,
	send func(agentObservability.TraceEvent) error,
) error {
	if s.traceEventReader == nil {
		return errors.New("execution event reader is not available")
	}
	if send == nil {
		return errors.New("execution event sender is required")
	}
	cursor, err := agentObservability.NormalizeTraceEventCursor(afterCursor)
	if err != nil {
		return err
	}
	if _, err := s.GetWorkflowRun(ctx, userID, runID); err != nil {
		return err
	}

	heartbeat := s.workflowEventHeartbeat
	if heartbeat <= 0 {
		heartbeat = defaultWorkflowEventHeartbeat
	}
	window := s.workflowEventWindow
	if window <= 0 {
		window = defaultWorkflowEventStreamWindow
	}
	expiresAt := time.Now().Add(window)

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		remaining := time.Until(expiresAt)
		if remaining <= 0 {
			return send(workflowControlEvent(cursor, false, false, "window_expired"))
		}
		block := heartbeat
		if remaining < block {
			block = remaining
		}
		batch, err := s.traceEventReader.ReadTraceEvents(ctx, userID, runID, cursor, agentObservability.DefaultTraceEventBatchSize, block)
		if err != nil {
			return err
		}
		if batch.Reset {
			if err := send(workflowControlEvent(cursor, true, false, "cursor_reset_required")); err != nil {
				return err
			}
		}

		for _, event := range batch.Events {
			comparison, compareErr := agentObservability.CompareTraceEventCursor(event.Cursor, cursor)
			if compareErr != nil {
				return compareErr
			}
			if comparison <= 0 {
				continue
			}
			cursor = event.Cursor
			if event.Run != nil && workflowRunStatusIsQuiescent(event.Run.Status) {
				event.Terminal = true
			}
			if err := send(event); err != nil {
				return err
			}
			if event.Terminal {
				return nil
			}
		}
		if len(batch.Events) > 0 {
			continue
		}

		run, err := s.GetWorkflowRun(ctx, userID, runID)
		if err != nil {
			return err
		}
		if workflowRunStatusIsQuiescent(run.Status) {
			return send(workflowControlEvent(cursor, false, true, run.Status))
		}
		if time.Now().Before(expiresAt) {
			if err := send(agentObservability.TraceEvent{
				Cursor: cursor, Kind: agentObservability.TraceEventControl,
				Heartbeat: true, Reason: "heartbeat", CreatedAt: time.Now(),
			}); err != nil {
				return err
			}
		}
	}
}

func workflowControlEvent(cursor string, reset, terminal bool, reason string) agentObservability.TraceEvent {
	return agentObservability.TraceEvent{
		Cursor: cursor, Kind: agentObservability.TraceEventControl,
		Reset: reset, Terminal: terminal, Reason: reason, CreatedAt: time.Now(),
	}
}

func workflowRunStatusIsQuiescent(status string) bool {
	switch status {
	case WorkflowRunStatusSuspended,
		WorkflowRunStatusSuccess,
		WorkflowRunStatusFailed,
		WorkflowRunStatusRejected,
		WorkflowRunStatusCompensated,
		WorkflowRunStatusCompensationFailed,
		WorkflowRunStatusCanceled:
		return true
	default:
		return false
	}
}
