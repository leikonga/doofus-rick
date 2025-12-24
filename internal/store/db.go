package store

import (
	"fmt"

	"github.com/leikonga/doofus-rick/internal/config"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type Store struct {
	db *gorm.DB
}

func MustInit(c *config.Config) *Store {
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
		c.DBHost, c.DBUser, c.DBPass, c.DBName, c.DBPort)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		panic(err)
	}

	err = db.AutoMigrate(&Quote{})
	if err != nil {
		panic(err)
	}

	return &Store{db: db}
}
