package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
)

const (
	agentTaskLiveRedisStateSnapshotSchemaVersion = "agent-task-live-authorization-redis-state/v1"
	agentTaskLiveRedisAdminOutputSchemaVersion   = "agent-task-live-authorization-redis-admin-output/v1"
)

var (
	agentTaskLiveRedisRevocationOperatorPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:@-]{0,127}$`)
	agentTaskLiveRedisRevocationReasons         = map[string]struct{}{
		"budget_cancelled":         {},
		"credential_rotation":      {},
		"evaluation_cancelled":     {},
		"operator_request":         {},
		"state_integrity_incident": {},
	}
)

type agentTaskLiveRedisUsageSnapshot struct {
	Runs                int   `json:"runs"`
	ProviderCalls       int   `json:"provider_calls"`
	CapturedOutputs     int   `json:"captured_outputs"`
	EstimatedCostMicros int64 `json:"estimated_cost_micros"`
}

type agentTaskLiveRedisAuditSnapshot struct {
	Sequence           int64  `json:"sequence"`
	EventCount         int64  `json:"event_count"`
	LastStreamID       string `json:"last_stream_id"`
	ReplayStatus       string `json:"replay_status"`
	VerifiedEventCount int64  `json:"verified_event_count"`
	StreamSHA256       string `json:"stream_sha256"`
}

type agentTaskLiveRedisRevocationSnapshot struct {
	RevokedAtUnixMS int64  `json:"revoked_at_unix_ms"`
	OperatorSHA256  string `json:"operator_sha256"`
	ReasonCode      string `json:"reason_code"`
	AuditMode       string `json:"audit_mode"`
	Sequence        int64  `json:"sequence"`
	RequestSHA256   string `json:"request_sha256"`
}

type agentTaskLiveRedisStateSnapshot struct {
	SchemaVersion              string                                `json:"schema_version"`
	AuthorizationID            string                                `json:"authorization_id"`
	AuthorizationPayloadSHA256 string                                `json:"authorization_payload_sha256"`
	AuthorizationKeyID         string                                `json:"authorization_key_id"`
	StateBackend               string                                `json:"state_backend"`
	StateNamespaceSHA256       string                                `json:"state_namespace_sha256"`
	Status                     string                                `json:"status"`
	IntegrityStatus            string                                `json:"integrity_status"`
	InspectedAtUnixMS          int64                                 `json:"inspected_at_unix_ms"`
	IssuedAtUnixMS             int64                                 `json:"issued_at_unix_ms"`
	ExpiresAtUnixMS            int64                                 `json:"expires_at_unix_ms"`
	Limits                     agentTaskLiveAuthorizationLimits      `json:"limits"`
	Usage                      *agentTaskLiveRedisUsageSnapshot      `json:"usage,omitempty"`
	Audit                      *agentTaskLiveRedisAuditSnapshot      `json:"audit,omitempty"`
	Revocation                 *agentTaskLiveRedisRevocationSnapshot `json:"revocation,omitempty"`
}

type agentTaskLiveRedisAdminOutput struct {
	SchemaVersion string                          `json:"schema_version"`
	Operation     string                          `json:"operation"`
	Changed       bool                            `json:"changed"`
	State         agentTaskLiveRedisStateSnapshot `json:"state"`
}

type agentTaskLiveRedisRevocationRequest struct {
	AuthorizationPayloadSHA256 string `json:"authorization_payload_sha256"`
	OperatorSHA256             string `json:"operator_sha256"`
	ReasonCode                 string `json:"reason_code"`
}

var inspectAgentTaskLiveRedisLedgerScript = redis.NewScript(`
local marker_exists = redis.call('EXISTS', KEYS[3])
if marker_exists ~= 1 then
  return redis.error_reply('live authorization redis ledger is not initialized')
end
if redis.call('HGET', KEYS[3], 'schema_version') ~= ARGV[1] or
   redis.call('HGET', KEYS[3], 'authorization_payload_sha256') ~= ARGV[2] or
   redis.call('HGET', KEYS[3], 'authorization_key_id') ~= ARGV[3] or
   redis.call('HGET', KEYS[3], 'expires_at_unix_ms') ~= ARGV[5] then
  return redis.error_reply('live authorization redis marker identity mismatch')
end

local status = redis.call('HGET', KEYS[3], 'status')
if status ~= 'initialized' and status ~= 'revoked' then
  return redis.error_reply('live authorization redis marker status is invalid')
end
local now_parts = redis.call('TIME')
local now_ms = tonumber(now_parts[1]) * 1000 + math.floor(tonumber(now_parts[2]) / 1000)
local state_exists = redis.call('EXISTS', KEYS[1])
local events_exists = redis.call('EXISTS', KEYS[2])
if state_exists ~= 1 or events_exists ~= 1 then
  if status ~= 'revoked' or redis.call('HGET', KEYS[3], 'revocation_audit_mode') ~= 'marker_only' then
    return redis.error_reply('live authorization redis ledger state was lost; revoke with state_integrity_incident')
  end
  return {
    status, 'state_lost', tostring(now_ms), '-1', '-1', '-1', '-1', '-1', '-1',
    redis.call('HGET', KEYS[3], 'revoked_at_unix_ms') or '',
    redis.call('HGET', KEYS[3], 'revocation_operator_sha256') or '',
    redis.call('HGET', KEYS[3], 'revocation_reason_code') or '',
    'marker_only',
    redis.call('HGET', KEYS[3], 'revocation_sequence') or '-1',
    redis.call('HGET', KEYS[3], 'revocation_request_sha256') or '',
    ''
  }
end

local expected = {
  'schema_version', ARGV[1],
  'authorization_payload_sha256', ARGV[2],
  'authorization_key_id', ARGV[3],
  'issued_at_unix_ms', ARGV[4],
  'expires_at_unix_ms', ARGV[5],
  'max_runs', ARGV[6],
  'max_provider_calls', ARGV[7],
  'max_captured_outputs', ARGV[8],
  'max_estimated_cost_micros', ARGV[9]
}
for index = 1, #expected, 2 do
  if redis.call('HGET', KEYS[1], expected[index]) ~= expected[index + 1] then
    return redis.error_reply('live authorization redis ledger identity mismatch')
  end
end
local state_status = redis.call('HGET', KEYS[1], 'status')
if status == 'initialized' and ((state_status and state_status ~= 'initialized') or redis.call('HGET', KEYS[1], 'revocation_request_sha256')) then
  return redis.error_reply('live authorization redis ledger state status is invalid')
end
local sequence = tonumber(redis.call('HGET', KEYS[1], 'sequence') or '-1')
local event_count = redis.call('XLEN', KEYS[2])
local runs = tonumber(redis.call('HGET', KEYS[1], 'runs') or '-1')
local calls = tonumber(redis.call('HGET', KEYS[1], 'provider_calls') or '-1')
local outputs = tonumber(redis.call('HGET', KEYS[1], 'captured_outputs') or '-1')
local cost = tonumber(redis.call('HGET', KEYS[1], 'estimated_cost_micros') or '-1')
if sequence < 0 or event_count ~= sequence + 1 then
  return redis.error_reply('live authorization redis ledger audit sequence mismatch')
end
if runs < 0 or calls < 0 or outputs < 0 or cost < 0 then
  return redis.error_reply('live authorization redis usage state is invalid')
end
local last_entries = redis.call('XREVRANGE', KEYS[2], '+', '-', 'COUNT', 1)
if #last_entries ~= 1 then
  return redis.error_reply('live authorization redis ledger last event is unavailable')
end
local last_stream_id = last_entries[1][1]

local revoked_at = ''
local operator_sha = ''
local reason_code = ''
local audit_mode = ''
local revocation_sequence = '-1'
local request_sha = ''
if status == 'revoked' then
  revoked_at = redis.call('HGET', KEYS[3], 'revoked_at_unix_ms') or ''
  operator_sha = redis.call('HGET', KEYS[3], 'revocation_operator_sha256') or ''
  reason_code = redis.call('HGET', KEYS[3], 'revocation_reason_code') or ''
  audit_mode = redis.call('HGET', KEYS[3], 'revocation_audit_mode') or ''
  revocation_sequence = redis.call('HGET', KEYS[3], 'revocation_sequence') or '-1'
  request_sha = redis.call('HGET', KEYS[3], 'revocation_request_sha256') or ''
  if audit_mode ~= 'stream' or
     redis.call('HGET', KEYS[1], 'status') ~= 'revoked' or
     redis.call('HGET', KEYS[1], 'revocation_request_sha256') ~= request_sha or
     tonumber(revocation_sequence) ~= sequence then
    return redis.error_reply('live authorization redis revocation state is inconsistent')
  end
end
return {
  status, 'intact', tostring(now_ms), tostring(sequence), tostring(event_count),
  tostring(runs), tostring(calls), tostring(outputs), tostring(cost),
  revoked_at, operator_sha, reason_code, audit_mode, revocation_sequence, request_sha,
  last_stream_id
}
`)

var revokeAgentTaskLiveRedisLedgerScript = redis.NewScript(`
local marker_exists = redis.call('EXISTS', KEYS[3])
if marker_exists ~= 1 then
  return redis.error_reply('live authorization redis ledger is not initialized')
end
if redis.call('HGET', KEYS[3], 'schema_version') ~= ARGV[1] or
   redis.call('HGET', KEYS[3], 'authorization_payload_sha256') ~= ARGV[2] or
   redis.call('HGET', KEYS[3], 'authorization_key_id') ~= ARGV[3] or
   redis.call('HGET', KEYS[3], 'expires_at_unix_ms') ~= ARGV[5] then
  return redis.error_reply('live authorization redis marker identity mismatch')
end
local now_parts = redis.call('TIME')
local now_ms = tonumber(now_parts[1]) * 1000 + math.floor(tonumber(now_parts[2]) / 1000)
if now_ms < tonumber(ARGV[4]) or now_ms >= tonumber(ARGV[5]) then
  return redis.error_reply('live authorization redis ledger is outside its validity window')
end

local status = redis.call('HGET', KEYS[3], 'status')
if status == 'revoked' then
  if not redis.call('HGET', KEYS[3], 'revocation_request_sha256') then
    return redis.error_reply('live authorization redis revocation marker is incomplete')
  end
  return 0
end
if status ~= 'initialized' then
  return redis.error_reply('live authorization redis marker status is invalid')
end

local state_exists = redis.call('EXISTS', KEYS[1])
local events_exists = redis.call('EXISTS', KEYS[2])
if state_exists ~= 1 or events_exists ~= 1 then
  if ARGV[11] ~= 'state_integrity_incident' then
    return redis.error_reply('live authorization redis ledger state was lost; revocation reason must be state_integrity_incident')
  end
  redis.call('HSET', KEYS[3],
    'status', 'revoked',
    'revoked_at_unix_ms', tostring(now_ms),
    'revocation_operator_sha256', ARGV[10],
    'revocation_reason_code', ARGV[11],
    'revocation_audit_mode', 'marker_only',
    'revocation_sequence', '-1',
    'revocation_request_sha256', ARGV[12])
  return 1
end

local expected = {
  'schema_version', ARGV[1],
  'authorization_payload_sha256', ARGV[2],
  'authorization_key_id', ARGV[3],
  'issued_at_unix_ms', ARGV[4],
  'expires_at_unix_ms', ARGV[5],
  'max_runs', ARGV[6],
  'max_provider_calls', ARGV[7],
  'max_captured_outputs', ARGV[8],
  'max_estimated_cost_micros', ARGV[9]
}
for index = 1, #expected, 2 do
  if redis.call('HGET', KEYS[1], expected[index]) ~= expected[index + 1] then
    return redis.error_reply('live authorization redis ledger identity mismatch')
  end
end
local state_status = redis.call('HGET', KEYS[1], 'status')
if (state_status and state_status ~= 'initialized') or redis.call('HGET', KEYS[1], 'revocation_request_sha256') then
  return redis.error_reply('live authorization redis ledger state status is invalid')
end
local sequence = tonumber(redis.call('HGET', KEYS[1], 'sequence') or '-1')
if sequence < 0 or redis.call('XLEN', KEYS[2]) ~= sequence + 1 then
  return redis.error_reply('live authorization redis ledger audit sequence mismatch')
end
local next_sequence = sequence + 1
redis.call('HSET', KEYS[1],
  'sequence', tostring(next_sequence),
  'status', 'revoked',
  'revoked_at_unix_ms', tostring(now_ms),
  'revocation_operator_sha256', ARGV[10],
  'revocation_reason_code', ARGV[11],
  'revocation_request_sha256', ARGV[12])
redis.call('HSET', KEYS[3],
  'status', 'revoked',
  'revoked_at_unix_ms', tostring(now_ms),
  'revocation_operator_sha256', ARGV[10],
  'revocation_reason_code', ARGV[11],
  'revocation_audit_mode', 'stream',
  'revocation_sequence', tostring(next_sequence),
  'revocation_request_sha256', ARGV[12])
redis.call('XADD', KEYS[2], '*',
  'sequence', tostring(next_sequence),
  'event_type', 'authorization_revoked',
  'created_at_unix_ms', tostring(now_ms),
  'authorization_payload_sha256', ARGV[2],
  'operator_sha256', ARGV[10],
  'reason_code', ARGV[11],
  'request_sha256', ARGV[12])
redis.call('PEXPIREAT', KEYS[1], tonumber(ARGV[5]))
redis.call('PEXPIREAT', KEYS[2], tonumber(ARGV[5]))
return 1
`)

func (ledger *agentTaskLiveRedisLedger) Inspect(ctx context.Context) (agentTaskLiveRedisStateSnapshot, error) {
	if ledger == nil || ledger.client == nil {
		return agentTaskLiveRedisStateSnapshot{}, errors.New("live authorization redis ledger is not configured")
	}
	operationCtx, cancel := ledger.operationContext(ctx)
	result, err := inspectAgentTaskLiveRedisLedgerScript.Run(
		operationCtx,
		ledger.client,
		[]string{ledger.stateKey, ledger.eventsKey, ledger.markerKey},
		ledger.identityArguments()...,
	).Result()
	cancel()
	if err != nil {
		return agentTaskLiveRedisStateSnapshot{}, fmt.Errorf("inspect live authorization redis ledger: %w", err)
	}
	snapshot, err := ledger.decodeStateSnapshot(result)
	if err != nil {
		return agentTaskLiveRedisStateSnapshot{}, err
	}
	if snapshot.IntegrityStatus == "intact" {
		if err := ledger.verifyAuditStream(ctx, &snapshot); err != nil {
			return agentTaskLiveRedisStateSnapshot{}, err
		}
	}
	return snapshot, nil
}

func (ledger *agentTaskLiveRedisLedger) Revoke(
	ctx context.Context,
	operatorID string,
	reasonCode string,
) (bool, agentTaskLiveRedisStateSnapshot, error) {
	if ledger == nil || ledger.client == nil {
		return false, agentTaskLiveRedisStateSnapshot{}, errors.New("live authorization redis ledger is not configured")
	}
	operatorID = strings.TrimSpace(operatorID)
	reasonCode = strings.ToLower(strings.TrimSpace(reasonCode))
	if err := validateAgentTaskLiveRedisRevocationMetadata(operatorID, reasonCode); err != nil {
		return false, agentTaskLiveRedisStateSnapshot{}, err
	}
	operatorHash := hashAgentTaskLiveAuthorizationSubject(operatorID)
	requestPayload, err := json.Marshal(agentTaskLiveRedisRevocationRequest{
		AuthorizationPayloadSHA256: ledger.authorizationPayloadHash,
		OperatorSHA256:             operatorHash,
		ReasonCode:                 reasonCode,
	})
	if err != nil {
		return false, agentTaskLiveRedisStateSnapshot{}, fmt.Errorf("encode live authorization redis revocation: %w", err)
	}
	requestDigest := sha256.Sum256(requestPayload)
	arguments := append(
		ledger.identityArguments(),
		operatorHash,
		reasonCode,
		hex.EncodeToString(requestDigest[:]),
	)
	operationCtx, cancel := ledger.operationContext(ctx)
	changed, err := revokeAgentTaskLiveRedisLedgerScript.Run(
		operationCtx,
		ledger.client,
		[]string{ledger.stateKey, ledger.eventsKey, ledger.markerKey},
		arguments...,
	).Int()
	cancel()
	if err != nil {
		return false, agentTaskLiveRedisStateSnapshot{}, fmt.Errorf("revoke live authorization redis ledger: %w", err)
	}
	if changed != 0 && changed != 1 {
		return false, agentTaskLiveRedisStateSnapshot{}, fmt.Errorf("revoke live authorization redis ledger returned unexpected result %d", changed)
	}
	snapshot, err := ledger.Inspect(ctx)
	if err != nil {
		return false, agentTaskLiveRedisStateSnapshot{}, err
	}
	return changed == 1, snapshot, nil
}

func validateAgentTaskLiveRedisRevocationMetadata(operatorID, reasonCode string) error {
	operatorID = strings.TrimSpace(operatorID)
	reasonCode = strings.ToLower(strings.TrimSpace(reasonCode))
	if !agentTaskLiveRedisRevocationOperatorPattern.MatchString(operatorID) {
		return errors.New("live authorization revocation operator must be a portable 1-128 character identifier")
	}
	if _, ok := agentTaskLiveRedisRevocationReasons[reasonCode]; !ok {
		return errors.New("live authorization revocation reason code is invalid")
	}
	return nil
}

func (ledger *agentTaskLiveRedisLedger) decodeStateSnapshot(result any) (agentTaskLiveRedisStateSnapshot, error) {
	values, ok := result.([]interface{})
	if !ok || len(values) != 16 {
		return agentTaskLiveRedisStateSnapshot{}, errors.New("live authorization redis inspection returned an invalid shape")
	}
	fields := make([]string, len(values))
	for index, value := range values {
		fields[index] = fmt.Sprint(value)
	}
	parseInt64 := func(index int, label string) (int64, error) {
		value, err := strconv.ParseInt(fields[index], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("live authorization redis inspection %s is invalid", label)
		}
		return value, nil
	}
	nowMS, err := parseInt64(2, "server time")
	if err != nil {
		return agentTaskLiveRedisStateSnapshot{}, err
	}
	snapshot := agentTaskLiveRedisStateSnapshot{
		SchemaVersion:              agentTaskLiveRedisStateSnapshotSchemaVersion,
		AuthorizationID:            ledger.authorization.AuthorizationID,
		AuthorizationPayloadSHA256: ledger.authorizationPayloadHash,
		AuthorizationKeyID:         ledger.keyID,
		StateBackend:               "redis",
		StateNamespaceSHA256:       ledger.stateNamespaceHash,
		Status:                     fields[0],
		IntegrityStatus:            fields[1],
		InspectedAtUnixMS:          nowMS,
		IssuedAtUnixMS:             ledger.authorization.IssuedAt.UnixMilli(),
		ExpiresAtUnixMS:            ledger.authorization.ExpiresAt.UnixMilli(),
		Limits:                     ledger.authorization.Limits,
	}
	if snapshot.Status != "initialized" && snapshot.Status != "revoked" {
		return agentTaskLiveRedisStateSnapshot{}, errors.New("live authorization redis inspection status is invalid")
	}
	if snapshot.IntegrityStatus != "intact" && snapshot.IntegrityStatus != "state_lost" {
		return agentTaskLiveRedisStateSnapshot{}, errors.New("live authorization redis inspection integrity status is invalid")
	}
	if snapshot.IntegrityStatus == "intact" {
		sequence, parseErr := parseInt64(3, "sequence")
		if parseErr != nil {
			return agentTaskLiveRedisStateSnapshot{}, parseErr
		}
		eventCount, parseErr := parseInt64(4, "event count")
		if parseErr != nil {
			return agentTaskLiveRedisStateSnapshot{}, parseErr
		}
		runs, parseErr := parseInt64(5, "run usage")
		if parseErr != nil {
			return agentTaskLiveRedisStateSnapshot{}, parseErr
		}
		providerCalls, parseErr := parseInt64(6, "provider call usage")
		if parseErr != nil {
			return agentTaskLiveRedisStateSnapshot{}, parseErr
		}
		capturedOutputs, parseErr := parseInt64(7, "captured output usage")
		if parseErr != nil {
			return agentTaskLiveRedisStateSnapshot{}, parseErr
		}
		cost, parseErr := parseInt64(8, "estimated cost usage")
		if parseErr != nil {
			return agentTaskLiveRedisStateSnapshot{}, parseErr
		}
		if runs > int64(^uint(0)>>1) || providerCalls > int64(^uint(0)>>1) || capturedOutputs > int64(^uint(0)>>1) {
			return agentTaskLiveRedisStateSnapshot{}, errors.New("live authorization redis inspection usage exceeds local integer bounds")
		}
		snapshot.Usage = &agentTaskLiveRedisUsageSnapshot{
			Runs: int(runs), ProviderCalls: int(providerCalls), CapturedOutputs: int(capturedOutputs),
			EstimatedCostMicros: cost,
		}
		if strings.TrimSpace(fields[15]) == "" {
			return agentTaskLiveRedisStateSnapshot{}, errors.New("live authorization redis inspection last stream ID is missing")
		}
		snapshot.Audit = &agentTaskLiveRedisAuditSnapshot{
			Sequence: sequence, EventCount: eventCount, LastStreamID: fields[15],
		}
	}
	if snapshot.Status == "revoked" {
		revokedAt, parseErr := parseInt64(9, "revoked time")
		if parseErr != nil {
			return agentTaskLiveRedisStateSnapshot{}, parseErr
		}
		revocationSequence, parseErr := parseInt64(13, "revocation sequence")
		if parseErr != nil {
			return agentTaskLiveRedisStateSnapshot{}, parseErr
		}
		if !validAgentTaskLiveAuthorizationSHA256(fields[10]) || !validAgentTaskLiveAuthorizationSHA256(fields[14]) {
			return agentTaskLiveRedisStateSnapshot{}, errors.New("live authorization redis revocation digests are invalid")
		}
		if _, ok := agentTaskLiveRedisRevocationReasons[fields[11]]; !ok {
			return agentTaskLiveRedisStateSnapshot{}, errors.New("live authorization redis revocation reason is invalid")
		}
		if fields[12] != "stream" && fields[12] != "marker_only" {
			return agentTaskLiveRedisStateSnapshot{}, errors.New("live authorization redis revocation audit mode is invalid")
		}
		snapshot.Revocation = &agentTaskLiveRedisRevocationSnapshot{
			RevokedAtUnixMS: revokedAt, OperatorSHA256: fields[10], ReasonCode: fields[11],
			AuditMode: fields[12], Sequence: revocationSequence, RequestSHA256: fields[14],
		}
	}
	return snapshot, nil
}

func inspectAgentTaskLiveRedisAuthorizationState(
	ctx context.Context,
	authorizationPath string,
	redisConfigPath string,
	key []byte,
	keyID string,
	now time.Time,
) (agentTaskLiveRedisStateSnapshot, error) {
	ledger, client, err := openAgentTaskLiveRedisAdminLedger(authorizationPath, redisConfigPath, key, keyID, now)
	if err != nil {
		return agentTaskLiveRedisStateSnapshot{}, err
	}
	defer client.Close()
	return ledger.Inspect(ctx)
}

func revokeAgentTaskLiveRedisAuthorizationState(
	ctx context.Context,
	authorizationPath string,
	redisConfigPath string,
	key []byte,
	keyID string,
	operatorID string,
	reasonCode string,
	now time.Time,
) (bool, agentTaskLiveRedisStateSnapshot, error) {
	ledger, client, err := openAgentTaskLiveRedisAdminLedger(authorizationPath, redisConfigPath, key, keyID, now)
	if err != nil {
		return false, agentTaskLiveRedisStateSnapshot{}, err
	}
	defer client.Close()
	return ledger.Revoke(ctx, operatorID, reasonCode)
}

func openAgentTaskLiveRedisAdminLedger(
	authorizationPath string,
	redisConfigPath string,
	key []byte,
	keyID string,
	now time.Time,
) (*agentTaskLiveRedisLedger, *redis.Client, error) {
	authorization, err := loadAndVerifyAgentTaskLiveAuthorizationDocument(authorizationPath, key, keyID, now)
	if err != nil {
		return nil, nil, err
	}
	config, err := loadAgentTaskLiveRedisConfig(redisConfigPath)
	if err != nil {
		return nil, nil, err
	}
	client, err := newAgentTaskLiveRedisClient(config)
	if err != nil {
		return nil, nil, err
	}
	ledger, err := newAgentTaskLiveRedisLedger(client, config, authorization)
	if err != nil {
		_ = client.Close()
		return nil, nil, err
	}
	return ledger, client, nil
}
