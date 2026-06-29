package temporal

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"twitter-clone/internal/module/agent/workflow/dsl"
	"twitter-clone/internal/module/agent/workflow/engine"
	"twitter-clone/internal/module/agent/workflow/guardrails"
	"twitter-clone/internal/module/agent/workflow/tool"
)

// WorkflowAgentActivities 包装了执行引擎节点活动的 Temporal 结构
type WorkflowAgentActivities struct{}

// ExecuteNodeActivity 用于在 Temporal Activity 容器中隔离运行单个工作流节点
func (a *WorkflowAgentActivities) ExecuteNodeActivity(ctx context.Context, toolName string, inputs map[string]interface{}) (map[string]interface{}, error) {
	reg := tool.GetRegistry()
	t, ok := reg.Get(toolName)
	if !ok {
		return nil, fmt.Errorf("tool %s is not registered", toolName)
	}

	activity.GetLogger(ctx).Info("Executing tool via Temporal Activity", "ToolName", toolName)

	guardedInputs, err := guardrails.NewSecurityGuardrail().ValidateAndInjectToolInputs(ctx, toolName, cloneActivityInputs(inputs))
	if err != nil {
		return nil, fmt.Errorf("tool %s blocked by security guardrail: %w", toolName, err)
	}

	// 调用具体工具逻辑
	outputs, err := t.Execute(ctx, guardedInputs)
	if err != nil {
		return nil, fmt.Errorf("tool %s execute failed: %w", toolName, err)
	}

	return outputs, nil
}

func cloneActivityInputs(inputs map[string]interface{}) map[string]interface{} {
	cloned := make(map[string]interface{}, len(inputs))
	for k, v := range inputs {
		cloned[k] = v
	}
	return cloned
}

// WorkflowAgentTemporalWorkflow 分布式容错的智能体工作流引擎
func WorkflowAgentTemporalWorkflow(ctx workflow.Context, dslObj dsl.WorkflowDSL, initialInputs map[string]interface{}) (map[string]interface{}, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting WorkflowAgentTemporalWorkflow", "WorkflowID", dslObj.ID, "Name", dslObj.Name)

	// 1. 初始化黑板状态树
	blackboard := engine.NewBlackboard()
	blackboard.ApplyDelta("start", initialInputs)

	// 2. Kahn 拓扑依赖编排
	adjList := make(map[string][]dsl.EdgeDSL)
	inDegrees := make(map[string]int)
	skipped := make(map[string]bool)

	// 初始化入度
	for _, n := range dslObj.Nodes {
		inDegrees[n.ID] = 0
	}
	for _, edge := range dslObj.Edges {
		adjList[edge.Source] = append(adjList[edge.Source], edge)
		inDegrees[edge.Target]++
	}

	// 3. 构建就绪节点列表
	var readyQueue []string
	for id, deg := range inDegrees {
		if deg == 0 {
			readyQueue = append(readyQueue, id)
		}
	}

	// 4. 拓扑迭代调度
	var activities *WorkflowAgentActivities
	for len(readyQueue) > 0 {
		// 取出当前就绪节点
		currNodeID := readyQueue[0]
		readyQueue = readyQueue[1:]

		// 检查是否被上游标记为 skip
		if skipped[currNodeID] {
			// 传播 skip 到下游
			for _, edge := range adjList[currNodeID] {
				skipped[edge.Target] = true
				inDegrees[edge.Target]--
				if inDegrees[edge.Target] == 0 {
					readyQueue = append(readyQueue, edge.Target)
				}
			}
			continue
		}

		nodeDSL := findNodeDSL(dslObj, currNodeID)
		if nodeDSL == nil {
			return nil, fmt.Errorf("node %s metadata missing in DSL", currNodeID)
		}

		// 🎯 核心护栏：状态挂起与人工协同 (Human-in-the-loop)
		// 如果节点类型为 "approve" (审批节点)，触发长挂起，等待外部审批 Signal 信号唤醒水化
		if nodeDSL.Type == "approve" || nodeDSL.Type == "wait" {
			logger.Info("Workflow execution suspended, waiting for human approval signal", "NodeID", currNodeID)

			var approved bool
			signalChan := workflow.GetSignalChannel(ctx, fmt.Sprintf("approve-signal-%s", currNodeID))

			// 阻塞等待外部审批事件，Temporal 内部会自动把状态树序列化，释放计算资源
			signalChan.Receive(ctx, &approved)

			logger.Info("Workflow rehydrated and resumed from signal", "NodeID", currNodeID, "Approved", approved)

			// 如果审批拒绝，直接熔断并退出
			if !approved {
				return map[string]interface{}{
					"status": "terminated_by_user",
					"node":   currNodeID,
				}, nil
			}

			// 记录结果并向下推进
			blackboard.ApplyDelta(currNodeID, map[string]interface{}{"approved": true})
			for _, edge := range adjList[currNodeID] {
				inDegrees[edge.Target]--
				if inDegrees[edge.Target] == 0 {
					readyQueue = append(readyQueue, edge.Target)
				}
			}
			continue
		}

		// 解析输入参数引用 {{nodeID.field}}
		inputs := resolveInputsFromBlackboard(blackboard, nodeDSL)

		// 调用 Activity 运行节点
		var outputs map[string]interface{}

		// 设定节点级的超时与指数级退避重试策略，防御网络波动与大模型短暂 429
		retryPolicy := &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    10 * time.Second,
			MaximumAttempts:    3,
		}

		ao := workflow.ActivityOptions{
			StartToCloseTimeout: time.Duration(nodeDSL.TimeoutSec) * time.Second,
			RetryPolicy:         retryPolicy,
		}
		if ao.StartToCloseTimeout <= 0 {
			ao.StartToCloseTimeout = 30 * time.Second // 默认 30s 超时
		}

		ctxActivity := workflow.WithActivityOptions(ctx, ao)

		// 获取对应的 Tool 名称
		var toolName string
		if nodeDSL.Type == "llm" {
			toolName = "LLMChat" // 默认映射
		} else if nodeDSL.Type == "tool" {
			var props map[string]interface{}
			_ = json.Unmarshal(nodeDSL.Properties, &props)
			if tName, ok := props["tool_name"].(string); ok {
				toolName = tName
			}
		}

		if toolName != "" {
			err := workflow.ExecuteActivity(ctxActivity, activities.ExecuteNodeActivity, toolName, inputs).Get(ctxActivity, &outputs)
			if err != nil {
				return nil, fmt.Errorf("temporal activity execution failed for node %s: %w", currNodeID, err)
			}
		}

		// 存入黑板
		blackboard.ApplyDelta(currNodeID, outputs)

		// 处理下游入度扣减与 Router 分支流转
		activeBranch := ""
		if branchVal, ok := outputs["_branch"]; ok {
			if branchStr, isStr := branchVal.(string); isStr {
				activeBranch = branchStr
			}
		}
		isRouter := nodeDSL.Type == "router"

		for _, edge := range adjList[currNodeID] {
			if isRouter && activeBranch != "" && edge.SourceHandle != activeBranch {
				skipped[edge.Target] = true
			}
			inDegrees[edge.Target]--
			if inDegrees[edge.Target] == 0 {
				readyQueue = append(readyQueue, edge.Target)
			}
		}
	}

	logger.Info("WorkflowAgentTemporalWorkflow executed successfully")

	// 返回 End 节点或最后一个节点的输出
	snapshot := blackboard.GetSnapshot()
	if endData, ok := snapshot["end"]; ok {
		return endData, nil
	}

	// 默认返回全部黑板快照
	flatSnapshot := make(map[string]interface{})
	for nodeID, fields := range snapshot {
		for k, v := range fields {
			flatSnapshot[nodeID+"."+k] = v
		}
	}
	return flatSnapshot, nil
}

func findNodeDSL(dslObj dsl.WorkflowDSL, id string) *dsl.NodeDSL {
	for i := range dslObj.Nodes {
		if dslObj.Nodes[i].ID == id {
			return &dslObj.Nodes[i]
		}
	}
	return nil
}

func resolveInputsFromBlackboard(blackboard *engine.Blackboard, nodeDSL *dsl.NodeDSL) map[string]interface{} {
	inputs := make(map[string]interface{})
	if len(nodeDSL.Properties) == 0 {
		return inputs
	}

	var rawProps map[string]interface{}
	_ = json.Unmarshal(nodeDSL.Properties, &rawProps)

	snapshot := blackboard.GetSnapshot()

	for k, v := range rawProps {
		if strVal, ok := v.(string); ok {
			// 支持 {{nodeID.field}} 替换
			for srcNode, fields := range snapshot {
				for srcField, val := range fields {
					placeholder := fmt.Sprintf("{{%s.%s}}", srcNode, srcField)
					if strings.Contains(strVal, placeholder) {
						strVal = strings.ReplaceAll(strVal, placeholder, fmt.Sprintf("%v", val))
					}
				}
			}
			inputs[k] = strVal
		} else {
			inputs[k] = v
		}
	}
	return inputs
}
