package archive

import (
	"context"
	"encoding/json"
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

	r := NewRetriever(RetrievalConfig{TopK: 10, MinScore: 0.001, EmbedModel: testEmbedModel, NeighborChunks: 1}, s, fakeEmbedClient(t, unitVector(0)))

	got, err := r.Retrieve(ctx, "zebra", []uint64{1})
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

	both2, err := r.Retrieve(ctx, "zebra", []uint64{1, 2})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(chunkIDs(both2), otherChannel) {
		t.Errorf("channel 2 chunk missing when channelIDs includes it: %v", chunkIDs(both2))
	}
}

func TestRetrieveMinScoreAndTopK(t *testing.T) {
	s := pgtest.Store(t)
	ctx := context.Background()
	both := seedChunk(t, s, 1, "zebra crossing\n", 0)
	seedChunk(t, s, 1, "zebra herd\n", -1)
	seedChunk(t, s, 1, "unrelated words\n", 5)
	client := fakeEmbedClient(t, unitVector(0))

	tests := []struct {
		name string
		cfg  RetrievalConfig
		want int
	}{
		{"topk limits", RetrievalConfig{TopK: 1, MinScore: 0.001}, 1},
		{"min score filters weaker hits", RetrievalConfig{TopK: 10, MinScore: 0.02}, 1},
		{"min score above everything", RetrievalConfig{TopK: 10, MinScore: 0.5}, 0},
		{"no filtering", RetrievalConfig{TopK: 10, MinScore: 0.001}, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.cfg.EmbedModel = testEmbedModel
			r := NewRetriever(tt.cfg, s, client)
			got, err := r.Retrieve(ctx, "zebra", []uint64{1})
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != tt.want {
				t.Fatalf("got %d chunks %v, want %d", len(got), chunkIDs(got), tt.want)
			}
			if tt.want > 0 && got[0].ID != both {
				t.Errorf("first = %d, want %d", got[0].ID, both)
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
			r := NewRetriever(RetrievalConfig{TopK: 1, MinScore: 0.001, EmbedModel: testEmbedModel, NeighborChunks: tt.neighbors}, s, client)
			got, err := r.Retrieve(ctx, "nomatch", []uint64{3})
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

	got, err := r.Retrieve(context.Background(), "nomatch", []uint64{1})
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

	if _, err := r.Retrieve(context.Background(), "zebra", []uint64{9, 10}); err != nil {
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
	got, err := r.Retrieve(context.Background(), "zebra", nil)
	if err != nil || len(got) != 0 {
		t.Errorf("got %v, %v; want no results and no error", got, err)
	}
	rows := pgtest.Query[store.TokenUsage](t, "SELECT * FROM token_usages")
	if len(rows) != 1 || rows[0].ChannelID != "0" {
		t.Errorf("rows = %+v, want one row with channel 0", rows)
	}
}
