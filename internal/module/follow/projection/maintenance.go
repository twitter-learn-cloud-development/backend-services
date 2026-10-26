package projection

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"sort"
	"strings"
	"time"
	"twitter-clone/internal/domain"
	"twitter-clone/internal/events"
)

type Scope struct{ Author, Follower uint64 }
type AuditRow struct {
	Subject           string `json:"subject_digest"`
	Count             int64  `json:"follower_count"`
	Generation        string `json:"generation_digest"`
	Following         bool   `json:"following"`
	StateRevision     uint64 `json:"projection_revision"`
	CheckpointPending bool   `json:"checkpoint_pending"`
	Celebrity         bool   `json:"celebrity"`
	CachedCelebrity   bool   `json:"cached_celebrity"`
	CachedFollower    bool   `json:"cached_follower"`
}
type AuditPlan struct {
	Target string     `json:"target_digest"`
	Rows   []AuditRow `json:"rows"`
	Digest string     `json:"plan_digest"`
}

func digest(value interface{}) string {
	body, _ := json.Marshal(value)
	return fmt.Sprintf("%x", sha256.Sum256(body))
}
func NormalizeScope(input []Scope) ([]Scope, error) {
	if len(input) == 0 || len(input) > 100 {
		return nil, errors.New("explicit follow scope requires 1-100 entries")
	}
	rows := append([]Scope(nil), input...)
	seen := map[Scope]bool{}
	for _, scope := range rows {
		if scope.Author == 0 || scope.Follower == scope.Author || seen[scope] {
			return nil, errors.New("invalid or duplicate follow scope")
		}
		seen[scope] = true
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Author == rows[j].Author {
			return rows[i].Follower < rows[j].Follower
		}
		return rows[i].Author < rows[j].Author
	})
	return rows, nil
}

type Maintenance struct {
	Store  *SQLStore
	Cache  *RedisCache
	Target string
}

func (m Maintenance) Audit(ctx context.Context, scopes []Scope) (AuditPlan, error) {
	rows, err := NormalizeScope(scopes)
	if err != nil {
		return AuditPlan{}, err
	}
	if m.Store == nil || m.Cache == nil || len(m.Target) != 64 {
		return AuditPlan{}, errors.New("follow maintenance unavailable")
	}
	plan := AuditPlan{Target: m.Target, Rows: []AuditRow{}}
	for _, scope := range rows {
		err := m.Store.WithAuthorLock(ctx, scope.Author, func(locked context.Context) error {
			state, err := m.Store.ReadAuthor(locked, scope.Author)
			if err != nil {
				return err
			}
			count, err := m.Store.FollowerCount(locked, scope.Author)
			if err != nil {
				return err
			}
			row := AuditRow{Subject: digest(scope), Count: count, StateRevision: state.Revision, CheckpointPending: !state.Complete, Celebrity: DecideCelebrity(state.Celebrity, count)}
			row.CachedCelebrity, err = m.Cache.Client.SIsMember(locked, "global:celebrities", uintString(scope.Author)).Result()
			if err != nil {
				return err
			}
			if state.Revision == 0 {
				row.Celebrity = DecideCelebrity(row.CachedCelebrity, count)
			}
			if scope.Follower > 0 {
				fact, err := m.Store.ReadPair(locked, scope.Follower, scope.Author)
				if err != nil {
					return err
				}
				row.Following = fact.Active
				row.Generation = digest(fact.Generation)
				row.CachedFollower, err = m.Cache.Client.SIsMember(locked, "user:celebrities:"+uintString(scope.Follower), uintString(scope.Author)).Result()
				if err != nil {
					return err
				}
			}
			plan.Rows = append(plan.Rows, row)
			return nil
		})
		if err != nil {
			return AuditPlan{}, errors.New("follow audit current facts unavailable")
		}
	}
	plan.Digest = digest(plan)
	return plan, nil
}
func (m Maintenance) Repair(ctx context.Context, scopes []Scope, approved, operation, operator, reason string) (AuditPlan, int, error) {
	if len(approved) != 64 || operation == "" || len(operation) > 64 || strings.TrimSpace(operator) == "" || len(operator) > 100 || strings.TrimSpace(reason) == "" || len(reason) > 500 {
		return AuditPlan{}, 0, errors.New("explicit follow repair authorization required")
	}
	plan, err := m.Audit(ctx, scopes)
	if err != nil {
		return plan, 0, err
	}
	if plan.Digest != approved {
		return plan, 0, errors.New("follow repair plan changed")
	}
	rows, _ := NormalizeScope(scopes)
	count := 0
	for i, scope := range rows {
		one, err := m.Audit(ctx, []Scope{scope})
		if err != nil {
			return plan, count, err
		}
		if digest(one.Rows[0]) != digest(plan.Rows[i]) {
			return plan, count, errors.New("follow repair facts changed")
		}
		created, err := m.Store.RegisterRepair(ctx, scope, operation, plan.Digest, operator, reason)
		if err != nil {
			return plan, count, err
		}
		count += created
	}
	return plan, count, nil
}
func (s *SQLStore) RegisterRepair(ctx context.Context, scope Scope, operation, approved, operator, reason string) (int, error) {
	identity := digest(struct {
		Scope                             Scope
		Operation, Plan, Operator, Reason string
	}{scope, operation, approved, digest(operator), digest(reason)})
	key := "follow-repair:" + identity
	count := 0
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		marker, err := token()
		if err != nil {
			return err
		}
		receipt := IngressReceipt{EventKey: key, Digest: identity, CreatedAt: time.Now().UnixMilli(), RegistrationToken: marker}
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&receipt)
		if result.Error != nil {
			return result.Error
		}
		var existing IngressReceipt
		if err := tx.Where("event_key = ?", key).Take(&existing).Error; err != nil {
			return err
		}
		if existing.Digest != identity {
			return errors.New("follow maintenance identity conflict")
		}
		if existing.RegistrationToken != marker {
			return nil
		}
		p := Checkpoint{Event: events.FollowRelationEvent{FollowerID: scope.Follower, FolloweeID: scope.Author, SchemaVersion: "follow-projection-repair-v1"}, Repair: identity, ApprovedPlan: approved, OperatorDigest: digest(operator), ReasonDigest: digest(reason), OperationDigest: digest(operation)}
		body, _ := json.Marshal(p)
		kinds := []string{AuthorTask}
		if scope.Follower > 0 {
			kinds = append(kinds, PairTask)
		}
		for _, kind := range kinds {
			id := uint64(0)
			if !s.MaintenanceAutoID {
				if s.NewID == nil {
					return errors.New("follow repair allocator missing")
				}
				var err error
				id, err = s.NewID()
				if err != nil {
					return err
				}
				if id == 0 {
					return errors.New("follow repair identity unavailable")
				}
			}
			taskKey := key + ":" + kind
			if err := tx.Create(&domain.OutboxTask{ID: id, TaskType: kind, DedupKey: &taskKey, Payload: string(body), MaxRetries: 5, CreatedAt: time.Now().UnixMilli(), UpdatedAt: time.Now().UnixMilli()}).Error; err != nil {
				return err
			}
			count++
		}
		return nil
	})
	if err != nil {
		return 0, errors.New("follow repair registration unavailable")
	}
	return count, nil
}
