package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
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
	if err := db.WithContext(ctx).AutoMigrate(&Quote{}, &TokenUsage{}, &FailureTrace{}, &Message{}, &ForgottenAuthor{}, &BackfillState{}, &BackfillChannel{}, &Chunk{}, &ChunkEmbedding{}, &UserAffinity{}, &PersonProfile{}, &AmbientLog{}, &AmbientState{}); err != nil {
		return nil, fmt.Errorf("automigrate: %w", err)
	}

	return s, nil
}

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

func RunMigrationsDSN(ctx context.Context, dsn string) error {
	gdb, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		return fmt.Errorf("get sql db: %w", err)
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
