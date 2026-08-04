package eval

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunRouterCalculatesLayerMisroutesAndStageRates(t *testing.T) {
	dataset := []RouterCase{
		{ID: "persona", Query: "persona", ExpectedIntent: "persona_memory", ExpectedLayer: "l1_persona"},
		{ID: "memory", Query: "last time", ExpectedIntent: "episodic", ExpectedLayer: "l2_episodic"},
		{ID: "knowledge", Query: "docs", ExpectedIntent: "global", ExpectedLayer: "l3_global"},
		{ID: "wrong", Query: "action", ExpectedIntent: "global", ExpectedLayer: "l3_global"},
	}
	report, err := RunRouter(context.Background(), dataset, func(_ context.Context, query string) (RouterDecision, error) {
		switch query {
		case "persona":
			return RouterDecision{Intent: "persona_memory", Stage: "lexical"}, nil
		case "last time":
			return RouterDecision{Intent: "episodic", Stage: "lexical"}, nil
		case "docs":
			return RouterDecision{Intent: "global", Stage: "lexical"}, nil
		default:
			return RouterDecision{Intent: "direct_action", Stage: "lexical"}, nil
		}
	}, RouterRunnerConfig{DatasetVersion: "test"})
	if err != nil {
		t.Fatalf("run router evaluation: %v", err)
	}
	if report.Metrics.Correct != 3 || report.Metrics.MisrouteRate != 0.25 {
		t.Fatalf("unexpected metrics: %#v", report.Metrics)
	}
	if report.Metrics.ByLayer["l3_global"].Misrouted != 1 {
		t.Fatalf("unexpected layer metrics: %#v", report.Metrics.ByLayer)
	}
	if report.Metrics.ByStage["lexical"].Cases != 4 || report.Metrics.ByStage["lexical"].Correct != 3 || report.Metrics.ByStage["lexical"].Accuracy != 0.75 {
		t.Fatalf("unexpected stage metrics: %#v", report.Metrics.ByStage)
	}
}

func TestLoadRouterDatasetDerivesLayer(t *testing.T) {
	dataset, err := LoadRouterDataset(strings.NewReader(`[{"id":"p","query":"my profile","expected_intent":"persona_memory"}]`))
	if err != nil {
		t.Fatalf("load dataset: %v", err)
	}
	if dataset[0].ExpectedLayer != "l1_persona" {
		t.Fatalf("expected derived layer, got %#v", dataset[0])
	}
}

func TestLoadRouterDatasetRejectsInconsistentLayer(t *testing.T) {
	_, err := LoadRouterDataset(strings.NewReader(`[{"id":"p","query":"my profile","expected_intent":"persona_memory","expected_layer":"l3_global"}]`))
	if err == nil {
		t.Fatal("expected inconsistent layer to be rejected")
	}
}

func TestRouterDatasetCoversEveryRouteLayer(t *testing.T) {
	file, err := os.Open(filepath.Join("testdata", "router_cases.json"))
	if err != nil {
		t.Fatalf("open router dataset: %v", err)
	}
	defer file.Close()
	dataset, err := LoadRouterDataset(file)
	if err != nil {
		t.Fatalf("load router dataset: %v", err)
	}
	if len(dataset) < 30 {
		t.Fatalf("expected at least 30 router cases, got %d", len(dataset))
	}
	layers := make(map[string]int)
	for _, sample := range dataset {
		layers[sample.ExpectedLayer]++
	}
	for _, layer := range []string{"l1_persona", "l2_episodic", "l3_global", "action"} {
		if layers[layer] < 5 {
			t.Fatalf("expected at least 5 cases for %s, got %d", layer, layers[layer])
		}
	}
}

func TestRunRouterRecordsProviderErrorsAndExecutionMetadata(t *testing.T) {
	dataset := []RouterCase{{ID: "p", Query: "ambiguous", ExpectedIntent: "persona_memory", ExpectedLayer: "l1_persona"}}
	execution := RouterExecutionReport{Mode: "full", SemanticThreshold: 0.82, CaseTimeoutMS: 1000, InitializationError: "anchor unavailable"}
	report, err := RunRouter(context.Background(), dataset, func(context.Context, string) (RouterDecision, error) {
		return RouterDecision{
			Intent: "persona_memory", Stage: "llm_fallback",
			SemanticError: "embedding unavailable", LLMError: "invalid router JSON response",
		}, nil
	}, RouterRunnerConfig{Execution: execution})
	if err != nil {
		t.Fatalf("run router evaluation: %v", err)
	}
	if report.Execution.Mode != "full" || report.Execution.InitializationError == "" || report.Metrics.ProviderErrorCases != 1 || report.Metrics.ProviderErrorRate != 1 {
		t.Fatalf("unexpected provider metrics: %#v", report)
	}
	if report.Metrics.SemanticErrorCases != 1 || report.Metrics.LLMErrorCases != 1 {
		t.Fatalf("unexpected provider error split: %#v", report.Metrics)
	}
}

func TestRunRouterAppliesPerCaseTimeout(t *testing.T) {
	dataset := []RouterCase{{ID: "p", Query: "slow", ExpectedIntent: "global", ExpectedLayer: "l3_global"}}
	report, err := RunRouter(context.Background(), dataset, func(ctx context.Context, _ string) (RouterDecision, error) {
		<-ctx.Done()
		return RouterDecision{}, ctx.Err()
	}, RouterRunnerConfig{CaseTimeout: 10 * time.Millisecond})
	if err != nil {
		t.Fatalf("run router evaluation: %v", err)
	}
	if report.Metrics.Errors != 1 || report.Metrics.ErrorRate != 1 || !strings.Contains(report.CaseResults[0].Error, "deadline") {
		t.Fatalf("expected case timeout to be reported: %#v", report)
	}
}
