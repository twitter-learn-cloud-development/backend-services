package service

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"twitter-clone/internal/domain"
)

type relationPager struct {
	domain.FollowRepository
	rows    []domain.Follow
	limits  []int
	cursors []uint64
	err     error
}

func (p *relationPager) page(ctx context.Context, cursor uint64, limit int, followers bool) ([]uint64, uint64, bool, error) {
	p.limits = append(p.limits, limit)
	p.cursors = append(p.cursors, cursor)
	if ctx.Err() != nil {
		return nil, 0, false, ctx.Err()
	}
	if p.err != nil {
		return nil, 0, false, p.err
	}
	rows := []domain.Follow{}
	for _, row := range p.rows {
		if row.DeletedAt == 0 && (cursor == 0 || row.ID < cursor) {
			rows = append(rows, row)
		}
	}
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	ids := make([]uint64, len(rows))
	for i, row := range rows {
		ids[i] = row.FolloweeID
		if followers {
			ids[i] = row.FollowerID
		}
	}
	var next uint64
	if more {
		next = rows[len(rows)-1].ID
	}
	return ids, next, more, nil
}
func (p *relationPager) ListFollowerPage(ctx context.Context, _ uint64, cursor uint64, limit int) (domain.FollowerPage, error) {
	ids, next, more, err := p.page(ctx, cursor, limit, true)
	return domain.FollowerPage{FollowerIDs: ids, NextCursor: next, HasMore: more}, err
}
func (p *relationPager) ListFolloweePage(ctx context.Context, _ uint64, cursor uint64, limit int) (domain.FolloweePage, error) {
	ids, next, more, err := p.page(ctx, cursor, limit, false)
	return domain.FolloweePage{FolloweeIDs: ids, NextCursor: next, HasMore: more}, err
}

func TestFollowServicePagesByRelationIDWithoutGaps(t *testing.T) {
	for _, followers := range []bool{true, false} {
		pager := &relationPager{rows: []domain.Follow{
			{ID: 9000, FollowerID: 400, FolloweeID: 30}, {ID: 8000, FollowerID: 2, FolloweeID: 900},
			{ID: 7000, FollowerID: 90000, FolloweeID: 7, DeletedAt: 1}, {ID: 6000, FollowerID: 90, FolloweeID: 2},
			{ID: 5000, FollowerID: 1, FolloweeID: 8000},
		}}
		svc := NewFollowService(pager, nil, nil, nil)
		read := svc.GetFollowees
		expected := []uint64{30, 900, 2, 8000}
		if followers {
			read = svc.GetFollowers
			expected = []uint64{400, 2, 90, 1}
		}
		first, cursor, more, err := read(context.Background(), 42, 0, 2)
		if err != nil || !more || cursor != 8000 {
			t.Fatalf("first relation page: %v %d %v %v", first, cursor, more, err)
		}
		// A newly inserted higher relation does not enter an existing next page.
		pager.rows = append([]domain.Follow{{ID: 10000, FollowerID: 10, FolloweeID: 100}}, pager.rows...)
		last, next, more, err := read(context.Background(), 42, cursor, 2)
		if err != nil || more || next != 0 || !reflect.DeepEqual(append(first, last...), expected) {
			t.Fatalf("second page lost or repeated users: %v %d %v %v", last, next, more, err)
		}
		if !reflect.DeepEqual(pager.cursors, []uint64{0, 8000}) {
			t.Fatalf("wrong cursor passed: %v", pager.cursors)
		}
	}
}

func TestFollowServicePageSizeAndLookahead(t *testing.T) {
	for _, followers := range []bool{true, false} {
		for _, input := range []int{-1, 0, 1, 100, 101, 1000000} {
			pager := &relationPager{}
			for i := 0; i < 101; i++ {
				pager.rows = append(pager.rows, domain.Follow{ID: uint64(10000 - i), FollowerID: uint64(i + 1), FolloweeID: uint64(500 + i)})
			}
			svc := NewFollowService(pager, nil, nil, nil)
			read := svc.GetFollowees
			if followers {
				read = svc.GetFollowers
			}
			want := input
			if want <= 0 {
				want = 20
			}
			if want > 100 {
				want = 100
			}
			ids, cursor, more, err := read(context.Background(), 42, 0, input)
			if err != nil || len(ids) != want || !more || cursor != pager.rows[want-1].ID || pager.limits[0] != want {
				t.Fatalf("limit %d: %v %d %v %v", input, ids, cursor, more, err)
			}
		}
	}
}

func TestFollowServicePaginationErrorsAndEmpty(t *testing.T) {
	for _, followers := range []bool{true, false} {
		pager := &relationPager{}
		svc := NewFollowService(pager, nil, nil, nil)
		read := svc.GetFollowees
		if followers {
			read = svc.GetFollowers
		}
		ids, cursor, more, err := read(context.Background(), 42, 0, 20)
		if err != nil || ids == nil || len(ids) != 0 || cursor != 0 || more {
			t.Fatalf("empty: %v %d %v %v", ids, cursor, more, err)
		}
		_, _, _, err = read(context.Background(), 0, 0, 20)
		if !errors.Is(err, ErrInvalidFollowPageRequest) {
			t.Fatalf("zero owner: %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, _, _, err = read(ctx, 42, 0, 20)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel: %v", err)
		}
		if len(pager.limits) != 1 {
			t.Fatal("invalid/canceled requests reached repository")
		}
		failure := errors.New("query failure")
		pager.err = failure
		_, _, _, err = read(context.Background(), 42, 0, 20)
		if !errors.Is(err, failure) {
			t.Fatalf("swallowed error: %v", err)
		}
		missing := NewFollowService(nil, nil, nil, nil)
		read = missing.GetFollowees
		if followers {
			read = missing.GetFollowers
		}
		_, _, _, err = read(context.Background(), 42, 0, 20)
		if !errors.Is(err, ErrFollowPaginationUnavailable) {
			t.Fatalf("IDs-only fallback: %v", err)
		}
	}
}

func TestFollowServiceRejectsBrokenPage(t *testing.T) {
	for _, tc := range []struct {
		ids  []uint64
		next uint64
		more bool
	}{
		{[]uint64{1, 2, 3}, 70, false}, {[]uint64{1}, 0, true}, {[]uint64{1, 2}, 90, true},
		{[]uint64{1, 2}, 100, true}, {[]uint64{1, 2}, 10, false}, {[]uint64{0}, 0, false},
	} {
		if err := validateFollowPage(tc.ids, tc.next, tc.more, 90, 2); !errors.Is(err, ErrInvalidFollowPage) {
			t.Fatalf("bad page accepted: %+v", tc)
		}
	}
}
