package store_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/leikonga/doofus-rick/internal/pgtest"
	"github.com/leikonga/doofus-rick/internal/store"
	"gorm.io/gorm"
)

func msg(id, channel uint64, at time.Time) store.Message {
	return store.Message{ID: id, ChannelID: channel, AuthorID: 7, AuthorName: "a", Content: "c", CreatedAt: at}
}

func messageIDs(msgs []store.Message) []uint64 {
	out := make([]uint64, len(msgs))
	for i, m := range msgs {
		out[i] = m.ID
	}
	return out
}

func mustCreateMessages(t *testing.T, s *store.Store, msgs ...store.Message) {
	t.Helper()
	for _, m := range msgs {
		if err := s.CreateMessage(context.Background(), m); err != nil {
			t.Fatalf("CreateMessage %d: %v", m.ID, err)
		}
	}
}

func TestMessageCreate(t *testing.T) {
	s := pgtest.Store(t)
	ctx := context.Background()
	at := time.Now().Add(-time.Minute).Truncate(time.Microsecond)
	mustCreateMessages(t, s, msg(1, 5, at))

	got := pgtest.Query[store.Message](t, s, "SELECT * FROM messages WHERE channel_id = ? AND id > ? ORDER BY id", 5, 0)
	if len(got) != 1 || got[0].ID != 1 || got[0].Content != "c" || got[0].AuthorName != "a" || !got[0].CreatedAt.Equal(at) {
		t.Errorf("unexpected: %+v", got)
	}
	if err := s.CreateMessage(ctx, msg(1, 5, at)); err == nil {
		t.Error("duplicate primary key should error")
	}
}

func TestForgetAuthor(t *testing.T) {
	s := pgtest.Store(t)
	ctx := context.Background()

	forgotten, err := s.IsAuthorForgotten(ctx, 77)
	if err != nil || forgotten {
		t.Fatalf("before: %v, %v", forgotten, err)
	}
	if err := s.ForgetAuthor(ctx, 77); err != nil {
		t.Fatal(err)
	}
	forgotten, err = s.IsAuthorForgotten(ctx, 77)
	if err != nil || !forgotten {
		t.Fatalf("after: %v, %v", forgotten, err)
	}
	if other, _ := s.IsAuthorForgotten(ctx, 78); other {
		t.Error("other author reported forgotten")
	}
	if err := s.ForgetAuthor(ctx, 77); err == nil {
		t.Error("forgetting twice should error on duplicate key")
	}
}

func TestGetRecentMessagesSince(t *testing.T) {
	s := pgtest.Store(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Microsecond)
	bot := msg(4, 1, now.Add(-time.Minute))
	bot.IsBot = true
	mustCreateMessages(t, s,
		msg(1, 1, now.Add(-time.Hour)),
		msg(2, 1, now.Add(-10*time.Minute)),
		msg(3, 1, now.Add(-5*time.Minute)),
		bot,
		msg(5, 2, now.Add(-time.Minute)),
	)

	tests := []struct {
		name    string
		channel uint64
		since   time.Time
		limit   int
		want    []uint64
	}{
		{"channel and time filter, includes bots", 1, now.Add(-30 * time.Minute), 10, []uint64{2, 3, 4}},
		{"limit keeps oldest", 1, now.Add(-30 * time.Minute), 2, []uint64{2, 3}},
		{"since is exclusive", 1, now.Add(-10 * time.Minute), 10, []uint64{3, 4}},
		{"other channel", 2, now.Add(-30 * time.Minute), 10, []uint64{5}},
		{"nothing recent", 1, now, 10, []uint64{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.GetRecentMessagesSince(ctx, tt.channel, tt.since, tt.limit)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(messageIDs(got), tt.want) {
				t.Errorf("got %v, want %v", messageIDs(got), tt.want)
			}
		})
	}
}

func TestChunkingQueries(t *testing.T) {
	s := pgtest.Store(t)
	ctx := context.Background()
	now := time.Now()
	for id := uint64(1); id <= 5; id++ {
		mustCreateMessages(t, s, msg(id, 1, now))
	}
	mustCreateMessages(t, s, msg(100, 2, now))

	channels, err := s.GetChannelsWithUnchunkedMessages(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(channels)
	if !slices.Equal(channels, []uint64{1, 2}) {
		t.Errorf("channels before chunk = %v", channels)
	}
	if last, err := s.GetLastChunkedMessageID(ctx, 1); err != nil || last != 0 {
		t.Errorf("last before chunk = %d, %v", last, err)
	}

	chunk := store.Chunk{ChannelID: 1, Content: "x", StartedAt: now, EndedAt: now, MessageCount: 3, FirstMessageID: 1, LastMessageID: 3}
	if err := s.CreateChunk(ctx, chunk); err != nil {
		t.Fatal(err)
	}

	last, err := s.GetLastChunkedMessageID(ctx, 1)
	if err != nil || last != 3 {
		t.Errorf("last after chunk = %d, %v", last, err)
	}
	if other, _ := s.GetLastChunkedMessageID(ctx, 2); other != 0 {
		t.Errorf("channel 2 last = %d, want 0", other)
	}

	unchunked, err := s.GetUnchunkedMessages(ctx, 1, last, 10)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(messageIDs(unchunked), []uint64{4, 5}) {
		t.Errorf("unchunked since last = %v", messageIDs(unchunked))
	}
	limited, _ := s.GetUnchunkedMessages(ctx, 1, last, 1)
	if !slices.Equal(messageIDs(limited), []uint64{4}) {
		t.Errorf("limited = %v", messageIDs(limited))
	}

	fromZero, err := s.GetUnchunkedMessages(ctx, 1, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(messageIDs(fromZero), []uint64{1, 2, 4, 5}) {
		t.Errorf("unchunked from zero = %v; only the chunk's last message id is excluded", messageIDs(fromZero))
	}

	channels, err = s.GetChannelsWithUnchunkedMessages(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(channels)
	if !slices.Equal(channels, []uint64{1, 2}) {
		t.Errorf("channels after partial chunk = %v", channels)
	}

	rest := store.Chunk{ChannelID: 1, Content: "y", StartedAt: now, EndedAt: now, MessageCount: 2, FirstMessageID: 4, LastMessageID: 5}
	if err := s.CreateChunk(ctx, rest); err != nil {
		t.Fatal(err)
	}
	channels, err = s.GetChannelsWithUnchunkedMessages(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(channels, []uint64{2}) {
		t.Errorf("channels after full chunk = %v", channels)
	}
	if last, _ := s.GetLastChunkedMessageID(ctx, 1); last != 5 {
		t.Errorf("last = %d, want 5", last)
	}
	if none, _ := s.GetUnchunkedMessages(ctx, 1, 5, 10); len(none) != 0 {
		t.Errorf("unchunked = %v, want none", messageIDs(none))
	}
	limitedChannels, _ := s.GetChannelsWithUnchunkedMessages(ctx, 0)
	if len(limitedChannels) != 0 {
		t.Errorf("limit 0 returned %v", limitedChannels)
	}
}

func TestBackfillState(t *testing.T) {
	s := pgtest.Store(t)
	ctx := context.Background()

	if _, err := s.GetBackfillState(ctx); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("GetBackfillState on empty = %v", err)
	}
	first, err := s.GetOrCreateBackfillState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != 1 || first.Status != "idle" {
		t.Errorf("first = %+v", first)
	}
	second, err := s.GetOrCreateBackfillState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != 1 || second.Status != "idle" || !second.UpdatedAt.Equal(first.UpdatedAt.Truncate(time.Microsecond)) {
		t.Errorf("second = %+v, first = %+v", second, first)
	}
	if count := pgtest.Query[int64](t, s, "SELECT count(*) FROM backfill_states")[0]; count != 1 {
		t.Errorf("rows = %d, want 1", count)
	}
}

func TestSeedBackfillChannels(t *testing.T) {
	s := pgtest.Store(t)
	ctx := context.Background()

	n, err := s.SeedBackfillChannels(ctx, nil)
	if err != nil || n != 0 {
		t.Fatalf("empty seed = %d, %v", n, err)
	}
	n, err = s.SeedBackfillChannels(ctx, []uint64{1, 2, 3})
	if err != nil || n != 3 {
		t.Fatalf("first seed = %d, %v", n, err)
	}
	ch, err := s.GetBackfillChannel(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	ch.Done = true
	if err := s.SaveBackfillChannel(ctx, ch); err != nil {
		t.Fatal(err)
	}

	n, err = s.SeedBackfillChannels(ctx, []uint64{2, 3, 4, 5})
	if err != nil || n != 4 {
		t.Fatalf("overlapping seed count = %d (batch size, not inserted count), err %v", n, err)
	}
	if rows := pgtest.Query[int64](t, s, "SELECT count(*) FROM backfill_channels")[0]; rows != 5 {
		t.Fatalf("rows = %d, want 5", rows)
	}
	n, err = s.SeedBackfillChannels(ctx, []uint64{1, 5})
	if err != nil || n != 0 {
		t.Fatalf("repeat seed = %d, %v", n, err)
	}
	kept, err := s.GetBackfillChannel(ctx, 2)
	if err != nil || !kept.Done {
		t.Errorf("existing row modified: %+v, %v", kept, err)
	}
	pending, err := s.GetBackfillChannels(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 4 {
		t.Errorf("pending = %d, want 4 (done channel excluded)", len(pending))
	}
}

func TestGetBackfillChannelMissing(t *testing.T) {
	s := pgtest.Store(t)
	ch, err := s.GetBackfillChannel(context.Background(), 404)
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("err = %v, want gorm.ErrRecordNotFound", err)
	}
	if ch == nil {
		t.Error("returns non-nil empty channel alongside the error")
	}
}
