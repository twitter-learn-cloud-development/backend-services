package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"strconv"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
)

const (
	agentTaskLiveRedisAuditPageSize = int64(512)
	agentTaskLiveRedisAuditTimeout  = 30 * time.Second
)

var (
	agentTaskLiveRedisInitializedEventFields = []string{
		"sequence", "event_type", "created_at_unix_ms", "authorization_payload_sha256",
	}
	agentTaskLiveRedisReservationEventFields = []string{
		"sequence", "event_type", "created_at_unix_ms", "invocation_sha256", "subject_sha256",
		"runs", "provider_calls", "captured_outputs", "estimated_cost_micros", "reservation_sha256",
	}
	agentTaskLiveRedisRevocationEventFields = []string{
		"sequence", "event_type", "created_at_unix_ms", "authorization_payload_sha256",
		"operator_sha256", "reason_code", "request_sha256",
	}
)

type agentTaskLiveRedisCanonicalAuditEvent struct {
	StreamID                   string `json:"stream_id"`
	Sequence                   int64  `json:"sequence"`
	EventType                  string `json:"event_type"`
	CreatedAtUnixMS            int64  `json:"created_at_unix_ms"`
	AuthorizationPayloadSHA256 string `json:"authorization_payload_sha256"`
	InvocationSHA256           string `json:"invocation_sha256"`
	SubjectSHA256              string `json:"subject_sha256"`
	Runs                       int64  `json:"runs"`
	ProviderCalls              int64  `json:"provider_calls"`
	CapturedOutputs            int64  `json:"captured_outputs"`
	EstimatedCostMicros        int64  `json:"estimated_cost_micros"`
	ReservationSHA256          string `json:"reservation_sha256"`
	OperatorSHA256             string `json:"operator_sha256"`
	ReasonCode                 string `json:"reason_code"`
	RequestSHA256              string `json:"request_sha256"`
}

type agentTaskLiveRedisAuditVerifier struct {
	ledger            *agentTaskLiveRedisLedger
	snapshot          *agentTaskLiveRedisStateSnapshot
	digest            hash.Hash
	processed         int64
	previousStreamID  string
	previousCreatedMS int64
	runs              int64
	providerCalls     int64
	capturedOutputs   int64
	estimatedCost     int64
	revocationSeen    bool
}

func (ledger *agentTaskLiveRedisLedger) verifyAuditStream(
	ctx context.Context,
	snapshot *agentTaskLiveRedisStateSnapshot,
) error {
	if ledger == nil || ledger.client == nil || snapshot == nil || snapshot.Audit == nil || snapshot.Usage == nil {
		return errors.New("verify live authorization redis audit stream: intact snapshot is incomplete")
	}
	audit := snapshot.Audit
	if audit.EventCount < 1 || audit.Sequence != audit.EventCount-1 {
		return errors.New("verify live authorization redis audit stream: snapshot sequence is inconsistent")
	}
	maximumEvents := int64(2 + ledger.authorization.Limits.MaxRuns + ledger.authorization.Limits.MaxProviderCalls)
	if audit.EventCount > maximumEvents {
		return errors.New("verify live authorization redis audit stream: event count exceeds authorization bounds")
	}
	if _, _, err := parseAgentTaskLiveRedisStreamID(audit.LastStreamID); err != nil {
		return fmt.Errorf("verify live authorization redis audit stream: invalid last stream ID: %w", err)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	auditCtx, cancel := context.WithTimeout(ctx, agentTaskLiveRedisAuditTimeout)
	defer cancel()
	verifier := agentTaskLiveRedisAuditVerifier{
		ledger: ledger, snapshot: snapshot, digest: sha256.New(),
	}
	start := "-"
	for verifier.processed < audit.EventCount {
		count := agentTaskLiveRedisAuditPageSize
		if start != "-" {
			count++
		}
		messages, err := ledger.client.XRangeN(auditCtx, ledger.eventsKey, start, audit.LastStreamID, count).Result()
		if err != nil {
			return fmt.Errorf("verify live authorization redis audit stream: read events: %w", err)
		}
		if start != "-" {
			if len(messages) == 0 || messages[0].ID != start {
				return errors.New("verify live authorization redis audit stream: pagination cursor was removed or replaced")
			}
			messages = messages[1:]
		}
		if len(messages) == 0 {
			return errors.New("verify live authorization redis audit stream: event range ended before the snapshot boundary")
		}
		for _, message := range messages {
			if verifier.processed >= audit.EventCount {
				return errors.New("verify live authorization redis audit stream: event range exceeded the snapshot count")
			}
			if err := verifier.consume(message); err != nil {
				return err
			}
		}
		start = messages[len(messages)-1].ID
		if start == audit.LastStreamID {
			break
		}
	}
	if err := verifier.finish(); err != nil {
		return err
	}
	audit.ReplayStatus = "verified"
	audit.VerifiedEventCount = verifier.processed
	audit.StreamSHA256 = hex.EncodeToString(verifier.digest.Sum(nil))
	return nil
}

func (verifier *agentTaskLiveRedisAuditVerifier) consume(message redis.XMessage) error {
	streamMS, streamSequence, err := parseAgentTaskLiveRedisStreamID(message.ID)
	if err != nil {
		return fmt.Errorf("verify live authorization redis audit stream: invalid event stream ID: %w", err)
	}
	if verifier.previousStreamID != "" {
		previousMS, previousSequence, parseErr := parseAgentTaskLiveRedisStreamID(verifier.previousStreamID)
		if parseErr != nil || streamMS < previousMS || streamMS == previousMS && streamSequence <= previousSequence {
			return errors.New("verify live authorization redis audit stream: event stream IDs are not strictly increasing")
		}
	}
	sequence, err := agentTaskLiveRedisAuditInt64(message.Values, "sequence")
	if err != nil || sequence != verifier.processed {
		return errors.New("verify live authorization redis audit stream: event sequence is not contiguous")
	}
	eventType, err := agentTaskLiveRedisAuditString(message.Values, "event_type")
	if err != nil {
		return fmt.Errorf("verify live authorization redis audit stream: %w", err)
	}
	createdAt, err := agentTaskLiveRedisAuditInt64(message.Values, "created_at_unix_ms")
	if err != nil || createdAt < verifier.ledger.authorization.IssuedAt.UnixMilli() ||
		createdAt >= verifier.ledger.authorization.ExpiresAt.UnixMilli() {
		return errors.New("verify live authorization redis audit stream: event time is outside the authorization window")
	}
	if verifier.processed > 0 && createdAt < verifier.previousCreatedMS {
		return errors.New("verify live authorization redis audit stream: event times are not monotonic")
	}
	event := agentTaskLiveRedisCanonicalAuditEvent{
		StreamID: message.ID, Sequence: sequence, EventType: eventType, CreatedAtUnixMS: createdAt,
	}
	switch eventType {
	case "initialized":
		err = verifier.consumeInitialized(message.Values, &event)
	case "run_reserved", "provider_call_reserved":
		err = verifier.consumeReservation(message.Values, &event)
	case "authorization_revoked":
		err = verifier.consumeRevocation(message.Values, &event)
	default:
		err = fmt.Errorf("unsupported event type %q", eventType)
	}
	if err != nil {
		return fmt.Errorf("verify live authorization redis audit stream: sequence %d: %w", sequence, err)
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("verify live authorization redis audit stream: encode canonical event: %w", err)
	}
	_, _ = verifier.digest.Write(payload)
	_, _ = verifier.digest.Write([]byte{'\n'})
	verifier.processed++
	verifier.previousStreamID = message.ID
	verifier.previousCreatedMS = createdAt
	return nil
}

func (verifier *agentTaskLiveRedisAuditVerifier) consumeInitialized(
	values map[string]interface{},
	event *agentTaskLiveRedisCanonicalAuditEvent,
) error {
	if verifier.processed != 0 {
		return errors.New("initialization event is not first")
	}
	if err := requireAgentTaskLiveRedisAuditFields(values, agentTaskLiveRedisInitializedEventFields); err != nil {
		return err
	}
	payloadHash, err := agentTaskLiveRedisAuditString(values, "authorization_payload_sha256")
	if err != nil || payloadHash != verifier.ledger.authorizationPayloadHash {
		return errors.New("initialization authorization digest mismatch")
	}
	event.AuthorizationPayloadSHA256 = payloadHash
	return nil
}

func (verifier *agentTaskLiveRedisAuditVerifier) consumeReservation(
	values map[string]interface{},
	event *agentTaskLiveRedisCanonicalAuditEvent,
) error {
	if verifier.processed == 0 || verifier.revocationSeen {
		return errors.New("reservation event is outside the active audit interval")
	}
	if err := requireAgentTaskLiveRedisAuditFields(values, agentTaskLiveRedisReservationEventFields); err != nil {
		return err
	}
	invocationHash, err := agentTaskLiveRedisAuditString(values, "invocation_sha256")
	if err != nil || !validAgentTaskLiveAuthorizationSHA256(invocationHash) {
		return errors.New("reservation invocation digest is invalid")
	}
	subjectHash, err := agentTaskLiveRedisAuditString(values, "subject_sha256")
	if err != nil || !validAgentTaskLiveAuthorizationSHA256(subjectHash) {
		return errors.New("reservation subject digest is invalid")
	}
	reservationHash, err := agentTaskLiveRedisAuditString(values, "reservation_sha256")
	if err != nil || !validAgentTaskLiveAuthorizationSHA256(reservationHash) {
		return errors.New("reservation digest is invalid")
	}
	runs, err := agentTaskLiveRedisAuditNonnegativeInt64(values, "runs")
	if err != nil {
		return err
	}
	providerCalls, err := agentTaskLiveRedisAuditNonnegativeInt64(values, "provider_calls")
	if err != nil {
		return err
	}
	capturedOutputs, err := agentTaskLiveRedisAuditNonnegativeInt64(values, "captured_outputs")
	if err != nil {
		return err
	}
	cost, err := agentTaskLiveRedisAuditNonnegativeInt64(values, "estimated_cost_micros")
	if err != nil {
		return err
	}
	if event.EventType == "run_reserved" {
		if runs != 1 || providerCalls != 0 || cost != 0 {
			return errors.New("run reservation delta is invalid")
		}
	} else if runs != 0 || providerCalls != 1 || capturedOutputs != 0 {
		return errors.New("provider call reservation delta is invalid")
	}
	if err := verifier.addUsage(runs, providerCalls, capturedOutputs, cost); err != nil {
		return err
	}
	event.InvocationSHA256 = invocationHash
	event.SubjectSHA256 = subjectHash
	event.ReservationSHA256 = reservationHash
	event.Runs = runs
	event.ProviderCalls = providerCalls
	event.CapturedOutputs = capturedOutputs
	event.EstimatedCostMicros = cost
	return nil
}

func (verifier *agentTaskLiveRedisAuditVerifier) consumeRevocation(
	values map[string]interface{},
	event *agentTaskLiveRedisCanonicalAuditEvent,
) error {
	if verifier.processed == 0 || verifier.revocationSeen || verifier.snapshot.Status != "revoked" ||
		verifier.snapshot.Revocation == nil || verifier.snapshot.Revocation.AuditMode != "stream" {
		return errors.New("revocation event does not match the snapshot state")
	}
	if verifier.processed != verifier.snapshot.Audit.Sequence {
		return errors.New("revocation event is not the terminal sequence")
	}
	if err := requireAgentTaskLiveRedisAuditFields(values, agentTaskLiveRedisRevocationEventFields); err != nil {
		return err
	}
	payloadHash, err := agentTaskLiveRedisAuditString(values, "authorization_payload_sha256")
	if err != nil || payloadHash != verifier.ledger.authorizationPayloadHash {
		return errors.New("revocation authorization digest mismatch")
	}
	operatorHash, err := agentTaskLiveRedisAuditString(values, "operator_sha256")
	if err != nil || operatorHash != verifier.snapshot.Revocation.OperatorSHA256 {
		return errors.New("revocation operator digest mismatch")
	}
	reasonCode, err := agentTaskLiveRedisAuditString(values, "reason_code")
	if err != nil || reasonCode != verifier.snapshot.Revocation.ReasonCode {
		return errors.New("revocation reason mismatch")
	}
	requestHash, err := agentTaskLiveRedisAuditString(values, "request_sha256")
	if err != nil || requestHash != verifier.snapshot.Revocation.RequestSHA256 {
		return errors.New("revocation request digest mismatch")
	}
	if event.CreatedAtUnixMS != verifier.snapshot.Revocation.RevokedAtUnixMS {
		return errors.New("revocation time mismatch")
	}
	verifier.revocationSeen = true
	event.AuthorizationPayloadSHA256 = payloadHash
	event.OperatorSHA256 = operatorHash
	event.ReasonCode = reasonCode
	event.RequestSHA256 = requestHash
	return nil
}

func (verifier *agentTaskLiveRedisAuditVerifier) addUsage(runs, calls, outputs, cost int64) error {
	limits := verifier.ledger.authorization.Limits
	if runs > int64(limits.MaxRuns)-verifier.runs ||
		calls > int64(limits.MaxProviderCalls)-verifier.providerCalls ||
		outputs > int64(limits.MaxCapturedOutputs)-verifier.capturedOutputs ||
		cost > limits.MaxEstimatedCostMicros-verifier.estimatedCost {
		return errors.New("replayed usage exceeds authorization limits")
	}
	verifier.runs += runs
	verifier.providerCalls += calls
	verifier.capturedOutputs += outputs
	verifier.estimatedCost += cost
	return nil
}

func (verifier *agentTaskLiveRedisAuditVerifier) finish() error {
	if verifier.processed != verifier.snapshot.Audit.EventCount ||
		verifier.previousStreamID != verifier.snapshot.Audit.LastStreamID {
		return errors.New("verify live authorization redis audit stream: replay did not reach the snapshot boundary")
	}
	if verifier.snapshot.Status == "revoked" && !verifier.revocationSeen ||
		verifier.snapshot.Status == "initialized" && verifier.revocationSeen {
		return errors.New("verify live authorization redis audit stream: replayed terminal state does not match the snapshot")
	}
	usage := verifier.snapshot.Usage
	if verifier.runs != int64(usage.Runs) || verifier.providerCalls != int64(usage.ProviderCalls) ||
		verifier.capturedOutputs != int64(usage.CapturedOutputs) || verifier.estimatedCost != usage.EstimatedCostMicros {
		return errors.New("verify live authorization redis audit stream: replayed usage does not match the state snapshot")
	}
	return nil
}

func requireAgentTaskLiveRedisAuditFields(values map[string]interface{}, expected []string) error {
	if len(values) != len(expected) {
		return errors.New("event field set is invalid")
	}
	for _, field := range expected {
		if _, ok := values[field]; !ok {
			return fmt.Errorf("event field %q is missing", field)
		}
	}
	return nil
}

func agentTaskLiveRedisAuditString(values map[string]interface{}, field string) (string, error) {
	value, ok := values[field]
	if !ok {
		return "", fmt.Errorf("event field %q is missing", field)
	}
	switch typed := value.(type) {
	case string:
		return typed, nil
	case []byte:
		return string(typed), nil
	default:
		return fmt.Sprint(value), nil
	}
}

func agentTaskLiveRedisAuditInt64(values map[string]interface{}, field string) (int64, error) {
	value, err := agentTaskLiveRedisAuditString(values, field)
	if err != nil {
		return 0, err
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("event field %q is not an integer", field)
	}
	return parsed, nil
}

func agentTaskLiveRedisAuditNonnegativeInt64(values map[string]interface{}, field string) (int64, error) {
	value, err := agentTaskLiveRedisAuditInt64(values, field)
	if err != nil {
		return 0, err
	}
	if value < 0 {
		return 0, fmt.Errorf("event field %q is negative", field)
	}
	return value, nil
}

func parseAgentTaskLiveRedisStreamID(value string) (uint64, uint64, error) {
	milliseconds, sequence, ok := strings.Cut(strings.TrimSpace(value), "-")
	if !ok || milliseconds == "" || sequence == "" || strings.Contains(sequence, "-") {
		return 0, 0, errors.New("stream ID must contain milliseconds and sequence")
	}
	millisecondsValue, err := strconv.ParseUint(milliseconds, 10, 64)
	if err != nil {
		return 0, 0, errors.New("stream ID milliseconds are invalid")
	}
	sequenceValue, err := strconv.ParseUint(sequence, 10, 64)
	if err != nil {
		return 0, 0, errors.New("stream ID sequence is invalid")
	}
	return millisecondsValue, sequenceValue, nil
}
