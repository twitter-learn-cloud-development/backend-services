package projection

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"twitter-clone/internal/domain"
)

type RepairAuthorization struct {
	ApprovedPlan string
	Operation    string
	Operator     string
	Reason       string
}

var operationPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

func (auth RepairAuthorization) Valid() bool {
	return operationPattern.MatchString(auth.Operation) && len(auth.ApprovedPlan) == 64 && strings.TrimSpace(auth.Operator) != "" && len(auth.Operator) <= 100 && strings.TrimSpace(auth.Reason) != "" && len(auth.Reason) <= 500
}

// Repair only registers durable work. The Worker owns all index mutations.
func (a Auditor) Repair(ctx context.Context, ids []uint64, auth RepairAuthorization, registrar domain.ProjectionRepairRepository) (Plan, int, error) {
	if !auth.Valid() || registrar == nil {
		return Plan{}, 0, errors.New("explicit repair authorization unavailable")
	}
	plan, err := a.Audit(ctx, ids)
	if err != nil {
		return Plan{}, 0, err
	}
	if plan.Digest != auth.ApprovedPlan {
		return plan, 0, errors.New("repair plan changed; review new plan")
	}
	ids, _ = NormalizeIDs(ids)
	createdTotal := 0
	for i, id := range ids {
		row := plan.Rows[i]
		// Re-read inside the same ID lock at registration time to catch fact
		// changes after the audit. Writes are all-or-none for this one ID.
		err = a.Facts.WithProjectionLock(ctx, id, func(locked context.Context) error {
			fact, err := a.Facts.ReadProjectionTweet(locked, id)
			if err != nil {
				return errors.New("repair current fact unavailable")
			}
			if domain.ProjectionDigest(fact) != row.FactDigest {
				return errors.New("repair fact changed")
			}
			identity := uint64(0)
			if fact != nil {
				identity = fact.UserID
			}
			payload := domain.ProjectionTaskPayload{TweetID: id, AuthorID: identity, SchemaVersion: domain.ProjectionVersion, Embedding: a.Profile, RepairOperation: auth.Operation, ApprovedPlan: plan.Digest, OperatorDigest: Hash([]byte(auth.Operator)), ReasonDigest: Hash([]byte(auth.Reason))}
			body, _ := json.Marshal(payload)
			perID := []*domain.OutboxTask{}
			for backend, status := range map[string]string{"text": row.TextStatus, "vector": row.VectorStatus} {
				if status == "consistent" {
					continue
				}
				kind := "delete_es"
				if backend == "vector" {
					kind = "delete_vector"
				}
				if row.Public {
					kind = domain.ProjectionSyncText
					if backend == "vector" {
						kind = domain.ProjectionSyncVector
					}
				}
				key := fmt.Sprintf("repair:%s:%s:%d:%s", auth.Operation, plan.Digest[:24], id, kind)
				perID = append(perID, &domain.OutboxTask{TaskType: kind, DedupKey: &key, Payload: string(body), MaxRetries: 5})
			}
			// Sort deterministic target order before the transaction.
			if len(perID) == 2 && perID[0].TaskType > perID[1].TaskType {
				perID[0], perID[1] = perID[1], perID[0]
			}
			count, err := registrar.RegisterProjectionRepair(locked, perID)
			if err != nil {
				return err
			}
			createdTotal += count
			return nil
		})
		if err != nil {
			return plan, createdTotal, err
		}
	}
	return plan, createdTotal, nil
}
