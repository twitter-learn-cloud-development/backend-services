package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"twitter-clone/internal/module/agent/eval"
)

const (
	maxAgentTaskContentReviewDecisionBytes = 2 << 20
	maxAgentTaskContentReviewSignoffBytes  = 4 << 20
)

type agentTaskContentReviewSignoffCommand struct {
	CreatePath       string
	VerifyPath       string
	DecisionPath     string
	ReportPath       string
	ReviewBundlePath string
	ReportKey        []byte
	ReportKeyID      string
	ReviewKey        []byte
	ReviewKeyID      string
	SignoffKey       []byte
	SignoffKeyID     string
	Now              func() time.Time
}

type agentTaskContentReviewSignoffResult struct {
	Signoff eval.AgentTaskContentReviewSignoff
	Created bool
}

func runAgentTaskContentReviewSignoffCommand(
	command agentTaskContentReviewSignoffCommand,
) (agentTaskContentReviewSignoffResult, error) {
	command.CreatePath = strings.TrimSpace(command.CreatePath)
	command.VerifyPath = strings.TrimSpace(command.VerifyPath)
	command.DecisionPath = strings.TrimSpace(command.DecisionPath)
	command.ReportPath = strings.TrimSpace(command.ReportPath)
	command.ReviewBundlePath = strings.TrimSpace(command.ReviewBundlePath)
	command.ReportKeyID = strings.TrimSpace(command.ReportKeyID)
	command.ReviewKeyID = strings.TrimSpace(command.ReviewKeyID)
	command.SignoffKeyID = strings.TrimSpace(command.SignoffKeyID)
	if (command.CreatePath == "") == (command.VerifyPath == "") {
		return agentTaskContentReviewSignoffResult{}, errors.New("exactly one content review signoff operation is required")
	}
	if command.ReportPath == "" || command.ReviewBundlePath == "" {
		return agentTaskContentReviewSignoffResult{}, errors.New("content review signoff requires report and review bundle paths")
	}
	if len(command.ReportKey) == 0 || len(command.ReviewKey) == 0 || len(command.SignoffKey) == 0 {
		return agentTaskContentReviewSignoffResult{}, errors.New("content review signoff requires report, review and signoff keys")
	}
	if command.ReportKeyID == command.ReviewKeyID || command.ReportKeyID == command.SignoffKeyID ||
		command.ReviewKeyID == command.SignoffKeyID || bytes.Equal(command.ReportKey, command.ReviewKey) ||
		bytes.Equal(command.ReportKey, command.SignoffKey) || bytes.Equal(command.ReviewKey, command.SignoffKey) {
		return agentTaskContentReviewSignoffResult{}, errors.New("content review report, bundle and signoff keys must be independent")
	}
	output, err := loadVerifiedEvaluationOutput(command.ReportPath, command.ReportKey, command.ReportKeyID)
	if err != nil {
		return agentTaskContentReviewSignoffResult{}, fmt.Errorf("verify content review report: %w", err)
	}
	_, binding, err := loadAndOpenAgentTaskReviewBundleWithBinding(
		command.ReviewBundlePath, command.ReviewKey, command.ReviewKeyID, output,
	)
	if err != nil {
		return agentTaskContentReviewSignoffResult{}, fmt.Errorf("verify content review bundle: %w", err)
	}
	if command.CreatePath != "" {
		return createAgentTaskContentReviewSignoff(command, output, binding)
	}
	payload, err := readBoundedReviewFile(command.VerifyPath, maxAgentTaskContentReviewSignoffBytes)
	if err != nil {
		return agentTaskContentReviewSignoffResult{}, fmt.Errorf("read content review signoff: %w", err)
	}
	signoff, err := eval.DecodeAgentTaskContentReviewSignoff(payload)
	if err != nil {
		return agentTaskContentReviewSignoffResult{}, err
	}
	if err := eval.VerifyAgentTaskContentReviewSignoff(
		signoff, output, binding, command.SignoffKey, command.SignoffKeyID,
	); err != nil {
		return agentTaskContentReviewSignoffResult{}, err
	}
	return agentTaskContentReviewSignoffResult{Signoff: signoff}, nil
}

func createAgentTaskContentReviewSignoff(
	command agentTaskContentReviewSignoffCommand,
	output agentTaskEvaluationOutput,
	binding eval.AgentTaskContentReviewBundleBinding,
) (agentTaskContentReviewSignoffResult, error) {
	if command.DecisionPath == "" {
		return agentTaskContentReviewSignoffResult{}, errors.New("creating a content review signoff requires a decision path")
	}
	if err := ensureReviewPathAvailable(command.CreatePath, "content review signoff"); err != nil {
		return agentTaskContentReviewSignoffResult{}, err
	}
	for _, source := range []string{command.DecisionPath, command.ReportPath, command.ReviewBundlePath} {
		same, err := sameReviewPath(command.CreatePath, source)
		if err != nil {
			return agentTaskContentReviewSignoffResult{}, fmt.Errorf("compare content review paths: %w", err)
		}
		if same {
			return agentTaskContentReviewSignoffResult{}, errors.New("content review signoff output must differ from every input path")
		}
	}
	payload, err := readBoundedReviewFile(command.DecisionPath, maxAgentTaskContentReviewDecisionBytes)
	if err != nil {
		return agentTaskContentReviewSignoffResult{}, fmt.Errorf("read content review decision: %w", err)
	}
	decision, err := eval.DecodeAgentTaskContentReviewDecision(payload)
	if err != nil {
		return agentTaskContentReviewSignoffResult{}, err
	}
	now := time.Now
	if command.Now != nil {
		now = command.Now
	}
	signoff, err := eval.BuildAndSignAgentTaskContentReviewSignoff(
		output, binding, decision, command.SignoffKey, command.SignoffKeyID, now().UTC(),
	)
	if err != nil {
		return agentTaskContentReviewSignoffResult{}, err
	}
	if err := eval.VerifyAgentTaskContentReviewSignoff(
		signoff, output, binding, command.SignoffKey, command.SignoffKeyID,
	); err != nil {
		return agentTaskContentReviewSignoffResult{}, fmt.Errorf("verify newly created content review signoff: %w", err)
	}
	if err := writeExclusiveReviewJSON(command.CreatePath, signoff, "content review signoff"); err != nil {
		return agentTaskContentReviewSignoffResult{}, err
	}
	return agentTaskContentReviewSignoffResult{Signoff: signoff, Created: true}, nil
}

func readAgentTaskContentReviewSignoffKey(envName, keyID string) ([]byte, error) {
	envName = strings.TrimSpace(envName)
	keyID = strings.TrimSpace(keyID)
	if envName == "" || keyID == "" {
		return nil, errors.New("content review signoff key environment variable and key ID are required")
	}
	key := []byte(os.Getenv(envName))
	if len(key) < 32 {
		return nil, fmt.Errorf("content review signoff key environment variable %q must contain at least 32 bytes", envName)
	}
	return key, nil
}
