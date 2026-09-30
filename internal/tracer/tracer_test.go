package tracer

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func newTracer() (*Tracer, *[]*Entry) {
	var persisted []*Entry
	return New(func(e *Entry) { persisted = append(persisted, e) }), &persisted
}

func TestFinishSuccessGoesToRing(t *testing.T) {
	tr, persisted := newTracer()
	tr.Start("c", "u", "sys", "p").Finish("ok", false, nil)
	if len(*persisted) != 0 {
		t.Fatalf("persisted %d, want 0", len(*persisted))
	}
	if got := tr.RecentSuccesses(); len(got) != 1 || got[0].Response != "ok" || got[0].Failed {
		t.Fatalf("unexpected successes: %+v", got)
	}
}

func TestFinishErrorPersists(t *testing.T) {
	tr, persisted := newTracer()
	tr.Start("c", "u", "sys", "p").Finish("", false, errors.New("boom"))
	if len(*persisted) != 1 || !(*persisted)[0].Failed || (*persisted)[0].Err != "boom" {
		t.Fatalf("unexpected persisted: %+v", *persisted)
	}
	if len(tr.RecentSuccesses()) != 0 {
		t.Fatal("failure must not enter ring")
	}
}

func TestFinishDeclinePersists(t *testing.T) {
	tr, persisted := newTracer()
	tr.Start("c", "u", "sys", "p").Finish("no", true, nil)
	if len(*persisted) != 1 || !(*persisted)[0].Decline {
		t.Fatalf("unexpected persisted: %+v", *persisted)
	}
	if len(tr.RecentSuccesses()) != 0 {
		t.Fatal("decline must not enter ring")
	}
}

func TestFinishCanceledSkipsPersistAndRing(t *testing.T) {
	cases := map[string]error{
		"bare":    context.Canceled,
		"wrapped": fmt.Errorf("turn aborted: %w", context.Canceled),
	}
	for name, err := range cases {
		t.Run(name, func(t *testing.T) {
			tr, persisted := newTracer()
			rec := tr.Start("c", "u", "sys", "p")
			rec.AddTokens(7, 3)
			e := rec.Finish("", false, err)
			if len(*persisted) != 0 || len(tr.RecentSuccesses()) != 0 {
				t.Fatal("canceled trace must go nowhere")
			}
			if e == nil || e.InputTokens != 7 || e.OutputTokens != 3 || !e.Failed {
				t.Fatalf("unexpected entry: %+v", e)
			}
		})
	}
}

func TestFinishDeadlineExceededPersists(t *testing.T) {
	tr, persisted := newTracer()
	tr.Start("c", "u", "sys", "p").Finish("", false, context.DeadlineExceeded)
	if len(*persisted) != 1 {
		t.Fatalf("persisted %d, want 1", len(*persisted))
	}
}

func TestRingKeepsNewest50InOrder(t *testing.T) {
	tr, _ := newTracer()
	for i := range maxEntries + 10 {
		tr.Start("c", "u", "sys", "p").Finish(fmt.Sprint(i), false, nil)
	}
	got := tr.RecentSuccesses()
	if len(got) != maxEntries {
		t.Fatalf("len %d, want %d", len(got), maxEntries)
	}
	for i, e := range got {
		if want := fmt.Sprint(i + 10); e.Response != want {
			t.Fatalf("entry %d response %q, want %q", i, e.Response, want)
		}
	}
}

func TestFindByID(t *testing.T) {
	tr, _ := newTracer()
	e := tr.Start("c", "u", "sys", "p").Finish("ok", false, nil)
	if got := tr.FindByID(e.ID); got != e {
		t.Fatalf("FindByID = %+v, want %+v", got, e)
	}
	if tr.FindByID("missing") != nil {
		t.Fatal("expected nil for unknown id")
	}
}
