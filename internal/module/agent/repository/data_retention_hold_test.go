package repository

import (
	"testing"
	"time"
)

func TestValidateDataRetentionHoldInput(t *testing.T) {
	now := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		userID     uint64
		reasonCode string
		operator   string
		holdUntil  time.Time
		wantErr    bool
	}{
		{name: "indefinite", userID: 42, reasonCode: "legal.case_2026-01", operator: "security@example.com"},
		{name: "bounded", userID: 42, reasonCode: "user_request", operator: "privacy", holdUntil: now.Add(24 * time.Hour)},
		{name: "missing subject", reasonCode: "legal", operator: "privacy", wantErr: true},
		{name: "invalid reason", userID: 42, reasonCode: "Legal Case", operator: "privacy", wantErr: true},
		{name: "expired", userID: 42, reasonCode: "legal", operator: "privacy", holdUntil: now, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateDataRetentionHoldInput(test.userID, test.reasonCode, test.operator, now, test.holdUntil)
			if (err != nil) != test.wantErr {
				t.Fatalf("validateDataRetentionHoldInput() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestDataRetentionOperatorDigestNormalizesIdentity(t *testing.T) {
	if dataRetentionOperatorDigest(" Privacy@Example.com ") != dataRetentionOperatorDigest("privacy@example.com") {
		t.Fatal("operator digest should normalize case and surrounding whitespace")
	}
}

func TestDataRetentionSubjectHoldActive(t *testing.T) {
	now := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	if !(dataRetentionSubjectDocument{Held: true}).holdActive(now) {
		t.Fatal("indefinite hold should be active")
	}
	if (dataRetentionSubjectDocument{Held: true, HoldUntil: now}).holdActive(now) {
		t.Fatal("expired hold should not be active")
	}
}
