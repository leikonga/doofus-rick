package affinity

import (
	"context"
	"testing"

	"github.com/leikonga/doofus-rick/internal/pgtest"
)

func TestLedgerUpdateCreatesThenAccumulates(t *testing.T) {
	l := New(Config{Baseline: -20}, pgtest.Store(t))
	ctx := context.Background()

	if err := l.Update(ctx, 1, "first", 5); err != nil {
		t.Fatalf("first Update: %v", err)
	}
	if err := l.Update(ctx, 1, "second", 3); err != nil {
		t.Fatalf("second Update: %v", err)
	}

	got, err := l.Get(ctx, 1)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Score != -12 || got.LastReason != "second" {
		t.Errorf("got score %d reason %q, want -12 \"second\"", got.Score, got.LastReason)
	}
}
