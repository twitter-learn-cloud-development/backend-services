package engine

import (
	"reflect"
	"testing"
)

func TestBlackboardUsesImmutableGenerationsAndAppendOnlyEvents(t *testing.T) {
	blackboard := NewBlackboard()
	original := map[string]interface{}{
		"nested": map[string]interface{}{"value": "original"},
	}
	first := blackboard.ApplyDelta("node", original)
	original["nested"].(map[string]interface{})["value"] = "mutated outside"

	view := blackboard.View()
	value, ok := view.GetValue("node", "nested")
	if !ok || value.(map[string]interface{})["value"] != "original" {
		t.Fatalf("state was mutated through caller input: %#v", value)
	}
	value.(map[string]interface{})["value"] = "mutated view"
	stable, _ := blackboard.GetValue("node", "nested")
	if stable.(map[string]interface{})["value"] != "original" {
		t.Fatalf("state was mutated through read view: %#v", stable)
	}

	second := blackboard.ApplyDelta("node", map[string]interface{}{"next": true})
	if first.Sequence != 1 || second.Sequence != 2 || blackboard.Version() != 2 {
		t.Fatalf("unexpected state sequence: first=%d second=%d version=%d", first.Sequence, second.Sequence, blackboard.Version())
	}
	if view.Version() != 1 {
		t.Fatalf("captured view version changed: %d", view.Version())
	}
	if _, exists := view.GetValue("node", "next"); exists {
		t.Fatal("captured view observed a later generation")
	}
	events := blackboard.EventsAfter(0)
	if len(events) != 2 || events[0].Sequence != 1 || events[1].Sequence != 2 {
		t.Fatalf("unexpected append-only events: %#v", events)
	}
	events[0].Delta["nested"].(map[string]interface{})["value"] = "event mutation"
	again := blackboard.EventsAfter(0)
	if again[0].Delta["nested"].(map[string]interface{})["value"] != "original" {
		t.Fatal("event history was mutable through accessor")
	}
}

func TestBlackboardRestoresCheckpointVersion(t *testing.T) {
	blackboard := NewBlackboard()
	blackboard.LoadSnapshotAtVersion(map[string]map[string]interface{}{
		"done": {"value": "persisted"},
	}, 17)
	if blackboard.Version() != 17 {
		t.Fatalf("expected restored version 17, got %d", blackboard.Version())
	}
	event := blackboard.ApplyDelta("next", map[string]interface{}{"value": "continued"})
	if event.Sequence != 18 {
		t.Fatalf("expected next sequence 18, got %d", event.Sequence)
	}
}

func TestBlackboardReplaysPersistedEventsAndRejectsSequenceGaps(t *testing.T) {
	blackboard := NewBlackboard()
	blackboard.LoadSnapshotAtVersion(map[string]map[string]interface{}{
		"seed": {"value": "persisted"},
	}, 3)
	err := blackboard.Replay([]StateEvent{
		{Sequence: 4, NodeID: "step", Delta: map[string]interface{}{"value": "replayed"}, AppliedAt: 100},
		{Sequence: 5, NodeID: "step", Delta: map[string]interface{}{"done": true}, AppliedAt: 101},
	})
	if err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
	if blackboard.Version() != 5 {
		t.Fatalf("expected version 5, got %d", blackboard.Version())
	}
	value, _ := blackboard.GetValue("step", "value")
	if value != "replayed" {
		t.Fatalf("unexpected replayed value: %#v", value)
	}

	before := blackboard.GetSnapshot()
	if err := blackboard.Replay([]StateEvent{{Sequence: 7, NodeID: "gap", Delta: map[string]interface{}{"bad": true}}}); err == nil {
		t.Fatal("expected sequence gap to fail")
	}
	if blackboard.Version() != 5 {
		t.Fatalf("failed replay mutated version: %d", blackboard.Version())
	}
	if got := blackboard.GetSnapshot(); !reflect.DeepEqual(got, before) {
		t.Fatalf("failed replay mutated state: %#v", got)
	}
}

func TestBlackboardCommitIsImmutable(t *testing.T) {
	blackboard := NewBlackboard()
	blackboard.ApplyDelta("node", map[string]interface{}{"nested": map[string]interface{}{"value": "stable"}})
	commit := blackboard.Commit()
	commit.Snapshot["node"]["nested"].(map[string]interface{})["value"] = "changed"
	commit.Events[0].Delta["nested"].(map[string]interface{})["value"] = "changed"

	value, _ := blackboard.GetValue("node", "nested")
	if value.(map[string]interface{})["value"] != "stable" {
		t.Fatalf("commit exposed mutable state: %#v", value)
	}
}
