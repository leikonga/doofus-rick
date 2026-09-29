package pgtest

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/leikonga/doofus-rick/internal/store"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const image = "pgvector/pgvector:pg18"

var (
	once    sync.Once
	shared  *store.Store
	gdb     *gorm.DB
	initErr error
)

func Store(t *testing.T) *store.Store {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping postgres integration test in short mode")
	}
	testcontainers.SkipIfProviderIsNotHealthy(t)

	once.Do(func() {
		shared, gdb, initErr = start()
	})
	if initErr != nil {
		t.Fatalf("start postgres: %v", initErr)
	}
	truncateAll(t)
	return shared
}

func handle(t *testing.T) *gorm.DB {
	t.Helper()
	if gdb == nil {
		t.Fatal("pgtest: Store(t) must be called before using pgtest helpers")
	}
	return gdb
}

func Exec(t *testing.T, query string, args ...any) {
	t.Helper()
	if err := handle(t).Exec(query, args...).Error; err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

func start() (*store.Store, *gorm.DB, error) {
	ctx := context.Background()
	ctr, err := tcpostgres.Run(ctx, image,
		tcpostgres.WithDatabase("rick"),
		tcpostgres.WithUsername("rick"),
		tcpostgres.WithPassword("rick"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("run container: %w", err)
	}
	host, err := ctr.Host(ctx)
	if err != nil {
		return nil, nil, err
	}
	port, err := ctr.MappedPort(ctx, "5432/tcp")
	if err != nil {
		return nil, nil, err
	}
	dsn := fmt.Sprintf("host=%s user=rick password=rick dbname=rick port=%s sslmode=disable", host, port.Port())
	s, err := store.Open(ctx, dsn)
	if err != nil {
		return nil, nil, err
	}
	g, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, nil, fmt.Errorf("open test handle: %w", err)
	}
	return s, g, nil
}

func truncateAll(t *testing.T) {
	t.Helper()
	var tables []string
	err := handle(t).Raw(`SELECT table_name FROM information_schema.tables
		WHERE table_schema = 'public' AND table_type = 'BASE TABLE' AND table_name <> 'goose_db_version'`).Scan(&tables).Error
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	if len(tables) == 0 {
		return
	}
	quoted := make([]string, len(tables))
	for i, name := range tables {
		quoted[i] = `"` + name + `"`
	}
	if err := handle(t).Exec("TRUNCATE " + strings.Join(quoted, ", ") + " RESTART IDENTITY CASCADE").Error; err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

func Query[T any](t *testing.T, query string, args ...any) []T {
	t.Helper()
	var rows []T
	if err := handle(t).Raw(query, args...).Scan(&rows).Error; err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	return rows
}

func SQLDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := handle(t).DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	return db
}
