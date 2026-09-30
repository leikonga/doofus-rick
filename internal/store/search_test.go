package store_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/leikonga/doofus-rick/internal/pgtest"
	"github.com/leikonga/doofus-rick/internal/store"
)

const searchModel = "test/model"

func axisVector(axis int) []float32 {
	v := make([]float32, 1024)
	v[axis] = 1
	return v
}

func seedSearchChunk(t *testing.T, s *store.Store, id, channel uint64, content string, axis int, start, end time.Time) {
	t.Helper()
	ctx := context.Background()
	chunk := store.Chunk{ID: id, ChannelID: channel, Content: content, StartedAt: start, EndedAt: end, MessageCount: 1, FirstMessageID: id, LastMessageID: id}
	if err := s.CreateChunk(ctx, chunk); err != nil {
		t.Fatalf("CreateChunk: %v", err)
	}
	if axis >= 0 {
		emb := store.ChunkEmbedding{ChunkID: id, Model: searchModel, Embedding: store.HalfVector(axisVector(axis))}
		if err := s.SaveChunkEmbedding(ctx, emb); err != nil {
			t.Fatalf("SaveChunkEmbedding: %v", err)
		}
	}
}

func searchIDs(chunks []store.ScoredChunk) []uint64 {
	out := make([]uint64, len(chunks))
	for i, c := range chunks {
		out[i] = c.ID
	}
	slices.Sort(out)
	return out
}

func baseSearch() store.ChunkSearch {
	return store.ChunkSearch{Vector: axisVector(0), Query: "zebra", ChannelIDs: []uint64{1}, Model: searchModel, TopK: 10}
}

func TestSearchChunksSimilarityFloor(t *testing.T) {
	s := pgtest.Store(t)
	now := time.Now()
	seedSearchChunk(t, s, 1, 1, "zebra crossing", 0, now, now)
	seedSearchChunk(t, s, 2, 1, "zebra herd", -1, now, now)
	seedSearchChunk(t, s, 3, 1, "unrelated", 5, now, now)

	q := baseSearch()
	got, err := s.SearchChunks(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if want := []uint64{1, 2, 3}; !slices.Equal(searchIDs(got), want) {
		t.Fatalf("no floor: got %v, want %v", searchIDs(got), want)
	}

	q.MinSimilarity = 0.5
	got, err = s.SearchChunks(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if want := []uint64{1, 2}; !slices.Equal(searchIDs(got), want) {
		t.Fatalf("floor: got %v, want %v", searchIDs(got), want)
	}
	for _, c := range got {
		switch c.ID {
		case 1:
			if c.Similarity < 0.99 {
				t.Errorf("chunk 1 similarity = %v, want about 1", c.Similarity)
			}
		case 2:
			if c.Similarity != 0 {
				t.Errorf("lexical only chunk similarity = %v, want 0", c.Similarity)
			}
		}
	}
}

func TestSearchChunksAuthorFilter(t *testing.T) {
	s := pgtest.Store(t)
	now := time.Now().Truncate(time.Microsecond)
	seedSearchChunk(t, s, 10, 1, "zebra one", 0, now, now)
	seedSearchChunk(t, s, 20, 1, "zebra two", 0, now, now)
	seedSearchChunk(t, s, 30, 2, "zebra three", 0, now, now)
	mustCreateMessages(t, s,
		store.Message{ID: 10, ChannelID: 1, AuthorID: 7, AuthorName: "a", Content: "zebra", CreatedAt: now},
		store.Message{ID: 20, ChannelID: 1, AuthorID: 8, AuthorName: "b", Content: "zebra", CreatedAt: now},
		store.Message{ID: 30, ChannelID: 2, AuthorID: 7, AuthorName: "a", Content: "zebra", CreatedAt: now},
	)

	author := uint64(7)
	q := baseSearch()
	q.AuthorID = &author
	got, err := s.SearchChunks(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if want := []uint64{10}; !slices.Equal(searchIDs(got), want) {
		t.Fatalf("got %v, want %v", searchIDs(got), want)
	}

	q.Vector = axisVector(5)
	q.Query = "nomatch"
	got, err = s.SearchChunks(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if want := []uint64{10}; !slices.Equal(searchIDs(got), want) {
		t.Fatalf("vector leg only: got %v, want %v", searchIDs(got), want)
	}
}

func TestSearchChunksTimeFilter(t *testing.T) {
	s := pgtest.Store(t)
	day := func(d int) time.Time { return time.Date(2026, 1, d, 12, 0, 0, 0, time.UTC) }
	seedSearchChunk(t, s, 1, 1, "zebra early", 0, day(1), day(2))
	seedSearchChunk(t, s, 2, 1, "zebra middle", 0, day(10), day(11))
	seedSearchChunk(t, s, 3, 1, "zebra late", 0, day(20), day(21))

	tests := []struct {
		name         string
		since, until *time.Time
		want         []uint64
	}{
		{"since", ptr(day(10)), nil, []uint64{2, 3}},
		{"until", nil, ptr(day(10)), []uint64{1}},
		{"range", ptr(day(5)), ptr(day(15)), []uint64{2}},
		{"overlap", ptr(day(11).Add(-time.Hour)), ptr(day(20).Add(time.Hour)), []uint64{2, 3}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := baseSearch()
			q.Since, q.Until = tt.since, tt.until
			got, err := s.SearchChunks(context.Background(), q)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(searchIDs(got), tt.want) {
				t.Fatalf("got %v, want %v", searchIDs(got), tt.want)
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }

func TestFindAuthorsByName(t *testing.T) {
	s := pgtest.Store(t)
	now := time.Now()
	mustCreateMessages(t, s,
		store.Message{ID: 1, ChannelID: 1, AuthorID: 7, AuthorName: "Klaus", Content: "c", CreatedAt: now},
		store.Message{ID: 2, ChannelID: 1, AuthorID: 8, AuthorName: "klaus", Content: "c", CreatedAt: now},
		store.Message{ID: 3, ChannelID: 1, AuthorID: 9, AuthorName: "hans", Content: "c", CreatedAt: now},
	)
	got, err := s.FindAuthorsByName(context.Background(), "KLAUS")
	if err != nil || len(got) != 2 {
		t.Fatalf("got %+v, %v; want 2 authors", got, err)
	}
	got, err = s.FindAuthorsByName(context.Background(), "hans")
	if err != nil || len(got) != 1 || got[0].AuthorID != 9 {
		t.Fatalf("got %+v, %v; want author 9", got, err)
	}
	got, err = s.FindAuthorsByName(context.Background(), "nobody")
	if err != nil || len(got) != 0 {
		t.Fatalf("got %+v, %v; want none", got, err)
	}
}

func TestSearchChunksKeywordQuery(t *testing.T) {
	s := pgtest.Store(t)
	now := time.Now()
	seedSearchChunk(t, s, 1, 1, "zebra crossing", -1, now, now)
	seedSearchChunk(t, s, 2, 1, "giraffe neck", -1, now, now)

	q := baseSearch()
	q.Query = "something about animals"
	q.KeywordQuery = "giraffe"
	got, err := s.SearchChunks(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if ids := searchIDs(got); !slices.Equal(ids, []uint64{2}) {
		t.Errorf("keyword query ids = %v, want [2]", ids)
	}

	q.KeywordQuery = ""
	q.Query = "zebra"
	got, err = s.SearchChunks(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if ids := searchIDs(got); !slices.Equal(ids, []uint64{1}) {
		t.Errorf("default ids = %v, want [1]", ids)
	}
}
