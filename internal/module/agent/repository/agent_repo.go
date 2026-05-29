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
}

// ========================== MongoDB 实现 ==========================

// MongoAgentRepository MongoDB 实现的对话仓储
type MongoAgentRepository struct {
	dialogueColl *mongo.Collection
	messageColl  *mongo.Collection
}

// NewMongoAgentRepository 创建 MongoDB 对话仓储
func NewMongoAgentRepository(db *mongo.Database) *MongoAgentRepository {
	return &MongoAgentRepository{
		dialogueColl: db.Collection(CollectionDialogues),
		messageColl:  db.Collection(CollectionMessages),
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

	return nil
}
