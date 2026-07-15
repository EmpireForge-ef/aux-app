// Package db opens the GORM/PostgreSQL connection shared by the persistence
// stores and runs their schema migrations. Each store owns its own model and
// AutoMigrate call; this package just provides the connection.
package db

import (
	"fmt"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Open connects to PostgreSQL via GORM using a libpq DSN or URL, e.g.
// "postgres://user:pass@host:5432/aux?sslmode=disable". The driver is pure Go
// (pgx), so the static CGO_ENABLED=0 build is unaffected.
func Open(dsn string) (*gorm.DB, error) {
	if dsn == "" {
		return nil, fmt.Errorf("no database URL configured — set AUX_DATABASE_URL to a PostgreSQL connection string")
	}
	gdb, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger:                                   logger.Default.LogMode(logger.Warn),
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetMaxIdleConns(2)
	sqlDB.SetConnMaxIdleTime(5 * time.Minute)
	return gdb, nil
}
