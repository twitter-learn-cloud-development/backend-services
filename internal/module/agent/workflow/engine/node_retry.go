package engine

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"net"
	"time"

	"twitter-clone/internal/module/agent/workflow/dsl"
)

const (
	defaultNodeRetryInitialBackoff = 100 * time.Millisecond
	defaultNodeRetryMaxBackoff     = 5 * time.Second
	defaultNodeRetryMultiplier     = 2.0
)

// RetryableError is intentionally tiny so providers and governed tools can
// expose retry intent without making the workflow engine depend on them.
type RetryableError interface {
	IsRetryable() bool
}

type nodeRetryPolicy struct {
	maxAttempts int
	initial     time.Duration
	maximum     time.Duration
	multiplier  float64
	jitter      float64
}

func normalizeNodeRetryPolicy(source *dsl.RetryPolicyDSL) nodeRetryPolicy {
	policy := nodeRetryPolicy{
		maxAttempts: 1,
		initial:     defaultNodeRetryInitialBackoff,
		maximum:     defaultNodeRetryMaxBackoff,
		multiplier:  defaultNodeRetryMultiplier,
	}
	if source == nil {
		return policy
	}
	if source.MaxAttempts > 0 {
		policy.maxAttempts = source.MaxAttempts
	}
	if source.InitialBackoffMS > 0 {
		policy.initial = time.Duration(source.InitialBackoffMS) * time.Millisecond
	}
	if source.MaxBackoffMS > 0 {
		policy.maximum = time.Duration(source.MaxBackoffMS) * time.Millisecond
	}
	if policy.maximum < policy.initial {
		policy.maximum = policy.initial
	}
	if source.Multiplier > 0 {
		policy.multiplier = source.Multiplier
	}
	policy.jitter = source.Jitter
	return policy
}

func shouldRetryNode(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var suspension *SuspensionError
	if errors.As(err, &suspension) {
		return false
	}
	var retryable RetryableError
	if errors.As(err, &retryable) {
		return retryable.IsRetryable()
	}
	var networkError net.Error
	return errors.As(err, &networkError) && (networkError.Timeout() || networkError.Temporary())
}

func nodeRetryDelay(policy nodeRetryPolicy, nodeID string, failedAttempt int) time.Duration {
	if failedAttempt < 1 {
		failedAttempt = 1
	}
	delay := float64(policy.initial) * math.Pow(policy.multiplier, float64(failedAttempt-1))
	if delay > float64(policy.maximum) || math.IsInf(delay, 1) {
		delay = float64(policy.maximum)
	}
	if policy.jitter > 0 {
		hasher := fnv.New64a()
		_, _ = fmt.Fprintf(hasher, "%s:%d", nodeID, failedAttempt)
		unit := float64(hasher.Sum64()%1_000_001) / 1_000_000
		delay *= 1 + policy.jitter*((2*unit)-1)
	}
	if delay < 0 {
		return 0
	}
	return time.Duration(delay)
}

func waitNodeRetry(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
