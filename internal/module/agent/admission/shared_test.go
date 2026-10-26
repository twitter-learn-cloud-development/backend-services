package admission

import (
	"context"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
	"time"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

func TestSharedProviderCostReservationAndInvalidBudgetAreAtomic(t *testing.T) {
	cfg := DefaultConfig()
	cfg.OutputTokenLimit = 10
	cfg.CostMicrosPerToken = 2
	request := agentRuntime.ModelRequest{}
	input := agentRuntime.NewHeuristicTokenCounter().EstimateRequest(request).InputTokens
	cfg.MaxCostMicrosPerMinute = int64(input+10) * 2
	_, a, b := fixture(t, cfg)
	ctx := context.Background()
	require.NoError(t, a.Initialize(ctx))
	_, done, err := a.AcquireCall(ctx, "provider", &request)
	require.NoError(t, err)
	done() // releasing a slot cannot refund uncertain Provider consumption.
	_, _, err = b.AcquireCall(ctx, "provider", &request)
	require.ErrorIs(t, err, ErrBudgetExceeded)
	require.Zero(t, b.client.ZCard(ctx, b.key("provider:all")).Val())
	require.NoError(t, b.client.HSet(ctx, b.prefix+"provider-budget", "tokens", "broken").Err())
	_, _, err = b.AcquireCall(ctx, "provider", &request)
	require.ErrorIs(t, err, ErrUnavailable)
	require.Zero(t, b.client.ZCard(ctx, b.key("provider:all")).Val())
}

func TestSharedLeaseRenewalKeepsCapacityAcrossOriginalExpiry(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Lease = 6 * time.Second
	cfg.MaxRuns = 1
	s, a, b := fixture(t, cfg)
	ctx := context.Background()
	require.NoError(t, a.Initialize(ctx))
	leased, done, err := a.Acquire(ctx, agentRuntime.AdmissionRequest{UserID: 7})
	require.NoError(t, err)
	defer done()
	initial := a.client.ZRangeWithScores(ctx, a.key("runs"), 0, 0).Val()[0].Score
	s.SetTime(time.Now().Add(4 * time.Second))
	require.Eventually(t, func() bool {
		rows := a.client.ZRangeWithScores(ctx, a.key("runs"), 0, 0).Val()
		return len(rows) == 1 && rows[0].Score > initial
	}, 3*time.Second, 20*time.Millisecond)
	s.SetTime(time.Now().Add(7 * time.Second))
	_, _, err = b.Acquire(ctx, agentRuntime.AdmissionRequest{UserID: 8})
	require.ErrorIs(t, err, agentRuntime.ErrConcurrencyLimitExceeded)
	require.NoError(t, leased.Err())
}

func fixture(t *testing.T, config Config) (*miniredis.Miniredis, *Shared, *Shared) {
	t.Helper()
	server := miniredis.RunT(t)
	clients := []*redis.Client{redis.NewClient(&redis.Options{Addr: server.Addr()}), redis.NewClient(&redis.Options{Addr: server.Addr()})}
	for _, client := range clients {
		t.Cleanup(func() { require.NoError(t, client.Close()) })
	}
	a, err := New(clients[0], config, nil)
	require.NoError(t, err)
	b, err := New(clients[1], config, nil)
	require.NoError(t, err)
	return server, a, b
}
func TestSharedAdmissionMultiReplicaAtomicLimits(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxRuns = 1
	cfg.MaxPerUser = 1
	_, a, b := fixture(t, cfg)
	ctx := context.Background()
	_, _, err := a.Acquire(ctx, agentRuntime.AdmissionRequest{UserID: 7})
	require.ErrorIs(t, err, ErrUnavailable)
	require.NoError(t, a.Initialize(ctx))
	require.NoError(t, b.Ready(ctx))
	var wg sync.WaitGroup
	release := make(chan func(), 16)
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			gate := a
			if index%2 == 1 {
				gate = b
			}
			_, done, err := gate.Acquire(ctx, agentRuntime.AdmissionRequest{UserID: 7})
			if err == nil {
				release <- func() { done() }
			}
			errs <- err
		}(i)
	}
	wg.Wait()
	close(release)
	close(errs)
	won := 0
	for err := range errs {
		if err == nil {
			won++
		} else {
			require.ErrorIs(t, err, agentRuntime.ErrConcurrencyLimitExceeded)
		}
	}
	require.Equal(t, 1, won)
	for done := range release {
		done()
		done()
	}
	_, done, err := b.Acquire(ctx, agentRuntime.AdmissionRequest{UserID: 7})
	require.NoError(t, err)
	done()
}
func TestSharedAdmissionNestedWorkflowHasIndependentSlot(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxRuns = 1
	cfg.MaxPerWorkflow = 1
	_, a, b := fixture(t, cfg)
	ctx := context.Background()
	require.NoError(t, a.Initialize(ctx))
	parent, done, err := a.Acquire(ctx, agentRuntime.AdmissionRequest{UserID: 7})
	require.NoError(t, err)
	defer done()
	_, nested, err := a.Acquire(parent, agentRuntime.AdmissionRequest{UserID: 7})
	require.NoError(t, err)
	nested() // borrowed scope never releases parent's slot.
	child, childDone, err := a.Acquire(parent, agentRuntime.AdmissionRequest{WorkflowID: "wf"})
	require.NoError(t, err)
	require.NoError(t, child.Err())
	_, _, err = a.Acquire(parent, agentRuntime.AdmissionRequest{WorkflowID: "wf"})
	require.ErrorIs(t, err, agentRuntime.ErrConcurrencyLimitExceeded)
	childDone()
	_, _, err = b.Acquire(ctx, agentRuntime.AdmissionRequest{UserID: 8})
	require.ErrorIs(t, err, agentRuntime.ErrConcurrencyLimitExceeded)
}
func TestSharedLeaseExpiryCannotRenewOrReleaseReplacement(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxRuns = 1
	cfg.Lease = 6 * time.Second
	s, a, b := fixture(t, cfg)
	ctx := context.Background()
	require.NoError(t, a.Initialize(ctx))
	cancelCtx, cancel := context.WithCancel(ctx)
	_, old, err := a.Acquire(cancelCtx, agentRuntime.AdmissionRequest{UserID: 7})
	require.NoError(t, err)
	cancel()
	s.SetTime(time.Now().Add(7 * time.Second))
	s.FastForward(7 * time.Second)
	_, fresh, err := b.Acquire(ctx, agentRuntime.AdmissionRequest{UserID: 7})
	require.NoError(t, err)
	defer fresh()
	old()
	_, _, err = a.Acquire(ctx, agentRuntime.AdmissionRequest{UserID: 7})
	require.ErrorIs(t, err, agentRuntime.ErrConcurrencyLimitExceeded)
}
func TestSharedLeaseLossCancelsInFlightAndMissingPolicyFailsClosed(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Lease = 6 * time.Second
	s, a, _ := fixture(t, cfg)
	ctx := context.Background()
	require.NoError(t, a.Initialize(ctx))
	leased, done, err := a.Acquire(ctx, agentRuntime.AdmissionRequest{UserID: 7})
	require.NoError(t, err)
	defer done()
	require.NoError(t, a.client.Del(ctx, a.key("runs")).Err())
	select {
	case <-leased.Done():
	case <-time.After(4 * time.Second):
		t.Fatal("lost lease did not cancel execution")
	}
	require.NoError(t, a.client.Del(ctx, a.prefix+"ready").Err())
	_, _, err = a.Acquire(ctx, agentRuntime.AdmissionRequest{UserID: 7})
	require.ErrorIs(t, err, ErrUnavailable)
	require.False(t, s.Exists(a.prefix+"ready"))
}
func TestSharedProviderBudgetsConservativeAndScoped(t *testing.T) {
	cfg := DefaultConfig()
	cfg.OutputTokenLimit = 20
	cfg.MaxTokensPerMinute = 60
	cfg.MaxProviderCalls = 1
	cfg.MaxPerProvider = 1
	s, a, b := fixture(t, cfg)
	ctx := context.Background()
	require.NoError(t, a.Initialize(ctx))
	request := agentRuntime.ModelRequest{Messages: []agentRuntime.Message{{Role: agentRuntime.RoleUser, Content: "q"}}}
	_, done, err := a.AcquireCall(ctx, "provider", &request)
	require.NoError(t, err)
	require.Equal(t, 20, request.MaxOutputTokens)
	_, _, err = b.AcquireCall(ctx, "other", &request)
	require.ErrorIs(t, err, agentRuntime.ErrConcurrencyLimitExceeded)
	done()
	_, done, err = b.AcquireCall(ctx, "provider", &request)
	require.NoError(t, err)
	done()
	_, _, err = a.AcquireCall(ctx, "provider", &request)
	require.ErrorIs(t, err, ErrBudgetExceeded)
	s.SetTime(time.Now().Add(2 * time.Minute))
	_, done, err = a.AcquireCall(ctx, "provider", &request)
	require.NoError(t, err)
	done()
}
func TestSharedAdmissionInvalidStatePolicyAndMetrics(t *testing.T) {
	cfg := DefaultConfig()
	_, a, _ := fixture(t, cfg)
	ctx := context.Background()
	require.NoError(t, a.Initialize(ctx))
	otherCfg := cfg
	otherCfg.MaxRuns++
	other, err := New(a.client, otherCfg, nil)
	require.NoError(t, err)
	require.Error(t, other.Initialize(ctx))
	require.Error(t, other.Ready(ctx))
	require.NoError(t, a.client.Set(ctx, a.key("user:7"), "invalid", 0).Err())
	_, _, err = a.Acquire(ctx, agentRuntime.AdmissionRequest{UserID: 7})
	require.Error(t, err)
	require.Equal(t, int64(0), a.client.ZCard(ctx, a.key("runs")).Val())
	cfg.MaxCostMicrosPerMinute = 1
	require.Error(t, cfg.Validate())
	reg := prometheus.NewPedanticRegistry()
	m, err := NewMetrics(reg)
	require.NoError(t, err)
	m.Observe("user-42", "private Redis error")
	families, err := reg.Gather()
	require.NoError(t, err)
	for _, metric := range families[0].Metric {
		for _, label := range metric.Label {
			require.Equal(t, "unknown", label.GetValue())
		}
	}
}
