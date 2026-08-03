package profile

import (
	"context"
	"errors"
	"testing"
)

func TestCatalogResolveUsesDeterministicRelease(t *testing.T) {
	profiles := []AgentProfile{
		testAgentProfile("assist.draft", "v1"),
		testAgentProfile("assist.draft", "v2"),
	}
	catalog, err := NewCatalog(profiles, []Release{{
		ProfileID:            "assist.draft",
		StableVersion:        "v1",
		CandidateVersion:     "v2",
		CandidateBasisPoints: 5_000,
		Salt:                 "release-2026-07",
	}})
	if err != nil {
		t.Fatalf("NewCatalog() error = %v", err)
	}

	subject := SelectionSubject{UserID: 42}
	first, err := catalog.Resolve(context.Background(), "assist.draft", subject)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	for i := 0; i < 20; i++ {
		next, resolveErr := catalog.Resolve(context.Background(), "assist.draft", subject)
		if resolveErr != nil {
			t.Fatalf("Resolve() iteration %d error = %v", i, resolveErr)
		}
		if next.Version != first.Version {
			t.Fatalf("Resolve() drifted from %q to %q", first.Version, next.Version)
		}
	}
}

func TestCatalogReleaseBoundariesAndImmutableCopies(t *testing.T) {
	profiles := []AgentProfile{
		testAgentProfile("assist.draft", "v1"),
		testAgentProfile("assist.draft", "v2"),
	}
	stable, err := NewCatalog(profiles, []Release{{
		ProfileID: "assist.draft", StableVersion: "v1", CandidateVersion: "v2",
		CandidateBasisPoints: 0,
	}})
	if err != nil {
		t.Fatalf("NewCatalog(stable) error = %v", err)
	}
	selected, err := stable.Resolve(context.Background(), "assist.draft", SelectionSubject{})
	if err != nil {
		t.Fatalf("Resolve(stable) error = %v", err)
	}
	if selected.Version != "v1" {
		t.Fatalf("stable version = %q, want v1", selected.Version)
	}
	selected.AllowedTools[0] = "mutated"
	again, err := stable.Resolve(context.Background(), "assist.draft", SelectionSubject{})
	if err != nil {
		t.Fatalf("Resolve(stable again) error = %v", err)
	}
	if again.AllowedTools[0] != "search" {
		t.Fatalf("catalog snapshot was mutated: %+v", again.AllowedTools)
	}

	candidate, err := NewCatalog(profiles, []Release{{
		ProfileID: "assist.draft", StableVersion: "v1", CandidateVersion: "v2",
		CandidateBasisPoints: MaxReleaseBasisPoints,
	}})
	if err != nil {
		t.Fatalf("NewCatalog(candidate) error = %v", err)
	}
	selected, err = candidate.Resolve(context.Background(), "assist.draft", SelectionSubject{})
	if err != nil {
		t.Fatalf("Resolve(candidate) error = %v", err)
	}
	if selected.Version != "v2" {
		t.Fatalf("candidate version = %q, want v2", selected.Version)
	}
}

func TestCatalogResolveProfileSetUsesAnchorVersionForEveryMember(t *testing.T) {
	profiles := []AgentProfile{
		testAgentProfile("research.parent", "v1"),
		testAgentProfile("research.parent", "v2"),
		testAgentProfile("researcher", "v1"),
		testAgentProfile("researcher", "v2"),
		testAgentProfile("drafter", "v1"),
		testAgentProfile("drafter", "v2"),
	}
	catalog, err := NewCatalog(profiles, []Release{{
		ProfileID:            "research.parent",
		StableVersion:        "v1",
		CandidateVersion:     "v2",
		CandidateBasisPoints: MaxReleaseBasisPoints,
	}})
	if err != nil {
		t.Fatalf("NewCatalog() error = %v", err)
	}

	selected, err := catalog.ResolveProfileSet(
		context.Background(),
		"research.parent",
		[]string{"researcher", "drafter"},
		SelectionSubject{UserID: 42},
	)
	if err != nil {
		t.Fatalf("ResolveProfileSet() error = %v", err)
	}
	if selected.AnchorID != "research.parent" || selected.Version != "v2" {
		t.Fatalf("profile set identity = %+v", selected)
	}
	for _, profileID := range []string{"research.parent", "researcher", "drafter"} {
		candidate, ok := selected.Profile(profileID)
		if !ok || candidate.Version != "v2" {
			t.Fatalf("profile set member %s = %+v/%v", profileID, candidate, ok)
		}
	}

	mutated, _ := selected.Profile("drafter")
	mutated.AllowedTools[0] = "mutated"
	again, err := catalog.ResolveVersion(context.Background(), "drafter", "v2")
	if err != nil || again.AllowedTools[0] != "search" {
		t.Fatalf("ResolveVersion() immutable copy = %+v, %v", again, err)
	}
}

func TestCatalogResolveProfileSetFailsClosedOnIncompleteOrDuplicateMembers(t *testing.T) {
	catalog, err := NewCatalog([]AgentProfile{
		testAgentProfile("research.parent", "v1"),
		testAgentProfile("research.parent", "v2"),
		testAgentProfile("researcher", "v1"),
	}, []Release{{
		ProfileID:            "research.parent",
		StableVersion:        "v1",
		CandidateVersion:     "v2",
		CandidateBasisPoints: MaxReleaseBasisPoints,
	}})
	if err != nil {
		t.Fatalf("NewCatalog() error = %v", err)
	}

	if _, err := catalog.ResolveProfileSet(
		context.Background(), "research.parent", []string{"researcher"}, SelectionSubject{},
	); !errors.Is(err, ErrProfileVersionAbsent) {
		t.Fatalf("ResolveProfileSet(incomplete) error = %v", err)
	}
	if _, err := catalog.ResolveProfileSet(
		context.Background(), "research.parent", []string{"research.parent"}, SelectionSubject{},
	); err == nil {
		t.Fatal("ResolveProfileSet(duplicate anchor) error = nil")
	}
}

func TestCatalogRejectsInvalidReleases(t *testing.T) {
	profiles := []AgentProfile{
		testAgentProfile("assist.draft", "v1"),
		testAgentProfile("assist.draft", "v2"),
	}
	tests := []struct {
		name    string
		release Release
	}{
		{name: "unknown profile", release: Release{ProfileID: "missing", StableVersion: "v1", CandidateVersion: "v2"}},
		{name: "unknown version", release: Release{ProfileID: "assist.draft", StableVersion: "v1", CandidateVersion: "v3"}},
		{name: "same version", release: Release{ProfileID: "assist.draft", StableVersion: "v1", CandidateVersion: "v1"}},
		{name: "invalid basis points", release: Release{ProfileID: "assist.draft", StableVersion: "v1", CandidateVersion: "v2", CandidateBasisPoints: MaxReleaseBasisPoints + 1}},
		{name: "partial without salt", release: Release{ProfileID: "assist.draft", StableVersion: "v1", CandidateVersion: "v2", CandidateBasisPoints: 1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewCatalog(profiles, []Release{test.release}); err == nil {
				t.Fatal("NewCatalog() error = nil")
			}
		})
	}
}

func TestCatalogPartialReleaseRequiresStableIdentity(t *testing.T) {
	catalog, err := NewCatalog([]AgentProfile{
		testAgentProfile("assist.draft", "v1"),
		testAgentProfile("assist.draft", "v2"),
	}, []Release{{
		ProfileID: "assist.draft", StableVersion: "v1", CandidateVersion: "v2",
		CandidateBasisPoints: 1, Salt: "release-2026-07",
	}})
	if err != nil {
		t.Fatalf("NewCatalog() error = %v", err)
	}
	if _, err := catalog.Resolve(context.Background(), "assist.draft", SelectionSubject{}); err == nil {
		t.Fatal("Resolve() error = nil")
	}
}

func TestCatalogNotFoundAndStrictReleaseParsing(t *testing.T) {
	catalog, err := NewCatalog([]AgentProfile{testAgentProfile("workflow.react", "v1")}, nil)
	if err != nil {
		t.Fatalf("NewCatalog() error = %v", err)
	}
	if _, err := catalog.Resolve(context.Background(), "missing", SelectionSubject{}); !errors.Is(err, ErrProfileNotFound) {
		t.Fatalf("Resolve() error = %v, want ErrProfileNotFound", err)
	}

	releases, err := ParseReleases(`[{"profile_id":"assist.draft","stable_version":"v1","candidate_version":"v2","candidate_basis_points":2500,"salt":"canary-a"}]`)
	if err != nil {
		t.Fatalf("ParseReleases() error = %v", err)
	}
	if len(releases) != 1 || releases[0].CandidateBasisPoints != 2_500 {
		t.Fatalf("ParseReleases() = %+v", releases)
	}
	if _, err := ParseReleases(`[{"profile_id":"assist.draft","unknown":true}]`); err == nil {
		t.Fatal("ParseReleases(unknown field) error = nil")
	}
	if _, err := ParseReleases(`[] []`); err == nil {
		t.Fatal("ParseReleases(multiple values) error = nil")
	}
}

func testAgentProfile(id, version string) AgentProfile {
	return AgentProfile{
		ID: id, Version: version,
		Prompt:       PromptProfile{ID: id + ".system", Version: version, SystemPrompt: "system"},
		AllowedTools: []string{"search"},
	}
}
