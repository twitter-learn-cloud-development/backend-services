package main

import (
	"path/filepath"
	"testing"
	"time"

	"twitter-clone/internal/module/agent/eval"
)

func testLiveAuthorizationArgs(
	t *testing.T,
	datasetPath string,
	datasetVersion string,
	runtimeConfigPath string,
	strategyConfigPath string,
	maxRuns int,
	maxCapturedOutputs int,
) []string {
	t.Helper()
	key := []byte("test-live-authorization-key-material-v1")
	keyID := "test-live-authorization-key-v1"
	keyEnv := "TEST_AGENT_TASK_LIVE_AUTHORIZATION_KEY"
	t.Setenv(keyEnv, string(key))

	dataset, err := loadAgentTaskDataset(datasetPath)
	if err != nil {
		t.Fatalf("load authorization dataset: %v", err)
	}
	datasetHash, err := eval.HashAgentTaskDataset(dataset)
	if err != nil {
		t.Fatalf("hash authorization dataset: %v", err)
	}
	provider := ""
	model := ""
	configHash := ""
	switch {
	case runtimeConfigPath != "" && strategyConfigPath == "":
		config, loadErr := loadRuntimeEvalConfig(runtimeConfigPath)
		if loadErr != nil {
			t.Fatalf("load authorization runtime config: %v", loadErr)
		}
		provider, model = config.Provider, config.Model
		configHash, err = hashRuntimeEvalConfig(config)
	case strategyConfigPath != "" && runtimeConfigPath == "":
		config, loadErr := loadStrategyRuntimeEvalConfig(strategyConfigPath)
		if loadErr != nil {
			t.Fatalf("load authorization strategy config: %v", loadErr)
		}
		provider, model = config.Provider, config.Model
		configHash, err = hashStrategyRuntimeEvalConfig(config)
	default:
		t.Fatal("test live authorization requires exactly one runtime config")
	}
	if err != nil {
		t.Fatalf("hash authorization runtime config: %v", err)
	}

	now := time.Now().UTC()
	authorization, err := buildAndSignAgentTaskLiveAuthorization(agentTaskLiveAuthorization{
		AuthorizationID:       "test-" + hashAgentTaskLiveAuthorizationSubject(t.Name())[:16],
		ExpiresAt:             now.Add(time.Hour),
		Provider:              provider,
		Model:                 model,
		DatasetVersion:        datasetVersion,
		DatasetSHA256:         datasetHash,
		ExecutionConfigSHA256: configHash,
		Limits: agentTaskLiveAuthorizationLimits{
			MaxRuns: maxRuns, MaxProviderCalls: 1_000,
			MaxCapturedOutputs: maxCapturedOutputs, MaxEstimatedCostMicros: 1_000_000_000,
		},
	}, key, keyID, now)
	if err != nil {
		t.Fatalf("build test live authorization: %v", err)
	}
	root := t.TempDir()
	authorizationPath := filepath.Join(root, "authorization.json")
	if err := writeAgentTaskLiveAuthorization(authorizationPath, authorization); err != nil {
		t.Fatalf("write test live authorization: %v", err)
	}
	return []string{
		"--live-authorization", authorizationPath,
		"--live-authorization-state", filepath.Join(root, "state"),
		"--live-authorization-key-env", keyEnv,
		"--live-authorization-key-id", keyID,
	}
}
