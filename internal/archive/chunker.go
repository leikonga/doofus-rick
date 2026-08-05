package archive

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/leikonga/doofus-rick/internal/store"
)

const (
	DefaultChunkGap      = 10 * time.Minute
	DefaultChunkMaxMsgs  = 15
	DefaultChunkMaxChars = 2000
)

type ChunkConfig struct {
	ChunkGap      time.Duration
	ChunkMaxMsgs  int
	ChunkMaxChars int
}

// UsernameResolver resolves a Discord user ID to its current display name
// (nickname/global name), so archived chunks read naturally instead of
// showing the stale raw username captured at message ingestion time.
type UsernameResolver interface {
	GetUsernameForID(id string) (string, error)
}

type Chunker struct {
	config   ChunkConfig
	store    *store.Store
	resolver UsernameResolver
}

func NewChunker(config ChunkConfig, s *store.Store, resolver UsernameResolver) *Chunker {
	if config.ChunkGap == 0 {
		config.ChunkGap = DefaultChunkGap
	}
	if config.ChunkMaxMsgs == 0 {
		config.ChunkMaxMsgs = DefaultChunkMaxMsgs
	}
	if config.ChunkMaxChars == 0 {
		config.ChunkMaxChars = DefaultChunkMaxChars
	}
	return &Chunker{config: config, store: s, resolver: resolver}
}

type Chunk struct {
	ChannelID      uint64
	Messages       []store.Message
	StartedAt      time.Time
	EndedAt        time.Time
	FirstMessageID uint64
	LastMessageID  uint64
	Content        string
}

func (c *Chunker) ChunkMessages(messages []store.Message) []Chunk {
	if len(messages) == 0 {
		return nil
	}

	var chunks []Chunk
	var current Chunk
	var currentChars int
	var lastTime time.Time

	for i, msg := range messages {
		startNew := i == 0
		if !startNew {
			gap := msg.CreatedAt.Sub(lastTime)
			if gap > c.config.ChunkGap || len(current.Messages) >= c.config.ChunkMaxMsgs || currentChars+len(msg.Content) > c.config.ChunkMaxChars {
				chunks = append(chunks, current)
				startNew = true
			}
		}

		if startNew {
			current = Chunk{
				ChannelID:      msg.ChannelID,
				Messages:       []store.Message{msg},
				StartedAt:      msg.CreatedAt,
				EndedAt:        msg.CreatedAt,
				FirstMessageID: msg.ID,
				LastMessageID:  msg.ID,
			}
			currentChars = len(msg.Content)
		} else {
			current.Messages = append(current.Messages, msg)
			current.EndedAt = msg.CreatedAt
			current.LastMessageID = msg.ID
			currentChars += len(msg.Content)
		}

		lastTime = msg.CreatedAt
	}

	if len(current.Messages) > 0 {
		chunks = append(chunks, current)
	}

	return chunks
}

func (c *Chunker) BuildChunkContent(chunk Chunk) string {
	var content strings.Builder
	for _, msg := range chunk.Messages {
		ts := msg.CreatedAt.Format("15:04")
		content.WriteString("[" + ts + " " + c.displayName(msg) + "]: " + msg.Content + "\n")
	}
	return content.String()
}

// displayName prefers the author's current nickname/global name over the
// raw username baked into msg.AuthorName at ingestion time, since chats
// referring to someone by their old handle read as wrong once they rename.
func (c *Chunker) displayName(msg store.Message) string {
	if c.resolver == nil {
		return msg.AuthorName
	}
	name, err := c.resolver.GetUsernameForID(strconv.FormatUint(msg.AuthorID, 10))
	if err != nil || name == "" {
		return msg.AuthorName
	}
	return name
}

func (c *Chunker) ChunkAndSave(ctx context.Context, messages []store.Message) error {
	chunks := c.ChunkMessages(messages)
	for _, chunk := range chunks {
		chunk.Content = c.BuildChunkContent(chunk)
		storedChunk := store.Chunk{
			ChannelID:      chunk.ChannelID,
			Content:        chunk.Content,
			StartedAt:      chunk.StartedAt,
			EndedAt:        chunk.EndedAt,
			MessageCount:   len(chunk.Messages),
			FirstMessageID: chunk.FirstMessageID,
			LastMessageID:  chunk.LastMessageID,
		}
		if err := c.store.CreateChunk(ctx, storedChunk); err != nil {
			return err
		}
	}
	return nil
}

func (c *Chunker) ChunkMessagesIncremental(ctx context.Context, messages []store.Message) error {
	if len(messages) == 0 {
		return nil
	}

	return c.ChunkAndSave(ctx, messages)
}
