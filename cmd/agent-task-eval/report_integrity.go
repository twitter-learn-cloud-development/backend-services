package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"twitter-clone/internal/module/agent/eval"
)

func readIntegrityKey(envName, keyID string) ([]byte, error) {
	envName = strings.TrimSpace(envName)
	keyID = strings.TrimSpace(keyID)
	if envName == "" {
		return nil, errors.New("integrity key environment variable name is required")
	}
	key := []byte(os.Getenv(envName))
	if len(key) == 0 {
		if keyID != "" {
			return nil, fmt.Errorf("integrity key environment variable %q is empty", envName)
		}
		return nil, nil
	}
	if keyID == "" {
		return nil, errors.New("integrity key ID is required when a signing key is configured")
	}
	if len(key) < 32 {
		return nil, fmt.Errorf("integrity key environment variable %q must contain at least 32 bytes", envName)
	}
	return key, nil
}

func signEvaluationOutput(output *agentTaskEvaluationOutput, key []byte, keyID string, signedAt time.Time) error {
	return eval.SignAgentTaskEvaluationOutput(output, key, keyID, signedAt)
}

func verifyEvaluationOutput(output agentTaskEvaluationOutput, key []byte, trustedKeyID string) error {
	return eval.VerifyAgentTaskEvaluationOutput(output, key, trustedKeyID)
}

func loadVerifiedEvaluationOutput(path string, key []byte, trustedKeyID string) (agentTaskEvaluationOutput, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return agentTaskEvaluationOutput{}, err
	}
	return decodeVerifiedEvaluationOutput(payload, key, trustedKeyID)
}

func decodeVerifiedEvaluationOutput(payload, key []byte, trustedKeyID string) (agentTaskEvaluationOutput, error) {
	return eval.DecodeAndVerifyAgentTaskEvaluationOutput(payload, key, trustedKeyID)
}
