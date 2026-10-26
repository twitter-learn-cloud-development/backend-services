package repository

import (
	"context"
	"errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strings"
	"time"
	"twitter-clone/internal/domain"
)

type SQLPersonaStore struct{ DB *gorm.DB }

func (s SQLPersonaStore) Get(ctx context.Context, user uint64) (string, error) {
	if user == 0 || s.DB == nil {
		return "", errors.New("persona store unavailable")
	}
	bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var row domain.AgentPersona
	err := s.DB.WithContext(bounded).Select("user_id", "persona").Where("user_id = ?", user).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	if err != nil {
		return "", errors.New("persona read unavailable")
	}
	if row.UserID != user || len(row.Persona) > 65536 {
		return "", errors.New("persona identity or size invalid")
	}
	return row.Persona, nil
}
func (s SQLPersonaStore) Put(ctx context.Context, user uint64, value string) error {
	if user == 0 || s.DB == nil || len(value) > 65536 {
		return errors.New("invalid persona write")
	}
	bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	row := domain.AgentPersona{UserID: user, Persona: strings.TrimSpace(value), UpdatedAt: time.Now().Unix()}
	if err := s.DB.WithContext(bounded).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}}, DoUpdates: clause.AssignmentColumns([]string{"persona", "updated_at"})}).Create(&row).Error; err != nil {
		return errors.New("persona write unavailable")
	}
	return nil
}
