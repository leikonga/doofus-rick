package archive

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/leikonga/doofus-rick/internal/llm"
	"github.com/leikonga/doofus-rick/internal/store"
)

const (
	maxRewrittenQueries = 3
	maxKnownAuthors     = 50
	knownAuthorWindow   = 180 * 24 * time.Hour
	rewriteMaxTokens    = 300
	rewriteDateLayout   = "2006-01-02"
)

const rewriteSystemPrompt = `You turn a Discord message into search queries over a chat archive.

Rules:
- Return at most 3 queries, each about a distinct person, topic or event worth looking up.
- "text": short standalone natural language description of what to find, no instructions to the bot.
- "keywords": 1 to 4 literal words likely to appear in the archived messages (names, rare words). Empty string if none.
- "author": display name or snowflake of the person the lookup is about, as written in the message or mapped from the known authors list. Empty string if none.
- "since" and "until": only when the message implies a time range, resolved against the current date. Format YYYY-MM-DD, empty string otherwise.
- If the message names no concrete person, topic or event to look up (chit chat, greetings, pure commands), return an empty queries array.`

var rewriteResponseSchema = &llm.ResponseSchema{
	Name: "recall_queries",
	Schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"queries": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"text":     map[string]any{"type": "string"},
						"keywords": map[string]any{"type": "string"},
						"author":   map[string]any{"type": "string"},
						"since":    map[string]any{"type": "string"},
						"until":    map[string]any{"type": "string"},
					},
					"required":             []string{"text", "keywords", "author", "since", "until"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []string{"queries"},
		"additionalProperties": false,
	},
}

type RewrittenQuery struct {
	Text     string `json:"text"`
	Keywords string `json:"keywords"`
	Author   string `json:"author"`
	Since    string `json:"since"`
	Until    string `json:"until"`
}

type completer interface {
	Complete(ctx context.Context, req llm.CompletionRequest) (llm.CompletionResponse, error)
}

type rewriterStore interface {
	SaveTokenUsage(ctx context.Context, u store.TokenUsage) error
	GetActiveAuthors(ctx context.Context, since time.Time, limit int) ([]store.ActiveAuthor, error)
}

type QueryRewriter struct {
	model  string
	client completer
	store  rewriterStore
	now    func() time.Time
}

func NewQueryRewriter(model string, c completer, s rewriterStore) *QueryRewriter {
	return &QueryRewriter{model: model, client: c, store: s, now: time.Now}
}

func (w *QueryRewriter) Rewrite(ctx context.Context, message, channelKey string) ([]RewrittenQuery, error) {
	now := w.now()

	var prompt strings.Builder
	fmt.Fprintf(&prompt, "Current date: %s\n", now.Format(rewriteDateLayout))
	authors, err := w.store.GetActiveAuthors(ctx, now.Add(-knownAuthorWindow), maxKnownAuthors)
	if err != nil {
		slog.Warn("failed to load known authors for rewrite", "error", err)
	}
	if len(authors) > 0 {
		names := make([]string, len(authors))
		for i, a := range authors {
			names[i] = a.AuthorName
		}
		fmt.Fprintf(&prompt, "Known authors: %s\n", strings.Join(names, ", "))
	}
	fmt.Fprintf(&prompt, "\nMessage:\n%s", message)

	resp, err := w.client.Complete(ctx, llm.CompletionRequest{
		Model:          w.model,
		MaxTokens:      rewriteMaxTokens,
		ResponseSchema: rewriteResponseSchema,
		System:         rewriteSystemPrompt,
		Messages:       []llm.Message{llm.NewUserMessage(llm.TextPart(prompt.String()))},
	})
	if err != nil {
		return nil, fmt.Errorf("rewrite query: %w", err)
	}
	if err := w.store.SaveTokenUsage(ctx, store.TokenUsage{ChannelID: channelKey, UserID: "recall-rewriter", ModelName: w.model, InputTokens: resp.InputTokens, OutputTokens: resp.OutputTokens}); err != nil {
		slog.Warn("failed to save token usage", "error", err)
	}

	return parseRewrittenQueries(resp.Message.Text())
}

func parseRewrittenQueries(raw string) ([]RewrittenQuery, error) {
	var out struct {
		Queries []RewrittenQuery `json:"queries"`
	}
	if err := json.Unmarshal([]byte(stripCodeFence(raw)), &out); err != nil {
		return nil, fmt.Errorf("parse rewritten queries: %w", err)
	}
	queries := make([]RewrittenQuery, 0, len(out.Queries))
	for _, q := range out.Queries {
		q.Text = strings.TrimSpace(q.Text)
		q.Keywords = strings.TrimSpace(q.Keywords)
		if q.Text == "" {
			continue
		}
		queries = append(queries, q)
		if len(queries) == maxRewrittenQueries {
			break
		}
	}
	return queries, nil
}

func stripCodeFence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	s = strings.TrimPrefix(s, "```")
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	return strings.TrimSpace(s)
}

func (q RewrittenQuery) dateRange() (since, until *time.Time) {
	if t, err := time.Parse(rewriteDateLayout, strings.TrimSpace(q.Since)); err == nil {
		since = &t
	}
	if t, err := time.Parse(rewriteDateLayout, strings.TrimSpace(q.Until)); err == nil {
		t = t.AddDate(0, 0, 1)
		until = &t
	}
	return since, until
}
