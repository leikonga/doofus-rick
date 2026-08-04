package archive

import (
	"context"
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

type Chunker struct {
	config ChunkConfig
	store  *store.Store
}

func NewChunker(config ChunkConfig, s *store.Store) *Chunker {
	if config.ChunkGap == 0 {
		config.ChunkGap = DefaultChunkGap
	}
	if config.ChunkMaxMsgs == 0 {
		config.ChunkMaxMsgs = DefaultChunkMaxMsgs
	}
	if config.ChunkMaxChars == 0 {
		config.ChunkMaxChars = DefaultChunkMaxChars
	}
	return &Chunker{config: config, store: s}
}

type Chunk struct {
	ChannelID      uint64
	Messages       []store.Message
	StartedAt      time.Time
	EndedAt        time.Time
	MessageCount   int
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
	var lastTime time.Time

	for i, msg := range messages {
		if i == 0 {
			current = Chunk{
				ChannelID:      msg.ChannelID,
				Messages:       []store.Message{msg},
				StartedAt:      msg.CreatedAt,
				EndedAt:        msg.CreatedAt,
				FirstMessageID: msg.ID,
				LastMessageID:  msg.ID,
			}
			lastTime = msg.CreatedAt
			continue
		}

		gap := msg.CreatedAt.Sub(lastTime)
		if gap > c.config.ChunkGap || current.MessageCount >= c.config.ChunkMaxMsgs || len(current.Content)+len(msg.Content) > c.config.ChunkMaxChars {
			if current.MessageCount > 0 {
				chunks = append(chunks, current)
			}
			current = Chunk{
				ChannelID:      msg.ChannelID,
				Messages:       []store.Message{msg},
				StartedAt:      msg.CreatedAt,
				EndedAt:        msg.CreatedAt,
				FirstMessageID: msg.ID,
				LastMessageID:  msg.ID,
			}
		} else {
			current.Messages = append(current.Messages, msg)
			current.EndedAt = msg.CreatedAt
			current.LastMessageID = msg.ID
		}

		lastTime = msg.CreatedAt
	}

	if current.MessageCount > 0 {
		chunks = append(chunks, current)
	}

	return chunks
}

func (c *Chunker) BuildChunkContent(chunk Chunk) string {
	var content string
	for _, msg := range chunk.Messages {
		ts := msg.CreatedAt.Format("15:04")
		content += "[" + ts + " " + msg.AuthorName + "]: " + msg.Content + "\n"
	}
	return content
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
