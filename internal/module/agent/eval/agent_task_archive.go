package eval

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	AgentTaskReportArchiveReceiptSchemaVersion = "agent-task-eval-archive-receipt/v1"
	AgentTaskReportArchiveContentType          = "application/json"
	AgentTaskReportRetentionCompliance         = "COMPLIANCE"
)

type AgentTaskReportArchiveRequest struct {
	ReportSchemaVersion string
	DatasetVersion      string
	DatasetSHA256       string
	ExecutionConfigHash string
	IntegrityKeyID      string
	SignedAt            time.Time
	RetainUntil         time.Time
	ReportSHA256        string
	Payload             []byte
}

type AgentTaskReportArchiveReceipt struct {
	SchemaVersion       string    `json:"schema_version"`
	Storage             string    `json:"storage"`
	Bucket              string    `json:"bucket"`
	Key                 string    `json:"key"`
	VersionID           string    `json:"version_id"`
	ETag                string    `json:"etag"`
	ReportSHA256        string    `json:"report_sha256"`
	Length              int       `json:"length"`
	ContentType         string    `json:"content_type"`
	RetentionMode       string    `json:"retention_mode"`
	RetainUntil         time.Time `json:"retain_until"`
	ArchivedAt          time.Time `json:"archived_at"`
	Created             bool      `json:"created"`
	DatasetVersion      string    `json:"dataset_version"`
	DatasetSHA256       string    `json:"dataset_sha256"`
	ExecutionConfigHash string    `json:"execution_config_sha256,omitempty"`
	IntegrityKeyID      string    `json:"integrity_key_id"`
}

type AgentTaskReportArchive interface {
	Ensure(ctx context.Context) error
	PutImmutable(ctx context.Context, request AgentTaskReportArchiveRequest) (AgentTaskReportArchiveReceipt, error)
	Get(ctx context.Context, receipt AgentTaskReportArchiveReceipt, maxBytes int) ([]byte, error)
}

func ValidateAgentTaskReportArchiveRequest(request AgentTaskReportArchiveRequest, now time.Time) error {
	if strings.TrimSpace(request.ReportSchemaVersion) == "" || strings.TrimSpace(request.DatasetVersion) == "" {
		return errors.New("agent task report archive identity is incomplete")
	}
	if !validSHA256(request.DatasetSHA256) || !validOptionalSHA256(request.ExecutionConfigHash) || !validSHA256(request.ReportSHA256) {
		return errors.New("agent task report archive digest is invalid")
	}
	if strings.TrimSpace(request.IntegrityKeyID) == "" || request.SignedAt.IsZero() {
		return errors.New("agent task report archive signature metadata is incomplete")
	}
	if len(request.Payload) == 0 {
		return errors.New("agent task report archive payload is empty")
	}
	if len(request.Payload) > maxEvaluationJSONBytes {
		return fmt.Errorf("agent task report archive payload exceeds %d bytes", maxEvaluationJSONBytes)
	}
	actualDigest := hashBytes(request.Payload)
	if actualDigest != strings.ToLower(strings.TrimSpace(request.ReportSHA256)) {
		return errors.New("agent task report archive digest does not match payload")
	}
	if now.IsZero() {
		now = time.Now()
	}
	if !request.RetainUntil.After(now.UTC()) {
		return errors.New("agent task report retention must end in the future")
	}
	return nil
}

func ValidateAgentTaskReportArchiveReceipt(receipt AgentTaskReportArchiveReceipt) error {
	if receipt.SchemaVersion != AgentTaskReportArchiveReceiptSchemaVersion {
		return fmt.Errorf("unsupported agent task archive receipt schema %q", receipt.SchemaVersion)
	}
	if receipt.Storage != "minio" || strings.TrimSpace(receipt.Bucket) == "" || strings.TrimSpace(receipt.Key) == "" || strings.TrimSpace(receipt.VersionID) == "" {
		return errors.New("agent task archive receipt object identity is incomplete")
	}
	if !strings.HasPrefix(receipt.Key, "agent-task-eval/") || strings.Contains(receipt.Key, "..") || strings.ContainsAny(receipt.Key, "\\\r\n") {
		return errors.New("agent task archive receipt object key is invalid")
	}
	if !validSHA256(receipt.ReportSHA256) || !validSHA256(receipt.DatasetSHA256) || !validOptionalSHA256(receipt.ExecutionConfigHash) {
		return errors.New("agent task archive receipt digest is invalid")
	}
	if receipt.Length <= 0 || receipt.Length > maxEvaluationJSONBytes || receipt.ContentType != AgentTaskReportArchiveContentType {
		return errors.New("agent task archive receipt content metadata is invalid")
	}
	if receipt.RetentionMode != AgentTaskReportRetentionCompliance {
		return errors.New("agent task archive receipt is not protected by compliance retention")
	}
	if receipt.ArchivedAt.IsZero() || !receipt.RetainUntil.After(receipt.ArchivedAt) {
		return errors.New("agent task archive receipt retention metadata is invalid")
	}
	if strings.TrimSpace(receipt.DatasetVersion) == "" || strings.TrimSpace(receipt.IntegrityKeyID) == "" {
		return errors.New("agent task archive receipt report identity is incomplete")
	}
	return nil
}

func validOptionalSHA256(value string) bool {
	return strings.TrimSpace(value) == "" || validSHA256(value)
}
