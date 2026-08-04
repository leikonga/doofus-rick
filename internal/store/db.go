package store

import (
	"embed"
	"fmt"
	"log/slog"
	"os"

	"github.com/leikonga/doofus-rick/internal/config"
	"github.com/pressly/goose/v3"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

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

	if err = runMigrations(db, c.DBDriver); err != nil {
		panic(err)
	}

	if err = db.AutoMigrate(&Quote{}, &Reminder{}, &TokenUsage{}, &FailureTrace{}, &Message{}, &ForgottenAuthor{}); err != nil {
		panic(err)
	}

	// One-time migration: legacy rows used a "timestamp" column instead of created_at.
	db.Exec(`UPDATE quotes SET created_at = "timestamp" WHERE (created_at IS NULL OR created_at = '0001-01-01 00:00:00') AND "timestamp" IS NOT NULL`)

	return &Store{db: db}
}

func runMigrations(db *gorm.DB, driver string) error {
	gooseLogger := &slogLogger{slog.Default()}

	var dialect string
	switch driver {
	case "postgres":
		dialect = "postgres"
	default:
		dialect = "sqlite"
	}

	if err := goose.SetDialect(dialect); err != nil {
		return err
	}

	goose.SetLogger(gooseLogger)
	goose.SetBaseFS(migrationsFS)

	migrationsPath := "migrations"
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	if err := goose.Up(sqlDB, migrationsPath); err != nil {
		return err
	}

	goose.SetBaseFS(nil)

	return nil
}

type slogLogger struct {
	*slog.Logger
}

func (l *slogLogger) Printf(format string, v ...any) {
	l.Info(fmt.Sprintf(format, v...))
}

func (l *slogLogger) Fatalf(format string, v ...any) {
	l.Error(fmt.Sprintf(format, v...))
	os.Exit(1)
}
