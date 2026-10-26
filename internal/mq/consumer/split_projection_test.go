package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"twitter-clone/internal/domain"
	"twitter-clone/internal/events"
	"twitter-clone/pkg/es"
)

func splitPayload(id uint64) domain.ProjectionTaskPayload {
	return domain.ProjectionTaskPayload{TweetID: id, AuthorID: 42, SchemaVersion: domain.ProjectionVersion, Embedding: domain.EmbeddingProfile{Model: "fixture-model", Dimension: 2, Version: "v1"}}
}
func splitTask(t *testing.T, id uint64, kind string, payload domain.ProjectionTaskPayload) *domain.OutboxTask {
	t.Helper()
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	return &domain.OutboxTask{ID: id, TaskType: kind, Payload: string(body), Status: domain.OutboxStatusProcessing, MaxRetries: 5}
}

func TestSplitProjectionTextCompletesDuringBlockedEmbedding(t *testing.T) {
	payload := splitPayload(17)
	fact := &projectionFactFake{current: &domain.Tweet{ID: 17, UserID: 42, Content: "current"}}
	started, release := make(chan struct{}), make(chan struct{})
	c := &TimelineConsumer{projectionRepo: fact, embeddingProfile: payload.Embedding,
		embeddingExecutor: func(context.Context, string, string) ([]float32, error) {
			close(started)
			<-release
			return nil, errors.New("fixture embedding outage")
		},
		textProjectionExecutor: func(_ context.Context, doc es.TweetDocument) error {
			require.Equal(t, "Document: current", doc.Content)
			require.Equal(t, domain.ProjectionDigest(fact.current), doc.ProjectionDigest)
			require.Nil(t, doc.ContentVector)
			return nil
		},
	}
	vectorDone := make(chan error, 1)
	go func() { vectorDone <- c.reconcileVectorProjection(context.Background(), payload) }()
	<-started
	textDone := make(chan error, 1)
	go func() { textDone <- c.reconcileTextProjection(context.Background(), payload) }()
	select {
	case err := <-textDone:
		require.NoError(t, err)
	case <-time.After(time.Second):
		close(release)
		t.Fatal("text held behind embedding lock")
	}
	close(release)
	require.ErrorContains(t, <-vectorDone, "embedding execution failed")
}

func TestSplitProjectionRejectsProfileOrFactDriftAndRepairCleansAliases(t *testing.T) {
	for _, kind := range []string{"profile", "dimension", "deleted_during_embedding", "content_changed", "repair"} {
		t.Run(kind, func(t *testing.T) {
			payload := splitPayload(17)
			fact := &projectionFactFake{current: &domain.Tweet{ID: 17, UserID: 42, Content: "current"}}
			operations := []string{}
			c := &TimelineConsumer{projectionRepo: fact, embeddingProfile: payload.Embedding,
				deleteVectorExecutor: func(context.Context, uint64) error { operations = append(operations, "delete"); return nil },
				vectorProjectionExecutor: func(_ context.Context, id uint64, vector []float32, metadata map[string]interface{}) error {
					operations = append(operations, "write")
					require.Equal(t, "Document: current", metadata["content"])
					require.Equal(t, payload.Embedding.Digest(), metadata["embedding_profile"])
					return nil
				},
			}
			c.embeddingExecutor = func(context.Context, string, string) ([]float32, error) {
				operations = append(operations, "embed")
				switch kind {
				case "dimension":
					return []float32{1}, nil
				case "deleted_during_embedding":
					fact.set(&domain.Tweet{ID: 17, UserID: 42, DeletedAt: 1})
				case "content_changed":
					fact.set(&domain.Tweet{ID: 17, UserID: 42, Content: "new"})
				}
				return []float32{1, 2}, nil
			}
			if kind == "profile" {
				payload.Embedding.Version = "v2"
			}
			if kind == "repair" {
				payload.RepairOperation = "fixture-repair"
			}
			err := c.reconcileVectorProjection(context.Background(), payload)
			switch kind {
			case "profile":
				require.Error(t, err)
				require.Empty(t, operations)
			case "dimension", "content_changed":
				require.Error(t, err)
				require.Equal(t, []string{"embed"}, operations)
			case "deleted_during_embedding":
				require.NoError(t, err)
				require.Equal(t, []string{"embed", "delete"}, operations)
			case "repair":
				require.NoError(t, err)
				require.Equal(t, []string{"embed", "delete", "write"}, operations)
			}
		})
	}
}

type splitEnqueueFake struct {
	tweetCreatedOutboxRepositoryFake
	failVector bool
}

func (f *splitEnqueueFake) CreateIdempotent(ctx context.Context, task *domain.OutboxTask) (bool, error) {
	if f.failVector && task.TaskType == domain.ProjectionSyncVector {
		return false, errors.New("fixture vector registration failure")
	}
	return f.tweetCreatedOutboxRepositoryFake.CreateIdempotent(ctx, task)
}
func TestSplitProjectionRegistersLegacyChildrenBeforeParentCompletes(t *testing.T) {
	repo := &splitEnqueueFake{failVector: true}
	id := uint64(100)
	c := &TimelineConsumer{outboxRepo: repo, newOutboxTaskID: func() (uint64, error) { id++; return id, nil }, embeddingProfile: splitPayload(17).Embedding}
	legacy := newSearchSyncOutboxTask(t, 90, 17)
	event := &events.TweetCreatedEvent{TweetID: 17, AuthorID: 42, Content: "private old event body"}
	require.Error(t, c.executeProjectionTask(context.Background(), legacy, event))
	require.Len(t, repo.byDedupKey, 1)
	repo.failVector = false
	require.NoError(t, c.executeProjectionTask(context.Background(), legacy, event))
	require.Len(t, repo.byDedupKey, 2)
	require.NoError(t, c.executeProjectionTask(context.Background(), legacy, event))
	require.Len(t, repo.byDedupKey, 2)
	for _, task := range repo.byDedupKey {
		require.NotContains(t, task.Payload, event.Content)
	}
}

type laneRepositoryFake struct {
	outboxWorkerRepositoryFake
	muLane        sync.Mutex
	vectorStarted chan struct{}
	release       chan struct{}
	textClaimed   chan struct{}
}

func (r *laneRepositoryFake) Claim(ctx context.Context, request domain.OutboxClaimRequest) ([]*domain.OutboxTask, error) {
	if len(request.TaskTypes) > 0 && request.TaskTypes[0] == domain.ProjectionSyncVector {
		close(r.vectorStarted)
		select {
		case <-r.release:
		case <-ctx.Done():
		}
		return nil, nil
	}
	close(r.textClaimed)
	return nil, nil
}
func TestProjectionLaneClaimsReserveTextCapacity(t *testing.T) {
	repo := &laneRepositoryFake{vectorStarted: make(chan struct{}), release: make(chan struct{}), textClaimed: make(chan struct{})}
	c := newOutboxTestConsumer(repo, nil)
	vectorDone := make(chan struct{})
	go func() {
		c.processProjectionLane(context.Background(), []string{domain.ProjectionSyncVector})
		close(vectorDone)
	}()
	<-repo.vectorStarted
	textDone := make(chan struct{})
	go func() {
		c.processProjectionLane(context.Background(), []string{domain.ProjectionSyncText})
		close(textDone)
	}()
	select {
	case <-repo.textClaimed:
	case <-time.After(time.Second):
		close(repo.release)
		t.Fatal("text claim blocked by vector lane")
	}
	<-textDone
	close(repo.release)
	<-vectorDone
}

func TestProjectionMetricsClampLabelsAndKeepBacklogEvidence(t *testing.T) {
	registry := prometheus.NewRegistry()
	observer, err := NewPrometheusProjectionObserver(registry)
	require.NoError(t, err)
	observer.ObserveProjection("raw-user-id", "private provider failure", time.Second)
	observer.SetProjectionBacklog([]domain.ProjectionBacklog{{TaskType: domain.ProjectionSyncVector, State: "exhausted", Count: 2, OldestAt: 1000}}, time.UnixMilli(10000))
	observer.ObserveBacklogFailure()
	families, err := registry.Gather()
	require.NoError(t, err)
	found := false
	for _, family := range families {
		for _, metric := range family.Metric {
			for _, label := range metric.Label {
				require.NotContains(t, label.GetValue(), "raw-user-id")
				require.NotContains(t, label.GetValue(), "private provider failure")
			}
			if family.GetName() == "tweet_projection_backlog" && metric.GetGauge().GetValue() == 2 {
				found = true
			}
		}
	}
	require.True(t, found)
}
