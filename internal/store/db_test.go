package store_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/leikonga/doofus-rick/internal/pgtest"
	"github.com/leikonga/doofus-rick/internal/store"
)

func TestRunMigrationsIdempotent(t *testing.T) {
	pgtest.Store(t)
	sqlDB := pgtest.SQLDB(t)
	countVersions := func() int {
		var n int
		if err := sqlDB.QueryRow("SELECT count(*) FROM goose_db_version").Scan(&n); err != nil {
			t.Fatalf("count versions: %v", err)
		}
		return n
	}

	before := countVersions()
	if before == 0 {
		t.Fatal("no goose versions recorded")
	}
	if err := store.RunMigrations(context.Background(), sqlDB); err != nil {
		t.Fatalf("second RunMigrations: %v", err)
	}
	if after := countVersions(); after != before {
		t.Errorf("goose_db_version rows %d -> %d", before, after)
	}
}

func TestLegacyQuoteTimestampMigration(t *testing.T) {
	pgtest.Store(t)
	migration, err := os.ReadFile("migrations/008_legacy_quote_timestamp.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}

	pgtest.Exec(t, string(migration))

	pgtest.Exec(t, `ALTER TABLE quotes ADD COLUMN "timestamp" TIMESTAMPTZ`)
	t.Cleanup(func() { pgtest.Exec(t, `ALTER TABLE quotes DROP COLUMN "timestamp"`) })
	legacy := time.Date(2021, 5, 1, 12, 0, 0, 0, time.UTC)
	pgtest.Exec(t, `INSERT INTO quotes (content, creator, votes, created_at, "timestamp") VALUES ('q', 'c', 0, '0001-01-01 00:00:00', ?)`, legacy)

	pgtest.Exec(t, string(migration))

	got := pgtest.Query[time.Time](t, "SELECT created_at FROM quotes")
	if len(got) != 1 || !got[0].Equal(legacy) {
		t.Errorf("created_at = %v, want %v", got, legacy)
	}
}

func TestSingularTablesDropped(t *testing.T) {
	pgtest.Store(t)
	left := pgtest.Query[string](t, `SELECT table_name FROM information_schema.tables
		WHERE table_schema = current_schema() AND table_name IN ('backfill_state', 'backfill_channel', 'user_affinity', 'ambient_log')`)
	if len(left) != 0 {
		t.Errorf("singular tables still present: %v", left)
	}
}
