package evidence

import (
	"fmt"
	"strings"

	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

func validatePlatformSearchVerifierTask(
	task agentRuntime.TaskSpec,
	criterionID string,
) error {
	for _, criterion := range task.CompletionCriteria {
		if criterion.Required && strings.TrimSpace(criterion.ID) != criterionID {
			return fmt.Errorf(
				"platform search verifier cannot prove required criterion %q",
				criterion.ID,
			)
		}
	}
	return nil
}
