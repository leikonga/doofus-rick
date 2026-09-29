package pgtest

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/leikonga/doofus-rick/internal/config"
	"github.com/leikonga/doofus-rick/internal/store"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

const image = "pgvector/pgvector:pg18"

var (
	once    sync.Once
	shared  *store.Store
	initErr error
)

func Store(t *testing.T) *store.Store {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping postgres integration test in short mode")
	}
	testcontainers.SkipIfProviderIsNotHealthy(t)

	once.Do(func() {
		shared, initErr = start()
	})
	if initErr != nil {
		t.Fatalf("start postgres: %v", initErr)
	}
	truncateAll(t, shared)
	return shared
}

func Exec(t *testing.T, s *store.Store, query string, args ...any) {
	t.Helper()
	if err := s.DB().Exec(query, args...).Error; err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

func start() (*store.Store, error) {
	ctx := context.Background()
	ctr, err := tcpostgres.Run(ctx, image,
		tcpostgres.WithDatabase("rick"),
		tcpostgres.WithUsername("rick"),
		tcpostgres.WithPassword("rick"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		return nil, fmt.Errorf("run container: %w", err)
	}
	host, err := ctr.Host(ctx)
	if err != nil {
		return nil, err
	}
	port, err := ctr.MappedPort(ctx, "5432/tcp")
	if err != nil {
		return nil, err
	}
	return store.Init(&config.Config{
		DBHost: host,
		DBPort: port.Port(),
		DBUser: "rick",
		DBPass: "rick",
		DBName: "rick",
	})
}

func truncateAll(t *testing.T, s *store.Store) {
	t.Helper()
	var tables []string
	err := s.DB().Raw(`SELECT table_name FROM information_schema.tables
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
	if err := s.DB().Exec("TRUNCATE " + strings.Join(quoted, ", ") + " RESTART IDENTITY CASCADE").Error; err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

func Query[T any](t *testing.T, s *store.Store, query string, args ...any) []T {
	t.Helper()
	var rows []T
	if err := s.DB().Raw(query, args...).Scan(&rows).Error; err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	return rows
}

func SQLDB(t *testing.T, s *store.Store) *sql.DB {
	t.Helper()
	db, err := s.DB().DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	return db
}
