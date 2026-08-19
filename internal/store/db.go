package store

import (
	"database/sql"
	"embed"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/leikonga/doofus-rick/internal/config"
	"github.com/pressly/goose/v3"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

type Store struct {
	db *gorm.DB
}

func MustInit(c *config.Config) *Store {
	s, err := Init(c)
	if err != nil {
		panic(err)
	}
	return s
}

func Init(c *config.Config) (*Store, error) {
	var db *gorm.DB
	var err error

	gormConfig := &gorm.Config{
		Logger: logger.New(&slogLogger{slog.Default()}, logger.Config{
			SlowThreshold:             200 * time.Millisecond,
			LogLevel:                  logger.Warn,
			IgnoreRecordNotFoundError: true,
		}),
	}

	switch c.DBDriver {
	case "postgres":
		dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
			c.DBHost, c.DBUser, c.DBPass, c.DBName, c.DBPort)
		db, err = gorm.Open(postgres.Open(dsn), gormConfig)
	default:
		db, err = gorm.Open(sqlite.Open(c.DBPath), gormConfig)
	}

	if err != nil {
		return nil, err
	}

	if err = runMigrations(db, c.DBDriver); err != nil {
		return nil, err
	}

	if err = db.AutoMigrate(&Quote{}, &Reminder{}, &TokenUsage{}, &FailureTrace{}, &Message{}, &ForgottenAuthor{}, &BackfillState{}, &BackfillChannel{}, &Chunk{}, &ChunkEmbedding{}, &UserAffinity{}, &AmbientLog{}, &AmbientState{}); err != nil {
		return nil, err
	}

	// One-time migration: legacy rows used a "timestamp" column instead of created_at.
	db.Exec(`UPDATE quotes SET created_at = "timestamp" WHERE (created_at IS NULL OR created_at = '0001-01-01 00:00:00') AND "timestamp" IS NOT NULL`)

	return &Store{db: db}, nil
}

func (s *Store) DB() *gorm.DB {
	return s.db
}

// The goose migrations are postgres-only (pgvector, tsvector); AutoMigrate
// below covers the equivalent schema for the sqlite fallback driver.
func runMigrations(db *gorm.DB, driver string) error {
	if driver != "postgres" {
		return nil
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return RunMigrations(sqlDB)
}

// RunMigrations applies the embedded goose migrations to db. Exported for
// internal/selfcode to verify a pending migration against a scratch database.
func RunMigrations(db *sql.DB) error {
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	goose.SetLogger(&slogLogger{slog.Default()})
	goose.SetBaseFS(migrationsFS)
	defer goose.SetBaseFS(nil)
	return goose.Up(db, "migrations")
}

// RunMigrationsDSN opens a postgres connection to dsn and applies the
// embedded migrations to it, closing the connection afterward.
func RunMigrationsDSN(dsn string) error {
	gdb, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		return err
	}
	defer func() { _ = sqlDB.Close() }()
	return RunMigrations(sqlDB)
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
