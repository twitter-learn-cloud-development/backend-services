package repository

import (
	"strings"
	"testing"
	"time"
)

func TestPrepareProfileVersionForCreateOwnsLifecycleFields(t *testing.T) {
	now := time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC)
	record := &ProfileVersionRecord{
		ProfileID:    "  custom.writer  ",
		Version:      " v1 ",
		SnapshotJSON: `{"id":"custom.writer","version":"v1"}`,
		CreatedBy:    42,
		PublishedBy:  99,
		PublishedAt:  now.Add(-time.Hour),
		Revision:     88,
	}

	if err := prepareProfileVersionForCreate(record, now); err != nil {
		t.Fatalf("prepareProfileVersionForCreate() error = %v", err)
	}
	if record.ProfileID != "custom.writer" || record.Version != "v1" {
		t.Fatalf("identity = %q@%q", record.ProfileID, record.Version)
	}
	if record.Status != ProfileVersionStatusDraft || record.Revision != 1 {
		t.Fatalf("lifecycle = %q revision %d", record.Status, record.Revision)
	}
	if record.SnapshotSchema != ProfileSnapshotSchemaV1 {
		t.Fatalf("snapshot schema = %q", record.SnapshotSchema)
	}
	if record.SnapshotHash != ProfileSnapshotHash(record.SnapshotJSON) {
		t.Fatalf("snapshot hash = %q", record.SnapshotHash)
	}
	if record.PublishedBy != 0 || !record.PublishedAt.IsZero() {
		t.Fatalf("publish metadata must be cleared: %+v", record)
	}
	if record.ID.IsZero() || !record.CreatedAt.Equal(now) || !record.UpdatedAt.Equal(now) {
		t.Fatalf("server-owned metadata was not populated: %+v", record)
	}
}

func TestPrepareProfileVersionForCreateRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name   string
		record *ProfileVersionRecord
		want   string
	}{
		{name: "nil", record: nil, want: "required"},
		{name: "identity", record: &ProfileVersionRecord{SnapshotJSON: `{}`, CreatedBy: 1}, want: "identity"},
		{name: "creator", record: &ProfileVersionRecord{ProfileID: "p", Version: "v1", SnapshotJSON: `{}`}, want: "creator"},
		{name: "json", record: &ProfileVersionRecord{ProfileID: "p", Version: "v1", SnapshotJSON: `{`, CreatedBy: 1}, want: "valid JSON"},
		{name: "published", record: &ProfileVersionRecord{ProfileID: "p", Version: "v1", Status: ProfileVersionStatusPublished, SnapshotJSON: `{}`, CreatedBy: 1}, want: "start as draft"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := prepareProfileVersionForCreate(test.record, time.Now())
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestProfileVersionRecordVerifySnapshot(t *testing.T) {
	record := &ProfileVersionRecord{SnapshotJSON: `{"id":"p"}`}
	record.SnapshotHash = ProfileSnapshotHash(record.SnapshotJSON)
	if err := record.VerifySnapshot(); err != nil {
		t.Fatalf("VerifySnapshot() error = %v", err)
	}
	record.SnapshotJSON = `{"id":"tampered"}`
	if err := record.VerifySnapshot(); err == nil || !strings.Contains(err.Error(), "hash mismatch") {
		t.Fatalf("VerifySnapshot() error = %v", err)
	}
}

func TestPrepareProfileReleaseAppliesOptimisticRevision(t *testing.T) {
	now := time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC)
	release := &ProfileReleaseRecord{
		ProfileID:        " custom.writer ",
		StableVersion:    " v1 ",
		CandidateVersion: " v2 ",
		Salt:             " rollout ",
		UpdatedBy:        42,
	}
	if err := prepareProfileRelease(release, 0, now); err != nil {
		t.Fatalf("prepareProfileRelease(create) error = %v", err)
	}
	if release.Revision != 1 || release.CreatedBy != 42 || release.ID.IsZero() {
		t.Fatalf("created release = %+v", release)
	}
	if release.ProfileID != "custom.writer" || release.StableVersion != "v1" || release.CandidateVersion != "v2" || release.Salt != "rollout" {
		t.Fatalf("trimmed release = %+v", release)
	}

	updated := &ProfileReleaseRecord{
		ProfileID:        "custom.writer",
		StableVersion:    "v1",
		CandidateVersion: "v2",
		UpdatedBy:        7,
	}
	if err := prepareProfileRelease(updated, 5, now); err != nil {
		t.Fatalf("prepareProfileRelease(update) error = %v", err)
	}
	if updated.Revision != 5 || !updated.UpdatedAt.Equal(now) {
		t.Fatalf("updated release = %+v", updated)
	}
}

func TestPrepareProfileAuditEventOwnsMetadataAndRejectsSensitiveShape(t *testing.T) {
	now := time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC)
	event := &ProfileAuditEvent{
		OperationID: " operation-1 ",
		Action:      ProfileAuditActionCreateDraft,
		Outcome:     ProfileAuditOutcomeRequested,
		ProfileID:   " custom.writer ",
		Version:     " v1 ",
		ActorUserID: 42,
		ErrorCode:   " invalid_profile ",
	}
	if err := prepareProfileAuditEvent(event, now); err != nil {
		t.Fatalf("prepareProfileAuditEvent() error = %v", err)
	}
	if event.OperationID != "operation-1" || event.ProfileID != "custom.writer" || event.Version != "v1" {
		t.Fatalf("trimmed audit event = %+v", event)
	}
	if event.ID.IsZero() || !event.CreatedAt.Equal(now) {
		t.Fatalf("server-owned audit metadata = %+v", event)
	}

	invalid := *event
	invalid.ErrorCode = strings.Repeat("x", 65)
	if err := prepareProfileAuditEvent(&invalid, now); err == nil || !strings.Contains(err.Error(), "too long") {
		t.Fatalf("prepareProfileAuditEvent(long error code) error = %v", err)
	}
}
