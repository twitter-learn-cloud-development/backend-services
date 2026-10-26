package domain

import "context"

// AgentPersona is owned by Agent; the table is not a Tweet/User extension.
type AgentPersona struct {
	UserID    uint64 `gorm:"primaryKey;column:user_id"`
	Persona   string `gorm:"column:persona;type:text"`
	UpdatedAt int64  `gorm:"column:updated_at"`
}

func (AgentPersona) TableName() string { return "agent_personas" }

type AgentPersonaStore interface {
	Get(context.Context, uint64) (string, error)
	Put(context.Context, uint64, string) error
}
