package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"twitter-clone/internal/module/agent/profile"
)

const profileCatalogUserIDPlaceholder = "{{user_id}}"

// NewBuiltInProfileResolver creates one immutable application snapshot. The
// caller may replace the default release for a profile, but duplicate release
// declarations are rejected instead of relying on last-write-wins behavior.
func NewBuiltInProfileResolver(releases []profile.Release) (profile.Resolver, error) {
	return NewBuiltInProfileCatalog(nil, nil, releases)
}

// NewBuiltInProfileCatalog builds one validated runtime snapshot. Release
// precedence is defaults < persisted < emergency environment overrides.
func NewBuiltInProfileCatalog(
	persistedProfiles []profile.AgentProfile,
	persistedReleases []profile.Release,
	overrideReleases []profile.Release,
) (*profile.Catalog, error) {
	profiles := append(builtInAgentProfiles(), persistedProfiles...)
	merged := make(map[string]profile.Release)
	for _, layer := range []struct {
		name     string
		releases []profile.Release
	}{
		{name: "default", releases: defaultProfileReleaseSet()},
		{name: "persisted", releases: persistedReleases},
		{name: "override", releases: overrideReleases},
	} {
		if err := mergeProfileReleaseLayer(merged, layer.name, layer.releases); err != nil {
			return nil, err
		}
	}

	profileIDs := make([]string, 0, len(merged))
	for profileID := range merged {
		profileIDs = append(profileIDs, profileID)
	}
	sort.Strings(profileIDs)
	resolvedReleases := make([]profile.Release, 0, len(profileIDs))
	for _, profileID := range profileIDs {
		resolvedReleases = append(resolvedReleases, merged[profileID])
	}
	return profile.NewCatalog(profiles, resolvedReleases)
}

func mergeProfileReleaseLayer(target map[string]profile.Release, name string, releases []profile.Release) error {
	seen := make(map[string]struct{}, len(releases))
	for _, release := range releases {
		profileID := strings.TrimSpace(release.ProfileID)
		if isCoordinatedProfileSetMember(profileID) {
			return fmt.Errorf(
				"%s release for profile set member %q is not allowed; release its parent profile instead",
				name,
				profileID,
			)
		}
		if _, exists := seen[profileID]; exists {
			return fmt.Errorf("duplicate %s release for profile %q", name, profileID)
		}
		seen[profileID] = struct{}{}
		target[profileID] = release
	}
	return nil
}

func builtInAgentProfiles() []profile.AgentProfile {
	assistV1 := assistDraftAgentProfile(0)
	assistV1.Prompt.SystemPrompt = strings.Replace(
		assistV1.Prompt.SystemPrompt,
		"user_id: 0",
		"user_id: "+profileCatalogUserIDPlaceholder,
		1,
	)
	assistV2 := assistV1
	assistV2.Version = "v2"
	assistV2.Prompt.Version = "v2"
	assistV2.Prompt.SystemPrompt += `
6. 输出候选时使用一致结构：角度、正文、适用场景。正文必须紧扣用户主题，不得把检索噪声、历史对话中的无关主题或内部分析过程混入成稿。`
	assistV2.AllowedTools = append([]string(nil), assistV1.AllowedTools...)
	platformSearch := unifiedPlatformSearchAgentProfile(0)
	platformSearch.Prompt.SystemPrompt = strings.Replace(
		platformSearch.Prompt.SystemPrompt,
		"user_id: 0",
		"user_id: "+profileCatalogUserIDPlaceholder,
		1,
	)
	researchDraft := unifiedResearchDraftAgentProfile(0)
	researchDraft.Prompt.SystemPrompt = strings.Replace(
		researchDraft.Prompt.SystemPrompt,
		"user_id: 0",
		"user_id: "+profileCatalogUserIDPlaceholder,
		1,
	)
	researchDraftV2 := unifiedResearchDraftAgentProfileV2(0)
	researchDraftV2.Prompt.SystemPrompt = strings.Replace(
		researchDraftV2.Prompt.SystemPrompt,
		"user_id: 0",
		"user_id: "+profileCatalogUserIDPlaceholder,
		1,
	)
	webSearch := unifiedWebSearchAgentProfile(0, false)
	webSearch.Prompt.SystemPrompt = strings.Replace(
		webSearch.Prompt.SystemPrompt,
		"user_id: 0",
		"user_id: "+profileCatalogUserIDPlaceholder,
		1,
	)
	webDraft := unifiedWebSearchAgentProfile(0, true)
	webDraft.Prompt.SystemPrompt = strings.Replace(
		webDraft.Prompt.SystemPrompt,
		"user_id: 0",
		"user_id: "+profileCatalogUserIDPlaceholder,
		1,
	)
	webDraftV2 := unifiedWebResearchDraftAgentProfileV2(0)
	webDraftV2.Prompt.SystemPrompt = strings.Replace(
		webDraftV2.Prompt.SystemPrompt,
		"user_id: 0",
		"user_id: "+profileCatalogUserIDPlaceholder,
		1,
	)
	externalMCP := unifiedExternalMCPAgentProfile(0)
	externalMCP.Prompt.SystemPrompt = strings.Replace(
		externalMCP.Prompt.SystemPrompt,
		"user_id: 0",
		"user_id: "+profileCatalogUserIDPlaceholder,
		1,
	)
	externalMCPGoverned := unifiedExternalMCPGovernedAgentProfile(0)
	externalMCPGoverned.Prompt.SystemPrompt = strings.Replace(
		externalMCPGoverned.Prompt.SystemPrompt,
		"user_id: 0",
		"user_id: "+profileCatalogUserIDPlaceholder,
		1,
	)
	workflow := unifiedWorkflowAgentProfile(0)
	workflow.Prompt.SystemPrompt = strings.Replace(
		workflow.Prompt.SystemPrompt,
		"user_id 0",
		"user_id "+profileCatalogUserIDPlaceholder,
		1,
	)

	return []profile.AgentProfile{
		conversationReplyAgentProfile(),
		assistV1,
		assistV2,
		platformSearch,
		researchDraft,
		researchDraftV2,
		webSearch,
		webDraft,
		webDraftV2,
		externalMCP,
		externalMCPGoverned,
		workflow,
		workflowStrategyAgentProfile("ReAct"),
		workflowStrategyAgentProfile("PlanExecutor"),
		cloneServiceProfile(multiSearchAgentProfile),
		cloneServiceProfile(multiStyleAgentProfile),
		cloneServiceProfile(multiWriterAgentProfile),
		cloneServiceProfile(multiReviewAgentProfile),
		multiPlatformResearcherAgentProfile(),
		multiPlatformResearcherAgentProfileV2(),
		multiWebResearcherAgentProfile(),
		multiWebResearcherAgentProfileV2(),
		multiDrafterAgentProfile(),
		multiDrafterAgentProfileV2(),
		multiReviewerAgentProfile(),
		multiReviewerAgentProfileV2(),
	}
}

func (s *AgentService) resolveAgentProfile(ctx context.Context, profileID string, userID uint64) (profile.AgentProfile, error) {
	if s == nil || s.profileResolver == nil {
		return profile.AgentProfile{}, fmt.Errorf("agent profile resolver is not configured")
	}
	selected, err := s.profileResolver.Resolve(ctx, profileID, profile.SelectionSubject{UserID: userID})
	if err != nil {
		return profile.AgentProfile{}, err
	}
	return materializeAgentProfileUser(selected, userID), nil
}

func (s *AgentService) resolveAgentProfileSetVersion(
	ctx context.Context,
	anchorID string,
	memberIDs []string,
	version string,
	userID uint64,
) (profile.ProfileSet, error) {
	if s == nil || s.profileResolver == nil {
		return profile.ProfileSet{}, fmt.Errorf("agent profile resolver is not configured")
	}
	resolver, ok := s.profileResolver.(profile.ProfileSetResolver)
	if !ok {
		return profile.ProfileSet{}, fmt.Errorf("agent profile resolver does not support atomic profile sets")
	}
	selected, err := resolver.ResolveProfileSetVersion(
		ctx,
		anchorID,
		append([]string(nil), memberIDs...),
		version,
	)
	if err != nil {
		return profile.ProfileSet{}, err
	}
	for profileID, candidate := range selected.Profiles {
		selected.Profiles[profileID] = materializeAgentProfileUser(candidate, userID)
	}
	return selected, nil
}

func materializeAgentProfileUser(selected profile.AgentProfile, userID uint64) profile.AgentProfile {
	selected.Prompt.SystemPrompt = strings.Replace(
		selected.Prompt.SystemPrompt,
		profileCatalogUserIDPlaceholder,
		fmt.Sprintf("%d", userID),
		1,
	)
	return selected
}

func defaultProfileReleaseSet() []profile.Release {
	profileIDs := []string{
		profileAssistDraft,
		profileUnifiedResearchDraft,
		profileUnifiedWebDraft,
	}
	releases := make([]profile.Release, 0, len(profileIDs))
	for _, profileID := range profileIDs {
		releases = append(releases, profile.Release{
			ProfileID:            profileID,
			StableVersion:        "v1",
			CandidateVersion:     "v2",
			CandidateBasisPoints: 0,
		})
	}
	return releases
}

func isCoordinatedProfileSetMember(profileID string) bool {
	switch strings.TrimSpace(profileID) {
	case profileMultiPlatformResearcher, profileMultiWebResearcher, profileMultiDrafter, profileMultiReviewer:
		return true
	default:
		return false
	}
}

func workflowAgentProfileID(strategy string) string {
	if strings.EqualFold(strings.TrimSpace(strategy), "PlanExecutor") {
		return profileWorkflowPlanExecute
	}
	return profileWorkflowReAct
}

func cloneServiceProfile(source profile.AgentProfile) profile.AgentProfile {
	clone := source
	clone.AllowedTools = append([]string(nil), source.AllowedTools...)
	return clone
}
