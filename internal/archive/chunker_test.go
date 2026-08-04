package archive

import (
	"testing"
	"time"

	"github.com/leikonga/doofus-rick/internal/store"
)

func msgAt(id uint64, t time.Time, content string) store.Message {
	return store.Message{
		ID:         id,
		ChannelID:  1,
		AuthorID:   1,
		AuthorName: "user",
		Content:    content,
		CreatedAt:  t,
	}
}

func TestChunkMessages_Empty(t *testing.T) {
	c := NewChunker(ChunkConfig{}, nil)
	if got := c.ChunkMessages(nil); got != nil {
		t.Fatalf("expected nil chunks for empty input, got %v", got)
	}
}

func TestChunkMessages_SingleChunkWhenWithinLimits(t *testing.T) {
	c := NewChunker(ChunkConfig{ChunkGap: time.Hour, ChunkMaxMsgs: 10, ChunkMaxChars: 1000}, nil)
	base := time.Now()
	msgs := []store.Message{
		msgAt(1, base, "hey"),
		msgAt(2, base.Add(time.Minute), "sup"),
		msgAt(3, base.Add(2*time.Minute), "nm"),
	}

	chunks := c.ChunkMessages(msgs)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if len(chunks[0].Messages) != 3 {
		t.Fatalf("expected 3 messages in chunk, got %d", len(chunks[0].Messages))
	}
	if chunks[0].FirstMessageID != 1 || chunks[0].LastMessageID != 3 {
		t.Fatalf("unexpected first/last message id: %d/%d", chunks[0].FirstMessageID, chunks[0].LastMessageID)
	}
}

func TestChunkMessages_SplitsOnGap(t *testing.T) {
	c := NewChunker(ChunkConfig{ChunkGap: 10 * time.Minute, ChunkMaxMsgs: 100, ChunkMaxChars: 10000}, nil)
	base := time.Now()
	msgs := []store.Message{
		msgAt(1, base, "a"),
		msgAt(2, base.Add(time.Minute), "b"),
		msgAt(3, base.Add(20*time.Minute), "c"), // gap > 10m, new chunk
		msgAt(4, base.Add(21*time.Minute), "d"),
	}

	chunks := c.ChunkMessages(msgs)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if len(chunks[0].Messages) != 2 || len(chunks[1].Messages) != 2 {
		t.Fatalf("expected 2+2 messages, got %d+%d", len(chunks[0].Messages), len(chunks[1].Messages))
	}
}

func TestChunkMessages_SplitsOnMaxMsgs(t *testing.T) {
	c := NewChunker(ChunkConfig{ChunkGap: time.Hour, ChunkMaxMsgs: 2, ChunkMaxChars: 10000}, nil)
	base := time.Now()
	msgs := []store.Message{
		msgAt(1, base, "a"),
		msgAt(2, base.Add(time.Second), "b"),
		msgAt(3, base.Add(2*time.Second), "c"),
	}

	chunks := c.ChunkMessages(msgs)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if len(chunks[0].Messages) != 2 {
		t.Fatalf("expected first chunk capped at 2 messages, got %d", len(chunks[0].Messages))
	}
	if len(chunks[1].Messages) != 1 {
		t.Fatalf("expected second chunk to hold the overflow message, got %d", len(chunks[1].Messages))
	}
}

func TestChunkMessages_SplitsOnMaxChars(t *testing.T) {
	c := NewChunker(ChunkConfig{ChunkGap: time.Hour, ChunkMaxMsgs: 100, ChunkMaxChars: 10}, nil)
	base := time.Now()
	msgs := []store.Message{
		msgAt(1, base, "12345"),
		msgAt(2, base.Add(time.Second), "12345"),   // 5+5=10, still fits
		msgAt(3, base.Add(2*time.Second), "12345"), // 10+5=15 > 10, new chunk
	}

	chunks := c.ChunkMessages(msgs)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if len(chunks[0].Messages) != 2 {
		t.Fatalf("expected first chunk to hold 2 messages under the char cap, got %d", len(chunks[0].Messages))
	}
	if len(chunks[1].Messages) != 1 {
		t.Fatalf("expected second chunk to hold the overflow message, got %d", len(chunks[1].Messages))
	}
}

func TestChunkMessages_AccumulatesCharsAcrossMessages(t *testing.T) {
	// Regression test: char accounting must track the running total of the
	// current chunk, not just the length of the incoming message.
	c := NewChunker(ChunkConfig{ChunkGap: time.Hour, ChunkMaxMsgs: 100, ChunkMaxChars: 12}, nil)
	base := time.Now()
	msgs := []store.Message{
		msgAt(1, base, "1234567"),
		msgAt(2, base.Add(time.Second), "1234567"),
	}

	chunks := c.ChunkMessages(msgs)
	if len(chunks) != 2 {
		t.Fatalf("expected the second message to start a new chunk once the running total exceeds the cap, got %d chunks", len(chunks))
	}
}

func TestChunkMessages_MessageCountMatchesMessages(t *testing.T) {
	// Regression test: an internal counter previously never incremented,
	// leaving ChunkMessages unable to close or emit any chunk at all.
	c := NewChunker(ChunkConfig{ChunkGap: time.Hour, ChunkMaxMsgs: 100, ChunkMaxChars: 10000}, nil)
	base := time.Now()
	msgs := []store.Message{
		msgAt(1, base, "a"),
		msgAt(2, base.Add(time.Second), "b"),
	}

	chunks := c.ChunkMessages(msgs)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if len(chunks[0].Messages) != 2 {
		t.Fatalf("expected chunk to contain both messages, got %d", len(chunks[0].Messages))
	}
}

func TestBuildChunkContent(t *testing.T) {
	c := NewChunker(ChunkConfig{}, nil)
	base := time.Date(2026, 1, 1, 12, 30, 0, 0, time.UTC)
	chunk := Chunk{
		Messages: []store.Message{
			msgAt(1, base, "hi"),
			msgAt(2, base.Add(time.Minute), "there"),
		},
	}

	got := c.BuildChunkContent(chunk)
	want := "[12:30 user]: hi\n[12:31 user]: there\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
