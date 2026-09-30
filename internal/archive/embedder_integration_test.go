package archive

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/leikonga/doofus-rick/internal/llm"
	"github.com/leikonga/doofus-rick/internal/pgtest"
	"github.com/leikonga/doofus-rick/internal/store"
)

type fakeBatchEmbedClient struct {
	inputs [][]string
	short  bool
}

func (f *fakeBatchEmbedClient) Embed(_ context.Context, req llm.EmbeddingRequest) (llm.EmbeddingResponse, error) {
	f.inputs = append(f.inputs, req.Input)
	n := len(req.Input)
	if f.short {
		n--
	}
	embeddings := make([][]float32, n)
	for i := range embeddings {
		embeddings[i] = make([]float32, 1024)
		embeddings[i][0] = 1
	}
	return llm.EmbeddingResponse{Embeddings: embeddings, InputTokens: 7}, nil
}

type staticChannelNamer string

func (n staticChannelNamer) ChannelName(context.Context, uint64) string { return string(n) }

func seedEmbedChunk(t *testing.T, s *store.Store) store.Chunk {
	t.Helper()
	ctx := context.Background()
	at := time.Date(2025, 3, 14, 14, 32, 0, 0, time.UTC)
	for i, name := range []string{"alice", "bob"} {
		err := s.CreateMessage(ctx, store.Message{
			ID: uint64(10 + i), ChannelID: 5, AuthorID: uint64(i + 1), AuthorName: name, Content: "hello", CreatedAt: at,
		})
		if err != nil {
			t.Fatalf("CreateMessage: %v", err)
		}
	}
	chunk := store.Chunk{ID: 1, ChannelID: 5, Content: "[14:32 alice]: hello\n[14:32 bob]: hello\n", StartedAt: at, EndedAt: at, MessageCount: 2, FirstMessageID: 10, LastMessageID: 11}
	if err := s.CreateChunk(ctx, chunk); err != nil {
		t.Fatalf("CreateChunk: %v", err)
	}
	return chunk
}

func TestEmbedderSendsHeaderAndStoresCurrentVersion(t *testing.T) {
	s := pgtest.Store(t)
	chunk := seedEmbedChunk(t, s)
	client := &fakeBatchEmbedClient{}
	e := NewEmbedder(EmbeddingConfig{Model: "m"}, s, client, NewChunker(ChunkConfig{}, nil), staticChannelNamer("general"))

	if err := e.EmbedChunks(context.Background(), []store.Chunk{chunk}); err != nil {
		t.Fatal(err)
	}

	want := "Channel #general, 2025-03-14 (Friday). Participants: alice, bob.\n" + chunk.Content
	if len(client.inputs) != 1 || len(client.inputs[0]) != 1 || client.inputs[0][0] != want {
		t.Fatalf("embedding input = %q, want %q", client.inputs, want)
	}
	if got := pgtest.Query[int](t, "SELECT version FROM chunk_embeddings WHERE chunk_id = 1 AND model = 'm'"); len(got) != 1 || got[0] != store.CurrentEmbedVersion {
		t.Fatalf("versions = %v, want [%d]", got, store.CurrentEmbedVersion)
	}
	if got := pgtest.Query[string](t, "SELECT content FROM chunks WHERE id = 1"); got[0] != chunk.Content {
		t.Fatalf("stored content changed: %q", got[0])
	}
	if got := pgtest.Query[int64](t, "SELECT input_tokens FROM token_usages WHERE user_id = 'embedder'"); len(got) != 1 || got[0] != 7 {
		t.Fatalf("token usage = %v, want [7]", got)
	}
}

func TestEmbedderReplacesStaleEmbedding(t *testing.T) {
	s := pgtest.Store(t)
	chunk := seedEmbedChunk(t, s)
	ctx := context.Background()
	old := store.ChunkEmbedding{ChunkID: chunk.ID, Model: "m", Embedding: store.HalfVector(make([]float32, 1024)), Version: 1}
	if err := s.SaveChunkEmbedding(ctx, old); err != nil {
		t.Fatal(err)
	}
	e := NewEmbedder(EmbeddingConfig{Model: "m"}, s, &fakeBatchEmbedClient{}, NewChunker(ChunkConfig{}, nil), nil)

	stale, err := s.GetChunksWithoutEmbedding(ctx, "m", 10)
	if err != nil || len(stale) != 1 {
		t.Fatalf("stale = %v, err = %v, want 1 chunk", stale, err)
	}
	if err := e.EmbedChunks(ctx, stale); err != nil {
		t.Fatal(err)
	}
	if stale, _ = s.GetChunksWithoutEmbedding(ctx, "m", 10); len(stale) != 0 {
		t.Fatalf("got %d stale chunks after re-embed, want 0", len(stale))
	}
	if got := pgtest.Query[int](t, "SELECT count(*)::int FROM chunk_embeddings"); got[0] != 1 {
		t.Fatalf("got %d embedding rows, want 1", got[0])
	}
}

func TestEmbedderRejectsShortResponse(t *testing.T) {
	s := pgtest.Store(t)
	chunk := seedEmbedChunk(t, s)
	e := NewEmbedder(EmbeddingConfig{Model: "m"}, s, &fakeBatchEmbedClient{short: true}, NewChunker(ChunkConfig{}, nil), nil)

	err := e.EmbedChunks(context.Background(), []store.Chunk{chunk})
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("err = %v, want count mismatch", err)
	}
}
