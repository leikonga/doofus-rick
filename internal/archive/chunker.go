package archive

import (
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

type DisplayNameResolver interface {
	GetUsernameForID(id string) (string, error)
}

type Chunker struct {
	config   ChunkConfig
	resolver DisplayNameResolver
}

func NewChunker(config ChunkConfig, resolver DisplayNameResolver) *Chunker {
	if config.ChunkGap == 0 {
		config.ChunkGap = DefaultChunkGap
	}
	if config.ChunkMaxMsgs == 0 {
		config.ChunkMaxMsgs = DefaultChunkMaxMsgs
	}
	if config.ChunkMaxChars == 0 {
		config.ChunkMaxChars = DefaultChunkMaxChars
	}
	return &Chunker{config: config, resolver: resolver}
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
