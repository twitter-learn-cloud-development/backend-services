package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"

	"twitter-clone/internal/module/agent/eval"
)

const (
	agentTaskLiveRedisConfigSchemaVersion = "agent-task-live-authorization-redis-config/v1"
	agentTaskLiveRedisLedgerSchemaVersion = "agent-task-live-authorization-redis-ledger/v1"
	defaultAgentTaskLiveRedisKeyPrefix    = "agent-task-eval:live-authorization"
	defaultAgentTaskLiveRedisTimeoutMS    = 3000
)

var agentTaskLiveRedisKeyPrefixPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9:_-]{0,127}$`)

type agentTaskLiveRedisConfig struct {
	SchemaVersion      string `json:"schema_version"`
	Address            string `json:"address"`
	UsernameEnv        string `json:"username_env,omitempty"`
	PasswordEnv        string `json:"password_env,omitempty"`
	Database           int    `json:"database"`
	KeyPrefix          string `json:"key_prefix"`
	TLSEnabled         bool   `json:"tls_enabled"`
	TLSServerName      string `json:"tls_server_name,omitempty"`
	ConnectTimeoutMS   int    `json:"connect_timeout_ms"`
	OperationTimeoutMS int    `json:"operation_timeout_ms"`
}

type agentTaskLiveRedisLedger struct {
	client                   *redis.Client
	authorization            agentTaskLiveAuthorization
	authorizationPayloadHash string
	keyID                    string
	invocationID             string
	stateKey                 string
	eventsKey                string
	markerKey                string
	stateNamespaceHash       string
	operationTimeout         time.Duration
}

type agentTaskLiveRedisReservation struct {
	AuthorizationPayloadSHA256 string `json:"authorization_payload_sha256"`
	ReservationID              string `json:"reservation_id"`
	InvocationSHA256           string `json:"invocation_sha256"`
	EventType                  string `json:"event_type"`
	SubjectSHA256              string `json:"subject_sha256,omitempty"`
	Runs                       int    `json:"runs"`
	ProviderCalls              int    `json:"provider_calls"`
	CapturedOutputs            int    `json:"captured_outputs"`
	EstimatedCostMicros        int64  `json:"estimated_cost_micros"`
}

var initializeAgentTaskLiveRedisLedgerScript = redis.NewScript(`
local now_parts = redis.call('TIME')
local now_ms = tonumber(now_parts[1]) * 1000 + math.floor(tonumber(now_parts[2]) / 1000)
local issued_ms = tonumber(ARGV[4])
local expires_ms = tonumber(ARGV[5])
if now_ms < issued_ms or now_ms >= expires_ms then
  return redis.error_reply('live authorization redis ledger is outside its validity window')
end

local state_exists = redis.call('EXISTS', KEYS[1])
local events_exists = redis.call('EXISTS', KEYS[2])
local marker_exists = redis.call('EXISTS', KEYS[3])
if marker_exists == 1 then
  if redis.call('HGET', KEYS[3], 'schema_version') ~= ARGV[1] or
     redis.call('HGET', KEYS[3], 'authorization_payload_sha256') ~= ARGV[2] or
     redis.call('HGET', KEYS[3], 'authorization_key_id') ~= ARGV[3] or
     redis.call('HGET', KEYS[3], 'expires_at_unix_ms') ~= ARGV[5] then
    return redis.error_reply('live authorization redis marker identity mismatch')
  end
  local marker_status = redis.call('HGET', KEYS[3], 'status')
  if marker_status == 'revoked' then
    return redis.error_reply('live authorization redis authorization is revoked')
  end
  if marker_status ~= 'initialized' then
    return redis.error_reply('live authorization redis marker identity mismatch')
  end
end
if state_exists == 1 then
  if events_exists ~= 1 or marker_exists ~= 1 then
    return redis.error_reply('live authorization redis ledger has partial state')
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
  return 0
end
if marker_exists == 1 then
  return redis.error_reply('live authorization redis ledger state was lost; revoke the authorization')
end
if events_exists ~= 0 then
  return redis.error_reply('live authorization redis ledger has orphaned audit state')
end

redis.call('HSET', KEYS[3],
  'schema_version', ARGV[1],
  'authorization_payload_sha256', ARGV[2],
  'authorization_key_id', ARGV[3],
  'expires_at_unix_ms', ARGV[5],
  'status', 'initialized')
redis.call('HSET', KEYS[1],
  'schema_version', ARGV[1],
  'authorization_payload_sha256', ARGV[2],
  'authorization_key_id', ARGV[3],
  'issued_at_unix_ms', ARGV[4],
  'expires_at_unix_ms', ARGV[5],
  'max_runs', ARGV[6],
  'max_provider_calls', ARGV[7],
  'max_captured_outputs', ARGV[8],
  'max_estimated_cost_micros', ARGV[9],
  'status', 'initialized',
  'runs', '0',
  'provider_calls', '0',
  'captured_outputs', '0',
  'estimated_cost_micros', '0',
  'sequence', '0')
redis.call('XADD', KEYS[2], '*',
  'sequence', '0',
  'event_type', 'initialized',
  'created_at_unix_ms', tostring(now_ms),
  'authorization_payload_sha256', ARGV[2])
redis.call('PEXPIREAT', KEYS[1], expires_ms)
redis.call('PEXPIREAT', KEYS[2], expires_ms)
return 1
`)

var reserveAgentTaskLiveRedisLedgerScript = redis.NewScript(`
local state_exists = redis.call('EXISTS', KEYS[1])
local events_exists = redis.call('EXISTS', KEYS[2])
local marker_exists = redis.call('EXISTS', KEYS[3])
if marker_exists == 1 then
  if redis.call('HGET', KEYS[3], 'schema_version') ~= ARGV[1] or
     redis.call('HGET', KEYS[3], 'authorization_payload_sha256') ~= ARGV[2] or
     redis.call('HGET', KEYS[3], 'authorization_key_id') ~= ARGV[3] or
     redis.call('HGET', KEYS[3], 'expires_at_unix_ms') ~= ARGV[5] then
    return redis.error_reply('live authorization redis marker identity mismatch')
  end
  local marker_status = redis.call('HGET', KEYS[3], 'status')
  if marker_status == 'revoked' then
    return redis.error_reply('live authorization redis authorization is revoked')
  end
  if marker_status ~= 'initialized' then
    return redis.error_reply('live authorization redis marker identity mismatch')
  end
end
if marker_exists == 1 and (state_exists ~= 1 or events_exists ~= 1) then
  return redis.error_reply('live authorization redis ledger state was lost; revoke the authorization')
end
if state_exists ~= 1 or events_exists ~= 1 or marker_exists ~= 1 then
  return redis.error_reply('live authorization redis ledger is not initialized')
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
local now_parts = redis.call('TIME')
local now_ms = tonumber(now_parts[1]) * 1000 + math.floor(tonumber(now_parts[2]) / 1000)
local issued_ms = tonumber(ARGV[4])
local expires_ms = tonumber(ARGV[5])
if now_ms < issued_ms or now_ms >= expires_ms then
  return redis.error_reply('live authorization redis ledger is outside its validity window')
end

local sequence = tonumber(redis.call('HGET', KEYS[1], 'sequence') or '-1')
if sequence < 0 or redis.call('XLEN', KEYS[2]) ~= sequence + 1 then
  return redis.error_reply('live authorization redis ledger audit sequence mismatch')
end
local reservation_field = 'reservation:' .. ARGV[10]
local existing_request = redis.call('HGET', KEYS[1], reservation_field)
if existing_request then
  if existing_request ~= ARGV[11] then
    return redis.error_reply('live authorization redis reservation identity conflict')
  end
  return sequence
end

local add_runs = tonumber(ARGV[15])
local add_calls = tonumber(ARGV[16])
local add_outputs = tonumber(ARGV[17])
local add_cost = tonumber(ARGV[18])
if not add_runs or not add_calls or not add_outputs or not add_cost or
   add_runs < 0 or add_calls < 0 or add_outputs < 0 or add_cost < 0 then
  return redis.error_reply('live authorization redis reservation is invalid')
end
if ARGV[13] ~= 'run_reserved' and ARGV[13] ~= 'provider_call_reserved' then
  return redis.error_reply('live authorization redis event type is invalid')
end
if ARGV[13] == 'run_reserved' then
  if add_runs ~= 1 or add_calls ~= 0 or add_cost ~= 0 then
    return redis.error_reply('live authorization redis run reservation delta is invalid')
  end
elseif add_runs ~= 0 or add_calls ~= 1 or add_outputs ~= 0 then
  return redis.error_reply('live authorization redis provider call reservation delta is invalid')
end

local runs = tonumber(redis.call('HGET', KEYS[1], 'runs') or '-1')
local calls = tonumber(redis.call('HGET', KEYS[1], 'provider_calls') or '-1')
local outputs = tonumber(redis.call('HGET', KEYS[1], 'captured_outputs') or '-1')
local cost = tonumber(redis.call('HGET', KEYS[1], 'estimated_cost_micros') or '-1')
if runs < 0 or calls < 0 or outputs < 0 or cost < 0 then
  return redis.error_reply('live authorization redis usage state is invalid')
end
local next_runs = runs + add_runs
local next_calls = calls + add_calls
local next_outputs = outputs + add_outputs
local next_cost = cost + add_cost
if next_runs > tonumber(ARGV[6]) then
  return redis.error_reply('live authorization run budget exhausted')
end
if next_calls > tonumber(ARGV[7]) then
  return redis.error_reply('live authorization provider call budget exhausted')
end
if next_outputs > tonumber(ARGV[8]) then
  return redis.error_reply('live authorization captured output budget exhausted')
end
if next_cost > tonumber(ARGV[9]) then
  return redis.error_reply('live authorization estimated cost budget exhausted')
end

local next_sequence = sequence + 1
redis.call('HSET', KEYS[1],
  'runs', tostring(next_runs),
  'provider_calls', tostring(next_calls),
  'captured_outputs', tostring(next_outputs),
  'estimated_cost_micros', tostring(next_cost),
  'sequence', tostring(next_sequence),
  reservation_field, ARGV[11])
redis.call('XADD', KEYS[2], '*',
  'sequence', tostring(next_sequence),
  'event_type', ARGV[13],
  'created_at_unix_ms', tostring(now_ms),
  'invocation_sha256', ARGV[12],
  'subject_sha256', ARGV[14],
  'runs', ARGV[15],
  'provider_calls', ARGV[16],
  'captured_outputs', ARGV[17],
  'estimated_cost_micros', ARGV[18],
  'reservation_sha256', ARGV[11])
redis.call('PEXPIREAT', KEYS[1], expires_ms)
redis.call('PEXPIREAT', KEYS[2], expires_ms)
return next_sequence
`)

func loadAgentTaskLiveRedisConfig(path string) (agentTaskLiveRedisConfig, error) {
	payload, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		return agentTaskLiveRedisConfig{}, fmt.Errorf("read live authorization redis config: %w", err)
	}
	var config agentTaskLiveRedisConfig
	if err := decodeStrictJSON(payload, &config, "live authorization redis config"); err != nil {
		return agentTaskLiveRedisConfig{}, err
	}
	config.SchemaVersion = strings.TrimSpace(config.SchemaVersion)
	config.Address = strings.TrimSpace(config.Address)
	config.UsernameEnv = strings.TrimSpace(config.UsernameEnv)
	config.PasswordEnv = strings.TrimSpace(config.PasswordEnv)
	config.KeyPrefix = strings.TrimSpace(config.KeyPrefix)
	config.TLSServerName = strings.TrimSpace(config.TLSServerName)
	if config.KeyPrefix == "" {
		config.KeyPrefix = defaultAgentTaskLiveRedisKeyPrefix
	}
	if config.ConnectTimeoutMS == 0 {
		config.ConnectTimeoutMS = defaultAgentTaskLiveRedisTimeoutMS
	}
	if config.OperationTimeoutMS == 0 {
		config.OperationTimeoutMS = defaultAgentTaskLiveRedisTimeoutMS
	}
	if err := validateAgentTaskLiveRedisConfig(config); err != nil {
		return agentTaskLiveRedisConfig{}, err
	}
	return config, nil
}

func validateAgentTaskLiveRedisConfig(config agentTaskLiveRedisConfig) error {
	if config.SchemaVersion != agentTaskLiveRedisConfigSchemaVersion {
		return fmt.Errorf("unsupported live authorization redis config schema version %q", config.SchemaVersion)
	}
	host, portText, err := net.SplitHostPort(config.Address)
	if err != nil || strings.TrimSpace(host) == "" || strings.ContainsAny(host, " \t\r\n@/") {
		return errors.New("live authorization redis address must be a host:port without credentials")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return errors.New("live authorization redis port is invalid")
	}
	if config.Database < 0 || config.Database > 1024 {
		return errors.New("live authorization redis database is outside supported bounds")
	}
	if !agentTaskLiveRedisKeyPrefixPattern.MatchString(config.KeyPrefix) {
		return errors.New("live authorization redis key prefix is invalid")
	}
	if config.UsernameEnv != "" && !validEnvironmentVariableName(config.UsernameEnv) {
		return errors.New("live authorization redis username environment variable is invalid")
	}
	if config.PasswordEnv != "" && !validEnvironmentVariableName(config.PasswordEnv) {
		return errors.New("live authorization redis password environment variable is invalid")
	}
	if config.UsernameEnv != "" && config.PasswordEnv == "" {
		return errors.New("live authorization redis username requires a password environment variable")
	}
	if !config.TLSEnabled && config.TLSServerName != "" {
		return errors.New("live authorization redis TLS server name requires TLS")
	}
	if config.TLSEnabled && config.TLSServerName == "" {
		return errors.New("live authorization redis TLS requires an explicit server name")
	}
	if config.ConnectTimeoutMS < 100 || config.ConnectTimeoutMS > 30_000 ||
		config.OperationTimeoutMS < 100 || config.OperationTimeoutMS > 30_000 {
		return errors.New("live authorization redis timeouts must be between 100 and 30000 milliseconds")
	}
	return nil
}

func newAgentTaskLiveRedisClient(config agentTaskLiveRedisConfig) (*redis.Client, error) {
	username, err := readAgentTaskLiveRedisSecret(config.UsernameEnv, "username")
	if err != nil {
		return nil, err
	}
	password, err := readAgentTaskLiveRedisSecret(config.PasswordEnv, "password")
	if err != nil {
		return nil, err
	}
	connectTimeout := time.Duration(config.ConnectTimeoutMS) * time.Millisecond
	operationTimeout := time.Duration(config.OperationTimeoutMS) * time.Millisecond
	options := &redis.Options{
		Addr: config.Address, Username: username, Password: password, DB: config.Database,
		MaxRetries: 2, PoolSize: 4, MinIdleConns: 0,
		DialTimeout: connectTimeout, ReadTimeout: operationTimeout, WriteTimeout: operationTimeout,
	}
	if config.TLSEnabled {
		options.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12, ServerName: config.TLSServerName}
	}
	client := redis.NewClient(options)
	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("connect live authorization redis ledger: %w", err)
	}
	return client, nil
}

func readAgentTaskLiveRedisSecret(envName, label string) (string, error) {
	if envName == "" {
		return "", nil
	}
	value, ok := os.LookupEnv(envName)
	if !ok || value == "" {
		return "", fmt.Errorf("live authorization redis %s environment variable %q is missing or empty", label, envName)
	}
	return value, nil
}

func newAgentTaskLiveRedisLedger(
	client *redis.Client,
	config agentTaskLiveRedisConfig,
	authorization agentTaskLiveAuthorization,
) (*agentTaskLiveRedisLedger, error) {
	if client == nil {
		return nil, errors.New("live authorization redis client is required")
	}
	if err := validateAgentTaskLiveRedisConfig(config); err != nil {
		return nil, err
	}
	if authorization.Integrity == nil {
		return nil, errors.New("live authorization redis ledger requires a signed authorization")
	}
	payload, err := unsignedAgentTaskLiveAuthorizationPayload(authorization)
	if err != nil {
		return nil, err
	}
	payloadDigest := sha256.Sum256(payload)
	payloadHash := hex.EncodeToString(payloadDigest[:])
	invocationID, err := randomAgentTaskLiveAuthorizationID()
	if err != nil {
		return nil, err
	}
	keyDigest := sha256.Sum256([]byte(authorization.AuthorizationID + ":" + payloadHash))
	keyTag := hex.EncodeToString(keyDigest[:])
	stateKey := config.KeyPrefix + ":{" + keyTag + "}:state"
	eventsKey := config.KeyPrefix + ":{" + keyTag + "}:events"
	markerKey := config.KeyPrefix + ":{" + keyTag + "}:marker"
	identityPayload, err := json.Marshal(struct {
		SchemaVersion string `json:"schema_version"`
		Address       string `json:"address"`
		Database      int    `json:"database"`
		KeyPrefix     string `json:"key_prefix"`
		TLSEnabled    bool   `json:"tls_enabled"`
		TLSServerName string `json:"tls_server_name,omitempty"`
		StateKey      string `json:"state_key"`
	}{
		SchemaVersion: config.SchemaVersion, Address: config.Address, Database: config.Database,
		KeyPrefix: config.KeyPrefix, TLSEnabled: config.TLSEnabled, TLSServerName: config.TLSServerName,
		StateKey: stateKey,
	})
	if err != nil {
		return nil, fmt.Errorf("encode live authorization redis namespace identity: %w", err)
	}
	namespaceDigest := sha256.Sum256(identityPayload)
	return &agentTaskLiveRedisLedger{
		client: client, authorization: authorization, authorizationPayloadHash: payloadHash,
		keyID: authorization.Integrity.KeyID, invocationID: invocationID,
		stateKey: stateKey, eventsKey: eventsKey, markerKey: markerKey,
		stateNamespaceHash: hex.EncodeToString(namespaceDigest[:]),
		operationTimeout:   time.Duration(config.OperationTimeoutMS) * time.Millisecond,
	}, nil
}

func (ledger *agentTaskLiveRedisLedger) Evidence() eval.AgentTaskLiveAuthorizationEvidence {
	if ledger == nil {
		return eval.AgentTaskLiveAuthorizationEvidence{}
	}
	limits := ledger.authorization.Limits
	return eval.AgentTaskLiveAuthorizationEvidence{
		SchemaVersion:              agentTaskLiveAuthorizationSchemaVersion,
		AuthorizationID:            ledger.authorization.AuthorizationID,
		AuthorizationPayloadSHA256: ledger.authorizationPayloadHash,
		AuthorizationKeyID:         ledger.keyID,
		InvocationSHA256:           hashAgentTaskLiveAuthorizationSubject(ledger.invocationID),
		StateBackend:               "redis", StateNamespaceSHA256: ledger.stateNamespaceHash,
		Limits: eval.AgentTaskLiveAuthorizationLimits{
			MaxRuns: limits.MaxRuns, MaxProviderCalls: limits.MaxProviderCalls,
			MaxCapturedOutputs:     limits.MaxCapturedOutputs,
			MaxEstimatedCostMicros: limits.MaxEstimatedCostMicros,
		},
	}
}

func (ledger *agentTaskLiveRedisLedger) Initialize(ctx context.Context) (bool, error) {
	if ledger == nil || ledger.client == nil {
		return false, errors.New("live authorization redis ledger is not configured")
	}
	ctx, cancel := ledger.operationContext(ctx)
	defer cancel()
	created, err := initializeAgentTaskLiveRedisLedgerScript.Run(
		ctx,
		ledger.client,
		[]string{ledger.stateKey, ledger.eventsKey, ledger.markerKey},
		ledger.identityArguments()...,
	).Int()
	if err != nil {
		return false, fmt.Errorf("initialize live authorization redis ledger: %w", err)
	}
	if created != 0 && created != 1 {
		return false, fmt.Errorf("initialize live authorization redis ledger returned unexpected result %d", created)
	}
	return created == 1, nil
}

func (ledger *agentTaskLiveRedisLedger) ReserveRunContext(
	ctx context.Context,
	capturedOutputs int,
	now time.Time,
) error {
	if capturedOutputs < 0 {
		return errors.New("captured output reservation cannot be negative")
	}
	return ledger.reserve(ctx, agentTaskLiveLedgerRecord{
		EventType: "run_reserved", Runs: 1, CapturedOutputs: capturedOutputs,
		SubjectSHA256: hashAgentTaskLiveAuthorizationSubject(ledger.invocationID), CreatedAt: now.UTC(),
	})
}

func (ledger *agentTaskLiveRedisLedger) ReserveProviderCallContext(
	ctx context.Context,
	subject string,
	estimatedCostMicros int64,
	now time.Time,
) error {
	if estimatedCostMicros < 0 {
		return errors.New("provider call cost reservation cannot be negative")
	}
	return ledger.reserve(ctx, agentTaskLiveLedgerRecord{
		EventType: "provider_call_reserved", ProviderCalls: 1, EstimatedCostMicros: estimatedCostMicros,
		SubjectSHA256: hashAgentTaskLiveAuthorizationSubject(subject), CreatedAt: now.UTC(),
	})
}

func (ledger *agentTaskLiveRedisLedger) reserve(ctx context.Context, record agentTaskLiveLedgerRecord) error {
	reservationID, err := randomAgentTaskLiveAuthorizationID()
	if err != nil {
		return err
	}
	return ledger.reserveWithID(ctx, record, reservationID)
}

func (ledger *agentTaskLiveRedisLedger) reserveWithID(
	ctx context.Context,
	record agentTaskLiveLedgerRecord,
	reservationID string,
) error {
	if ledger == nil || ledger.client == nil {
		return errors.New("live authorization redis ledger is not configured")
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	} else {
		record.CreatedAt = record.CreatedAt.UTC()
	}
	if record.CreatedAt.Before(ledger.authorization.IssuedAt) || !record.CreatedAt.Before(ledger.authorization.ExpiresAt) {
		return errors.New("live authorization redis reservation is outside its local validity window")
	}
	if record.Runs < 0 || record.ProviderCalls < 0 || record.CapturedOutputs < 0 || record.EstimatedCostMicros < 0 {
		return errors.New("live authorization reservation cannot decrement usage")
	}
	switch record.EventType {
	case "run_reserved":
		if record.Runs != 1 || record.ProviderCalls != 0 || record.EstimatedCostMicros != 0 {
			return errors.New("live authorization redis run reservation delta is invalid")
		}
	case "provider_call_reserved":
		if record.Runs != 0 || record.ProviderCalls != 1 || record.CapturedOutputs != 0 {
			return errors.New("live authorization redis provider call reservation delta is invalid")
		}
	default:
		return errors.New("live authorization redis event type is invalid")
	}
	if !validAgentTaskLiveAuthorizationSHA256(record.SubjectSHA256) {
		return errors.New("live authorization redis reservation subject digest is invalid")
	}
	reservationID = strings.TrimSpace(reservationID)
	if !agentTaskLiveAuthorizationIDPattern.MatchString(reservationID) {
		return errors.New("live authorization redis reservation ID is invalid")
	}
	reservation := agentTaskLiveRedisReservation{
		AuthorizationPayloadSHA256: ledger.authorizationPayloadHash,
		ReservationID:              reservationID,
		InvocationSHA256:           hashAgentTaskLiveAuthorizationSubject(ledger.invocationID),
		EventType:                  record.EventType, SubjectSHA256: record.SubjectSHA256,
		Runs: record.Runs, ProviderCalls: record.ProviderCalls, CapturedOutputs: record.CapturedOutputs,
		EstimatedCostMicros: record.EstimatedCostMicros,
	}
	payload, err := json.Marshal(reservation)
	if err != nil {
		return fmt.Errorf("encode live authorization redis reservation: %w", err)
	}
	digest := sha256.Sum256(payload)
	arguments := append(ledger.identityArguments(),
		reservationID,
		hex.EncodeToString(digest[:]),
		reservation.InvocationSHA256,
		reservation.EventType,
		reservation.SubjectSHA256,
		strconv.Itoa(reservation.Runs),
		strconv.Itoa(reservation.ProviderCalls),
		strconv.Itoa(reservation.CapturedOutputs),
		strconv.FormatInt(reservation.EstimatedCostMicros, 10),
	)
	ctx, cancel := ledger.operationContext(ctx)
	defer cancel()
	if _, err := reserveAgentTaskLiveRedisLedgerScript.Run(
		ctx,
		ledger.client,
		[]string{ledger.stateKey, ledger.eventsKey, ledger.markerKey},
		arguments...,
	).Int(); err != nil {
		return fmt.Errorf("reserve live authorization redis budget: %w", err)
	}
	return nil
}

func (ledger *agentTaskLiveRedisLedger) identityArguments() []any {
	limits := ledger.authorization.Limits
	return []any{
		agentTaskLiveRedisLedgerSchemaVersion,
		ledger.authorizationPayloadHash,
		ledger.keyID,
		strconv.FormatInt(ledger.authorization.IssuedAt.UnixMilli(), 10),
		strconv.FormatInt(ledger.authorization.ExpiresAt.UnixMilli(), 10),
		strconv.Itoa(limits.MaxRuns),
		strconv.Itoa(limits.MaxProviderCalls),
		strconv.Itoa(limits.MaxCapturedOutputs),
		strconv.FormatInt(limits.MaxEstimatedCostMicros, 10),
	}
}

func (ledger *agentTaskLiveRedisLedger) operationContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(ctx, ledger.operationTimeout)
}

func initializeAgentTaskLiveRedisAuthorizationState(
	ctx context.Context,
	authorizationPath string,
	redisConfigPath string,
	key []byte,
	keyID string,
	now time.Time,
) (bool, eval.AgentTaskLiveAuthorizationEvidence, error) {
	authorization, err := loadAndVerifyAgentTaskLiveAuthorizationDocument(authorizationPath, key, keyID, now)
	if err != nil {
		return false, eval.AgentTaskLiveAuthorizationEvidence{}, err
	}
	config, err := loadAgentTaskLiveRedisConfig(redisConfigPath)
	if err != nil {
		return false, eval.AgentTaskLiveAuthorizationEvidence{}, err
	}
	client, err := newAgentTaskLiveRedisClient(config)
	if err != nil {
		return false, eval.AgentTaskLiveAuthorizationEvidence{}, err
	}
	defer client.Close()
	ledger, err := newAgentTaskLiveRedisLedger(client, config, authorization)
	if err != nil {
		return false, eval.AgentTaskLiveAuthorizationEvidence{}, err
	}
	created, err := ledger.Initialize(ctx)
	if err != nil {
		return false, eval.AgentTaskLiveAuthorizationEvidence{}, err
	}
	return created, ledger.Evidence(), nil
}

func openAndReserveAgentTaskLiveRedisAuthorization(
	ctx context.Context,
	authorizationPath string,
	redisConfigPath string,
	key []byte,
	keyID string,
	binding agentTaskLiveAuthorizationBinding,
	capturedOutputs int,
	now time.Time,
) (agentTaskLiveAuthorizationBudget, *redis.Client, error) {
	authorization, err := loadAndVerifyAgentTaskLiveAuthorization(authorizationPath, key, keyID, binding, now)
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
	if err := ledger.ReserveRunContext(ctx, capturedOutputs, now); err != nil {
		_ = client.Close()
		return nil, nil, err
	}
	return ledger, client, nil
}
