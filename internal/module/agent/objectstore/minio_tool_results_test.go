package objectstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	workflowTool "twitter-clone/internal/module/agent/workflow/tool"
)

type toolResultBackendFake struct {
	exists      bool
	hasPolicy   bool
	makeCalls   int
	putBucket   string
	putKey      string
	putPayload  []byte
	getPayload  []byte
	deleteCalls int
	deletedKeys []string
	listObjects []toolResultObjectMetadata
	listCursor  string
	listPrefix  string
}

func (b *toolResultBackendFake) BucketExists(context.Context, string) (bool, error) {
	return b.exists, nil
}

func (b *toolResultBackendFake) MakeBucket(context.Context, string) error {
	b.makeCalls++
	b.exists = true
	return nil
}

func (b *toolResultBackendFake) BucketHasPolicy(context.Context, string) (bool, error) {
	return b.hasPolicy, nil
}

func (b *toolResultBackendFake) Put(_ context.Context, bucket, key, _ string, payload []byte) error {
	b.putBucket = bucket
	b.putKey = key
	b.putPayload = append([]byte(nil), payload...)
	return nil
}

func (b *toolResultBackendFake) Get(context.Context, string, string, int) ([]byte, error) {
	if b.getPayload == nil {
		return nil, errors.New("not found")
	}
	return append([]byte(nil), b.getPayload...), nil
}

func (b *toolResultBackendFake) Delete(_ context.Context, _ string, key string) error {
	b.deleteCalls++
	b.deletedKeys = append(b.deletedKeys, key)
	for index, object := range b.listObjects {
		if object.Key == key {
			b.listObjects = append(b.listObjects[:index], b.listObjects[index+1:]...)
			break
		}
	}
	return nil
}

func (b *toolResultBackendFake) List(_ context.Context, _ string, prefix, _ string, _ int) ([]toolResultObjectMetadata, string, error) {
	b.listPrefix = prefix
	return b.listObjects, b.listCursor, nil
}

func TestMinIOToolResultStoreCreatesPrivateBucketAndUsesHashedKey(t *testing.T) {
	backend := &toolResultBackendFake{}
	store := newMinIOToolResultStore(backend, "agent-tool-results")
	require.NoError(t, store.EnsureBucket(context.Background()))
	require.Equal(t, 1, backend.makeCalls)
	payload := []byte(`{"results":["one","two"]}`)
	sum := sha256.Sum256(payload)
	request := workflowTool.ResultArchiveRequest{
		UserID: 42, ResultID: "execution-1",
		Snapshot: workflowTool.ResultSnapshot{Payload: payload, Digest: hex.EncodeToString(sum[:]), Length: len(payload)},
	}

	reference, err := store.Put(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, "minio://agent-tool-results/"+backend.putKey, reference.URI())
	require.NotContains(t, backend.putKey, "42")
	require.NotContains(t, backend.putKey, "execution-1")
	require.Equal(t, payload, backend.putPayload)
	require.Equal(t, "agent-tool-results", backend.putBucket)
}

func TestMinIOToolResultStoreRejectsCrossBucketReference(t *testing.T) {
	backend := &toolResultBackendFake{getPayload: []byte(`{}`)}
	store := newMinIOToolResultStore(backend, "agent-tool-results")
	reference := workflowTool.ResultReference{
		Storage: "minio", Bucket: "twitter-media", Key: "tool-results/a/b/c.json",
		Digest: hex.EncodeToString(make([]byte, sha256.Size)), Length: 2, ContentType: toolResultContentType,
	}

	_, err := store.Get(context.Background(), reference, 1024)
	require.ErrorContains(t, err, "unsupported object store")
}

func TestMinIOToolResultStoreRejectsBucketWithAnonymousPolicy(t *testing.T) {
	store := newMinIOToolResultStore(&toolResultBackendFake{exists: true, hasPolicy: true}, "agent-tool-results")
	require.ErrorContains(t, store.EnsureBucket(context.Background()), "policy")
}

func TestMinIOToolResultStoreReadsAndDeletesOwnedReference(t *testing.T) {
	payload := []byte(`{"ok":true}`)
	sum := sha256.Sum256(payload)
	backend := &toolResultBackendFake{getPayload: payload}
	store := newMinIOToolResultStore(backend, "agent-tool-results")
	request := workflowTool.ResultArchiveRequest{
		UserID: 42, ResultID: "execution-1",
		Snapshot: workflowTool.ResultSnapshot{Payload: payload, Digest: hex.EncodeToString(sum[:]), Length: len(payload)},
	}
	reference := workflowTool.ResultReference{
		Storage: "minio", Bucket: "agent-tool-results", Key: toolResultObjectKey(request),
		Digest: hex.EncodeToString(sum[:]), Length: len(payload), ContentType: toolResultContentType,
	}

	actual, err := store.Get(context.Background(), reference, 1024)
	require.NoError(t, err)
	require.Equal(t, payload, actual)
	require.NoError(t, store.Delete(context.Background(), reference))
	require.Equal(t, 1, backend.deleteCalls)
}

func TestMinIOToolResultStoreRejectsCorruptObject(t *testing.T) {
	payload := []byte(`{"ok":true}`)
	sum := sha256.Sum256(payload)
	request := workflowTool.ResultArchiveRequest{
		UserID: 42, ResultID: "execution-1",
		Snapshot: workflowTool.ResultSnapshot{Payload: payload, Digest: hex.EncodeToString(sum[:]), Length: len(payload)},
	}
	reference := workflowTool.ResultReference{
		Storage: "minio", Bucket: "agent-tool-results", Key: toolResultObjectKey(request),
		Digest: request.Snapshot.Digest, Length: len(payload), ContentType: toolResultContentType,
	}
	store := newMinIOToolResultStore(&toolResultBackendFake{getPayload: []byte(`{"ok":false}`)}, "agent-tool-results")

	_, err := store.Get(context.Background(), reference, 1024)
	require.ErrorContains(t, err, "mismatch")
}

func TestMinIOToolResultStoreRejectsOversizedArchive(t *testing.T) {
	payload := make([]byte, maxToolResultArchiveBytes+1)
	sum := sha256.Sum256(payload)
	store := newMinIOToolResultStore(&toolResultBackendFake{}, "agent-tool-results")
	_, err := store.Put(context.Background(), workflowTool.ResultArchiveRequest{
		UserID: 42, ResultID: "execution-1",
		Snapshot: workflowTool.ResultSnapshot{Payload: payload, Digest: hex.EncodeToString(sum[:]), Length: len(payload)},
	})
	require.ErrorContains(t, err, "exceeds")
}

func TestNewMinIOToolResultStoreValidatesPrivateBucketConfig(t *testing.T) {
	_, err := NewMinIOToolResultStore(MinIOToolResultConfig{
		Endpoint: "localhost:9000", AccessKey: "key", SecretKey: "secret", Bucket: "Twitter_Media",
	})
	require.ErrorContains(t, err, "bucket name")
}

func TestMinIOToolResultStoreListsBoundedRedactedInventory(t *testing.T) {
	payload := []byte(`{"ok":true}`)
	sum := sha256.Sum256(payload)
	request := workflowTool.ResultArchiveRequest{
		UserID: 42, ResultID: "execution-1",
		Snapshot: workflowTool.ResultSnapshot{Payload: payload, Digest: hex.EncodeToString(sum[:]), Length: len(payload)},
	}
	key := toolResultObjectKey(request)
	modified := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	backend := &toolResultBackendFake{
		listObjects: []toolResultObjectMetadata{{Key: key, LastModified: modified, Size: int64(len(payload))}},
		listCursor:  key,
	}
	store := newMinIOToolResultStore(backend, "agent-tool-results")
	items, cursor, err := store.ListToolResultObjects(context.Background(), "", 10)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.True(t, items[0].ValidOwnedKey)
	require.Equal(t, key, items[0].Key)
	require.Equal(t, DataRetentionTenantHashForTest(42), items[0].TenantHash)
	require.Equal(t, request.Snapshot.Digest, items[0].ObjectDigest)
	require.Len(t, items[0].KeyDigest, sha256.Size*2)
	require.Equal(t, key, cursor)
}

func DataRetentionTenantHashForTest(userID uint64) string {
	digest := sha256.Sum256([]byte("tenant:" + strconv.FormatUint(userID, 10)))
	return hex.EncodeToString(digest[:8])
}

func TestSubjectToolResultDeletionUsesTenantPrefixAndVerifiesEmpty(t *testing.T) {
	payload := []byte(`{"ok":true}`)
	sum := sha256.Sum256(payload)
	request := workflowTool.ResultArchiveRequest{
		UserID: 42, ResultID: "execution-1",
		Snapshot: workflowTool.ResultSnapshot{Payload: payload, Digest: hex.EncodeToString(sum[:]), Length: len(payload)},
	}
	key := toolResultObjectKey(request)
	backend := &toolResultBackendFake{listObjects: []toolResultObjectMetadata{{Key: key, Size: int64(len(payload))}}}
	store := newMinIOToolResultStore(backend, "agent-tool-results")

	count, err := store.CountSubjectToolResults(t.Context(), 42, 100)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	result, err := store.DeleteSubjectToolResults(t.Context(), 42, 100)
	require.NoError(t, err)
	require.EqualValues(t, 1, result.DeletedCount)
	require.Zero(t, result.RemainingCount)
	require.Equal(t, []string{key}, backend.deletedKeys)
	require.Equal(t, toolResultSubjectPrefix(42), backend.listPrefix)
}

func TestSubjectToolResultDeletionRejectsCrossTenantListingBeforeDelete(t *testing.T) {
	payload := []byte(`{"ok":true}`)
	sum := sha256.Sum256(payload)
	foreign := toolResultObjectKey(workflowTool.ResultArchiveRequest{
		UserID: 7, ResultID: "foreign",
		Snapshot: workflowTool.ResultSnapshot{Payload: payload, Digest: hex.EncodeToString(sum[:]), Length: len(payload)},
	})
	backend := &toolResultBackendFake{listObjects: []toolResultObjectMetadata{{Key: foreign}}}
	store := newMinIOToolResultStore(backend, "agent-tool-results")

	_, err := store.DeleteSubjectToolResults(t.Context(), 42, 100)
	require.ErrorContains(t, err, "outside the subject deletion scope")
	require.Zero(t, backend.deleteCalls)
}
