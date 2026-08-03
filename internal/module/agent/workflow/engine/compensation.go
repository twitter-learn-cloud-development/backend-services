package engine

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"twitter-clone/internal/module/agent/workflow/dsl"
)

type CompensationExecuteFunc func(ctx context.Context, attempt int) (map[string]interface{}, error)

// CompensationTask is an immutable, deterministic description of one
// reverse-order governed tool call. Execution and persistence stay outside the
// engine so it does not depend on repositories or tool implementations.
type CompensationTask struct {
	Sequence     int                    `json:"sequence"`
	SourceNodeID string                 `json:"source_node_id"`
	StepID       string                 `json:"step_id"`
	ToolName     string                 `json:"tool_name"`
	Inputs       map[string]interface{} `json:"inputs"`
	TimeoutSec   int                    `json:"timeout_sec,omitempty"`
	Retry        *dsl.RetryPolicyDSL    `json:"retry,omitempty"`
}

func (s *Scheduler) CompensationPlan() []CompensationTask {
	if s == nil || s.plan == nil || s.blackboard == nil {
		return nil
	}
	snapshot := s.blackboard.GetSnapshot()
	traces := make(map[string]NodeTrace)
	for _, trace := range s.GetTraces() {
		traces[trace.NodeID] = trace
	}
	order := s.plan.TopologicalOrder()
	tasks := make([]CompensationTask, 0)
	for index := len(order) - 1; index >= 0; index-- {
		node, exists := s.plan.Node(order[index])
		if !exists || node.Compensation == nil || traces[node.ID].Status != NodeStatusSuccess {
			continue
		}
		tasks = append(tasks, CompensationTask{
			Sequence:     len(tasks) + 1,
			SourceNodeID: node.ID,
			StepID:       node.ID + "$compensate",
			ToolName:     node.Compensation.ToolName,
			Inputs:       resolveRawInputs(node.Compensation.Properties, snapshot),
			TimeoutSec:   node.Compensation.TimeoutSec,
			Retry:        cloneCompensationRetry(node.Compensation.Retry),
		})
	}
	return tasks
}

func cloneCompensationRetry(source *dsl.RetryPolicyDSL) *dsl.RetryPolicyDSL {
	if source == nil {
		return nil
	}
	copy := *source
	return &copy
}

func cloneCompensationTasks(source []CompensationTask) []CompensationTask {
	result := make([]CompensationTask, len(source))
	for index, task := range source {
		result[index] = task
		result[index].Inputs = cloneFields(task.Inputs)
		result[index].Retry = cloneCompensationRetry(task.Retry)
	}
	return result
}

func compensationTaskJSON(task CompensationTask) string {
	encoded, _ := json.Marshal(task)
	return string(encoded)
}

// ExecuteCompensationTask applies the same deterministic retry and total
// timeout semantics used by normal nodes without depending on ToolExecutor.
func ExecuteCompensationTask(ctx context.Context, task CompensationTask, execute CompensationExecuteFunc) (map[string]interface{}, int, error) {
	if execute == nil {
		return nil, 0, errors.New("compensation executor is required")
	}
	executionCtx := ctx
	if task.TimeoutSec > 0 {
		var cancel context.CancelFunc
		executionCtx, cancel = context.WithTimeout(ctx, time.Duration(task.TimeoutSec)*time.Second)
		defer cancel()
	}
	policy := normalizeNodeRetryPolicy(task.Retry)
	for attempt := 1; attempt <= policy.maxAttempts; attempt++ {
		outputs, err := execute(executionCtx, attempt)
		if err == nil {
			return outputs, attempt, nil
		}
		if attempt == policy.maxAttempts || !shouldRetryNode(err) || executionCtx.Err() != nil {
			return nil, attempt, err
		}
		if err := waitNodeRetry(executionCtx, nodeRetryDelay(policy, task.StepID, attempt)); err != nil {
			return nil, attempt, err
		}
	}
	return nil, policy.maxAttempts, errors.New("compensation retry policy exhausted without a result")
}
