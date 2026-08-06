package repository

import (
	"testing"
	"time"
)

func TestPrepareProfileRoleBindingOwnsRevisionAndTimestamps(t *testing.T) {
	now := time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC)
	record := &ProfileRoleBindingRecord{UserID: 42, Roles: []string{" viewer ", "editor"}, UpdatedBy: 1}
	if err := prepareProfileRoleBinding(record, 0, now); err != nil {
		t.Fatalf("prepareProfileRoleBinding() error = %v", err)
	}
	if record.Revision != 1 || record.CreatedBy != 1 || record.ID.IsZero() {
		t.Fatalf("created binding = %+v", record)
	}
	if record.Roles[0] != "viewer" || !record.CreatedAt.Equal(now) || !record.UpdatedAt.Equal(now) {
		t.Fatalf("normalized binding = %+v", record)
	}
}

func TestPrepareProfileRoleAuditEventRejectsMissingIdentity(t *testing.T) {
	event := &ProfileRoleAuditEvent{Action: ProfileRoleAuditActionUpsert, Outcome: ProfileAuditOutcomeRequested}
	if err := prepareProfileRoleAuditEvent(event, time.Now()); err == nil {
		t.Fatal("prepareProfileRoleAuditEvent() accepted missing identity")
	}
}
