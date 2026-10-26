package service

import (
	"context"
	"errors"
	"testing"
	"time"
)

type toolResultObjectInventoryFake struct {
	items      []ToolResultObjectInventoryItem
	nextCursor string
	err        error
}

func (f *toolResultObjectInventoryFake) ListToolResultObjects(context.Context, string, int) ([]ToolResultObjectInventoryItem, string, error) {
	return f.items, f.nextCursor, f.err
}

type toolResultReferenceLookupFake struct {
	referenced map[string]bool
	errors     map[string]error
}

func (f *toolResultReferenceLookupFake) ToolResultReferenceExists(_ context.Context, key string) (bool, error) {
	if err := f.errors[key]; err != nil {
		return false, err
	}
	return f.referenced[key], nil
}

func TestToolResultOrphanAuditorClassifiesBoundedInventory(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	inventory := &toolResultObjectInventoryFake{
		nextCursor: "tool-results/next",
		items: []ToolResultObjectInventoryItem{
			{Key: "referenced", KeyDigest: "key-1", ValidOwnedKey: true, LastModified: now.Add(-48 * time.Hour), Length: 10},
			{Key: "orphan", KeyDigest: "key-2", TenantHash: "tenant", ObjectDigest: "object", ValidOwnedKey: true, LastModified: now.Add(-48 * time.Hour), Length: 20},
			{Key: "recent", KeyDigest: "key-3", ValidOwnedKey: true, LastModified: now.Add(-time.Hour), Length: 30},
			{Key: "invalid", KeyDigest: "key-4"},
		},
	}
	auditor, err := NewToolResultOrphanAuditor(inventory, &toolResultReferenceLookupFake{
		referenced: map[string]bool{"referenced": true}, errors: map[string]error{},
	}, DefaultToolResultOrphanAuditConfig())
	if err != nil {
		t.Fatal(err)
	}
	report, err := auditor.Audit(context.Background(), now, "")
	if err != nil {
		t.Fatal(err)
	}
	if report.Scanned != 4 || report.Referenced != 1 || report.Recent != 1 || report.Invalid != 1 || len(report.Candidates) != 1 {
		t.Fatalf("unexpected report: %+v", report)
	}
	if report.NextCursor != inventory.nextCursor || report.Candidates[0].KeyDigest != "key-2" {
		t.Fatalf("unexpected cursor or candidate: %+v", report)
	}
}

func TestToolResultOrphanAuditorFailsClosedOnReferenceLookupError(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	auditor, err := NewToolResultOrphanAuditor(
		&toolResultObjectInventoryFake{items: []ToolResultObjectInventoryItem{{
			Key: "uncertain", KeyDigest: "digest", ValidOwnedKey: true,
			LastModified: now.Add(-48 * time.Hour), Length: 10,
		}}},
		&toolResultReferenceLookupFake{referenced: map[string]bool{}, errors: map[string]error{"uncertain": errors.New("mongo unavailable")}},
		DefaultToolResultOrphanAuditConfig(),
	)
	if err != nil {
		t.Fatal(err)
	}
	report, err := auditor.Audit(context.Background(), now, "")
	if err == nil || report.LookupFailed != 1 || len(report.Candidates) != 0 {
		t.Fatalf("lookup failure must not become an orphan candidate: report=%+v err=%v", report, err)
	}
}

func TestToolResultOrphanAuditConfigIsBounded(t *testing.T) {
	config := DefaultToolResultOrphanAuditConfig()
	config.Limit = 1001
	if err := config.Validate(); err == nil {
		t.Fatal("expected bounded audit limit validation")
	}
}
