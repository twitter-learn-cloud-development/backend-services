package runtime

import (
	"fmt"
	"reflect"
)

func validateResumedGoalResult(checkpoint RunCheckpoint, resumed RunResult) error {
	if !reflect.DeepEqual(checkpoint.Context, resumed.Context) {
		return fmt.Errorf("resumed run context does not match the checkpoint")
	}
	if resumed.Status != RunStatusCompleted && !isGoalSuspendedStatus(resumed.Status) {
		return fmt.Errorf("resumed run status %q is not valid", resumed.Status)
	}
	if len(resumed.Messages) < len(checkpoint.Messages) {
		return fmt.Errorf("resumed run message history was truncated")
	}
	for index := range checkpoint.Messages {
		if !reflect.DeepEqual(checkpoint.Messages[index], resumed.Messages[index]) {
			return fmt.Errorf("resumed run message history changed at index %d", index)
		}
	}
	if len(resumed.Steps) < len(checkpoint.Steps) {
		return fmt.Errorf("resumed run step history was truncated")
	}
	// The last checkpoint step owns the pending action and may replace its
	// failed observation during resume. Earlier steps are immutable.
	for index := 0; index+1 < len(checkpoint.Steps); index++ {
		if !reflect.DeepEqual(checkpoint.Steps[index], resumed.Steps[index]) {
			return fmt.Errorf("resumed run step history changed at index %d", index)
		}
	}
	if len(checkpoint.Steps) > 0 {
		pendingIndex := len(checkpoint.Steps) - 1
		if !reflect.DeepEqual(
			checkpoint.Steps[pendingIndex].Actions,
			resumed.Steps[pendingIndex].Actions,
		) {
			return fmt.Errorf("resumed run pending step actions changed")
		}
	}
	if !usageIsMonotonic(checkpoint.Usage, resumed.Usage) {
		return fmt.Errorf("resumed run usage moved backwards")
	}
	return nil
}

func usageIsMonotonic(previous, current TokenUsage) bool {
	return current.InputTokens >= previous.InputTokens &&
		current.OutputTokens >= previous.OutputTokens &&
		current.TotalTokens >= previous.TotalTokens &&
		current.EstimatedCostMicros >= previous.EstimatedCostMicros
}
