package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"

	"twitter-clone/internal/module/agent/eval"
)

func TestAgentTaskLiveRedisLedgerRequiresInitializationAndSerializesInstances(t *testing.T) {
	server := miniredis.RunT(t)
	config := testAgentTaskLiveRedisConfig(server.Addr())
	client, err := newAgentTaskLiveRedisClient(config)
	if err != nil {
		t.Fatalf("connect redis ledger: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	now := time.Now().UTC()
	authorization := testAgentTaskLiveRedisAuthorization(t, now, agentTaskLiveAuthorizationLimits{
		MaxRuns: 1, MaxProviderCalls: 8, MaxCapturedOutputs: 0, MaxEstimatedCostMicros: 8,
	})
	ledger, err := newAgentTaskLiveRedisLedger(client, config, authorization)
	if err != nil {
		t.Fatalf("create redis ledger: %v", err)
	}
	if err := ledger.ReserveRunContext(t.Context(), 0, now); err == nil || !strings.Contains(err.Error(), "not initialized") {
		t.Fatalf("uninitialized redis ledger accepted a run: %v", err)
	}
	created, err := ledger.Initialize(t.Context())
	if err != nil || !created {
		t.Fatalf("initialize redis ledger: created=%t err=%v", created, err)
	}
	created, err = ledger.Initialize(t.Context())
	if err != nil || created {
		t.Fatalf("idempotent redis initialization: created=%t err=%v", created, err)
	}
	if err := ledger.ReserveRunContext(t.Context(), 0, time.Now().UTC()); err != nil {
		t.Fatalf("reserve redis run: %v", err)
	}

	ledgers := make([]*agentTaskLiveRedisLedger, 8)
	for index := range ledgers {
		instanceClient, clientErr := newAgentTaskLiveRedisClient(config)
		if clientErr != nil {
			t.Fatalf("connect redis ledger instance %d: %v", index, clientErr)
		}
		t.Cleanup(func() { _ = instanceClient.Close() })
		ledgers[index], err = newAgentTaskLiveRedisLedger(instanceClient, config, authorization)
		if err != nil {
			t.Fatalf("create redis ledger instance %d: %v", index, err)
		}
	}
	start := make(chan struct{})
	errorsByCall := make(chan error, len(ledgers))
	var wait sync.WaitGroup
	for index, current := range ledgers {
		wait.Add(1)
		go func(index int, current *agentTaskLiveRedisLedger) {
			defer wait.Done()
			<-start
			errorsByCall <- current.ReserveProviderCallContext(
				context.Background(), fmt.Sprintf("case-%d", index), 1, time.Now().UTC(),
			)
		}(index, current)
	}
	close(start)
	wait.Wait()
	close(errorsByCall)
	for callErr := range errorsByCall {
		if callErr != nil {
			t.Fatalf("concurrent redis reservation: %v", callErr)
		}
	}
	if err := ledger.ReserveProviderCallContext(t.Context(), "overflow", 0, time.Now().UTC()); err == nil ||
		!strings.Contains(err.Error(), "provider call budget exhausted") {
		t.Fatalf("redis provider budget overflow was accepted: %v", err)
	}

	usage, err := client.HMGet(t.Context(), ledger.stateKey, "runs", "provider_calls", "estimated_cost_micros", "sequence").Result()
	if err != nil {
		t.Fatalf("read redis usage: %v", err)
	}
	if fmt.Sprint(usage) != "[1 8 8 9]" {
		t.Fatalf("redis usage = %v", usage)
	}
	eventCount, err := client.XLen(t.Context(), ledger.eventsKey).Result()
	if err != nil || eventCount != 10 {
		t.Fatalf("redis audit events = %d, err=%v", eventCount, err)
	}
	evidence := ledger.Evidence()
	if evidence.StateBackend != "redis" || len(evidence.StateNamespaceSHA256) != 64 {
		t.Fatalf("redis state evidence = %+v", evidence)
	}
}

func TestAgentTaskLiveRedisLedgerReservationIsIdempotent(t *testing.T) {
	server := miniredis.RunT(t)
	config := testAgentTaskLiveRedisConfig(server.Addr())
	client, err := newAgentTaskLiveRedisClient(config)
	if err != nil {
		t.Fatalf("connect redis ledger: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	now := time.Now().UTC()
	authorization := testAgentTaskLiveRedisAuthorization(t, now, agentTaskLiveAuthorizationLimits{
		MaxRuns: 1, MaxProviderCalls: 2, MaxCapturedOutputs: 0, MaxEstimatedCostMicros: 3,
	})
	ledger, err := newAgentTaskLiveRedisLedger(client, config, authorization)
	if err != nil {
		t.Fatalf("create redis ledger: %v", err)
	}
	if _, err := ledger.Initialize(t.Context()); err != nil {
		t.Fatalf("initialize redis ledger: %v", err)
	}
	record := agentTaskLiveLedgerRecord{
		EventType: "provider_call_reserved", ProviderCalls: 1, EstimatedCostMicros: 2,
		SubjectSHA256: hashAgentTaskLiveAuthorizationSubject("case-1"), CreatedAt: time.Now().UTC(),
	}
	if err := ledger.reserveWithID(t.Context(), record, "reservation-retry-001"); err != nil {
		t.Fatalf("first reservation: %v", err)
	}
	if err := ledger.reserveWithID(t.Context(), record, "reservation-retry-001"); err != nil {
		t.Fatalf("idempotent reservation retry: %v", err)
	}
	record.EstimatedCostMicros = 1
	if err := ledger.reserveWithID(t.Context(), record, "reservation-retry-001"); err == nil ||
		!strings.Contains(err.Error(), "identity conflict") {
		t.Fatalf("conflicting reservation retry was accepted: %v", err)
	}
	invalidRecord := record
	invalidRecord.EventType = "run_reserved"
	invalidRecord.Runs = 1
	invalidRecord.ProviderCalls = 1
	invalidRecord.EstimatedCostMicros = 0
	if err := ledger.reserveWithID(t.Context(), invalidRecord, "reservation-invalid-go-001"); err == nil ||
		!strings.Contains(err.Error(), "run reservation delta is invalid") {
		t.Fatalf("invalid Go reservation shape was accepted: %v", err)
	}
	invalidRecord = record
	invalidRecord.SubjectSHA256 = "invalid"
	if err := ledger.reserveWithID(t.Context(), invalidRecord, "reservation-invalid-subject-001"); err == nil ||
		!strings.Contains(err.Error(), "subject digest is invalid") {
		t.Fatalf("invalid reservation subject digest was accepted: %v", err)
	}
	malformedArguments := append(
		ledger.identityArguments(),
		"reservation-invalid-lua-001",
		strings.Repeat("b", 64),
		hashAgentTaskLiveAuthorizationSubject(ledger.invocationID),
		"provider_call_reserved",
		hashAgentTaskLiveAuthorizationSubject("malformed-lua-subject"),
		"1", "1", "0", "0",
	)
	operationCtx, cancel := ledger.operationContext(t.Context())
	_, err = reserveAgentTaskLiveRedisLedgerScript.Run(
		operationCtx,
		ledger.client,
		[]string{ledger.stateKey, ledger.eventsKey, ledger.markerKey},
		malformedArguments...,
	).Int()
	cancel()
	if err == nil || !strings.Contains(err.Error(), "provider call reservation delta is invalid") {
		t.Fatalf("invalid Lua reservation shape was accepted: %v", err)
	}
	usage, err := client.HMGet(t.Context(), ledger.stateKey, "provider_calls", "estimated_cost_micros", "sequence").Result()
	if err != nil || fmt.Sprint(usage) != "[1 2 1]" {
		t.Fatalf("idempotent redis usage = %v, err=%v", usage, err)
	}
}

func TestAgentTaskLiveRedisLedgerInspectAndRevokePreservesUsage(t *testing.T) {
	server := miniredis.RunT(t)
	config := testAgentTaskLiveRedisConfig(server.Addr())
	client, err := newAgentTaskLiveRedisClient(config)
	if err != nil {
		t.Fatalf("connect redis ledger: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	now := time.Now().UTC()
	authorization := testAgentTaskLiveRedisAuthorization(t, now, agentTaskLiveAuthorizationLimits{
		MaxRuns: 2, MaxProviderCalls: 2, MaxCapturedOutputs: 1, MaxEstimatedCostMicros: 10,
	})
	ledger, err := newAgentTaskLiveRedisLedger(client, config, authorization)
	if err != nil {
		t.Fatalf("create redis ledger: %v", err)
	}
	if _, err := ledger.Initialize(t.Context()); err != nil {
		t.Fatalf("initialize redis ledger: %v", err)
	}
	if err := ledger.ReserveRunContext(t.Context(), 1, time.Now().UTC()); err != nil {
		t.Fatalf("reserve redis run: %v", err)
	}
	if err := ledger.ReserveProviderCallContext(t.Context(), "case-1", 4, time.Now().UTC()); err != nil {
		t.Fatalf("reserve redis provider call: %v", err)
	}

	before, err := ledger.Inspect(t.Context())
	if err != nil {
		t.Fatalf("inspect initialized redis ledger: %v", err)
	}
	if before.Status != "initialized" || before.IntegrityStatus != "intact" || before.Usage == nil || before.Audit == nil ||
		before.Usage.Runs != 1 || before.Usage.ProviderCalls != 1 || before.Usage.CapturedOutputs != 1 ||
		before.Usage.EstimatedCostMicros != 4 || before.Audit.Sequence != 2 || before.Audit.EventCount != 3 ||
		before.Audit.ReplayStatus != "verified" || before.Audit.VerifiedEventCount != 3 ||
		before.Audit.LastStreamID == "" || len(before.Audit.StreamSHA256) != 64 {
		t.Fatalf("initialized redis snapshot = %+v", before)
	}
	if _, _, err := ledger.Revoke(t.Context(), "release operator", "operator_request"); err == nil ||
		!strings.Contains(err.Error(), "portable") {
		t.Fatalf("redis revocation accepted an unsafe operator identifier: %v", err)
	}
	if _, _, err := ledger.Revoke(t.Context(), "release-operator-01", "free_text"); err == nil ||
		!strings.Contains(err.Error(), "reason code is invalid") {
		t.Fatalf("redis revocation accepted a free-text reason: %v", err)
	}

	changed, revoked, err := ledger.Revoke(t.Context(), "release-operator-01", "budget_cancelled")
	if err != nil || !changed {
		t.Fatalf("revoke redis ledger: changed=%t err=%v", changed, err)
	}
	if revoked.Status != "revoked" || revoked.IntegrityStatus != "intact" || revoked.Revocation == nil ||
		revoked.Revocation.ReasonCode != "budget_cancelled" || revoked.Revocation.AuditMode != "stream" ||
		revoked.Revocation.Sequence != 3 || revoked.Usage == nil || revoked.Audit == nil ||
		revoked.Usage.Runs != 1 || revoked.Usage.ProviderCalls != 1 || revoked.Usage.CapturedOutputs != 1 ||
		revoked.Usage.EstimatedCostMicros != 4 || revoked.Audit.Sequence != 3 || revoked.Audit.EventCount != 4 ||
		revoked.Audit.ReplayStatus != "verified" || revoked.Audit.VerifiedEventCount != 4 ||
		revoked.Audit.LastStreamID == "" || len(revoked.Audit.StreamSHA256) != 64 {
		t.Fatalf("revoked redis snapshot = %+v", revoked)
	}
	marker, err := client.HGetAll(t.Context(), ledger.markerKey).Result()
	if err != nil {
		t.Fatalf("read revoked redis marker: %v", err)
	}
	if strings.Contains(fmt.Sprint(marker), "release-operator-01") || marker["revocation_operator_sha256"] != revoked.Revocation.OperatorSHA256 {
		t.Fatalf("redis revocation marker exposed operator or lost its digest: %v", marker)
	}

	changed, repeated, err := ledger.Revoke(t.Context(), "another-operator", "operator_request")
	if err != nil || changed {
		t.Fatalf("repeat redis revocation: changed=%t err=%v", changed, err)
	}
	if repeated.Revocation == nil || repeated.Revocation.ReasonCode != "budget_cancelled" || repeated.Audit == nil ||
		repeated.Audit.EventCount != 4 || repeated.Audit.ReplayStatus != "verified" ||
		repeated.Audit.StreamSHA256 != revoked.Audit.StreamSHA256 {
		t.Fatalf("repeat redis revocation changed canonical evidence: %+v", repeated)
	}
	if err := ledger.ReserveProviderCallContext(t.Context(), "after-revoke", 1, time.Now().UTC()); err == nil ||
		!strings.Contains(err.Error(), "authorization is revoked") {
		t.Fatalf("revoked redis authorization accepted a provider call: %v", err)
	}
	if _, err := ledger.Initialize(t.Context()); err == nil || !strings.Contains(err.Error(), "authorization is revoked") {
		t.Fatalf("revoked redis authorization was reinitialized: %v", err)
	}
	if err := client.HSet(t.Context(), ledger.markerKey, "status", "initialized").Err(); err != nil {
		t.Fatalf("tamper revoked redis marker: %v", err)
	}
	if err := ledger.ReserveProviderCallContext(t.Context(), "after-marker-reopen", 1, time.Now().UTC()); err == nil ||
		!strings.Contains(err.Error(), "state status is invalid") {
		t.Fatalf("marker-only tamper reopened a revoked redis authorization: %v", err)
	}
	if err := client.HDel(t.Context(), ledger.stateKey, "status").Err(); err != nil {
		t.Fatalf("remove revoked redis state status: %v", err)
	}
	if err := ledger.ReserveProviderCallContext(t.Context(), "after-state-status-delete", 1, time.Now().UTC()); err == nil ||
		!strings.Contains(err.Error(), "state status is invalid") {
		t.Fatalf("removing only the state status reopened a revoked redis authorization: %v", err)
	}
}

func TestAgentTaskLiveRedisLedgerRevokesAfterStateLossWithoutRebuildingState(t *testing.T) {
	server := miniredis.RunT(t)
	config := testAgentTaskLiveRedisConfig(server.Addr())
	client, err := newAgentTaskLiveRedisClient(config)
	if err != nil {
		t.Fatalf("connect redis ledger: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	now := time.Now().UTC()
	authorization := testAgentTaskLiveRedisAuthorization(t, now, agentTaskLiveAuthorizationLimits{
		MaxRuns: 1, MaxProviderCalls: 2, MaxCapturedOutputs: 0, MaxEstimatedCostMicros: 2,
	})
	ledger, err := newAgentTaskLiveRedisLedger(client, config, authorization)
	if err != nil {
		t.Fatalf("create redis ledger: %v", err)
	}
	if _, err := ledger.Initialize(t.Context()); err != nil {
		t.Fatalf("initialize redis ledger: %v", err)
	}
	if err := client.Del(t.Context(), ledger.stateKey, ledger.eventsKey).Err(); err != nil {
		t.Fatalf("delete redis ledger state: %v", err)
	}
	if _, _, err := ledger.Revoke(t.Context(), "incident-operator", "operator_request"); err == nil ||
		!strings.Contains(err.Error(), "state_integrity_incident") {
		t.Fatalf("state loss accepted a non-incident revocation: %v", err)
	}
	changed, snapshot, err := ledger.Revoke(t.Context(), "incident-operator", "state_integrity_incident")
	if err != nil || !changed {
		t.Fatalf("revoke lost redis ledger: changed=%t err=%v", changed, err)
	}
	if snapshot.Status != "revoked" || snapshot.IntegrityStatus != "state_lost" || snapshot.Usage != nil || snapshot.Audit != nil ||
		snapshot.Revocation == nil || snapshot.Revocation.AuditMode != "marker_only" || snapshot.Revocation.Sequence != -1 {
		t.Fatalf("lost-state redis revocation snapshot = %+v", snapshot)
	}
	stateExists, err := client.Exists(t.Context(), ledger.stateKey, ledger.eventsKey).Result()
	if err != nil || stateExists != 0 {
		t.Fatalf("lost redis state was rebuilt: exists=%d err=%v", stateExists, err)
	}
	if err := ledger.ReserveProviderCallContext(t.Context(), "after-incident", 1, time.Now().UTC()); err == nil ||
		!strings.Contains(err.Error(), "authorization is revoked") {
		t.Fatalf("incident-revoked redis authorization accepted a call: %v", err)
	}
}

func TestAgentTaskLiveRedisLedgerConcurrentRevokeSerializesReservations(t *testing.T) {
	server := miniredis.RunT(t)
	config := testAgentTaskLiveRedisConfig(server.Addr())
	client, err := newAgentTaskLiveRedisClient(config)
	if err != nil {
		t.Fatalf("connect redis ledger: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	now := time.Now().UTC()
	authorization := testAgentTaskLiveRedisAuthorization(t, now, agentTaskLiveAuthorizationLimits{
		MaxRuns: 1, MaxProviderCalls: 16, MaxCapturedOutputs: 0, MaxEstimatedCostMicros: 16,
	})
	ledger, err := newAgentTaskLiveRedisLedger(client, config, authorization)
	if err != nil {
		t.Fatalf("create redis ledger: %v", err)
	}
	if _, err := ledger.Initialize(t.Context()); err != nil {
		t.Fatalf("initialize redis ledger: %v", err)
	}

	ledgers := make([]*agentTaskLiveRedisLedger, 16)
	for index := range ledgers {
		instanceClient, clientErr := newAgentTaskLiveRedisClient(config)
		if clientErr != nil {
			t.Fatalf("connect redis ledger instance %d: %v", index, clientErr)
		}
		t.Cleanup(func() { _ = instanceClient.Close() })
		ledgers[index], err = newAgentTaskLiveRedisLedger(instanceClient, config, authorization)
		if err != nil {
			t.Fatalf("create redis ledger instance %d: %v", index, err)
		}
	}

	type revokeResult struct {
		changed  bool
		snapshot agentTaskLiveRedisStateSnapshot
		err      error
	}
	start := make(chan struct{})
	reservationErrors := make(chan error, len(ledgers))
	revokeResults := make(chan revokeResult, 1)
	var wait sync.WaitGroup
	for index, current := range ledgers {
		wait.Add(1)
		go func(index int, current *agentTaskLiveRedisLedger) {
			defer wait.Done()
			<-start
			reservationErrors <- current.ReserveProviderCallContext(
				context.Background(), fmt.Sprintf("concurrent-case-%d", index), 1, time.Now().UTC(),
			)
		}(index, current)
	}
	wait.Add(1)
	go func() {
		defer wait.Done()
		<-start
		changed, snapshot, revokeErr := ledger.Revoke(context.Background(), "concurrent-operator", "operator_request")
		revokeResults <- revokeResult{changed: changed, snapshot: snapshot, err: revokeErr}
	}()
	close(start)
	wait.Wait()
	close(reservationErrors)
	result := <-revokeResults
	if result.err != nil || !result.changed || result.snapshot.Status != "revoked" {
		t.Fatalf("concurrent redis revocation = %+v", result)
	}
	successfulReservations := 0
	for reservationErr := range reservationErrors {
		if reservationErr == nil {
			successfulReservations++
			continue
		}
		if !strings.Contains(reservationErr.Error(), "authorization is revoked") {
			t.Fatalf("concurrent redis reservation failed unexpectedly: %v", reservationErr)
		}
	}
	finalSnapshot, err := ledger.Inspect(t.Context())
	if err != nil {
		t.Fatalf("inspect concurrently revoked redis ledger: %v", err)
	}
	if finalSnapshot.Usage == nil || finalSnapshot.Audit == nil || finalSnapshot.Revocation == nil ||
		finalSnapshot.Usage.ProviderCalls != successfulReservations ||
		finalSnapshot.Usage.EstimatedCostMicros != int64(successfulReservations) ||
		finalSnapshot.Audit.Sequence != int64(successfulReservations+1) ||
		finalSnapshot.Audit.EventCount != int64(successfulReservations+2) ||
		finalSnapshot.Audit.ReplayStatus != "verified" ||
		finalSnapshot.Audit.VerifiedEventCount != finalSnapshot.Audit.EventCount ||
		finalSnapshot.Revocation.Sequence != finalSnapshot.Audit.Sequence {
		t.Fatalf("concurrent redis ledger invariant failed: successes=%d snapshot=%+v", successfulReservations, finalSnapshot)
	}
}

func TestAgentTaskLiveRedisLedgerAuditReplayRejectsBalancedTamper(t *testing.T) {
	server := miniredis.RunT(t)
	config := testAgentTaskLiveRedisConfig(server.Addr())
	client, err := newAgentTaskLiveRedisClient(config)
	if err != nil {
		t.Fatalf("connect redis ledger: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	now := time.Now().UTC()
	authorization := testAgentTaskLiveRedisAuthorization(t, now, agentTaskLiveAuthorizationLimits{
		MaxRuns: 1, MaxProviderCalls: 1, MaxCapturedOutputs: 0, MaxEstimatedCostMicros: 1,
	})
	ledger, err := newAgentTaskLiveRedisLedger(client, config, authorization)
	if err != nil {
		t.Fatalf("create redis ledger: %v", err)
	}
	if _, err := ledger.Initialize(t.Context()); err != nil {
		t.Fatalf("initialize redis ledger: %v", err)
	}
	if err := ledger.ReserveRunContext(t.Context(), 0, time.Now().UTC()); err != nil {
		t.Fatalf("reserve redis run: %v", err)
	}

	events, err := client.XRange(t.Context(), ledger.eventsKey, "-", "+").Result()
	if err != nil || len(events) != 2 {
		t.Fatalf("read redis audit events: count=%d err=%v", len(events), err)
	}
	if removed, deleteErr := client.XDel(t.Context(), ledger.eventsKey, events[1].ID).Result(); deleteErr != nil || removed != 1 {
		t.Fatalf("remove redis audit event: removed=%d err=%v", removed, deleteErr)
	}
	_, err = client.XAdd(t.Context(), &redis.XAddArgs{
		Stream: ledger.eventsKey,
		Values: map[string]interface{}{
			"sequence":              "1",
			"event_type":            "provider_call_reserved",
			"created_at_unix_ms":    fmt.Sprint(events[0].Values["created_at_unix_ms"]),
			"invocation_sha256":     hashAgentTaskLiveAuthorizationSubject(ledger.invocationID),
			"subject_sha256":        hashAgentTaskLiveAuthorizationSubject("forged-subject"),
			"runs":                  "0",
			"provider_calls":        "1",
			"captured_outputs":      "0",
			"estimated_cost_micros": "0",
			"reservation_sha256":    strings.Repeat("a", 64),
		},
	}).Result()
	if err != nil {
		t.Fatalf("replace redis audit event: %v", err)
	}
	if eventCount, countErr := client.XLen(t.Context(), ledger.eventsKey).Result(); countErr != nil || eventCount != 2 {
		t.Fatalf("balanced redis audit event count: count=%d err=%v", eventCount, countErr)
	}
	if _, err := ledger.Inspect(t.Context()); err == nil || !strings.Contains(err.Error(), "replayed usage does not match") {
		t.Fatalf("balanced redis audit tamper was accepted: %v", err)
	}
}

func TestAgentTaskLiveRedisLedgerAuditReplayPaginatesToAtomicBoundary(t *testing.T) {
	server := miniredis.RunT(t)
	config := testAgentTaskLiveRedisConfig(server.Addr())
	client, err := newAgentTaskLiveRedisClient(config)
	if err != nil {
		t.Fatalf("connect redis ledger: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	now := time.Now().UTC()
	authorization := testAgentTaskLiveRedisAuthorization(t, now, agentTaskLiveAuthorizationLimits{
		MaxRuns: 1, MaxProviderCalls: 521, MaxCapturedOutputs: 0, MaxEstimatedCostMicros: 521,
	})
	ledger, err := newAgentTaskLiveRedisLedger(client, config, authorization)
	if err != nil {
		t.Fatalf("create redis ledger: %v", err)
	}
	if _, err := ledger.Initialize(t.Context()); err != nil {
		t.Fatalf("initialize redis ledger: %v", err)
	}
	for index := 0; index < 520; index++ {
		if err := ledger.ReserveProviderCallContext(
			t.Context(), fmt.Sprintf("page-case-%03d", index), 1, time.Now().UTC(),
		); err != nil {
			t.Fatalf("reserve paginated redis provider call %d: %v", index, err)
		}
	}

	snapshot, err := ledger.Inspect(t.Context())
	if err != nil {
		t.Fatalf("inspect paginated redis ledger: %v", err)
	}
	if snapshot.Usage == nil || snapshot.Audit == nil || snapshot.Usage.ProviderCalls != 520 ||
		snapshot.Audit.EventCount != 521 || snapshot.Audit.VerifiedEventCount != 521 ||
		snapshot.Audit.ReplayStatus != "verified" || len(snapshot.Audit.StreamSHA256) != 64 {
		t.Fatalf("paginated redis snapshot = %+v", snapshot)
	}
	boundaryID := snapshot.Audit.LastStreamID
	boundaryDigest := snapshot.Audit.StreamSHA256
	if err := ledger.ReserveProviderCallContext(t.Context(), "after-snapshot-boundary", 1, time.Now().UTC()); err != nil {
		t.Fatalf("reserve redis provider call after snapshot: %v", err)
	}
	if err := ledger.verifyAuditStream(t.Context(), &snapshot); err != nil {
		t.Fatalf("replay atomic redis snapshot boundary: %v", err)
	}
	if snapshot.Audit.LastStreamID != boundaryID || snapshot.Audit.StreamSHA256 != boundaryDigest ||
		snapshot.Audit.VerifiedEventCount != 521 {
		t.Fatalf("later redis event contaminated snapshot replay: %+v", snapshot.Audit)
	}
	current, err := ledger.Inspect(t.Context())
	if err != nil {
		t.Fatalf("inspect current redis ledger: %v", err)
	}
	if current.Audit == nil || current.Usage == nil || current.Audit.EventCount != 522 ||
		current.Usage.ProviderCalls != 521 || current.Audit.LastStreamID == boundaryID ||
		current.Audit.StreamSHA256 == boundaryDigest {
		t.Fatalf("current redis snapshot did not advance: %+v", current)
	}
}

func TestAgentTaskLiveRedisLedgerFailsClosedOnStateLossOrTamper(t *testing.T) {
	server := miniredis.RunT(t)
	config := testAgentTaskLiveRedisConfig(server.Addr())
	client, err := newAgentTaskLiveRedisClient(config)
	if err != nil {
		t.Fatalf("connect redis ledger: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	now := time.Now().UTC()
	authorization := testAgentTaskLiveRedisAuthorization(t, now, agentTaskLiveAuthorizationLimits{
		MaxRuns: 1, MaxProviderCalls: 2, MaxCapturedOutputs: 0, MaxEstimatedCostMicros: 2,
	})
	ledger, err := newAgentTaskLiveRedisLedger(client, config, authorization)
	if err != nil {
		t.Fatalf("create redis ledger: %v", err)
	}
	if _, err := ledger.Initialize(t.Context()); err != nil {
		t.Fatalf("initialize redis ledger: %v", err)
	}
	if err := ledger.ReserveRunContext(t.Context(), 0, time.Now().UTC()); err != nil {
		t.Fatalf("reserve redis run: %v", err)
	}
	if err := client.Del(t.Context(), ledger.stateKey, ledger.eventsKey).Err(); err != nil {
		t.Fatalf("delete redis ledger state: %v", err)
	}
	if err := ledger.ReserveProviderCallContext(t.Context(), "after-loss", 1, time.Now().UTC()); err == nil ||
		!strings.Contains(err.Error(), "state was lost") {
		t.Fatalf("lost redis state was silently recreated: %v", err)
	}
	if _, err := ledger.Initialize(t.Context()); err == nil || !strings.Contains(err.Error(), "state was lost") {
		t.Fatalf("lost redis state was explicitly reinitialized: %v", err)
	}

	tamperConfig := config
	tamperConfig.KeyPrefix = "test:agent-task-live-tamper"
	tamperLedger, err := newAgentTaskLiveRedisLedger(client, tamperConfig, authorization)
	if err != nil {
		t.Fatalf("create tamper redis ledger: %v", err)
	}
	if _, err := tamperLedger.Initialize(t.Context()); err != nil {
		t.Fatalf("initialize tamper redis ledger: %v", err)
	}
	if err := client.HSet(t.Context(), tamperLedger.markerKey, "status", "modified").Err(); err != nil {
		t.Fatalf("tamper redis marker: %v", err)
	}
	if err := tamperLedger.ReserveProviderCallContext(t.Context(), "after-marker-tamper", 1, time.Now().UTC()); err == nil ||
		!strings.Contains(err.Error(), "marker identity mismatch") {
		t.Fatalf("tampered redis marker was accepted: %v", err)
	}
	if err := client.HSet(t.Context(), tamperLedger.markerKey, "status", "initialized").Err(); err != nil {
		t.Fatalf("restore redis marker for identity test: %v", err)
	}
	if err := client.HSet(t.Context(), tamperLedger.stateKey, "authorization_payload_sha256", strings.Repeat("f", 64)).Err(); err != nil {
		t.Fatalf("tamper redis identity: %v", err)
	}
	if err := tamperLedger.ReserveProviderCallContext(t.Context(), "after-tamper", 1, time.Now().UTC()); err == nil ||
		!strings.Contains(err.Error(), "identity mismatch") {
		t.Fatalf("tampered redis identity was accepted: %v", err)
	}
}

func TestRunInitializesRedisLiveAuthorizationStateWithoutProviderCall(t *testing.T) {
	server := miniredis.RunT(t)
	key := []byte("redis-live-authorization-key-material-v1")
	t.Setenv("TEST_REDIS_LIVE_AUTHORIZATION_KEY", string(key))
	now := time.Now().UTC()
	authorization := testAgentTaskLiveRedisAuthorizationWithKey(t, now, agentTaskLiveAuthorizationLimits{
		MaxRuns: 1, MaxProviderCalls: 2, MaxCapturedOutputs: 0, MaxEstimatedCostMicros: 2,
	}, key, "redis-key-v1")
	tempDir := t.TempDir()
	authorizationPath := filepath.Join(tempDir, "authorization.json")
	if err := writeAgentTaskLiveAuthorization(authorizationPath, authorization); err != nil {
		t.Fatalf("write authorization: %v", err)
	}
	configPath := filepath.Join(tempDir, "redis.json")
	config := testAgentTaskLiveRedisConfig(server.Addr())
	payload, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("encode redis config: %v", err)
	}
	if err := os.WriteFile(configPath, payload, 0o600); err != nil {
		t.Fatalf("write redis config: %v", err)
	}

	args := []string{
		"--initialize-live-authorization-state",
		"--live-authorization", authorizationPath,
		"--live-authorization-state-backend", "redis",
		"--live-authorization-redis-config", configPath,
		"--live-authorization-key-env", "TEST_REDIS_LIVE_AUTHORIZATION_KEY",
		"--live-authorization-key-id", "redis-key-v1",
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exitCode := run(args, &stdout, &stderr); exitCode != 0 || !strings.Contains(stdout.String(), "state initialized") {
		t.Fatalf("initialize redis state: code=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if exitCode := run(args, &stdout, &stderr); exitCode != 0 || !strings.Contains(stdout.String(), "already_initialized") {
		t.Fatalf("repeat redis initialization: code=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if exitCode := run(append(append([]string(nil), args...), "--live-authorization-max-runs", "2"), &stdout, &stderr); exitCode == 0 ||
		!strings.Contains(stderr.String(), "cannot be combined") {
		t.Fatalf("redis initialization ignored limit flags: code=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}

	adminArgs := append([]string(nil), args[1:]...)
	stdout.Reset()
	stderr.Reset()
	if exitCode := run(append([]string{"--inspect-live-authorization-state"}, adminArgs...), &stdout, &stderr); exitCode != 0 {
		t.Fatalf("inspect redis state: code=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	var inspected agentTaskLiveRedisAdminOutput
	if err := json.Unmarshal(stdout.Bytes(), &inspected); err != nil || inspected.Operation != "inspect" || inspected.Changed ||
		inspected.State.Status != "initialized" {
		t.Fatalf("redis inspection output = %+v, err=%v, raw=%q", inspected, err, stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := run(append([]string{"--revoke-live-authorization-state"}, adminArgs...), &stdout, &stderr); exitCode == 0 ||
		!strings.Contains(stderr.String(), "requires --live-authorization-revocation-operator") {
		t.Fatalf("redis revocation accepted missing metadata: code=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	revokeArgs := append([]string{"--revoke-live-authorization-state"}, adminArgs...)
	revokeArgs = append(revokeArgs,
		"--live-authorization-revocation-operator", "test-operator",
		"--live-authorization-revocation-reason", "evaluation_cancelled",
	)
	stdout.Reset()
	stderr.Reset()
	if exitCode := run(revokeArgs, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("revoke redis state: code=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	var revoked agentTaskLiveRedisAdminOutput
	if err := json.Unmarshal(stdout.Bytes(), &revoked); err != nil || revoked.Operation != "revoke" || !revoked.Changed ||
		revoked.State.Status != "revoked" || revoked.State.Revocation == nil ||
		revoked.State.Revocation.ReasonCode != "evaluation_cancelled" {
		t.Fatalf("redis revocation output = %+v, err=%v, raw=%q", revoked, err, stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	if exitCode := run(revokeArgs, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("repeat redis revocation: code=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	var repeated agentTaskLiveRedisAdminOutput
	if err := json.Unmarshal(stdout.Bytes(), &repeated); err != nil || repeated.Changed || repeated.State.Status != "revoked" {
		t.Fatalf("repeat redis revocation output = %+v, err=%v, raw=%q", repeated, err, stdout.String())
	}
}

func TestRunRedisLiveEvaluationFailsBeforeProviderWhenStateIsUninitialized(t *testing.T) {
	server := miniredis.RunT(t)
	t.Setenv("TEST_REDIS_RUN_AUTHORIZATION_KEY", "redis-run-authorization-key-material-v1")
	t.Setenv("TEST_REDIS_RUN_REPORT_KEY", "redis-run-report-integrity-key-material-v1")
	tempDir := t.TempDir()
	datasetPath := filepath.Join(tempDir, "dataset.json")
	if err := os.WriteFile(datasetPath, []byte(`[{"id":"case-1","category":"chat","mode":"chat","input":"hello","expected_outcome":"completed"}]`), 0o600); err != nil {
		t.Fatalf("write dataset: %v", err)
	}
	dataset, err := loadAgentTaskDataset(datasetPath)
	if err != nil {
		t.Fatalf("load dataset: %v", err)
	}
	datasetHash, err := eval.HashAgentTaskDataset(dataset)
	if err != nil {
		t.Fatalf("hash dataset: %v", err)
	}
	config := validRuntimeEvalConfig()
	config.BaseURL = "http://127.0.0.1:1/v1"
	configPath := filepath.Join(tempDir, "runtime.json")
	configPayload, _ := json.Marshal(config)
	if err := os.WriteFile(configPath, configPayload, 0o600); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}
	configHash, err := hashRuntimeEvalConfig(config)
	if err != nil {
		t.Fatalf("hash runtime config: %v", err)
	}
	now := time.Now().UTC()
	authorization, err := buildAndSignAgentTaskLiveAuthorization(agentTaskLiveAuthorization{
		AuthorizationID: "authorization-redis-run-001", ExpiresAt: now.Add(time.Hour),
		Provider: config.Provider, Model: config.Model, DatasetVersion: "cases-v1",
		DatasetSHA256: datasetHash, ExecutionConfigSHA256: configHash,
		Limits: agentTaskLiveAuthorizationLimits{
			MaxRuns: 1, MaxProviderCalls: 4, MaxCapturedOutputs: 0, MaxEstimatedCostMicros: 4,
		},
	}, []byte(os.Getenv("TEST_REDIS_RUN_AUTHORIZATION_KEY")), "redis-run-key-v1", now)
	if err != nil {
		t.Fatalf("build live authorization: %v", err)
	}
	authorizationPath := filepath.Join(tempDir, "authorization.json")
	if err := writeAgentTaskLiveAuthorization(authorizationPath, authorization); err != nil {
		t.Fatalf("write live authorization: %v", err)
	}
	redisConfigPath := filepath.Join(tempDir, "redis.json")
	redisConfigPayload, _ := json.Marshal(testAgentTaskLiveRedisConfig(server.Addr()))
	if err := os.WriteFile(redisConfigPath, redisConfigPayload, 0o600); err != nil {
		t.Fatalf("write redis config: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{
		"--allow-live",
		"--dataset", datasetPath,
		"--dataset-version", "cases-v1",
		"--runtime-config", configPath,
		"--live-authorization", authorizationPath,
		"--live-authorization-state-backend", "redis",
		"--live-authorization-redis-config", redisConfigPath,
		"--live-authorization-key-env", "TEST_REDIS_RUN_AUTHORIZATION_KEY",
		"--live-authorization-key-id", "redis-run-key-v1",
		"--integrity-key-env", "TEST_REDIS_RUN_REPORT_KEY",
		"--integrity-key-id", "redis-run-report-v1",
	}, &stdout, &stderr)
	if exitCode == 0 || !strings.Contains(stderr.String(), "redis ledger is not initialized") {
		t.Fatalf("uninitialized redis live run reached provider: code=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
}

func TestLoadAgentTaskLiveRedisExampleConfig(t *testing.T) {
	path := filepath.Join(
		"..", "..", "internal", "module", "agent", "eval", "testdata",
		"agent_task_live_redis_config.example.json",
	)
	config, err := loadAgentTaskLiveRedisConfig(path)
	if err != nil {
		t.Fatalf("load Redis shared-ledger example: %v", err)
	}
	if config.SchemaVersion != agentTaskLiveRedisConfigSchemaVersion || !config.TLSEnabled ||
		config.PasswordEnv != "AGENT_TASK_EVAL_REDIS_PASSWORD" || config.OperationTimeoutMS != 3000 {
		t.Fatalf("Redis shared-ledger example = %+v", config)
	}
}

func testAgentTaskLiveRedisConfig(address string) agentTaskLiveRedisConfig {
	return agentTaskLiveRedisConfig{
		SchemaVersion: agentTaskLiveRedisConfigSchemaVersion,
		Address:       address, Database: 0, KeyPrefix: "test:agent-task-live",
		ConnectTimeoutMS: 1000, OperationTimeoutMS: 1000,
	}
}

func testAgentTaskLiveRedisAuthorization(
	t *testing.T,
	now time.Time,
	limits agentTaskLiveAuthorizationLimits,
) agentTaskLiveAuthorization {
	t.Helper()
	return testAgentTaskLiveRedisAuthorizationWithKey(
		t, now, limits, []byte("redis-live-ledger-test-key-material-v1"), "authorization-key-v1",
	)
}

func testAgentTaskLiveRedisAuthorizationWithKey(
	t *testing.T,
	now time.Time,
	limits agentTaskLiveAuthorizationLimits,
	key []byte,
	keyID string,
) agentTaskLiveAuthorization {
	t.Helper()
	authorization, err := buildAndSignAgentTaskLiveAuthorization(agentTaskLiveAuthorization{
		AuthorizationID: "authorization-redis-001", ExpiresAt: now.Add(time.Hour),
		Provider: "dashscope", Model: "qwen-fixed", DatasetVersion: "cases-v1",
		DatasetSHA256: strings.Repeat("a", 64), ExecutionConfigSHA256: strings.Repeat("b", 64),
		Limits: limits,
	}, key, keyID, now.Add(-time.Second))
	if err != nil {
		t.Fatalf("build redis live authorization: %v", err)
	}
	return authorization
}
