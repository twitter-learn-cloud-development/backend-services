package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"twitter-clone/internal/module/agent/repository"
	"twitter-clone/internal/module/agent/workflow/dsl"
	"twitter-clone/internal/module/agent/workflow/engine"
	"twitter-clone/internal/module/agent/workflow/guardrails"
	"twitter-clone/internal/module/agent/workflow/tool"
)

const (
	WorkflowRunStatusRunning   = "running"
	WorkflowRunStatusSuspended = "suspended"
	WorkflowRunStatusSuccess   = "success"
	WorkflowRunStatusFailed    = "failed"
)

type WorkflowExecutionResult struct {
	Run         *repository.WorkflowRunRecord
	Snapshot    map[string]map[string]interface{}
	Traces      []engine.NodeTrace
	DialogueKey string
	Response    string
}

func (s *AgentService) CreateWorkflow(ctx context.Context, userID uint64, name string, dslJSON string) (*repository.WorkflowDefinition, error) {
	if s.repo == nil {
		return nil, errors.New("agent repository is not initialized")
	}
	if name == "" {
		return nil, errors.New("workflow name is required")
	}
	if err := validateWorkflowDSL(dslJSON); err != nil {
		return nil, err
	}

	workflow := &repository.WorkflowDefinition{
		UserID:  userID,
		Name:    name,
		DSLJSON: dslJSON,
	}
	if err := s.repo.CreateWorkflow(ctx, workflow); err != nil {
		return nil, err
	}
	return workflow, nil
}

func (s *AgentService) UpdateWorkflow(ctx context.Context, userID uint64, workflowID string, name string, dslJSON string) (*repository.WorkflowDefinition, error) {
	if s.repo == nil {
		return nil, errors.New("agent repository is not initialized")
	}
	if name == "" {
		return nil, errors.New("workflow name is required")
	}
	if err := validateWorkflowDSL(dslJSON); err != nil {
		return nil, err
	}

	oid, err := primitive.ObjectIDFromHex(workflowID)
	if err != nil {
		return nil, fmt.Errorf("invalid workflow_id: %w", err)
	}

	workflow := &repository.WorkflowDefinition{
		ID:      oid,
		UserID:  userID,
		Name:    name,
		DSLJSON: dslJSON,
	}
	if err := s.repo.UpdateWorkflow(ctx, workflow); err != nil {
		return nil, err
	}
	return s.repo.GetWorkflow(ctx, oid, userID)
}

func (s *AgentService) ListWorkflows(ctx context.Context, userID uint64, page, pageSize int) ([]*repository.WorkflowDefinition, int64, error) {
	if s.repo == nil {
		return nil, 0, errors.New("agent repository is not initialized")
	}
	return s.repo.ListWorkflows(ctx, userID, page, pageSize)
}

func (s *AgentService) GetWorkflow(ctx context.Context, userID uint64, workflowID string) (*repository.WorkflowDefinition, error) {
	if s.repo == nil {
		return nil, errors.New("agent repository is not initialized")
	}
	oid, err := primitive.ObjectIDFromHex(workflowID)
	if err != nil {
		return nil, fmt.Errorf("invalid workflow_id: %w", err)
	}
	return s.repo.GetWorkflow(ctx, oid, userID)
}

func (s *AgentService) RunWorkflow(ctx context.Context, userID uint64, workflowID string, inputJSON string) (*WorkflowExecutionResult, error) {
	if s.repo == nil {
		return nil, errors.New("agent repository is not initialized")
	}

	workflow, err := s.GetWorkflow(ctx, userID, workflowID)
	if err != nil {
		return nil, err
	}

	var dslObj dsl.WorkflowDSL
	if err := json.Unmarshal([]byte(workflow.DSLJSON), &dslObj); err != nil {
		return nil, fmt.Errorf("invalid workflow DSL JSON: %w", err)
	}

	initialInputs, err := parseWorkflowInput(inputJSON)
	if err != nil {
		return nil, err
	}

	var workflowDialogue *repository.Dialogue
	if boolWorkflowInput(initialInputs, "persist_dialogue") {
		userInput, _ := initialInputs["user_input"].(string)
		dialogueKey, _ := initialInputs["dialogue_key"].(string)
		workflowDialogue, err = s.getOrCreateDialogue(
			ctx,
			userID,
			resolveDialogueKey(0, dialogueKey),
			userInput,
			repository.ModeWorkflow,
		)
		if err != nil {
			return nil, fmt.Errorf("prepare workflow dialogue failed: %w", err)
		}
		initialInputs["dialogue_key"] = workflowDialogue.ID.Hex()
	}

	run := &repository.WorkflowRunRecord{
		WorkflowID: workflow.ID,
		UserID:     userID,
		Status:     WorkflowRunStatusRunning,
		InputJSON:  inputJSON,
		StartedAt:  time.Now(),
	}
	if run.InputJSON == "" {
		run.InputJSON = "{}"
	}
	if err := s.repo.CreateWorkflowRun(ctx, run); err != nil {
		return nil, err
	}

	nodeImpls, err := buildWorkflowNodes(&dslObj)
	if err != nil {
		result, finishErr := s.finishWorkflowRun(ctx, run, nil, nil, err)
		return s.persistWorkflowDialogue(ctx, result, workflowDialogue, initialInputs, nil, err, finishErr)
	}

	scheduler, err := engine.NewScheduler(&dslObj, nodeImpls)
	if err != nil {
		result, finishErr := s.finishWorkflowRun(ctx, run, nil, nil, err)
		return s.persistWorkflowDialogue(ctx, result, workflowDialogue, initialInputs, nil, err, finishErr)
	}

	execCtx := guardrails.InjectUserContext(ctx, userID)
	err = scheduler.Execute(execCtx, initialInputs)
	snapshot := scheduler.GetBlackboard().GetSnapshot()
	traces := scheduler.GetTraces()
	var suspension *engine.SuspensionError
	if errors.As(err, &suspension) {
		checkpoint := scheduler.GetCheckpoint(suspension)
		result, suspendErr := s.suspendWorkflowRun(ctx, run, snapshot, traces, checkpoint, suspension)
		return s.persistWorkflowDialogue(ctx, result, workflowDialogue, initialInputs, &dslObj, nil, suspendErr)
	}
	result, finishErr := s.finishWorkflowRun(ctx, run, snapshot, traces, err)
	return s.persistWorkflowDialogue(ctx, result, workflowDialogue, initialInputs, &dslObj, err, finishErr)
}

func (s *AgentService) ResumeWorkflowRun(ctx context.Context, userID uint64, runID string, resumeInputJSON string) (*WorkflowExecutionResult, error) {
	if s.repo == nil {
		return nil, errors.New("agent repository is not initialized")
	}
	oid, err := primitive.ObjectIDFromHex(runID)
	if err != nil {
		return nil, fmt.Errorf("invalid run_id: %w", err)
	}
	run, err := s.repo.GetWorkflowRun(ctx, oid, userID)
	if err != nil {
		return nil, err
	}
	if run.Status != WorkflowRunStatusSuspended {
		return nil, fmt.Errorf("workflow run %s is not suspended", runID)
	}

	workflow, err := s.repo.GetWorkflow(ctx, run.WorkflowID, userID)
	if err != nil {
		return nil, err
	}

	var dslObj dsl.WorkflowDSL
	if err := json.Unmarshal([]byte(workflow.DSLJSON), &dslObj); err != nil {
		return nil, fmt.Errorf("invalid workflow DSL JSON: %w", err)
	}
	var checkpoint engine.WorkflowCheckpoint
	if err := json.Unmarshal([]byte(run.CheckpointJSON), &checkpoint); err != nil {
		return nil, fmt.Errorf("invalid workflow checkpoint JSON: %w", err)
	}
	resumeInputs, err := parseWorkflowInput(resumeInputJSON)
	if err != nil {
		return nil, err
	}

	nodeImpls, err := buildWorkflowNodes(&dslObj)
	if err != nil {
		return s.finishWorkflowRun(ctx, run, nil, nil, err)
	}
	scheduler, err := engine.NewScheduler(&dslObj, nodeImpls)
	if err != nil {
		return s.finishWorkflowRun(ctx, run, nil, nil, err)
	}

	run.Status = WorkflowRunStatusRunning
	run.ErrorMessage = ""
	execCtx := guardrails.InjectUserContext(ctx, userID)
	err = scheduler.ExecuteFromCheckpoint(execCtx, checkpoint, resumeInputs)
	snapshot := scheduler.GetBlackboard().GetSnapshot()
	traces := scheduler.GetTraces()
	var suspension *engine.SuspensionError
	if errors.As(err, &suspension) {
		nextCheckpoint := scheduler.GetCheckpoint(suspension)
		return s.suspendWorkflowRun(ctx, run, snapshot, traces, nextCheckpoint, suspension)
	}
	return s.finishWorkflowRun(ctx, run, snapshot, traces, err)
}

func (s *AgentService) GetWorkflowRun(ctx context.Context, userID uint64, runID string) (*repository.WorkflowRunRecord, error) {
	if s.repo == nil {
		return nil, errors.New("agent repository is not initialized")
	}
	oid, err := primitive.ObjectIDFromHex(runID)
	if err != nil {
		return nil, fmt.Errorf("invalid run_id: %w", err)
	}
	return s.repo.GetWorkflowRun(ctx, oid, userID)
}

func (s *AgentService) finishWorkflowRun(ctx context.Context, run *repository.WorkflowRunRecord, snapshot map[string]map[string]interface{}, traces []engine.NodeTrace, execErr error) (*WorkflowExecutionResult, error) {
	run.FinishedAt = time.Now()
	if snapshot != nil || len(traces) > 0 {
		output := make(map[string]interface{})
		for nodeID, values := range snapshot {
			output[nodeID] = values
		}
		output["blackboard"] = snapshot
		output["traces"] = traces
		outputBytes, _ := json.Marshal(output)
		run.OutputJSON = string(outputBytes)
	}
	if run.OutputJSON == "" {
		run.OutputJSON = "{}"
	}

	if execErr != nil {
		run.Status = WorkflowRunStatusFailed
		run.ErrorMessage = execErr.Error()
	} else {
		run.Status = WorkflowRunStatusSuccess
	}
	run.CheckpointJSON = ""
	run.WaitingNodeID = ""
	run.ResumeToken = ""
	run.SuspendedAt = time.Time{}

	if err := s.repo.UpdateWorkflowRun(ctx, run); err != nil {
		return nil, err
	}

	return &WorkflowExecutionResult{
		Run:      run,
		Snapshot: snapshot,
		Traces:   traces,
	}, nil
}

func (s *AgentService) suspendWorkflowRun(ctx context.Context, run *repository.WorkflowRunRecord, snapshot map[string]map[string]interface{}, traces []engine.NodeTrace, checkpoint engine.WorkflowCheckpoint, suspension *engine.SuspensionError) (*WorkflowExecutionResult, error) {
	run.Status = WorkflowRunStatusSuspended
	run.ErrorMessage = ""
	run.FinishedAt = time.Time{}
	run.SuspendedAt = time.Now()
	if suspension != nil {
		run.WaitingNodeID = suspension.Suspension.NodeID
		run.ResumeToken = suspension.Suspension.ResumeToken
	}

	checkpointBytes, _ := json.Marshal(checkpoint)
	run.CheckpointJSON = string(checkpointBytes)

	output := make(map[string]interface{})
	for nodeID, values := range snapshot {
		output[nodeID] = values
	}
	output["blackboard"] = snapshot
	output["traces"] = traces
	output["checkpoint"] = checkpoint
	outputBytes, _ := json.Marshal(output)
	run.OutputJSON = string(outputBytes)
	if run.OutputJSON == "" {
		run.OutputJSON = "{}"
	}

	if err := s.repo.UpdateWorkflowRun(ctx, run); err != nil {
		return nil, err
	}
	return &WorkflowExecutionResult{
		Run:      run,
		Snapshot: snapshot,
		Traces:   traces,
	}, nil
}

func validateWorkflowDSL(dslJSON string) error {
	if dslJSON == "" {
		return errors.New("dsl_json is required")
	}
	var dslObj dsl.WorkflowDSL
	if err := json.Unmarshal([]byte(dslJSON), &dslObj); err != nil {
		return fmt.Errorf("invalid workflow DSL JSON: %w", err)
	}
	nodeImpls, err := buildWorkflowNodes(&dslObj)
	if err != nil {
		return err
	}
	if _, err := engine.NewScheduler(&dslObj, nodeImpls); err != nil {
		return err
	}
	return nil
}

func parseWorkflowInput(inputJSON string) (map[string]interface{}, error) {
	if inputJSON == "" {
		return map[string]interface{}{}, nil
	}
	var inputs map[string]interface{}
	if err := json.Unmarshal([]byte(inputJSON), &inputs); err != nil {
		return nil, fmt.Errorf("invalid workflow input JSON: %w", err)
	}
	return inputs, nil
}

func boolWorkflowInput(inputs map[string]interface{}, key string) bool {
	value, ok := inputs[key]
	if !ok {
		return false
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return typed == "true" || typed == "1"
	default:
		return false
	}
}

func (s *AgentService) persistWorkflowDialogue(
	ctx context.Context,
	result *WorkflowExecutionResult,
	dialogue *repository.Dialogue,
	inputs map[string]interface{},
	dslObj *dsl.WorkflowDSL,
	execErr error,
	resultErr error,
) (*WorkflowExecutionResult, error) {
	if resultErr != nil || result == nil || dialogue == nil {
		return result, resultErr
	}

	result.DialogueKey = dialogue.ID.Hex()
	userInput, _ := inputs["user_input"].(string)
	assistantContent := workflowAssistantContent(result.Snapshot, dslObj)
	if result.Run.Status == WorkflowRunStatusSuspended {
		assistantContent = fmt.Sprintf(
			"工作流已在节点 %s 挂起，等待审批或外部回调后继续执行。",
			result.Run.WaitingNodeID,
		)
	} else if execErr != nil {
		assistantContent = fmt.Sprintf("工作流执行失败：%v", execErr)
	}
	if assistantContent == "" {
		assistantContent = "工作流执行完成，但没有产生可展示的文本结果。"
	}
	result.Response = assistantContent

	metadata := map[string]any{
		"workflow_id": result.Run.WorkflowID.Hex(),
		"run_id":      result.Run.ID.Hex(),
		"status":      result.Run.Status,
	}
	if err := s.saveUserAndAssistantMessages(ctx, dialogue.ID, dialogue.UserID, userInput, assistantContent, metadata); err != nil {
		return nil, fmt.Errorf("persist workflow dialogue messages failed: %w", err)
	}
	return result, nil
}

func workflowAssistantContent(snapshot map[string]map[string]interface{}, dslObj *dsl.WorkflowDSL) string {
	preferredKeys := []string{"text", "response", "result", "content", "answer", "final", "summary", "plan"}
	readNode := func(nodeID string) string {
		values := snapshot[nodeID]
		for _, key := range preferredKeys {
			if text, ok := values[key].(string); ok && text != "" {
				return text
			}
		}
		return ""
	}

	if dslObj != nil {
		for i := len(dslObj.Nodes) - 1; i >= 0; i-- {
			if text := readNode(dslObj.Nodes[i].ID); text != "" {
				return text
			}
		}
	}
	for nodeID := range snapshot {
		if text := readNode(nodeID); text != "" {
			return text
		}
	}
	return ""
}

func buildWorkflowNodes(dslObj *dsl.WorkflowDSL) ([]engine.WorkflowNode, error) {
	nodes := make([]engine.WorkflowNode, 0, len(dslObj.Nodes))
	for _, nodeDSL := range dslObj.Nodes {
		switch nodeDSL.Type {
		case "start":
			nodes = append(nodes, &passthroughWorkflowNode{id: nodeDSL.ID, nodeType: nodeDSL.Type})
		case "end":
			nodes = append(nodes, &passthroughWorkflowNode{id: nodeDSL.ID, nodeType: nodeDSL.Type})
		case "router":
			nodes = append(nodes, &routerWorkflowNode{id: nodeDSL.ID, props: nodeDSL.Properties})
		case "wait":
			nodes = append(nodes, &waitWorkflowNode{id: nodeDSL.ID, props: nodeDSL.Properties})
		case "llm":
			nodes = append(nodes, tool.NewToolNode(nodeDSL.ID, "LLMChat"))
		case "agent":
			toolName, err := resolveWorkflowToolName(nodeDSL.Properties)
			if err != nil {
				return nil, fmt.Errorf("node %s invalid agent config: %w", nodeDSL.ID, err)
			}
			nodes = append(nodes, tool.NewToolNode(nodeDSL.ID, toolName))
		case "tool":
			toolName, err := resolveWorkflowToolName(nodeDSL.Properties)
			if err != nil {
				return nil, fmt.Errorf("node %s invalid tool config: %w", nodeDSL.ID, err)
			}
			nodes = append(nodes, tool.NewToolNode(nodeDSL.ID, toolName))
		default:
			return nil, fmt.Errorf("unsupported workflow node type %q for node %s", nodeDSL.Type, nodeDSL.ID)
		}
	}
	return nodes, nil
}

func resolveWorkflowToolName(rawProps json.RawMessage) (string, error) {
	var props map[string]interface{}
	if len(rawProps) > 0 {
		if err := json.Unmarshal(rawProps, &props); err != nil {
			return "", fmt.Errorf("invalid tool properties JSON: %w", err)
		}
	}
	if toolName, ok := props["tool_name"].(string); ok && toolName != "" {
		return toolName, nil
	}
	if _, ok := props["content"]; ok {
		return "PublishTweet", nil
	}
	if _, ok := props["query"]; ok {
		return "WebSearch", nil
	}
	return "", errors.New("missing tool_name")
}

type passthroughWorkflowNode struct {
	id       string
	nodeType string
}

func (n *passthroughWorkflowNode) ID() string {
	return n.id
}

func (n *passthroughWorkflowNode) Type() string {
	return n.nodeType
}

func (n *passthroughWorkflowNode) Execute(ctx context.Context, blackboard *engine.Blackboard, inputs map[string]interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}

type routerWorkflowNode struct {
	id    string
	props json.RawMessage
}

func (n *routerWorkflowNode) ID() string {
	return n.id
}

func (n *routerWorkflowNode) Type() string {
	return "router"
}

func (n *routerWorkflowNode) Execute(ctx context.Context, blackboard *engine.Blackboard, inputs map[string]interface{}) (map[string]interface{}, error) {
	var props map[string]interface{}
	if len(n.props) > 0 {
		_ = json.Unmarshal(n.props, &props)
	}
	if branch, ok := props["branch"].(string); ok && branch != "" {
		return map[string]interface{}{"_branch": branch}, nil
	}
	if branch, ok := props["_branch"].(string); ok && branch != "" {
		return map[string]interface{}{"_branch": branch}, nil
	}
	return map[string]interface{}{"_branch": "true"}, nil
}

type waitWorkflowNode struct {
	id    string
	props json.RawMessage
}

func (n *waitWorkflowNode) ID() string {
	return n.id
}

func (n *waitWorkflowNode) Type() string {
	return "wait"
}

func (n *waitWorkflowNode) Execute(ctx context.Context, blackboard *engine.Blackboard, inputs map[string]interface{}) (map[string]interface{}, error) {
	var props map[string]interface{}
	if len(n.props) > 0 {
		_ = json.Unmarshal(n.props, &props)
	}

	reason := "waiting for external callback"
	if value, ok := props["reason"].(string); ok && value != "" {
		reason = value
	}
	resumeToken := n.id
	if value, ok := props["resume_token"].(string); ok && value != "" {
		resumeToken = value
	}

	metadata := make(map[string]interface{})
	for k, v := range props {
		metadata[k] = v
	}
	return nil, engine.NewSuspensionError(n.id, reason, resumeToken, metadata)
}
