package domain

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

type OutboxPublication struct {
	EventID        uint64 `gorm:"primaryKey;autoIncrement:false;column:event_id"`
	EventType      string `gorm:"type:varchar(100);not null;column:event_type"`
	PayloadDigest  string `gorm:"type:char(64);not null;column:payload_digest"`
	ConfirmedAt    int64  `gorm:"not null;column:confirmed_at;index:idx_outbox_publication_confirmed"`
	Operation      string `gorm:"type:varchar(64);not null;default:'';column:operation"`
	OperatorDigest string `gorm:"type:varchar(64);not null;default:'';column:operator_digest"`
	ReasonDigest   string `gorm:"type:varchar(64);not null;default:'';column:reason_digest"`
}

func (OutboxPublication) TableName() string { return "outbox_publications" }

type OutboxPublicationRepository interface {
	RecordPublication(context.Context, OutboxPublication) error
	ReadPublication(context.Context, uint64) (*OutboxPublication, error)
	ReadEvents(context.Context, []uint64) ([]OutboxEvent, error)
}

// The JSON identity must preserve large integer tokens and object key order
// must not depend on binlog/SQL formatting.
func OutboxPayloadDigest(payload string) (string, error) {
	var value interface{}
	decoder := json.NewDecoder(strings.NewReader(payload))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return "", errors.New("invalid publication JSON")
	}
	var trailing interface{}
	if decoder.Decode(&trailing) != io.EOF {
		return "", errors.New("multiple publication JSON values")
	}
	data, err := json.Marshal(value)
	if err != nil {
		return "", errors.New("invalid publication JSON")
	}
	d := sha256.Sum256(data)
	return fmt.Sprintf("%x", d[:]), nil
}
