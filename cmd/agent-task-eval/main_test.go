package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"twitter-clone/internal/module/agent/eval"
)

func TestRunWritesPassingOfflineComparison(t *testing.T) {
	dataset := filepath.Join("..", "..", "internal", "module", "agent", "eval", "testdata", "agent_task_cases.json")
	results := filepath.Join("..", "..", "internal", "module", "agent", "eval", "testdata", "agent_task_recorded_results.json")
	out := filepath.Join(t.TempDir(), "report.json")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{
		"--dataset", dataset,
		"--results", results,
		"--stable-results", results,
		"--out", out,
		"--enforce-gate",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("run failed with %d: %s", exitCode, stderr.String())
	}
	file, err := loadOutput(out)
	if err != nil {
		t.Fatalf("load output: %v", err)
	}
	if file.Gate == nil || file.Gate.Status != eval.AgentQualityGatePassed {
		t.Fatalf("expected passing gate: %#v", file.Gate)
	}
	if file.Candidate.Metrics.Cases < 50 || file.Candidate.Metrics.UnauthorizedWriteSuccesses != 0 || file.Candidate.Metrics.FabricatedToolResults != 0 {
		t.Fatalf("unexpected candidate metrics: %#v", file.Candidate.Metrics)
	}
}

func TestRunRequiresStableReportWhenEnforcingGate(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"--enforce-gate"}, &stdout, &stderr)
	if exitCode != 2 || !bytes.Contains(stderr.Bytes(), []byte("requires --stable-results")) {
		t.Fatalf("unexpected result: code=%d stderr=%q", exitCode, stderr.String())
	}
}

func TestRunWritesPassingOfflineStrategyComparison(t *testing.T) {
	const key = "0123456789abcdef0123456789abcdef"
	t.Setenv("TEST_AGENT_STRATEGY_EVAL_KEY", key)
	base := filepath.Join("..", "..", "internal", "module", "agent", "eval", "testdata")
	out := filepath.Join(t.TempDir(), "strategy-report.json")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{
		"--dataset", filepath.Join(base, "agent_strategy_cases.json"),
		"--dataset-version", "agent-strategy-cases-v2",
		"--results", filepath.Join(base, "agent_strategy_multi_results.json"),
		"--stable-results", filepath.Join(base, "agent_strategy_single_results.json"),
		"--min-cases", "20",
		"--enforce-gate",
		"--strategy-gate",
		"--enforce-strategy-gate",
		"--integrity-key-env", "TEST_AGENT_STRATEGY_EVAL_KEY",
		"--integrity-key-id", "strategy-eval-key-v1",
		"--out", out,
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("strategy comparison failed with %d: %s", exitCode, stderr.String())
	}
	output, err := loadOutput(out)
	if err != nil {
		t.Fatalf("load strategy output: %v", err)
	}
	if output.StrategyGate == nil || output.StrategyGate.Status != eval.AgentQualityGatePassed {
		t.Fatalf("strategy gate = %#v", output.StrategyGate)
	}
	if err := eval.VerifyAgentTaskEvaluationOutput(output, []byte(key), "strategy-eval-key-v1"); err != nil {
		t.Fatalf("verify signed strategy output: %v", err)
	}
	if output.StrategyGate.SemanticGainBPS != 2000 || output.StrategyGate.AverageCostRatioBPS <= 10000 ||
		output.StrategyGate.P95LatencyRatioBPS <= 10000 {
		t.Fatalf("strategy metrics = %#v", output.StrategyGate)
	}
}

func TestRunEnforcedStrategyComparisonReturnsNonZeroOnCostRegression(t *testing.T) {
	base := filepath.Join("..", "..", "internal", "module", "agent", "eval", "testdata")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{
		"--dataset", filepath.Join(base, "agent_strategy_cases.json"),
		"--dataset-version", "agent-strategy-cases-v2",
		"--results", filepath.Join(base, "agent_strategy_multi_results.json"),
		"--stable-results", filepath.Join(base, "agent_strategy_single_results.json"),
		"--strategy-gate",
		"--enforce-strategy-gate",
		"--strategy-max-average-cost-ratio-bps", "20000",
	}, &stdout, &stderr)
	if exitCode != 2 || !strings.Contains(stderr.String(), "strategy gate did not pass") {
		t.Fatalf("unexpected result: code=%d stderr=%q", exitCode, stderr.String())
	}
	var output agentTaskEvaluationOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("decode strategy output: %v", err)
	}
	if output.StrategyGate == nil || output.StrategyGate.Status != eval.AgentQualityGateFailed ||
		!slices.Contains(output.StrategyGate.ReasonCodes, "average_cost_ratio_exceeded") {
		t.Fatalf("strategy gate = %#v", output.StrategyGate)
	}
}

func TestRunRequiresStableEvidenceForStrategyGate(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"--enforce-strategy-gate"}, &stdout, &stderr)
	if exitCode != 2 || !strings.Contains(stderr.String(), "requires --stable-results") {
		t.Fatalf("unexpected result: code=%d stderr=%q", exitCode, stderr.String())
	}
}

func TestRunSignsVerifiesAndUsesStableReport(t *testing.T) {
	const key = "0123456789abcdef0123456789abcdef"
	t.Setenv("TEST_AGENT_TASK_EVAL_INTEGRITY_KEY", key)
	dataset := filepath.Join("..", "..", "internal", "module", "agent", "eval", "testdata", "agent_task_cases.json")
	results := filepath.Join("..", "..", "internal", "module", "agent", "eval", "testdata", "agent_task_recorded_results.json")
	stablePath := filepath.Join(t.TempDir(), "stable.json")
	common := []string{
		"--dataset", dataset,
		"--results", results,
		"--integrity-key-env", "TEST_AGENT_TASK_EVAL_INTEGRITY_KEY",
		"--integrity-key-id", "test-key-v1",
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run(append(append([]string(nil), common...), "--out", stablePath), &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("write signed report failed with %d: %s", exitCode, stderr.String())
	}
	signed, err := loadOutput(stablePath)
	if err != nil {
		t.Fatalf("load signed output: %v", err)
	}
	if signed.Integrity == nil || signed.Integrity.KeyID != "test-key-v1" {
		t.Fatalf("missing report signature: %#v", signed.Integrity)
	}

	stdout.Reset()
	stderr.Reset()
	exitCode = run([]string{
		"--verify-report", stablePath,
		"--integrity-key-env", "TEST_AGENT_TASK_EVAL_INTEGRITY_KEY",
		"--integrity-key-id", "test-key-v1",
	}, &stdout, &stderr)
	if exitCode != 0 || !strings.Contains(stdout.String(), "report verified") {
		t.Fatalf("verify signed report failed: code=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}

	candidatePath := filepath.Join(t.TempDir(), "candidate.json")
	stdout.Reset()
	stderr.Reset()
	gateArgs := append(append([]string(nil), common...), "--stable-report", stablePath, "--enforce-gate", "--out", candidatePath)
	exitCode = run(gateArgs, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("signed stable gate failed with %d: %s", exitCode, stderr.String())
	}
	candidate, err := loadOutput(candidatePath)
	if err != nil {
		t.Fatalf("load candidate output: %v", err)
	}
	if candidate.Gate == nil || candidate.Gate.Status != eval.AgentQualityGatePassed || candidate.Stable == nil {
		t.Fatalf("unexpected signed stable gate: %#v", candidate)
	}
}

func TestRunRejectsTamperedSignedReport(t *testing.T) {
	const key = "0123456789abcdef0123456789abcdef"
	t.Setenv("TEST_AGENT_TASK_EVAL_INTEGRITY_KEY", key)
	dataset := filepath.Join("..", "..", "internal", "module", "agent", "eval", "testdata", "agent_task_cases.json")
	results := filepath.Join("..", "..", "internal", "module", "agent", "eval", "testdata", "agent_task_recorded_results.json")
	path := filepath.Join(t.TempDir(), "report.json")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{
		"--dataset", dataset, "--results", results, "--out", path,
		"--integrity-key-env", "TEST_AGENT_TASK_EVAL_INTEGRITY_KEY", "--integrity-key-id", "test-key-v1",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("write signed report: %s", stderr.String())
	}
	output, err := loadOutput(path)
	if err != nil {
		t.Fatalf("load signed report: %v", err)
	}
	output.Candidate.Metrics.Cases++
	payload, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		t.Fatalf("encode tampered report: %v", err)
	}
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatalf("write tampered report: %v", err)
	}
	stdout.Reset()
	stderr.Reset()
	exitCode = run([]string{
		"--verify-report", path,
		"--integrity-key-env", "TEST_AGENT_TASK_EVAL_INTEGRITY_KEY", "--integrity-key-id", "test-key-v1",
	}, &stdout, &stderr)
	if exitCode != 2 || !strings.Contains(stderr.String(), "payload hash mismatch") {
		t.Fatalf("tampered report was not rejected: code=%d stderr=%q", exitCode, stderr.String())
	}
}

func TestLoadVerifiedEvaluationOutputRejectsUnsignedUnknownFields(t *testing.T) {
	const key = "0123456789abcdef0123456789abcdef"
	output := agentTaskEvaluationOutput{
		SchemaVersion: agentTaskEvaluationSchemaVersion,
		Candidate: eval.AgentTaskReport{
			DatasetVersion: "v1", DatasetSHA256: strings.Repeat("a", 64), ExecutionConfigHash: strings.Repeat("b", 64),
		},
	}
	if err := signEvaluationOutput(&output, []byte(key), "test-key-v1", time.Now()); err != nil {
		t.Fatalf("sign output: %v", err)
	}
	payload, err := json.Marshal(output)
	if err != nil {
		t.Fatalf("encode output: %v", err)
	}
	payload = bytes.Replace(payload, []byte(`{"schema_version"`), []byte(`{"unsigned_note":"ignored","schema_version"`), 1)
	path := filepath.Join(t.TempDir(), "unknown-field.json")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatalf("write output: %v", err)
	}
	_, err = loadVerifiedEvaluationOutput(path, []byte(key), "test-key-v1")
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected unknown field rejection, got %v", err)
	}
}

func TestRunLiveRuntimeUsesFixedProfileAndSignedReport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/chat/completions" {
			http.NotFound(writer, request)
			return
		}
		payload, _ := io.ReadAll(request.Body)
		writer.Header().Set("Content-Type", "application/json")
		if bytes.Contains(payload, []byte(`"eval_preflight"`)) {
			_, _ = io.WriteString(writer, openAIPreflightResponse())
			return
		}
		_, _ = io.WriteString(writer, `{
			"id":"eval","object":"chat.completion","created":1,"model":"fixed-model",
			"choices":[{"index":0,"message":{"role":"assistant","content":"云原生通过容器、编排、弹性和可观测性构建现代应用。"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":20,"completion_tokens":18,"total_tokens":38}
		}`)
	}))
	defer server.Close()

	const key = "0123456789abcdef0123456789abcdef"
	t.Setenv("TEST_AGENT_TASK_EVAL_INTEGRITY_KEY", key)
	tempDir := t.TempDir()
	datasetPath := filepath.Join(tempDir, "dataset.json")
	configPath := filepath.Join(tempDir, "runtime.json")
	outPath := filepath.Join(tempDir, "report.json")
	datasetPayload, err := json.Marshal([]eval.AgentTaskCase{{
		ID: "live-chat", Category: "direct_chat", Mode: "chat", Input: "什么是云原生？",
		ExpectedOutcome: eval.AgentTaskOutcomeCompleted, RequiredKeywords: []string{"云原生"}, MinOutputCharacters: 20,
	}})
	if err != nil {
		t.Fatalf("encode live dataset: %v", err)
	}
	if err := os.WriteFile(datasetPath, datasetPayload, 0o600); err != nil {
		t.Fatalf("write live dataset: %v", err)
	}
	config := validRuntimeEvalConfig()
	config.BaseURL = server.URL + "/v1"
	config.Model = "fixed-model"
	configPayload, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("encode live config: %v", err)
	}
	if err := os.WriteFile(configPath, configPayload, 0o600); err != nil {
		t.Fatalf("write live config: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	args := []string{
		"--dataset", datasetPath,
		"--runtime-config", configPath,
		"--allow-live",
		"--out", outPath,
		"--integrity-key-env", "TEST_AGENT_TASK_EVAL_INTEGRITY_KEY",
		"--integrity-key-id", "test-key-v1",
	}
	args = append(args, testLiveAuthorizationArgs(t, datasetPath, "agent-task-cases-v1", configPath, "", 1, 0)...)
	exitCode := run(args, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("live runtime evaluation failed with %d: %s", exitCode, stderr.String())
	}
	output, err := loadOutput(outPath)
	if err != nil {
		t.Fatalf("load live output: %v", err)
	}
	if output.Candidate.Metrics.Passed != 1 || output.Candidate.Execution.Kind != "runtime_live" || output.Candidate.Execution.Provider != "lmstudio" {
		t.Fatalf("unexpected live report: %#v", output.Candidate)
	}
	if output.Integrity == nil || output.Candidate.ExecutionConfigHash == "" || output.Candidate.DatasetSHA256 == "" {
		t.Fatalf("live report is missing integrity evidence: %#v", output)
	}
	if output.LiveAuthorization == nil ||
		output.LiveAuthorization.AuthorizationID == "" ||
		len(output.LiveAuthorization.AuthorizationPayloadSHA256) != 64 ||
		len(output.LiveAuthorization.InvocationSHA256) != 64 ||
		output.LiveAuthorization.AuthorizationKeyID != "test-live-authorization-key-v1" ||
		output.LiveAuthorization.Limits.MaxRuns != 1 {
		t.Fatalf("live report is missing authorization evidence: %#v", output.LiveAuthorization)
	}
}

func loadOutput(path string) (agentTaskEvaluationOutput, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return agentTaskEvaluationOutput{}, err
	}
	var output agentTaskEvaluationOutput
	err = json.Unmarshal(data, &output)
	return output, err
}
