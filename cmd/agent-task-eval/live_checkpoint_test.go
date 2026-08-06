package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"twitter-clone/internal/module/agent/eval"
)

func TestRunLiveRuntimeResumesSignedCheckpointAfterProviderFailure(t *testing.T) {
	var taskCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/chat/completions" {
			http.NotFound(writer, request)
			return
		}
		payload, _ := io.ReadAll(request.Body)
		if bytes.Contains(payload, []byte(`"eval_preflight"`)) {
			writer.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(writer, openAIPreflightResponse())
			return
		}
		switch taskCalls.Add(1) {
		case 1:
			writer.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(writer, openAIFinalResponse("PRIVATE_FIRST first complete"))
		case 2:
			http.Error(writer, "controlled provider outage", http.StatusServiceUnavailable)
		default:
			writer.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(writer, openAIFinalResponse("PRIVATE_SECOND second complete"))
		}
	}))
	defer server.Close()

	const key = "0123456789abcdef0123456789abcdef"
	t.Setenv("TEST_AGENT_TASK_CHECKPOINT_KEY", key)
	tempDir := t.TempDir()
	datasetPath := filepath.Join(tempDir, "dataset.json")
	configPath := filepath.Join(tempDir, "runtime.json")
	checkpointRoot := filepath.Join(tempDir, "checkpoint")
	outPath := filepath.Join(tempDir, "report.json")
	datasetPayload, err := json.Marshal([]eval.AgentTaskCase{
		{ID: "live-one", Category: "direct_chat", Mode: "chat", Input: "first", ExpectedOutcome: eval.AgentTaskOutcomeCompleted, RequiredKeywords: []string{"first"}},
		{ID: "live-two", Category: "direct_chat", Mode: "chat", Input: "second", ExpectedOutcome: eval.AgentTaskOutcomeCompleted, RequiredKeywords: []string{"second"}},
	})
	if err != nil {
		t.Fatalf("encode dataset: %v", err)
	}
	if err := os.WriteFile(datasetPath, datasetPayload, 0o600); err != nil {
		t.Fatalf("write dataset: %v", err)
	}
	config := validRuntimeEvalConfig()
	config.BaseURL = server.URL + "/v1"
	config.Model = "fixed-model"
	configPayload, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("encode runtime config: %v", err)
	}
	if err := os.WriteFile(configPath, configPayload, 0o600); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}
	args := []string{
		"--dataset", datasetPath,
		"--dataset-version", "live-checkpoint-v1",
		"--runtime-config", configPath,
		"--allow-live",
		"--checkpoint-dir", checkpointRoot,
		"--progress",
		"--out", outPath,
		"--integrity-key-env", "TEST_AGENT_TASK_CHECKPOINT_KEY",
		"--integrity-key-id", "checkpoint-test-key-v1",
	}
	args = append(args, testLiveAuthorizationArgs(t, datasetPath, "live-checkpoint-v1", configPath, "", 2, 0)...)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exitCode := run(args, &stdout, &stderr); exitCode != 2 || !strings.Contains(stderr.String(), "live-two") {
		t.Fatalf("first run should stop at provider failure: code=%d stderr=%q", exitCode, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(checkpointRoot, "candidate", "000001.json")); err != nil {
		t.Fatalf("first case checkpoint missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(checkpointRoot, "candidate", "000002.json")); !os.IsNotExist(err) {
		t.Fatalf("failed case must not be checkpointed, stat err=%v", err)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := run(args, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("resumed run failed: code=%d stderr=%q", exitCode, stderr.String())
	}
	if !strings.Contains(stderr.String(), "candidate resumed 1/2") || taskCalls.Load() != 3 {
		t.Fatalf("run did not resume the signed prefix: calls=%d stderr=%q", taskCalls.Load(), stderr.String())
	}
	output, err := loadOutput(outPath)
	if err != nil {
		t.Fatalf("load resumed report: %v", err)
	}
	if output.Candidate.Metrics.Passed != 2 || len(output.Candidate.CaseResults) != 2 || output.Integrity == nil {
		t.Fatalf("unexpected resumed report: %#v", output)
	}
	for _, name := range []string{"000001.json", "000002.json"} {
		payload, err := os.ReadFile(filepath.Join(checkpointRoot, "candidate", name))
		if err != nil {
			t.Fatalf("read checkpoint %s: %v", name, err)
		}
		if bytes.Contains(payload, []byte("PRIVATE_FIRST")) || bytes.Contains(payload, []byte("PRIVATE_SECOND")) {
			t.Fatalf("checkpoint %s leaked model output", name)
		}
	}
}

func TestRunLiveRuntimeFailsPreflightBeforeCreatingCheckpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, openAIFinalResponse("not a tool call"))
	}))
	defer server.Close()

	const key = "0123456789abcdef0123456789abcdef"
	t.Setenv("TEST_AGENT_TASK_PREFLIGHT_KEY", key)
	tempDir := t.TempDir()
	datasetPath := filepath.Join(tempDir, "dataset.json")
	configPath := filepath.Join(tempDir, "runtime.json")
	checkpointRoot := filepath.Join(tempDir, "checkpoint")
	if err := os.WriteFile(datasetPath, []byte(`[{"id":"preflight","category":"chat","mode":"chat","input":"hello","expected_outcome":"completed"}]`), 0o600); err != nil {
		t.Fatalf("write dataset: %v", err)
	}
	config := validRuntimeEvalConfig()
	config.BaseURL = server.URL + "/v1"
	config.Model = "fixed-model"
	payload, _ := json.Marshal(config)
	if err := os.WriteFile(configPath, payload, 0o600); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	args := []string{
		"--dataset", datasetPath, "--runtime-config", configPath, "--allow-live",
		"--checkpoint-dir", checkpointRoot,
		"--integrity-key-env", "TEST_AGENT_TASK_PREFLIGHT_KEY", "--integrity-key-id", "preflight-key-v1",
	}
	args = append(args, testLiveAuthorizationArgs(t, datasetPath, "agent-task-cases-v1", configPath, "", 1, 0)...)
	exitCode := run(args, &stdout, &stderr)
	if exitCode != 2 || !strings.Contains(stderr.String(), "model/tool preflight") {
		t.Fatalf("invalid tool-call provider should fail preflight: code=%d stderr=%q", exitCode, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(checkpointRoot, "candidate")); !os.IsNotExist(err) {
		t.Fatalf("preflight failure must not create case checkpoint state: %v", err)
	}
}
