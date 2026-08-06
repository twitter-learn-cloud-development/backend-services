package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"twitter-clone/internal/module/agent/repository"
	workflowTool "twitter-clone/internal/module/agent/workflow/tool"
)

type toolResultRepositoryFake struct {
	record       *repository.ToolExecutionRecord
	acquired     bool
	claimErr     error
	completed    repository.ToolExecutionResult
	completeErr  error
	failedReason string
}

func (r *toolResultRepositoryFake) ClaimToolExecution(_ context.Context, record *repository.ToolExecutionRecord) (*repository.ToolExecutionRecord, bool, error) {
	if r.claimErr != nil {
		return nil, false, r.claimErr
	}
	if r.record == nil {
		copy := *record
		copy.ID = primitive.NewObjectID()
		copy.Status = repository.ToolExecutionStatusExecuting
		r.record = &copy
		r.acquired = true
	}
	copy := *r.record
	return &copy, r.acquired, nil
}

func (r *toolResultRepositoryFake) CompleteToolExecution(_ context.Context, _ primitive.ObjectID, _ string, result repository.ToolExecutionResult) error {
	r.completed = result
	return r.completeErr
}

func (r *toolResultRepositoryFake) FailToolExecution(_ context.Context, _ primitive.ObjectID, _ string, reason string) error {
	r.failedReason = reason
	return nil
}

type toolResultObjectStoreFake struct {
	payloads map[string][]byte
	puts     int
	deletes  int
}

func newToolResultObjectStoreFake() *toolResultObjectStoreFake {
	return &toolResultObjectStoreFake{payloads: make(map[string][]byte)}
}

func (s *toolResultObjectStoreFake) Put(_ context.Context, request workflowTool.ResultArchiveRequest) (*workflowTool.ResultReference, error) {
	s.puts++
	key := "tool-results/tenant/result/" + request.Snapshot.Digest + ".json"
	s.payloads[key] = append([]byte(nil), request.Snapshot.Payload...)
	return &workflowTool.ResultReference{
		Storage: "minio", Bucket: "agent-tool-results", Key: key,
		Digest: request.Snapshot.Digest, Length: request.Snapshot.Length, ContentType: "application/json",
	}, nil
}

func (s *toolResultObjectStoreFake) Get(_ context.Context, reference workflowTool.ResultReference, maxBytes int) ([]byte, error) {
	payload, ok := s.payloads[reference.Key]
	if !ok {
		return nil, errors.New("not found")
	}
	if len(payload) > maxBytes {
		return nil, workflowTool.ErrResultTooLarge
	}
	return append([]byte(nil), payload...), nil
}

func (s *toolResultObjectStoreFake) Delete(_ context.Context, reference workflowTool.ResultReference) error {
	s.deletes++
	delete(s.payloads, reference.Key)
	return nil
}

func TestPersistentToolResultStoreKeepsSmallResultInline(t *testing.T) {
	repo := &toolResultRepositoryFake{}
	objects := newToolResultObjectStoreFake()
	store := NewPersistentToolResultStore(repo, WithToolResultObjectStore(objects), WithToolResultLimits(1024, 4096))
	claim, err := store.Claim(context.Background(), workflowTool.IdempotencyCheck{
		ToolName: "PublishTweet", UserID: 7, IdempotencyKey: "run:publish", InputDigest: "input", LeaseUntil: time.Now().Add(time.Minute),
	})
	require.NoError(t, err)
	snapshot := toolResultSnapshot(t, map[string]interface{}{"tweet_id": 42})
	reference, err := store.Archive(context.Background(), workflowTool.ResultArchiveRequest{UserID: 7, ResultID: claim.ExecutionID, Snapshot: snapshot})
	require.NoError(t, err)
	require.Nil(t, reference)
	require.NoError(t, store.Complete(context.Background(), claim, snapshot))
	require.Equal(t, 42, repo.completed.Output["tweet_id"])
	require.Nil(t, repo.completed.OutputReference)
	require.Zero(t, objects.puts)
}

func TestPersistentToolResultStoreArchivesLargeResultAndPersistsReference(t *testing.T) {
	repo := &toolResultRepositoryFake{}
	objects := newToolResultObjectStoreFake()
	store := NewPersistentToolResultStore(repo, WithToolResultObjectStore(objects), WithToolResultLimits(16, 4096))
	claim, err := store.Claim(context.Background(), workflowTool.IdempotencyCheck{
		ToolName: "SearchTweets", UserID: 7, IdempotencyKey: "run:search", InputDigest: "input", LeaseUntil: time.Now().Add(time.Minute),
	})
	require.NoError(t, err)
	snapshot := toolResultSnapshot(t, map[string]interface{}{"results": "a result larger than the inline threshold"})
	reference, err := store.Archive(context.Background(), workflowTool.ResultArchiveRequest{UserID: 7, ResultID: claim.ExecutionID, Snapshot: snapshot})
	require.NoError(t, err)
	require.NotNil(t, reference)
	snapshot.Reference = reference
	require.NoError(t, store.Complete(context.Background(), claim, snapshot))
	require.Nil(t, repo.completed.Output)
	require.Equal(t, reference.Key, repo.completed.OutputReference.Key)
	require.Equal(t, snapshot.Digest, repo.completed.Digest)
	require.Equal(t, 1, objects.puts)
}

func TestPersistentToolResultStoreRehydratesAndVerifiesArchivedReplay(t *testing.T) {
	objects := newToolResultObjectStoreFake()
	snapshot := toolResultSnapshot(t, map[string]interface{}{"results": []interface{}{"one", "two"}})
	reference := workflowTool.ResultReference{
		Storage: "minio", Bucket: "agent-tool-results", Key: "tool-results/tenant/result/replay.json",
		Digest: snapshot.Digest, Length: snapshot.Length, ContentType: "application/json",
	}
	objects.payloads[reference.Key] = snapshot.Payload
	repo := &toolResultRepositoryFake{record: &repository.ToolExecutionRecord{
		ID: primitive.NewObjectID(), UserID: 7, ToolName: "SearchTweets", Status: repository.ToolExecutionStatusSucceeded,
		OutputReference: toolResultReferenceToRepository(&reference),
	}}
	store := NewPersistentToolResultStore(repo, WithToolResultObjectStore(objects), WithToolResultLimits(16, 4096))

	claim, err := store.Claim(context.Background(), workflowTool.IdempotencyCheck{ToolName: "SearchTweets", UserID: 7})
	require.NoError(t, err)
	require.True(t, claim.Replayed)
	require.Equal(t, []interface{}{"one", "two"}, claim.Outputs["results"])
	require.Equal(t, reference.URI(), claim.OutputReference.URI())
}

func TestPersistentToolResultStoreRejectsCorruptArchivedReplay(t *testing.T) {
	objects := newToolResultObjectStoreFake()
	reference := workflowTool.ResultReference{
		Storage: "minio", Bucket: "agent-tool-results", Key: "tool-results/tenant/result/replay.json",
		Digest: hex.EncodeToString(make([]byte, sha256.Size)), Length: 4, ContentType: "application/json",
	}
	objects.payloads[reference.Key] = []byte("nope")
	repo := &toolResultRepositoryFake{record: &repository.ToolExecutionRecord{
		ID: primitive.NewObjectID(), UserID: 7, Status: repository.ToolExecutionStatusSucceeded,
		OutputReference: toolResultReferenceToRepository(&reference),
	}}
	store := NewPersistentToolResultStore(repo, WithToolResultObjectStore(objects), WithToolResultLimits(16, 4096))

	_, err := store.Claim(context.Background(), workflowTool.IdempotencyCheck{ToolName: "SearchTweets", UserID: 7})
	require.ErrorIs(t, err, ErrToolResultCorrupt)
}

func TestPersistentToolResultStoreFailsClosedWithoutObjectStore(t *testing.T) {
	store := NewPersistentToolResultStore(&toolResultRepositoryFake{}, WithToolResultLimits(16, 4096))
	snapshot := toolResultSnapshot(t, map[string]interface{}{"results": "larger than inline"})

	_, err := store.Archive(context.Background(), workflowTool.ResultArchiveRequest{UserID: 7, ResultID: "result", Snapshot: snapshot})
	require.ErrorIs(t, err, ErrToolResultObjectStoreUnavailable)
}

func toolResultSnapshot(t *testing.T, outputs map[string]interface{}) workflowTool.ResultSnapshot {
	t.Helper()
	payload, err := json.Marshal(outputs)
	require.NoError(t, err)
	sum := sha256.Sum256(payload)
	return workflowTool.ResultSnapshot{
		Outputs: outputs, Payload: payload, Digest: hex.EncodeToString(sum[:]), Length: len(payload),
	}
}
