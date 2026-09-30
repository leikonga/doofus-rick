package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/leikonga/doofus-rick/internal/pgtest"
	"github.com/leikonga/doofus-rick/internal/store"
)

func TestPersonTool(t *testing.T) {
	s := pgtest.Store(t)
	ctx := context.Background()
	now := time.Now()
	for _, m := range []store.Message{
		{ID: 1, ChannelID: 1, AuthorID: 7, AuthorName: "klaus", Content: "hello", CreatedAt: now},
		{ID: 2, ChannelID: 1, AuthorID: 8, AuthorName: "twin", Content: "hello", CreatedAt: now},
		{ID: 3, ChannelID: 1, AuthorID: 9, AuthorName: "twin", Content: "hello", CreatedAt: now},
		{ID: 4, ChannelID: 1, AuthorID: 10, AuthorName: "fresh", Content: "hello", CreatedAt: now},
	} {
		if err := s.CreateMessage(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SavePersonProfile(ctx, store.PersonProfile{UserID: 7, Summary: "likes cats", WatermarkMessageID: 1, UpdatedAt: time.Date(2026, 3, 14, 12, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	tool := (&Agent{store: s}).personTool()

	run := func(user string) string {
		t.Helper()
		res, err := tool.Execute(ctx, []byte(`{"user":"`+user+`"}`))
		if err != nil {
			t.Fatalf("user %q: %v", user, err)
		}
		return res.Content()
	}

	for _, user := range []string{"7", "Klaus"} {
		got := run(user)
		if !strings.Contains(got, "likes cats") || !strings.Contains(got, "last updated 2026-03-14") {
			t.Errorf("user %q = %q", user, got)
		}
	}
	if got := run("10"); !strings.Contains(got, "no profile") || !strings.Contains(got, "memory_search") {
		t.Errorf("no profile = %q", got)
	}
	if got := run("ghost"); !strings.Contains(got, "no author named") {
		t.Errorf("unknown = %q", got)
	}
	if got := run("twin"); !strings.Contains(got, "ambiguous") || !strings.Contains(got, "8") || !strings.Contains(got, "9") {
		t.Errorf("ambiguous = %q", got)
	}
}
