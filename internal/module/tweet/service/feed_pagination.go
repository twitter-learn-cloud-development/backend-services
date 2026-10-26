package service

import (
	"context"
	"fmt"

	"go.uber.org/zap"
	"twitter-clone/internal/domain"
	"twitter-clone/pkg/logger"
)

const (
	feedBatchSize  = 100
	feedScanBudget = 1000
)

type feedCandidates struct {
	ids    []uint64
	tweets map[uint64]*domain.Tweet // pull mode already reads authoritative rows
}

func feedPageLimit(limit int) int {
	if limit <= 0 {
		return 20
	}
	if limit > 100 {
		return 100
	}
	return limit
}

func (s *TweetService) getFeedPage(ctx context.Context, userID, cursor uint64, limit int) ([]*domain.Tweet, uint64, bool, error) {
	if userID == 0 {
		return nil, 0, false, ErrInvalidAuthorID
	}
	if err := ctx.Err(); err != nil {
		return nil, 0, false, err
	}
	rows, next, more, err := s.getPullFeedPage(ctx, userID, cursor, feedPageLimit(limit), userID)
	if err != nil {
		return nil, 0, false, err
	}
	// Cache is a best-effort projection; neither stale associations nor cold
	// Inbox keys can prove source completeness or current follow permission.
	s.recoverFeedProjection(ctx, userID)
	if err := ctx.Err(); err != nil {
		return nil, 0, false, err
	}
	return rows, next, more, nil
}

func (s *TweetService) getPullFeedPage(ctx context.Context, userID, cursor uint64, limit int, viewer uint64) ([]*domain.Tweet, uint64, bool, error) {
	if userID != viewer || userID == 0 {
		return nil, 0, false, ErrInvalidAuthorID
	}
	if err := ctx.Err(); err != nil {
		return nil, 0, false, err
	}
	pager, ok := s.repo.(domain.FollowingFeedRepository)
	if !ok {
		return nil, 0, false, fmt.Errorf("following feed read port is unavailable")
	}
	if s.followRepo == nil {
		return nil, 0, false, fmt.Errorf("current follow read port is unavailable")
	}
	fetch := func(ctx context.Context, before uint64, count int) (feedCandidates, error) {
		rows, err := pager.ListFollowingFeed(ctx, viewer, before, count)
		if err != nil {
			return feedCandidates{}, fmt.Errorf("read current feed candidates: %w", err)
		}
		page := feedCandidates{tweets: make(map[uint64]*domain.Tweet, len(rows))}
		for _, row := range rows {
			if row == nil || page.tweets[row.ID] != nil {
				return feedCandidates{}, fmt.Errorf("invalid current feed candidate")
			}
			page.ids = append(page.ids, row.ID)
			page.tweets[row.ID] = row
		}
		return page, nil
	}
	return s.scanFeedPage(ctx, viewer, cursor, limit, fetch)
}

// Consume at most 1000 candidates, hydrating <=100 current MySQL rows per batch.
// A raw lookahead is not consumed. Empty filtered pages still advance before.
func (s *TweetService) scanFeedPage(ctx context.Context, viewer, cursor uint64, limit int, fetch func(context.Context, uint64, int) (feedCandidates, error)) ([]*domain.Tweet, uint64, bool, error) {
	result := make([]*domain.Tweet, 0, limit)
	before := cursor
	scanned := 0
	finish := func(more bool) ([]*domain.Tweet, uint64, bool, error) {
		if err := ctx.Err(); err != nil {
			return nil, 0, false, err
		}
		var next uint64
		if more {
			next = before
		}
		s.populateTweetStats(ctx, result, viewer)
		if err := ctx.Err(); err != nil {
			return nil, 0, false, err
		}
		logger.Info(ctx, "feed page scanned", zap.Int("candidates", scanned), zap.Int("returned", len(result)), zap.Bool("has_more", more))
		return result, next, more, nil
	}
	for scanned < feedScanBudget {
		if err := ctx.Err(); err != nil {
			return nil, 0, false, err
		}
		batch := feedBatchSize
		if batch > feedScanBudget-scanned {
			batch = feedScanBudget - scanned
		}
		page, err := fetch(ctx, before, batch+1)
		if err != nil {
			return nil, 0, false, err
		}
		if len(page.ids) > batch+1 {
			return nil, 0, false, fmt.Errorf("oversized feed candidate page")
		}
		previous := before
		for _, id := range page.ids {
			if id == 0 || (previous > 0 && id >= previous) {
				return nil, 0, false, fmt.Errorf("invalid feed candidate order")
			}
			previous = id
		}
		hasLookahead := len(page.ids) > batch
		ids := page.ids
		if hasLookahead {
			ids = ids[:batch]
		}
		if page.tweets == nil && len(ids) > 0 {
			rows, err := s.repo.GetByIDs(ctx, ids) // Current facts, not stale body caches.
			if err != nil {
				return nil, 0, false, fmt.Errorf("read current feed tweets: %w", err)
			}
			page.tweets = make(map[uint64]*domain.Tweet, len(rows))
			requested := make(map[uint64]bool, len(ids))
			for _, id := range ids {
				requested[id] = true
			}
			for _, row := range rows {
				if row == nil || !requested[row.ID] || page.tweets[row.ID] != nil {
					return nil, 0, false, fmt.Errorf("invalid current feed rows")
				}
				page.tweets[row.ID] = row
			}
		}
		authors := make([]uint64, 0, len(ids))
		seenAuthors := make(map[uint64]bool)
		for _, id := range ids {
			if row := page.tweets[id]; row != nil && row.UserID != viewer && !seenAuthors[row.UserID] {
				seenAuthors[row.UserID] = true
				authors = append(authors, row.UserID)
			}
		}
		followed := map[uint64]bool{}
		if len(authors) > 0 {
			reader := s.followRepo
			if reader == nil {
				return nil, 0, false, fmt.Errorf("current follow read port is unavailable")
			}
			followed, err = reader.BatchIsFollowing(ctx, viewer, authors)
			if err != nil {
				return nil, 0, false, fmt.Errorf("revalidate current feed follows: %w", err)
			}
		}
		for _, id := range ids {
			if len(result) == limit {
				return finish(true)
			}
			before = id
			scanned++
			row := page.tweets[id]
			if row == nil || row.UserID == 0 || row.DeletedAt != 0 || (row.UserID != viewer && (!followed[row.UserID] || (row.VisibleType != domain.VisiblePublic && row.VisibleType != domain.VisibleFollows))) {
				continue
			}
			// Copy before enriching so repository/cache objects are not mutated.
			copyRow := *row
			result = append(result, &copyRow)
		}
		if !hasLookahead {
			return finish(false)
		}
		if len(result) == limit || scanned == feedScanBudget {
			return finish(true)
		}
	}
	return finish(true)
}
