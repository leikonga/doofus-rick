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
		msgAt(3, base.Add(20*time.Minute), "c"),
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
		msgAt(2, base.Add(time.Second), "12345"),
		msgAt(3, base.Add(2*time.Second), "12345"),
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

type mockResolver map[string]string

func (m mockResolver) GetDisplayNameForID(id string) (string, error) {
	if name, ok := m[id]; ok {
		return name, nil
	}
	return "", nil
}

func TestBuildChunkContent_PrefersCurrentDisplayName(t *testing.T) {
	c := NewChunker(ChunkConfig{}, mockResolver{"1": "nickname"})
	base := time.Date(2026, 1, 1, 12, 30, 0, 0, time.UTC)
	chunk := Chunk{Messages: []store.Message{msgAt(1, base, "hi")}}

	got := c.BuildChunkContent(chunk)
	want := "[12:30 nickname]: hi\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestBuildChunkContent_FallsBackToStoredNameWhenUnresolved(t *testing.T) {
	c := NewChunker(ChunkConfig{}, mockResolver{})
	base := time.Date(2026, 1, 1, 12, 30, 0, 0, time.UTC)
	chunk := Chunk{Messages: []store.Message{msgAt(1, base, "hi")}}

	got := c.BuildChunkContent(chunk)
	want := "[12:30 user]: hi\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestBuildEmbedText(t *testing.T) {
	start := time.Date(2025, 3, 14, 14, 32, 0, 0, time.UTC)
	named := func(id uint64, name string) store.Message {
		m := msgAt(id, start, "x")
		m.AuthorID = id
		m.AuthorName = name
		return m
	}
	chunk := store.Chunk{StartedAt: start, Content: "[14:32 Alice]: hi\n"}

	tests := []struct {
		name     string
		resolver DisplayNameResolver
		messages []store.Message
		channel  string
		want     string
	}{
		{
			name:     "channel date and participants in first appearance order",
			messages: []store.Message{named(2, "Bob"), named(1, "Alice"), named(2, "Bob"), named(3, "Carol")},
			channel:  "general",
			want:     "Channel #general, 2025-03-14 (Friday). Participants: Bob, Alice, Carol.\n[14:32 Alice]: hi\n",
		},
		{
			name:     "missing channel name omits channel",
			messages: []store.Message{named(1, "Alice")},
			want:     "2025-03-14 (Friday). Participants: Alice.\n[14:32 Alice]: hi\n",
		},
		{
			name:    "no messages omits participants",
			channel: "general",
			want:    "Channel #general, 2025-03-14 (Friday).\n[14:32 Alice]: hi\n",
		},
		{
			name:     "resolver names win and dedupe",
			resolver: mockResolver{"1": "Nick", "2": "Nick"},
			messages: []store.Message{named(1, "Alice"), named(2, "Bob"), named(3, "Carol")},
			want:     "2025-03-14 (Friday). Participants: Nick, Carol.\n[14:32 Alice]: hi\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewChunker(ChunkConfig{}, tt.resolver)
			if got := c.BuildEmbedText(chunk, tt.messages, tt.channel); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
