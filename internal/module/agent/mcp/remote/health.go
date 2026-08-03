package remote

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"

	agentCredential "twitter-clone/internal/module/agent/credential"
	agentModel "twitter-clone/internal/module/agent/model"
)

const (
	HealthOutcomeHealthy = "healthy"
	HealthOutcomeFailed  = "failed"
	HealthOutcomeSkipped = "skipped"
)

type HealthCheckConfig struct {
	Enabled             bool
	PollInterval        time.Duration
	HealthyInterval     time.Duration
	Timeout             time.Duration
	LeaseDuration       time.Duration
	FailureBackoffMin   time.Duration
	FailureBackoffMax   time.Duration
	FailureThreshold    int64
	BatchSize           int
	MaxConcurrentChecks int
}

func DefaultHealthCheckConfig() HealthCheckConfig {
	return HealthCheckConfig{
		PollInterval:        15 * time.Second,
		HealthyInterval:     2 * time.Minute,
		Timeout:             5 * time.Second,
		LeaseDuration:       15 * time.Second,
		FailureBackoffMin:   30 * time.Second,
		FailureBackoffMax:   15 * time.Minute,
		FailureThreshold:    3,
		BatchSize:           20,
		MaxConcurrentChecks: 4,
	}
}

func normalizeHealthCheckConfig(config HealthCheckConfig) HealthCheckConfig {
	defaults := DefaultHealthCheckConfig()
	if config.PollInterval <= 0 {
		config.PollInterval = defaults.PollInterval
	}
	if config.HealthyInterval <= 0 {
		config.HealthyInterval = defaults.HealthyInterval
	}
	if config.Timeout <= 0 {
		config.Timeout = defaults.Timeout
	}
	if config.LeaseDuration <= config.Timeout {
		config.LeaseDuration = maxDuration(defaults.LeaseDuration, 2*config.Timeout)
	}
	if config.FailureBackoffMin <= 0 {
		config.FailureBackoffMin = defaults.FailureBackoffMin
	}
	if config.FailureBackoffMax < config.FailureBackoffMin {
		config.FailureBackoffMax = maxDuration(defaults.FailureBackoffMax, config.FailureBackoffMin)
	}
	if config.FailureThreshold <= 0 {
		config.FailureThreshold = defaults.FailureThreshold
	}
	if config.BatchSize <= 0 || config.BatchSize > 100 {
		config.BatchSize = defaults.BatchSize
	}
	if config.MaxConcurrentChecks <= 0 || config.MaxConcurrentChecks > config.BatchSize {
		config.MaxConcurrentChecks = minInt(defaults.MaxConcurrentChecks, config.BatchSize)
	}
	return config
}

type HealthObserver interface {
	RecordHealthCheck(transport, result, errorCode string, duration time.Duration)
	RecordHealthCycle(result string, claimed int)
}

func (manager *Manager) Start(ctx context.Context) {
	if manager == nil {
		return
	}
	manager.healthMu.Lock()
	defer manager.healthMu.Unlock()
	if manager.healthStarted {
		return
	}
	manager.healthStarted = true
	manager.healthConfig = normalizeHealthCheckConfig(manager.healthConfig)
	if !manager.enabled || !manager.healthConfig.Enabled || manager.healthStore == nil || manager.healthProber == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	healthCtx, cancel := context.WithCancel(ctx)
	manager.healthCancel = cancel
	if manager.healthOwner == "" {
		owner, err := manager.newID("mcphealth")
		if err != nil {
			owner = fmt.Sprintf("mcphealth_%d", manager.now().UnixNano())
		}
		manager.healthOwner = owner
	}
	manager.healthWG.Add(1)
	go manager.healthLoop(healthCtx)
}

func (manager *Manager) Close() error {
	if manager == nil {
		return nil
	}
	var closeErr error
	manager.closeOnce.Do(func() {
		manager.healthMu.Lock()
		cancel := manager.healthCancel
		manager.healthMu.Unlock()
		if cancel != nil {
			cancel()
		}
		manager.healthWG.Wait()
		if maintainer := manager.poolMaintainer(); maintainer != nil {
			closeErr = maintainer.Close()
		}
	})
	return closeErr
}

func (manager *Manager) healthLoop(ctx context.Context) {
	defer manager.healthWG.Done()
	manager.runHealthCycle(ctx)
	ticker := time.NewTicker(manager.healthConfig.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			manager.runHealthCycle(ctx)
		}
	}
}

func (manager *Manager) runHealthCycle(ctx context.Context) {
	if maintainer := manager.poolMaintainer(); maintainer != nil {
		maintainer.Prune()
	}
	now := manager.now().UTC()
	connections, err := manager.healthStore.ClaimMCPConnectionsForHealth(
		ctx,
		manager.healthOwner,
		now,
		now.Add(manager.healthConfig.LeaseDuration),
		manager.healthConfig.BatchSize,
	)
	if err != nil {
		manager.observeHealthCycle("claim_failed", 0)
		return
	}
	if len(connections) == 0 {
		manager.observeHealthCycle("empty", 0)
		return
	}

	workers := minInt(manager.healthConfig.MaxConcurrentChecks, len(connections))
	jobs := make(chan *Connection)
	var wait sync.WaitGroup
	wait.Add(workers)
	for index := 0; index < workers; index++ {
		go func() {
			defer wait.Done()
			for connection := range jobs {
				manager.checkConnectionHealth(ctx, connection)
			}
		}()
	}
	for _, connection := range connections {
		select {
		case <-ctx.Done():
			close(jobs)
			wait.Wait()
			manager.observeHealthCycle("canceled", len(connections))
			return
		case jobs <- connection:
		}
	}
	close(jobs)
	wait.Wait()
	manager.observeHealthCycle("completed", len(connections))
}

func (manager *Manager) checkConnectionHealth(ctx context.Context, connection *Connection) {
	if connection == nil {
		return
	}
	startedAt := manager.now()
	credential, err := manager.openCredential(ctx, connection)
	credentialFailed := err != nil
	if err == nil {
		checkCtx, cancel := context.WithTimeout(ctx, manager.healthConfig.Timeout)
		err = manager.healthProber.Ping(checkCtx, connectionDiscoveryRequest(connection, credential))
		cancel()
	}
	if ctx.Err() != nil {
		return
	}
	checkedAt := manager.now().UTC()
	completion := HealthCheckCompletion{
		ConnectionID: connection.ID,
		UserID:       connection.UserID,
		LeaseOwner:   manager.healthOwner,
		CheckedAt:    checkedAt,
	}
	result := HealthOutcomeHealthy
	errorCode := "none"
	switch {
	case err == nil:
		completion.Outcome = HealthOutcomeHealthy
		completion.HealthStatus = HealthStatusHealthy
		completion.LastHealthyAt = checkedAt
		completion.NextHealthCheckAt = checkedAt.Add(
			stableHealthJitter(connection.ID, manager.healthConfig.HealthyInterval),
		)
	case errors.Is(err, ErrClientPoolSaturated):
		completion.Outcome = HealthOutcomeSkipped
		completion.HealthStatus = normalizedHealthStatus(connection.HealthStatus)
		completion.FailureCount = connection.HealthFailureCount
		completion.NextHealthCheckAt = checkedAt.Add(manager.healthConfig.PollInterval)
		result = HealthOutcomeSkipped
		errorCode = "pool_saturated"
	default:
		completion.Outcome = HealthOutcomeFailed
		completion.FailureCount = connection.HealthFailureCount + 1
		completion.HealthStatus = HealthStatusDegraded
		if completion.FailureCount >= manager.healthConfig.FailureThreshold {
			completion.HealthStatus = HealthStatusUnhealthy
		}
		completion.ErrorCode = healthErrorCode(err)
		if credentialFailed {
			completion.ErrorCode = "credential_unavailable"
		}
		completion.NextHealthCheckAt = checkedAt.Add(
			stableHealthJitter(connection.ID, manager.failureBackoff(completion.FailureCount)),
		)
		result = HealthOutcomeFailed
		errorCode = completion.ErrorCode
	}
	if persistErr := manager.healthStore.CompleteMCPConnectionHealth(ctx, completion); persistErr != nil {
		manager.observeHealthCheck(connection.Transport, "persist_failed", "store_error", manager.now().Sub(startedAt))
		return
	}
	manager.observeHealthCheck(connection.Transport, result, errorCode, manager.now().Sub(startedAt))
}

func (manager *Manager) failureBackoff(failures int64) time.Duration {
	if failures < 1 {
		failures = 1
	}
	shift := failures - 1
	if shift > 10 {
		shift = 10
	}
	delay := manager.healthConfig.FailureBackoffMin * time.Duration(1<<shift)
	if delay > manager.healthConfig.FailureBackoffMax {
		return manager.healthConfig.FailureBackoffMax
	}
	return delay
}

func (manager *Manager) invalidate(request DiscoveryRequest) {
	if invalidator, ok := manager.discoverer.(ConnectionInvalidator); ok {
		invalidator.Invalidate(request)
		return
	}
	if invalidator, ok := manager.caller.(ConnectionInvalidator); ok {
		invalidator.Invalidate(request)
	}
}

func (manager *Manager) resetHealth(ctx context.Context, connection *Connection) {
	if manager.healthStore == nil || connection == nil {
		return
	}
	if err := manager.healthStore.ResetMCPConnectionHealth(
		ctx,
		connection.ID,
		connection.UserID,
		manager.now().UTC(),
	); err != nil {
		manager.observeHealthCycle("reset_failed", 0)
		return
	}
	connection.HealthStatus = HealthStatusUnknown
	connection.HealthErrorCode = ""
	connection.HealthFailureCount = 0
	connection.LastHealthCheckedAt = time.Time{}
	connection.LastHealthyAt = time.Time{}
	connection.NextHealthCheckAt = manager.now().UTC()
}

func (manager *Manager) poolMaintainer() PoolMaintainer {
	if maintainer, ok := manager.discoverer.(PoolMaintainer); ok {
		return maintainer
	}
	if maintainer, ok := manager.caller.(PoolMaintainer); ok {
		return maintainer
	}
	return nil
}

func (manager *Manager) observeHealthCheck(transport, result, errorCode string, duration time.Duration) {
	if manager.healthObserver != nil {
		manager.healthObserver.RecordHealthCheck(transport, result, errorCode, duration)
	}
}

func (manager *Manager) observeHealthCycle(result string, claimed int) {
	if manager.healthObserver != nil {
		manager.healthObserver.RecordHealthCycle(result, claimed)
	}
}

func connectionDiscoveryRequest(connection *Connection, credential resolvedConnectionCredential) DiscoveryRequest {
	if connection == nil {
		return DiscoveryRequest{}
	}
	return DiscoveryRequest{
		ConnectionID: connection.ID, CredentialVersion: connection.CredentialVersion,
		CredentialIdentity: credential.identity,
		Transport:          connection.Transport, Endpoint: connection.Endpoint, BearerToken: credential.bearerToken,
	}
}

func normalizedHealthStatus(status string) string {
	switch status {
	case HealthStatusHealthy, HealthStatusDegraded, HealthStatusUnhealthy:
		return status
	default:
		return HealthStatusUnknown
	}
}

func healthErrorCode(err error) string {
	switch {
	case err == nil:
		return "none"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, ErrClientPoolClosed):
		return "pool_closed"
	case errors.Is(err, ErrConnectionInvalidated):
		return "session_invalidated"
	case errors.Is(err, agentCredential.ErrSecretCipherUnavailable):
		return "credential_unavailable"
	case errors.Is(err, agentModel.ErrEndpointNotAllowed):
		return "endpoint_not_allowed"
	default:
		return "connection_failed"
	}
}

func stableHealthJitter(connectionID string, base time.Duration) time.Duration {
	if base <= 0 {
		return 0
	}
	digest := sha256.Sum256([]byte("external-mcp-health:v1:" + connectionID))
	fraction := binary.BigEndian.Uint64(digest[:8]) % 1001
	jitter := (base / 10) * time.Duration(fraction) / 1000
	return base + jitter
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
