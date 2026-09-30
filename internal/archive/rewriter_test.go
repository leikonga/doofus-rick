package archive

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/leikonga/doofus-rick/internal/llm"
	"github.com/leikonga/doofus-rick/internal/store"
)

type fakeCompleter struct {
	reply string
	err   error
	got   llm.CompletionRequest
}

func (f *fakeCompleter) Complete(_ context.Context, req llm.CompletionRequest) (llm.CompletionResponse, error) {
	f.got = req
	if f.err != nil {
		return llm.CompletionResponse{}, f.err
	}
	return llm.CompletionResponse{
		Message:     llm.Message{Role: llm.RoleAssistant, Parts: []llm.ContentPart{llm.TextPart(f.reply)}},
		InputTokens: 10, OutputTokens: 5,
	}, nil
}

type fakeRewriterStore struct {
	authors []store.ActiveAuthor
	usage   []store.TokenUsage
}

func (f *fakeRewriterStore) SaveTokenUsage(_ context.Context, u store.TokenUsage) error {
	f.usage = append(f.usage, u)
	return nil
}

func (f *fakeRewriterStore) GetActiveAuthors(context.Context, time.Time, int) ([]store.ActiveAuthor, error) {
	return f.authors, nil
}

func newTestRewriter(c *fakeCompleter, s *fakeRewriterStore) *QueryRewriter {
	w := NewQueryRewriter("test/rewrite", c, s)
	w.now = func() time.Time { return time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC) }
	return w
}

func TestRewriteParsesQueries(t *testing.T) {
	reply := `{"queries":[{"text":"dave being late","keywords":"dave late","author":"dave","since":"2025-01-01","until":""}]}`
	for name, wrapped := range map[string]string{
		"plain":      reply,
		"fence":      "```json\n" + reply + "\n```",
		"fence bare": "```\n" + reply + "\n```",
	} {
		t.Run(name, func(t *testing.T) {
			s := &fakeRewriterStore{authors: []store.ActiveAuthor{{AuthorName: "Dave"}, {AuthorName: "Klaus"}}}
			c := &fakeCompleter{reply: wrapped}
			got, err := newTestRewriter(c, s).Rewrite(context.Background(), "meme about dave", "5")
			if err != nil {
				t.Fatal(err)
			}
			want := []RewrittenQuery{{Text: "dave being late", Keywords: "dave late", Author: "dave", Since: "2025-01-01"}}
			if !slices.Equal(got, want) {
				t.Errorf("got %+v, want %+v", got, want)
			}
			if !strings.Contains(c.got.Messages[0].Text(), "Current date: 2026-09-30") || !strings.Contains(c.got.Messages[0].Text(), "Dave, Klaus") {
				t.Errorf("prompt missing date or authors: %q", c.got.Messages[0].Text())
			}
			if len(s.usage) != 1 || s.usage[0].UserID != "recall-rewriter" || s.usage[0].ChannelID != "5" || s.usage[0].InputTokens != 10 {
				t.Errorf("usage = %+v", s.usage)
			}
		})
	}
}

func TestRewriteSendsResponseSchema(t *testing.T) {
	c := &fakeCompleter{reply: `{"queries":[]}`}
	if _, err := newTestRewriter(c, &fakeRewriterStore{}).Rewrite(context.Background(), "hi", "1"); err != nil {
		t.Fatal(err)
	}
	rs := c.got.ResponseSchema
	if rs == nil || rs.Name != "recall_queries" {
		t.Fatalf("response schema = %+v", rs)
	}
	if required, _ := rs.Schema["required"].([]string); !slices.Contains(required, "queries") {
		t.Errorf("schema required = %v, want queries", rs.Schema["required"])
	}
}

func TestRewriteZeroQueries(t *testing.T) {
	got, err := newTestRewriter(&fakeCompleter{reply: `{"queries":[]}`}, &fakeRewriterStore{}).Rewrite(context.Background(), "hi", "1")
	if err != nil || len(got) != 0 {
		t.Errorf("got %v, %v", got, err)
	}
}

func TestRewriteCapsAndDropsEmptyText(t *testing.T) {
	reply := `{"queries":[{"text":""},{"text":"a"},{"text":"b"},{"text":"c"},{"text":"d"}]}`
	got, err := newTestRewriter(&fakeCompleter{reply: reply}, &fakeRewriterStore{}).Rewrite(context.Background(), "x", "1")
	if err != nil || len(got) != 3 || got[0].Text != "a" || got[2].Text != "c" {
		t.Errorf("got %v, %v", got, err)
	}
}

func TestRewriteErrors(t *testing.T) {
	if _, err := newTestRewriter(&fakeCompleter{reply: "not json"}, &fakeRewriterStore{}).Rewrite(context.Background(), "x", "1"); err == nil {
		t.Error("malformed JSON: want error")
	}
	boom := errors.New("boom")
	if _, err := newTestRewriter(&fakeCompleter{err: boom}, &fakeRewriterStore{}).Rewrite(context.Background(), "x", "1"); !errors.Is(err, boom) {
		t.Errorf("completion error = %v, want wrapped boom", err)
	}
}

func TestDateRange(t *testing.T) {
	since, until := RewrittenQuery{Since: "2025-01-01", Until: "2025-01-31"}.dateRange()
	if since == nil || !since.Equal(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("since = %v", since)
	}
	if until == nil || !until.Equal(time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("until = %v", until)
	}
	since, until = RewrittenQuery{Since: "last year", Until: ""}.dateRange()
	if since != nil || until != nil {
		t.Errorf("invalid dates should be dropped: %v %v", since, until)
	}
}
