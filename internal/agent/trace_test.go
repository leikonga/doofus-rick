package agent

import (
	"context"
	"testing"

	"github.com/leikonga/doofus-rick/internal/pgtest"
	"github.com/leikonga/doofus-rick/internal/tracer"
)

type usageRow struct {
	ModelName    string
	InputTokens  int64
	OutputTokens int64
}

func usageRows(t *testing.T) []usageRow {
	t.Helper()
	return pgtest.Query[usageRow](t, "SELECT model_name, input_tokens, output_tokens FROM token_usages")
}

func TestFinishTraceSavesServedModel(t *testing.T) {
	a := &Agent{store: pgtest.Store(t)}
	rec := tracer.New(nil).Start("chan", "user", "sys", "prompt")
	rec.AddTokens(11, 5)
	a.finishTrace(context.Background(), rec, "hi", false, nil, "served/model")
	a.Wait()

	rows := usageRows(t)
	if len(rows) != 1 {
		t.Fatalf("expected 1 token usage row, got %d", len(rows))
	}
	if rows[0] != (usageRow{ModelName: "served/model", InputTokens: 11, OutputTokens: 5}) {
		t.Fatalf("unexpected row: %+v", rows[0])
	}
}

func TestFinishTraceSavesAfterCancellation(t *testing.T) {
	a := &Agent{store: pgtest.Store(t)}
	rec := tracer.New(nil).Start("chan", "user", "sys", "prompt")
	rec.AddTokens(3, 2)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a.finishTrace(ctx, rec, "hi", false, nil, "served/model")
	a.Wait()

	if rows := usageRows(t); len(rows) != 1 {
		t.Fatalf("expected 1 token usage row after cancelled ctx, got %d", len(rows))
	}
}

func TestFinishTraceZeroTokensWritesNothing(t *testing.T) {
	a := &Agent{store: pgtest.Store(t)}
	tr := tracer.New(nil)
	a.finishTrace(context.Background(), tr.Start("chan", "user", "sys", "prompt"), "hi", false, nil, "served/model")
	a.Wait()

	if len(tr.RecentSuccesses()) == 0 {
		t.Fatal("trace was not finished")
	}
	if rows := usageRows(t); len(rows) != 0 {
		t.Fatalf("expected no rows, got %+v", rows)
	}
}
