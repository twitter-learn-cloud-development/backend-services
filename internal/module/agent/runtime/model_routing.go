package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

const (
	ProviderRoutingEvidenceSource = "agent_runtime.model_routing"
	ProviderRouteBlockedCode      = "provider_route_blocked"
)

type ModelProviderFailureCode string

const (
	ModelProviderFailureUnavailable  ModelProviderFailureCode = "provider_unavailable"
	ModelProviderFailureRateLimited  ModelProviderFailureCode = "provider_rate_limited"
	ModelProviderFailureTimeout      ModelProviderFailureCode = "provider_timeout"
	ModelProviderFailureInvalidInput ModelProviderFailureCode = "provider_invalid_request"
	ModelProviderFailureUnauthorized ModelProviderFailureCode = "provider_unauthorized"
	ModelProviderFailureCanceled     ModelProviderFailureCode = "provider_canceled"
	ModelProviderFailureUnclassified ModelProviderFailureCode = "provider_unclassified"
)

type ModelRouteDecision string

const (
	ModelRouteSelected          ModelRouteDecision = "selected"
	ModelRouteFallbackAllowed   ModelRouteDecision = "fallback_allowed"
	ModelRouteFallbackDenied    ModelRouteDecision = "fallback_denied"
	ModelRouteFallbackExhausted ModelRouteDecision = "fallback_exhausted"
)

type ModelRoutingAttempt struct {
	Model       string                   `json:"model"`
	Provider    string                   `json:"provider"`
	FailureCode ModelProviderFailureCode `json:"failure_code"`
	Decision    ModelRouteDecision       `json:"decision"`
}

// ModelRoutingTrace contains only stable route metadata and fixed failure
// codes. Provider error messages, credentials and response bodies stay out of
// Runtime checkpoints and evidence.
type ModelRoutingTrace struct {
	RequestedModel   string                `json:"requested_model"`
	SelectedModel    string                `json:"selected_model,omitempty"`
	SelectedProvider string                `json:"selected_provider,omitempty"`
	Attempts         []ModelRoutingAttempt `json:"attempts,omitempty"`
	TerminalDecision ModelRouteDecision    `json:"terminal_decision"`
}

func cloneModelRoutingTrace(trace *ModelRoutingTrace) *ModelRoutingTrace {
	if trace == nil {
		return nil
	}
	cloned := *trace
	cloned.Attempts = append([]ModelRoutingAttempt(nil), trace.Attempts...)
	return &cloned
}

func projectProviderRoutingBlock(
	task TaskSpec,
	result *VerifiedRunResult,
) (bool, error) {
	if result == nil {
		return false, nil
	}
	step, trace := terminalBlockedModelRoute(result.Run.Steps)
	if trace == nil {
		return false, nil
	}
	if err := validateBlockedModelRoute(*trace); err != nil {
		return false, &RunError{
			Code: ErrorInvalidAction, Step: step.Index,
			Message: "provider routing trace is invalid", Cause: err,
		}
	}

	payload, err := json.Marshal(trace)
	if err != nil {
		return false, fmt.Errorf("marshal provider routing evidence: %w", err)
	}
	digest := sha256.Sum256(payload)
	runID := strings.TrimSpace(result.Run.Context.RunID)
	evidenceID := fmt.Sprintf("provider-route:%s:%d", runID, step.Index)
	item := Evidence{
		ID: evidenceID, Kind: EvidenceProviderRouting, Source: ProviderRoutingEvidenceSource,
		Digest: "sha256:" + hex.EncodeToString(digest[:]),
		Reference: fmt.Sprintf(
			"agent-run://%s/model-routes/%d", url.PathEscape(runID), step.Index,
		),
		StepIndex: step.Index, CapturedAt: step.FinishedAt,
	}
	result.Evidence, err = result.Evidence.With(item)
	if err != nil {
		return false, fmt.Errorf("append provider routing evidence: %w", err)
	}

	verification := VerificationResult{Status: VerificationInconclusive}
	for _, criterion := range task.CompletionCriteria {
		if !criterion.Required {
			continue
		}
		verification.MissingEvidence = append(verification.MissingEvidence, criterion.ID)
		verification.Checks = append(verification.Checks, CheckResult{
			CriterionID: criterion.ID, Status: VerificationInconclusive,
			Code: ProviderRouteBlockedCode, EvidenceIDs: []string{evidenceID},
		})
	}
	result.Verification = verification
	result.Status = GoalRunBlocked
	return true, nil
}

func terminalBlockedModelRoute(steps []Step) (Step, *ModelRoutingTrace) {
	for index := len(steps) - 1; index >= 0; index-- {
		trace := steps[index].ModelRouting
		if trace == nil {
			continue
		}
		switch trace.TerminalDecision {
		case ModelRouteFallbackDenied, ModelRouteFallbackExhausted:
			return steps[index], trace
		default:
			return Step{}, nil
		}
	}
	return Step{}, nil
}

func validateBlockedModelRoute(trace ModelRoutingTrace) error {
	if strings.TrimSpace(trace.RequestedModel) == "" {
		return fmt.Errorf("requested model is required")
	}
	if trace.SelectedModel != "" || trace.SelectedProvider != "" {
		return fmt.Errorf("blocked route cannot select a model")
	}
	if len(trace.Attempts) == 0 {
		return fmt.Errorf("blocked route requires at least one attempt")
	}
	if trace.TerminalDecision != ModelRouteFallbackDenied &&
		trace.TerminalDecision != ModelRouteFallbackExhausted {
		return fmt.Errorf("blocked route terminal decision is invalid")
	}
	last := trace.Attempts[len(trace.Attempts)-1]
	if last.Decision != trace.TerminalDecision {
		return fmt.Errorf("last attempt does not match terminal decision")
	}
	for index, attempt := range trace.Attempts {
		if strings.TrimSpace(attempt.Model) == "" || strings.TrimSpace(attempt.Provider) == "" ||
			attempt.FailureCode == "" || attempt.Decision == "" {
			return fmt.Errorf("attempt %d is incomplete", index+1)
		}
		if index < len(trace.Attempts)-1 && attempt.Decision != ModelRouteFallbackAllowed {
			return fmt.Errorf("non-terminal attempt %d did not allow fallback", index+1)
		}
	}
	return nil
}
