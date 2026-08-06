package service

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	agentCredential "twitter-clone/internal/module/agent/credential"
	"twitter-clone/internal/module/agent/repository"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

const (
	DefaultAgentRunCheckpointMaxBytes  = 256 * 1024
	DefaultAgentRunResumeLeaseDuration = 5 * time.Minute
	MaxAgentRunHumanResponseBytes      = 64 * 1024
)

var (
	ErrAgentRunCheckpointUnavailable = errors.New("agent run checkpoint encryption is unavailable")
	ErrAgentRunCheckpointInvalid     = errors.New("agent run checkpoint is invalid")
)

type sealedAgentRunCheckpoint struct {
	Version    string
	KeyID      string
	Nonce      string
	Ciphertext string
	Digest     string
	SizeBytes  int
}

func (s *AgentService) sealAgentRunCheckpoint(
	run *repository.AgentExecutionRun,
	request agentRuntime.RunRequest,
	result agentRuntime.RunResult,
) (sealedAgentRunCheckpoint, error) {
	if s == nil || s.agentCheckpointCipher == nil {
		return sealedAgentRunCheckpoint{}, ErrAgentRunCheckpointUnavailable
	}
	checkpoint, err := agentRuntime.NewRunCheckpoint(request, result)
	if err != nil {
		return sealedAgentRunCheckpoint{}, fmt.Errorf("build agent run checkpoint: %w", err)
	}
	if err := rejectSensitiveCheckpointArguments(checkpoint); err != nil {
		return sealedAgentRunCheckpoint{}, err
	}
	plaintext, err := json.Marshal(checkpoint)
	if err != nil {
		return sealedAgentRunCheckpoint{}, fmt.Errorf("encode agent run checkpoint: %w", err)
	}
	defer zeroBytes(plaintext)
	maxBytes := s.agentCheckpointMaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultAgentRunCheckpointMaxBytes
	}
	if len(plaintext) == 0 || len(plaintext) > maxBytes {
		return sealedAgentRunCheckpoint{}, fmt.Errorf(
			"%w: encoded size %d exceeds limit %d",
			ErrAgentRunCheckpointInvalid,
			len(plaintext),
			maxBytes,
		)
	}
	secret, err := s.agentCheckpointCipher.Encrypt(plaintext, agentRunCheckpointAAD(run))
	if err != nil {
		return sealedAgentRunCheckpoint{}, fmt.Errorf("encrypt agent run checkpoint: %w", err)
	}
	return sealedAgentRunCheckpoint{
		Version:    checkpoint.Version,
		KeyID:      secret.KeyID,
		Nonce:      secret.Nonce,
		Ciphertext: secret.Ciphertext,
		Digest:     checkpointDigest(run.ID, plaintext),
		SizeBytes:  len(plaintext),
	}, nil
}

func (s *AgentService) openAgentRunCheckpoint(
	run *repository.AgentExecutionRun,
) (agentRuntime.RunCheckpoint, error) {
	if s == nil || s.agentCheckpointCipher == nil {
		return agentRuntime.RunCheckpoint{}, ErrAgentRunCheckpointUnavailable
	}
	maxBytes := s.agentCheckpointMaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultAgentRunCheckpointMaxBytes
	}
	if run == nil || !run.ResumeSupported || strings.TrimSpace(run.CheckpointCiphertext) == "" ||
		run.CheckpointSizeBytes <= 0 || run.CheckpointSizeBytes > maxBytes {
		return agentRuntime.RunCheckpoint{}, ErrAgentRunCheckpointInvalid
	}
	plaintext, err := s.agentCheckpointCipher.Decrypt(agentCredential.EncryptedSecret{
		KeyID: run.CheckpointKeyID, Nonce: run.CheckpointNonce, Ciphertext: run.CheckpointCiphertext,
	}, agentRunCheckpointAAD(run))
	if err != nil {
		return agentRuntime.RunCheckpoint{}, fmt.Errorf("%w: decrypt failed", ErrAgentRunCheckpointInvalid)
	}
	defer zeroBytes(plaintext)
	if len(plaintext) != run.CheckpointSizeBytes || !constantTimeHexDigestEqual(
		checkpointDigest(run.ID, plaintext),
		run.CheckpointDigest,
	) {
		return agentRuntime.RunCheckpoint{}, ErrAgentRunCheckpointInvalid
	}
	var checkpoint agentRuntime.RunCheckpoint
	if err := json.Unmarshal(plaintext, &checkpoint); err != nil {
		return agentRuntime.RunCheckpoint{}, fmt.Errorf("decode agent run checkpoint: %w", ErrAgentRunCheckpointInvalid)
	}
	if checkpoint.Version != run.CheckpointVersion {
		return agentRuntime.RunCheckpoint{}, ErrAgentRunCheckpointInvalid
	}
	if err := agentRuntime.ValidateRunCheckpoint(checkpoint); err != nil {
		return agentRuntime.RunCheckpoint{}, fmt.Errorf("%w: %v", ErrAgentRunCheckpointInvalid, err)
	}
	if checkpoint.Context.RunID != run.ID || checkpoint.Context.UserID != run.UserID ||
		checkpoint.Context.AgentProfileID != run.AgentProfileID ||
		checkpoint.Context.AgentProfileVersion != run.AgentProfileVersion ||
		checkpoint.Context.PromptTemplateID != run.PromptTemplateID ||
		checkpoint.Context.PromptTemplateVersion != run.PromptTemplateVersion ||
		checkpoint.Model != run.Model {
		return agentRuntime.RunCheckpoint{}, ErrAgentRunCheckpointInvalid
	}
	return checkpoint, nil
}

func agentRunCheckpointAAD(run *repository.AgentExecutionRun) []byte {
	if run == nil {
		return nil
	}
	return []byte(strings.Join([]string{
		"agent-run-checkpoint",
		strconv.FormatUint(run.UserID, 10),
		strings.TrimSpace(run.ID),
	}, "\x00"))
}

func checkpointDigest(runID string, plaintext []byte) string {
	digest := sha256.New()
	_, _ = digest.Write([]byte(strings.TrimSpace(runID)))
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write(plaintext)
	return hex.EncodeToString(digest.Sum(nil))
}

func constantTimeHexDigestEqual(left, right string) bool {
	leftBytes, leftErr := hex.DecodeString(strings.TrimSpace(left))
	rightBytes, rightErr := hex.DecodeString(strings.TrimSpace(right))
	if leftErr != nil || rightErr != nil || len(leftBytes) != sha256.Size || len(rightBytes) != sha256.Size {
		return false
	}
	return subtle.ConstantTimeCompare(leftBytes, rightBytes) == 1
}

func rejectSensitiveCheckpointArguments(checkpoint agentRuntime.RunCheckpoint) error {
	for _, message := range checkpoint.Messages {
		for _, action := range message.Actions {
			if rawJSONContainsSensitiveKey(action.Arguments) {
				return errors.New("agent run checkpoint action arguments contain a sensitive key")
			}
		}
	}
	for _, step := range checkpoint.Steps {
		for _, action := range step.Actions {
			if rawJSONContainsSensitiveKey(action.Arguments) {
				return errors.New("agent run checkpoint step arguments contain a sensitive key")
			}
		}
	}
	return nil
}

func rawJSONContainsSensitiveKey(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return true
	}
	return containsSensitiveJSONKey(value)
}

func containsSensitiveJSONKey(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			normalized := strings.NewReplacer("-", "_", " ", "_").Replace(strings.ToLower(strings.TrimSpace(key)))
			switch normalized {
			case "api_key", "apikey", "authorization", "bearer_token", "access_token",
				"refresh_token", "password", "secret", "credential", "credentials":
				return true
			}
			if containsSensitiveJSONKey(child) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if containsSensitiveJSONKey(child) {
				return true
			}
		}
	}
	return false
}

func zeroBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

func runtimeUserVisibleResponse(result agentRuntime.RunResult) (string, error) {
	switch result.Status {
	case agentRuntime.RunStatusCompleted:
		if response := strings.TrimSpace(result.FinalAnswer); response != "" {
			return response, nil
		}
	case agentRuntime.RunStatusAwaitingHuman:
		if result.PendingToolContinuation != nil &&
			result.PendingResumeKind == agentRuntime.ResumeKindHumanResponse {
			if prompt := strings.TrimSpace(result.PendingToolContinuation.Prompt); prompt != "" {
				return prompt, nil
			}
		}
		if result.PendingAction != nil && result.PendingAction.Type == agentRuntime.ActionAskHuman {
			if question := strings.TrimSpace(result.PendingAction.Content); question != "" {
				return question, nil
			}
		}
	case agentRuntime.RunStatusApprovalRequired:
		if result.PendingAction != nil && result.PendingAction.Type == agentRuntime.ActionToolCall {
			if name := strings.TrimSpace(result.PendingAction.Name); name != "" {
				return fmt.Sprintf("工具 %s 需要你的批准，批准后将从当前步骤继续。", name), nil
			}
		}
	}
	return "", fmt.Errorf("agent runtime ended with status %s without a user-visible response", result.Status)
}
