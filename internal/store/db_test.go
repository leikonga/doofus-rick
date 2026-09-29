package store_test

import (
	"context"
	"testing"

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
