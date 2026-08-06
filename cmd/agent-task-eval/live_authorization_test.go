package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"twitter-clone/internal/module/agent/eval"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

type fixedLiveAuthorizationEstimator struct {
	micros int64
}

func (estimator fixedLiveAuthorizationEstimator) EstimateCost(string, agentRuntime.TokenUsage) (agentRuntime.CostEstimate, error) {
	return agentRuntime.CostEstimate{Micros: estimator.micros, PricingVersion: "pricing-v1"}, nil
}

type countingLiveAuthorizationModel struct {
	calls int
}

func (model *countingLiveAuthorizationModel) Complete(context.Context, agentRuntime.ModelRequest) (agentRuntime.ModelResponse, error) {
	model.calls++
	return agentRuntime.ModelResponse{Model: "fixed-model", Provider: "local"}, nil
}

func TestAgentTaskLiveAuthorizationBindsIdentityAndExpiry(t *testing.T) {
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	key := []byte("live-authorization-test-key-material-v1")
	binding := agentTaskLiveAuthorizationBinding{
		Provider: "dashscope", Model: "qwen-fixed", DatasetVersion: "cases-v1",
		DatasetSHA256: strings.Repeat("a", 64), ExecutionConfigSHA256: strings.Repeat("b", 64),
	}
	authorization, err := buildAndSignAgentTaskLiveAuthorization(agentTaskLiveAuthorization{
		AuthorizationID: "authorization-001", ExpiresAt: now.Add(time.Hour),
		Provider: binding.Provider, Model: binding.Model, DatasetVersion: binding.DatasetVersion,
		DatasetSHA256: binding.DatasetSHA256, ExecutionConfigSHA256: binding.ExecutionConfigSHA256,
		Limits: agentTaskLiveAuthorizationLimits{
			MaxRuns: 1, MaxProviderCalls: 10, MaxCapturedOutputs: 4, MaxEstimatedCostMicros: 100,
		},
	}, key, "authorization-key-v1", now)
	if err != nil {
		t.Fatalf("build authorization: %v", err)
	}
	path := filepath.Join(t.TempDir(), "authorization.json")
	if err := writeAgentTaskLiveAuthorization(path, authorization); err != nil {
		t.Fatalf("write authorization: %v", err)
	}
	if _, err := loadAndVerifyAgentTaskLiveAuthorization(path, key, "authorization-key-v1", binding, now.Add(time.Minute)); err != nil {
		t.Fatalf("verify authorization: %v", err)
	}
	mismatch := binding
	mismatch.Model = "another-model"
	if _, err := loadAndVerifyAgentTaskLiveAuthorization(path, key, "authorization-key-v1", mismatch, now.Add(time.Minute)); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("model mismatch was accepted: %v", err)
	}
	if _, err := loadAndVerifyAgentTaskLiveAuthorization(path, key, "authorization-key-v1", binding, now.Add(2*time.Hour)); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expired authorization was accepted: %v", err)
	}
}

func TestAgentTaskLiveAuthorizationLedgerIsAppendOnlyAndFailClosed(t *testing.T) {
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	key := []byte("live-authorization-ledger-key-material-v1")
	authorization, err := buildAndSignAgentTaskLiveAuthorization(agentTaskLiveAuthorization{
		AuthorizationID: "authorization-ledger-001", ExpiresAt: now.Add(time.Hour),
		Provider: "local", Model: "fixed-model", DatasetVersion: "cases-v1",
		DatasetSHA256: strings.Repeat("a", 64), ExecutionConfigSHA256: strings.Repeat("b", 64),
		Limits: agentTaskLiveAuthorizationLimits{
			MaxRuns: 1, MaxProviderCalls: 2, MaxCapturedOutputs: 2, MaxEstimatedCostMicros: 15,
		},
	}, key, "authorization-key-v1", now)
	if err != nil {
		t.Fatalf("build authorization: %v", err)
	}
	root := t.TempDir()
	ledger, err := newAgentTaskLiveAuthorizationLedger(root, authorization, key, "authorization-key-v1")
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	if err := ledger.ReserveRun(2, now); err != nil {
		t.Fatalf("reserve run: %v", err)
	}
	if err := ledger.ReserveProviderCall("case-1", 7, now.Add(time.Second)); err != nil {
		t.Fatalf("reserve first call: %v", err)
	}
	second, err := newAgentTaskLiveAuthorizationLedger(root, authorization, key, "authorization-key-v1")
	if err != nil {
		t.Fatalf("open second ledger: %v", err)
	}
	if err := second.ReserveProviderCall("case-2", 8, now.Add(2*time.Second)); err != nil {
		t.Fatalf("reserve second call: %v", err)
	}
	if err := second.ReserveProviderCall("case-3", 1, now.Add(3*time.Second)); err == nil || !strings.Contains(err.Error(), "provider call budget exhausted") {
		t.Fatalf("provider call overflow was accepted: %v", err)
	}
	if err := second.ReserveRun(0, now.Add(4*time.Second)); err == nil || !strings.Contains(err.Error(), "run budget exhausted") {
		t.Fatalf("run overflow was accepted: %v", err)
	}
	if err := second.ReserveProviderCall("expired-case", 0, authorization.ExpiresAt); err == nil || !strings.Contains(err.Error(), "outside validity window") {
		t.Fatalf("expired event was accepted: %v", err)
	}
	records, usage, err := second.load()
	if err != nil {
		t.Fatalf("reload ledger: %v", err)
	}
	if len(records) != 3 || usage.Runs != 1 || usage.ProviderCalls != 2 || usage.CapturedOutputs != 2 || usage.EstimatedCostMicros != 15 {
		t.Fatalf("ledger records/usage = %d/%+v", len(records), usage)
	}

	recordPath := filepath.Join(root, authorization.AuthorizationID, "000002.json")
	payload, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatalf("read record: %v", err)
	}
	var record map[string]any
	if err := json.Unmarshal(payload, &record); err != nil {
		t.Fatalf("decode record: %v", err)
	}
	record["estimated_cost_micros"] = float64(1)
	tampered, _ := json.Marshal(record)
	if err := os.WriteFile(recordPath, tampered, 0o600); err != nil {
		t.Fatalf("tamper record: %v", err)
	}
	if _, _, err := second.load(); err == nil {
		t.Fatal("tampered ledger record was accepted")
	}
}

func TestAuthorizedLiveModelClientReservesBeforeProviderCall(t *testing.T) {
	now := time.Now().UTC()
	key := []byte("authorized-live-model-key-material-v1")
	authorization, err := buildAndSignAgentTaskLiveAuthorization(agentTaskLiveAuthorization{
		AuthorizationID: "authorization-model-001", ExpiresAt: now.Add(time.Hour),
		Provider: "local", Model: "fixed-model", DatasetVersion: "cases-v1",
		DatasetSHA256: strings.Repeat("a", 64), ExecutionConfigSHA256: strings.Repeat("b", 64),
		Limits: agentTaskLiveAuthorizationLimits{
			MaxRuns: 1, MaxProviderCalls: 1, MaxCapturedOutputs: 0, MaxEstimatedCostMicros: 5,
		},
	}, key, "authorization-key-v1", now)
	if err != nil {
		t.Fatalf("build authorization: %v", err)
	}
	ledger, err := newAgentTaskLiveAuthorizationLedger(t.TempDir(), authorization, key, "authorization-key-v1")
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	if err := ledger.ReserveRun(0, now); err != nil {
		t.Fatalf("reserve run: %v", err)
	}
	delegate := &countingLiveAuthorizationModel{}
	client, err := newAuthorizedLiveModelClient(delegate, ledger, fixedLiveAuthorizationEstimator{micros: 5}, "fixed-model", 64, true)
	if err != nil {
		t.Fatalf("configure client: %v", err)
	}
	request := agentRuntime.ModelRequest{
		Context: agentRuntime.RunContext{RunID: "case-1"}, Model: "fixed-model",
		Messages: []agentRuntime.Message{{Role: agentRuntime.RoleUser, Content: "hello"}}, MaxOutputTokens: 8,
	}
	if _, err := client.Complete(t.Context(), request); err != nil {
		t.Fatalf("first call: %v", err)
	}
	request.Context.RunID = "case-2"
	if _, err := client.Complete(t.Context(), request); err == nil || !strings.Contains(err.Error(), "provider call budget exhausted") {
		t.Fatalf("second call was not blocked: %v", err)
	}
	if delegate.calls != 1 {
		t.Fatalf("provider calls = %d, want 1", delegate.calls)
	}
}

func TestAgentTaskLiveAuthorizationLedgerSerializesConcurrentInstances(t *testing.T) {
	now := time.Now().UTC()
	key := []byte("concurrent-live-ledger-key-material-v1")
	const calls = 8
	authorization, err := buildAndSignAgentTaskLiveAuthorization(agentTaskLiveAuthorization{
		AuthorizationID: "authorization-concurrent-001", ExpiresAt: now.Add(time.Hour),
		Provider: "local", Model: "fixed-model", DatasetVersion: "cases-v1",
		DatasetSHA256: strings.Repeat("a", 64), ExecutionConfigSHA256: strings.Repeat("b", 64),
		Limits: agentTaskLiveAuthorizationLimits{
			MaxRuns: 1, MaxProviderCalls: calls, MaxCapturedOutputs: 0, MaxEstimatedCostMicros: calls,
		},
	}, key, "authorization-key-v1", now)
	if err != nil {
		t.Fatalf("build authorization: %v", err)
	}
	root := t.TempDir()
	ledger, err := newAgentTaskLiveAuthorizationLedger(root, authorization, key, "authorization-key-v1")
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	if err := ledger.ReserveRun(0, now); err != nil {
		t.Fatalf("reserve run: %v", err)
	}

	ledgers := make([]*agentTaskLiveAuthorizationLedger, calls)
	for index := range ledgers {
		ledgers[index], err = newAgentTaskLiveAuthorizationLedger(root, authorization, key, "authorization-key-v1")
		if err != nil {
			t.Fatalf("open concurrent ledger %d: %v", index, err)
		}
	}
	start := make(chan struct{})
	errorsByCall := make(chan error, calls)
	var wait sync.WaitGroup
	for index, current := range ledgers {
		wait.Add(1)
		go func(index int, current *agentTaskLiveAuthorizationLedger) {
			defer wait.Done()
			<-start
			errorsByCall <- current.ReserveProviderCall(fmt.Sprintf("case-%d", index), 1, now.Add(time.Second))
		}(index, current)
	}
	close(start)
	wait.Wait()
	close(errorsByCall)
	for callErr := range errorsByCall {
		if callErr != nil {
			t.Fatalf("concurrent reservation: %v", callErr)
		}
	}
	records, usage, err := ledger.load()
	if err != nil {
		t.Fatalf("load concurrent ledger: %v", err)
	}
	if len(records) != calls+1 || usage.Runs != 1 || usage.ProviderCalls != calls || usage.EstimatedCostMicros != calls {
		t.Fatalf("concurrent ledger records/usage = %d/%+v", len(records), usage)
	}
}

func TestRunCreatesSignedLiveAuthorizationWithoutProviderCall(t *testing.T) {
	key := "create-live-authorization-key-material-v1"
	t.Setenv("TEST_CREATE_LIVE_AUTHORIZATION_KEY", key)
	tempDir := t.TempDir()
	datasetPath := filepath.Join(tempDir, "dataset.json")
	configPath := filepath.Join(tempDir, "runtime.json")
	authorizationPath := filepath.Join(tempDir, "authorization.json")
	if err := os.WriteFile(datasetPath, []byte(`[{"id":"case-1","category":"chat","mode":"chat","input":"hello","expected_outcome":"completed"}]`), 0o600); err != nil {
		t.Fatalf("write dataset: %v", err)
	}
	config := validRuntimeEvalConfig()
	config.Provider = "lmstudio"
	config.Model = "fixed-model"
	encoded, _ := json.Marshal(config)
	if err := os.WriteFile(configPath, encoded, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{
		"--create-live-authorization", authorizationPath,
		"--live-authorization-id", "created-authorization-001",
		"--live-authorization-ttl", "1h",
		"--live-authorization-max-runs", "1",
		"--live-authorization-max-provider-calls", "10",
		"--dataset", datasetPath,
		"--dataset-version", "cases-v1",
		"--runtime-config", configPath,
		"--live-authorization-key-env", "TEST_CREATE_LIVE_AUTHORIZATION_KEY",
		"--live-authorization-key-id", "create-key-v1",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("create authorization: code=%d stderr=%q", exitCode, stderr.String())
	}
	dataset, _ := loadAgentTaskDataset(datasetPath)
	datasetHash, _ := eval.HashAgentTaskDataset(dataset)
	configHash, _ := hashRuntimeEvalConfig(config)
	if _, err := loadAndVerifyAgentTaskLiveAuthorization(
		authorizationPath, []byte(key), "create-key-v1",
		agentTaskLiveAuthorizationBinding{
			Provider: "lmstudio", Model: "fixed-model", DatasetVersion: "cases-v1",
			DatasetSHA256: datasetHash, ExecutionConfigSHA256: configHash,
		}, time.Now().UTC(),
	); err != nil {
		t.Fatalf("verify created authorization: %v", err)
	}
}

func TestRunRejectsLiveEvaluationWithoutAuthorizationBeforeProviderCall(t *testing.T) {
	t.Setenv("TEST_LIVE_REPORT_KEY", "live-report-integrity-key-material-v1")
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "runtime.json")
	config := validRuntimeEvalConfig()
	config.BaseURL = "http://127.0.0.1:1/v1"
	encoded, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("encode config: %v", err)
	}
	if err := os.WriteFile(configPath, encoded, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{
		"--allow-live",
		"--runtime-config", configPath,
		"--integrity-key-env", "TEST_LIVE_REPORT_KEY",
		"--integrity-key-id", "report-key-v1",
	}, &stdout, &stderr)
	if exitCode == 0 || !strings.Contains(stderr.String(), "requires --live-authorization and --live-authorization-state") {
		t.Fatalf("live evaluation without authorization was accepted: code=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
}
