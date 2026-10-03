// Package settings stores instance-wide settings as JSON values in the database.
package settings

import (
	"encoding/json"

	"github.com/supabase-manager/manager/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	KeyCLI             = "cli"
	KeyNetworkDefaults = "network_defaults"
	KeyStorageDefaults = "storage_defaults"
	KeySMTPDefaults    = "smtp_defaults"
)

type Store struct{ db *gorm.DB }

func New(db *gorm.DB) *Store { return &Store{db: db} }

// Get decodes the value of key into out and reports whether it was set.
func (s *Store) Get(key string, out any) (bool, error) {
	var rows []models.Setting
	if err := s.db.Where("key = ?", key).Limit(1).Find(&rows).Error; err != nil {
		return false, err
	}
	if len(rows) == 0 {
		return false, nil
	}
	return true, json.Unmarshal([]byte(rows[0].Value), out)
}

func (s *Store) Put(key string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.db.Clauses(clause.OnConflict{UpdateAll: true}).Create(&models.Setting{Key: key, Value: string(b)}).Error
}
