package objectstore

import (
	"context"
	"errors"
	"fmt"

	agentdeletion "twitter-clone/internal/module/agent/deletion"
)

const (
	minioSubjectDeletionAdapterName    = "minio_tool_results"
	minioSubjectDeletionAdapterVersion = "v1"
	defaultSubjectDeletionPageSize     = 200
)

type minioSubjectDeletionResource struct {
	store    *MinIOToolResultStore
	pageSize int
}

func NewMinIOSubjectDeletionAdapter(
	store *MinIOToolResultStore,
	journal agentdeletion.ExternalDeletionJournal,
) (*agentdeletion.ExternalDeletionAdapter, error) {
	if store == nil || store.backend == nil || store.bucket == "" {
		return nil, errors.New("MinIO subject deletion store is unavailable")
	}
	resource := &minioSubjectDeletionResource{store: store, pageSize: defaultSubjectDeletionPageSize}
	return agentdeletion.NewExternalDeletionAdapter(
		agentdeletion.CapabilityDescriptor{
			ID:      agentdeletion.CapabilityMinIOSubjectDelete,
			Adapter: minioSubjectDeletionAdapterName,
			Version: minioSubjectDeletionAdapterVersion,
		},
		MinIOSubjectDeletionScopeDigest(store.bucket),
		resource,
		journal,
	)
}

func MinIOSubjectDeletionScopeDigest(bucket string) string {
	return agentdeletion.DeletionScopeDigest(
		"minio-tool-results/v1", "bucket="+bucket,
		"prefix=tool-results/{sha256(tenant)}/", "object-format={result-hash}/{content-sha256}.json",
	)
}

func (r *minioSubjectDeletionResource) CountSubject(ctx context.Context, userID uint64) (int64, error) {
	if r == nil || r.store == nil {
		return 0, errors.New("MinIO subject deletion resource is unavailable")
	}
	return r.store.CountSubjectToolResults(ctx, userID, r.pageSize)
}

func (r *minioSubjectDeletionResource) DeleteSubject(ctx context.Context, userID uint64) error {
	if r == nil || r.store == nil {
		return errors.New("MinIO subject deletion resource is unavailable")
	}
	result, err := r.store.DeleteSubjectToolResults(ctx, userID, r.pageSize)
	if err != nil {
		return err
	}
	if result.RemainingCount != 0 {
		return fmt.Errorf("MinIO subject deletion left %d objects", result.RemainingCount)
	}
	return nil
}
