package service

import (
	"testing"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestExtractPersonaKeywords(t *testing.T) {
	keywords := extractPersonaKeywords("Go, Kubernetes;RAG\nGo|AI Agent,x")
	want := map[string]bool{
		"Go":         true,
		"Kubernetes": true,
		"RAG":        true,
		"AI Agent":   true,
	}

	for _, keyword := range keywords {
		delete(want, keyword)
	}
	if len(want) != 0 {
		t.Fatalf("missing expected keywords: %#v, got %#v", want, keywords)
	}
}

func TestBuildPersonaOnlyBlock(t *testing.T) {
	got := buildPersonaOnlyBlock("  prefers Go and concise answers  ")
	if got != "[Long-term persona]\nprefers Go and concise answers" {
		t.Fatalf("unexpected persona block: %q", got)
	}

	if empty := buildPersonaOnlyBlock(" "); empty != "" {
		t.Fatalf("expected empty block, got %q", empty)
	}
}

func TestDialogueMemoryPointIDUsesSalt(t *testing.T) {
	oid := primitive.NewObjectID()
	first := dialogueMemoryPointID(oid, 1)
	second := dialogueMemoryPointID(oid, 2)
	if first == 0 || second == 0 {
		t.Fatalf("expected non-zero memory point ids, got %d and %d", first, second)
	}
	if first == second {
		t.Fatalf("expected different salts to produce different point ids")
	}
}
