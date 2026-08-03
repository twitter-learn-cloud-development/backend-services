package acceptance

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

const (
	ReportSchemaVersion = "agent-mcp-acceptance-report/v1"
	IntegrityAlgorithm  = "hmac-sha256"

	StatusPassed  = "passed"
	StatusPartial = "partial"
	StatusFailed  = "failed"

	StepPassed  = "passed"
	StepSkipped = "skipped"
	StepFailed  = "failed"

	maxReportBytes = 4 * 1024 * 1024
)

type Report struct {
	SchemaVersion       string                 `json:"schema_version"`
	Target              string                 `json:"target"`
	Environment         string                 `json:"environment"`
	Status              string                 `json:"status"`
	StartedAt           time.Time              `json:"started_at"`
	FinishedAt          time.Time              `json:"finished_at"`
	ConfigSHA256        string                 `json:"config_sha256"`
	EndpointSHA256      string                 `json:"endpoint_sha256"`
	Transport           string                 `json:"transport"`
	CredentialSource    string                 `json:"credential_source"`
	ToolCatalogSHA256   string                 `json:"tool_catalog_sha256,omitempty"`
	DiscoveredToolCount int                    `json:"discovered_tool_count"`
	ToolContracts       []ToolContractEvidence `json:"tool_contracts,omitempty"`
	Steps               []StepResult           `json:"steps"`
	Summary             StepSummary            `json:"summary"`
	Limitations         []string               `json:"limitations,omitempty"`
	Integrity           *Integrity             `json:"integrity,omitempty"`
}

type ToolContractEvidence struct {
	Name                   string `json:"name"`
	SchemaSHA256           string `json:"schema_sha256"`
	DeclaredReadOnly       bool   `json:"declared_read_only"`
	DeclaredIdempotent     bool   `json:"declared_idempotent"`
	HasIdempotencyArgument bool   `json:"has_idempotency_argument"`
}

type StepResult struct {
	Name           string `json:"name"`
	Status         string `json:"status"`
	DurationMillis int64  `json:"duration_ms"`
	ErrorCode      string `json:"error_code,omitempty"`
	EvidenceSHA256 string `json:"evidence_sha256,omitempty"`
	EvidenceLevel  string `json:"evidence_level,omitempty"`
}

type StepSummary struct {
	Passed  int `json:"passed"`
	Skipped int `json:"skipped"`
	Failed  int `json:"failed"`
}

type Integrity struct {
	Algorithm     string    `json:"algorithm"`
	KeyID         string    `json:"key_id"`
	SignedAt      time.Time `json:"signed_at"`
	PayloadSHA256 string    `json:"payload_sha256"`
	Signature     string    `json:"signature"`
}

func SignReport(report *Report, key []byte, keyID string, signedAt time.Time) error {
	if report == nil {
		return errors.New("MCP acceptance report is nil")
	}
	keyID = strings.TrimSpace(keyID)
	if len(key) < 32 {
		return errors.New("MCP acceptance report signing key must contain at least 32 bytes")
	}
	if keyID == "" {
		return errors.New("MCP acceptance report signing key ID is required")
	}
	if signedAt.IsZero() {
		return errors.New("MCP acceptance report signing time is required")
	}
	payload, err := unsignedReportPayload(*report)
	if err != nil {
		return err
	}
	integrity := &Integrity{
		Algorithm: IntegrityAlgorithm, KeyID: keyID, SignedAt: signedAt.UTC(),
		PayloadSHA256: hashBytes(payload),
	}
	integrity.Signature = signIntegrity(*integrity, key)
	report.Integrity = integrity
	return nil
}

func VerifyReport(report Report, key []byte, trustedKeyID string) error {
	if report.SchemaVersion != ReportSchemaVersion {
		return fmt.Errorf("unsupported MCP acceptance report schema version %q", report.SchemaVersion)
	}
	if report.Integrity == nil {
		return errors.New("MCP acceptance report is unsigned")
	}
	integrity := *report.Integrity
	if trustedKeyID = strings.TrimSpace(trustedKeyID); trustedKeyID != "" && integrity.KeyID != trustedKeyID {
		return fmt.Errorf("MCP acceptance report key ID %q does not match trusted key ID %q", integrity.KeyID, trustedKeyID)
	}
	if integrity.Algorithm != IntegrityAlgorithm {
		return fmt.Errorf("unsupported MCP acceptance report integrity algorithm %q", integrity.Algorithm)
	}
	if len(key) < 32 {
		return errors.New("MCP acceptance report verification key must contain at least 32 bytes")
	}
	if strings.TrimSpace(integrity.KeyID) == "" || integrity.SignedAt.IsZero() ||
		!validSHA256(integrity.PayloadSHA256) || !validSHA256(integrity.Signature) {
		return errors.New("MCP acceptance report integrity metadata is invalid")
	}
	payload, err := unsignedReportPayload(report)
	if err != nil {
		return err
	}
	if !hmac.Equal([]byte(hashBytes(payload)), []byte(strings.ToLower(integrity.PayloadSHA256))) {
		return errors.New("MCP acceptance report payload hash mismatch")
	}
	expected, _ := hex.DecodeString(signIntegrity(integrity, key))
	actual, _ := hex.DecodeString(integrity.Signature)
	if !hmac.Equal(expected, actual) {
		return errors.New("MCP acceptance report signature mismatch")
	}
	return nil
}

func DecodeReport(reader io.Reader) (Report, error) {
	if reader == nil {
		return Report{}, errors.New("MCP acceptance report reader is nil")
	}
	payload, err := io.ReadAll(io.LimitReader(reader, maxReportBytes+1))
	if err != nil {
		return Report{}, fmt.Errorf("read MCP acceptance report: %w", err)
	}
	if len(payload) > maxReportBytes {
		return Report{}, errors.New("MCP acceptance report is too large")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var report Report
	if err := decoder.Decode(&report); err != nil {
		return Report{}, fmt.Errorf("decode MCP acceptance report: %w", err)
	}
	if err := ensureEOF(decoder, "MCP acceptance report"); err != nil {
		return Report{}, err
	}
	return report, nil
}

func MarshalReport(report Report) ([]byte, error) {
	payload, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode MCP acceptance report: %w", err)
	}
	return append(payload, '\n'), nil
}

func unsignedReportPayload(report Report) ([]byte, error) {
	report.Integrity = nil
	payload, err := json.Marshal(report)
	if err != nil {
		return nil, fmt.Errorf("encode unsigned MCP acceptance report: %w", err)
	}
	return payload, nil
}

func signIntegrity(integrity Integrity, key []byte) string {
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

func validSHA256(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
