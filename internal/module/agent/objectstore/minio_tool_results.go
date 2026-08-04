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

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

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
}

type MinIOToolResultStore struct {
	backend toolResultBackend
	bucket  string
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
	tenantHash := sha256.Sum256([]byte("tenant:" + strconv.FormatUint(request.UserID, 10)))
	resultHash := sha256.Sum256([]byte("result:" + request.ResultID))
	return fmt.Sprintf("tool-results/%s/%s/%s.json", hex.EncodeToString(tenantHash[:8]), hex.EncodeToString(resultHash[:16]), request.Snapshot.Digest)
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
