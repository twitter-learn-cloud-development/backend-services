package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"twitter-clone/internal/module/agent/attribution"
	agentObservability "twitter-clone/internal/module/agent/observability"
	"twitter-clone/internal/module/agent/profile"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

type contentAttributionStoreFake struct {
	record      *attribution.PublishedContent
	getErr      error
	saveErr     error
	markErr     error
	saved       *attribution.PublishedContent
	markCalls   int
	markedEvent string
	markedKind  string
}

func (s *contentAttributionStoreFake) SavePublishedContent(_ context.Context, record *attribution.PublishedContent) error {
	if record != nil {
		copyRecord := *record
		s.saved = &copyRecord
	}
	return s.saveErr
}

func (s *contentAttributionStoreFake) GetPublishedContent(_ context.Context, _ uint64) (*attribution.PublishedContent, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	if s.record == nil {
		return nil, attribution.ErrPublishedContentNotFound
	}
	copyRecord := *s.record
	return &copyRecord, nil
}

func (s *contentAttributionStoreFake) MarkOutcomeRecorded(
	_ context.Context, _ uint64, eventID, kind string, _ time.Time,
) (bool, error) {
	s.markCalls++
	s.markedEvent, s.markedKind = eventID, kind
	return s.markErr == nil, s.markErr
}

func completedAssistRun(now time.Time) agentObservability.RunRecord {
	return agentObservability.RunRecord{
		RecordID: "run-engagement", RunID: "run-engagement", UserID: 42,
		Mode: string(agentRuntime.ModeAssist), Status: string(agentRuntime.RunStatusCompleted),
		AgentProfileID: "assist.draft", AgentProfileVersion: "v2",
		StartedAt: now.Add(-time.Minute), FinishedAt: now.Add(-30 * time.Second), UpdatedAt: now,
	}
}

func TestContentEngagementProcessorAttributesFirstExternalInteraction(t *testing.T) {
	now := time.Now().UTC()
	store := &contentAttributionStoreFake{record: &attribution.PublishedContent{
		TweetID: 9001, AuthorUserID: 42, SourceRunID: "run-engagement",
		PublishedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
	}}
	traces := agentObservability.NewInMemoryRecorder()
	if err := traces.RecordRun(context.Background(), completedAssistRun(now)); err != nil {
		t.Fatal(err)
	}
	outcomes := &productOutcomeRecorderFake{}
	processor, err := NewContentEngagementProcessor(store, traces, outcomes)
	if err != nil {
		t.Fatal(err)
	}

	result, err := processor.Process(context.Background(), attribution.ContentEngagement{
		EventID: "like:9001:77", Kind: attribution.EngagementKindLike,
		TweetID: 9001, ActorUserID: 77, AuthorUserID: 42, OccurredAt: now,
	})
	if err != nil || result != ContentEngagementResultAttributed {
		t.Fatalf("Process() = %q, %v", result, err)
	}
	if outcomes.calls != 1 || outcomes.signal != profile.ExperimentOutcomeSignalContentEngaged || !outcomes.positive {
		t.Fatalf("outcome = %+v", outcomes)
	}
	if store.markCalls != 1 || store.markedEvent != "like:9001:77" || store.markedKind != attribution.EngagementKindLike {
		t.Fatalf("mark = %+v", store)
	}
}

func TestContentEngagementProcessorIgnoresNonAgentSelfExpiredAndReplayEvents(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name   string
		store  *contentAttributionStoreFake
		event  attribution.ContentEngagement
		result string
	}{
		{
			name: "non agent tweet", store: &contentAttributionStoreFake{getErr: attribution.ErrPublishedContentNotFound},
			event:  attribution.ContentEngagement{EventID: "comment:1", Kind: attribution.EngagementKindComment, TweetID: 1, ActorUserID: 7, AuthorUserID: 8, OccurredAt: now},
			result: ContentEngagementResultIgnoredNonAgent,
		},
		{
			name: "self interaction", store: &contentAttributionStoreFake{record: &attribution.PublishedContent{TweetID: 2, AuthorUserID: 42, SourceRunID: "run-engagement", PublishedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)}},
			event:  attribution.ContentEngagement{EventID: "like:2:42", Kind: attribution.EngagementKindLike, TweetID: 2, ActorUserID: 42, AuthorUserID: 42, OccurredAt: now},
			result: ContentEngagementResultIgnoredSelf,
		},
		{
			name: "expired interaction", store: &contentAttributionStoreFake{record: &attribution.PublishedContent{TweetID: 3, AuthorUserID: 42, SourceRunID: "run-engagement", PublishedAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(-time.Hour)}},
			event:  attribution.ContentEngagement{EventID: "comment:3", Kind: attribution.EngagementKindComment, TweetID: 3, ActorUserID: 77, AuthorUserID: 42, OccurredAt: now},
			result: ContentEngagementResultIgnoredExpired,
		},
		{
			name: "already attributed replay", store: &contentAttributionStoreFake{record: &attribution.PublishedContent{TweetID: 4, AuthorUserID: 42, SourceRunID: "run-engagement", PublishedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), OutcomeRecordedAt: now.Add(-time.Minute)}},
			event:  attribution.ContentEngagement{EventID: "like:4:77", Kind: attribution.EngagementKindLike, TweetID: 4, ActorUserID: 77, AuthorUserID: 42, OccurredAt: now},
			result: ContentEngagementResultReplayed,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			processor := &ContentEngagementProcessor{store: test.store, traceReader: agentObservability.NewInMemoryRecorder(), outcomeRecorder: &productOutcomeRecorderFake{}}
			result, err := processor.Process(context.Background(), test.event)
			if err != nil || result != test.result {
				t.Fatalf("Process() = %q, %v", result, err)
			}
			if test.store.markCalls != 0 {
				t.Fatalf("mark calls = %d", test.store.markCalls)
			}
		})
	}
}

func TestContentEngagementProcessorClassifiesCorruptAttributionAsPermanent(t *testing.T) {
	now := time.Now().UTC()
	store := &contentAttributionStoreFake{record: &attribution.PublishedContent{
		TweetID: 4, AuthorUserID: 42, SourceRunID: "run-engagement",
		PublishedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
	}}
	processor := &ContentEngagementProcessor{store: store, traceReader: agentObservability.NewInMemoryRecorder(), outcomeRecorder: &productOutcomeRecorderFake{}}
	_, err := processor.Process(context.Background(), attribution.ContentEngagement{
		EventID: "like:4:77", Kind: attribution.EngagementKindLike,
		TweetID: 4, ActorUserID: 77, AuthorUserID: 99, OccurredAt: now,
	})
	if !errors.Is(err, ErrInvalidContentEngagementAttribution) || !IsPermanentContentEngagementError(err) {
		t.Fatalf("Process() error = %v", err)
	}
}

func TestConfirmPublishTwitterPersistsBoundedContentAttribution(t *testing.T) {
	now := time.Now().UTC()
	traces := agentObservability.NewInMemoryRecorder()
	if err := traces.RecordRun(context.Background(), completedAssistRun(now)); err != nil {
		t.Fatal(err)
	}
	store := &contentAttributionStoreFake{}
	service := &AgentService{
		traceReader: traces, confirmedDraftPublisher: &confirmedDraftPublisherFake{tweetID: 9010},
		contentAttributionStore: store, contentAttributionWindow: 2 * time.Hour,
	}
	result, err := service.ConfirmPublishTwitter(context.Background(), 42, "run-engagement", "final draft")
	if err != nil || result.TweetID != 9010 {
		t.Fatalf("ConfirmPublishTwitter() = %+v, %v", result, err)
	}
	if store.saved == nil || store.saved.TweetID != 9010 || store.saved.SourceRunID != "run-engagement" ||
		store.saved.AuthorUserID != 42 || store.saved.ExpiresAt.Sub(store.saved.PublishedAt) != 2*time.Hour {
		t.Fatalf("saved attribution = %+v", store.saved)
	}
}
