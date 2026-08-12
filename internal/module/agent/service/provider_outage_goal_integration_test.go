package service

import (
	"context"
	"errors"
	"testing"

	agentModel "twitter-clone/internal/module/agent/model"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

type providerOutageClient struct {
	calls    int
	response agentRuntime.ModelResponse
	err      error
}

func (client *providerOutageClient) Complete(
	_ context.Context,
	_ agentRuntime.ModelRequest,
) (agentRuntime.ModelResponse, error) {
	client.calls++
	return client.response, client.err
}

func TestProviderOutageGoalUsesAllowedFallbackOrBlocksWithoutFabrication(t *testing.T) {
	t.Run("policy allowed fallback completes", func(t *testing.T) {
		primary := &providerOutageClient{err: agentModel.NewProviderCallError(
			agentRuntime.ModelProviderFailureUnavailable, true, errors.New("primary unavailable"),
		)}
		fallback := &providerOutageClient{response: agentRuntime.ModelResponse{
			Message: agentRuntime.Message{Content: "controlled fallback answer"},
		}}
		router := providerOutageRouter(t, primary, fallback)
		runner := agentRuntime.NewReActRunner(router, nil, nil)

		result, err := runner.Run(context.Background(), providerOutageRunRequest("run-provider-fallback"))
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
		if result.Status != agentRuntime.RunStatusCompleted || result.FinalAnswer != "controlled fallback answer" ||
			primary.calls != 1 || fallback.calls != 1 || len(result.Steps) != 1 {
			t.Fatalf("result/calls = %+v primary:%d fallback:%d", result, primary.calls, fallback.calls)
		}
		trace := result.Steps[0].ModelRouting
		if trace == nil || trace.TerminalDecision != agentRuntime.ModelRouteSelected ||
			trace.SelectedModel != "fallback" || trace.SelectedProvider != "local" ||
			len(trace.Attempts) != 1 || trace.Attempts[0].Decision != agentRuntime.ModelRouteFallbackAllowed {
			t.Fatalf("routing trace = %+v", trace)
		}
	})

	t.Run("all allowed routes exhausted", func(t *testing.T) {
		primary := &providerOutageClient{err: errors.New("primary unavailable")}
		fallback := &providerOutageClient{err: errors.New("fallback unavailable")}
		result := runBlockedProviderGoal(t, primary, fallback, "run-provider-exhausted")

		trace := result.Run.Steps[0].ModelRouting
		if trace.TerminalDecision != agentRuntime.ModelRouteFallbackExhausted ||
			len(trace.Attempts) != 2 || primary.calls != 1 || fallback.calls != 1 {
			t.Fatalf("routing trace/calls = %+v primary:%d fallback:%d", trace, primary.calls, fallback.calls)
		}
	})

	t.Run("permanent failure denies fallback", func(t *testing.T) {
		primary := &providerOutageClient{err: agentModel.NewProviderCallError(
			agentRuntime.ModelProviderFailureUnauthorized, false, errors.New("credential rejected"),
		)}
		fallback := &providerOutageClient{response: agentRuntime.ModelResponse{
			Message: agentRuntime.Message{Content: "must not run"},
		}}
		result := runBlockedProviderGoal(t, primary, fallback, "run-provider-denied")

		trace := result.Run.Steps[0].ModelRouting
		if trace.TerminalDecision != agentRuntime.ModelRouteFallbackDenied ||
			len(trace.Attempts) != 1 || primary.calls != 1 || fallback.calls != 0 {
			t.Fatalf("routing trace/calls = %+v primary:%d fallback:%d", trace, primary.calls, fallback.calls)
		}
	})
}

func runBlockedProviderGoal(
	t *testing.T,
	primary *providerOutageClient,
	fallback *providerOutageClient,
	runID string,
) agentRuntime.VerifiedRunResult {
	t.Helper()
	router := providerOutageRouter(t, primary, fallback)
	runner := agentRuntime.NewVerifiedRunner(
		agentRuntime.NewReActRunner(router, nil, nil),
		agentRuntime.RequiredEvidenceVerifier{},
		nil,
	)
	task := agentRuntime.TaskSpec{
		ID: "E2E-20", Goal: "handle a provider outage without fabricating output",
		CompletionCriteria: []agentRuntime.CompletionCriterion{{
			ID: "provider-route-terminal", Description: "provider routing terminates truthfully", Required: true,
		}},
	}
	result, err := runner.Run(context.Background(), agentRuntime.VerifiedRunRequest{
		Task: task,
		Run:  providerOutageRunRequest(runID),
	})
	if err != nil {
		t.Fatalf("Verified Run() error = %v", err)
	}
	if result.Status != agentRuntime.GoalRunBlocked || result.Run.Status != agentRuntime.RunStatusFailed ||
		result.Run.FinalAnswer != "" || len(result.Run.Messages) != 1 || len(result.Run.Steps) != 1 {
		t.Fatalf("blocked result = %+v", result)
	}
	if result.Verification.Status != agentRuntime.VerificationInconclusive ||
		len(result.Verification.Checks) != 1 ||
		result.Verification.Checks[0].Code != agentRuntime.ProviderRouteBlockedCode {
		t.Fatalf("verification = %+v", result.Verification)
	}
	if len(result.Evidence.Items) != 1 {
		t.Fatalf("evidence = %+v", result.Evidence.Items)
	}
	evidence := result.Evidence.Items[0]
	if evidence.Kind != agentRuntime.EvidenceProviderRouting ||
		evidence.Source != agentRuntime.ProviderRoutingEvidenceSource ||
		evidence.Digest == "" || evidence.Reference == "" ||
		len(evidence.CriterionIDs) != 0 {
		t.Fatalf("provider evidence = %+v", evidence)
	}
	outcome, err := agentRuntime.BuildObservedTaskOutcome(task, result)
	if err != nil {
		t.Fatalf("BuildObservedTaskOutcome() error = %v", err)
	}
	if outcome.Status != agentRuntime.GoalRunBlocked || outcome.FinalAnswerDigest != "" ||
		len(outcome.Artifacts) != 0 || len(outcome.Evidence.Items) != 1 {
		t.Fatalf("outcome = %+v", outcome)
	}
	return result
}

func providerOutageRouter(
	t *testing.T,
	primary agentRuntime.ModelClient,
	fallback agentRuntime.ModelClient,
) *agentModel.ProviderRouter {
	t.Helper()
	catalog, err := agentModel.NewCatalog([]agentModel.Definition{
		{ID: "primary", Provider: "cloud", ContextWindow: 8192, Capabilities: []agentModel.Capability{agentModel.CapabilityChat}, Fallbacks: []string{"fallback"}},
		{ID: "fallback", Provider: "local", ContextWindow: 4096, Capabilities: []agentModel.Capability{agentModel.CapabilityChat}},
	})
	if err != nil {
		t.Fatalf("NewCatalog() error = %v", err)
	}
	router, err := agentModel.NewProviderRouter(catalog, map[string]agentRuntime.ModelClient{
		"cloud": primary,
		"local": fallback,
	})
	if err != nil {
		t.Fatalf("NewProviderRouter() error = %v", err)
	}
	return router
}

func providerOutageRunRequest(runID string) agentRuntime.RunRequest {
	return agentRuntime.RunRequest{
		Context: agentRuntime.RunContext{
			RunID: runID, UserID: 42,
			Budget: agentRuntime.Budget{MaxSteps: 1, MaxOutputTokens: 128, MaxTotalTokens: 512},
		},
		Model:    "primary",
		Messages: []agentRuntime.Message{{Role: agentRuntime.RoleUser, Content: "answer only if a provider succeeds"}},
	}
}
