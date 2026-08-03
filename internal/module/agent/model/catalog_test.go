package model

import (
	"errors"
	"testing"
)

func TestCatalogResolveUsesExplicitCapabilityFallback(t *testing.T) {
	catalog, err := NewCatalog([]Definition{
		{
			ID: "chat-small", Provider: "local", ContextWindow: 8192, MaxOutputTokens: 1024,
			Capabilities: []Capability{CapabilityChat}, Fallbacks: []string{"chat-tools"},
		},
		{
			ID: "chat-tools", Provider: "cloud", ContextWindow: 32768, MaxOutputTokens: 4096,
			Capabilities: []Capability{CapabilityChat, CapabilityToolCall, CapabilityJSON},
		},
	})
	if err != nil {
		t.Fatalf("NewCatalog() error = %v", err)
	}

	definition, err := catalog.Resolve("chat-small", CapabilityChat, CapabilityToolCall)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if definition.ID != "chat-tools" || definition.Provider != "cloud" {
		t.Fatalf("Resolve() definition = %+v", definition)
	}
}

func TestCatalogIsImmutableAtBoundary(t *testing.T) {
	definitions := []Definition{{
		ID: "chat", Provider: "provider", ContextWindow: 4096,
		Capabilities: []Capability{CapabilityChat},
	}}
	catalog, err := NewCatalog(definitions)
	if err != nil {
		t.Fatalf("NewCatalog() error = %v", err)
	}
	definitions[0].Capabilities[0] = CapabilityEmbedding
	lookup, _ := catalog.Lookup("chat")
	lookup.Capabilities[0] = CapabilityVision

	again, _ := catalog.Lookup("chat")
	if !again.Supports(CapabilityChat) || again.Supports(CapabilityEmbedding, CapabilityVision) {
		t.Fatalf("catalog definition mutated = %+v", again)
	}
}

func TestCatalogRejectsUnknownFallbackAndMissingCapability(t *testing.T) {
	if _, err := NewCatalog([]Definition{{
		ID: "chat", Provider: "provider", ContextWindow: 4096, Fallbacks: []string{"missing"},
	}}); err == nil {
		t.Fatal("NewCatalog() error = nil, want unknown fallback error")
	}

	catalog, err := NewCatalog([]Definition{{
		ID: "chat", Provider: "provider", ContextWindow: 4096, Capabilities: []Capability{CapabilityChat},
	}})
	if err != nil {
		t.Fatalf("NewCatalog() error = %v", err)
	}
	_, err = catalog.Resolve("chat", CapabilityToolCall)
	if !errors.Is(err, ErrCapabilityMissing) {
		t.Fatalf("Resolve() error = %v, want ErrCapabilityMissing", err)
	}
}

func TestCatalogCandidatesPreserveExplicitFallbackOrder(t *testing.T) {
	catalog, err := NewCatalog([]Definition{
		{ID: "primary", Provider: "a", ContextWindow: 4096, Capabilities: []Capability{CapabilityChat}, Fallbacks: []string{"secondary", "tertiary"}},
		{ID: "secondary", Provider: "b", ContextWindow: 4096, Capabilities: []Capability{CapabilityChat}},
		{ID: "tertiary", Provider: "c", ContextWindow: 4096, Capabilities: []Capability{CapabilityChat}},
	})
	if err != nil {
		t.Fatalf("NewCatalog() error = %v", err)
	}
	candidates, err := catalog.Candidates("primary", CapabilityChat)
	if err != nil {
		t.Fatalf("Candidates() error = %v", err)
	}
	if len(candidates) != 3 || candidates[0].ID != "primary" || candidates[1].ID != "secondary" || candidates[2].ID != "tertiary" {
		t.Fatalf("Candidates() = %+v", candidates)
	}
}

func TestCatalogRequiresVersionForPricedDefinition(t *testing.T) {
	_, err := NewCatalog([]Definition{{
		ID: "priced", Provider: "provider", ContextWindow: 4096,
		Pricing:      Pricing{InputMicrosPerMillionTokens: 1},
		Capabilities: []Capability{CapabilityChat},
	}})
	if err == nil {
		t.Fatal("NewCatalog() error = nil, want pricing version validation")
	}
}
