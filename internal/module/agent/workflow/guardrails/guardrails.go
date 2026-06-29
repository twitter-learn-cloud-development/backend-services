package guardrails

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
)

type contextKey string

const UserIDKey contextKey = "workflow_user_id"

// RunawayLimit 限制单个工作流最大执行节点数，防御死循环与 Token 爆炸
const RunawayLimit = 50

// WorkflowTracker 用于跟踪当前工作流的执行指标
type WorkflowTracker struct {
	nodeCount int
	maxNodes  int
}

func NewWorkflowTracker() *WorkflowTracker {
	return &WorkflowTracker{
		nodeCount: 0,
		maxNodes:  RunawayLimit,
	}
}

// IncrementAndCheck 累加执行节点数，若超过阈值则熔断
func (t *WorkflowTracker) IncrementAndCheck() error {
	t.nodeCount++
	if t.nodeCount > t.maxNodes {
		return fmt.Errorf("workflow execution aborted: exceeded maximum allowed node count (%d), possible runaway loop detected", t.maxNodes)
	}
	return nil
}

// SecurityGuardrail 负责工作流执行期间的安全审计与权限硬性注入
type SecurityGuardrail struct{}

func NewSecurityGuardrail() *SecurityGuardrail {
	return &SecurityGuardrail{}
}

// InjectUserContext 将解析自 JWT 令牌的 userID 注入上下文，确保不可篡改
func InjectUserContext(ctx context.Context, userID uint64) context.Context {
	return context.WithValue(ctx, UserIDKey, userID)
}

// AuthenticatedUserID returns the gateway-authenticated workflow owner.
func AuthenticatedUserID(ctx context.Context) (uint64, bool) {
	userID, ok := ctx.Value(UserIDKey).(uint64)
	return userID, ok
}

// ValidateAndInjectToolInputs 拦截并校验工具参数。
// 🎯 核心权限防线：针对写推文（PublishTweet）等敏感操作，强制使用 Context 中的只读 userID 覆盖输入，防止 Prompt 注入越权。
func (g *SecurityGuardrail) ValidateAndInjectToolInputs(ctx context.Context, toolName string, inputs map[string]interface{}) (map[string]interface{}, error) {
	// 1. 从只读上下文提取经网关校验过的真实 UserID
	val := ctx.Value(UserIDKey)
	if val == nil {
		// 非认证请求，如果是执行发推等敏感写入操作，直接硬性拒绝
		if isSensitiveWriteTool(toolName) {
			return nil, errors.New("security audit failure: unauthorized workflow context attempt to write data")
		}
		return inputs, nil
	}

	userID, ok := val.(uint64)
	if !ok {
		return nil, errors.New("security audit failure: corrupted user_id context type")
	}

	// 2. 拦截 PublishTweet，强行覆写
	if isSensitiveWriteTool(toolName) {
		log.Printf("🔒 Security Guardrail activated for %s. Forcing user_id parameter injection: %d", toolName, userID)

		// 强制覆盖或注入只读 user_id，无视大模型/前端传递的伪造 user_id
		inputs["user_id"] = userID
	}

	return inputs, nil
}

func isSensitiveWriteTool(toolName string) bool {
	switch strings.ToLower(toolName) {
	case "publishtweet", "create_tweet":
		return true
	default:
		return false
	}
}
