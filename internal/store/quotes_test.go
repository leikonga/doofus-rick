package store_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/leikonga/doofus-rick/internal/pgtest"
	"github.com/leikonga/doofus-rick/internal/store"
)

func seedQuotes(t *testing.T, s *store.Store) {
	t.Helper()
	base := time.Now().Add(-time.Hour).Truncate(time.Second)
	quotes := []store.Quote{
		{Content: "Alpha Bravo", Creator: "1", Participants: &store.StringSlice{"111", "222"}},
		{Content: "bravo charlie", Creator: "2", Participants: &store.StringSlice{"1111"}},
		{Content: "Delta", Creator: "3", Participants: &store.StringSlice{"222"}},
	}
	for i, q := range quotes {
		q.CreatedAt = base.Add(time.Duration(i) * time.Minute)
		if err := s.CreateQuote(context.Background(), q); err != nil {
			t.Fatalf("CreateQuote: %v", err)
		}
	}
}

func contents(quotes []store.Quote) []string {
	out := make([]string, len(quotes))
	for i, q := range quotes {
		out[i] = q.Content
	}
	return out
}

func TestQuoteCreateAndGet(t *testing.T) {
	s := pgtest.Store(t)
	ctx := context.Background()

	err := s.CreateQuote(ctx, store.Quote{Content: "hello", Creator: "42", Participants: &store.StringSlice{"42", "43"}})
	if err != nil {
		t.Fatalf("CreateQuote: %v", err)
	}
	got, err := s.GetQuote(ctx, "1")
	if err != nil {
		t.Fatalf("GetQuote: %v", err)
	}
	if got.ID != 1 || got.Content != "hello" || got.Creator != "42" || got.Votes != 0 {
		t.Errorf("unexpected quote: %+v", got)
	}
	if got.Participants == nil || !slices.Equal(*got.Participants, []string{"42", "43"}) {
		t.Errorf("participants = %v", got.Participants)
	}
}

func TestQuoteGetMissing(t *testing.T) {
	s := pgtest.Store(t)
	_, err := s.GetQuote(context.Background(), "999")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v, want store.ErrNotFound", err)
	}
}

func TestQuoteGetRandomEmpty(t *testing.T) {
	s := pgtest.Store(t)
	_, err := s.GetRandomQuote(context.Background())
	if err == nil {
		t.Fatal("expected error on empty table")
	}
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("err = %v, want store.ErrNotFound", err)
	}
}

func TestQuoteGetRandomNonEmpty(t *testing.T) {
	s := pgtest.Store(t)
	seedQuotes(t, s)
	if _, err := s.GetRandomQuote(context.Background()); err != nil {
		t.Fatalf("GetRandomQuote: %v", err)
	}
}

func TestQuoteGetQuotesOrdering(t *testing.T) {
	s := pgtest.Store(t)
	seedQuotes(t, s)
	quotes, err := s.GetQuotes(context.Background())
	if err != nil {
		t.Fatalf("GetQuotes: %v", err)
	}
	got := contents(quotes)
	want := []string{"Delta", "bravo charlie", "Alpha Bravo"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestQuoteSearch(t *testing.T) {
	s := pgtest.Store(t)
	seedQuotes(t, s)

	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{"empty returns all", "", []string{"Delta", "bravo charlie", "Alpha Bravo"}},
		{"case insensitive", "BRAVO", []string{"bravo charlie", "Alpha Bravo"}},
		{"substring", "harl", []string{"bravo charlie"}},
		{"no match", "zzz", []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			quotes, err := s.SearchQuotes(context.Background(), tt.query)
			if err != nil {
				t.Fatalf("SearchQuotes: %v", err)
			}
			got := contents(quotes)
			if !slices.Equal(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestQuoteGetByParticipant(t *testing.T) {
	s := pgtest.Store(t)
	seedQuotes(t, s)

	tests := []struct {
		name string
		id   string
		want []string
	}{
		{"exact id", "222", []string{"Delta", "Alpha Bravo"}},
		{"shorter id does not match longer", "111", []string{"Alpha Bravo"}},
		{"longer id", "1111", []string{"bravo charlie"}},
		{"partial id does not match", "11", []string{}},
		{"unknown", "999", []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			quotes, err := s.GetQuotesByParticipant(context.Background(), tt.id)
			if err != nil {
				t.Fatalf("GetQuotesByParticipant: %v", err)
			}
			got := contents(quotes)
			if !slices.Equal(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
