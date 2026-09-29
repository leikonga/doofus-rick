package agent

import (
	"testing"
	"time"

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
	a.finishTrace(rec, "hi", false, nil, "served/model")

	deadline := time.Now().Add(5 * time.Second)
	for {
		rows := usageRows(t)
		if len(rows) == 1 {
			if rows[0] != (usageRow{ModelName: "served/model", InputTokens: 11, OutputTokens: 5}) {
				t.Fatalf("unexpected row: %+v", rows[0])
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("no token usage row, got %d", len(rows))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestFinishTraceZeroTokensWritesNothing(t *testing.T) {
	a := &Agent{store: pgtest.Store(t)}
	tr := tracer.New(nil)
	a.finishTrace(tr.Start("chan", "user", "sys", "prompt"), "hi", false, nil, "served/model")

	deadline := time.Now().Add(2 * time.Second)
	for len(tr.RecentSuccesses()) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("finishTrace goroutine did not finish")
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	if rows := usageRows(t); len(rows) != 0 {
		t.Fatalf("expected no rows, got %+v", rows)
	}
}
