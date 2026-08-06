package service

import (
	"context"
	"strings"
	"testing"

	"twitter-clone/internal/module/agent/profile"
)

func TestBuiltInProfileResolverDefaultsToStableAssistVersion(t *testing.T) {
	resolver, err := NewBuiltInProfileResolver(nil)
	if err != nil {
		t.Fatalf("NewBuiltInProfileResolver() error = %v", err)
	}
	selected, err := resolver.Resolve(context.Background(), profileAssistDraft, profile.SelectionSubject{UserID: 42})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if selected.Version != "v1" || selected.Prompt.Version != "v1" {
		t.Fatalf("default profile = %s/%s, want v1/v1", selected.Version, selected.Prompt.Version)
	}
}

func TestBuiltInProfileResolverKeepsQualificationProfilesInactiveByDefault(t *testing.T) {
	resolver, err := NewBuiltInProfileResolver(nil)
	if err != nil {
		t.Fatalf("NewBuiltInProfileResolver() error = %v", err)
	}
	profileIDs := []string{
		profileUnifiedResearchDraft,
		profileUnifiedWebDraft,
	}
	for _, profileID := range profileIDs {
		selected, err := resolver.Resolve(t.Context(), profileID, profile.SelectionSubject{UserID: 42})
		if err != nil {
			t.Fatalf("Resolve(%s) error = %v", profileID, err)
		}
		if selected.Version != "v1" || selected.Prompt.Version != "v1" {
			t.Fatalf("default %s profile = %s/%s, want v1/v1", profileID, selected.Version, selected.Prompt.Version)
		}
	}
	setResolver, ok := resolver.(profile.ProfileSetResolver)
	if !ok {
		t.Fatalf("built-in resolver type %T does not support profile sets", resolver)
	}
	for _, test := range []struct {
		anchor     string
		researcher string
	}{
		{anchor: profileUnifiedResearchDraft, researcher: profileMultiPlatformResearcher},
		{anchor: profileUnifiedWebDraft, researcher: profileMultiWebResearcher},
	} {
		selected, err := setResolver.ResolveProfileSet(t.Context(), test.anchor, []string{
			test.researcher, profileMultiDrafter, profileMultiReviewer,
		}, profile.SelectionSubject{UserID: 42})
		if err != nil {
			t.Fatalf("ResolveProfileSet(%s) error = %v", test.anchor, err)
		}
		if selected.Version != "v1" {
			t.Fatalf("default %s profile set version = %s, want v1", test.anchor, selected.Version)
		}
		for _, profileID := range []string{test.anchor, test.researcher, profileMultiDrafter, profileMultiReviewer} {
			candidate, found := selected.Profile(profileID)
			if !found || candidate.Version != "v1" || candidate.Prompt.Version != "v1" {
				t.Fatalf("default profile set member %s = %+v/%v", profileID, candidate, found)
			}
		}
	}
}

func TestBuiltInProfileResolverSelectsV2MultiRoleContractAsOneSet(t *testing.T) {
	resolver, err := NewBuiltInProfileResolver([]profile.Release{{
		ProfileID:            profileUnifiedResearchDraft,
		StableVersion:        "v1",
		CandidateVersion:     "v2",
		CandidateBasisPoints: profile.MaxReleaseBasisPoints,
	}})
	if err != nil {
		t.Fatalf("NewBuiltInProfileResolver() error = %v", err)
	}
	setResolver, ok := resolver.(profile.ProfileSetResolver)
	if !ok {
		t.Fatalf("built-in resolver type %T does not support profile sets", resolver)
	}
	selected, err := setResolver.ResolveProfileSet(t.Context(), profileUnifiedResearchDraft, []string{
		profileMultiPlatformResearcher, profileMultiDrafter, profileMultiReviewer,
	}, profile.SelectionSubject{UserID: 42})
	if err != nil {
		t.Fatalf("ResolveProfileSet() error = %v", err)
	}
	if selected.Version != "v2" {
		t.Fatalf("profile set version = %q, want v2", selected.Version)
	}
	drafter, found := selected.Profile(profileMultiDrafter)
	if !found || drafter.Version != "v2" || drafter.Prompt.Version != "v2" ||
		!strings.Contains(drafter.Prompt.SystemPrompt, "citations[].snippet") ||
		!strings.Contains(drafter.Prompt.SystemPrompt, "180-600 Chinese characters") {
		t.Fatalf("v2 multi-role drafting contract = %+v/%v", drafter, found)
	}
}

func TestBuiltInProfileResolverRejectsIndependentMultiRoleRelease(t *testing.T) {
	_, err := NewBuiltInProfileResolver([]profile.Release{{
		ProfileID:            profileMultiDrafter,
		StableVersion:        "v1",
		CandidateVersion:     "v2",
		CandidateBasisPoints: profile.MaxReleaseBasisPoints,
	}})
	if err == nil || !strings.Contains(err.Error(), "profile set member") {
		t.Fatalf("NewBuiltInProfileResolver() error = %v", err)
	}
}

func TestBuiltInProfileResolverProvidesToolFreeConversationProfile(t *testing.T) {
	resolver, err := NewBuiltInProfileResolver(nil)
	if err != nil {
		t.Fatalf("NewBuiltInProfileResolver() error = %v", err)
	}
	selected, err := resolver.Resolve(
		context.Background(),
		profileConversationReply,
		profile.SelectionSubject{UserID: 42},
	)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if selected.ID != profileConversationReply ||
		selected.Prompt.ID != "conversation.reply.system" ||
		selected.Version != "v1" ||
		selected.Prompt.Version != "v1" {
		t.Fatalf("conversation profile = %+v", selected)
	}
	if len(selected.AllowedTools) != 0 || selected.Budget.MaxSteps != 1 {
		t.Fatalf("conversation tool/budget policy = %+v/%+v", selected.AllowedTools, selected.Budget)
	}
}

func TestBuiltInProfileResolverRejectsDuplicateConfiguredRelease(t *testing.T) {
	release := profile.Release{
		ProfileID:            profileAssistDraft,
		StableVersion:        "v1",
		CandidateVersion:     "v2",
		CandidateBasisPoints: profile.MaxReleaseBasisPoints,
	}
	if _, err := NewBuiltInProfileResolver([]profile.Release{release, release}); err == nil || !strings.Contains(err.Error(), "duplicate override release") {
		t.Fatalf("NewBuiltInProfileResolver() error = %v", err)
	}
}

func TestResolveAgentProfileMaterializesOnlyReturnedCopy(t *testing.T) {
	resolver, err := NewBuiltInProfileResolver(nil)
	if err != nil {
		t.Fatalf("NewBuiltInProfileResolver() error = %v", err)
	}
	service := &AgentService{profileResolver: resolver}
	selected, err := service.resolveAgentProfile(context.Background(), profileAssistDraft, 99)
	if err != nil {
		t.Fatalf("resolveAgentProfile() error = %v", err)
	}
	if !strings.Contains(selected.Prompt.SystemPrompt, "user_id: 99") {
		t.Fatalf("materialized prompt = %q", selected.Prompt.SystemPrompt)
	}
	raw, err := resolver.Resolve(context.Background(), profileAssistDraft, profile.SelectionSubject{UserID: 99})
	if err != nil {
		t.Fatalf("Resolve(raw) error = %v", err)
	}
	if !strings.Contains(raw.Prompt.SystemPrompt, profileCatalogUserIDPlaceholder) {
		t.Fatalf("catalog prompt was mutated: %q", raw.Prompt.SystemPrompt)
	}
}
