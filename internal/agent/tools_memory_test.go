package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/leikonga/doofus-rick/internal/pgtest"
	"github.com/leikonga/doofus-rick/internal/store"
)

func TestParseSearchDates(t *testing.T) {
	since, until, err := parseSearchDates("2026-03-01", "2026-03-31")
	if err != nil {
		t.Fatal(err)
	}
	if !since.Equal(time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("since = %v", since)
	}
	if !until.Equal(time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("until = %v, want exclusive end of the day", until)
	}

	since, until, err = parseSearchDates("", "")
	if err != nil || since != nil || until != nil {
		t.Errorf("empty = %v, %v, %v", since, until, err)
	}

	for _, bad := range [][2]string{{"yesterday", ""}, {"", "2026-13-01"}, {"03/01/2026", ""}} {
		if _, _, err := parseSearchDates(bad[0], bad[1]); err == nil || !strings.Contains(err.Error(), "YYYY-MM-DD") {
			t.Errorf("parseSearchDates(%q, %q) error = %v, want format hint", bad[0], bad[1], err)
		}
	}
}

func TestResolveAuthor(t *testing.T) {
	s := pgtest.Store(t)
	now := time.Now()
	for _, m := range []store.Message{
		{ID: 1, ChannelID: 1, AuthorID: 7, AuthorName: "klaus", Content: "c", CreatedAt: now},
		{ID: 2, ChannelID: 1, AuthorID: 8, AuthorName: "twin", Content: "c", CreatedAt: now},
		{ID: 3, ChannelID: 1, AuthorID: 9, AuthorName: "twin", Content: "c", CreatedAt: now},
	} {
		if err := s.CreateMessage(context.Background(), m); err != nil {
			t.Fatal(err)
		}
	}
	a := &Agent{store: s}
	ctx := context.Background()

	id, notice, err := a.resolveAuthor(ctx, "123456")
	if err != nil || notice != "" || id == nil || *id != 123456 {
		t.Errorf("snowflake = %v, %q, %v", id, notice, err)
	}
	id, notice, err = a.resolveAuthor(ctx, "Klaus")
	if err != nil || notice != "" || id == nil || *id != 7 {
		t.Errorf("name = %v, %q, %v", id, notice, err)
	}
	id, notice, err = a.resolveAuthor(ctx, "ghost")
	if err != nil || id != nil || !strings.Contains(notice, "no author named") {
		t.Errorf("unknown = %v, %q, %v", id, notice, err)
	}
	id, notice, err = a.resolveAuthor(ctx, "twin")
	if err != nil || id != nil || !strings.Contains(notice, "ambiguous") || !strings.Contains(notice, "8") || !strings.Contains(notice, "9") {
		t.Errorf("ambiguous = %v, %q, %v", id, notice, err)
	}
	id, notice, err = a.resolveAuthor(ctx, "")
	if err != nil || id != nil || notice != "" {
		t.Errorf("empty = %v, %q, %v", id, notice, err)
	}
}
