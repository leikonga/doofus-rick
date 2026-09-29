package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"time"

	"github.com/pressly/goose/v3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

type Store struct {
	db *gorm.DB
}

func Open(ctx context.Context, dsn string) (*Store, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.New(&slogLogger{slog.Default()}, logger.Config{
			SlowThreshold:             200 * time.Millisecond,
			LogLevel:                  logger.Warn,
			IgnoreRecordNotFoundError: true,
		}),
	})
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	s := &Store{db: db}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := RunMigrations(ctx, sqlDB); err != nil {
		return nil, fmt.Errorf("run migrations: %w", err)
	}
	if err := db.WithContext(ctx).AutoMigrate(&Quote{}, &TokenUsage{}, &FailureTrace{}, &Message{}, &ForgottenAuthor{}, &BackfillState{}, &BackfillChannel{}, &Chunk{}, &ChunkEmbedding{}, &UserAffinity{}, &AmbientLog{}, &AmbientState{}); err != nil {
		return nil, fmt.Errorf("automigrate: %w", err)
	}

	// One-time migration: legacy rows used a "timestamp" column instead of created_at.
	db.WithContext(ctx).Exec(`UPDATE quotes SET created_at = "timestamp" WHERE (created_at IS NULL OR created_at = '0001-01-01 00:00:00') AND "timestamp" IS NOT NULL`)

	return s, nil
}

// RunMigrations applies the embedded goose migrations to db. Exported for
// internal/selfcode to verify a pending migration against a scratch database.
func RunMigrations(ctx context.Context, db *sql.DB) error {
	fsys, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("sub migrations fs: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, fsys, goose.WithSlog(slog.Default()))
	if err != nil {
		return fmt.Errorf("new goose provider: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	return nil
}

// RunMigrationsDSN opens a postgres connection to dsn and applies the
// embedded migrations to it, closing the connection afterward.
func RunMigrationsDSN(ctx context.Context, dsn string) error {
	gdb, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		return err
	}
	defer func() { _ = sqlDB.Close() }()
	return RunMigrations(ctx, sqlDB)
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
