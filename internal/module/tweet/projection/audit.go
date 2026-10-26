package projection

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"

	"twitter-clone/internal/domain"
)

type IndexReader interface {
	ReadTweetProjection(context.Context, uint64) (domain.IndexProjectionSnapshot, error)
}
type Auditor struct {
	Facts        domain.TweetProjectionRepository
	Text, Vector IndexReader
	Profile      domain.EmbeddingProfile
	TargetDigest string
}
type Plan struct {
	Version       string     `json:"version"`
	TargetDigest  string     `json:"target_digest"`
	ProfileDigest string     `json:"profile_digest"`
	Rows          []AuditRow `json:"rows"`
	Digest        string     `json:"plan_digest"`
}
type AuditRow struct {
	Subject      string                         `json:"subject_digest"`
	FactDigest   string                         `json:"fact_digest"`
	Public       bool                           `json:"public"`
	Text         domain.IndexProjectionSnapshot `json:"text"`
	Vector       domain.IndexProjectionSnapshot `json:"vector"`
	TextStatus   string                         `json:"text_status"`
	VectorStatus string                         `json:"vector_status"`
}

func Hash(value []byte) string { d := sha256.Sum256(value); return hex.EncodeToString(d[:]) }
func Subject(id uint64) string { b, _ := json.Marshal(id); return Hash(b) }
func NormalizeIDs(ids []uint64) ([]uint64, error) {
	if len(ids) == 0 || len(ids) > 100 {
		return nil, errors.New("explicit projection ID list must contain 1-100 entries")
	}
	seen := map[uint64]bool{}
	result := []uint64{}
	for _, id := range ids {
		if id == 0 || seen[id] {
			return nil, errors.New("invalid or duplicate projection identity")
		}
		seen[id] = true
		result = append(result, id)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result, nil
}
func (a Auditor) Audit(ctx context.Context, ids []uint64) (Plan, error) {
	ids, err := NormalizeIDs(ids)
	if err != nil {
		return Plan{}, err
	}
	if a.Facts == nil || a.Text == nil || a.Vector == nil || !a.Profile.Valid() || len(a.TargetDigest) != 64 {
		return Plan{}, errors.New("projection audit configuration unavailable")
	}
	plan := Plan{Version: "projection-audit-v1", TargetDigest: a.TargetDigest, ProfileDigest: a.Profile.Digest(), Rows: []AuditRow{}}
	for _, id := range ids {
		var row AuditRow
		err := a.Facts.WithProjectionLock(ctx, id, func(locked context.Context) error {
			fact, err := a.Facts.ReadProjectionTweet(locked, id)
			if err != nil {
				return errors.New("audit current fact unavailable")
			}
			if fact != nil && fact.ID != id {
				return errors.New("audit fact identity mismatch")
			}
			row = AuditRow{Subject: Subject(id), FactDigest: domain.ProjectionDigest(fact), Public: fact != nil && fact.DeletedAt == 0 && fact.VisibleType == domain.VisiblePublic}
			row.Text, err = a.Text.ReadTweetProjection(locked, id)
			if err != nil {
				return errors.New("audit text metadata unavailable")
			}
			row.Vector, err = a.Vector.ReadTweetProjection(locked, id)
			if err != nil {
				return errors.New("audit vector metadata unavailable")
			}
			after, err := a.Facts.ReadProjectionTweet(locked, id)
			if err != nil || domain.ProjectionDigest(after) != row.FactDigest {
				return errors.New("audit fact changed")
			}
			row.TextStatus = Compare(row.Public, row.FactDigest, "", row.Text)
			row.VectorStatus = Compare(row.Public, row.FactDigest, plan.ProfileDigest, row.Vector)
			return nil
		})
		if err != nil {
			return Plan{}, err
		}
		plan.Rows = append(plan.Rows, row)
	}
	unsigned := plan
	unsigned.Digest = ""
	data, _ := json.Marshal(unsigned)
	plan.Digest = Hash(data)
	return plan, nil
}
func Compare(public bool, fact, profile string, index domain.IndexProjectionSnapshot) string {
	if !public {
		if index.Present || index.Copies > 0 {
			return "residue"
		}
		return "consistent"
	}
	if !index.Present {
		return "missing"
	}
	if index.Copies != 1 {
		return "aliases"
	}
	if index.Version != domain.ProjectionVersion || index.Digest != fact {
		return "drift"
	}
	if profile != "" && index.EmbeddingProfile != profile {
		return "profile_drift"
	}
	return "consistent"
}
