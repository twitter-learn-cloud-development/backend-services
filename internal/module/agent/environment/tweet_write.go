package environment

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

const (
	TweetWriteEnvironmentName = "twitter.write.v1"
	TweetWriteSnapshotSchema  = "agent.environment.twitter_write.state.v1"
	TweetPublishToolName      = "create_tweet"

	maxTweetWriteCatalogTools = 128
	maxTweetWriteStateItems   = 64
)

// TweetWriteState is the minimum authoritative projection needed to prove
// that a write result exists in the user's timeline. Tweet content is never
// copied into the Agent environment.
type TweetWriteState struct {
	TweetID  uint64
	AuthorID uint64
}

type TweetWriteStatePage struct {
	Tweets  []TweetWriteState
	HasMore bool
}

type TweetWriteStateReader interface {
	ListRecentTweetWriteState(ctx context.Context, userID uint64, limit int) (TweetWriteStatePage, error)
}

// TweetWriteEnvironment observes current governed publish capability and the
// authoritative user timeline. Tool execution remains behind ToolExecutor.
type TweetWriteEnvironment struct {
	catalog ToolCatalog
	state   TweetWriteStateReader
	userID  uint64
	now     func() time.Time
}

type TweetWriteOption func(*TweetWriteEnvironment) error

func WithTweetWriteClock(now func() time.Time) TweetWriteOption {
	return func(environment *TweetWriteEnvironment) error {
		if now == nil {
			return fmt.Errorf("tweet write environment clock is required")
		}
		environment.now = now
		return nil
	}
}

func NewTweetWriteEnvironment(
	catalog ToolCatalog,
	state TweetWriteStateReader,
	userID uint64,
	options ...TweetWriteOption,
) (*TweetWriteEnvironment, error) {
	if catalog == nil {
		return nil, fmt.Errorf("tweet write tool catalog is required")
	}
	if state == nil {
		return nil, fmt.Errorf("tweet write state reader is required")
	}
	if userID == 0 {
		return nil, fmt.Errorf("tweet write environment user is required")
	}
	environment := &TweetWriteEnvironment{catalog: catalog, state: state, userID: userID, now: time.Now}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("tweet write environment option is required")
		}
		if err := option(environment); err != nil {
			return nil, err
		}
	}
	return environment, nil
}

func (environment *TweetWriteEnvironment) Name() string {
	return TweetWriteEnvironmentName
}

func (environment *TweetWriteEnvironment) Tools(
	ctx context.Context,
	task agentRuntime.TaskSpec,
) ([]agentRuntime.ToolDefinition, error) {
	if environment == nil || environment.catalog == nil || environment.userID == 0 {
		return nil, fmt.Errorf("tweet write environment is not configured")
	}
	if ctx == nil {
		return nil, fmt.Errorf("tweet write environment context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := task.Validate(); err != nil {
		return nil, fmt.Errorf("validate tweet write task: %w", err)
	}

	requested := make(map[string]struct{}, len(task.AllowedTools))
	for _, name := range task.AllowedTools {
		requested[name] = struct{}{}
	}
	tools, err := environment.catalog.ListTools(ctx)
	if err != nil {
		return nil, fmt.Errorf("list tweet write tools: %w", err)
	}
	if len(tools) > maxTweetWriteCatalogTools {
		return nil, fmt.Errorf("tweet write catalog exceeds %d tools", maxTweetWriteCatalogTools)
	}

	seen := make(map[string]struct{}, len(tools))
	available := make([]agentRuntime.ToolDefinition, 0, 1)
	for _, tool := range tools {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			return nil, fmt.Errorf("catalog tool name is required")
		}
		if _, exists := seen[name]; exists {
			return nil, fmt.Errorf("duplicate catalog tool %q", name)
		}
		seen[name] = struct{}{}
		if name != TweetPublishToolName {
			continue
		}
		if _, allowed := requested[name]; !allowed {
			continue
		}
		if tool.Category != agentRuntime.ToolCategoryWrite || !tool.ApprovalRequired() {
			return nil, fmt.Errorf("tweet write tool %q is not safely classified", name)
		}
		if len(bytes.TrimSpace(tool.InputSchema)) == 0 || !json.Valid(tool.InputSchema) {
			return nil, fmt.Errorf("tweet write tool %q input schema is invalid", name)
		}
		tool.Name = name
		tool.InputSchema = append(json.RawMessage(nil), tool.InputSchema...)
		available = append(available, tool)
	}
	return available, nil
}

func (environment *TweetWriteEnvironment) Snapshot(
	ctx context.Context,
	request agentRuntime.SnapshotRequest,
) (agentRuntime.EnvironmentSnapshot, error) {
	if environment == nil || environment.state == nil {
		return agentRuntime.EnvironmentSnapshot{}, fmt.Errorf("tweet write environment is not configured")
	}
	if request.Phase != agentRuntime.SnapshotPhaseBefore && request.Phase != agentRuntime.SnapshotPhaseAfter {
		return agentRuntime.EnvironmentSnapshot{}, fmt.Errorf("unsupported tweet write snapshot phase %q", request.Phase)
	}
	if len(request.Scope) != 0 {
		return agentRuntime.EnvironmentSnapshot{}, fmt.Errorf("tweet write snapshot does not support resource scope")
	}
	tools, err := environment.Tools(ctx, request.Task)
	if err != nil {
		return agentRuntime.EnvironmentSnapshot{}, err
	}
	page, err := environment.state.ListRecentTweetWriteState(ctx, environment.userID, maxTweetWriteStateItems)
	if err != nil {
		return agentRuntime.EnvironmentSnapshot{}, fmt.Errorf("list recent tweet write state: %w", err)
	}
	if len(page.Tweets) > maxTweetWriteStateItems {
		return agentRuntime.EnvironmentSnapshot{}, fmt.Errorf("tweet write state exceeds %d items", maxTweetWriteStateItems)
	}

	references := make([]string, 0, len(page.Tweets))
	seen := make(map[uint64]struct{}, len(page.Tweets))
	for _, tweet := range page.Tweets {
		if tweet.TweetID == 0 || tweet.AuthorID != environment.userID {
			return agentRuntime.EnvironmentSnapshot{}, fmt.Errorf("tweet write state contains an invalid ownership binding")
		}
		if _, exists := seen[tweet.TweetID]; exists {
			return agentRuntime.EnvironmentSnapshot{}, fmt.Errorf("tweet write state contains duplicate tweet %d", tweet.TweetID)
		}
		seen[tweet.TweetID] = struct{}{}
		references = append(references, tweetReference(tweet.TweetID))
	}
	sort.Strings(references)
	toolIdentities := make([]tweetWriteToolIdentity, 0, len(tools))
	for _, tool := range tools {
		toolIdentities = append(toolIdentities, tweetWriteToolIdentity{
			Name: tool.Name, Category: tool.Category, RequiresApproval: tool.ApprovalRequired(),
		})
	}
	identity := tweetWriteSnapshotIdentity{
		Schema: TweetWriteSnapshotSchema, Environment: TweetWriteEnvironmentName,
		ActorDigest: tweetWriteActorDigest(environment.userID), HasMore: page.HasMore,
		Tools: toolIdentities, TweetReferences: references,
	}
	digest, encodedIdentity, err := tweetWriteIdentityDigest(identity)
	if err != nil {
		return agentRuntime.EnvironmentSnapshot{}, err
	}
	_ = encodedIdentity
	hexDigest := strings.TrimPrefix(digest, "sha256:")
	metadata, err := json.Marshal(tweetWriteSnapshotMetadata{
		Schema: identity.Schema, Phase: request.Phase, ActorDigest: identity.ActorDigest,
		HasMore: identity.HasMore, ToolCount: len(identity.Tools), Tools: identity.Tools,
		TweetCount: len(identity.TweetReferences), TweetReferences: identity.TweetReferences,
	})
	if err != nil {
		return agentRuntime.EnvironmentSnapshot{}, fmt.Errorf("encode tweet write snapshot metadata: %w", err)
	}
	return agentRuntime.EnvironmentSnapshot{
		ID:          "tweet-write-state:" + hexDigest[:24],
		Environment: TweetWriteEnvironmentName,
		CapturedAt:  environment.now().UTC(),
		Digest:      digest,
		Reference:   "agent-environment://twitter.write.v1/state/sha256/" + hexDigest,
		Metadata:    metadata,
	}, nil
}

type TweetWriteSnapshotView struct {
	HasMore         bool
	ToolNames       []string
	TweetReferences []string
}

func DecodeTweetWriteSnapshot(
	snapshot *agentRuntime.EnvironmentSnapshot,
	phase agentRuntime.SnapshotPhase,
	userID uint64,
) (TweetWriteSnapshotView, error) {
	if snapshot == nil || snapshot.Environment != TweetWriteEnvironmentName || userID == 0 {
		return TweetWriteSnapshotView{}, fmt.Errorf("tweet write snapshot identity is invalid")
	}
	var metadata tweetWriteSnapshotMetadata
	if len(snapshot.Metadata) == 0 || !json.Valid(snapshot.Metadata) || json.Unmarshal(snapshot.Metadata, &metadata) != nil {
		return TweetWriteSnapshotView{}, fmt.Errorf("tweet write snapshot metadata is invalid")
	}
	if metadata.Schema != TweetWriteSnapshotSchema || metadata.Phase != phase ||
		metadata.ActorDigest != tweetWriteActorDigest(userID) ||
		metadata.ToolCount != len(metadata.Tools) || metadata.TweetCount != len(metadata.TweetReferences) ||
		len(metadata.TweetReferences) > maxTweetWriteStateItems {
		return TweetWriteSnapshotView{}, fmt.Errorf("tweet write snapshot metadata binding is invalid")
	}
	toolNames := make([]string, 0, len(metadata.Tools))
	seenTools := make(map[string]struct{}, len(metadata.Tools))
	for _, tool := range metadata.Tools {
		if tool.Name != TweetPublishToolName || tool.Category != agentRuntime.ToolCategoryWrite || !tool.RequiresApproval {
			return TweetWriteSnapshotView{}, fmt.Errorf("tweet write snapshot tool binding is invalid")
		}
		if _, exists := seenTools[tool.Name]; exists {
			return TweetWriteSnapshotView{}, fmt.Errorf("tweet write snapshot contains duplicate tool %q", tool.Name)
		}
		seenTools[tool.Name] = struct{}{}
		toolNames = append(toolNames, tool.Name)
	}
	if len(toolNames) != 1 {
		return TweetWriteSnapshotView{}, fmt.Errorf("tweet write snapshot requires the publish tool")
	}
	references := append([]string(nil), metadata.TweetReferences...)
	sorted := append([]string(nil), references...)
	sort.Strings(sorted)
	seenReferences := make(map[string]struct{}, len(sorted))
	for index, reference := range sorted {
		if reference != references[index] || !validTweetReference(reference) {
			return TweetWriteSnapshotView{}, fmt.Errorf("tweet write snapshot reference is invalid")
		}
		if _, exists := seenReferences[reference]; exists {
			return TweetWriteSnapshotView{}, fmt.Errorf("tweet write snapshot contains duplicate reference %q", reference)
		}
		seenReferences[reference] = struct{}{}
	}
	identity := tweetWriteSnapshotIdentity{
		Schema: metadata.Schema, Environment: TweetWriteEnvironmentName,
		ActorDigest: metadata.ActorDigest, HasMore: metadata.HasMore,
		Tools: metadata.Tools, TweetReferences: references,
	}
	digest, _, err := tweetWriteIdentityDigest(identity)
	if err != nil || digest != snapshot.Digest {
		return TweetWriteSnapshotView{}, fmt.Errorf("tweet write snapshot digest is invalid")
	}
	hexDigest := strings.TrimPrefix(digest, "sha256:")
	if snapshot.ID != "tweet-write-state:"+hexDigest[:24] ||
		snapshot.Reference != "agent-environment://twitter.write.v1/state/sha256/"+hexDigest {
		return TweetWriteSnapshotView{}, fmt.Errorf("tweet write snapshot reference binding is invalid")
	}
	return TweetWriteSnapshotView{HasMore: metadata.HasMore, ToolNames: toolNames, TweetReferences: references}, nil
}

func TweetReference(tweetID uint64) string {
	return tweetReference(tweetID)
}

func tweetReference(tweetID uint64) string {
	return "/tweets/" + strconv.FormatUint(tweetID, 10)
}

func validTweetReference(reference string) bool {
	value := strings.TrimPrefix(reference, "/tweets/")
	if value == reference || value == "" || strings.Contains(value, "/") {
		return false
	}
	id, err := strconv.ParseUint(value, 10, 64)
	return err == nil && id != 0 && strconv.FormatUint(id, 10) == value
}

func tweetWriteActorDigest(userID uint64) string {
	return sha256Digest([]byte("tweet-write-actor:" + strconv.FormatUint(userID, 10)))
}

func tweetWriteIdentityDigest(identity tweetWriteSnapshotIdentity) (string, []byte, error) {
	encoded, err := json.Marshal(identity)
	if err != nil {
		return "", nil, fmt.Errorf("encode tweet write snapshot identity: %w", err)
	}
	return sha256Digest(encoded), encoded, nil
}

type tweetWriteSnapshotIdentity struct {
	Schema          string                   `json:"schema"`
	Environment     string                   `json:"environment"`
	ActorDigest     string                   `json:"actor_digest"`
	HasMore         bool                     `json:"has_more"`
	Tools           []tweetWriteToolIdentity `json:"tools"`
	TweetReferences []string                 `json:"tweet_references"`
}

type tweetWriteToolIdentity struct {
	Name             string                    `json:"name"`
	Category         agentRuntime.ToolCategory `json:"category"`
	RequiresApproval bool                      `json:"requires_approval"`
}

type tweetWriteSnapshotMetadata struct {
	Schema          string                     `json:"schema"`
	Phase           agentRuntime.SnapshotPhase `json:"phase"`
	ActorDigest     string                     `json:"actor_digest"`
	HasMore         bool                       `json:"has_more"`
	ToolCount       int                        `json:"tool_count"`
	Tools           []tweetWriteToolIdentity   `json:"tools"`
	TweetCount      int                        `json:"tweet_count"`
	TweetReferences []string                   `json:"tweet_references"`
}

var _ agentRuntime.Environment = (*TweetWriteEnvironment)(nil)
