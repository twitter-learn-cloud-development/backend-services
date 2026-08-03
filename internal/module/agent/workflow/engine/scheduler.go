package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	agentRuntime "twitter-clone/internal/module/agent/runtime"
	"twitter-clone/internal/module/agent/workflow/dsl"
	"twitter-clone/internal/module/agent/workflow/ir"
)

// WorkflowNode receives one immutable state generation and returns a delta.
// Only the scheduler coordinator may merge that delta into the Blackboard.
type WorkflowNode interface {
	ID() string
	Type() string
	Execute(ctx context.Context, state StateView, inputs map[string]interface{}) (map[string]interface{}, error)
}

type NodeTrace struct {
	NodeID      string `json:"node_id"`
	NodeType    string `json:"node_type"`
	Status      string `json:"status"`
	Attempt     int    `json:"attempt,omitempty"`
	MaxAttempts int    `json:"max_attempts,omitempty"`
	StartedAt   int64  `json:"started_at,omitempty"`
	FinishedAt  int64  `json:"finished_at,omitempty"`
	DurationMs  int64  `json:"duration_ms,omitempty"`
	Error       string `json:"error,omitempty"`
}

// StateCommitHook persists immutable state boundaries without coupling the
// scheduler to a database implementation.
type StateCommitHook func(ctx context.Context, commit StateCommit) error

type SchedulerOption func(*Scheduler)

func WithStateCommitHook(hook StateCommitHook) SchedulerOption {
	return func(scheduler *Scheduler) {
		scheduler.stateCommitHook = hook
	}
}

func WithExecutionBudget(tracker *agentRuntime.BudgetTracker, maxParallelNodes int) SchedulerOption {
	return func(scheduler *Scheduler) {
		scheduler.budgetTracker = tracker
		if maxParallelNodes > 0 {
			scheduler.maxParallelNodes = maxParallelNodes
		}
	}
}

// Scheduler executes deterministic topological waves from a compiled IR.
type Scheduler struct {
	plan             *ir.Plan
	nodesMap         map[string]WorkflowNode
	blackboard       *Blackboard
	traceMu          sync.RWMutex
	traces           map[string]NodeTrace
	stateCommitHook  StateCommitHook
	budgetTracker    *agentRuntime.BudgetTracker
	maxParallelNodes int
}

func NewScheduler(definition *dsl.WorkflowDSL, nodeImpls []WorkflowNode, options ...SchedulerOption) (*Scheduler, error) {
	plan, err := ir.Compile(definition)
	if err != nil {
		return nil, fmt.Errorf("failed to compile DAG: %w", err)
	}
	nodesMap := make(map[string]WorkflowNode, len(nodeImpls))
	for _, implementation := range nodeImpls {
		if implementation == nil {
			return nil, fmt.Errorf("workflow node implementation cannot be nil")
		}
		if _, exists := nodesMap[implementation.ID()]; exists {
			return nil, fmt.Errorf("duplicate executable implementation for node %s", implementation.ID())
		}
		nodesMap[implementation.ID()] = implementation
	}
	for _, node := range plan.Nodes() {
		if _, exists := nodesMap[node.ID]; !exists {
			return nil, fmt.Errorf("node %s (type %s) has no executable implementation provided", node.ID, node.Type)
		}
	}
	scheduler := &Scheduler{
		plan:       plan,
		nodesMap:   nodesMap,
		blackboard: NewBlackboard(),
		traces:     make(map[string]NodeTrace),
	}
	for _, option := range options {
		if option != nil {
			option(scheduler)
		}
	}
	return scheduler, nil
}

func (s *Scheduler) GetBlackboard() *Blackboard {
	return s.blackboard
}

func (s *Scheduler) GetTraces() []NodeTrace {
	s.traceMu.RLock()
	defer s.traceMu.RUnlock()

	nodes := s.plan.Nodes()
	traces := make([]NodeTrace, 0, len(nodes))
	for _, node := range nodes {
		trace, ok := s.traces[node.ID]
		if !ok {
			trace = NodeTrace{NodeID: node.ID, NodeType: node.Type, Status: NodeStatusPending}
		}
		traces = append(traces, trace)
	}
	return traces
}

func (s *Scheduler) GetBudgetSnapshot() agentRuntime.BudgetSnapshot {
	if s == nil || s.budgetTracker == nil {
		return agentRuntime.BudgetSnapshot{}
	}
	return s.budgetTracker.Snapshot()
}

func (s *Scheduler) GetCheckpoint(suspension *SuspensionError) WorkflowCheckpoint {
	checkpoint := WorkflowCheckpoint{
		Blackboard:   s.blackboard.GetSnapshot(),
		StateVersion: s.blackboard.Version(),
		Traces:       s.GetTraces(),
		SuspendedAt:  time.Now().UnixMilli(),
		Budget:       s.GetBudgetSnapshot(),
	}
	if suspension != nil {
		checkpoint.CurrentNodeID = suspension.Suspension.NodeID
		checkpoint.Reason = suspension.Suspension.Reason
		checkpoint.ResumeToken = suspension.Suspension.ResumeToken
		checkpoint.Metadata = cloneFields(suspension.Suspension.Metadata)
		_, checkpoint.RetryCurrentNode = suspension.Suspension.Metadata["approval_request_id"]
	}
	return checkpoint
}

func (s *Scheduler) Execute(ctx context.Context, initialInputs map[string]interface{}) error {
	s.blackboard.ApplyDelta("start", initialInputs)

	currentInDegree := s.plan.InDegrees()
	activeInputs := make(map[string]int, len(currentInDegree))
	readyIDs := make([]string, 0)
	for _, node := range s.plan.Nodes() {
		if currentInDegree[node.ID] == 0 {
			activeInputs[node.ID] = 1
			readyIDs = append(readyIDs, node.ID)
		}
	}
	return s.executePrepared(ctx, currentInDegree, activeInputs, readyIDs)
}

func (s *Scheduler) ExecuteFromCheckpoint(ctx context.Context, checkpoint WorkflowCheckpoint, resumeInputs map[string]interface{}) error {
	s.blackboard.LoadSnapshotAtVersion(checkpoint.Blackboard, checkpoint.StateVersion)
	s.loadTraces(checkpoint.Traces)

	currentInDegree := s.plan.InDegrees()
	activeInputs := make(map[string]int, len(currentInDegree))
	terminal := make(map[string]NodeTrace)
	for _, trace := range checkpoint.Traces {
		switch trace.Status {
		case NodeStatusSuccess, NodeStatusSkipped:
			terminal[trace.NodeID] = trace
		}
	}

	readySet := make(map[string]struct{})
	for _, node := range s.plan.Nodes() {
		trace, ok := terminal[node.ID]
		if !ok {
			continue
		}
		s.replayDownstream(node.ID, trace.Status == NodeStatusSuccess, currentInDegree, activeInputs, readySet)
	}

	if checkpoint.CurrentNodeID != "" && !checkpoint.RetryCurrentNode {
		s.blackboard.ApplyDelta(checkpoint.CurrentNodeID, resumeInputs)
		node, _ := s.plan.Node(checkpoint.CurrentNodeID)
		if err := s.markNodeSuccess(node, time.Now().UnixMilli()); err != nil {
			return err
		}
		terminal[checkpoint.CurrentNodeID] = s.traceForNode(checkpoint.CurrentNodeID)
		s.replayDownstream(checkpoint.CurrentNodeID, true, currentInDegree, activeInputs, readySet)
	} else if checkpoint.CurrentNodeID != "" {
		if _, ready := readySet[checkpoint.CurrentNodeID]; !ready && currentInDegree[checkpoint.CurrentNodeID] == 0 {
			readySet[checkpoint.CurrentNodeID] = struct{}{}
		}
	}

	readyIDs := make([]string, 0, len(readySet))
	for id := range readySet {
		if _, done := terminal[id]; !done {
			readyIDs = append(readyIDs, id)
		}
	}
	s.sortNodeIDs(readyIDs)
	return s.executePrepared(ctx, currentInDegree, activeInputs, readyIDs)
}

type nodeResult struct {
	id         string
	node       ir.Node
	outputs    map[string]interface{}
	err        error
	skipped    bool
	finishedAt int64
}

func (s *Scheduler) executePrepared(ctx context.Context, currentInDegree map[string]int, activeInputs map[string]int, readyIDs []string) error {
	if s.budgetTracker != nil {
		ctx = agentRuntime.ContextWithBudgetTracker(ctx, s.budgetTracker)
		var cancel context.CancelFunc
		ctx, cancel = agentRuntime.WithBudgetContext(ctx, s.budgetTracker.Budget())
		defer cancel()
	}
	readyIDs = uniqueStrings(readyIDs)
	s.sortNodeIDs(readyIDs)

	for len(readyIDs) > 0 {
		versionBeforeWave := s.blackboard.Version()
		results := s.executeWave(ctx, readyIDs, activeInputs)
		s.sortResults(results)

		for index := range results {
			if results[index].err == nil && !results[index].skipped {
				results[index].err = validateStateWrites(results[index])
			}
			if results[index].err == nil && !results[index].skipped {
				results[index].err = s.applyResultDelta(results[index])
			}
			if traceErr := s.recordResult(results[index]); traceErr != nil {
				results[index].err = errors.Join(results[index].err, traceErr)
			}
		}
		if s.stateCommitHook != nil && s.blackboard.Version() > versionBeforeWave {
			if err := s.stateCommitHook(ctx, s.blackboard.Commit()); err != nil {
				return fmt.Errorf("persist workflow state commit: %w", err)
			}
		}

		if waveErr := selectWaveError(ctx, results); waveErr != nil {
			return waveErr
		}

		nextReady := make(map[string]struct{})
		for _, result := range results {
			s.advanceDownstream(result.id, result.outputs, !result.skipped, currentInDegree, activeInputs, nextReady)
		}
		readyIDs = readyIDs[:0]
		for id := range nextReady {
			readyIDs = append(readyIDs, id)
		}
		s.sortNodeIDs(readyIDs)
	}

	for _, trace := range s.GetTraces() {
		if trace.Status != NodeStatusSuccess && trace.Status != NodeStatusSkipped {
			return fmt.Errorf("workflow execution stalled at node %s with status %s", trace.NodeID, trace.Status)
		}
	}
	return nil
}

func (s *Scheduler) executeWave(ctx context.Context, readyIDs []string, activeInputs map[string]int) []nodeResult {
	view := s.blackboard.View()
	results := make(chan nodeResult, len(readyIDs))
	started := 0
	parallelism := s.maxParallelNodes
	if parallelism <= 0 || parallelism > len(readyIDs) {
		parallelism = len(readyIDs)
	}
	var semaphore chan struct{}
	if parallelism > 0 {
		semaphore = make(chan struct{}, parallelism)
	}

	for _, id := range readyIDs {
		node, exists := s.plan.Node(id)
		if !exists {
			results <- nodeResult{id: id, err: fmt.Errorf("compiled node %s is missing", id), finishedAt: time.Now().UnixMilli()}
			started++
			continue
		}
		isSkipped := activeInputs[id] == 0 && s.plan.InDegree(id) > 0
		if isSkipped {
			results <- nodeResult{id: id, node: node, skipped: true, finishedAt: time.Now().UnixMilli()}
			started++
			continue
		}

		inputs := s.resolveInputs(node, view.GetSnapshot())
		started++
		go func(compiledNode ir.Node, resolvedInputs map[string]interface{}) {
			result := nodeResult{id: compiledNode.ID, node: compiledNode}
			defer func() {
				if recovered := recover(); recovered != nil {
					result.err = fmt.Errorf("node panic: %v", recovered)
				}
				result.finishedAt = time.Now().UnixMilli()
				results <- result
			}()
			if semaphore != nil {
				select {
				case semaphore <- struct{}{}:
					defer func() { <-semaphore }()
				case <-ctx.Done():
					result.err = ctx.Err()
					return
				}
			}

			nodeCtx := ctx
			if compiledNode.TimeoutSec > 0 {
				var cancel context.CancelFunc
				nodeCtx, cancel = context.WithTimeout(ctx, time.Duration(compiledNode.TimeoutSec)*time.Second)
				defer cancel()
			}
			result.outputs, result.err = s.executeNode(nodeCtx, compiledNode, view, resolvedInputs)
		}(node, inputs)
	}

	collected := make([]nodeResult, 0, started)
	for len(collected) < started {
		collected = append(collected, <-results)
	}
	return collected
}

func (s *Scheduler) executeNode(ctx context.Context, node ir.Node, view StateView, inputs map[string]interface{}) (map[string]interface{}, error) {
	policy := normalizeNodeRetryPolicy(node.Retry)
	for attempt := 1; attempt <= policy.maxAttempts; attempt++ {
		if err := s.markNodeAttempt(node, attempt, policy.maxAttempts); err != nil {
			return nil, err
		}
		if s.budgetTracker != nil {
			if err := s.budgetTracker.AdmitStep(); err != nil {
				return nil, fmt.Errorf("node %s execution budget: %w", node.ID, err)
			}
		}
		outputs, err := s.nodesMap[node.ID].Execute(ctx, view, inputs)
		if err == nil {
			return outputs, nil
		}
		if contextErr := ctx.Err(); contextErr != nil {
			return nil, contextErr
		}
		if attempt == policy.maxAttempts || !shouldRetryNode(err) {
			return nil, err
		}
		if transitionErr := s.markNodeRetrying(node, err); transitionErr != nil {
			return nil, errors.Join(err, transitionErr)
		}
		if waitErr := waitNodeRetry(ctx, nodeRetryDelay(policy, node.ID, attempt)); waitErr != nil {
			return nil, waitErr
		}
	}
	return nil, errors.New("node retry policy exhausted without a result")
}

func (s *Scheduler) recordResult(result nodeResult) error {
	if result.skipped {
		return s.finishNodeTrace(result.node, NodeStatusSkipped, nil, result.finishedAt)
	}
	if result.err == nil {
		return s.markNodeSuccess(result.node, result.finishedAt)
	}
	var suspension *SuspensionError
	if errors.As(result.err, &suspension) {
		if suspension.Suspension.NodeID == "" {
			suspension.Suspension.NodeID = result.id
		}
		return s.finishNodeTrace(result.node, NodeStatusSuspended, suspension, result.finishedAt)
	}
	if errors.Is(result.err, context.Canceled) {
		return s.finishNodeTrace(result.node, NodeStatusCanceled, result.err, result.finishedAt)
	}
	if errors.Is(result.err, context.DeadlineExceeded) {
		return s.finishNodeTrace(result.node, NodeStatusTimedOut, result.err, result.finishedAt)
	}
	return s.finishNodeTrace(result.node, NodeStatusFailed, result.err, result.finishedAt)
}

func validateStateWrites(result nodeResult) error {
	for _, write := range result.node.Writes {
		if _, exists := result.outputs[write.Source]; !exists {
			return fmt.Errorf("declared state write %s requires missing output field %s", write.Path, write.Source)
		}
	}
	return nil
}

type stateWriteMutation struct {
	namespace string
	field     string
	value     interface{}
}

func (s *Scheduler) applyResultDelta(result nodeResult) error {
	mutations := make([]stateWriteMutation, 0, len(result.node.Writes))
	for _, write := range result.node.Writes {
		pathParts := strings.SplitN(write.Path, ".", 2)
		incoming := result.outputs[write.Source]
		current, exists := s.blackboard.GetValue(pathParts[0], pathParts[1])
		value, err := reduceStateValue(write.Reducer, current, incoming, exists)
		if err != nil {
			return fmt.Errorf("reduce state write %s from node %s: %w", write.Path, result.id, err)
		}
		mutations = append(mutations, stateWriteMutation{namespace: pathParts[0], field: pathParts[1], value: value})
	}

	s.blackboard.ApplyDelta(result.id, result.outputs)
	for _, mutation := range mutations {
		s.blackboard.ApplyDelta(mutation.namespace, map[string]interface{}{mutation.field: mutation.value})
	}
	return nil
}

func reduceStateValue(reducer string, current, incoming interface{}, exists bool) (interface{}, error) {
	switch reducer {
	case "":
		return incoming, nil
	case dsl.ReducerFirst:
		if exists {
			return current, nil
		}
		return incoming, nil
	case dsl.ReducerLast:
		return incoming, nil
	case dsl.ReducerAppend:
		values := make([]interface{}, 0)
		if exists {
			values = appendReducerValues(values, current)
		}
		return appendReducerValues(values, incoming), nil
	case dsl.ReducerMerge:
		merged := make(map[string]interface{})
		if exists {
			currentMap, ok := reducerStringMap(current)
			if !ok {
				return nil, fmt.Errorf("merge reducer requires object state, got %T", current)
			}
			for key, value := range currentMap {
				merged[key] = value
			}
		}
		incomingMap, ok := reducerStringMap(incoming)
		if !ok {
			return nil, fmt.Errorf("merge reducer requires object input, got %T", incoming)
		}
		for key, value := range incomingMap {
			merged[key] = value
		}
		return merged, nil
	case dsl.ReducerSum, dsl.ReducerMin, dsl.ReducerMax:
		incomingNumber, ok := reducerNumber(incoming)
		if !ok {
			return nil, fmt.Errorf("%s reducer requires numeric input, got %T", reducer, incoming)
		}
		if !exists {
			return incomingNumber, nil
		}
		currentNumber, ok := reducerNumber(current)
		if !ok {
			return nil, fmt.Errorf("%s reducer requires numeric state, got %T", reducer, current)
		}
		switch reducer {
		case dsl.ReducerSum:
			return currentNumber + incomingNumber, nil
		case dsl.ReducerMin:
			return math.Min(currentNumber, incomingNumber), nil
		default:
			return math.Max(currentNumber, incomingNumber), nil
		}
	default:
		return nil, fmt.Errorf("unsupported reducer %q", reducer)
	}
}

func appendReducerValues(target []interface{}, value interface{}) []interface{} {
	if value == nil {
		return append(target, nil)
	}
	reflected := reflect.ValueOf(value)
	if reflected.Kind() != reflect.Slice && reflected.Kind() != reflect.Array {
		return append(target, value)
	}
	for index := 0; index < reflected.Len(); index++ {
		target = append(target, reflected.Index(index).Interface())
	}
	return target
}

func reducerStringMap(value interface{}) (map[string]interface{}, bool) {
	if value == nil {
		return nil, false
	}
	reflected := reflect.ValueOf(value)
	if reflected.Kind() != reflect.Map || reflected.Type().Key().Kind() != reflect.String {
		return nil, false
	}
	result := make(map[string]interface{}, reflected.Len())
	iterator := reflected.MapRange()
	for iterator.Next() {
		result[iterator.Key().String()] = iterator.Value().Interface()
	}
	return result, true
}

func reducerNumber(value interface{}) (float64, bool) {
	if value == nil {
		return 0, false
	}
	reflected := reflect.ValueOf(value)
	var number float64
	switch reflected.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		number = float64(reflected.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		number = float64(reflected.Uint())
	case reflect.Float32, reflect.Float64:
		number = reflected.Float()
	default:
		return 0, false
	}
	if math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, false
	}
	return number, true
}

func selectWaveError(ctx context.Context, results []nodeResult) error {
	for _, result := range results {
		if result.err == nil {
			continue
		}
		var suspension *SuspensionError
		if errors.As(result.err, &suspension) {
			return suspension
		}
		if ctx.Err() != nil && errors.Is(result.err, ctx.Err()) {
			continue
		}
		return fmt.Errorf("node [%s] execution error: %w", result.id, result.err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func (s *Scheduler) advanceDownstream(id string, outputs map[string]interface{}, sourceExecuted bool, inDegrees map[string]int, activeInputs map[string]int, readySet map[string]struct{}) {
	activeBranch := ""
	if branchValue, ok := outputs["_branch"]; ok {
		activeBranch, _ = branchValue.(string)
	}
	node, _ := s.plan.Node(id)
	for _, edge := range s.plan.Outgoing(id) {
		edgeActive := sourceExecuted
		if node.Type == "router" && activeBranch != "" && edge.SourceHandle != activeBranch {
			edgeActive = false
		}
		if edgeActive {
			activeInputs[edge.Target]++
		}
		inDegrees[edge.Target]--
		if inDegrees[edge.Target] == 0 {
			readySet[edge.Target] = struct{}{}
		}
	}
}

func (s *Scheduler) replayDownstream(id string, sourceExecuted bool, inDegrees map[string]int, activeInputs map[string]int, readySet map[string]struct{}) {
	snapshot := s.blackboard.GetSnapshot()
	s.advanceDownstream(id, snapshot[id], sourceExecuted, inDegrees, activeInputs, readySet)
}

func (s *Scheduler) resolveInputs(node ir.Node, snapshot map[string]map[string]interface{}) map[string]interface{} {
	return resolveRawInputs(node.Properties, snapshot)
}

func resolveRawInputs(properties json.RawMessage, snapshot map[string]map[string]interface{}) map[string]interface{} {
	inputs := make(map[string]interface{})
	if len(properties) == 0 {
		return inputs
	}
	var rawProperties map[string]interface{}
	if err := json.Unmarshal(properties, &rawProperties); err != nil {
		return inputs
	}
	referencePattern := regexp.MustCompile(`\{\{([a-zA-Z0-9_\-]+)\.([a-zA-Z0-9_\-]+)\}\}`)
	for key, value := range rawProperties {
		inputs[key] = resolveInputValue(value, snapshot, referencePattern)
	}
	return inputs
}

func resolveInputValue(value interface{}, snapshot map[string]map[string]interface{}, referencePattern *regexp.Regexp) interface{} {
	switch typed := value.(type) {
	case string:
		return referencePattern.ReplaceAllStringFunc(typed, func(match string) string {
			parts := referencePattern.FindStringSubmatch(match)
			if len(parts) != 3 {
				return match
			}
			fields, exists := snapshot[parts[1]]
			if !exists {
				return match
			}
			resolved, exists := fields[parts[2]]
			if !exists {
				return match
			}
			return fmt.Sprintf("%v", resolved)
		})
	case map[string]interface{}:
		resolved := make(map[string]interface{}, len(typed))
		for key, child := range typed {
			resolved[key] = resolveInputValue(child, snapshot, referencePattern)
		}
		return resolved
	case []interface{}:
		resolved := make([]interface{}, len(typed))
		for index, child := range typed {
			resolved[index] = resolveInputValue(child, snapshot, referencePattern)
		}
		return resolved
	default:
		return typed
	}
}

func (s *Scheduler) markNodeAttempt(node ir.Node, attempt, maxAttempts int) error {
	now := time.Now().UnixMilli()
	s.traceMu.Lock()
	defer s.traceMu.Unlock()
	trace := s.traces[node.ID]
	if err := validateNodeStateTransition(trace.Status, NodeStatusRunning); err != nil {
		return fmt.Errorf("node %s: %w", node.ID, err)
	}
	if trace.NodeID == "" {
		trace.NodeID = node.ID
		trace.NodeType = node.Type
	}
	if trace.StartedAt == 0 {
		trace.StartedAt = now
	}
	trace.Status = NodeStatusRunning
	trace.Attempt = attempt
	trace.MaxAttempts = maxAttempts
	trace.FinishedAt = 0
	trace.DurationMs = 0
	trace.Error = ""
	s.traces[node.ID] = trace
	return nil
}

func (s *Scheduler) markNodeRetrying(node ir.Node, retryErr error) error {
	s.traceMu.Lock()
	defer s.traceMu.Unlock()
	trace := s.traces[node.ID]
	if err := validateNodeStateTransition(trace.Status, NodeStatusRetrying); err != nil {
		return fmt.Errorf("node %s: %w", node.ID, err)
	}
	trace.Status = NodeStatusRetrying
	trace.Error = retryErr.Error()
	s.traces[node.ID] = trace
	return nil
}

func (s *Scheduler) markNodeSuccess(node ir.Node, finishedAt int64) error {
	return s.finishNodeTrace(node, NodeStatusSuccess, nil, finishedAt)
}

func (s *Scheduler) finishNodeTrace(node ir.Node, status string, err error, finishedAt int64) error {
	if node.ID == "" {
		return errors.New("cannot transition trace for empty node id")
	}
	if finishedAt == 0 {
		finishedAt = time.Now().UnixMilli()
	}
	s.traceMu.Lock()
	defer s.traceMu.Unlock()
	trace := s.traces[node.ID]
	if transitionErr := validateNodeStateTransition(trace.Status, status); transitionErr != nil {
		return fmt.Errorf("node %s: %w", node.ID, transitionErr)
	}
	if trace.NodeID == "" {
		trace.NodeID = node.ID
		trace.NodeType = node.Type
	}
	if trace.StartedAt == 0 && status != NodeStatusSkipped {
		trace.StartedAt = finishedAt
	}
	trace.Status = status
	trace.FinishedAt = finishedAt
	if trace.StartedAt > 0 {
		trace.DurationMs = finishedAt - trace.StartedAt
	}
	if err != nil {
		trace.Error = err.Error()
	} else {
		trace.Error = ""
	}
	s.traces[node.ID] = trace
	return nil
}

func (s *Scheduler) loadTraces(traces []NodeTrace) {
	s.traceMu.Lock()
	defer s.traceMu.Unlock()
	s.traces = make(map[string]NodeTrace, len(traces))
	for _, trace := range traces {
		if trace.NodeID != "" {
			s.traces[trace.NodeID] = trace
		}
	}
}

func (s *Scheduler) traceForNode(id string) NodeTrace {
	s.traceMu.RLock()
	defer s.traceMu.RUnlock()
	return s.traces[id]
}

func (s *Scheduler) sortNodeIDs(ids []string) {
	sort.SliceStable(ids, func(i, j int) bool {
		left := s.plan.NodeOrder(ids[i])
		right := s.plan.NodeOrder(ids[j])
		if left != right {
			return left < right
		}
		return ids[i] < ids[j]
	})
}

func (s *Scheduler) sortResults(results []nodeResult) {
	sort.SliceStable(results, func(i, j int) bool {
		left := s.plan.NodeOrder(results[i].id)
		right := s.plan.NodeOrder(results[j].id)
		if left != right {
			return left < right
		}
		return results[i].id < results[j].id
	})
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
