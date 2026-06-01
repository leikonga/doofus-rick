package store

import (
	"fmt"

	"github.com/leikonga/doofus-rick/internal/config"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type Store struct {
	db *gorm.DB
}

func MustInit(c *config.Config) *Store {
	var db *gorm.DB
	var err error

	switch c.DBDriver {
	case "postgres":
		dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
			c.DBHost, c.DBUser, c.DBPass, c.DBName, c.DBPort)
		db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
	default:
		db, err = gorm.Open(sqlite.Open(c.DBPath), &gorm.Config{})
	}

	if err != nil {
		panic(err)
	}

	if err = db.AutoMigrate(&Quote{}, &Memory{}, &Reminder{}, &TokenUsage{}, &FailureTrace{}); err != nil {
		panic(err)
	}

	// One-time migration: legacy rows used a "timestamp" column instead of created_at.
	db.Exec(`UPDATE quotes SET created_at = "timestamp" WHERE (created_at IS NULL OR created_at = '0001-01-01 00:00:00') AND "timestamp" IS NOT NULL`)

	return &Store{db: db}
}
