package service

import (
	"context"
	"errors"
	"testing"
	"time"

	agentObservability "twitter-clone/internal/module/agent/observability"
	"twitter-clone/internal/module/agent/profile"
	"twitter-clone/internal/module/agent/repository"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
	agentStrategy "twitter-clone/internal/module/agent/strategy"
)

type confirmedDraftPublisherFake struct {
	calls          int
	userID         uint64
	content        string
	idempotencyKey string
	tweetID        uint64
	err            error
}

func (p *confirmedDraftPublisherFake) PublishConfirmedDraft(
	_ context.Context,
	userID uint64,
	content, idempotencyKey string,
) (uint64, error) {
	p.calls++
	p.userID, p.content, p.idempotencyKey = userID, content, idempotencyKey
	return p.tweetID, p.err
}

type productOutcomeRecorderFake struct {
	calls    int
	run      agentObservability.RunRecord
	signal   string
	positive bool
	err      error
}

func (r *productOutcomeRecorderFake) RecordProductOutcome(
	_ context.Context,
	run agentObservability.RunRecord,
	signal string,
	positive bool,
) error {
	r.calls++
	r.run, r.signal, r.positive = run, signal, positive
	return r.err
}

func TestConfirmPublishTwitterValidatesRunAndRecordsProductOutcome(t *testing.T) {
	traces := agentObservability.NewInMemoryRecorder()
	run := agentObservability.RunRecord{
		RecordID: "run-1", RunID: "run-1", UserID: 42,
		Mode: string(agentRuntime.ModeAssist), Status: string(agentRuntime.RunStatusCompleted),
		AgentProfileID: "assist.draft", AgentProfileVersion: "v2",
		StartedAt: time.Now().Add(-time.Second), FinishedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := traces.RecordRun(context.Background(), run); err != nil {
		t.Fatalf("RecordRun() error = %v", err)
	}
	publisher := &confirmedDraftPublisherFake{tweetID: 9001}
	outcomes := &productOutcomeRecorderFake{}
	service := &AgentService{
		traceReader: traces, confirmedDraftPublisher: publisher, productOutcomeRecorder: outcomes,
	}

	result, err := service.ConfirmPublishTwitter(context.Background(), 42, "run-1", " publish me ")
	if err != nil {
		t.Fatalf("ConfirmPublishTwitter() error = %v", err)
	}
	if result.TweetID != 9001 || publisher.calls != 1 || publisher.userID != 42 || publisher.content != "publish me" {
		t.Fatalf("result/publisher = %+v/%+v", result, publisher)
	}
	wantKey := confirmedDraftIdempotencyKey("run-1", "publish me")
	if publisher.idempotencyKey != wantKey {
		t.Fatalf("idempotency key = %q, want %q", publisher.idempotencyKey, wantKey)
	}
	if outcomes.calls != 1 || outcomes.run.RunID != "run-1" ||
		outcomes.signal != profile.ExperimentOutcomeSignalDraftPublished || !outcomes.positive {
		t.Fatalf("product outcome = %+v", outcomes)
	}
}

func TestConfirmPublishTwitterRejectsForeignOrNonAssistRunBeforePublishing(t *testing.T) {
	traces := agentObservability.NewInMemoryRecorder()
	if err := traces.RecordRun(context.Background(), agentObservability.RunRecord{
		RecordID: "chat-run", RunID: "chat-run", UserID: 42,
		Mode: string(agentRuntime.ModeChat), Status: string(agentRuntime.RunStatusCompleted),
		StartedAt: time.Now(), FinishedAt: time.Now(), UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("RecordRun() error = %v", err)
	}
	publisher := &confirmedDraftPublisherFake{tweetID: 9001}
	service := &AgentService{traceReader: traces, confirmedDraftPublisher: publisher}

	_, err := service.ConfirmPublishTwitter(context.Background(), 42, "chat-run", "do not publish")
	if !errors.Is(err, ErrInvalidDraftSourceRun) {
		t.Fatalf("ConfirmPublishTwitter() error = %v, want invalid source", err)
	}
	if publisher.calls != 0 {
		t.Fatalf("publisher calls = %d, want 0", publisher.calls)
	}
}

func TestConfirmPublishTwitterKeepsSuccessfulPublishWhenAttributionFails(t *testing.T) {
	traces := agentObservability.NewInMemoryRecorder()
	run := agentObservability.RunRecord{
		RecordID: "run-2", RunID: "run-2", UserID: 42,
		Mode: string(agentRuntime.ModeAssist), Status: string(agentRuntime.RunStatusCompleted),
		StartedAt: time.Now(), FinishedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := traces.RecordRun(context.Background(), run); err != nil {
		t.Fatalf("RecordRun() error = %v", err)
	}
	service := &AgentService{
		traceReader:             traces,
		confirmedDraftPublisher: &confirmedDraftPublisherFake{tweetID: 9002},
		productOutcomeRecorder:  &productOutcomeRecorderFake{err: errors.New("measurement unavailable")},
	}

	result, err := service.ConfirmPublishTwitter(context.Background(), 42, "run-2", "published once")
	if err != nil || result.TweetID != 9002 {
		t.Fatalf("ConfirmPublishTwitter() = %+v, %v", result, err)
	}
}

func TestConfirmPublishTwitterAttributesAuthoritativeDraftOnlyOnce(t *testing.T) {
	traces := agentObservability.NewInMemoryRecorder()
	if err := traces.RecordRun(context.Background(), agentObservability.RunRecord{
		RecordID: "run-draft-1", RunID: "run-draft-1", UserID: 42,
		Mode: string(agentRuntime.ModeAssist), Status: string(agentRuntime.RunStatusCompleted),
		StartedAt: time.Now().Add(-time.Second), FinishedAt: time.Now(), UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	strategyPlan := agentStrategy.Plan{SelectedStrategy: agentStrategy.KindSingleAgent}
	runStore := &memoryAgentExecutionRunStore{run: &repository.AgentExecutionRun{
		ID: "run-draft-1", UserID: 42, Status: repository.AgentExecutionRunCompleted,
		ExecutionProfile: ExecutionProfileRuntimeDraft, ExecutionStrategyPlan: &strategyPlan,
		PublishableDraft: true, Revision: 2,
	}}
	productEvents := &memoryProductEventStore{}
	observer := &recordingUnifiedAgentProductObserver{}
	service := &AgentService{
		traceReader: traces, confirmedDraftPublisher: &confirmedDraftPublisherFake{tweetID: 9003},
		agentExecutionRunStore: runStore, productEventStore: productEvents,
		unifiedAgentProductObserver: observer,
	}

	for range 2 {
		result, err := service.ConfirmPublishTwitter(
			context.Background(), 42, "run-draft-1", "publish once",
		)
		if err != nil || result.TweetID != 9003 {
			t.Fatalf("ConfirmPublishTwitter() = %+v, %v", result, err)
		}
	}
	if runStore.run.PublishedTweetID != 9003 || runStore.draftPublishCalls != 2 {
		t.Fatalf("draft adoption run=%+v calls=%d", runStore.run, runStore.draftPublishCalls)
	}
	if len(observer.published) != 1 {
		t.Fatalf("published observations = %d, want 1", len(observer.published))
	}
	if productEvents.count("draft_published") != 1 {
		t.Fatalf("persisted draft-published events = %d, want 1", productEvents.count("draft_published"))
	}
}
