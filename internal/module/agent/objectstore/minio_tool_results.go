package objectstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	agentService "twitter-clone/internal/module/agent/service"
	workflowTool "twitter-clone/internal/module/agent/workflow/tool"
)

const (
	toolResultContentType      = "application/json"
	maxToolResultArchiveBytes  = 8 << 20
	maxToolResultIdentityBytes = 256
)

type MinIOToolResultConfig struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Bucket    string
	Secure    bool
}

type toolResultBackend interface {
	BucketExists(ctx context.Context, bucket string) (bool, error)
	MakeBucket(ctx context.Context, bucket string) error
	BucketHasPolicy(ctx context.Context, bucket string) (bool, error)
	Put(ctx context.Context, bucket, key, contentType string, payload []byte) error
	Get(ctx context.Context, bucket, key string, maxBytes int) ([]byte, error)
	Delete(ctx context.Context, bucket, key string) error
	List(ctx context.Context, bucket, prefix, cursor string, limit int) ([]toolResultObjectMetadata, string, error)
}

type toolResultObjectMetadata struct {
	Key          string
	LastModified time.Time
	Size         int64
}

type MinIOToolResultStore struct {
	backend toolResultBackend
	bucket  string
}

type SubjectToolResultDeletion struct {
	DeletedCount   int64
	RemainingCount int64
}

func NewMinIOToolResultStore(config MinIOToolResultConfig) (*MinIOToolResultStore, error) {
	config.Endpoint = strings.TrimSpace(config.Endpoint)
	config.AccessKey = strings.TrimSpace(config.AccessKey)
	config.SecretKey = strings.TrimSpace(config.SecretKey)
	config.Bucket = strings.TrimSpace(config.Bucket)
	if config.Endpoint == "" || config.AccessKey == "" || config.SecretKey == "" {
		return nil, errors.New("MinIO endpoint and credentials are required")
	}
	if err := validateBucketName(config.Bucket); err != nil {
		return nil, err
	}
	client, err := minio.New(config.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(config.AccessKey, config.SecretKey, ""),
		Secure: config.Secure,
	})
	if err != nil {
		return nil, fmt.Errorf("create MinIO tool result client: %w", err)
	}
	return newMinIOToolResultStore(&minioSDKBackend{client: client}, config.Bucket), nil
}

func newMinIOToolResultStore(backend toolResultBackend, bucket string) *MinIOToolResultStore {
	return &MinIOToolResultStore{backend: backend, bucket: bucket}
}

func (s *MinIOToolResultStore) EnsureBucket(ctx context.Context) error {
	if s == nil || s.backend == nil {
		return errors.New("MinIO tool result store is unavailable")
	}
	if ctx == nil {
		return errors.New("MinIO tool result store context is nil")
	}
	exists, err := s.backend.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("check private tool result bucket: %w", err)
	}
	if !exists {
		if err := s.backend.MakeBucket(ctx, s.bucket); err != nil {
			if existsAfterRace, checkErr := s.backend.BucketExists(ctx, s.bucket); checkErr != nil || !existsAfterRace {
				return fmt.Errorf("create private tool result bucket: %w", err)
			}
		}
	}
	hasPolicy, err := s.backend.BucketHasPolicy(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("check private tool result bucket policy: %w", err)
	}
	if hasPolicy {
		return errors.New("private tool result bucket must not have an anonymous bucket policy")
	}
	return nil
}

func (s *MinIOToolResultStore) Put(ctx context.Context, request workflowTool.ResultArchiveRequest) (*workflowTool.ResultReference, error) {
	if s == nil || s.backend == nil {
		return nil, errors.New("MinIO tool result store is unavailable")
	}
	if ctx == nil {
		return nil, errors.New("MinIO tool result store context is nil")
	}
	if err := validateArchiveRequest(request); err != nil {
		return nil, err
	}
	key := toolResultObjectKey(request)
	if err := s.backend.Put(ctx, s.bucket, key, toolResultContentType, request.Snapshot.Payload); err != nil {
		return nil, fmt.Errorf("put private tool result object: %w", err)
	}
	return &workflowTool.ResultReference{
		Storage: "minio", Bucket: s.bucket, Key: key,
		Digest: request.Snapshot.Digest, Length: request.Snapshot.Length, ContentType: toolResultContentType,
	}, nil
}

func (s *MinIOToolResultStore) Get(ctx context.Context, reference workflowTool.ResultReference, maxBytes int) ([]byte, error) {
	if s == nil || s.backend == nil {
		return nil, errors.New("MinIO tool result store is unavailable")
	}
	if ctx == nil {
		return nil, errors.New("MinIO tool result store context is nil")
	}
	if err := s.validateReference(reference); err != nil {
		return nil, err
	}
	if maxBytes <= 0 || maxBytes > maxToolResultArchiveBytes || reference.Length > maxBytes {
		return nil, fmt.Errorf("archived tool result length %d exceeds limit %d", reference.Length, maxBytes)
	}
	payload, err := s.backend.Get(ctx, s.bucket, reference.Key, maxBytes)
	if err != nil {
		return nil, fmt.Errorf("get private tool result object: %w", err)
	}
	if len(payload) != reference.Length {
		return nil, fmt.Errorf("archived tool result length mismatch: expected %d bytes, got %d", reference.Length, len(payload))
	}
	sum := sha256.Sum256(payload)
	if hex.EncodeToString(sum[:]) != reference.Digest {
		return nil, errors.New("archived tool result digest mismatch")
	}
	return payload, nil
}

func (s *MinIOToolResultStore) Delete(ctx context.Context, reference workflowTool.ResultReference) error {
	if s == nil || s.backend == nil {
		return errors.New("MinIO tool result store is unavailable")
	}
	if ctx == nil {
		return errors.New("MinIO tool result store context is nil")
	}
	if err := s.validateReference(reference); err != nil {
		return err
	}
	if err := s.backend.Delete(ctx, s.bucket, reference.Key); err != nil {
		return fmt.Errorf("delete private tool result object: %w", err)
	}
	return nil
}

func (s *MinIOToolResultStore) ListToolResultObjects(
	ctx context.Context,
	cursor string,
	limit int,
) ([]agentService.ToolResultObjectInventoryItem, string, error) {
	if s == nil || s.backend == nil {
		return nil, "", errors.New("MinIO tool result store is unavailable")
	}
	if ctx == nil {
		return nil, "", errors.New("MinIO tool result store context is nil")
	}
	if limit <= 0 || limit > 1000 {
		return nil, "", errors.New("tool result object inventory limit must be between 1 and 1000")
	}
	if cursor != "" && (!strings.HasPrefix(cursor, "tool-results/") || strings.Contains(cursor, "..") || strings.Contains(cursor, "\\")) {
		return nil, "", errors.New("tool result object inventory cursor is invalid")
	}
	objects, nextCursor, err := s.backend.List(ctx, s.bucket, "tool-results/", cursor, limit)
	if err != nil {
		return nil, "", fmt.Errorf("list private tool result objects: %w", err)
	}
	items := make([]agentService.ToolResultObjectInventoryItem, 0, len(objects))
	for _, object := range objects {
		keyDigest := sha256.Sum256([]byte(object.Key))
		item := agentService.ToolResultObjectInventoryItem{
			Key: object.Key, KeyDigest: hex.EncodeToString(keyDigest[:]),
			LastModified: object.LastModified, Length: object.Size,
			ValidOwnedKey: validToolResultObjectKey(object.Key),
		}
		if item.ValidOwnedKey {
			parts := strings.Split(object.Key, "/")
			item.TenantHash = parts[1]
			item.ObjectDigest = strings.TrimSuffix(parts[3], ".json")
		}
		items = append(items, item)
	}
	return items, nextCursor, nil
}

// CountSubjectToolResults observes only objects in the current tenant-hashed
// key namespace. Legacy or malformed keys are intentionally outside this
// scope and must be handled by a separate audited migration.
func (s *MinIOToolResultStore) CountSubjectToolResults(ctx context.Context, userID uint64, pageSize int) (int64, error) {
	if err := s.validateSubjectOperation(ctx, userID, pageSize); err != nil {
		return 0, err
	}
	prefix := toolResultSubjectPrefix(userID)
	var total int64
	cursor := ""
	for {
		objects, nextCursor, err := s.backend.List(ctx, s.bucket, prefix, cursor, pageSize)
		if err != nil {
			return 0, fmt.Errorf("list subject tool result objects: %w", err)
		}
		if err := validateSubjectObjectPage(objects, prefix); err != nil {
			return 0, err
		}
		total += int64(len(objects))
		if nextCursor == "" {
			return total, nil
		}
		if nextCursor == cursor || !strings.HasPrefix(nextCursor, prefix) {
			return 0, errors.New("subject tool result cursor did not make scoped progress")
		}
		cursor = nextCursor
	}
}

// DeleteSubjectToolResults removes one tenant's current-format tool-result
// objects and verifies the scoped namespace is empty before returning.
func (s *MinIOToolResultStore) DeleteSubjectToolResults(ctx context.Context, userID uint64, pageSize int) (SubjectToolResultDeletion, error) {
	if err := s.validateSubjectOperation(ctx, userID, pageSize); err != nil {
		return SubjectToolResultDeletion{}, err
	}
	prefix := toolResultSubjectPrefix(userID)
	var deleted int64
	for {
		objects, _, err := s.backend.List(ctx, s.bucket, prefix, "", pageSize)
		if err != nil {
			return SubjectToolResultDeletion{DeletedCount: deleted}, fmt.Errorf("list subject tool result objects for deletion: %w", err)
		}
		if err := validateSubjectObjectPage(objects, prefix); err != nil {
			return SubjectToolResultDeletion{DeletedCount: deleted}, err
		}
		if len(objects) == 0 {
			return SubjectToolResultDeletion{DeletedCount: deleted}, nil
		}
		for _, object := range objects {
			if err := s.backend.Delete(ctx, s.bucket, object.Key); err != nil {
				return SubjectToolResultDeletion{DeletedCount: deleted, RemainingCount: int64(len(objects))}, fmt.Errorf("delete subject tool result object: %w", err)
			}
			deleted++
		}
	}
}

func (s *MinIOToolResultStore) validateSubjectOperation(ctx context.Context, userID uint64, pageSize int) error {
	if s == nil || s.backend == nil {
		return errors.New("MinIO tool result store is unavailable")
	}
	if ctx == nil {
		return errors.New("MinIO tool result store context is nil")
	}
	if userID == 0 {
		return errors.New("subject tool result operation requires a tenant identity")
	}
	if pageSize <= 0 || pageSize > 1000 {
		return errors.New("subject tool result page size must be between 1 and 1000")
	}
	return nil
}

func validateSubjectObjectPage(objects []toolResultObjectMetadata, prefix string) error {
	for _, object := range objects {
		if !strings.HasPrefix(object.Key, prefix) || !validToolResultObjectKey(object.Key) {
			return errors.New("object store returned an object outside the subject deletion scope")
		}
	}
	return nil
}

func (s *MinIOToolResultStore) validateReference(reference workflowTool.ResultReference) error {
	if reference.Storage != "minio" || reference.Bucket != s.bucket {
		return errors.New("tool result reference targets an unsupported object store")
	}
	if !validToolResultObjectKey(reference.Key) {
		return errors.New("tool result reference key is invalid")
	}
	if reference.ContentType != toolResultContentType {
		return errors.New("tool result reference content type is invalid")
	}
	if !validSHA256(reference.Digest) || reference.Digest != strings.ToLower(reference.Digest) {
		return errors.New("tool result reference digest is invalid")
	}
	if !strings.HasSuffix(reference.Key, "/"+reference.Digest+".json") {
		return errors.New("tool result reference key does not match digest")
	}
	if reference.Length <= 0 || reference.Length > maxToolResultArchiveBytes {
		return errors.New("tool result reference length is invalid")
	}
	return nil
}

func validateArchiveRequest(request workflowTool.ResultArchiveRequest) error {
	if request.UserID == 0 {
		return errors.New("tool result archive requires a tenant identity")
	}
	resultID := strings.TrimSpace(request.ResultID)
	if resultID == "" || resultID != request.ResultID || len(resultID) > maxToolResultIdentityBytes {
		return errors.New("tool result archive requires a result identity")
	}
	if request.Snapshot.Length != len(request.Snapshot.Payload) || request.Snapshot.Length <= 0 {
		return errors.New("tool result archive payload length is invalid")
	}
	if request.Snapshot.Length > maxToolResultArchiveBytes {
		return fmt.Errorf("tool result archive payload exceeds %d bytes", maxToolResultArchiveBytes)
	}
	if !validSHA256(request.Snapshot.Digest) {
		return errors.New("tool result archive digest is invalid")
	}
	sum := sha256.Sum256(request.Snapshot.Payload)
	if hex.EncodeToString(sum[:]) != request.Snapshot.Digest {
		return errors.New("tool result archive digest does not match payload")
	}
	return nil
}

func toolResultObjectKey(request workflowTool.ResultArchiveRequest) string {
	resultHash := sha256.Sum256([]byte("result:" + request.ResultID))
	return fmt.Sprintf("%s%s/%s.json", toolResultSubjectPrefix(request.UserID), hex.EncodeToString(resultHash[:16]), request.Snapshot.Digest)
}

func toolResultSubjectPrefix(userID uint64) string {
	tenantHash := sha256.Sum256([]byte("tenant:" + strconv.FormatUint(userID, 10)))
	return fmt.Sprintf("tool-results/%s/", hex.EncodeToString(tenantHash[:8]))
}

func validToolResultObjectKey(key string) bool {
	parts := strings.Split(key, "/")
	if len(parts) != 4 || parts[0] != "tool-results" || !validLowerHex(parts[1], 16) || !validLowerHex(parts[2], 32) {
		return false
	}
	digest, found := strings.CutSuffix(parts[3], ".json")
	return found && validLowerHex(digest, sha256.Size*2)
}

func validLowerHex(value string, length int) bool {
	if len(value) != length || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validateBucketName(bucket string) error {
	if len(bucket) < 3 || len(bucket) > 63 || strings.HasPrefix(bucket, ".") || strings.HasSuffix(bucket, ".") || strings.Contains(bucket, "..") {
		return errors.New("MinIO tool result bucket name is invalid")
	}
	for _, character := range bucket {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' && character != '.' {
			return errors.New("MinIO tool result bucket name is invalid")
		}
	}
	return nil
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

type minioSDKBackend struct {
	client *minio.Client
}

func (b *minioSDKBackend) BucketExists(ctx context.Context, bucket string) (bool, error) {
	return b.client.BucketExists(ctx, bucket)
}

func (b *minioSDKBackend) MakeBucket(ctx context.Context, bucket string) error {
	return b.client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{})
}

func (b *minioSDKBackend) BucketHasPolicy(ctx context.Context, bucket string) (bool, error) {
	policy, err := b.client.GetBucketPolicy(ctx, bucket)
	if err != nil {
		response := minio.ToErrorResponse(err)
		if response.Code == "NoSuchBucketPolicy" || response.Code == "NoSuchPolicy" {
			return false, nil
		}
		return false, err
	}
	return strings.TrimSpace(policy) != "", nil
}

func (b *minioSDKBackend) Put(ctx context.Context, bucket, key, contentType string, payload []byte) error {
	_, err := b.client.PutObject(ctx, bucket, key, bytes.NewReader(payload), int64(len(payload)), minio.PutObjectOptions{ContentType: contentType})
	return err
}

func (b *minioSDKBackend) Get(ctx context.Context, bucket, key string, maxBytes int) ([]byte, error) {
	info, err := b.client.StatObject(ctx, bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return nil, err
	}
	if info.Size < 0 || info.Size > int64(maxBytes) {
		return nil, fmt.Errorf("object size %d exceeds limit %d", info.Size, maxBytes)
	}
	object, err := b.client.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer object.Close()
	payload, err := io.ReadAll(io.LimitReader(object, int64(maxBytes)+1))
	if err != nil {
		return nil, err
	}
	if len(payload) > maxBytes {
		return nil, fmt.Errorf("object body exceeds limit %d", maxBytes)
	}
	return payload, nil
}

func (b *minioSDKBackend) Delete(ctx context.Context, bucket, key string) error {
	return b.client.RemoveObject(ctx, bucket, key, minio.RemoveObjectOptions{})
}

func (b *minioSDKBackend) List(
	ctx context.Context,
	bucket string,
	prefix string,
	cursor string,
	limit int,
) ([]toolResultObjectMetadata, string, error) {
	listCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	objects := make([]toolResultObjectMetadata, 0, limit)
	for object := range b.client.ListObjects(listCtx, bucket, minio.ListObjectsOptions{
		Prefix: prefix, Recursive: true, StartAfter: cursor, MaxKeys: limit,
	}) {
		if object.Err != nil {
			return nil, "", object.Err
		}
		objects = append(objects, toolResultObjectMetadata{
			Key: object.Key, LastModified: object.LastModified, Size: object.Size,
		})
		if len(objects) >= limit {
			cancel()
			break
		}
	}
	nextCursor := ""
	if len(objects) == limit {
		nextCursor = objects[len(objects)-1].Key
	}
	return objects, nextCursor, nil
}
