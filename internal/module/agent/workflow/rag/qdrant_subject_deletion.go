package rag

import (
	"context"
	"errors"
	"strings"

	agentdeletion "twitter-clone/internal/module/agent/deletion"
	"twitter-clone/pkg/qdrant"
)

const (
	qdrantSubjectDeletionAdapterName    = "qdrant_episodic_memory"
	qdrantSubjectDeletionAdapterVersion = "v1"
)

type qdrantSubjectDeletionResource struct {
	client     *qdrant.Client
	collection string
}

// QdrantSubjectDeletionConfig requires an explicit migration assertion. The
// online adapter never enumerates or drops legacy episodic_user_<id>
// collections; those must be migrated and retired by the offline migration
// command before this destructive capability can be constructed.
type QdrantSubjectDeletionConfig struct {
	CollectionName           string
	LegacyCollectionsRetired bool
}

func NewQdrantSubjectDeletionAdapter(
	client *qdrant.Client,
	config QdrantSubjectDeletionConfig,
	journal agentdeletion.ExternalDeletionJournal,
) (*agentdeletion.ExternalDeletionAdapter, error) {
	if client == nil {
		return nil, errors.New("Qdrant subject deletion client is unavailable")
	}
	if !config.LegacyCollectionsRetired {
		return nil, errors.New("Qdrant subject deletion requires retired legacy episodic collections")
	}
	defaults := DefaultEpisodicMemoryConfig()
	collection := strings.TrimSpace(config.CollectionName)
	if collection == "" {
		collection = defaults.CollectionName
	}
	resource := &qdrantSubjectDeletionResource{client: client, collection: collection}
	return agentdeletion.NewExternalDeletionAdapter(
		agentdeletion.CapabilityDescriptor{
			ID:      agentdeletion.CapabilityQdrantSubjectDelete,
			Adapter: qdrantSubjectDeletionAdapterName,
			Version: qdrantSubjectDeletionAdapterVersion,
		},
		QdrantSubjectDeletionScopeDigest(collection),
		resource,
		journal,
	)
}

func QdrantSubjectDeletionScopeDigest(collection string) string {
	return agentdeletion.DeletionScopeDigest(
		"qdrant-episodic-memory/v1", "collection="+strings.TrimSpace(collection),
		"filter=user_id:{subject}", "collection_schema=shared_user_payload_v1",
	)
}

func (r *qdrantSubjectDeletionResource) CountSubject(ctx context.Context, userID uint64) (int64, error) {
	if r == nil || r.client == nil || userID == 0 {
		return 0, errors.New("Qdrant subject deletion resource is invalid")
	}
	return r.client.CountByFilter(ctx, r.collection, episodicUserFilter(userID), true)
}

func (r *qdrantSubjectDeletionResource) DeleteSubject(ctx context.Context, userID uint64) error {
	if r == nil || r.client == nil || userID == 0 {
		return errors.New("Qdrant subject deletion resource is invalid")
	}
	return r.client.DeleteByFilter(ctx, r.collection, episodicUserFilter(userID))
}
