package ir

import (
	"container/heap"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"twitter-clone/internal/module/agent/workflow/dsl"
)

var referencePattern = regexp.MustCompile(`\{\{([a-zA-Z0-9_\-]+)\.([a-zA-Z0-9_\-]+)\}\}`)

var supportedReducers = map[string]struct{}{
	dsl.ReducerAppend: {},
	dsl.ReducerSum:    {},
	dsl.ReducerMin:    {},
	dsl.ReducerMax:    {},
	dsl.ReducerMerge:  {},
	dsl.ReducerFirst:  {},
	dsl.ReducerLast:   {},
}

const (
	maxWorkflowNodeExecutions = 1000
	maxWorkflowParallelNodes  = 64
	maxWorkflowTimeoutSec     = 3600
	maxWorkflowTotalTokens    = 10_000_000
	maxWorkflowCostMicros     = int64(1_000_000_000_000)
)

// Node is the execution-only representation of a DSL node.
type Node struct {
	ID              string
	Type            string
	Properties      json.RawMessage
	InputSchema     json.RawMessage
	OutputSchema    json.RawMessage
	TimeoutSec      int
	Retry           *dsl.RetryPolicyDSL
	Policy          json.RawMessage
	ProfileRef      string
	ProviderRef     string
	Writes          []dsl.StateWriteDSL
	Compensation    *dsl.CompensationDSL
	DeclarationRank int
}

// Edge is the execution-only representation of a DSL edge.
type Edge struct {
	ID           string
	Source       string
	Target       string
	SourceHandle string
	TargetHandle string
}

// Plan is an immutable, deterministic execution plan compiled from a DSL.
// Accessors return copies so schedulers cannot mutate compiler-owned state.
type Plan struct {
	dslVersion      string
	workflowVersion int64
	nodes           []Node
	edges           []Edge
	nodeByID        map[string]Node
	nodeOrder       map[string]int
	outgoing        map[string][]Edge
	inDegree        map[string]int
	topological     []string
}

func (p *Plan) DSLVersion() string {
	if p == nil {
		return ""
	}
	return p.dslVersion
}

func (p *Plan) WorkflowVersion() int64 {
	if p == nil {
		return 0
	}
	return p.workflowVersion
}

func (p *Plan) Nodes() []Node {
	if p == nil {
		return nil
	}
	result := make([]Node, len(p.nodes))
	for index, node := range p.nodes {
		result[index] = cloneNode(node)
	}
	return result
}

func (p *Plan) Node(id string) (Node, bool) {
	if p == nil {
		return Node{}, false
	}
	node, ok := p.nodeByID[id]
	return cloneNode(node), ok
}

func (p *Plan) NodeOrder(id string) int {
	if p == nil {
		return -1
	}
	order, ok := p.nodeOrder[id]
	if !ok {
		return -1
	}
	return order
}

func (p *Plan) Outgoing(id string) []Edge {
	if p == nil {
		return nil
	}
	return append([]Edge(nil), p.outgoing[id]...)
}

func (p *Plan) InDegree(id string) int {
	if p == nil {
		return 0
	}
	return p.inDegree[id]
}

func (p *Plan) InDegrees() map[string]int {
	result := make(map[string]int)
	if p == nil {
		return result
	}
	for id, degree := range p.inDegree {
		result[id] = degree
	}
	return result
}

func (p *Plan) TopologicalOrder() []string {
	if p == nil {
		return nil
	}
	return append([]string(nil), p.topological...)
}

// Compile validates a user DSL and converts it into deterministic IR.
func Compile(definition *dsl.WorkflowDSL) (*Plan, error) {
	if definition == nil {
		return nil, fmt.Errorf("workflow DSL is required")
	}
	if len(definition.Nodes) == 0 {
		return nil, fmt.Errorf("workflow must contain at least one node")
	}

	dslVersion, err := normalizeDSLVersion(definition.DSLVersion)
	if err != nil {
		return nil, err
	}
	workflowVersion := definition.WorkflowVersion
	if workflowVersion == 0 {
		workflowVersion = 1
	}
	if workflowVersion < 1 {
		return nil, fmt.Errorf("workflow_version must be greater than zero")
	}
	if err := validateWorkflowBudget(definition.Budget); err != nil {
		return nil, err
	}

	plan := &Plan{
		dslVersion:      dslVersion,
		workflowVersion: workflowVersion,
		nodes:           make([]Node, 0, len(definition.Nodes)),
		edges:           make([]Edge, 0, len(definition.Edges)),
		nodeByID:        make(map[string]Node, len(definition.Nodes)),
		nodeOrder:       make(map[string]int, len(definition.Nodes)),
		outgoing:        make(map[string][]Edge, len(definition.Nodes)),
		inDegree:        make(map[string]int, len(definition.Nodes)),
	}

	for index, source := range definition.Nodes {
		node, compileErr := compileNode(source, index)
		if compileErr != nil {
			return nil, compileErr
		}
		if _, exists := plan.nodeByID[node.ID]; exists {
			return nil, fmt.Errorf("duplicate workflow node id %s", node.ID)
		}
		plan.nodes = append(plan.nodes, node)
		plan.nodeByID[node.ID] = node
		plan.nodeOrder[node.ID] = index
		plan.inDegree[node.ID] = 0
	}

	edgeIDs := make(map[string]struct{}, len(definition.Edges))
	for _, source := range definition.Edges {
		edge, compileErr := compileEdge(source, plan.nodeByID, edgeIDs)
		if compileErr != nil {
			return nil, compileErr
		}
		plan.edges = append(plan.edges, edge)
		plan.outgoing[edge.Source] = append(plan.outgoing[edge.Source], edge)
		plan.inDegree[edge.Target]++
	}
	for nodeID := range plan.outgoing {
		sort.SliceStable(plan.outgoing[nodeID], func(i, j int) bool {
			left := plan.outgoing[nodeID][i]
			right := plan.outgoing[nodeID][j]
			if plan.nodeOrder[left.Target] != plan.nodeOrder[right.Target] {
				return plan.nodeOrder[left.Target] < plan.nodeOrder[right.Target]
			}
			if left.SourceHandle != right.SourceHandle {
				return left.SourceHandle < right.SourceHandle
			}
			return left.ID < right.ID
		})
	}

	topological, err := deterministicTopologicalOrder(plan)
	if err != nil {
		return nil, err
	}
	plan.topological = topological

	if err := validateReferences(plan); err != nil {
		return nil, err
	}
	if err := validateGlobalWrites(plan); err != nil {
		return nil, err
	}
	return plan, nil
}

func validateWorkflowBudget(budget *dsl.BudgetDSL) error {
	if budget == nil {
		return nil
	}
	if budget.MaxNodeExecutions < 0 || budget.MaxNodeExecutions > maxWorkflowNodeExecutions {
		return fmt.Errorf("budget.max_node_executions must be between 0 and %d", maxWorkflowNodeExecutions)
	}
	if budget.MaxParallelNodes < 0 || budget.MaxParallelNodes > maxWorkflowParallelNodes {
		return fmt.Errorf("budget.max_parallel_nodes must be between 0 and %d", maxWorkflowParallelNodes)
	}
	if budget.TimeoutSec < 0 || budget.TimeoutSec > maxWorkflowTimeoutSec {
		return fmt.Errorf("budget.timeout_sec must be between 0 and %d", maxWorkflowTimeoutSec)
	}
	if budget.MaxTotalTokens < 0 || budget.MaxTotalTokens > maxWorkflowTotalTokens {
		return fmt.Errorf("budget.max_total_tokens must be between 0 and %d", maxWorkflowTotalTokens)
	}
	if budget.MaxEstimatedCostMicros < 0 || budget.MaxEstimatedCostMicros > maxWorkflowCostMicros {
		return fmt.Errorf("budget.max_estimated_cost_micros must be between 0 and %d", maxWorkflowCostMicros)
	}
	return nil
}

func normalizeDSLVersion(value string) (string, error) {
	switch strings.TrimSpace(value) {
	case "", "1", dsl.CurrentVersion:
		return dsl.CurrentVersion, nil
	default:
		return "", fmt.Errorf("unsupported dsl_version %q", value)
	}
}

func compileNode(source dsl.NodeDSL, declarationRank int) (Node, error) {
	id := strings.TrimSpace(source.ID)
	if id == "" {
		return Node{}, fmt.Errorf("workflow node id cannot be empty")
	}
	nodeType := strings.TrimSpace(source.Type)
	if nodeType == "" {
		return Node{}, fmt.Errorf("workflow node %s type cannot be empty", id)
	}
	if source.TimeoutSec < 0 {
		return Node{}, fmt.Errorf("node %s timeout_sec cannot be negative", id)
	}
	if err := validateJSONObject(source.Properties, "node "+id+" properties"); err != nil {
		return Node{}, err
	}
	if err := validateJSONObject(source.InputSchema, "node "+id+" input_schema"); err != nil {
		return Node{}, err
	}
	if err := validateJSONObject(source.OutputSchema, "node "+id+" output_schema"); err != nil {
		return Node{}, err
	}
	if err := validateJSONObject(source.Policy, "node "+id+" policy"); err != nil {
		return Node{}, err
	}
	if err := validateRetry(id, source.Retry); err != nil {
		return Node{}, err
	}

	writes := append([]dsl.StateWriteDSL(nil), source.Writes...)
	seenWritePaths := make(map[string]struct{}, len(writes))
	for index := range writes {
		writes[index].Path = strings.TrimSpace(writes[index].Path)
		writes[index].Source = strings.TrimSpace(writes[index].Source)
		writes[index].Reducer = strings.ToLower(strings.TrimSpace(writes[index].Reducer))
		if writes[index].Path == "" {
			return Node{}, fmt.Errorf("node %s write path cannot be empty", id)
		}
		pathParts := strings.Split(writes[index].Path, ".")
		if len(pathParts) != 2 || !isStateIdentifier(pathParts[0]) || !isStateIdentifier(pathParts[1]) {
			return Node{}, fmt.Errorf("node %s write path %s must use namespace.field format", id, writes[index].Path)
		}
		if writes[index].Source == "" {
			writes[index].Source = pathParts[1]
		}
		if !isStateIdentifier(writes[index].Source) {
			return Node{}, fmt.Errorf("node %s write source %s is invalid", id, writes[index].Source)
		}
		if _, exists := seenWritePaths[writes[index].Path]; exists {
			return Node{}, fmt.Errorf("node %s declares duplicate state write path %s", id, writes[index].Path)
		}
		seenWritePaths[writes[index].Path] = struct{}{}
		if writes[index].Reducer != "" {
			if _, supported := supportedReducers[writes[index].Reducer]; !supported {
				return Node{}, fmt.Errorf("node %s declares unsupported reducer %s", id, writes[index].Reducer)
			}
		}
	}
	compensation, err := compileCompensation(id, nodeType, source.Compensation)
	if err != nil {
		return Node{}, err
	}

	return Node{
		ID:              id,
		Type:            nodeType,
		Properties:      cloneRaw(source.Properties),
		InputSchema:     cloneRaw(source.InputSchema),
		OutputSchema:    cloneRaw(source.OutputSchema),
		TimeoutSec:      source.TimeoutSec,
		Retry:           cloneRetry(source.Retry),
		Policy:          cloneRaw(source.Policy),
		ProfileRef:      strings.TrimSpace(source.ProfileRef),
		ProviderRef:     strings.TrimSpace(source.ProviderRef),
		Writes:          writes,
		Compensation:    compensation,
		DeclarationRank: declarationRank,
	}, nil
}

func compileCompensation(nodeID, nodeType string, source *dsl.CompensationDSL) (*dsl.CompensationDSL, error) {
	if source == nil {
		return nil, nil
	}
	if nodeType != "tool" {
		return nil, fmt.Errorf("node %s compensation is only supported for tool nodes", nodeID)
	}
	toolName := strings.TrimSpace(source.ToolName)
	if toolName == "" || !isStateIdentifier(toolName) {
		return nil, fmt.Errorf("node %s compensation tool_name is invalid", nodeID)
	}
	if source.TimeoutSec < 0 {
		return nil, fmt.Errorf("node %s compensation timeout_sec cannot be negative", nodeID)
	}
	if err := validateJSONObject(source.Properties, "node "+nodeID+" compensation properties"); err != nil {
		return nil, err
	}
	if err := validateRetry(nodeID+" compensation", source.Retry); err != nil {
		return nil, err
	}
	return &dsl.CompensationDSL{
		ToolName:   toolName,
		Properties: cloneRaw(source.Properties),
		TimeoutSec: source.TimeoutSec,
		Retry:      cloneRetry(source.Retry),
	}, nil
}

func compileEdge(source dsl.EdgeDSL, nodes map[string]Node, seen map[string]struct{}) (Edge, error) {
	id := strings.TrimSpace(source.ID)
	if id == "" {
		return Edge{}, fmt.Errorf("workflow edge id cannot be empty")
	}
	if _, exists := seen[id]; exists {
		return Edge{}, fmt.Errorf("duplicate workflow edge id %s", id)
	}
	seen[id] = struct{}{}
	sourceID := strings.TrimSpace(source.Source)
	targetID := strings.TrimSpace(source.Target)
	if _, exists := nodes[sourceID]; !exists {
		return Edge{}, fmt.Errorf("edge %s references unknown source node %s", id, sourceID)
	}
	if _, exists := nodes[targetID]; !exists {
		return Edge{}, fmt.Errorf("edge %s references unknown target node %s", id, targetID)
	}
	if sourceID == targetID {
		return Edge{}, fmt.Errorf("edge %s cannot connect node %s to itself", id, sourceID)
	}
	return Edge{
		ID:           id,
		Source:       sourceID,
		Target:       targetID,
		SourceHandle: strings.TrimSpace(source.SourceHandle),
		TargetHandle: strings.TrimSpace(source.TargetHandle),
	}, nil
}

func validateJSONObject(raw json.RawMessage, label string) error {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "null" {
		return nil
	}
	var value map[string]interface{}
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("%s must be a JSON object: %w", label, err)
	}
	return nil
}

func validateRetry(nodeID string, retry *dsl.RetryPolicyDSL) error {
	if retry == nil {
		return nil
	}
	if retry.MaxAttempts < 0 || retry.MaxAttempts > 10 {
		return fmt.Errorf("node %s retry max_attempts must be between 0 and 10", nodeID)
	}
	if retry.InitialBackoffMS < 0 || retry.MaxBackoffMS < 0 {
		return fmt.Errorf("node %s retry backoff cannot be negative", nodeID)
	}
	if retry.MaxBackoffMS > 0 && retry.InitialBackoffMS > retry.MaxBackoffMS {
		return fmt.Errorf("node %s retry initial_backoff_ms cannot exceed max_backoff_ms", nodeID)
	}
	if retry.Multiplier < 0 || retry.Jitter < 0 || retry.Jitter > 1 {
		return fmt.Errorf("node %s retry multiplier/jitter is invalid", nodeID)
	}
	return nil
}

func deterministicTopologicalOrder(plan *Plan) ([]string, error) {
	inDegree := plan.InDegrees()
	ready := &nodeHeap{order: plan.nodeOrder}
	heap.Init(ready)
	for _, node := range plan.nodes {
		if inDegree[node.ID] == 0 {
			heap.Push(ready, node.ID)
		}
	}

	result := make([]string, 0, len(plan.nodes))
	for ready.Len() > 0 {
		id := heap.Pop(ready).(string)
		result = append(result, id)
		for _, edge := range plan.outgoing[id] {
			inDegree[edge.Target]--
			if inDegree[edge.Target] == 0 {
				heap.Push(ready, edge.Target)
			}
		}
	}
	if len(result) != len(plan.nodes) {
		return nil, fmt.Errorf("cycle detected in workflow definition DAG")
	}
	return result, nil
}

func validateReferences(plan *Plan) error {
	for _, node := range plan.nodes {
		for _, referencedNode := range references(node.Properties) {
			if _, exists := plan.nodeByID[referencedNode]; !exists {
				return fmt.Errorf("node %s properties reference unknown node %s", node.ID, referencedNode)
			}
			if referencedNode == node.ID || !hasPath(plan, referencedNode, node.ID) {
				return fmt.Errorf("node %s properties reference %s without an upstream dependency path", node.ID, referencedNode)
			}
		}
		if node.Compensation == nil {
			continue
		}
		for _, referencedNode := range references(node.Compensation.Properties) {
			if _, exists := plan.nodeByID[referencedNode]; !exists {
				return fmt.Errorf("node %s compensation properties reference unknown node %s", node.ID, referencedNode)
			}
			if referencedNode != node.ID && !hasPath(plan, referencedNode, node.ID) {
				return fmt.Errorf("node %s compensation properties reference %s without an upstream dependency path", node.ID, referencedNode)
			}
		}
	}
	return nil
}

func references(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var value interface{}
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil
	}
	seen := make(map[string]struct{})
	collectReferences(value, seen)
	result := make([]string, 0, len(seen))
	for id := range seen {
		result = append(result, id)
	}
	sort.Strings(result)
	return result
}

func collectReferences(value interface{}, seen map[string]struct{}) {
	switch typed := value.(type) {
	case map[string]interface{}:
		for _, child := range typed {
			collectReferences(child, seen)
		}
	case []interface{}:
		for _, child := range typed {
			collectReferences(child, seen)
		}
	case string:
		for _, match := range referencePattern.FindAllStringSubmatch(typed, -1) {
			if len(match) == 3 {
				seen[match[1]] = struct{}{}
			}
		}
	}
}

func hasPath(plan *Plan, source, target string) bool {
	queue := []string{source}
	visited := map[string]struct{}{source: {}}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, edge := range plan.outgoing[current] {
			if edge.Target == target {
				return true
			}
			if _, exists := visited[edge.Target]; exists {
				continue
			}
			visited[edge.Target] = struct{}{}
			queue = append(queue, edge.Target)
		}
	}
	return false
}

func validateGlobalWrites(plan *Plan) error {
	type declaration struct {
		nodeID  string
		reducer string
	}
	writes := make(map[string][]declaration)
	for _, node := range plan.nodes {
		for _, write := range node.Writes {
			writes[write.Path] = append(writes[write.Path], declaration{nodeID: node.ID, reducer: write.Reducer})
		}
	}
	paths := make([]string, 0, len(writes))
	for path := range writes {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		declarations := writes[path]
		configuredReducer := declarations[0].reducer
		for _, current := range declarations[1:] {
			if current.reducer != configuredReducer {
				return fmt.Errorf("state path %s must use one reducer across all writers: %s=%q %s=%q", path, declarations[0].nodeID, configuredReducer, current.nodeID, current.reducer)
			}
		}
		for i := 0; i < len(declarations); i++ {
			for j := i + 1; j < len(declarations); j++ {
				left := declarations[i]
				right := declarations[j]
				if hasPath(plan, left.nodeID, right.nodeID) || hasPath(plan, right.nodeID, left.nodeID) {
					continue
				}
				if left.reducer == "" {
					return fmt.Errorf("parallel nodes %s and %s write state path %s without a reducer", left.nodeID, right.nodeID, path)
				}
			}
		}
	}
	return nil
}

func isStateIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for index, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || char == '_' || (index > 0 && char >= '0' && char <= '9') || (index > 0 && char == '-') {
			continue
		}
		return false
	}
	return true
}

func cloneNode(source Node) Node {
	source.Properties = cloneRaw(source.Properties)
	source.InputSchema = cloneRaw(source.InputSchema)
	source.OutputSchema = cloneRaw(source.OutputSchema)
	source.Policy = cloneRaw(source.Policy)
	source.Retry = cloneRetry(source.Retry)
	source.Writes = append([]dsl.StateWriteDSL(nil), source.Writes...)
	source.Compensation = cloneCompensation(source.Compensation)
	return source
}

func cloneRaw(source json.RawMessage) json.RawMessage {
	return append(json.RawMessage(nil), source...)
}

func cloneRetry(source *dsl.RetryPolicyDSL) *dsl.RetryPolicyDSL {
	if source == nil {
		return nil
	}
	cloned := *source
	return &cloned
}

func cloneCompensation(source *dsl.CompensationDSL) *dsl.CompensationDSL {
	if source == nil {
		return nil
	}
	return &dsl.CompensationDSL{
		ToolName:   source.ToolName,
		Properties: cloneRaw(source.Properties),
		TimeoutSec: source.TimeoutSec,
		Retry:      cloneRetry(source.Retry),
	}
}

type nodeHeap struct {
	values []string
	order  map[string]int
}

func (h nodeHeap) Len() int { return len(h.values) }
func (h nodeHeap) Less(i, j int) bool {
	left := h.values[i]
	right := h.values[j]
	if h.order[left] != h.order[right] {
		return h.order[left] < h.order[right]
	}
	return left < right
}
func (h nodeHeap) Swap(i, j int) { h.values[i], h.values[j] = h.values[j], h.values[i] }
func (h *nodeHeap) Push(value interface{}) {
	h.values = append(h.values, value.(string))
}
func (h *nodeHeap) Pop() interface{} {
	last := len(h.values) - 1
	value := h.values[last]
	h.values = h.values[:last]
	return value
}
