package repository

import (
	"context"
	"errors"
	"time"
)

type subjectWriteLease interface {
	AcquireSubjectWrite(context.Context, uint64) error
	ReleaseSubjectWrite(context.Context, uint64) error
}

type subjectWriteNotCommittedError struct {
	cause error
}

func (e *subjectWriteNotCommittedError) Error() string { return e.cause.Error() }
func (e *subjectWriteNotCommittedError) Unwrap() error { return e.cause }

// Only use after Mongo proves that no document matched and no write occurred.
func subjectWriteNotCommitted(err error) error {
	return &subjectWriteNotCommittedError{cause: err}
}

func withSubjectWriteLease(ctx context.Context, lease subjectWriteLease, userID uint64, write func() error) error {
	if lease == nil || write == nil || userID == 0 {
		return errors.New("subject write lease is invalid")
	}
	if err := lease.AcquireSubjectWrite(ctx, userID); err != nil {
		return err
	}
	writeErr := write()
	if writeErr != nil {
		var notCommitted *subjectWriteNotCommittedError
		if !errors.As(writeErr, &notCommitted) {
			// A failed Mongo call may still commit after the client loses its reply.
			// Retain the count until the write is reconciled before deletion.
			return writeErr
		}
	}
	releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := lease.ReleaseSubjectWrite(releaseCtx, userID); err != nil {
		return errors.Join(writeErr, err)
	}
	return writeErr
}
