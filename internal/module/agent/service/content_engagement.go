package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"twitter-clone/internal/module/agent/attribution"
	agentObservability "twitter-clone/internal/module/agent/observability"
	"twitter-clone/internal/module/agent/profile"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

const (
	DefaultContentAttributionWindow = 7 * 24 * time.Hour
	contentAttributionClockSkew     = 5 * time.Second

	ContentEngagementResultAttributed      = "attributed"
	ContentEngagementResultReplayed        = "replayed"
	ContentEngagementResultIgnoredNonAgent = "ignored_non_agent"
	ContentEngagementResultIgnoredSelf     = "ignored_self"
	ContentEngagementResultIgnoredExpired  = "ignored_expired"
)

var ErrInvalidContentEngagementAttribution = errors.New("invalid content engagement attribution")

type ContentEngagementProcessor struct {
	store           attribution.Store
	traceReader     agentObservability.Reader
	outcomeRecorder ProductOutcomeRecorder
}

func NewContentEngagementProcessor(
	store attribution.Store,
	traceReader agentObservability.Reader,
	outcomeRecorder ProductOutcomeRecorder,
) (*ContentEngagementProcessor, error) {
	if store == nil || traceReader == nil || outcomeRecorder == nil {
		return nil, errors.New("content attribution store, trace reader and outcome recorder are required")
	}
	return &ContentEngagementProcessor{store: store, traceReader: traceReader, outcomeRecorder: outcomeRecorder}, nil
}

func (p *ContentEngagementProcessor) Process(
	ctx context.Context,
	event attribution.ContentEngagement,
) (string, error) {
	if p == nil || p.store == nil || p.traceReader == nil || p.outcomeRecorder == nil {
		return "", errors.New("content engagement processor is unavailable")
	}
	if err := event.Validate(); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidContentEngagementAttribution, err)
	}
	record, err := p.store.GetPublishedContent(ctx, event.TweetID)
	if errors.Is(err, attribution.ErrPublishedContentNotFound) {
		return ContentEngagementResultIgnoredNonAgent, nil
	}
	if err != nil {
		return "", err
	}
	if record.AuthorUserID != event.AuthorUserID {
		return "", fmt.Errorf("%w: tweet author does not match attribution", ErrInvalidContentEngagementAttribution)
	}
	if event.ActorUserID == record.AuthorUserID {
		return ContentEngagementResultIgnoredSelf, nil
	}
	if event.OccurredAt.Before(record.PublishedAt.Add(-contentAttributionClockSkew)) || event.OccurredAt.After(record.ExpiresAt) {
		return ContentEngagementResultIgnoredExpired, nil
	}
	if !record.OutcomeRecordedAt.IsZero() {
		return ContentEngagementResultReplayed, nil
	}
	bundle, err := p.traceReader.GetTraceBundle(ctx, record.AuthorUserID, record.SourceRunID)
	if err != nil {
		return "", fmt.Errorf("read attributed Agent run: %w", err)
	}
	if bundle == nil || bundle.Run == nil || bundle.Run.UserID != record.AuthorUserID ||
		strings.TrimSpace(bundle.Run.RunID) != record.SourceRunID ||
		bundle.Run.Mode != string(agentRuntime.ModeAssist) ||
		bundle.Run.Status != string(agentRuntime.RunStatusCompleted) {
		return "", fmt.Errorf("%w: source Agent run is unavailable or invalid", ErrInvalidContentEngagementAttribution)
	}
	if err := p.outcomeRecorder.RecordProductOutcome(
		ctx, *bundle.Run, profile.ExperimentOutcomeSignalContentEngaged, true,
	); err != nil {
		return "", fmt.Errorf("record content engagement product outcome: %w", err)
	}
	inserted, err := p.store.MarkOutcomeRecorded(ctx, record.TweetID, event.EventID, event.Kind, event.OccurredAt)
	if err != nil {
		return "", err
	}
	if !inserted {
		return ContentEngagementResultReplayed, nil
	}
	return ContentEngagementResultAttributed, nil
}

func IsPermanentContentEngagementError(err error) bool {
	return errors.Is(err, ErrInvalidContentEngagementAttribution)
}
