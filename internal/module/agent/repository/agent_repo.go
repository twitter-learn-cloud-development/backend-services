package repository

import (
	"context"
	"crypto/rand"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ========================== 接口定义 ==========================

// AgentRepository 对话持久化接口
// 所有对话历史的存取操作均通过此接口完成，底层实现使用 MongoDB
type AgentRepository interface {
	// ---- 对话会话管理 ----

	// CreateDialogue 创建新对话会话
	CreateDialogue(ctx context.Context, userID uint64, title string, mode DialogueMode) (*Dialogue, error)

	// ListDialogues 获取用户的对话列表（按更新时间倒序）
	ListDialogues(ctx context.Context, userID uint64, page, pageSize int) ([]*Dialogue, int64, error)

	// GetDialogue 根据 ID 获取单个对话
	GetDialogue(ctx context.Context, dialogueID primitive.ObjectID) (*Dialogue, error)

	// UpdateDialogueTitle 更新对话标题
	UpdateDialogueTitle(ctx context.Context, dialogueID primitive.ObjectID, title string) error

	// TouchDialogue 更新对话的 updated_at 时间（每次新增消息时调用）
	TouchDialogue(ctx context.Context, dialogueID primitive.ObjectID) error

	// DeleteDialogue 删除对话及其所有消息
	DeleteDialogue(ctx context.Context, dialogueID primitive.ObjectID, userID uint64) error

	// ---- 对话消息管理 ----

	// SaveMessage 保存单条消息到指定对话
	SaveMessage(ctx context.Context, msg *DialogueMessage) error

	// SaveMessages 批量保存消息（用于一次性持久化 user + assistant 消息对）
	SaveMessages(ctx context.Context, msgs []*DialogueMessage) error

	// GetMessages 获取指定对话的所有消息（按创建时间正序）
	GetMessages(ctx context.Context, dialogueID primitive.ObjectID) ([]*DialogueMessage, error)

	// GetRecentMessages 获取指定对话的最近 N 条消息（用于构建多轮上下文）
	GetRecentMessages(ctx context.Context, dialogueID primitive.ObjectID, limit int) ([]*DialogueMessage, error)

	// ---- 索引初始化 ----

	// EnsureIndexes 创建必要的 MongoDB 索引
	EnsureIndexes(ctx context.Context) error

	// ---- 自定义工作流管理 ----

	CreateWorkflow(ctx context.Context, workflow *WorkflowDefinition) error
	UpdateWorkflow(ctx context.Context, workflow *WorkflowDefinition) error
	ListWorkflows(ctx context.Context, userID uint64, page, pageSize int) ([]*WorkflowDefinition, int64, error)
	GetWorkflow(ctx context.Context, workflowID primitive.ObjectID, userID uint64) (*WorkflowDefinition, error)

	// ---- 工作流运行记录 ----

	CreateWorkflowRun(ctx context.Context, run *WorkflowRunRecord) error
	UpdateWorkflowRun(ctx context.Context, run *WorkflowRunRecord) error
	GetWorkflowRun(ctx context.Context, runID primitive.ObjectID, userID uint64) (*WorkflowRunRecord, error)
}

// ========================== MongoDB 实现 ==========================

// MongoAgentRepository MongoDB 实现的对话仓储
type MongoAgentRepository struct {
	dialogueColl             *mongo.Collection
	messageColl              *mongo.Collection
	workflowColl             *mongo.Collection
	workflowRevisionColl     *mongo.Collection
	runColl                  *mongo.Collection
	workflowStateEventColl   *mongo.Collection
	workflowSnapshotColl     *mongo.Collection
	workflowCompensationColl *mongo.Collection
	providerConfigColl       *mongo.Collection
	agentProjectColl         *mongo.Collection
	mcpConnectionColl        *mongo.Collection
	mcpToolSnapshotColl      *mongo.Collection
	approvalColl             *mongo.Collection
	executionColl            *mongo.Collection
	agentExecutionRunColl    *mongo.Collection
}

// NewMongoAgentRepository 创建 MongoDB 对话仓储
func NewMongoAgentRepository(db *mongo.Database) *MongoAgentRepository {
	return &MongoAgentRepository{
		dialogueColl:             db.Collection(CollectionDialogues),
		messageColl:              db.Collection(CollectionMessages),
		workflowColl:             db.Collection(CollectionWorkflows),
		workflowRevisionColl:     db.Collection(CollectionWorkflowRevisions),
		runColl:                  db.Collection(CollectionRuns),
		workflowStateEventColl:   db.Collection(CollectionWorkflowStateEvents),
		workflowSnapshotColl:     db.Collection(CollectionWorkflowSnapshots),
		workflowCompensationColl: db.Collection(CollectionWorkflowCompensations),
		providerConfigColl:       db.Collection(CollectionProviderConfigs),
		agentProjectColl:         db.Collection(CollectionAgentProjects),
		mcpConnectionColl:        db.Collection(CollectionMCPConnections),
		mcpToolSnapshotColl:      db.Collection(CollectionMCPToolSnapshots),
		approvalColl:             db.Collection(CollectionToolApprovals),
		executionColl:            db.Collection(CollectionToolExecutions),
		agentExecutionRunColl:    db.Collection(CollectionAgentExecutionRuns),
	}
}

// ---- 对话会话管理 ----

func (r *MongoAgentRepository) CreateDialogue(ctx context.Context, userID uint64, title string, mode DialogueMode) (*Dialogue, error) {
	now := time.Now()

	// 为了兼容 gRPC 层的 uint64 传输（有损转换取后8字节），
	// 我们显式构造一个前 4 字节为 0，后 8 字节为随机 uint64 的 ObjectID。
	// 这能保证在双向转换时实现 100% 无损。
	var oid primitive.ObjectID
	var randBytes [8]byte
	if _, err := rand.Read(randBytes[:]); err != nil {
		return nil, fmt.Errorf("generate random bytes for ObjectID failed: %w", err)
	}
	// 填充后 8 字节
	copy(oid[4:], randBytes[:])

	dialogue := &Dialogue{
		ID:        oid,
		UserID:    userID,
		Title:     title,
		Mode:      mode,
		CreatedAt: now,
		UpdatedAt: now,
	}

	_, err := r.dialogueColl.InsertOne(ctx, dialogue)
	if err != nil {
		return nil, fmt.Errorf("insert dialogue failed: %w", err)
	}

	return dialogue, nil
}

func (r *MongoAgentRepository) ListDialogues(ctx context.Context, userID uint64, page, pageSize int) ([]*Dialogue, int64, error) {
	filter := bson.M{"user_id": userID}

	// 查询总数
	total, err := r.dialogueColl.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("count dialogues failed: %w", err)
	}

	// 分页查询，按 updated_at 倒序
	skip := int64((page - 1) * pageSize)
	opts := options.Find().
		SetSort(bson.D{{Key: "updated_at", Value: -1}}).
		SetSkip(skip).
		SetLimit(int64(pageSize))

	cursor, err := r.dialogueColl.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("find dialogues failed: %w", err)
	}
	defer cursor.Close(ctx)

	var dialogues []*Dialogue
	if err := cursor.All(ctx, &dialogues); err != nil {
		return nil, 0, fmt.Errorf("decode dialogues failed: %w", err)
	}

	return dialogues, total, nil
}

func (r *MongoAgentRepository) GetDialogue(ctx context.Context, dialogueID primitive.ObjectID) (*Dialogue, error) {
	var dialogue Dialogue
	err := r.dialogueColl.FindOne(ctx, bson.M{"_id": dialogueID}).Decode(&dialogue)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			// 🎯 降级兼容：如果是前 4 字节为零的有损 ID，尝试在内存中按后 8 字节匹配旧的历史会话
			isZeroPrefix := true
			for i := 0; i < 4; i++ {
				if dialogueID[i] != 0 {
					isZeroPrefix = false
					break
				}
			}
			if isZeroPrefix {
				cursor, findErr := r.dialogueColl.Find(ctx, bson.M{})
				if findErr == nil {
					defer cursor.Close(ctx)
					var list []*Dialogue
					if cursor.All(ctx, &list) == nil {
						for _, d := range list {
							// 比较后 8 字节是否一致
							matched := true
							for i := 4; i < 12; i++ {
								if d.ID[i] != dialogueID[i] {
									matched = false
									break
								}
							}
							if matched {
								return d, nil
							}
						}
					}
				}
			}
			return nil, fmt.Errorf("dialogue not found: %s", dialogueID.Hex())
		}
		return nil, fmt.Errorf("find dialogue failed: %w", err)
	}
	return &dialogue, nil
}

func (r *MongoAgentRepository) UpdateDialogueTitle(ctx context.Context, dialogueID primitive.ObjectID, title string) error {
	_, err := r.dialogueColl.UpdateByID(ctx, dialogueID, bson.M{
		"$set": bson.M{
			"title":      title,
			"updated_at": time.Now(),
		},
	})
	if err != nil {
		return fmt.Errorf("update dialogue title failed: %w", err)
	}
	return nil
}

func (r *MongoAgentRepository) TouchDialogue(ctx context.Context, dialogueID primitive.ObjectID) error {
	_, err := r.dialogueColl.UpdateByID(ctx, dialogueID, bson.M{
		"$set": bson.M{"updated_at": time.Now()},
	})
	if err != nil {
		return fmt.Errorf("touch dialogue failed: %w", err)
	}
	return nil
}

func (r *MongoAgentRepository) DeleteDialogue(ctx context.Context, dialogueID primitive.ObjectID, userID uint64) error {
	// 先验证对话归属
	dialogue, err := r.GetDialogue(ctx, dialogueID)
	if err != nil {
		return err
	}
	if dialogue.UserID != userID {
		return fmt.Errorf("dialogue does not belong to user %d", userID)
	}

	// 删除对话下的所有消息
	_, err = r.messageColl.DeleteMany(ctx, bson.M{"dialogue_id": dialogueID})
	if err != nil {
		return fmt.Errorf("delete dialogue messages failed: %w", err)
	}

	// 删除对话本身
	_, err = r.dialogueColl.DeleteOne(ctx, bson.M{"_id": dialogueID})
	if err != nil {
		return fmt.Errorf("delete dialogue failed: %w", err)
	}

	return nil
}

// ---- 对话消息管理 ----

func (r *MongoAgentRepository) SaveMessage(ctx context.Context, msg *DialogueMessage) error {
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now()
	}

	result, err := r.messageColl.InsertOne(ctx, msg)
	if err != nil {
		return fmt.Errorf("insert message failed: %w", err)
	}

	msg.ID = result.InsertedID.(primitive.ObjectID)
	return nil
}

func (r *MongoAgentRepository) SaveMessages(ctx context.Context, msgs []*DialogueMessage) error {
	if len(msgs) == 0 {
		return nil
	}

	now := time.Now()
	docs := make([]any, len(msgs))
	for i, msg := range msgs {
		if msg.CreatedAt.IsZero() {
			msg.CreatedAt = now
		}
		docs[i] = msg
	}

	results, err := r.messageColl.InsertMany(ctx, docs)
	if err != nil {
		return fmt.Errorf("insert messages failed: %w", err)
	}

	// 回填生成的 ObjectID
	for i, id := range results.InsertedIDs {
		msgs[i].ID = id.(primitive.ObjectID)
	}

	return nil
}

func (r *MongoAgentRepository) GetMessages(ctx context.Context, dialogueID primitive.ObjectID) ([]*DialogueMessage, error) {
	opts := options.Find().SetSort(bson.D{{Key: "created_at", Value: 1}})

	cursor, err := r.messageColl.Find(ctx, bson.M{"dialogue_id": dialogueID}, opts)
	if err != nil {
		return nil, fmt.Errorf("find messages failed: %w", err)
	}
	defer cursor.Close(ctx)

	var messages []*DialogueMessage
	if err := cursor.All(ctx, &messages); err != nil {
		return nil, fmt.Errorf("decode messages failed: %w", err)
	}

	return messages, nil
}

func (r *MongoAgentRepository) GetRecentMessages(ctx context.Context, dialogueID primitive.ObjectID, limit int) ([]*DialogueMessage, error) {
	// 先按 created_at 倒序取最近 N 条，再反转为正序
	// 这样保证拿到的是最新的消息，且顺序正确
	opts := options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}}).
		SetLimit(int64(limit))

	cursor, err := r.messageColl.Find(ctx, bson.M{"dialogue_id": dialogueID}, opts)
	if err != nil {
		return nil, fmt.Errorf("find recent messages failed: %w", err)
	}
	defer cursor.Close(ctx)

	var messages []*DialogueMessage
	if err := cursor.All(ctx, &messages); err != nil {
		return nil, fmt.Errorf("decode recent messages failed: %w", err)
	}

	// 反转为时间正序
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	return messages, nil
}

// ---- 索引初始化 ----

func (r *MongoAgentRepository) EnsureIndexes(ctx context.Context) error {
	// dialogues 集合索引
	dialogueIndexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "user_id", Value: 1}, {Key: "updated_at", Value: -1}},
			Options: options.Index().SetName("idx_user_updated"),
		},
		{
			Keys: bson.D{
				{Key: "user_id", Value: 1},
				{Key: "summary_status", Value: 1},
				{Key: "summary_lease_until", Value: 1},
			},
			Options: options.Index().SetName("idx_user_summary_lease"),
		},
	}
	if _, err := r.dialogueColl.Indexes().CreateMany(ctx, dialogueIndexes); err != nil {
		return fmt.Errorf("create dialogue indexes failed: %w", err)
	}

	// dialogue_messages 集合索引
	messageIndexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "dialogue_id", Value: 1}, {Key: "created_at", Value: 1}},
			Options: options.Index().SetName("idx_dialogue_time"),
		},
	}
	if _, err := r.messageColl.Indexes().CreateMany(ctx, messageIndexes); err != nil {
		return fmt.Errorf("create message indexes failed: %w", err)
	}

	workflowIndexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "user_id", Value: 1}, {Key: "updated_at", Value: -1}},
			Options: options.Index().SetName("idx_workflow_user_updated"),
		},
	}
	if _, err := r.workflowColl.Indexes().CreateMany(ctx, workflowIndexes); err != nil {
		return fmt.Errorf("create workflow indexes failed: %w", err)
	}

	workflowRevisionIndexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "workflow_id", Value: 1}, {Key: "revision_number", Value: 1}},
			Options: options.Index().SetName("uniq_workflow_revision_number").SetUnique(true),
		},
		{
			Keys:    bson.D{{Key: "user_id", Value: 1}, {Key: "workflow_id", Value: 1}, {Key: "created_at", Value: -1}},
			Options: options.Index().SetName("idx_workflow_revision_user_created"),
		},
	}
	if _, err := r.workflowRevisionColl.Indexes().CreateMany(ctx, workflowRevisionIndexes); err != nil {
		return fmt.Errorf("create workflow revision indexes failed: %w", err)
	}

	runIndexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "user_id", Value: 1}, {Key: "started_at", Value: -1}},
			Options: options.Index().SetName("idx_workflow_run_user_started"),
		},
		{
			Keys:    bson.D{{Key: "workflow_id", Value: 1}, {Key: "started_at", Value: -1}},
			Options: options.Index().SetName("idx_workflow_run_workflow_started"),
		},
		{
			Keys:    bson.D{{Key: "user_id", Value: 1}, {Key: "status", Value: 1}, {Key: "suspended_at", Value: -1}},
			Options: options.Index().SetName("idx_workflow_run_user_status_suspended"),
		},
		{
			Keys:    bson.D{{Key: "user_id", Value: 1}, {Key: "workflow_id", Value: 1}, {Key: "started_at", Value: -1}},
			Options: options.Index().SetName("idx_workflow_run_user_workflow_started"),
		},
		{
			Keys: bson.D{
				{Key: "user_id", Value: 1}, {Key: "parent_run_id", Value: 1},
				{Key: "started_at", Value: 1}, {Key: "_id", Value: 1},
			},
			Options: options.Index().SetName("idx_workflow_run_user_parent_started"),
		},
	}
	if _, err := r.runColl.Indexes().CreateMany(ctx, runIndexes); err != nil {
		return fmt.Errorf("create workflow run indexes failed: %w", err)
	}

	workflowStateEventIndexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "run_id", Value: 1}, {Key: "sequence", Value: 1}},
			Options: options.Index().SetName("uniq_workflow_state_event_sequence").SetUnique(true),
		},
		{
			Keys:    bson.D{{Key: "user_id", Value: 1}, {Key: "run_id", Value: 1}, {Key: "sequence", Value: 1}},
			Options: options.Index().SetName("idx_workflow_state_event_user_sequence"),
		},
	}
	if _, err := r.workflowStateEventColl.Indexes().CreateMany(ctx, workflowStateEventIndexes); err != nil {
		return fmt.Errorf("create workflow state event indexes failed: %w", err)
	}

	workflowSnapshotIndexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "run_id", Value: 1}, {Key: "state_version", Value: 1}},
			Options: options.Index().SetName("uniq_workflow_state_snapshot_version").SetUnique(true),
		},
		{
			Keys:    bson.D{{Key: "user_id", Value: 1}, {Key: "run_id", Value: 1}, {Key: "state_version", Value: -1}},
			Options: options.Index().SetName("idx_workflow_state_snapshot_user_version"),
		},
	}
	if _, err := r.workflowSnapshotColl.Indexes().CreateMany(ctx, workflowSnapshotIndexes); err != nil {
		return fmt.Errorf("create workflow state snapshot indexes failed: %w", err)
	}

	workflowCompensationIndexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "run_id", Value: 1}, {Key: "sequence", Value: 1}},
			Options: options.Index().SetName("uniq_workflow_compensation_sequence").SetUnique(true),
		},
		{
			Keys: bson.D{
				{Key: "user_id", Value: 1}, {Key: "run_id", Value: 1},
				{Key: "status", Value: 1}, {Key: "sequence", Value: 1},
			},
			Options: options.Index().SetName("idx_workflow_compensation_user_status_sequence"),
		},
		{
			Keys:    bson.D{{Key: "status", Value: 1}, {Key: "lease_until", Value: 1}},
			Options: options.Index().SetName("idx_workflow_compensation_status_lease"),
		},
	}
	if _, err := r.workflowCompensationColl.Indexes().CreateMany(ctx, workflowCompensationIndexes); err != nil {
		return fmt.Errorf("create workflow compensation indexes failed: %w", err)
	}

	providerConfigIndexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "user_id", Value: 1}, {Key: "status", Value: 1}, {Key: "updated_at", Value: -1}},
			Options: options.Index().SetName("idx_provider_config_user_status_updated"),
		},
		{
			Keys:    bson.D{{Key: "user_id", Value: 1}, {Key: "kind", Value: 1}, {Key: "updated_at", Value: -1}},
			Options: options.Index().SetName("idx_provider_config_user_kind_updated"),
		},
	}
	if _, err := r.providerConfigColl.Indexes().CreateMany(ctx, providerConfigIndexes); err != nil {
		return fmt.Errorf("create provider config indexes failed: %w", err)
	}

	agentProjectIndexes := []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "members.user_id", Value: 1}, {Key: "updated_at", Value: -1}, {Key: "_id", Value: 1},
			},
			Options: options.Index().SetName("idx_agent_project_member_updated"),
		},
		{
			Keys:    bson.D{{Key: "owner_id", Value: 1}, {Key: "created_at", Value: -1}},
			Options: options.Index().SetName("idx_agent_project_owner_created"),
		},
	}
	if _, err := r.agentProjectColl.Indexes().CreateMany(ctx, agentProjectIndexes); err != nil {
		return fmt.Errorf("create Agent project indexes failed: %w", err)
	}

	mcpConnectionIndexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "server_id", Value: 1}},
			Options: options.Index().SetName("uniq_mcp_connection_server").SetUnique(true),
		},
		{
			Keys:    bson.D{{Key: "user_id", Value: 1}, {Key: "server_id", Value: 1}},
			Options: options.Index().SetName("uniq_mcp_connection_user_server").SetUnique(true),
		},
		{
			Keys:    bson.D{{Key: "user_id", Value: 1}, {Key: "status", Value: 1}, {Key: "updated_at", Value: -1}},
			Options: options.Index().SetName("idx_mcp_connection_user_status_updated"),
		},
		{
			Keys: bson.D{
				{Key: "project_id", Value: 1}, {Key: "status", Value: 1}, {Key: "updated_at", Value: -1},
			},
			Options: options.Index().SetName("idx_mcp_connection_project_status_updated"),
		},
		{
			Keys: bson.D{
				{Key: "status", Value: 1}, {Key: "next_health_check_at", Value: 1},
				{Key: "health_lease_until", Value: 1},
			},
			Options: options.Index().SetName("idx_mcp_connection_health_due_lease"),
		},
	}
	if _, err := r.mcpConnectionColl.Indexes().CreateMany(ctx, mcpConnectionIndexes); err != nil {
		return fmt.Errorf("create external MCP connection indexes failed: %w", err)
	}

	mcpSnapshotIndexes := []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "user_id", Value: 1}, {Key: "connection_id", Value: 1},
				{Key: "schema_hash", Value: 1},
			},
			Options: options.Index().SetName("uniq_mcp_snapshot_user_connection_hash").SetUnique(true),
		},
		{
			Keys:    bson.D{{Key: "user_id", Value: 1}, {Key: "connection_id", Value: 1}, {Key: "created_at", Value: -1}},
			Options: options.Index().SetName("idx_mcp_snapshot_user_connection_created"),
		},
	}
	if _, err := r.mcpToolSnapshotColl.Indexes().CreateMany(ctx, mcpSnapshotIndexes); err != nil {
		return fmt.Errorf("create external MCP snapshot indexes failed: %w", err)
	}

	approvalIndexes := []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "user_id", Value: 1}, {Key: "run_id", Value: 1}, {Key: "step_id", Value: 1},
				{Key: "tool_name", Value: 1}, {Key: "idempotency_key", Value: 1}, {Key: "input_digest", Value: 1},
			},
			Options: options.Index().SetName("uniq_tool_approval_invocation").SetUnique(true),
		},
		{
			Keys:    bson.D{{Key: "user_id", Value: 1}, {Key: "status", Value: 1}, {Key: "created_at", Value: -1}},
			Options: options.Index().SetName("idx_tool_approval_user_status_created"),
		},
		{
			Keys:    bson.D{{Key: "status", Value: 1}, {Key: "expires_at", Value: 1}},
			Options: options.Index().SetName("idx_tool_approval_status_expiry"),
		},
		{
			Keys:    bson.D{{Key: "status", Value: 1}, {Key: "execution_lease_until", Value: 1}},
			Options: options.Index().SetName("idx_tool_approval_status_lease"),
		},
		{
			Keys:    bson.D{{Key: "status", Value: 1}, {Key: "run_reconciled_at", Value: 1}},
			Options: options.Index().SetName("idx_tool_approval_run_reconcile"),
		},
	}
	if _, err := r.approvalColl.Indexes().CreateMany(ctx, approvalIndexes); err != nil {
		return fmt.Errorf("create tool approval indexes failed: %w", err)
	}

	executionIndexes := []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "user_id", Value: 1}, {Key: "tool_name", Value: 1}, {Key: "idempotency_key", Value: 1},
			},
			Options: options.Index().SetName("uniq_tool_execution_key").SetUnique(true),
		},
		{
			Keys:    bson.D{{Key: "status", Value: 1}, {Key: "lease_until", Value: 1}},
			Options: options.Index().SetName("idx_tool_execution_status_lease"),
		},
	}
	if _, err := r.executionColl.Indexes().CreateMany(ctx, executionIndexes); err != nil {
		return fmt.Errorf("create tool execution indexes failed: %w", err)
	}

	return nil
}

func (r *MongoAgentRepository) CreateWorkflow(ctx context.Context, workflow *WorkflowDefinition) error {
	return r.createWorkflowWithRevision(ctx, workflow)
}

func (r *MongoAgentRepository) UpdateWorkflow(ctx context.Context, workflow *WorkflowDefinition) error {
	return r.updateWorkflowWithRevision(ctx, workflow)
}

func (r *MongoAgentRepository) ListWorkflows(ctx context.Context, userID uint64, page, pageSize int) ([]*WorkflowDefinition, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 50 {
		pageSize = 20
	}

	filter := bson.M{"user_id": userID}
	total, err := r.workflowColl.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("count workflows failed: %w", err)
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "updated_at", Value: -1}}).
		SetSkip(int64((page - 1) * pageSize)).
		SetLimit(int64(pageSize))
	cursor, err := r.workflowColl.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("find workflows failed: %w", err)
	}
	defer cursor.Close(ctx)

	var workflows []*WorkflowDefinition
	if err := cursor.All(ctx, &workflows); err != nil {
		return nil, 0, fmt.Errorf("decode workflows failed: %w", err)
	}
	return workflows, total, nil
}

func (r *MongoAgentRepository) GetWorkflow(ctx context.Context, workflowID primitive.ObjectID, userID uint64) (*WorkflowDefinition, error) {
	var workflow WorkflowDefinition
	err := r.workflowColl.FindOne(ctx, bson.M{"_id": workflowID, "user_id": userID}).Decode(&workflow)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("workflow not found: %s", workflowID.Hex())
		}
		return nil, fmt.Errorf("find workflow failed: %w", err)
	}
	return &workflow, nil
}

func (r *MongoAgentRepository) CreateWorkflowRun(ctx context.Context, run *WorkflowRunRecord) error {
	now := time.Now()
	if run.ID.IsZero() {
		run.ID = primitive.NewObjectID()
	}
	if run.StartedAt.IsZero() {
		run.StartedAt = now
	}
	if run.Revision <= 0 {
		run.Revision = 1
	}

	if _, err := r.runColl.InsertOne(ctx, run); err != nil {
		return fmt.Errorf("insert workflow run failed: %w", err)
	}
	return nil
}

func (r *MongoAgentRepository) UpdateWorkflowRun(ctx context.Context, run *WorkflowRunRecord) error {
	res, err := r.runColl.UpdateOne(ctx,
		bson.M{"_id": run.ID, "user_id": run.UserID},
		bson.M{"$set": bson.M{
			"status":                    run.Status,
			"output_json":               run.OutputJSON,
			"checkpoint_json":           run.CheckpointJSON,
			"waiting_node_id":           run.WaitingNodeID,
			"approval_request_id":       run.ApprovalRequestID,
			"resume_token":              run.ResumeToken,
			"resume_token_hash":         run.ResumeTokenHash,
			"resume_attempt_id":         run.ResumeAttemptID,
			"resume_grant_issued_at":    run.ResumeGrantIssuedAt,
			"resume_grant_expires_at":   run.ResumeGrantExpiresAt,
			"state_version":             run.StateVersion,
			"node_executions":           run.NodeExecutions,
			"input_tokens":              run.InputTokens,
			"output_tokens":             run.OutputTokens,
			"total_tokens":              run.TotalTokens,
			"usage_estimated":           run.UsageEstimated,
			"estimated_cost_micros":     run.EstimatedCostMicros,
			"cost_estimated":            run.CostEstimated,
			"pricing_version":           run.PricingVersion,
			"max_steps":                 run.MaxSteps,
			"max_total_tokens":          run.MaxTotalTokens,
			"max_estimated_cost_micros": run.MaxEstimatedCostMicros,
			"accounting_version":        run.AccountingVersion,
			"error_message":             run.ErrorMessage,
			"suspended_at":              run.SuspendedAt,
			"finished_at":               run.FinishedAt,
		}, "$inc": bson.M{"revision": 1}},
	)
	if err != nil {
		return fmt.Errorf("update workflow run failed: %w", err)
	}
	if res.MatchedCount == 0 {
		return fmt.Errorf("workflow run not found or not owned by user")
	}
	run.Revision++
	return nil
}

func (r *MongoAgentRepository) GetWorkflowRun(ctx context.Context, runID primitive.ObjectID, userID uint64) (*WorkflowRunRecord, error) {
	var run WorkflowRunRecord
	err := r.runColl.FindOne(ctx, bson.M{"_id": runID, "user_id": userID}).Decode(&run)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("workflow run not found: %s", runID.Hex())
		}
		return nil, fmt.Errorf("find workflow run failed: %w", err)
	}
	return &run, nil
}
