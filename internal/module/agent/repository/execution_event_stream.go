package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"

	agentObservability "twitter-clone/internal/module/agent/observability"
)

const (
	defaultExecutionEventStreamMaxLength = int64(2000)
	defaultExecutionEventStreamTTL       = 24 * time.Hour
	maxExecutionEventPayloadBytes        = 64 * 1024
)

type ExecutionEventStreamConfig struct {
	MaxLength int64
	TTL       time.Duration
}

type RedisExecutionEventStore struct {
	client    *redis.Client
	maxLength int64
	ttl       time.Duration
	now       func() time.Time
}

func NewRedisExecutionEventStore(client *redis.Client, config ExecutionEventStreamConfig) *RedisExecutionEventStore {
	if config.MaxLength <= 0 {
		config.MaxLength = defaultExecutionEventStreamMaxLength
	}
	if config.TTL <= 0 {
		config.TTL = defaultExecutionEventStreamTTL
	}
	return &RedisExecutionEventStore{client: client, maxLength: config.MaxLength, ttl: config.TTL, now: time.Now}
}

func (store *RedisExecutionEventStore) RecordRun(ctx context.Context, record agentObservability.RunRecord) error {
	return store.append(ctx, agentObservability.TraceEventRun, record.UserID, record.RunID, record)
}

func (store *RedisExecutionEventStore) RecordStep(ctx context.Context, record agentObservability.StepRecord) error {
	return store.append(ctx, agentObservability.TraceEventStep, record.UserID, record.RunID, record)
}

func (store *RedisExecutionEventStore) RecordLLMCall(ctx context.Context, record agentObservability.LLMCallRecord) error {
	return store.append(ctx, agentObservability.TraceEventLLMCall, record.UserID, record.RunID, record)
}

func (store *RedisExecutionEventStore) RecordToolCall(ctx context.Context, record agentObservability.ToolCallRecord) error {
	return store.append(ctx, agentObservability.TraceEventToolCall, record.UserID, record.RunID, record)
}

func (store *RedisExecutionEventStore) append(
	ctx context.Context,
	kind agentObservability.TraceEventKind,
	userID uint64,
	runID string,
	record interface{},
) error {
	if store == nil || store.client == nil {
		return errors.New("execution event Redis client is not configured")
	}
	if userID == 0 || strings.TrimSpace(runID) == "" {
		return errors.New("trace event identity is incomplete")
	}
	payload, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal trace event: %w", err)
	}
	if len(payload) > maxExecutionEventPayloadBytes {
		return fmt.Errorf("trace event payload exceeds %d bytes", maxExecutionEventPayloadBytes)
	}
	key := executionEventStreamKey(userID, runID)
	pipeline := store.client.TxPipeline()
	pipeline.XAdd(ctx, &redis.XAddArgs{
		Stream: key, MaxLen: store.maxLength, Approx: true,
		Values: map[string]interface{}{
			"version": "1", "kind": string(kind), "payload": string(payload),
			"created_at_ms": store.now().UnixMilli(),
		},
	})
	pipeline.Expire(ctx, key, store.ttl)
	if _, err := pipeline.Exec(ctx); err != nil {
		return fmt.Errorf("append execution trace event: %w", err)
	}
	return nil
}

func (store *RedisExecutionEventStore) ReadTraceEvents(
	ctx context.Context,
	userID uint64,
	runID string,
	afterCursor string,
	limit int,
	block time.Duration,
) (agentObservability.TraceEventBatch, error) {
	if store == nil || store.client == nil {
		return agentObservability.TraceEventBatch{}, errors.New("execution event Redis client is not configured")
	}
	if userID == 0 || strings.TrimSpace(runID) == "" {
		return agentObservability.TraceEventBatch{}, errors.New("trace event identity is incomplete")
	}
	cursor, err := agentObservability.NormalizeTraceEventCursor(afterCursor)
	if err != nil {
		return agentObservability.TraceEventBatch{}, err
	}
	limit = agentObservability.ClampTraceEventBatchSize(limit)
	key := executionEventStreamKey(userID, runID)
	reset, readCursor, err := store.resolveReadCursor(ctx, key, cursor)
	if err != nil {
		return agentObservability.TraceEventBatch{}, err
	}
	readArgs := &redis.XReadArgs{
		Streams: []string{key, readCursor}, Count: int64(limit), Block: -1,
	}
	if block > 0 {
		readArgs.Block = block
	}
	streams, err := store.client.XRead(ctx, readArgs).Result()
	if errors.Is(err, redis.Nil) {
		return agentObservability.TraceEventBatch{Events: []agentObservability.TraceEvent{}, Reset: reset}, nil
	}
	if err != nil {
		return agentObservability.TraceEventBatch{}, fmt.Errorf("read execution trace events: %w", err)
	}
	batch := agentObservability.TraceEventBatch{Events: []agentObservability.TraceEvent{}, Reset: reset}
	for _, stream := range streams {
		for _, message := range stream.Messages {
			event := decodeExecutionTraceEvent(message)
			batch.Events = append(batch.Events, event)
		}
	}
	return batch, nil
}

func (store *RedisExecutionEventStore) resolveReadCursor(ctx context.Context, key, cursor string) (bool, string, error) {
	firstEntries, err := store.client.XRangeN(ctx, key, "-", "+", 1).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return false, "", fmt.Errorf("inspect first execution trace event: %w", err)
	}
	if len(firstEntries) == 0 {
		return cursor != "0-0", "0-0", nil
	}
	lastEntries, err := store.client.XRevRangeN(ctx, key, "+", "-", 1).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return false, "", fmt.Errorf("inspect last execution trace event: %w", err)
	}
	if len(lastEntries) == 0 {
		return cursor != "0-0", "0-0", nil
	}
	firstComparison, err := agentObservability.CompareTraceEventCursor(cursor, firstEntries[0].ID)
	if err != nil {
		return false, "", err
	}
	lastComparison, err := agentObservability.CompareTraceEventCursor(cursor, lastEntries[0].ID)
	if err != nil {
		return false, "", err
	}
	if lastComparison > 0 {
		return true, "0-0", nil
	}
	return firstComparison < 0 && cursor != "0-0", cursor, nil
}

func decodeExecutionTraceEvent(message redis.XMessage) agentObservability.TraceEvent {
	event := agentObservability.TraceEvent{
		Cursor: message.ID, Kind: agentObservability.TraceEventKind(redisValueString(message.Values["kind"])),
		CreatedAt: time.UnixMilli(redisValueInt64(message.Values["created_at_ms"])),
	}
	payload := []byte(redisValueString(message.Values["payload"]))
	var target interface{}
	switch event.Kind {
	case agentObservability.TraceEventRun:
		event.Run = &agentObservability.RunRecord{}
		target = event.Run
	case agentObservability.TraceEventStep:
		event.Step = &agentObservability.StepRecord{}
		target = event.Step
	case agentObservability.TraceEventLLMCall:
		event.LLMCall = &agentObservability.LLMCallRecord{}
		target = event.LLMCall
	case agentObservability.TraceEventToolCall:
		event.ToolCall = &agentObservability.ToolCallRecord{}
		target = event.ToolCall
	default:
		event.Kind = agentObservability.TraceEventControl
		event.Reset = true
		event.Reason = "unsupported_event_kind"
		return event
	}
	if len(payload) == 0 || json.Unmarshal(payload, target) != nil {
		event.Kind = agentObservability.TraceEventControl
		event.Run, event.Step, event.LLMCall, event.ToolCall = nil, nil, nil, nil
		event.Reset = true
		event.Reason = "event_decode_failed"
	}
	return event
}

func executionEventStreamKey(userID uint64, runID string) string {
	sum := sha256.Sum256([]byte(strconv.FormatUint(userID, 10) + ":" + strings.TrimSpace(runID)))
	return "agent:run_events:{" + hex.EncodeToString(sum[:]) + "}"
}

func redisValueString(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []byte:
		return string(typed)
	default:
		return fmt.Sprint(typed)
	}
}

func redisValueInt64(value interface{}) int64 {
	parsed, _ := strconv.ParseInt(redisValueString(value), 10, 64)
	return parsed
}
