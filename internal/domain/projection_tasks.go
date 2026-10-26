package domain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	ProjectionSyncText   = "sync_text"
	ProjectionSyncVector = "sync_vector"
	ProjectionVersion    = "tweet-projection-v1"
)

type EmbeddingProfile struct {
	Model     string `json:"model"`
	Dimension int    `json:"dimension"`
	Version   string `json:"version"`
}

func (p EmbeddingProfile) Valid() bool {
	return strings.TrimSpace(p.Model) == p.Model && p.Model != "" && len(p.Model) <= 200 && p.Dimension > 0 && p.Dimension <= 8192 && strings.TrimSpace(p.Version) == p.Version && p.Version != "" && len(p.Version) <= 80
}
func (p EmbeddingProfile) Digest() string {
	b, _ := json.Marshal(p)
	d := sha256.Sum256(b)
	return hex.EncodeToString(d[:])
}

type ProjectionTaskPayload struct {
	TweetID         uint64           `json:"tweet_id"`
	AuthorID        uint64           `json:"author_id"`
	SchemaVersion   string           `json:"schema_version"`
	Embedding       EmbeddingProfile `json:"embedding"`
	RepairOperation string           `json:"repair_operation,omitempty"`
	ApprovedPlan    string           `json:"approved_plan,omitempty"`
	OperatorDigest  string           `json:"operator_digest,omitempty"`
	ReasonDigest    string           `json:"reason_digest,omitempty"`
}

type ProjectionRepairRepository interface {
	RegisterProjectionRepair(context.Context, []*OutboxTask) (int, error)
}

type IndexProjectionSnapshot struct {
	Present          bool   `json:"present"`
	Copies           int64  `json:"copies"`
	Digest           string `json:"digest"`
	Version          string `json:"version"`
	EmbeddingProfile string `json:"embedding_profile,omitempty"`
}

// ProjectionDigest describes current DB facts, without persisting the body.
func ProjectionDigest(tweet *Tweet) string {
	if tweet == nil {
		return "absent"
	}
	value := struct {
		ID, Author, Parent        uint64
		Content                   string
		Type, Visibility          int
		Created, Updated, Deleted int64
	}{tweet.ID, tweet.UserID, tweet.ParentID, tweet.Content, tweet.Type, tweet.VisibleType, tweet.CreatedAt, tweet.UpdatedAt, tweet.DeletedAt}
	b, _ := json.Marshal(value)
	d := sha256.Sum256(b)
	return fmt.Sprintf("%x", d[:])
}
