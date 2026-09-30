package archive

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/leikonga/doofus-rick/internal/llm"
	"github.com/leikonga/doofus-rick/internal/pgtest"
	"github.com/leikonga/doofus-rick/internal/store"
)

const testEmbedModel = "test/embed-model"

func unitVector(axis int) []float32 {
	v := make([]float32, 1024)
	v[axis] = 1
	return v
}

func fakeEmbedClient(t *testing.T, queryVector []float32) *llm.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"model":  testEmbedModel,
			"data":   []any{map[string]any{"object": "embedding", "index": 0, "embedding": queryVector}},
			"usage":  map[string]any{"prompt_tokens": 7, "total_tokens": 7},
		})
	}))
	t.Cleanup(srv.Close)
	return llm.NewClientWithServerURL("test-key", srv.URL)
}

func seedChunk(t *testing.T, s *store.Store, channelID uint64, content string, axis int) uint64 {
	t.Helper()
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)
	id := pgtest.Query[uint64](t, "SELECT COALESCE(MAX(id), 0) + 1 FROM chunks")[0]
	chunk := store.Chunk{ID: id, ChannelID: channelID, Content: content, StartedAt: now, EndedAt: now, MessageCount: 1, FirstMessageID: id, LastMessageID: id}
	if err := s.CreateChunk(ctx, chunk); err != nil {
		t.Fatalf("CreateChunk: %v", err)
	}
	if axis >= 0 {
		emb := store.ChunkEmbedding{ChunkID: id, Model: testEmbedModel, Embedding: store.HalfVector(unitVector(axis))}
		if err := s.SaveChunkEmbedding(ctx, emb); err != nil {
			t.Fatalf("SaveChunkEmbedding: %v", err)
		}
	}
	return id
}

func chunkIDs(chunks []RetrievedChunk) []uint64 {
	out := make([]uint64, len(chunks))
	for i, c := range chunks {
		out[i] = c.ID
	}
	return out
}

func TestRetrieveHybridRanking(t *testing.T) {
	s := pgtest.Store(t)
	ctx := context.Background()

	both := seedChunk(t, s, 1, "zebra crossing\n", 0)
	lexOnly := seedChunk(t, s, 1, "zebra herd\n", -1)
	vecOnly := seedChunk(t, s, 1, "unrelated words\n", 5)
	otherChannel := seedChunk(t, s, 2, "zebra elsewhere\n", 0)

	r := NewRetriever(RetrievalConfig{TopK: 10, EmbedModel: testEmbedModel, NeighborChunks: 1}, s, fakeEmbedClient(t, unitVector(0)))

	got, err := r.Retrieve(ctx, RetrieveRequest{Query: "zebra", ChannelIDs: []uint64{1}})
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	ids := chunkIDs(got)
	if len(ids) != 3 || ids[0] != both {
		t.Fatalf("ids = %v, want %d first of 3", ids, both)
	}
	if !slices.Contains(ids, lexOnly) {
		t.Errorf("lexical-only chunk %d missing from %v", lexOnly, ids)
	}
	if !slices.Contains(ids, vecOnly) {
		t.Errorf("vector-only chunk %d missing from %v", vecOnly, ids)
	}
	if slices.Contains(ids, otherChannel) {
		t.Errorf("chunk from channel outside channelIDs returned: %v", ids)
	}
	for _, c := range got {
		if c.ChannelID != 1 || !c.ChannelVisible {
			t.Errorf("unexpected chunk metadata: %+v", c)
		}
	}
	if got[0].Score <= got[1].Score {
		t.Errorf("scores not descending: %v, %v", got[0].Score, got[1].Score)
	}

	both2, err := r.Retrieve(ctx, RetrieveRequest{Query: "zebra", ChannelIDs: []uint64{1, 2}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(chunkIDs(both2), otherChannel) {
		t.Errorf("channel 2 chunk missing when channelIDs includes it: %v", chunkIDs(both2))
	}
}

func TestRetrieveMinSimilarityAndTopK(t *testing.T) {
	s := pgtest.Store(t)
	ctx := context.Background()
	both := seedChunk(t, s, 1, "zebra crossing\n", 0)
	lexOnly := seedChunk(t, s, 1, "zebra herd\n", -1)
	vecOnly := seedChunk(t, s, 1, "unrelated words\n", 5)
	client := fakeEmbedClient(t, unitVector(0))

	tests := []struct {
		name string
		cfg  RetrievalConfig
		want []uint64
	}{
		{"topk limits", RetrievalConfig{TopK: 1}, []uint64{both}},
		{"floor drops weak vector hit, keeps lexical", RetrievalConfig{TopK: 10, MinSimilarity: 0.5}, []uint64{both, lexOnly}},
		{"disabled floor keeps all", RetrievalConfig{TopK: 10}, []uint64{both, lexOnly, vecOnly}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.cfg.EmbedModel = testEmbedModel
			r := NewRetriever(tt.cfg, s, client)
			got, err := r.Retrieve(ctx, RetrieveRequest{Query: "zebra", ChannelIDs: []uint64{1}})
			if err != nil {
				t.Fatal(err)
			}
			ids := chunkIDs(got)
			if len(ids) != len(tt.want) || ids[0] != both {
				t.Fatalf("got %v, want %v with %d first", ids, tt.want, both)
			}
			for _, id := range tt.want {
				if !slices.Contains(ids, id) {
					t.Errorf("chunk %d missing from %v", id, ids)
				}
			}
		})
	}
}

func TestRetrievePassesFilters(t *testing.T) {
	s := pgtest.Store(t)
	ctx := context.Background()
	id := seedChunk(t, s, 1, "zebra\n", 0)
	pgtest.Exec(t, "INSERT INTO messages (id, channel_id, author_id, author_name, content, is_bot, created_at) VALUES (?, 1, 42, 'klaus', 'zebra', false, now())", id)
	r := NewRetriever(RetrievalConfig{EmbedModel: testEmbedModel}, s, fakeEmbedClient(t, unitVector(0)))

	author, other := uint64(42), uint64(43)
	future := time.Now().Add(24 * time.Hour)
	tests := []struct {
		name string
		req  RetrieveRequest
		want int
	}{
		{"matching author", RetrieveRequest{AuthorID: &author}, 1},
		{"other author", RetrieveRequest{AuthorID: &other}, 0},
		{"since in future", RetrieveRequest{Since: &future}, 0},
		{"until in future", RetrieveRequest{Until: &future}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.req.Query = "zebra"
			tt.req.ChannelIDs = []uint64{1}
			got, err := r.Retrieve(ctx, tt.req)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != tt.want {
				t.Fatalf("got %v, want %d results", chunkIDs(got), tt.want)
			}
		})
	}
}

func TestRetrieveNeighborExpansion(t *testing.T) {
	s := pgtest.Store(t)
	ctx := context.Background()
	seedChunk(t, s, 3, "far-before\n", -1)
	seedChunk(t, s, 3, "before\n", -1)
	hit := seedChunk(t, s, 3, "hit\n", 0)
	seedChunk(t, s, 3, "after\n", -1)
	seedChunk(t, s, 3, "far-after\n", -1)
	seedChunk(t, s, 4, "other-channel\n", -1)
	client := fakeEmbedClient(t, unitVector(0))

	tests := []struct {
		name      string
		neighbors int
		want      string
	}{
		{"one neighbor each side", 1, "before\n\nhit\n\nafter\n"},
		{"two neighbors each side", 2, "far-before\n\nbefore\n\nhit\n\nafter\n\nfar-after\n"},
		{"zero disables expansion", 0, "hit\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRetriever(RetrievalConfig{TopK: 1, EmbedModel: testEmbedModel, NeighborChunks: tt.neighbors}, s, client)
			got, err := r.Retrieve(ctx, RetrieveRequest{Query: "nomatch", ChannelIDs: []uint64{3}})
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || got[0].ID != hit {
				t.Fatalf("got %v, want [%d]", chunkIDs(got), hit)
			}
			if got[0].Content != tt.want {
				t.Errorf("content = %q, want %q", got[0].Content, tt.want)
			}
		})
	}
}

func TestRetrieveNeighborAtEdges(t *testing.T) {
	s := pgtest.Store(t)
	first := seedChunk(t, s, 1, "first\n", 0)
	seedChunk(t, s, 1, "second\n", -1)
	r := NewRetriever(RetrievalConfig{TopK: 1, EmbedModel: testEmbedModel, NeighborChunks: 1}, s, fakeEmbedClient(t, unitVector(0)))

	got, err := r.Retrieve(context.Background(), RetrieveRequest{Query: "nomatch", ChannelIDs: []uint64{1}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != first || got[0].Content != "first\n\nsecond\n" {
		t.Errorf("got %+v", got)
	}
}

func TestRetrieveRecordsTokenUsage(t *testing.T) {
	s := pgtest.Store(t)
	seedChunk(t, s, 9, "zebra\n", 0)
	r := NewRetriever(RetrievalConfig{EmbedModel: testEmbedModel}, s, fakeEmbedClient(t, unitVector(0)))

	if _, err := r.Retrieve(context.Background(), RetrieveRequest{Query: "zebra", ChannelIDs: []uint64{9, 10}}); err != nil {
		t.Fatal(err)
	}
	rows := pgtest.Query[store.TokenUsage](t, "SELECT * FROM token_usages")
	if len(rows) != 1 {
		t.Fatalf("token_usages rows = %d, want 1", len(rows))
	}
	got := rows[0]
	if got.ChannelID != "9" || got.UserID != "retriever" || got.ModelName != testEmbedModel || got.InputTokens != 7 || got.OutputTokens != 0 {
		t.Errorf("unexpected row: %+v", got)
	}
}

func TestRetrieveEmptyChannelIDs(t *testing.T) {
	s := pgtest.Store(t)
	r := NewRetriever(RetrievalConfig{EmbedModel: testEmbedModel}, s, fakeEmbedClient(t, unitVector(0)))
	got, err := r.Retrieve(context.Background(), RetrieveRequest{Query: "zebra", ChannelIDs: nil})
	if err != nil || len(got) != 0 {
		t.Errorf("got %v, %v; want no results and no error", got, err)
	}
	rows := pgtest.Query[store.TokenUsage](t, "SELECT * FROM token_usages")
	if len(rows) != 1 || rows[0].ChannelID != "0" {
		t.Errorf("rows = %+v, want one row with channel 0", rows)
	}
}

type fakeRewriter struct {
	queries []RewrittenQuery
	err     error
	calls   int
}

func (f *fakeRewriter) Rewrite(context.Context, string, string) ([]RewrittenQuery, error) {
	f.calls++
	return f.queries, f.err
}

func TestRetrieveRewrittenMergesAndDedupes(t *testing.T) {
	s := pgtest.Store(t)
	ctx := context.Background()
	both := seedChunk(t, s, 1, "zebra crossing\n", 0)
	lexOnly := seedChunk(t, s, 1, "zebra herd\n", -1)
	giraffe := seedChunk(t, s, 1, "giraffe neck\n", -1)
	seedChunk(t, s, 1, "giraffe tall\n", -1)

	r := NewRetriever(RetrievalConfig{TopK: 3, EmbedModel: testEmbedModel}, s, fakeEmbedClient(t, unitVector(0)))
	r.rewriter = &fakeRewriter{queries: []RewrittenQuery{
		{Text: "zebras", Keywords: "zebra"},
		{Text: "zebras again", Keywords: "zebra"},
		{Text: "giraffes", Keywords: "giraffe"},
	}}

	got, err := r.RetrieveRewritten(ctx, "tell me about animals", []uint64{1})
	if err != nil {
		t.Fatal(err)
	}
	ids := chunkIDs(got)
	if len(ids) != 3 {
		t.Fatalf("ids = %v, want 3 capped at TopK", ids)
	}
	seen := map[uint64]bool{}
	for _, id := range ids {
		if seen[id] {
			t.Errorf("duplicate chunk %d in %v", id, ids)
		}
		seen[id] = true
	}
	if ids[0] != both {
		t.Errorf("best chunk %d should lead: %v", both, ids)
	}
	if !seen[lexOnly] && !seen[giraffe] {
		t.Errorf("neither keyword-only branch represented: %v", ids)
	}
	for i := 1; i < len(got); i++ {
		if got[i].Score > got[i-1].Score {
			t.Errorf("not ordered by score: %v", got)
		}
	}
}

func TestRetrieveRewrittenFallsBackToRawQuery(t *testing.T) {
	s := pgtest.Store(t)
	ctx := context.Background()
	zebra := seedChunk(t, s, 1, "zebra crossing\n", 0)
	client := fakeEmbedClient(t, unitVector(0))

	failing := NewRetriever(RetrievalConfig{TopK: 3, EmbedModel: testEmbedModel}, s, client)
	failing.rewriter = &fakeRewriter{err: errors.New("boom")}
	disabled := NewRetriever(RetrievalConfig{TopK: 3, EmbedModel: testEmbedModel}, s, client)

	for name, r := range map[string]*Retriever{"rewriter error": failing, "rewriter unset": disabled} {
		t.Run(name, func(t *testing.T) {
			got, err := r.RetrieveRewritten(ctx, "zebra", []uint64{1})
			if err != nil || !slices.Equal(chunkIDs(got), []uint64{zebra}) {
				t.Errorf("got %v, %v", chunkIDs(got), err)
			}
		})
	}
}

func TestRetrieveRewrittenZeroQueriesSkipsRecall(t *testing.T) {
	s := pgtest.Store(t)
	seedChunk(t, s, 1, "zebra crossing\n", 0)
	r := NewRetriever(RetrievalConfig{TopK: 3, EmbedModel: testEmbedModel}, s, fakeEmbedClient(t, unitVector(0)))
	r.rewriter = &fakeRewriter{}
	got, err := r.RetrieveRewritten(context.Background(), "lol", []uint64{1})
	if err != nil || len(got) != 0 {
		t.Errorf("got %v, %v", got, err)
	}
}

func TestRetrieveRewrittenAuthorFilter(t *testing.T) {
	s := pgtest.Store(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)
	for _, m := range []store.Message{
		{ID: 1, ChannelID: 1, AuthorID: 7, AuthorName: "klaus", Content: "c", CreatedAt: now},
		{ID: 2, ChannelID: 1, AuthorID: 8, AuthorName: "twin", Content: "c", CreatedAt: now},
		{ID: 3, ChannelID: 1, AuthorID: 9, AuthorName: "twin", Content: "c", CreatedAt: now},
	} {
		if err := s.CreateMessage(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	chunk := func(id uint64, content string, axis int) {
		c := store.Chunk{ID: id, ChannelID: 1, Content: content, StartedAt: now, EndedAt: now, MessageCount: 1, FirstMessageID: id, LastMessageID: id}
		if err := s.CreateChunk(ctx, c); err != nil {
			t.Fatal(err)
		}
		if err := s.SaveChunkEmbedding(ctx, store.ChunkEmbedding{ChunkID: id, Model: testEmbedModel, Embedding: store.HalfVector(unitVector(axis))}); err != nil {
			t.Fatal(err)
		}
	}
	chunk(1, "zebra from klaus\n", 0)
	chunk(2, "zebra from twin\n", 0)

	r := NewRetriever(RetrievalConfig{TopK: 5, EmbedModel: testEmbedModel}, s, fakeEmbedClient(t, unitVector(0)))
	for author, want := range map[string][]uint64{
		"Klaus": {1},
		"ghost": {1, 2},
		"twin":  {1, 2},
	} {
		r.rewriter = &fakeRewriter{queries: []RewrittenQuery{{Text: "zebra", Keywords: "zebra", Author: author}}}
		got, err := r.RetrieveRewritten(ctx, "zebra", []uint64{1})
		if err != nil {
			t.Fatal(err)
		}
		ids := chunkIDs(got)
		slices.Sort(ids)
		if !slices.Equal(ids, want) {
			t.Errorf("author %q: ids = %v, want %v", author, ids, want)
		}
	}
}

type fakeReranker struct {
	scores map[string]float64
	err    error
	calls  int
	last   llm.RerankRequest
}

func (f *fakeReranker) Rerank(_ context.Context, req llm.RerankRequest) (llm.RerankResponse, error) {
	f.calls++
	f.last = req
	if f.err != nil {
		return llm.RerankResponse{}, f.err
	}
	resp := llm.RerankResponse{TotalTokens: 11}
	for i, d := range req.Documents {
		resp.Results = append(resp.Results, llm.RerankResult{Index: i, Score: f.scores[d]})
	}
	slices.SortFunc(resp.Results, func(a, b llm.RerankResult) int {
		switch {
		case a.Score > b.Score:
			return -1
		case a.Score < b.Score:
			return 1
		}
		return 0
	})
	return resp, nil
}

func rerankRetriever(t *testing.T, s *store.Store, cfg RetrievalConfig, rr *fakeReranker) *Retriever {
	t.Helper()
	cfg.EmbedModel = testEmbedModel
	cfg.RerankModel = "test/rerank"
	r := NewRetriever(cfg, s, fakeEmbedClient(t, unitVector(0)))
	r.reranker = rr
	return r
}

func TestRetrieveRerankReordersAndKeepsTopK(t *testing.T) {
	s := pgtest.Store(t)
	ctx := context.Background()
	a := seedChunk(t, s, 1, "zebra a\n", 0)
	b := seedChunk(t, s, 1, "zebra b\n", -1)
	c := seedChunk(t, s, 1, "zebra c\n", -1)
	rr := &fakeReranker{scores: map[string]float64{"zebra a\n": 0.1, "zebra b\n": 0.9, "zebra c\n": 0.5}}
	r := rerankRetriever(t, s, RetrievalConfig{TopK: 2, RerankCandidates: 10}, rr)

	got, err := r.Retrieve(ctx, RetrieveRequest{Query: "zebra", ChannelIDs: []uint64{1}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(chunkIDs(got), []uint64{b, c}) {
		t.Fatalf("ids = %v, want [%d %d] (not %d)", chunkIDs(got), b, c, a)
	}
	if len(rr.last.Documents) != 3 || rr.last.Query != "zebra" || rr.last.Model != "test/rerank" {
		t.Errorf("rerank request = %+v", rr.last)
	}
	if got[0].Score != 0.9 || !got[0].Reranked || got[0].RRF <= 0 {
		t.Errorf("unexpected scores: %+v", got[0])
	}
	rows := pgtest.Query[store.TokenUsage](t, "SELECT * FROM token_usages WHERE user_id = 'reranker'")
	if len(rows) != 1 || rows[0].InputTokens != 11 || rows[0].ModelName != "test/rerank" {
		t.Errorf("reranker usage rows = %+v", rows)
	}
}

func TestRetrieveRerankFetchesCandidatesBeyondTopK(t *testing.T) {
	s := pgtest.Store(t)
	seedChunk(t, s, 1, "zebra a\n", -1)
	seedChunk(t, s, 1, "zebra b\n", -1)
	seedChunk(t, s, 1, "zebra c\n", -1)
	rr := &fakeReranker{scores: map[string]float64{}}
	r := rerankRetriever(t, s, RetrievalConfig{TopK: 1, RerankCandidates: 3}, rr)

	got, err := r.Retrieve(context.Background(), RetrieveRequest{Query: "zebra", ChannelIDs: []uint64{1}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rr.last.Documents) != 3 || len(got) != 1 {
		t.Errorf("reranked %d docs, returned %d; want 3 and 1", len(rr.last.Documents), len(got))
	}
}

func TestRetrieveRerankMinScore(t *testing.T) {
	s := pgtest.Store(t)
	ctx := context.Background()
	strong := seedChunk(t, s, 1, "zebra strong\n", -1)
	seedChunk(t, s, 1, "zebra weak\n", -1)
	rr := &fakeReranker{scores: map[string]float64{"zebra strong\n": 0.8, "zebra weak\n": 0.2}}

	r := rerankRetriever(t, s, RetrievalConfig{TopK: 5, RerankCandidates: 10, RerankMinScore: 0.5}, rr)
	got, err := r.Retrieve(ctx, RetrieveRequest{Query: "zebra", ChannelIDs: []uint64{1}})
	if err != nil || !slices.Equal(chunkIDs(got), []uint64{strong}) {
		t.Errorf("got %v, %v; want only %d", chunkIDs(got), err, strong)
	}

	r = rerankRetriever(t, s, RetrievalConfig{TopK: 5, RerankCandidates: 10, RerankMinScore: 0.95}, rr)
	got, err = r.Retrieve(ctx, RetrieveRequest{Query: "zebra", ChannelIDs: []uint64{1}})
	if err != nil || len(got) != 0 {
		t.Errorf("got %v, %v; want empty", chunkIDs(got), err)
	}
}

func TestRetrieveRerankNeighborsOnlyForKept(t *testing.T) {
	s := pgtest.Store(t)
	ctx := context.Background()
	seedChunk(t, s, 3, "before\n", -1)
	hit := seedChunk(t, s, 3, "zebra hit\n", -1)
	seedChunk(t, s, 3, "after\n", -1)
	rr := &fakeReranker{scores: map[string]float64{"zebra hit\n": 0.9}}
	r := rerankRetriever(t, s, RetrievalConfig{TopK: 5, RerankCandidates: 10, RerankMinScore: 0.5, NeighborChunks: 1}, rr)

	got, err := r.Retrieve(ctx, RetrieveRequest{Query: "zebra", ChannelIDs: []uint64{3}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != hit || got[0].Content != "before\n\nzebra hit\n\nafter\n" {
		t.Errorf("got %+v", got)
	}
	for _, d := range rr.last.Documents {
		if d != "zebra hit\n" {
			t.Errorf("reranker received non-raw content %q", d)
		}
	}
}

func TestRetrieveRerankErrorFallsBackToFusedOrder(t *testing.T) {
	s := pgtest.Store(t)
	both := seedChunk(t, s, 1, "zebra crossing\n", 0)
	seedChunk(t, s, 1, "zebra herd\n", -1)
	rr := &fakeReranker{err: errors.New("boom")}
	r := rerankRetriever(t, s, RetrievalConfig{TopK: 1, RerankCandidates: 10, RerankMinScore: 0.5}, rr)

	got, err := r.Retrieve(context.Background(), RetrieveRequest{Query: "zebra", ChannelIDs: []uint64{1}})
	if err != nil || !slices.Equal(chunkIDs(got), []uint64{both}) || got[0].Reranked {
		t.Errorf("got %+v, %v; want fused top %d", got, err, both)
	}
}

func TestRetrieveRerankSkippedWhenNoCandidates(t *testing.T) {
	s := pgtest.Store(t)
	rr := &fakeReranker{}
	r := rerankRetriever(t, s, RetrievalConfig{TopK: 3, RerankCandidates: 10}, rr)
	got, err := r.Retrieve(context.Background(), RetrieveRequest{Query: "zebra", ChannelIDs: []uint64{1}})
	if err != nil || len(got) != 0 || rr.calls != 0 {
		t.Errorf("got %v, %v, calls=%d", got, err, rr.calls)
	}
}

func TestRetrieveRerankDisabledLeavesBehaviorUnchanged(t *testing.T) {
	s := pgtest.Store(t)
	both := seedChunk(t, s, 1, "zebra crossing\n", 0)
	seedChunk(t, s, 1, "zebra herd\n", -1)
	r := NewRetriever(RetrievalConfig{TopK: 1, EmbedModel: testEmbedModel, RerankCandidates: 30, RerankMinScore: 0.9}, s, fakeEmbedClient(t, unitVector(0)))
	if r.reranker != nil {
		t.Fatal("reranker set without RerankModel")
	}
	got, err := r.Retrieve(context.Background(), RetrieveRequest{Query: "zebra", ChannelIDs: []uint64{1}})
	if err != nil || !slices.Equal(chunkIDs(got), []uint64{both}) || got[0].Reranked {
		t.Errorf("got %+v, %v", got, err)
	}
}

func TestRetrieveRewrittenMergesRerankedScores(t *testing.T) {
	s := pgtest.Store(t)
	zebra := seedChunk(t, s, 1, "zebra crossing\n", -1)
	giraffe := seedChunk(t, s, 1, "giraffe neck\n", -1)
	seedChunk(t, s, 1, "giraffe tall\n", -1)
	rr := &fakeReranker{scores: map[string]float64{"zebra crossing\n": 0.7, "giraffe neck\n": 0.9, "giraffe tall\n": 0.3}}
	r := rerankRetriever(t, s, RetrievalConfig{TopK: 2, RerankCandidates: 10}, rr)
	r.rewriter = &fakeRewriter{queries: []RewrittenQuery{
		{Text: "zebras", Keywords: "zebra"},
		{Text: "giraffes", Keywords: "giraffe"},
		{Text: "giraffes again", Keywords: "giraffe"},
	}}

	got, err := r.RetrieveRewritten(context.Background(), "animals", []uint64{1})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(chunkIDs(got), []uint64{giraffe, zebra}) || got[0].Score != 0.9 || got[1].Score != 0.7 {
		t.Errorf("got %+v", got)
	}
}
