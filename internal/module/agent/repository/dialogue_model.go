package repository

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ========================== MongoDB 集合名 ==========================

const (
	CollectionDialogues = "dialogues"
	CollectionMessages  = "dialogue_messages"
)

// ========================== 对话模式枚举 ==========================

type DialogueMode int32

const (
	ModeChat     DialogueMode = 1 // 模式一：直接 AI 对话
	ModeConsult  DialogueMode = 2 // 模式二：语义搜索推文（RAG）
	ModeAssist   DialogueMode = 3 // 模式三：AI 辅助写推文
	ModeMulti    DialogueMode = 4 // 模式四：多 Agent 协作写推文
)

// ========================== 数据模型 ==========================

// Dialogue 对话会话
// 对应 MongoDB 集合：dialogues
// 一个用户可以有多个对话会话，每个会话包含多条消息
type Dialogue struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	UserID    uint64             `bson:"user_id"       json:"user_id"`
	Title     string             `bson:"title"         json:"title"`      // 对话标题，取自首条用户消息的前30字
	Mode      DialogueMode       `bson:"mode"          json:"mode"`       // 对话模式
	CreatedAt time.Time          `bson:"created_at"    json:"created_at"`
	UpdatedAt time.Time          `bson:"updated_at"    json:"updated_at"`
}

// MessageRole 消息角色
type MessageRole string

const (
	RoleUser      MessageRole = "user"
	RoleAssistant MessageRole = "assistant"
	RoleSystem    MessageRole = "system"
	RoleTool      MessageRole = "tool"
)

// DialogueMessage 对话中的单条消息
// 对应 MongoDB 集合：dialogue_messages
// 通过 dialogue_id 关联到 Dialogue
type DialogueMessage struct {
	ID         primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	DialogueID primitive.ObjectID `bson:"dialogue_id"   json:"dialogue_id"`
	UserID     uint64             `bson:"user_id"       json:"user_id"`
	Role       MessageRole        `bson:"role"          json:"role"`    // user / assistant / system / tool
	Content    string             `bson:"content"       json:"content"`
	ToolName   string             `bson:"tool_name,omitempty"   json:"tool_name,omitempty"`   // 仅 role=tool 时有值
	ToolCallID string             `bson:"tool_call_id,omitempty" json:"tool_call_id,omitempty"` // 仅 role=tool 时有值
	Metadata   map[string]any     `bson:"metadata,omitempty"    json:"metadata,omitempty"`    // 扩展字段：存推文结果等附加数据
	CreatedAt  time.Time          `bson:"created_at"    json:"created_at"`
}

// GenerateTitle 根据用户首条消息生成对话标题
// 截取前 30 个 rune，超出部分用 ... 替代
func GenerateTitle(content string) string {
	runes := []rune(content)
	if len(runes) > 30 {
		return string(runes[:30]) + "..."
	}
	return string(runes)
}
