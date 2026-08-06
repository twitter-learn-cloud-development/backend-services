package product

import (
	"testing"
	"time"
)

func TestNewEventBuildsStableContentFreeIdentity(t *testing.T) {
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	first, err := NewEvent(
		EventConnectorUsed, 42, SubjectExternalMCPConnection, "mcpconn_1", "run-1", "",
		Dimensions{Scope: " user ", Transport: "streamable_http"}, now,
	)
	if err != nil {
		t.Fatalf("NewEvent() error = %v", err)
	}
	second, err := NewEvent(
		EventConnectorUsed, 42, SubjectExternalMCPConnection, "mcpconn_1", "run-1", "",
		Dimensions{Scope: "user", Transport: "streamable_http"}, now.Add(time.Hour),
	)
	if err != nil {
		t.Fatalf("NewEvent() replay error = %v", err)
	}
	if first.ID != second.ID || first.OccurrenceDigest != second.OccurrenceDigest {
		t.Fatalf("event identity changed across replay: first=%+v second=%+v", first, second)
	}
	if first.OccurrenceDigest == "run-1" || first.Dimensions.Scope != "user" {
		t.Fatalf("event did not normalize or digest occurrence identity: %+v", first)
	}
	if !SameFact(first, second) {
		t.Fatal("SameFact() = false for the same logical event")
	}
}

func TestNewEventSeparatesDistinctConnectorRuns(t *testing.T) {
	now := time.Now()
	first, err := NewEvent(
		EventConnectorUsed, 42, SubjectExternalMCPConnection, "mcpconn_1", "run-1", "",
		Dimensions{}, now,
	)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewEvent(
		EventConnectorUsed, 42, SubjectExternalMCPConnection, "mcpconn_1", "run-2", "",
		Dimensions{}, now,
	)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID {
		t.Fatal("different run occurrences produced the same event id")
	}
}
