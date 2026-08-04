package eval

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const AgentTaskIntegrityAlgorithm = "hmac-sha256"

type AgentTaskReportIntegrity struct {
	Algorithm     string    `json:"algorithm"`
	KeyID         string    `json:"key_id"`
	SignedAt      time.Time `json:"signed_at"`
	PayloadSHA256 string    `json:"payload_sha256"`
	Signature     string    `json:"signature"`
}

func HashAgentTaskDataset(dataset []AgentTaskCase) (string, error) {
	return HashCanonicalJSON(dataset)
}

func HashCanonicalJSON(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode canonical JSON: %w", err)
	}
	return hashBytes(payload), nil
}

func SignAgentTaskPayload(payload, key []byte, keyID string, signedAt time.Time) (AgentTaskReportIntegrity, error) {
	keyID = strings.TrimSpace(keyID)
	if len(key) < 32 {
		return AgentTaskReportIntegrity{}, errors.New("agent task report signing key must contain at least 32 bytes")
	}
	if keyID == "" {
		return AgentTaskReportIntegrity{}, errors.New("agent task report signing key ID is required")
	}
	if signedAt.IsZero() {
		return AgentTaskReportIntegrity{}, errors.New("agent task report signing time is required")
	}
	integrity := AgentTaskReportIntegrity{
		Algorithm:     AgentTaskIntegrityAlgorithm,
		KeyID:         keyID,
		SignedAt:      signedAt.UTC(),
		PayloadSHA256: hashBytes(payload),
	}
	integrity.Signature = signAgentTaskDigest(integrity, key)
	return integrity, nil
}

func VerifyAgentTaskPayload(payload, key []byte, integrity AgentTaskReportIntegrity) error {
	if integrity.Algorithm != AgentTaskIntegrityAlgorithm {
		return fmt.Errorf("unsupported agent task report integrity algorithm %q", integrity.Algorithm)
	}
	if len(key) < 32 {
		return errors.New("agent task report verification key must contain at least 32 bytes")
	}
	if strings.TrimSpace(integrity.KeyID) == "" || integrity.SignedAt.IsZero() {
		return errors.New("agent task report integrity metadata is incomplete")
	}
	if !validSHA256(integrity.PayloadSHA256) || !validSHA256(integrity.Signature) {
		return errors.New("agent task report integrity digest is invalid")
	}
	actualPayloadHash := hashBytes(payload)
	if !hmac.Equal([]byte(actualPayloadHash), []byte(strings.ToLower(integrity.PayloadSHA256))) {
		return errors.New("agent task report payload hash mismatch")
	}
	expectedSignature := signAgentTaskDigest(integrity, key)
	actualSignature, _ := hex.DecodeString(integrity.Signature)
	expectedBytes, _ := hex.DecodeString(expectedSignature)
	if !hmac.Equal(actualSignature, expectedBytes) {
		return errors.New("agent task report signature mismatch")
	}
	return nil
}

func signAgentTaskDigest(integrity AgentTaskReportIntegrity, key []byte) string {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(integrity.Algorithm))
	_, _ = mac.Write([]byte{'\n'})
	_, _ = mac.Write([]byte(strings.TrimSpace(integrity.KeyID)))
	_, _ = mac.Write([]byte{'\n'})
	_, _ = mac.Write([]byte(integrity.SignedAt.UTC().Format(time.RFC3339Nano)))
	_, _ = mac.Write([]byte{'\n'})
	_, _ = mac.Write([]byte(strings.ToLower(integrity.PayloadSHA256)))
	return hex.EncodeToString(mac.Sum(nil))
}

func hashBytes(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func validSHA256(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
