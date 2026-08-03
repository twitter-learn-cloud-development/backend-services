package profile

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	MaxReleaseBasisPoints = 10_000
	ReleasesEnv           = "AGENT_PROFILE_RELEASES"
	StoreEnabledEnv       = "AGENT_PROFILE_STORE_ENABLED"
)

var (
	ErrProfileNotFound      = errors.New("agent profile not found")
	ErrProfileVersionAbsent = errors.New("agent profile version not found")
)

// SelectionSubject provides a stable identity for deterministic rollout.
// UserID is preferred; StickyKey supports future project/session-scoped tests.
type SelectionSubject struct {
	UserID    uint64
	StickyKey string
}

// Release selects a candidate profile version for a stable percentage of
// subjects. Basis points avoid floating-point drift across processes.
type Release struct {
	ProfileID            string `json:"profile_id"`
	StableVersion        string `json:"stable_version"`
	CandidateVersion     string `json:"candidate_version"`
	CandidateBasisPoints int    `json:"candidate_basis_points"`
	Salt                 string `json:"salt"`
}

type Resolver interface {
	Resolve(context.Context, string, SelectionSubject) (AgentProfile, error)
}

// VersionResolver resolves an exact immutable profile version without
// consulting release percentages. It is used when a parent profile has
// already selected the version for a coordinated profile set.
type VersionResolver interface {
	Resolver
	ResolveVersion(context.Context, string, string) (AgentProfile, error)
}

// ProfileSet is a coordinated collection selected from one immutable Catalog
// snapshot. Profiles contains both the anchor and every requested member.
type ProfileSet struct {
	AnchorID string
	Version  string
	Profiles map[string]AgentProfile
}

func (s ProfileSet) Profile(profileID string) (AgentProfile, bool) {
	selected, ok := s.Profiles[strings.TrimSpace(profileID)]
	if !ok {
		return AgentProfile{}, false
	}
	return cloneAgentProfile(selected), true
}

// ProfileSetResolver makes the anchor release the sole rollout decision for
// all members and guarantees they are read from one Catalog snapshot.
type ProfileSetResolver interface {
	VersionResolver
	ResolveProfileSet(context.Context, string, []string, SelectionSubject) (ProfileSet, error)
	ResolveProfileSetVersion(context.Context, string, []string, string) (ProfileSet, error)
}

// Catalog is an immutable in-memory snapshot. Replacing a release means
// constructing a new Catalog and atomically swapping the Resolver at the
// application boundary; in-flight runs continue using their selected copy.
type Catalog struct {
	profiles map[string]map[string]AgentProfile
	releases map[string]Release
}

func NewCatalog(profiles []AgentProfile, releases []Release) (*Catalog, error) {
	catalog := &Catalog{
		profiles: make(map[string]map[string]AgentProfile),
		releases: make(map[string]Release),
	}
	for index, candidate := range profiles {
		if err := validateAgentProfile(candidate); err != nil {
			return nil, fmt.Errorf("profile %d: %w", index, err)
		}
		versions := catalog.profiles[candidate.ID]
		if versions == nil {
			versions = make(map[string]AgentProfile)
			catalog.profiles[candidate.ID] = versions
		}
		if _, exists := versions[candidate.Version]; exists {
			return nil, fmt.Errorf("duplicate agent profile %q version %q", candidate.ID, candidate.Version)
		}
		versions[candidate.Version] = cloneAgentProfile(candidate)
	}
	for index, release := range releases {
		if err := catalog.validateRelease(release); err != nil {
			return nil, fmt.Errorf("release %d: %w", index, err)
		}
		if _, exists := catalog.releases[release.ProfileID]; exists {
			return nil, fmt.Errorf("duplicate release for profile %q", release.ProfileID)
		}
		catalog.releases[release.ProfileID] = release
	}
	return catalog, nil
}

func (c *Catalog) Resolve(_ context.Context, profileID string, subject SelectionSubject) (AgentProfile, error) {
	if c == nil {
		return AgentProfile{}, errors.New("agent profile catalog is nil")
	}
	profileID = strings.TrimSpace(profileID)
	versions, ok := c.profiles[profileID]
	if !ok {
		return AgentProfile{}, fmt.Errorf("%w: %s", ErrProfileNotFound, profileID)
	}
	release, hasRelease := c.releases[profileID]
	if !hasRelease {
		if len(versions) != 1 {
			return AgentProfile{}, fmt.Errorf("profile %q has %d versions but no release", profileID, len(versions))
		}
		for _, candidate := range versions {
			return cloneAgentProfile(candidate), nil
		}
	}

	version := release.StableVersion
	if release.CandidateBasisPoints == MaxReleaseBasisPoints {
		version = release.CandidateVersion
	} else if release.CandidateBasisPoints > 0 {
		identity := rolloutIdentity(subject)
		if identity == "" {
			return AgentProfile{}, fmt.Errorf("profile %q rollout requires a stable subject identity", profileID)
		}
		if releaseBucket(profileID, release.Salt, identity) < release.CandidateBasisPoints {
			version = release.CandidateVersion
		}
	}
	candidate, ok := versions[version]
	if !ok {
		return AgentProfile{}, fmt.Errorf("%w: %s@%s", ErrProfileVersionAbsent, profileID, version)
	}
	return cloneAgentProfile(candidate), nil
}

func (c *Catalog) ResolveVersion(_ context.Context, profileID, version string) (AgentProfile, error) {
	if c == nil {
		return AgentProfile{}, errors.New("agent profile catalog is nil")
	}
	profileID = strings.TrimSpace(profileID)
	version = strings.TrimSpace(version)
	versions, ok := c.profiles[profileID]
	if !ok {
		return AgentProfile{}, fmt.Errorf("%w: %s", ErrProfileNotFound, profileID)
	}
	candidate, ok := versions[version]
	if !ok {
		return AgentProfile{}, fmt.Errorf("%w: %s@%s", ErrProfileVersionAbsent, profileID, version)
	}
	return cloneAgentProfile(candidate), nil
}

func (c *Catalog) ResolveProfileSet(
	ctx context.Context,
	anchorID string,
	memberIDs []string,
	subject SelectionSubject,
) (ProfileSet, error) {
	anchorID = strings.TrimSpace(anchorID)
	anchor, err := c.Resolve(ctx, anchorID, subject)
	if err != nil {
		return ProfileSet{}, fmt.Errorf("resolve profile set anchor: %w", err)
	}
	return c.ResolveProfileSetVersion(ctx, anchorID, memberIDs, anchor.Version)
}

func (c *Catalog) ResolveProfileSetVersion(
	ctx context.Context,
	anchorID string,
	memberIDs []string,
	version string,
) (ProfileSet, error) {
	anchorID = strings.TrimSpace(anchorID)
	version = strings.TrimSpace(version)
	anchor, err := c.ResolveVersion(ctx, anchorID, version)
	if err != nil {
		return ProfileSet{}, fmt.Errorf("resolve profile set anchor version: %w", err)
	}
	resolved := ProfileSet{
		AnchorID: anchorID,
		Version:  version,
		Profiles: map[string]AgentProfile{anchorID: anchor},
	}
	seen := map[string]struct{}{anchorID: {}}
	for index, memberID := range memberIDs {
		memberID = strings.TrimSpace(memberID)
		if memberID == "" {
			return ProfileSet{}, fmt.Errorf("profile set member %d is empty", index)
		}
		if _, exists := seen[memberID]; exists {
			return ProfileSet{}, fmt.Errorf("duplicate profile set member %q", memberID)
		}
		seen[memberID] = struct{}{}
		member, resolveErr := c.ResolveVersion(ctx, memberID, version)
		if resolveErr != nil {
			return ProfileSet{}, fmt.Errorf(
				"resolve profile set member %s@%s: %w",
				memberID,
				version,
				resolveErr,
			)
		}
		resolved.Profiles[memberID] = member
	}
	return resolved, nil
}

func ParseReleases(raw string) ([]Release, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	var releases []Release
	if err := decoder.Decode(&releases); err != nil {
		return nil, fmt.Errorf("decode profile releases: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return nil, errors.New("decode profile releases: multiple JSON values are not allowed")
		}
		return nil, fmt.Errorf("decode profile releases: %w", err)
	}
	return releases, nil
}

func (c *Catalog) validateRelease(release Release) error {
	release.ProfileID = strings.TrimSpace(release.ProfileID)
	release.StableVersion = strings.TrimSpace(release.StableVersion)
	release.CandidateVersion = strings.TrimSpace(release.CandidateVersion)
	release.Salt = strings.TrimSpace(release.Salt)
	if release.ProfileID == "" || release.StableVersion == "" || release.CandidateVersion == "" {
		return errors.New("profile_id, stable_version and candidate_version are required")
	}
	if release.StableVersion == release.CandidateVersion {
		return errors.New("stable and candidate versions must differ")
	}
	if release.CandidateBasisPoints < 0 || release.CandidateBasisPoints > MaxReleaseBasisPoints {
		return fmt.Errorf("candidate_basis_points must be within 0..%d", MaxReleaseBasisPoints)
	}
	if release.CandidateBasisPoints > 0 && release.CandidateBasisPoints < MaxReleaseBasisPoints && release.Salt == "" {
		return errors.New("salt is required for a partial rollout")
	}
	versions, ok := c.profiles[release.ProfileID]
	if !ok {
		return fmt.Errorf("%w: %s", ErrProfileNotFound, release.ProfileID)
	}
	if _, ok := versions[release.StableVersion]; !ok {
		return fmt.Errorf("%w: %s@%s", ErrProfileVersionAbsent, release.ProfileID, release.StableVersion)
	}
	if _, ok := versions[release.CandidateVersion]; !ok {
		return fmt.Errorf("%w: %s@%s", ErrProfileVersionAbsent, release.ProfileID, release.CandidateVersion)
	}
	return nil
}

func validateAgentProfile(candidate AgentProfile) error {
	if strings.TrimSpace(candidate.ID) == "" || strings.TrimSpace(candidate.Version) == "" {
		return errors.New("profile ID and version are required")
	}
	if strings.TrimSpace(candidate.Prompt.ID) == "" || strings.TrimSpace(candidate.Prompt.Version) == "" {
		return errors.New("prompt ID and version are required")
	}
	if strings.TrimSpace(candidate.Prompt.SystemPrompt) == "" {
		return errors.New("system prompt is required")
	}
	seenTools := make(map[string]struct{}, len(candidate.AllowedTools))
	for _, tool := range candidate.AllowedTools {
		tool = strings.TrimSpace(tool)
		if tool == "" {
			return errors.New("allowed tool name cannot be empty")
		}
		if _, exists := seenTools[tool]; exists {
			return fmt.Errorf("duplicate allowed tool %q", tool)
		}
		seenTools[tool] = struct{}{}
	}
	return nil
}

func rolloutIdentity(subject SelectionSubject) string {
	if subject.UserID > 0 {
		return fmt.Sprintf("user:%d", subject.UserID)
	}
	if sticky := strings.TrimSpace(subject.StickyKey); sticky != "" {
		return "key:" + sticky
	}
	return ""
}

func releaseBucket(profileID, salt, identity string) int {
	sum := sha256.Sum256([]byte(profileID + "\x00" + salt + "\x00" + identity))
	return int(binary.BigEndian.Uint64(sum[:8]) % MaxReleaseBasisPoints)
}

func cloneAgentProfile(source AgentProfile) AgentProfile {
	clone := source
	clone.AllowedTools = append([]string(nil), source.AllowedTools...)
	return clone
}
