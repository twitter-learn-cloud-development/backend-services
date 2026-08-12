package environment

import (
	"context"
	"fmt"
	"time"

	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

const (
	TwitterReadEnvironmentName = "twitter.read.v1"
	twitterReadSnapshotSchema  = "agent.environment.twitter_read.catalog.v1"
)

var twitterReadToolNames = []string{
	"get_tweets_by_ids",
	"get_user_tweets",
	"hybrid_search_tweets",
	"search_tweets_by_semantic",
	"search_users",
}

// TwitterReadEnvironment is a read-only view over first-party Twitter tools.
// It never executes tools or stores catalog descriptions and schemas.
type TwitterReadEnvironment struct {
	read *readCatalogEnvironment
}

type TwitterReadOption func(*TwitterReadEnvironment) error

func WithTwitterReadClock(now func() time.Time) TwitterReadOption {
	return func(environment *TwitterReadEnvironment) error {
		if now == nil {
			return fmt.Errorf("twitter read environment clock is required")
		}
		environment.read.now = now
		return nil
	}
}

func NewTwitterReadEnvironment(catalog ToolCatalog, options ...TwitterReadOption) (*TwitterReadEnvironment, error) {
	read, err := newReadCatalogEnvironment(catalog, readCatalogConfig{
		name: TwitterReadEnvironmentName, label: "twitter read", snapshotSchema: twitterReadSnapshotSchema,
		snapshotIDPrefix: "twitter-read-catalog:", referencePrefix: "agent-environment://twitter.read.v1/catalog/sha256/",
		toolNames: twitterReadToolNames,
	})
	if err != nil {
		return nil, err
	}
	environment := &TwitterReadEnvironment{read: read}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("twitter read environment option is required")
		}
		if err := option(environment); err != nil {
			return nil, err
		}
	}
	return environment, nil
}

func TwitterReadToolNames() []string {
	return append([]string(nil), twitterReadToolNames...)
}

func (environment *TwitterReadEnvironment) Name() string {
	return TwitterReadEnvironmentName
}

func (environment *TwitterReadEnvironment) Tools(
	ctx context.Context,
	task agentRuntime.TaskSpec,
) ([]agentRuntime.ToolDefinition, error) {
	if environment == nil || environment.read == nil {
		return nil, fmt.Errorf("twitter read environment is not configured")
	}
	return environment.read.tools(ctx, task)
}

func (environment *TwitterReadEnvironment) Snapshot(
	ctx context.Context,
	request agentRuntime.SnapshotRequest,
) (agentRuntime.EnvironmentSnapshot, error) {
	if environment == nil || environment.read == nil {
		return agentRuntime.EnvironmentSnapshot{}, fmt.Errorf("twitter read environment is not configured")
	}
	return environment.read.snapshot(ctx, request)
}

var _ agentRuntime.Environment = (*TwitterReadEnvironment)(nil)
