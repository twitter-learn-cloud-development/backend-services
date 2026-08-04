package objectstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"twitter-clone/internal/module/agent/eval"
)

const maxAgentTaskReportArchiveBytes = 8 << 20

var errAgentTaskReportObjectExists = errors.New("agent task report object already exists")

type MinIOAgentTaskReportConfig struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Bucket    string
	Secure    bool
}

type agentTaskReportObjectInfo struct {
	VersionID   string
	ETag        string
	Size        int64
	ContentType string
	ModifiedAt  time.Time
}

type agentTaskReportBackend interface {
	BucketExists(ctx context.Context, bucket string) (bool, error)
	MakeLockedBucket(ctx context.Context, bucket string) error
	BucketVersioningEnabled(ctx context.Context, bucket string) (bool, error)
	BucketObjectLockEnabled(ctx context.Context, bucket string) (bool, error)
	BucketHasPolicy(ctx context.Context, bucket string) (bool, error)
	PutImmutable(ctx context.Context, bucket, key string, payload []byte, request eval.AgentTaskReportArchiveRequest) (agentTaskReportObjectInfo, error)
	Stat(ctx context.Context, bucket, key, versionID string) (agentTaskReportObjectInfo, error)
	Get(ctx context.Context, bucket, key, versionID string, maxBytes int) ([]byte, error)
	GetRetention(ctx context.Context, bucket, key, versionID string) (string, time.Time, error)
}

type MinIOAgentTaskReportArchive struct {
	backend agentTaskReportBackend
	bucket  string
	now     func() time.Time
}

func NewMinIOAgentTaskReportArchive(config MinIOAgentTaskReportConfig) (*MinIOAgentTaskReportArchive, error) {
	config.Endpoint = strings.TrimSpace(config.Endpoint)
	config.AccessKey = strings.TrimSpace(config.AccessKey)
	config.SecretKey = strings.TrimSpace(config.SecretKey)
	config.Bucket = strings.TrimSpace(config.Bucket)
	if config.Endpoint == "" || config.AccessKey == "" || config.SecretKey == "" {
		return nil, errors.New("MinIO agent task archive endpoint and credentials are required")
	}
	if err := validateBucketName(config.Bucket); err != nil {
		return nil, err
	}
	client, err := minio.New(config.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(config.AccessKey, config.SecretKey, ""),
		Secure: config.Secure,
	})
	if err != nil {
		return nil, fmt.Errorf("create MinIO agent task archive client: %w", err)
	}
	return newMinIOAgentTaskReportArchive(&minioAgentTaskReportBackend{client: client}, config.Bucket), nil
}

func newMinIOAgentTaskReportArchive(backend agentTaskReportBackend, bucket string) *MinIOAgentTaskReportArchive {
	return &MinIOAgentTaskReportArchive{backend: backend, bucket: bucket, now: time.Now}
}

func (s *MinIOAgentTaskReportArchive) Ensure(ctx context.Context) error {
	if s == nil || s.backend == nil {
		return errors.New("MinIO agent task report archive is unavailable")
	}
	if ctx == nil {
		return errors.New("MinIO agent task report archive context is nil")
	}
	exists, err := s.backend.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("check agent task report archive bucket: %w", err)
	}
	if !exists {
		if err := s.backend.MakeLockedBucket(ctx, s.bucket); err != nil {
			if existsAfterRace, checkErr := s.backend.BucketExists(ctx, s.bucket); checkErr != nil || !existsAfterRace {
				return fmt.Errorf("create object-locked agent task report bucket: %w", err)
			}
		}
	}
	versioned, err := s.backend.BucketVersioningEnabled(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("check agent task archive bucket versioning: %w", err)
	}
	if !versioned {
		return errors.New("agent task archive bucket versioning is not enabled")
	}
	locked, err := s.backend.BucketObjectLockEnabled(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("check agent task archive object lock: %w", err)
	}
	if !locked {
		return errors.New("agent task archive bucket object lock is not enabled")
	}
	hasPolicy, err := s.backend.BucketHasPolicy(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("check agent task archive bucket policy: %w", err)
	}
	if hasPolicy {
		return errors.New("agent task archive bucket must not have an anonymous bucket policy")
	}
	return nil
}

func (s *MinIOAgentTaskReportArchive) PutImmutable(ctx context.Context, request eval.AgentTaskReportArchiveRequest) (eval.AgentTaskReportArchiveReceipt, error) {
	if s == nil || s.backend == nil {
		return eval.AgentTaskReportArchiveReceipt{}, errors.New("MinIO agent task report archive is unavailable")
	}
	if ctx == nil {
		return eval.AgentTaskReportArchiveReceipt{}, errors.New("MinIO agent task report archive context is nil")
	}
	now := s.now().UTC()
	if err := eval.ValidateAgentTaskReportArchiveRequest(request, now); err != nil {
		return eval.AgentTaskReportArchiveReceipt{}, err
	}
	if len(request.Payload) > maxAgentTaskReportArchiveBytes {
		return eval.AgentTaskReportArchiveReceipt{}, fmt.Errorf("agent task report size %d exceeds archive limit %d", len(request.Payload), maxAgentTaskReportArchiveBytes)
	}
	key := agentTaskReportObjectKey(request)
	info, err := s.backend.PutImmutable(ctx, s.bucket, key, request.Payload, request)
	created := true
	if errors.Is(err, errAgentTaskReportObjectExists) {
		created = false
		info, err = s.backend.Stat(ctx, s.bucket, key, "")
	}
	if err != nil {
		return eval.AgentTaskReportArchiveReceipt{}, fmt.Errorf("put immutable agent task report: %w", err)
	}
	if strings.TrimSpace(info.VersionID) == "" {
		return eval.AgentTaskReportArchiveReceipt{}, errors.New("agent task report archive did not return an object version")
	}
	if created {
		info, err = s.backend.Stat(ctx, s.bucket, key, info.VersionID)
		if err != nil {
			return eval.AgentTaskReportArchiveReceipt{}, fmt.Errorf("stat archived agent task report: %w", err)
		}
	}
	etag := strings.Trim(strings.TrimSpace(info.ETag), "\"")
	if info.VersionID == "" || etag == "" || info.Size != int64(len(request.Payload)) || info.ContentType != eval.AgentTaskReportArchiveContentType {
		return eval.AgentTaskReportArchiveReceipt{}, errors.New("archived agent task report object metadata does not match request")
	}
	archivedAt := info.ModifiedAt.UTC()
	if archivedAt.IsZero() || archivedAt.After(now.Add(5*time.Minute)) {
		return eval.AgentTaskReportArchiveReceipt{}, errors.New("archived agent task report modification time is invalid")
	}
	payload, err := s.backend.Get(ctx, s.bucket, key, info.VersionID, maxAgentTaskReportArchiveBytes)
	if err != nil {
		return eval.AgentTaskReportArchiveReceipt{}, fmt.Errorf("read back archived agent task report: %w", err)
	}
	if digestBytes(payload) != strings.ToLower(request.ReportSHA256) || !bytes.Equal(payload, request.Payload) {
		return eval.AgentTaskReportArchiveReceipt{}, errors.New("archived agent task report failed read-after-write integrity verification")
	}
	mode, retainUntil, err := s.backend.GetRetention(ctx, s.bucket, key, info.VersionID)
	if err != nil {
		return eval.AgentTaskReportArchiveReceipt{}, fmt.Errorf("read archived agent task report retention: %w", err)
	}
	if mode != eval.AgentTaskReportRetentionCompliance || !retainUntil.After(now) {
		return eval.AgentTaskReportArchiveReceipt{}, errors.New("archived agent task report is not protected by active compliance retention")
	}
	if retainUntil.Before(request.RetainUntil.Add(-time.Second)) {
		return eval.AgentTaskReportArchiveReceipt{}, errors.New("archived agent task report retention is shorter than requested")
	}
	if info.Size != int64(len(payload)) || info.ContentType != eval.AgentTaskReportArchiveContentType {
		return eval.AgentTaskReportArchiveReceipt{}, errors.New("archived agent task report object metadata does not match payload")
	}
	receipt := eval.AgentTaskReportArchiveReceipt{
		SchemaVersion:       eval.AgentTaskReportArchiveReceiptSchemaVersion,
		Storage:             "minio",
		Bucket:              s.bucket,
		Key:                 key,
		VersionID:           info.VersionID,
		ETag:                etag,
		ReportSHA256:        strings.ToLower(request.ReportSHA256),
		Length:              len(payload),
		ContentType:         eval.AgentTaskReportArchiveContentType,
		RetentionMode:       mode,
		RetainUntil:         retainUntil.UTC(),
		ArchivedAt:          archivedAt,
		Created:             created,
		DatasetVersion:      request.DatasetVersion,
		DatasetSHA256:       strings.ToLower(request.DatasetSHA256),
		ExecutionConfigHash: strings.ToLower(request.ExecutionConfigHash),
		IntegrityKeyID:      request.IntegrityKeyID,
	}
	if err := eval.ValidateAgentTaskReportArchiveReceipt(receipt); err != nil {
		return eval.AgentTaskReportArchiveReceipt{}, err
	}
	return receipt, nil
}

func (s *MinIOAgentTaskReportArchive) Get(ctx context.Context, receipt eval.AgentTaskReportArchiveReceipt, maxBytes int) ([]byte, error) {
	if s == nil || s.backend == nil {
		return nil, errors.New("MinIO agent task report archive is unavailable")
	}
	if ctx == nil {
		return nil, errors.New("MinIO agent task report archive context is nil")
	}
	if err := eval.ValidateAgentTaskReportArchiveReceipt(receipt); err != nil {
		return nil, err
	}
	if receipt.Bucket != s.bucket {
		return nil, errors.New("agent task archive receipt targets a different bucket")
	}
	if maxBytes <= 0 || receipt.Length > maxBytes || maxBytes > maxAgentTaskReportArchiveBytes {
		return nil, fmt.Errorf("agent task archive read limit %d is invalid for report length %d", maxBytes, receipt.Length)
	}
	info, err := s.backend.Stat(ctx, s.bucket, receipt.Key, receipt.VersionID)
	if err != nil {
		return nil, fmt.Errorf("stat archived agent task report: %w", err)
	}
	etag := strings.Trim(strings.TrimSpace(info.ETag), "\"")
	modifiedAt := info.ModifiedAt.UTC()
	modifiedDelta := modifiedAt.Sub(receipt.ArchivedAt.UTC())
	if info.VersionID != receipt.VersionID || etag == "" ||
		(receipt.ETag != "" && etag != strings.Trim(strings.TrimSpace(receipt.ETag), "\"")) ||
		info.Size != int64(receipt.Length) || info.ContentType != receipt.ContentType ||
		modifiedAt.IsZero() || modifiedDelta < -time.Second || modifiedDelta > time.Second {
		return nil, errors.New("archived agent task report object metadata does not match its receipt")
	}
	payload, err := s.backend.Get(ctx, s.bucket, receipt.Key, receipt.VersionID, maxBytes)
	if err != nil {
		return nil, fmt.Errorf("get archived agent task report: %w", err)
	}
	if len(payload) != receipt.Length || digestBytes(payload) != strings.ToLower(receipt.ReportSHA256) {
		return nil, errors.New("archived agent task report does not match its receipt")
	}
	mode, retainUntil, err := s.backend.GetRetention(ctx, s.bucket, receipt.Key, receipt.VersionID)
	if err != nil {
		return nil, fmt.Errorf("read archived agent task report retention: %w", err)
	}
	if mode != receipt.RetentionMode || retainUntil.Before(receipt.RetainUntil) {
		return nil, errors.New("archived agent task report retention does not match its receipt")
	}
	return payload, nil
}

func agentTaskReportObjectKey(request eval.AgentTaskReportArchiveRequest) string {
	versionHash := sha256.Sum256([]byte("dataset-version:" + request.DatasetVersion))
	configHash := request.ExecutionConfigHash
	if configHash == "" {
		configHash = "recorded"
	} else {
		configHash = strings.ToLower(configHash[:16])
	}
	return fmt.Sprintf(
		"agent-task-eval/%s/%s/%s/%s/%s.json",
		hex.EncodeToString(versionHash[:8]),
		strings.ToLower(request.DatasetSHA256[:16]),
		configHash,
		request.SignedAt.UTC().Format("2006/01/02"),
		strings.ToLower(request.ReportSHA256),
	)
}

func digestBytes(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

type minioAgentTaskReportBackend struct {
	client *minio.Client
}

func (b *minioAgentTaskReportBackend) BucketExists(ctx context.Context, bucket string) (bool, error) {
	return b.client.BucketExists(ctx, bucket)
}

func (b *minioAgentTaskReportBackend) MakeLockedBucket(ctx context.Context, bucket string) error {
	return b.client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{ObjectLocking: true})
}

func (b *minioAgentTaskReportBackend) BucketVersioningEnabled(ctx context.Context, bucket string) (bool, error) {
	configuration, err := b.client.GetBucketVersioning(ctx, bucket)
	return configuration.Enabled(), err
}

func (b *minioAgentTaskReportBackend) BucketObjectLockEnabled(ctx context.Context, bucket string) (bool, error) {
	status, _, _, _, err := b.client.GetObjectLockConfig(ctx, bucket)
	return status == "Enabled", err
}

func (b *minioAgentTaskReportBackend) BucketHasPolicy(ctx context.Context, bucket string) (bool, error) {
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

func (b *minioAgentTaskReportBackend) PutImmutable(ctx context.Context, bucket, key string, payload []byte, request eval.AgentTaskReportArchiveRequest) (agentTaskReportObjectInfo, error) {
	options := minio.PutObjectOptions{
		ContentType:     eval.AgentTaskReportArchiveContentType,
		Mode:            minio.Compliance,
		RetainUntilDate: request.RetainUntil.UTC(),
		UserMetadata: map[string]string{
			"report-sha256":           request.ReportSHA256,
			"dataset-version":         request.DatasetVersion,
			"dataset-sha256":          request.DatasetSHA256,
			"execution-config-sha256": request.ExecutionConfigHash,
			"report-schema-version":   request.ReportSchemaVersion,
			"integrity-key-id":        request.IntegrityKeyID,
		},
	}
	options.SetMatchETagExcept("*")
	upload, err := b.client.PutObject(ctx, bucket, key, bytes.NewReader(payload), int64(len(payload)), options)
	if err != nil {
		response := minio.ToErrorResponse(err)
		if response.StatusCode == 412 || response.Code == "PreconditionFailed" {
			return agentTaskReportObjectInfo{}, errAgentTaskReportObjectExists
		}
		return agentTaskReportObjectInfo{}, err
	}
	return agentTaskReportObjectInfo{VersionID: upload.VersionID, ETag: upload.ETag, Size: upload.Size, ModifiedAt: upload.LastModified}, nil
}

func (b *minioAgentTaskReportBackend) Stat(ctx context.Context, bucket, key, versionID string) (agentTaskReportObjectInfo, error) {
	info, err := b.client.StatObject(ctx, bucket, key, minio.StatObjectOptions{VersionID: versionID})
	if err != nil {
		return agentTaskReportObjectInfo{}, err
	}
	return agentTaskReportObjectInfo{
		VersionID: info.VersionID, ETag: info.ETag, Size: info.Size,
		ContentType: info.ContentType, ModifiedAt: info.LastModified,
	}, nil
}

func (b *minioAgentTaskReportBackend) Get(ctx context.Context, bucket, key, versionID string, maxBytes int) ([]byte, error) {
	options := minio.GetObjectOptions{VersionID: versionID}
	object, err := b.client.GetObject(ctx, bucket, key, options)
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

func (b *minioAgentTaskReportBackend) GetRetention(ctx context.Context, bucket, key, versionID string) (string, time.Time, error) {
	mode, retainUntil, err := b.client.GetObjectRetention(ctx, bucket, key, versionID)
	if err != nil {
		return "", time.Time{}, err
	}
	if mode == nil || retainUntil == nil {
		return "", time.Time{}, errors.New("object retention metadata is missing")
	}
	return mode.String(), retainUntil.UTC(), nil
}
