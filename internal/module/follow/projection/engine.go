package projection

import (
	"context"
	"encoding/json"
	"errors"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"time"
	"twitter-clone/internal/domain"
)

func DecideCelebrity(previous bool, count int64) bool {
	if count >= 5000 {
		return true
	}
	if count < 4500 {
		return false
	}
	return previous
}

// Execute advances at most one page, releasing the lease on a successful yield.
// Failures leave the page checkpoint unchanged for idempotent replay.
func (e Engine) Execute(ctx context.Context, task *domain.OutboxTask) error {
	if task == nil || (task.TaskType != PairTask && task.TaskType != AuthorTask) || e.Store == nil || e.Cache == nil {
		return errors.New("follow projection dependencies unavailable")
	}
	var p Checkpoint
	if json.Unmarshal([]byte(task.Payload), &p) != nil || !validCheckpoint(task.TaskType, p) {
		return errors.New("invalid follow projection payload")
	}
	carrier := propagation.MapCarrier{"traceparent": p.Event.Traceparent, "tracestate": p.Event.Tracestate}
	ctx = propagation.TraceContext{}.Extract(ctx, carrier)
	ctx, span := otel.Tracer("follow-projection").Start(ctx, "FollowProjection.Page")
	defer span.End()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	start := time.Now()
	result := "failed"
	defer func() {
		if result == "failed" && errors.Is(ctx.Err(), context.Canceled) {
			result = "cancelled"
		}
		if e.Observer != nil {
			e.Observer.Observe(task.TaskType, result)
			e.Observer.Duration(task.TaskType, time.Since(start).Seconds())
		}
	}()
	err := e.Store.WithAuthorLock(ctx, p.Event.FolloweeID, func(locked context.Context) error {
		owns, err := e.Store.Owns(locked, task)
		if err != nil {
			return err
		}
		if !owns {
			return errors.New("follow claim is stale")
		}
		state, err := e.Store.ReadAuthor(locked, p.Event.FolloweeID)
		if err != nil {
			return err
		}
		count, err := e.Store.FollowerCount(locked, p.Event.FolloweeID)
		if err != nil {
			return err
		}
		previous := state.Celebrity
		if state.Revision == 0 && task.TaskType == AuthorTask {
			previous, err = e.Cache.Celebrity(locked, state.AuthorID)
			if err != nil {
				return err
			}
		}
		desired := DecideCelebrity(previous, count)
		if task.TaskType == PairTask && (state.Revision == 0 || desired != state.Celebrity) {
			advanced, err := e.Store.Advance(locked, task, p, false)
			if err != nil {
				return err
			}
			if !advanced {
				return errors.New("follow yield rejected")
			}
			result = "waiting"
			return nil
		}
		if task.TaskType == AuthorTask && (state.Revision == 0 || desired != state.Celebrity || (p.Repair != "" && state.LastRepair != p.Repair)) {
			if state.Revision == ^uint64(0) {
				return errors.New("follow revision exhausted")
			}
			state.Revision++
			state.Celebrity = desired
			state.Cursor = 0
			state.Complete = false
			if p.Repair != "" {
				state.LastRepair = p.Repair
			}
		}
		if state.Epoch == ^uint64(0) {
			return errors.New("follow epoch exhausted")
		}
		state.Epoch++
		if err := e.Store.SaveAuthor(locked, state); err != nil {
			return err
		}
		fence := Fence{Epoch: state.Epoch, Token: task.LeaseToken, Until: task.LeaseUntil}
		if err := e.Cache.Begin(locked, state.AuthorID, state, fence); err != nil {
			return err
		}
		done := false
		if task.TaskType == AuthorTask {
			if !state.Complete {
				page, err := e.Store.HistoryFollowers(locked, state.AuthorID, state.Cursor)
				if err != nil {
					return err
				}
				if err := validateRelationPage(page, state.Cursor); err != nil {
					return err
				}
				ids := []uint64{}
				for _, row := range page.Rows {
					ids = append(ids, row.FollowerID)
				}
				active, err := e.Store.ActiveFollowers(locked, state.AuthorID, ids)
				if err != nil {
					return err
				}
				for _, row := range page.Rows {
					if err := e.Cache.Follower(locked, state.AuthorID, row.FollowerID, state.Celebrity && active[row.FollowerID], fence); err != nil {
						return err
					}
				}
				state.Cursor = page.Next
				state.Complete = !page.More
				if err := e.Store.SaveAuthor(locked, state); err != nil {
					return err
				}
			}
			done = state.Complete
		} else {
			fact, err := e.Store.ReadPair(locked, p.Event.FollowerID, state.AuthorID)
			if err != nil {
				return err
			}
			if !p.Initialized || p.Generation != fact.Generation {
				p.Cursor = 0
				p.Generation = fact.Generation
				p.Initialized = true
			}
			if err := e.Cache.Follower(locked, state.AuthorID, p.Event.FollowerID, state.Celebrity && fact.Active, fence); err != nil {
				return err
			}
			page, err := e.Store.HistoryTweets(locked, state.AuthorID, p.Cursor)
			if err != nil {
				return err
			}
			if err := validateTweetPage(page, p.Cursor); err != nil {
				return err
			}
			if err := e.Cache.PairTweets(locked, state.AuthorID, p.Event.FollowerID, fact.Active, page.Rows, fence); err != nil {
				return err
			}
			after, err := e.Store.ReadPair(locked, p.Event.FollowerID, state.AuthorID)
			if err != nil {
				return err
			}
			if after != fact {
				p.Cursor = 0
				p.Initialized = false
			} else {
				p.Cursor = page.Next
				done = !page.More
			}
		}
		advanced, err := e.Store.Advance(locked, task, p, done)
		if err != nil {
			return err
		}
		if !advanced {
			return errors.New("follow checkpoint rejected")
		}
		result = "yielded"
		if done {
			result = "completed"
		}
		return nil
	})
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

func validCheckpoint(kind string, p Checkpoint) bool {
	if p.Repair != "" {
		return len(p.Repair) == 64 && len(p.ApprovedPlan) == 64 && len(p.OperatorDigest) == 64 && len(p.ReasonDigest) == 64 && len(p.OperationDigest) == 64 && p.Event.SchemaVersion == "follow-projection-repair-v1" && p.Event.FolloweeID > 0 && (kind == AuthorTask || (p.Event.FollowerID > 0 && p.Event.FollowerID != p.Event.FolloweeID))
	}
	return p.Event.Validate(p.Event.EventKey == "follow:"+uintString(p.Event.RelationID)+":followed") == nil
}

func validateRelationPage(p RelationPage, cursor uint64) error {
	if len(p.Rows) > PageSize || (p.More && (len(p.Rows) == 0 || p.Next == 0)) {
		return errors.New("invalid follower projection page")
	}
	prev := cursor
	for _, row := range p.Rows {
		if row.ID == 0 || row.FollowerID == 0 || (prev > 0 && row.ID >= prev) {
			return errors.New("nonadvancing relation page")
		}
		prev = row.ID
	}
	if p.More && p.Next != prev {
		return errors.New("invalid relation checkpoint")
	}
	return nil
}
func validateTweetPage(p TweetPage, cursor uint64) error {
	if len(p.Rows) > PageSize || (p.More && (len(p.Rows) == 0 || p.Next == 0)) {
		return errors.New("invalid tweet projection page")
	}
	prev := cursor
	for _, row := range p.Rows {
		if row.ID == 0 || (prev > 0 && row.ID >= prev) {
			return errors.New("nonadvancing tweet page")
		}
		prev = row.ID
	}
	if p.More && p.Next != prev {
		return errors.New("invalid tweet checkpoint")
	}
	return nil
}
