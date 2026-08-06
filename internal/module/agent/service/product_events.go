package service

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"time"

	externalmcp "twitter-clone/internal/module/agent/mcp/remote"
	agentProduct "twitter-clone/internal/module/agent/product"
	"twitter-clone/internal/module/agent/repository"
)

const productEventPersistenceTimeout = 2 * time.Second

func (s *AgentService) recordDraftReadyProductEvent(
	ctx context.Context,
	run *repository.AgentExecutionRun,
) {
	if s == nil || s.productEventStore == nil || run == nil || !run.PublishableDraft {
		return
	}
	dimensions := agentProduct.Dimensions{ExecutionProfile: run.ExecutionProfile}
	if run.ExecutionStrategyPlan != nil {
		dimensions.Strategy = string(run.ExecutionStrategyPlan.SelectedStrategy)
	}
	event, err := agentProduct.NewEvent(
		agentProduct.EventDraftReady,
		run.UserID,
		agentProduct.SubjectAgentRun,
		run.ID,
		"",
		"",
		dimensions,
		firstNonZeroTime(run.FinishedAt, run.UpdatedAt, time.Now()),
	)
	if err != nil {
		slog.WarnContext(ctx, "build draft-ready product event failed", "error", err)
		return
	}
	s.recordProductEventBestEffort(ctx, event)
}

func (s *AgentService) recordDraftPublishedProductEvent(
	ctx context.Context,
	run *repository.AgentExecutionRun,
	tweetID uint64,
) {
	if s == nil || s.productEventStore == nil || run == nil || tweetID == 0 {
		return
	}
	dimensions := agentProduct.Dimensions{ExecutionProfile: run.ExecutionProfile}
	if run.ExecutionStrategyPlan != nil {
		dimensions.Strategy = string(run.ExecutionStrategyPlan.SelectedStrategy)
	}
	event, err := agentProduct.NewEvent(
		agentProduct.EventDraftPublished,
		run.UserID,
		agentProduct.SubjectAgentRun,
		run.ID,
		"",
		strconv.FormatUint(tweetID, 10),
		dimensions,
		firstNonZeroTime(run.DraftPublishedAt, time.Now()),
	)
	if err != nil {
		slog.WarnContext(ctx, "build draft-published product event failed", "error", err)
		return
	}
	s.recordProductEventBestEffort(ctx, event)
}

func (s *AgentService) recordExternalMCPConnectionFacts(
	ctx context.Context,
	connection *externalmcp.Connection,
) {
	if s == nil || s.productEventStore == nil || connection == nil {
		return
	}
	persistCtx, cancel := productEventContext(ctx)
	defer cancel()
	dimensions := externalMCPProductDimensions(connection.Scope, connection.Transport)
	s.recordExternalMCPProductFact(
		persistCtx,
		connection.UserID,
		connection.ID,
		agentProduct.EventConnectorConfigured,
		firstNonZeroTime(connection.CreatedAt, connection.UpdatedAt, time.Now()),
		dimensions,
		"configured",
	)
	if !connection.FirstActivatedAt.IsZero() {
		s.recordExternalMCPProductFact(
			persistCtx,
			connection.UserID,
			connection.ID,
			agentProduct.EventConnectorActivated,
			connection.FirstActivatedAt,
			dimensions,
			"activated",
		)
	}
}

func (s *AgentService) recordExternalMCPUse(
	ctx context.Context,
	definition externalmcp.ExecutableTool,
	runID string,
) {
	runID = strings.TrimSpace(runID)
	if s == nil || s.productEventStore == nil || definition.ConnectionOwnerID == 0 ||
		strings.TrimSpace(definition.ConnectionID) == "" || runID == "" {
		return
	}
	persistCtx, cancel := productEventContext(ctx)
	defer cancel()
	dimensions := externalMCPProductDimensions(definition.ConnectionScope, definition.Transport)
	s.recordExternalMCPProductFact(
		persistCtx,
		definition.ConnectionOwnerID,
		definition.ConnectionID,
		agentProduct.EventConnectorConfigured,
		firstNonZeroTime(definition.ConnectionCreatedAt, time.Now()),
		dimensions,
		"configured",
	)
	s.recordExternalMCPProductFact(
		persistCtx,
		definition.ConnectionOwnerID,
		definition.ConnectionID,
		agentProduct.EventConnectorActivated,
		firstNonZeroTime(definition.ConnectionActivatedAt, time.Now()),
		dimensions,
		"activated",
	)

	used, err := agentProduct.NewEvent(
		agentProduct.EventConnectorUsed,
		definition.ConnectionOwnerID,
		agentProduct.SubjectExternalMCPConnection,
		definition.ConnectionID,
		runID,
		"",
		dimensions,
		time.Now(),
	)
	if err != nil {
		slog.WarnContext(ctx, "build external MCP use product event failed", "error", err)
		return
	}
	_, err = s.productEventStore.RecordProductEvent(persistCtx, used)
	if err != nil {
		slog.WarnContext(ctx, "record external MCP use product event failed", "error", err)
		return
	}
	firstUsed, err := agentProduct.NewEvent(
		agentProduct.EventConnectorFirstUsed,
		definition.ConnectionOwnerID,
		agentProduct.SubjectExternalMCPConnection,
		definition.ConnectionID,
		"",
		"",
		dimensions,
		used.OccurredAt,
	)
	if err != nil {
		slog.WarnContext(ctx, "build external MCP first-use product event failed", "error", err)
		return
	}
	firstCreated, err := s.productEventStore.RecordProductEvent(persistCtx, firstUsed)
	if err != nil {
		slog.WarnContext(ctx, "record external MCP first-use product event failed", "error", err)
		return
	}
	if firstCreated {
		s.observeExternalMCPProductEvent(dimensions, "first_used")
	}
	useCount, err := s.productEventStore.CountProductEvents(
		persistCtx,
		definition.ConnectionOwnerID,
		agentProduct.SubjectExternalMCPConnection,
		definition.ConnectionID,
		agentProduct.EventConnectorUsed,
		2,
	)
	if err != nil {
		slog.WarnContext(ctx, "count external MCP use product events failed", "error", err)
		return
	}
	if useCount < 2 {
		return
	}
	s.recordExternalMCPProductFact(
		persistCtx,
		definition.ConnectionOwnerID,
		definition.ConnectionID,
		agentProduct.EventConnectorReused,
		used.OccurredAt,
		dimensions,
		"reused",
	)
}

func (s *AgentService) recordExternalMCPProductFact(
	ctx context.Context,
	userID uint64,
	connectionID string,
	kind string,
	occurredAt time.Time,
	dimensions agentProduct.Dimensions,
	metricEvent string,
) {
	event, err := agentProduct.NewEvent(
		kind,
		userID,
		agentProduct.SubjectExternalMCPConnection,
		connectionID,
		"",
		"",
		dimensions,
		occurredAt,
	)
	if err != nil {
		slog.WarnContext(ctx, "build external MCP product event failed", "kind", kind, "error", err)
		return
	}
	created, err := s.productEventStore.RecordProductEvent(ctx, event)
	if err != nil {
		slog.WarnContext(ctx, "record external MCP product event failed", "kind", kind, "error", err)
		return
	}
	if created {
		s.observeExternalMCPProductEvent(dimensions, metricEvent)
	}
}

func (s *AgentService) recordProductEventBestEffort(ctx context.Context, event *agentProduct.Event) {
	persistCtx, cancel := productEventContext(ctx)
	defer cancel()
	if _, err := s.productEventStore.RecordProductEvent(persistCtx, event); err != nil {
		slog.WarnContext(ctx, "record Agent product event failed", "kind", event.Kind, "error", err)
	}
}

func (s *AgentService) observeExternalMCPProductEvent(
	dimensions agentProduct.Dimensions,
	event string,
) {
	if s.externalMCPProductObserver != nil {
		s.externalMCPProductObserver.RecordProductEvent(dimensions.Scope, dimensions.Transport, event)
	}
}

func externalMCPProductDimensions(scope, transport string) agentProduct.Dimensions {
	return agentProduct.Dimensions{Scope: strings.TrimSpace(scope), Transport: strings.TrimSpace(transport)}
}

func productEventContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(context.WithoutCancel(ctx), productEventPersistenceTimeout)
}

func firstNonZeroTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value
		}
	}
	return time.Now()
}
