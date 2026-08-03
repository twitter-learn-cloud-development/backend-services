package runtime

import (
	"context"
	"errors"
	"testing"
)

func TestConcurrencyLimiterEnforcesUserAndWorkflowQuotas(t *testing.T) {
	limiter := NewInMemoryConcurrencyLimiter(ConcurrencyLimits{MaxPerUser: 2, MaxPerWorkflow: 1})
	ctx, release, err := limiter.Acquire(context.Background(), AdmissionRequest{UserID: 7, WorkflowID: "wf-a"})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	defer release()

	if _, _, err := limiter.Acquire(context.Background(), AdmissionRequest{UserID: 8, WorkflowID: "wf-a"}); !errors.Is(err, ErrConcurrencyLimitExceeded) {
		t.Fatalf("workflow Acquire() error = %v", err)
	}
	if _, nestedRelease, err := limiter.Acquire(ctx, AdmissionRequest{UserID: 7, WorkflowID: "wf-a"}); err != nil {
		t.Fatalf("nested Acquire() error = %v", err)
	} else {
		nestedRelease()
	}
}

func TestConcurrencyLimiterReleaseIsIdempotent(t *testing.T) {
	limiter := NewInMemoryConcurrencyLimiter(ConcurrencyLimits{MaxPerUser: 1})
	_, release, err := limiter.Acquire(context.Background(), AdmissionRequest{UserID: 7})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	release()
	release()

	if _, secondRelease, err := limiter.Acquire(context.Background(), AdmissionRequest{UserID: 7}); err != nil {
		t.Fatalf("Acquire() after release error = %v", err)
	} else {
		secondRelease()
	}
}
