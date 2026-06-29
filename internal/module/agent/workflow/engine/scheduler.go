package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	"twitter-clone/internal/module/agent/workflow/dsl"
)

// WorkflowNode defines an executable node in the workflow scheduler.
type WorkflowNode interface {
	ID() string
	Type() string
	Execute(ctx context.Context, blackboard *Blackboard, inputs map[string]interface{}) (map[string]interface{}, error)
}

const (
	NodeStatusPending = "pending"
	NodeStatusRunning = "running"
	NodeStatusSuccess = "success"
	NodeStatusFailed  = "failed"
	NodeStatusSkipped = "skipped"
)

type NodeTrace struct {
	NodeID     string `json:"node_id"`
	NodeType   string `json:"node_type"`
	Status     string `json:"status"`
	StartedAt  int64  `json:"started_at,omitempty"`
	FinishedAt int64  `json:"finished_at,omitempty"`
	DurationMs int64  `json:"duration_ms,omitempty"`
	Error      string `json:"error,omitempty"`
}

// Scheduler compiles a workflow DSL and executes its DAG concurrently.
type Scheduler struct {
	dslObj     *dsl.WorkflowDSL
	nodesMap   map[string]WorkflowNode
	nodeDSLMap map[string]*dsl.NodeDSL
	adjList    map[string][]dsl.EdgeDSL
	inDegree   map[string]int
	blackboard *Blackboard
	traceMu    sync.RWMutex
	traces     map[string]NodeTrace
}

func NewScheduler(dslObj *dsl.WorkflowDSL, nodeImpls []WorkflowNode) (*Scheduler, error) {
	nodesMap := make(map[string]WorkflowNode)
	for _, n := range nodeImpls {
		nodesMap[n.ID()] = n
	}

	for _, nodeDSL := range dslObj.Nodes {
		if _, exists := nodesMap[nodeDSL.ID]; !exists {
			return nil, fmt.Errorf("node %s (type %s) has no executable implementation provided", nodeDSL.ID, nodeDSL.Type)
		}
	}

	s := &Scheduler{
		dslObj:     dslObj,
		nodesMap:   nodesMap,
		nodeDSLMap: make(map[string]*dsl.NodeDSL),
		adjList:    make(map[string][]dsl.EdgeDSL),
		inDegree:   make(map[string]int),
		blackboard: NewBlackboard(),
		traces:     make(map[string]NodeTrace),
	}

	if err := s.compile(); err != nil {
		return nil, fmt.Errorf("failed to compile DAG: %w", err)
	}
	return s, nil
}

func (s *Scheduler) GetBlackboard() *Blackboard {
	return s.blackboard
}

func (s *Scheduler) GetTraces() []NodeTrace {
	s.traceMu.RLock()
	defer s.traceMu.RUnlock()

	traces := make([]NodeTrace, 0, len(s.dslObj.Nodes))
	seen := make(map[string]struct{}, len(s.traces))
	for _, nodeDSL := range s.dslObj.Nodes {
		trace, ok := s.traces[nodeDSL.ID]
		if !ok {
			trace = NodeTrace{
				NodeID:   nodeDSL.ID,
				NodeType: nodeDSL.Type,
				Status:   NodeStatusPending,
			}
		}
		traces = append(traces, trace)
		seen[nodeDSL.ID] = struct{}{}
	}
	for id, trace := range s.traces {
		if _, ok := seen[id]; !ok {
			traces = append(traces, trace)
		}
	}
	return traces
}

func (s *Scheduler) GetCheckpoint(suspension *SuspensionError) WorkflowCheckpoint {
	checkpoint := WorkflowCheckpoint{
		Blackboard:  s.blackboard.GetSnapshot(),
		Traces:      s.GetTraces(),
		SuspendedAt: time.Now().UnixMilli(),
	}
	if suspension != nil {
		checkpoint.CurrentNodeID = suspension.Suspension.NodeID
		checkpoint.Reason = suspension.Suspension.Reason
		checkpoint.ResumeToken = suspension.Suspension.ResumeToken
	}
	return checkpoint
}

func (s *Scheduler) compile() error {
	if len(s.dslObj.Nodes) == 0 {
		return fmt.Errorf("workflow must contain at least one node")
	}

	for i := range s.dslObj.Nodes {
		n := &s.dslObj.Nodes[i]
		if n.ID == "" {
			return fmt.Errorf("workflow node id cannot be empty")
		}
		if _, exists := s.nodeDSLMap[n.ID]; exists {
			return fmt.Errorf("duplicate workflow node id %s", n.ID)
		}
		if _, exists := s.nodesMap[n.ID]; !exists {
			return fmt.Errorf("node %s (type %s) has no executable implementation provided", n.ID, n.Type)
		}
		s.nodeDSLMap[n.ID] = n
		s.inDegree[n.ID] = 0
	}

	for _, edge := range s.dslObj.Edges {
		if edge.ID == "" {
			return fmt.Errorf("workflow edge id cannot be empty")
		}
		if _, exists := s.nodeDSLMap[edge.Source]; !exists {
			return fmt.Errorf("edge %s references unknown source node %s", edge.ID, edge.Source)
		}
		if _, exists := s.nodeDSLMap[edge.Target]; !exists {
			return fmt.Errorf("edge %s references unknown target node %s", edge.ID, edge.Target)
		}
		s.adjList[edge.Source] = append(s.adjList[edge.Source], edge)
		s.inDegree[edge.Target]++
	}

	inDegreeCopy := make(map[string]int)
	for k, v := range s.inDegree {
		inDegreeCopy[k] = v
	}

	var queue []string
	for id, deg := range inDegreeCopy {
		if deg == 0 {
			queue = append(queue, id)
		}
	}

	visitedCount := 0
	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		visitedCount++

		for _, edge := range s.adjList[curr] {
			inDegreeCopy[edge.Target]--
			if inDegreeCopy[edge.Target] == 0 {
				queue = append(queue, edge.Target)
			}
		}
	}

	if visitedCount != len(s.dslObj.Nodes) {
		return fmt.Errorf("cycle detected in workflow definition DAG")
	}
	return nil
}

func (s *Scheduler) Execute(ctx context.Context, initialInputs map[string]interface{}) error {
	s.blackboard.ApplyDelta("start", initialInputs)

	currentInDegree := make(map[string]int)
	activeInputs := make(map[string]int)
	readyIDs := make([]string, 0)
	for id, deg := range s.inDegree {
		currentInDegree[id] = deg
		if deg == 0 {
			activeInputs[id] = 1
			readyIDs = append(readyIDs, id)
		}
	}

	return s.executePrepared(ctx, currentInDegree, activeInputs, readyIDs, 0)
}

func (s *Scheduler) ExecuteFromCheckpoint(ctx context.Context, checkpoint WorkflowCheckpoint, resumeInputs map[string]interface{}) error {
	s.blackboard.LoadSnapshot(checkpoint.Blackboard)
	s.loadTraces(checkpoint.Traces)

	currentInDegree := make(map[string]int, len(s.inDegree))
	activeInputs := make(map[string]int, len(s.inDegree))
	for id, deg := range s.inDegree {
		currentInDegree[id] = deg
	}

	terminal := make(map[string]NodeTrace)
	for _, trace := range checkpoint.Traces {
		switch trace.Status {
		case NodeStatusSuccess, NodeStatusSkipped:
			terminal[trace.NodeID] = trace
		}
	}

	readySet := make(map[string]struct{})
	for _, nodeDSL := range s.dslObj.Nodes {
		trace, ok := terminal[nodeDSL.ID]
		if !ok {
			continue
		}
		s.replayDownstream(nodeDSL.ID, trace.Status == NodeStatusSuccess, currentInDegree, activeInputs, readySet)
	}

	if checkpoint.CurrentNodeID != "" {
		s.blackboard.ApplyDelta(checkpoint.CurrentNodeID, resumeInputs)
		s.markNodeSuccess(s.getNodeDSL(checkpoint.CurrentNodeID))
		terminal[checkpoint.CurrentNodeID] = s.traceForNode(checkpoint.CurrentNodeID)
		s.replayDownstream(checkpoint.CurrentNodeID, true, currentInDegree, activeInputs, readySet)
	}

	readyIDs := make([]string, 0, len(readySet))
	for id := range readySet {
		if _, done := terminal[id]; !done {
			readyIDs = append(readyIDs, id)
		}
	}

	return s.executePrepared(ctx, currentInDegree, activeInputs, readyIDs, int32(len(terminal)))
}

func (s *Scheduler) executePrepared(ctx context.Context, currentInDegree map[string]int, activeInputs map[string]int, readyIDs []string, completedInitial int32) error {
	var inDegreeMu sync.Mutex
	readyQueue := make(chan string, len(s.dslObj.Nodes))
	errChan := make(chan error, len(s.dslObj.Nodes))
	doneChan := make(chan struct{})
	completedCount := completedInitial
	var doneOnce sync.Once

	for _, id := range readyIDs {
		readyQueue <- id
	}
	if completedCount == int32(len(s.dslObj.Nodes)) {
		close(doneChan)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case nodeID, ok := <-readyQueue:
				if !ok {
					return
				}
				go func(id string) {
					defer func() {
						if atomic.AddInt32(&completedCount, 1) == int32(len(s.dslObj.Nodes)) {
							doneOnce.Do(func() {
								close(doneChan)
							})
						}
					}()

					nodeDSL := s.getNodeDSL(id)
					inDegreeMu.Lock()
					isSkipped := activeInputs[id] == 0 && s.inDegree[id] > 0
					inDegreeMu.Unlock()

					if isSkipped {
						s.markNodeSkipped(nodeDSL)
						s.pushDownstream(id, nil, false, &inDegreeMu, currentInDegree, activeInputs, readyQueue)
						return
					}

					s.markNodeRunning(nodeDSL)
					nodeCtx := ctx
					if nodeDSL.TimeoutSec > 0 {
						var nodeCancel context.CancelFunc
						nodeCtx, nodeCancel = context.WithTimeout(ctx, time.Duration(nodeDSL.TimeoutSec)*time.Second)
						defer nodeCancel()
					}

					inputs := s.resolveInputs(nodeDSL)
					outputs, err := s.nodesMap[id].Execute(nodeCtx, s.blackboard, inputs)
					if err != nil {
						var suspension *SuspensionError
						if errors.As(err, &suspension) {
							if suspension.Suspension.NodeID == "" {
								suspension.Suspension.NodeID = id
							}
							s.markNodeSuspended(nodeDSL, suspension)
							errChan <- suspension
							cancel()
							return
						}
						s.markNodeFailed(nodeDSL, err)
						errChan <- fmt.Errorf("node [%s] execution error: %w", id, err)
						cancel()
						return
					}

					s.blackboard.ApplyDelta(id, outputs)
					s.markNodeSuccess(nodeDSL)
					s.pushDownstream(id, outputs, true, &inDegreeMu, currentInDegree, activeInputs, readyQueue)
				}(nodeID)
			}
		}
	}()

	select {
	case <-ctx.Done():
		select {
		case err := <-errChan:
			return err
		default:
		}
		return ctx.Err()
	case <-doneChan:
	}

	close(readyQueue)

	select {
	case err := <-errChan:
		return err
	default:
		return nil
	}
}

func (s *Scheduler) pushDownstream(id string, outputs map[string]interface{}, sourceExecuted bool, mu *sync.Mutex, inDegrees map[string]int, activeInputs map[string]int, readyQueue chan string) {
	mu.Lock()
	defer mu.Unlock()

	activeBranch := ""
	if branchVal, ok := outputs["_branch"]; ok {
		if branchStr, isStr := branchVal.(string); isStr {
			activeBranch = branchStr
		}
	}

	nodeDSL := s.getNodeDSL(id)
	isRouter := nodeDSL != nil && nodeDSL.Type == "router"

	for _, edge := range s.adjList[id] {
		edgeActive := sourceExecuted
		if isRouter && activeBranch != "" && edge.SourceHandle != activeBranch {
			edgeActive = false
		}
		if edgeActive {
			activeInputs[edge.Target]++
		}

		inDegrees[edge.Target]--
		if inDegrees[edge.Target] == 0 {
			readyQueue <- edge.Target
		}
	}
}

func (s *Scheduler) replayDownstream(id string, sourceExecuted bool, inDegrees map[string]int, activeInputs map[string]int, readySet map[string]struct{}) {
	outputs := map[string]interface{}(nil)
	if snapshot := s.blackboard.GetSnapshot(); snapshot != nil {
		outputs = snapshot[id]
	}

	activeBranch := ""
	if branchVal, ok := outputs["_branch"]; ok {
		if branchStr, isStr := branchVal.(string); isStr {
			activeBranch = branchStr
		}
	}

	nodeDSL := s.getNodeDSL(id)
	isRouter := nodeDSL != nil && nodeDSL.Type == "router"

	for _, edge := range s.adjList[id] {
		edgeActive := sourceExecuted
		if isRouter && activeBranch != "" && edge.SourceHandle != activeBranch {
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

func (s *Scheduler) resolveInputs(nodeDSL *dsl.NodeDSL) map[string]interface{} {
	inputs := make(map[string]interface{})
	if nodeDSL == nil || len(nodeDSL.Properties) == 0 {
		return inputs
	}

	var rawProps map[string]interface{}
	if err := json.Unmarshal(nodeDSL.Properties, &rawProps); err != nil {
		return inputs
	}

	snapshot := s.blackboard.GetSnapshot()
	re := regexp.MustCompile(`\{\{([a-zA-Z0-9_\-]+)\.([a-zA-Z0-9_\-]+)\}\}`)

	for k, v := range rawProps {
		if strVal, ok := v.(string); ok {
			resolvedStr := re.ReplaceAllStringFunc(strVal, func(match string) string {
				submatches := re.FindStringSubmatch(match)
				if len(submatches) == 3 {
					srcNode := submatches[1]
					srcField := submatches[2]
					if nodeFields, exists := snapshot[srcNode]; exists {
						if val, ok := nodeFields[srcField]; ok {
							return fmt.Sprintf("%v", val)
						}
					}
				}
				return match
			})
			inputs[k] = resolvedStr
		} else {
			inputs[k] = v
		}
	}

	return inputs
}

func (s *Scheduler) markNodeRunning(nodeDSL *dsl.NodeDSL) {
	if nodeDSL == nil {
		return
	}
	now := time.Now().UnixMilli()
	s.traceMu.Lock()
	defer s.traceMu.Unlock()
	s.traces[nodeDSL.ID] = NodeTrace{
		NodeID:    nodeDSL.ID,
		NodeType:  nodeDSL.Type,
		Status:    NodeStatusRunning,
		StartedAt: now,
	}
}

func (s *Scheduler) markNodeSuccess(nodeDSL *dsl.NodeDSL) {
	s.finishNodeTrace(nodeDSL, NodeStatusSuccess, nil)
}

func (s *Scheduler) markNodeFailed(nodeDSL *dsl.NodeDSL, err error) {
	s.finishNodeTrace(nodeDSL, NodeStatusFailed, err)
}

func (s *Scheduler) markNodeSkipped(nodeDSL *dsl.NodeDSL) {
	s.finishNodeTrace(nodeDSL, NodeStatusSkipped, nil)
}

func (s *Scheduler) markNodeSuspended(nodeDSL *dsl.NodeDSL, err error) {
	s.finishNodeTrace(nodeDSL, NodeStatusSuspended, err)
}

func (s *Scheduler) finishNodeTrace(nodeDSL *dsl.NodeDSL, status string, err error) {
	if nodeDSL == nil {
		return
	}
	now := time.Now().UnixMilli()
	s.traceMu.Lock()
	defer s.traceMu.Unlock()

	trace := s.traces[nodeDSL.ID]
	if trace.NodeID == "" {
		trace.NodeID = nodeDSL.ID
		trace.NodeType = nodeDSL.Type
	}
	if trace.StartedAt == 0 && status != NodeStatusSkipped {
		trace.StartedAt = now
	}
	trace.Status = status
	trace.FinishedAt = now
	if trace.StartedAt > 0 {
		trace.DurationMs = now - trace.StartedAt
	}
	if err != nil {
		trace.Error = err.Error()
	} else {
		trace.Error = ""
	}
	s.traces[nodeDSL.ID] = trace
}

func (s *Scheduler) loadTraces(traces []NodeTrace) {
	s.traceMu.Lock()
	defer s.traceMu.Unlock()
	s.traces = make(map[string]NodeTrace, len(traces))
	for _, trace := range traces {
		if trace.NodeID == "" {
			continue
		}
		s.traces[trace.NodeID] = trace
	}
}

func (s *Scheduler) traceForNode(id string) NodeTrace {
	s.traceMu.RLock()
	defer s.traceMu.RUnlock()
	return s.traces[id]
}

func (s *Scheduler) getNodeDSL(id string) *dsl.NodeDSL {
	if nodeDSL, ok := s.nodeDSLMap[id]; ok {
		return nodeDSL
	}
	return nil
}
