package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"twitter-clone/internal/module/agent/repository"
	"twitter-clone/internal/module/agent/workflow/engine"
)

const (
	defaultWorkflowBlackboardPageSize = 25
	maxWorkflowBlackboardPageSize     = 100
	maxWorkflowBlackboardQueryBytes   = 128
	maxWorkflowBlackboardPathBytes    = 256
	maxWorkflowBlackboardCursorBytes  = 1024
	maxWorkflowBlackboardEntries      = 10_000
	maxWorkflowBlackboardEvents       = 10_000
	maxWorkflowBlackboardValueBytes   = 16 << 10
)

var ErrWorkflowBlackboardInvalidQuery = errors.New("invalid workflow blackboard query")

type WorkflowBlackboardSearchRequest struct {
	StateVersion int64
	Query        string
	PathPrefix   string
	AfterCursor  string
	PageSize     int
}

type WorkflowBlackboardEntry struct {
	Path        string
	ValueJSON   string
	ValueType   string
	ValueHash   string
	ValueLength int64
	Truncated   bool
}

type WorkflowBlackboardSearchResult struct {
	RunID               string
	StateVersion        int64
	BaseSnapshotVersion int64
	BaseSnapshotHash    string
	StateHash           string
	Verified            bool
	Entries             []WorkflowBlackboardEntry
	MatchedTotal        int64
	NextCursor          string
	HasMore             bool
}

type workflowBlackboardCursor struct {
	StateVersion int64  `json:"v"`
	Path         string `json:"p"`
	FilterHash   string `json:"f"`
}

type materializedWorkflowBlackboard struct {
	State               map[string]map[string]interface{}
	StateVersion        int64
	BaseSnapshotVersion int64
	BaseSnapshotHash    string
	StateHash           string
}

// SearchWorkflowBlackboard materializes a verified historical Blackboard and
// returns a stable, tenant-scoped page of redacted state entries. It never
// invokes the scheduler, an LLM, or a tool.
func (s *AgentService) SearchWorkflowBlackboard(
	ctx context.Context,
	userID uint64,
	runID string,
	request WorkflowBlackboardSearchRequest,
) (*WorkflowBlackboardSearchResult, error) {
	if s.repo == nil {
		return nil, errors.New("agent repository is not initialized")
	}
	runOID, err := primitive.ObjectIDFromHex(strings.TrimSpace(runID))
	if err != nil {
		return nil, fmt.Errorf("%w: invalid run_id", ErrWorkflowBlackboardInvalidQuery)
	}
	run, err := s.repo.GetWorkflowRun(ctx, runOID, userID)
	if err != nil {
		return nil, err
	}
	if run.StateVersion < 0 {
		return nil, errors.New("workflow run has negative state version")
	}

	request.Query = strings.TrimSpace(request.Query)
	request.PathPrefix = strings.TrimSpace(request.PathPrefix)
	if len(request.Query) > maxWorkflowBlackboardQueryBytes {
		return nil, fmt.Errorf("%w: query exceeds %d bytes", ErrWorkflowBlackboardInvalidQuery, maxWorkflowBlackboardQueryBytes)
	}
	if len(request.PathPrefix) > maxWorkflowBlackboardPathBytes {
		return nil, fmt.Errorf("%w: path_prefix exceeds %d bytes", ErrWorkflowBlackboardInvalidQuery, maxWorkflowBlackboardPathBytes)
	}
	if request.StateVersion < 0 {
		return nil, fmt.Errorf("%w: state_version cannot be negative", ErrWorkflowBlackboardInvalidQuery)
	}
	if request.PageSize < 1 {
		request.PageSize = defaultWorkflowBlackboardPageSize
	}
	if request.PageSize > maxWorkflowBlackboardPageSize {
		return nil, fmt.Errorf("%w: page_size exceeds %d", ErrWorkflowBlackboardInvalidQuery, maxWorkflowBlackboardPageSize)
	}

	filterHash := workflowBlackboardFilterHash(request.Query, request.PathPrefix)
	afterPath := ""
	targetVersion := request.StateVersion
	if request.AfterCursor != "" {
		cursor, err := decodeWorkflowBlackboardCursor(request.AfterCursor)
		if err != nil {
			return nil, err
		}
		if cursor.FilterHash != filterHash {
			return nil, fmt.Errorf("%w: cursor does not match current filters", ErrWorkflowBlackboardInvalidQuery)
		}
		if targetVersion > 0 && targetVersion != cursor.StateVersion {
			return nil, fmt.Errorf("%w: cursor state version mismatch", ErrWorkflowBlackboardInvalidQuery)
		}
		targetVersion = cursor.StateVersion
		afterPath = cursor.Path
	}
	if targetVersion == 0 {
		targetVersion = run.StateVersion
	}
	if targetVersion < 0 || targetVersion > run.StateVersion {
		return nil, fmt.Errorf("%w: state_version %d is outside run range 0..%d", ErrWorkflowBlackboardInvalidQuery, targetVersion, run.StateVersion)
	}

	materialized, err := s.materializeWorkflowBlackboard(ctx, run, targetVersion)
	if err != nil {
		return nil, err
	}
	entries, err := searchableWorkflowBlackboardEntries(materialized.State, request.Query, request.PathPrefix)
	if err != nil {
		return nil, err
	}
	start := sort.Search(len(entries), func(index int) bool {
		return entries[index].Path > afterPath
	})
	end := start + request.PageSize
	if end > len(entries) {
		end = len(entries)
	}
	page := append([]WorkflowBlackboardEntry(nil), entries[start:end]...)
	hasMore := end < len(entries)
	nextCursor := ""
	if hasMore && len(page) > 0 {
		nextCursor, err = encodeWorkflowBlackboardCursor(workflowBlackboardCursor{
			StateVersion: targetVersion,
			Path:         page[len(page)-1].Path,
			FilterHash:   filterHash,
		})
		if err != nil {
			return nil, err
		}
	}
	return &WorkflowBlackboardSearchResult{
		RunID: run.ID.Hex(), StateVersion: materialized.StateVersion,
		BaseSnapshotVersion: materialized.BaseSnapshotVersion,
		BaseSnapshotHash:    materialized.BaseSnapshotHash, StateHash: materialized.StateHash,
		Verified: true, Entries: page, MatchedTotal: int64(len(entries)),
		NextCursor: nextCursor, HasMore: hasMore,
	}, nil
}

func (s *AgentService) materializeWorkflowBlackboard(
	ctx context.Context,
	run *repository.WorkflowRunRecord,
	targetVersion int64,
) (*materializedWorkflowBlackboard, error) {
	blackboard := engine.NewBlackboard()
	baseVersion := int64(0)
	baseHash := ""
	if targetVersion > 0 {
		snapshotRepo, ok := s.repo.(repository.WorkflowStateSnapshotRepository)
		if !ok {
			return nil, errors.New("workflow state snapshot repository is not available")
		}
		snapshot, err := snapshotRepo.GetLatestWorkflowStateSnapshot(ctx, run.ID, run.UserID, targetVersion)
		if err != nil {
			return nil, err
		}
		if snapshot != nil {
			state, err := decodeWorkflowStateSnapshot(snapshot)
			if err != nil {
				return nil, err
			}
			blackboard.LoadSnapshotAtVersion(state, uint64(snapshot.StateVersion))
			baseVersion = snapshot.StateVersion
			baseHash = snapshot.SnapshotHash
		}
	}

	if targetVersion > baseVersion {
		rangeRepo, ok := s.repo.(repository.WorkflowStateRangeRepository)
		if !ok {
			return nil, errors.New("workflow state range repository is not available")
		}
		records, err := rangeRepo.ListWorkflowStateEventsRange(
			ctx, run.ID, run.UserID, baseVersion, targetVersion, maxWorkflowBlackboardEvents+1,
		)
		if err != nil {
			return nil, err
		}
		if len(records) > maxWorkflowBlackboardEvents {
			return nil, fmt.Errorf("workflow blackboard materialization exceeds maximum event count %d", maxWorkflowBlackboardEvents)
		}
		events := make([]engine.StateEvent, 0, len(records))
		expected := baseVersion + 1
		for _, record := range records {
			if record == nil {
				return nil, errors.New("workflow state event range contains a nil record")
			}
			if record.Sequence != expected {
				return nil, fmt.Errorf("workflow state event range is not contiguous: expected=%d actual=%d", expected, record.Sequence)
			}
			decoded, err := decodeWorkflowStateEvent(run, record)
			if err != nil {
				return nil, err
			}
			events = append(events, decoded)
			expected++
		}
		if expected-1 != targetVersion {
			return nil, fmt.Errorf("workflow state event range incomplete: expected=%d actual=%d", targetVersion, expected-1)
		}
		if err := blackboard.Replay(events); err != nil {
			return nil, err
		}
	}
	if blackboard.Version() != uint64(targetVersion) {
		return nil, fmt.Errorf("workflow blackboard version mismatch: expected=%d actual=%d", targetVersion, blackboard.Version())
	}
	state := blackboard.GetSnapshot()
	_, stateHash, err := workflowStateDigest(state)
	if err != nil {
		return nil, err
	}
	return &materializedWorkflowBlackboard{
		State: state, StateVersion: targetVersion,
		BaseSnapshotVersion: baseVersion, BaseSnapshotHash: baseHash, StateHash: stateHash,
	}, nil
}

func searchableWorkflowBlackboardEntries(
	state map[string]map[string]interface{},
	query string,
	pathPrefix string,
) ([]WorkflowBlackboardEntry, error) {
	query = strings.ToLower(query)
	entries := make([]WorkflowBlackboardEntry, 0)
	seen := 0
	for namespace, fields := range state {
		for field, value := range fields {
			seen++
			if seen > maxWorkflowBlackboardEntries {
				return nil, fmt.Errorf("workflow blackboard exceeds maximum field count %d", maxWorkflowBlackboardEntries)
			}
			path := namespace + "." + field
			if pathPrefix != "" && !strings.HasPrefix(path, pathPrefix) {
				continue
			}
			originalJSON, err := json.Marshal(value)
			if err != nil {
				return nil, fmt.Errorf("encode workflow blackboard value %s: %w", path, err)
			}
			redactedValue := redactWorkflowBlackboardValue(value)
			if isSensitiveWorkflowBlackboardKey(field) {
				redactedValue = "[REDACTED]"
			}
			redactedJSON, err := json.Marshal(redactedValue)
			if err != nil {
				return nil, fmt.Errorf("encode redacted workflow blackboard value %s: %w", path, err)
			}
			searchableValue := redactedJSON
			if len(searchableValue) > maxWorkflowBlackboardValueBytes {
				searchableValue = searchableValue[:maxWorkflowBlackboardValueBytes]
			}
			if query != "" && !strings.Contains(strings.ToLower(path+"\n"+string(searchableValue)), query) {
				continue
			}
			valueHash := sha256.Sum256(originalJSON)
			entry := WorkflowBlackboardEntry{
				Path: path, ValueType: workflowBlackboardValueType(value),
				ValueHash: hex.EncodeToString(valueHash[:]), ValueLength: int64(len(originalJSON)),
				Truncated: len(redactedJSON) > maxWorkflowBlackboardValueBytes,
			}
			if !entry.Truncated {
				entry.ValueJSON = string(redactedJSON)
			}
			entries = append(entries, entry)
		}
	}
	sort.Slice(entries, func(left, right int) bool { return entries[left].Path < entries[right].Path })
	return entries, nil
}

func redactWorkflowBlackboardValue(value interface{}) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		redacted := make(map[string]interface{}, len(typed))
		for key, child := range typed {
			if isSensitiveWorkflowBlackboardKey(key) {
				redacted[key] = "[REDACTED]"
				continue
			}
			redacted[key] = redactWorkflowBlackboardValue(child)
		}
		return redacted
	case []interface{}:
		redacted := make([]interface{}, len(typed))
		for index, child := range typed {
			redacted[index] = redactWorkflowBlackboardValue(child)
		}
		return redacted
	default:
		return value
	}
}

func isSensitiveWorkflowBlackboardKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(key), "-", "_"))
	switch normalized {
	case "api_key", "apikey", "authorization", "cookie", "password", "secret", "token",
		"access_token", "refresh_token", "resume_token", "resume_token_hash", "client_secret":
		return true
	}
	return strings.HasSuffix(normalized, "_api_key") ||
		strings.HasSuffix(normalized, "_password") ||
		strings.HasSuffix(normalized, "_secret") ||
		strings.HasSuffix(normalized, "_access_token") ||
		strings.HasSuffix(normalized, "_refresh_token") ||
		strings.HasSuffix(normalized, "_resume_token")
}

func workflowBlackboardValueType(value interface{}) string {
	switch value.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case float32, float64, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, json.Number:
		return "number"
	case []interface{}:
		return "array"
	case map[string]interface{}:
		return "object"
	default:
		return "unknown"
	}
}

func workflowBlackboardFilterHash(query, pathPrefix string) string {
	hash := sha256.Sum256([]byte(strings.ToLower(query) + "\x00" + pathPrefix))
	return hex.EncodeToString(hash[:])
}

func encodeWorkflowBlackboardCursor(cursor workflowBlackboardCursor) (string, error) {
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", fmt.Errorf("encode workflow blackboard cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeWorkflowBlackboardCursor(raw string) (workflowBlackboardCursor, error) {
	if len(raw) > maxWorkflowBlackboardCursorBytes {
		return workflowBlackboardCursor{}, fmt.Errorf("%w: cursor exceeds %d bytes", ErrWorkflowBlackboardInvalidQuery, maxWorkflowBlackboardCursorBytes)
	}
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return workflowBlackboardCursor{}, fmt.Errorf("%w: malformed cursor", ErrWorkflowBlackboardInvalidQuery)
	}
	var cursor workflowBlackboardCursor
	if err := json.Unmarshal(payload, &cursor); err != nil || cursor.StateVersion < 0 || cursor.Path == "" || cursor.FilterHash == "" {
		return workflowBlackboardCursor{}, fmt.Errorf("%w: malformed cursor", ErrWorkflowBlackboardInvalidQuery)
	}
	return cursor, nil
}
