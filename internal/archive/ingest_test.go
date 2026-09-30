package archive

import (
	"strings"
	"testing"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
)

func TestIsChannelDenied(t *testing.T) {
	tests := []struct {
		name     string
		denyList string
		channel  snowflake.ID
		want     bool
	}{
		{"empty list", "", 100, false},
		{"single match", "100", 100, true},
		{"single miss", "200", 100, false},
		{"whitespace around entries", " 200 , 100 ,300", 100, true},
		{"empty entries ignored", ",,100,", 100, true},
		{"only blanks", " , ,", 100, false},
		{"prefix is not a match", "1000", 100, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isChannelDenied(tt.denyList, tt.channel); got != tt.want {
				t.Errorf("isChannelDenied(%q, %d) = %v, want %v", tt.denyList, tt.channel, got, tt.want)
			}
		})
	}
}

func TestFilter(t *testing.T) {
	human := discord.User{ID: 1}
	otherBot := discord.User{ID: 2, Bot: true}
	rick := discord.User{ID: 3, Bot: true}

	tests := []struct {
		name     string
		backfill bool
		author   discord.User
		content  string
		denyList string
		want     bool
	}{
		{"live human", false, human, "hi", "", true},
		{"backfill human", true, human, "hi", "", true},
		{"live rick archived", false, rick, "hi", "", true},
		{"backfill rick skipped", true, rick, "hi", "", false},
		{"live other bot skipped", false, otherBot, "hi", "", false},
		{"backfill other bot archived", true, otherBot, "hi", "", true},
		{"live slash prefix", false, human, "/cmd", "", false},
		{"backfill slash prefix", true, human, "/cmd", "", false},
		{"live rick slash prefix", false, rick, "/cmd", "", false},
		{"slash not at start", false, human, "a /cmd", "", true},
		{"live denied channel with whitespace", false, human, "hi", " 5 , 42 ", false},
		{"backfill denied channel with whitespace", true, human, "hi", " 5 , 42 ", false},
		{"live channel not denied", false, human, "hi", "5,6", true},
		{"backfill empty deny list", true, human, "hi", "", true},
		{"live empty content", false, human, "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := discord.Message{Author: tt.author, Content: tt.content}
			f := filter{denyList: tt.denyList}
			keep := f.keepLive
			if tt.backfill {
				keep = f.keepBackfill
			}
			if got := keep(msg, 42, rick.ID); got != tt.want {
				t.Errorf("keep() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestToStoredMessage(t *testing.T) {
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	tests := []struct {
		name           string
		content        string
		attachments    []discord.Attachment
		bot            bool
		wantContentLen int
		wantAttachment *string
	}{
		{name: "short content", content: "hello", wantContentLen: 5},
		{name: "exactly at limit", content: strings.Repeat("a", 10000), wantContentLen: 10000},
		{name: "one byte over limit", content: strings.Repeat("a", 10001), wantContentLen: 10000},
		{name: "bot author", content: "x", bot: true, wantContentLen: 1},
		{
			name:           "first attachment filename used",
			content:        "x",
			attachments:    []discord.Attachment{{Filename: "a.png"}, {Filename: "b.png"}},
			wantContentLen: 1,
			wantAttachment: new("a.png"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := discord.Message{
				ID:          10,
				Author:      discord.User{ID: 20, Username: "alice", Bot: tt.bot},
				Content:     tt.content,
				Attachments: tt.attachments,
				CreatedAt:   created,
			}
			got := toStoredMessage(msg, 30)

			if got.ID != 10 || got.ChannelID != 30 || got.AuthorID != 20 || got.AuthorName != "alice" {
				t.Errorf("identity fields = %+v", got)
			}
			if len(got.Content) != tt.wantContentLen {
				t.Errorf("content len = %d, want %d", len(got.Content), tt.wantContentLen)
			}
			if got.IsBot != tt.bot {
				t.Errorf("IsBot = %v, want %v", got.IsBot, tt.bot)
			}
			if !got.CreatedAt.Equal(created) {
				t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, created)
			}
			if got.ReplyToID != nil || got.EditedAt != nil {
				t.Errorf("ReplyToID/EditedAt = %v/%v, want nil", got.ReplyToID, got.EditedAt)
			}
			switch {
			case tt.wantAttachment == nil && got.Attachments != nil:
				t.Errorf("Attachments = %q, want nil", *got.Attachments)
			case tt.wantAttachment != nil && (got.Attachments == nil || *got.Attachments != *tt.wantAttachment):
				t.Errorf("Attachments = %v, want %q", got.Attachments, *tt.wantAttachment)
			}
		})
	}
}

func TestToStoredMessageUsesGivenChannel(t *testing.T) {
	msg := discord.Message{ChannelID: 1, Author: discord.User{ID: 2}}
	if got := toStoredMessage(msg, 99); got.ChannelID != 99 {
		t.Errorf("ChannelID = %d, want 99", got.ChannelID)
	}
}

func TestCompleteChunks(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	gap := 30 * time.Minute
	cutoff := now.Add(-gap)

	chunkEndingAt := func(id uint64, end time.Time) Chunk {
		return Chunk{FirstMessageID: id, EndedAt: end}
	}

	tests := []struct {
		name    string
		chunks  []Chunk
		wantIDs []uint64
	}{
		{"empty", nil, nil},
		{"last chunk inside gap dropped", []Chunk{
			chunkEndingAt(1, now.Add(-2*time.Hour)),
			chunkEndingAt(2, now.Add(-time.Minute)),
		}, []uint64{1}},
		{"last chunk at cutoff kept", []Chunk{
			chunkEndingAt(1, now.Add(-2*time.Hour)),
			chunkEndingAt(2, cutoff),
		}, []uint64{1, 2}},
		{"last chunk one nanosecond inside cutoff dropped", []Chunk{
			chunkEndingAt(1, now.Add(-2*time.Hour)),
			chunkEndingAt(2, cutoff.Add(time.Nanosecond)),
		}, []uint64{1}},
		{"all old kept", []Chunk{
			chunkEndingAt(1, now.Add(-3*time.Hour)),
			chunkEndingAt(2, now.Add(-2*time.Hour)),
		}, []uint64{1, 2}},
		{"single fresh chunk leaves nothing", []Chunk{
			chunkEndingAt(1, now),
		}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := completeChunks(tt.chunks, now, gap)
			if len(got) != len(tt.wantIDs) {
				t.Fatalf("len = %d, want %d", len(got), len(tt.wantIDs))
			}
			for i, c := range got {
				if c.FirstMessageID != tt.wantIDs[i] {
					t.Errorf("chunk %d id = %d, want %d", i, c.FirstMessageID, tt.wantIDs[i])
				}
			}
		})
	}
}
