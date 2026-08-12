package environment

import (
	"context"
	"fmt"
	"time"

	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

const (
	WebReadEnvironmentName = "web.read.v1"
	webReadSnapshotSchema  = "agent.environment.web_read.catalog.v1"
)

var webReadToolNames = []string{
	"page_read",
	"web_search",
}

// WebReadEnvironment exposes only registered, governed public-web read tools.
// Provider credentials and resource content remain in their owning adapters.
type WebReadEnvironment struct {
	read *readCatalogEnvironment
}

type WebReadOption func(*WebReadEnvironment) error

func WithWebReadClock(now func() time.Time) WebReadOption {
	return func(environment *WebReadEnvironment) error {
		if now == nil {
			return fmt.Errorf("web read environment clock is required")
		}
		environment.read.now = now
		return nil
	}
}

func NewWebReadEnvironment(catalog ToolCatalog, options ...WebReadOption) (*WebReadEnvironment, error) {
	read, err := newReadCatalogEnvironment(catalog, readCatalogConfig{
		name: WebReadEnvironmentName, label: "web read", snapshotSchema: webReadSnapshotSchema,
		snapshotIDPrefix: "web-read-catalog:", referencePrefix: "agent-environment://web.read.v1/catalog/sha256/",
		toolNames: webReadToolNames,
	})
	if err != nil {
		return nil, err
	}
	environment := &WebReadEnvironment{read: read}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("web read environment option is required")
		}
		if err := option(environment); err != nil {
			return nil, err
		}
	}
	return environment, nil
}

func WebReadToolNames() []string {
	return append([]string(nil), webReadToolNames...)
}

func (environment *WebReadEnvironment) Name() string {
	return WebReadEnvironmentName
}

func (environment *WebReadEnvironment) Tools(
	ctx context.Context,
	task agentRuntime.TaskSpec,
) ([]agentRuntime.ToolDefinition, error) {
	if environment == nil || environment.read == nil {
		return nil, fmt.Errorf("web read environment is not configured")
	}
	return environment.read.tools(ctx, task)
}

func (environment *WebReadEnvironment) Snapshot(
	ctx context.Context,
	request agentRuntime.SnapshotRequest,
) (agentRuntime.EnvironmentSnapshot, error) {
	if environment == nil || environment.read == nil {
		return agentRuntime.EnvironmentSnapshot{}, fmt.Errorf("web read environment is not configured")
	}
	return environment.read.snapshot(ctx, request)
}

var _ agentRuntime.Environment = (*WebReadEnvironment)(nil)
