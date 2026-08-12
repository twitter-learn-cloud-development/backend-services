package runtime

import (
	"strings"
	"testing"
	"time"
)

func TestProjectProviderRoutingBlockRejectsIncompleteTrace(t *testing.T) {
	result := VerifiedRunResult{Run: RunResult{
		Context: RunContext{RunID: "run-forged-route"},
		Status:  RunStatusFailed,
		Steps: []Step{{
			Index: 1,
			ModelRouting: &ModelRoutingTrace{
				RequestedModel:   "primary",
				TerminalDecision: ModelRouteFallbackExhausted,
			},
		}},
	}}
	task := TaskSpec{
		ID: "E2E-20", Goal: "reject forged provider routing evidence",
		CompletionCriteria: []CompletionCriterion{{
			ID: "route-terminal", Description: "route terminates truthfully", Required: true,
		}},
	}

	blocked, err := projectProviderRoutingBlock(task, &result)
	if blocked || !HasErrorCode(err, ErrorInvalidAction) {
		t.Fatalf("projectProviderRoutingBlock() blocked/error = %v/%v", blocked, err)
	}
	if len(result.Evidence.Items) != 0 || result.Status != "" {
		t.Fatalf("forged trace mutated result = %+v", result)
	}
}

func TestProjectProviderRoutingBlockStoresOnlyDigestAndReference(t *testing.T) {
	finishedAt := time.Unix(123, 0).UTC()
	result := VerifiedRunResult{Run: RunResult{
		Context: RunContext{RunID: "run-provider-block"},
		Status:  RunStatusFailed,
		Steps: []Step{{
			Index: 1, FinishedAt: finishedAt,
			ModelRouting: &ModelRoutingTrace{
				RequestedModel: "primary",
				Attempts: []ModelRoutingAttempt{{
					Model: "primary", Provider: "cloud",
					FailureCode: ModelProviderFailureUnavailable,
					Decision:    ModelRouteFallbackExhausted,
				}},
				TerminalDecision: ModelRouteFallbackExhausted,
			},
		}},
	}}
	task := TaskSpec{
		ID: "E2E-20", Goal: "record a blocked provider route",
		CompletionCriteria: []CompletionCriterion{{
			ID: "route-terminal", Description: "route terminates truthfully", Required: true,
		}},
	}

	blocked, err := projectProviderRoutingBlock(task, &result)
	if err != nil || !blocked {
		t.Fatalf("projectProviderRoutingBlock() blocked/error = %v/%v", blocked, err)
	}
	if result.Status != GoalRunBlocked || len(result.Evidence.Items) != 1 {
		t.Fatalf("result = %+v", result)
	}
	evidence := result.Evidence.Items[0]
	if evidence.CapturedAt != finishedAt || !strings.HasPrefix(evidence.Digest, "sha256:") ||
		!strings.HasPrefix(evidence.Reference, "agent-run://run-provider-block/") ||
		strings.Contains(evidence.Reference, "unavailable") {
		t.Fatalf("evidence = %+v", evidence)
	}
}

func TestCloneStepsDeepCopiesModelRoutingAttempts(t *testing.T) {
	steps := []Step{{ModelRouting: &ModelRoutingTrace{
		RequestedModel: "primary",
		Attempts: []ModelRoutingAttempt{{
			Model: "primary", Provider: "cloud",
			FailureCode: ModelProviderFailureUnavailable,
			Decision:    ModelRouteFallbackAllowed,
		}},
		SelectedModel: "fallback", SelectedProvider: "local",
		TerminalDecision: ModelRouteSelected,
	}}}

	cloned := cloneSteps(steps)
	cloned[0].ModelRouting.Attempts[0].Model = "mutated"
	if steps[0].ModelRouting.Attempts[0].Model != "primary" {
		t.Fatalf("source routing trace mutated = %+v", steps[0].ModelRouting)
	}
}
