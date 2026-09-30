package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/leikonga/doofus-rick/internal/pgtest"
	"github.com/leikonga/doofus-rick/internal/store"
)

func saveEmbeddingVersion(t *testing.T, s *store.Store, id uint64, axis, version int) {
	t.Helper()
	emb := store.ChunkEmbedding{ChunkID: id, Model: searchModel, Embedding: store.HalfVector(axisVector(axis)), Version: version}
	if err := s.SaveChunkEmbedding(context.Background(), emb); err != nil {
		t.Fatalf("SaveChunkEmbedding: %v", err)
	}
}

func TestGetChunksWithoutEmbeddingHonorsVersion(t *testing.T) {
	s := pgtest.Store(t)
	now := time.Now()
	seedSearchChunk(t, s, 1, 1, "missing", -1, now, now)
	seedSearchChunk(t, s, 2, 1, "old", -1, now, now)
	seedSearchChunk(t, s, 3, 1, "current", -1, now, now)
	seedSearchChunk(t, s, 4, 1, "other model", -1, now, now)
	saveEmbeddingVersion(t, s, 2, 1, 1)
	saveEmbeddingVersion(t, s, 3, 2, store.CurrentEmbedVersion)
	other := store.ChunkEmbedding{ChunkID: 4, Model: "other", Embedding: store.HalfVector(axisVector(3)), Version: store.CurrentEmbedVersion}
	if err := s.SaveChunkEmbedding(context.Background(), other); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetChunksWithoutEmbedding(context.Background(), searchModel, 10)
	if err != nil {
		t.Fatal(err)
	}
	if ids := searchIDsOf(got); len(ids) != 3 || ids[0] != 1 || ids[1] != 2 || ids[2] != 4 {
		t.Fatalf("got %v, want [1 2 4]", ids)
	}
	n, err := s.CountChunksWithoutEmbedding(context.Background(), searchModel)
	if err != nil || n != 3 {
		t.Fatalf("count = %d, err = %v, want 3", n, err)
	}
}

func searchIDsOf(chunks []store.Chunk) []uint64 {
	out := make([]uint64, len(chunks))
	for i, c := range chunks {
		out[i] = c.ID
	}
	return out
}

func TestSaveChunkEmbeddingUpserts(t *testing.T) {
	s := pgtest.Store(t)
	now := time.Now()
	seedSearchChunk(t, s, 1, 1, "zebra", 0, now, now)
	saveEmbeddingVersion(t, s, 1, 1, store.CurrentEmbedVersion)

	if got := pgtest.Query[int](t, "SELECT count(*)::int FROM chunk_embeddings"); got[0] != 1 {
		t.Fatalf("got %d rows, want 1", got[0])
	}
	if got := pgtest.Query[int](t, "SELECT version FROM chunk_embeddings WHERE chunk_id = 1"); got[0] != store.CurrentEmbedVersion {
		t.Fatalf("version = %d, want %d", got[0], store.CurrentEmbedVersion)
	}
	q := baseSearch()
	q.Vector = axisVector(1)
	got, err := s.SearchChunks(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if ids := searchIDs(got); len(ids) != 1 || ids[0] != 1 {
		t.Fatalf("search by new vector = %v, want [1]", ids)
	}
}

func TestSearchFindsChunksAcrossEmbeddingVersions(t *testing.T) {
	s := pgtest.Store(t)
	now := time.Now()
	seedSearchChunk(t, s, 1, 1, "zebra old", -1, now, now)
	seedSearchChunk(t, s, 2, 1, "zebra new", -1, now, now)
	saveEmbeddingVersion(t, s, 1, 0, 1)
	saveEmbeddingVersion(t, s, 2, 0, store.CurrentEmbedVersion)

	got, err := s.SearchChunks(context.Background(), baseSearch())
	if err != nil {
		t.Fatal(err)
	}
	if ids := searchIDs(got); len(ids) != 2 || ids[0] != 1 || ids[1] != 2 {
		t.Fatalf("got %v, want [1 2]", ids)
	}
}
