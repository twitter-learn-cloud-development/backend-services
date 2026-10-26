package service

import (
	"encoding/json"
	"fmt"
	"strings"

	agentCredential "twitter-clone/internal/module/agent/credential"
	"twitter-clone/internal/module/agent/repository"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

func (s *AgentService) sealVerifiedAgentRunCheckpoint(
	run *repository.AgentExecutionRun,
	checkpoint agentRuntime.VerifiedCheckpoint,
) (sealedAgentRunCheckpoint, error) {
	if s == nil || s.agentCheckpointCipher == nil {
		return sealedAgentRunCheckpoint{}, ErrAgentRunCheckpointUnavailable
	}
	if err := agentRuntime.ValidateVerifiedCheckpoint(checkpoint); err != nil {
		return sealedAgentRunCheckpoint{}, fmt.Errorf("validate verified agent checkpoint: %w", err)
	}
	if err := rejectSensitiveCheckpointArguments(checkpoint.Run); err != nil {
		return sealedAgentRunCheckpoint{}, err
	}
	plaintext, err := json.Marshal(checkpoint)
	if err != nil {
		return sealedAgentRunCheckpoint{}, fmt.Errorf("encode verified agent checkpoint: %w", err)
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
		return sealedAgentRunCheckpoint{}, fmt.Errorf("encrypt verified agent checkpoint: %w", err)
	}
	return sealedAgentRunCheckpoint{
		Version: checkpoint.Version, KeyID: secret.KeyID, Nonce: secret.Nonce,
		Ciphertext: secret.Ciphertext, Digest: checkpointDigest(run.ID, plaintext),
		SizeBytes: len(plaintext),
	}, nil
}

func (s *AgentService) openVerifiedAgentRunCheckpoint(
	run *repository.AgentExecutionRun,
) (agentRuntime.VerifiedCheckpoint, error) {
	if s == nil || s.agentCheckpointCipher == nil {
		return agentRuntime.VerifiedCheckpoint{}, ErrAgentRunCheckpointUnavailable
	}
	maxBytes := s.agentCheckpointMaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultAgentRunCheckpointMaxBytes
	}
	if run == nil || !run.ResumeSupported || run.CheckpointVersion != agentRuntime.VerifiedCheckpointVersion ||
		strings.TrimSpace(run.CheckpointCiphertext) == "" || run.CheckpointSizeBytes <= 0 ||
		run.CheckpointSizeBytes > maxBytes {
		return agentRuntime.VerifiedCheckpoint{}, ErrAgentRunCheckpointInvalid
	}
	plaintext, err := s.agentCheckpointCipher.Decrypt(agentCredential.EncryptedSecret{
		KeyID: run.CheckpointKeyID, Nonce: run.CheckpointNonce, Ciphertext: run.CheckpointCiphertext,
	}, agentRunCheckpointAAD(run))
	if err != nil {
		return agentRuntime.VerifiedCheckpoint{}, fmt.Errorf("%w: decrypt failed", ErrAgentRunCheckpointInvalid)
	}
	defer zeroBytes(plaintext)
	if len(plaintext) != run.CheckpointSizeBytes || !constantTimeHexDigestEqual(
		checkpointDigest(run.ID, plaintext), run.CheckpointDigest,
	) {
		return agentRuntime.VerifiedCheckpoint{}, ErrAgentRunCheckpointInvalid
	}
	var checkpoint agentRuntime.VerifiedCheckpoint
	if err := json.Unmarshal(plaintext, &checkpoint); err != nil {
		return agentRuntime.VerifiedCheckpoint{}, fmt.Errorf("decode verified agent checkpoint: %w", ErrAgentRunCheckpointInvalid)
	}
	if err := agentRuntime.ValidateVerifiedCheckpoint(checkpoint); err != nil {
		return agentRuntime.VerifiedCheckpoint{}, fmt.Errorf("%w: %v", ErrAgentRunCheckpointInvalid, err)
	}
	bound := checkpoint.Run
	if checkpoint.Version != run.CheckpointVersion || checkpoint.Version != run.VerifiedCheckpointVersion ||
		checkpoint.Revision != run.VerifiedCheckpointRevision ||
		bound.Context.RunID != run.ID || bound.Context.UserID != run.UserID ||
		bound.Context.AgentProfileID != run.AgentProfileID ||
		bound.Context.AgentProfileVersion != run.AgentProfileVersion ||
		bound.Context.PromptTemplateID != run.PromptTemplateID ||
		bound.Context.PromptTemplateVersion != run.PromptTemplateVersion || bound.Model != run.Model {
		return agentRuntime.VerifiedCheckpoint{}, ErrAgentRunCheckpointInvalid
	}
	if payload, marshalErr := json.Marshal(checkpoint); marshalErr != nil ||
		!constantTimeHexDigestEqual(agentExecutionScopedDigest(run.ID, string(payload)), run.VerifiedCheckpointDigest) {
		return agentRuntime.VerifiedCheckpoint{}, ErrAgentRunCheckpointInvalid
	}
	return checkpoint, nil
}
