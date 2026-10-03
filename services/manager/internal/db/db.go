package db

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/glebarez/sqlite"
	"github.com/supabase-manager/manager/config"
	"github.com/supabase-manager/manager/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func Open(cfg *config.Config) (*gorm.DB, error) {
	var dialector gorm.Dialector
	switch cfg.DBDriver {
	case "sqlite":
		if dir := filepath.Dir(cfg.DBDSN); dir != "." {
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return nil, err
			}
		}
		dialector = sqlite.Open(cfg.DBDSN + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	case "postgres":
		dialector = postgres.Open(cfg.DBDSN)
	default:
		return nil, fmt.Errorf("unsupported DB_DRIVER %q (use sqlite or postgres)", cfg.DBDriver)
	}

	gdb, err := gorm.Open(dialector, &gorm.Config{Logger: logger.Default.LogMode(logger.Warn)})
	if err != nil {
		return nil, err
	}
	if cfg.DBDriver == "sqlite" {
		if sqlDB, err := gdb.DB(); err == nil {
			sqlDB.SetMaxOpenConns(1)
		}
	}
	if err := gdb.AutoMigrate(models.All()...); err != nil {
		return nil, fmt.Errorf("auto migrate: %w", err)
	}
	return gdb, nil
}
