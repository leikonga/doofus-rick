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

	return &Store{db: db}
}
