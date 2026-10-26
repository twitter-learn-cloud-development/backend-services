package repository

import (
	"context"
	"errors"
	"testing"
)

type subjectWriteLeaseFake struct {
	acquireErr error
	releaseErr error
	active     bool
	acquires   int
	releases   int
}

func (f *subjectWriteLeaseFake) AcquireSubjectWrite(_ context.Context, _ uint64) error {
	f.acquires++
	if f.acquireErr == nil {
		f.active = true
	}
	return f.acquireErr
}

func (f *subjectWriteLeaseFake) ReleaseSubjectWrite(ctx context.Context, _ uint64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.releases++
	f.active = false
	return f.releaseErr
}

func TestSubjectWriteLeaseReleasesConfirmedWriteAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	lease := &subjectWriteLeaseFake{}
	err := withSubjectWriteLease(ctx, lease, 42, func() error {
		if !lease.active {
			t.Fatal("write began without an active lease")
		}
		cancel()
		return nil
	})
	if err != nil || lease.active || lease.acquires != 1 || lease.releases != 1 {
		t.Fatalf("lease was not released after cancellation: err=%v lease=%+v", err, lease)
	}
}

func TestSubjectWriteLeaseRetainsCountAfterAmbiguousWriteFailure(t *testing.T) {
	lease := &subjectWriteLeaseFake{}
	writeErr := errors.New("write acknowledgement lost")
	if err := withSubjectWriteLease(context.Background(), lease, 42, func() error {
		return writeErr
	}); !errors.Is(err, writeErr) || !lease.active || lease.releases != 0 {
		t.Fatalf("ambiguous write lost its fence blocker: err=%v lease=%+v", err, lease)
	}
}

func TestSubjectWriteLeaseReleasesProvenNoWrite(t *testing.T) {
	lease := &subjectWriteLeaseFake{}
	conflict := errors.New("no matching document")
	err := withSubjectWriteLease(context.Background(), lease, 42, func() error {
		return subjectWriteNotCommitted(conflict)
	})
	if !errors.Is(err, conflict) || lease.active || lease.acquires != 1 || lease.releases != 1 {
		t.Fatalf("proven no-write held the subject count: err=%v lease=%+v", err, lease)
	}
	releaseFailure := errors.New("release failed")
	lease = &subjectWriteLeaseFake{releaseErr: releaseFailure}
	err = withSubjectWriteLease(context.Background(), lease, 42, func() error {
		return subjectWriteNotCommitted(conflict)
	})
	if !errors.Is(err, conflict) || !errors.Is(err, releaseFailure) {
		t.Fatalf("lost no-write or release error: %v", err)
	}
}

func TestSubjectWriteLeaseRejectsFencedSubjectAndReportsReleaseFailure(t *testing.T) {
	blocked := &subjectWriteLeaseFake{acquireErr: ErrDataDeletionSubjectWriteFenced}
	called := false
	if err := withSubjectWriteLease(context.Background(), blocked, 42, func() error {
		called = true
		return nil
	}); !errors.Is(err, ErrDataDeletionSubjectWriteFenced) || called || blocked.releases != 0 {
		t.Fatalf("fenced write was attempted: err=%v lease=%+v", err, blocked)
	}
	broken := errors.New("release failed")
	lease := &subjectWriteLeaseFake{releaseErr: broken}
	if err := withSubjectWriteLease(context.Background(), lease, 42, func() error { return nil }); !errors.Is(err, broken) {
		t.Fatalf("release failure hidden: %v", err)
	}
}
