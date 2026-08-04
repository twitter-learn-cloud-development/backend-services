package objectstore

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"twitter-clone/internal/module/agent/eval"
)

type fakeAgentTaskReportBackend struct {
	exists       bool
	versioned    bool
	locked       bool
	hasPolicy    bool
	makeCalls    int
	payload      []byte
	key          string
	versionID    string
	etag         string
	retention    string
	retainUntil  time.Time
	modifiedAt   time.Time
	tamperOnRead bool
}

func (f *fakeAgentTaskReportBackend) BucketExists(context.Context, string) (bool, error) {
	return f.exists, nil
}

func (f *fakeAgentTaskReportBackend) MakeLockedBucket(context.Context, string) error {
	f.makeCalls++
	f.exists = true
	f.versioned = true
	f.locked = true
	return nil
}

func (f *fakeAgentTaskReportBackend) BucketVersioningEnabled(context.Context, string) (bool, error) {
	return f.versioned, nil
}

func (f *fakeAgentTaskReportBackend) BucketObjectLockEnabled(context.Context, string) (bool, error) {
	return f.locked, nil
}

func (f *fakeAgentTaskReportBackend) BucketHasPolicy(context.Context, string) (bool, error) {
	return f.hasPolicy, nil
}

func (f *fakeAgentTaskReportBackend) PutImmutable(_ context.Context, _, key string, payload []byte, request eval.AgentTaskReportArchiveRequest) (agentTaskReportObjectInfo, error) {
	if f.payload != nil {
		return agentTaskReportObjectInfo{}, errAgentTaskReportObjectExists
	}
	f.payload = append([]byte(nil), payload...)
	f.key = key
	f.versionID = "version-1"
	f.etag = "etag"
	f.retention = eval.AgentTaskReportRetentionCompliance
	f.retainUntil = request.RetainUntil.UTC()
	return f.objectInfo(), nil
}

func (f *fakeAgentTaskReportBackend) Stat(context.Context, string, string, string) (agentTaskReportObjectInfo, error) {
	if f.payload == nil {
		return agentTaskReportObjectInfo{}, errors.New("missing object")
	}
	return f.objectInfo(), nil
}

func (f *fakeAgentTaskReportBackend) Get(context.Context, string, string, string, int) ([]byte, error) {
	payload := append([]byte(nil), f.payload...)
	if f.tamperOnRead && len(payload) > 0 {
		payload[0] ^= 1
	}
	return payload, nil
}

func (f *fakeAgentTaskReportBackend) GetRetention(context.Context, string, string, string) (string, time.Time, error) {
	return f.retention, f.retainUntil, nil
}

func (f *fakeAgentTaskReportBackend) objectInfo() agentTaskReportObjectInfo {
	etag := f.etag
	if etag == "" {
		etag = "etag"
	}
	return agentTaskReportObjectInfo{
		VersionID: f.versionID, ETag: etag, Size: int64(len(f.payload)),
		ContentType: eval.AgentTaskReportArchiveContentType, ModifiedAt: f.modifiedAt,
	}
}

func TestMinIOAgentTaskReportArchiveEnsureCreatesLockedBucket(t *testing.T) {
	backend := &fakeAgentTaskReportBackend{}
	archive := newMinIOAgentTaskReportArchive(backend, "agent-task-eval")
	if err := archive.Ensure(context.Background()); err != nil {
		t.Fatalf("ensure archive: %v", err)
	}
	if backend.makeCalls != 1 || !backend.versioned || !backend.locked {
		t.Fatalf("expected locked versioned bucket creation: %#v", backend)
	}
}

func TestMinIOAgentTaskReportArchiveEnsureFailsClosed(t *testing.T) {
	tests := []struct {
		name      string
		backend   *fakeAgentTaskReportBackend
		wantError string
	}{
		{name: "versioning disabled", backend: &fakeAgentTaskReportBackend{exists: true, locked: true}, wantError: "versioning"},
		{name: "object lock disabled", backend: &fakeAgentTaskReportBackend{exists: true, versioned: true}, wantError: "object lock"},
		{name: "bucket policy present", backend: &fakeAgentTaskReportBackend{exists: true, versioned: true, locked: true, hasPolicy: true}, wantError: "policy"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			archive := newMinIOAgentTaskReportArchive(test.backend, "agent-task-eval")
			err := archive.Ensure(context.Background())
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("expected %q error, got %v", test.wantError, err)
			}
		})
	}
}

func TestMinIOAgentTaskReportArchivePutIsImmutableAndVerified(t *testing.T) {
	now := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)
	backend := &fakeAgentTaskReportBackend{
		exists: true, versioned: true, locked: true, modifiedAt: now,
	}
	archive := newMinIOAgentTaskReportArchive(backend, "agent-task-eval")
	archive.now = func() time.Time { return now }
	payload := []byte(`{"schema_version":"agent-task-eval-report/v2"}`)
	request := eval.AgentTaskReportArchiveRequest{
		ReportSchemaVersion: "agent-task-eval-report/v2",
		DatasetVersion:      "cases-v1",
		DatasetSHA256:       strings.Repeat("a", 64),
		ExecutionConfigHash: strings.Repeat("b", 64),
		IntegrityKeyID:      "eval-key-v1",
		SignedAt:            now.Add(-time.Minute),
		RetainUntil:         now.Add(365 * 24 * time.Hour),
		ReportSHA256:        digestBytes(payload),
		Payload:             payload,
	}
	receipt, err := archive.PutImmutable(context.Background(), request)
	if err != nil {
		t.Fatalf("put immutable report: %v", err)
	}
	if !receipt.Created || receipt.VersionID != "version-1" || receipt.RetentionMode != eval.AgentTaskReportRetentionCompliance {
		t.Fatalf("unexpected receipt: %#v", receipt)
	}
	stored, err := archive.Get(context.Background(), receipt, maxAgentTaskReportArchiveBytes)
	if err != nil || !bytes.Equal(stored, payload) {
		t.Fatalf("read archived report: payload=%q err=%v", stored, err)
	}
	backend.retainUntil = receipt.RetainUntil.Add(24 * time.Hour)
	if _, err := archive.Get(context.Background(), receipt, maxAgentTaskReportArchiveBytes); err != nil {
		t.Fatalf("extended compliance retention should remain valid: %v", err)
	}
	backend.retainUntil = receipt.RetainUntil

	repeated, err := archive.PutImmutable(context.Background(), request)
	if err != nil {
		t.Fatalf("idempotent immutable put: %v", err)
	}
	if repeated.Created || repeated.VersionID != receipt.VersionID {
		t.Fatalf("expected existing immutable version, got %#v", repeated)
	}
}

func TestMinIOAgentTaskReportArchiveRejectsTamperedReadback(t *testing.T) {
	now := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)
	payload := []byte(`{"report":"signed"}`)
	backend := &fakeAgentTaskReportBackend{
		exists: true, versioned: true, locked: true, modifiedAt: now, tamperOnRead: true,
	}
	archive := newMinIOAgentTaskReportArchive(backend, "agent-task-eval")
	archive.now = func() time.Time { return now }
	_, err := archive.PutImmutable(context.Background(), eval.AgentTaskReportArchiveRequest{
		ReportSchemaVersion: "agent-task-eval-report/v2", DatasetVersion: "cases-v1",
		DatasetSHA256: strings.Repeat("a", 64), ExecutionConfigHash: strings.Repeat("b", 64),
		IntegrityKeyID: "eval-key-v1", SignedAt: now.Add(-time.Minute), RetainUntil: now.Add(24 * time.Hour),
		ReportSHA256: digestBytes(payload), Payload: payload,
	})
	if err == nil || !strings.Contains(err.Error(), "read-after-write") {
		t.Fatalf("expected tampered readback rejection, got %v", err)
	}
}

func TestMinIOAgentTaskReportArchiveGetRejectsReceiptMetadataMismatch(t *testing.T) {
	now := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)
	payload := []byte(`{"report":"signed"}`)
	backend := &fakeAgentTaskReportBackend{exists: true, versioned: true, locked: true, modifiedAt: now}
	archive := newMinIOAgentTaskReportArchive(backend, "agent-task-eval")
	archive.now = func() time.Time { return now }
	receipt, err := archive.PutImmutable(context.Background(), eval.AgentTaskReportArchiveRequest{
		ReportSchemaVersion: "agent-task-eval-report/v2", DatasetVersion: "cases-v1",
		DatasetSHA256: strings.Repeat("a", 64), ExecutionConfigHash: strings.Repeat("b", 64),
		IntegrityKeyID: "eval-key-v1", SignedAt: now.Add(-time.Minute), RetainUntil: now.Add(24 * time.Hour),
		ReportSHA256: digestBytes(payload), Payload: payload,
	})
	if err != nil {
		t.Fatalf("put immutable report: %v", err)
	}
	backend.etag = "different-etag"
	_, err = archive.Get(context.Background(), receipt, maxAgentTaskReportArchiveBytes)
	if err == nil || !strings.Contains(err.Error(), "metadata") {
		t.Fatalf("expected metadata mismatch rejection, got %v", err)
	}
}

func TestMinIOAgentTaskReportArchiveRejectsExistingShorterRetention(t *testing.T) {
	now := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)
	payload := []byte(`{"report":"signed"}`)
	backend := &fakeAgentTaskReportBackend{
		exists: true, versioned: true, locked: true, modifiedAt: now,
		payload: append([]byte(nil), payload...), versionID: "version-1", etag: "etag",
		retention: eval.AgentTaskReportRetentionCompliance, retainUntil: now.Add(2 * time.Hour),
	}
	archive := newMinIOAgentTaskReportArchive(backend, "agent-task-eval")
	archive.now = func() time.Time { return now }
	_, err := archive.PutImmutable(context.Background(), eval.AgentTaskReportArchiveRequest{
		ReportSchemaVersion: "agent-task-eval-report/v2", DatasetVersion: "cases-v1",
		DatasetSHA256: strings.Repeat("a", 64), ExecutionConfigHash: strings.Repeat("b", 64),
		IntegrityKeyID: "eval-key-v1", SignedAt: now.Add(-time.Minute), RetainUntil: now.Add(24 * time.Hour),
		ReportSHA256: digestBytes(payload), Payload: payload,
	})
	if err == nil || !strings.Contains(err.Error(), "shorter than requested") {
		t.Fatalf("expected shorter retention rejection, got %v", err)
	}
}

func TestMinIOAgentTaskReportArchiveRejectsGovernanceRetention(t *testing.T) {
	now := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)
	payload := []byte(`{"report":"signed"}`)
	backend := &fakeAgentTaskReportBackend{exists: true, versioned: true, locked: true, modifiedAt: now}
	archive := newMinIOAgentTaskReportArchive(backend, "agent-task-eval")
	archive.now = func() time.Time { return now }
	backend.payload = append([]byte(nil), payload...)
	backend.versionID = "version-1"
	backend.retention = "GOVERNANCE"
	backend.retainUntil = now.Add(24 * time.Hour)
	_, err := archive.PutImmutable(context.Background(), eval.AgentTaskReportArchiveRequest{
		ReportSchemaVersion: "agent-task-eval-report/v2", DatasetVersion: "cases-v1",
		DatasetSHA256: strings.Repeat("a", 64), ExecutionConfigHash: strings.Repeat("b", 64),
		IntegrityKeyID: "eval-key-v1", SignedAt: now.Add(-time.Minute), RetainUntil: now.Add(24 * time.Hour),
		ReportSHA256: digestBytes(payload), Payload: payload,
	})
	if err == nil || !strings.Contains(err.Error(), "compliance") {
		t.Fatalf("expected compliance retention rejection, got %v", err)
	}
}
