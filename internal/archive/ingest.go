package archive

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"
	"github.com/leikonga/doofus-rick/internal/store"
)

const maxArchivedContentBytes = 10000

type discordREST interface {
	GetMessages(channelID snowflake.ID, around, before, after snowflake.ID, limit int, opts ...rest.RequestOpt) ([]discord.Message, error)
	GetGuildChannels(guildID snowflake.ID, opts ...rest.RequestOpt) ([]discord.GuildChannel, error)
}

type ChunkScorer interface {
	ScoreChunk(ctx context.Context, chunk Chunk, rickID uint64) error
}

type IngestConfig struct {
	ArchiveEnabled  bool
	BackfillEnabled bool
	DenyList        string
	GuildID         string
	BackfillDelay   time.Duration
	BackfillBatch   int
	ChunkGap        time.Duration
	EmbedModel      string
}

// Ingest owns the message-archive pipeline: live recording, backfill, chunking and embedding.
type Ingest struct {
	config        IngestConfig
	store         *store.Store
	rest          discordREST
	selfID        func() snowflake.ID
	chunker       *Chunker
	embedder      *Embedder
	scorer        ChunkScorer
	filter        filter
	backfillMutex sync.Mutex
	wg            sync.WaitGroup
}

func NewIngest(cfg IngestConfig, s *store.Store, client discordREST, selfID func() snowflake.ID, chunker *Chunker, embedder *Embedder, scorer ChunkScorer) *Ingest {
	return &Ingest{
		config:   cfg,
		store:    s,
		rest:     client,
		selfID:   selfID,
		chunker:  chunker,
		embedder: embedder,
		scorer:   scorer,
		filter:   filter{denyList: cfg.DenyList},
	}
}

// Run starts the background workers and does not block.
func (i *Ingest) Run(ctx context.Context) {
	if i.config.BackfillEnabled {
		i.wg.Go(func() { i.runBackfillWorker(ctx) })
	}

	if i.config.ArchiveEnabled {
		i.wg.Go(func() { i.runChunkingLoop(ctx) })
		i.wg.Go(func() { i.runEmbeddingLoop(ctx) })
	}
}

func (i *Ingest) Wait() {
	i.wg.Wait()
}

// RecordLive archives a live message and reports whether it came from someone other than Rick.
func (i *Ingest) RecordLive(ctx context.Context, msg discord.Message, channelID snowflake.ID) bool {
	selfID := i.selfID()
	isRick := msg.Author.ID == selfID

	if !i.filter.keepLive(msg, channelID, selfID) {
		return false
	}

	checkCtx, cancelCheck := context.WithTimeout(ctx, 5*time.Second)
	defer cancelCheck()
	isForgotten, err := i.store.IsAuthorForgotten(checkCtx, uint64(msg.Author.ID))
	if err != nil {
		slog.Warn("failed to check if author is forgotten", "error", err)
		return false
	}
	if isForgotten {
		return false
	}

	stored := toStoredMessage(msg, channelID)

	i.wg.Go(func() {
		// The message was already received, so the write must survive shutdown cancellation.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := i.store.CreateMessage(ctx, stored); err != nil {
			slog.Warn("failed to archive message", "error", err)
		}
	})

	return !isRick
}

func (i *Ingest) runChunkingLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			channelIDs, err := i.store.GetChannelsWithUnchunkedMessages(ctx, 100)
			if err != nil {
				slog.Warn("failed to list channels with unchunked messages", "error", err)
				continue
			}
			for _, channelID := range channelIDs {
				i.chunkChannel(ctx, channelID)
			}
		}
	}
}

// chunkChannel closes any complete chunks for a channel's unchunked
// messages, leaving the trailing chunk unsaved if it's still within
// ChunkGap of now, since more messages could still extend it.
func (i *Ingest) chunkChannel(ctx context.Context, channelID uint64) {
	sinceID, err := i.store.GetLastChunkedMessageID(ctx, channelID)
	if err != nil {
		slog.Warn("failed to get last chunked message id", "channel", channelID, "error", err)
		return
	}

	msgs, err := i.store.GetUnchunkedMessages(ctx, channelID, sinceID, 500)
	if err != nil {
		slog.Warn("failed to get unchunked messages", "channel", channelID, "error", err)
		return
	}
	if len(msgs) == 0 {
		return
	}

	chunks := i.chunker.ChunkMessages(msgs)
	if len(chunks) == 0 {
		return
	}

	chunks = completeChunks(chunks, time.Now(), i.config.ChunkGap)

	botID := uint64(i.selfID())
	for _, c := range chunks {
		c.Content = i.chunker.BuildChunkContent(c)
		stored := store.Chunk{
			ChannelID:      c.ChannelID,
			Content:        c.Content,
			StartedAt:      c.StartedAt,
			EndedAt:        c.EndedAt,
			MessageCount:   len(c.Messages),
			FirstMessageID: c.FirstMessageID,
			LastMessageID:  c.LastMessageID,
		}
		if err := i.store.CreateChunk(ctx, stored); err != nil {
			slog.Warn("failed to save chunk", "channel", channelID, "error", err)
			return
		}

		if i.scorer != nil {
			i.wg.Go(func() {
				scoreCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
				defer cancel()
				if err := i.scorer.ScoreChunk(scoreCtx, c, botID); err != nil {
					slog.Warn("affinity scoring failed", "channel", channelID, "error", err)
				}
			})
		}
	}
}

func (i *Ingest) runEmbeddingLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			chunks, err := i.store.GetChunksWithoutEmbedding(ctx, i.config.EmbedModel, 100)
			if err != nil {
				slog.Warn("failed to get chunks pending embedding", "error", err)
				continue
			}
			if len(chunks) == 0 {
				continue
			}
			if err := i.embedder.EmbedChunks(ctx, chunks); err != nil {
				slog.Warn("failed to embed chunks", "error", err)
			}
		}
	}
}

func toStoredMessage(msg discord.Message, channelID snowflake.ID) store.Message {
	content := msg.Content
	if len(content) > maxArchivedContentBytes {
		content = content[:maxArchivedContentBytes]
	}

	attachmentsJSON, _ := serializeAttachments(msg.Attachments)

	return store.Message{
		ID:          uint64(msg.ID),
		ChannelID:   uint64(channelID),
		AuthorID:    uint64(msg.Author.ID),
		AuthorName:  msg.Author.Username,
		Content:     content,
		ReplyToID:   nil,
		IsBot:       msg.Author.Bot,
		Attachments: attachmentsJSON,
		CreatedAt:   msg.CreatedAt,
		EditedAt:    nil,
	}
}

func completeChunks(chunks []Chunk, now time.Time, gap time.Duration) []Chunk {
	if len(chunks) == 0 {
		return chunks
	}
	if chunks[len(chunks)-1].EndedAt.After(now.Add(-gap)) {
		return chunks[:len(chunks)-1]
	}
	return chunks
}

func serializeAttachments(attachments []discord.Attachment) (*string, error) {
	if len(attachments) == 0 {
		return nil, nil
	}
	return &attachments[0].Filename, nil
}

func (i *Ingest) runBackfillWorker(ctx context.Context) {
	i.backfillMutex.Lock()
	defer i.backfillMutex.Unlock()

	slog.Info("backfill worker starting")

	state, err := i.store.GetOrCreateBackfillState(ctx)
	if err != nil {
		slog.Warn("failed to get backfill state", "error", err)
		return
	}

	if state.Status == "running" {
		slog.Warn("backfill state was left running, previous attempt likely crashed; resetting and starting anew")
	}

	state.Status = "running"
	state.StartedAt = &[]time.Time{time.Now()}[0]
	state.LastError = nil
	state.UpdatedAt = time.Now()
	if err := i.store.UpdateBackfillState(ctx, state); err != nil {
		slog.Warn("failed to update backfill state", "error", err)
		return
	}

	defer func() {
		if r := recover(); r != nil {
			state.Status = "failed"
			errMsg := fmt.Sprintf("panic: %v", r)
			state.LastError = &errMsg
		} else if state.Status == "running" {
			state.Status = "done"
		}
		state.FinishedAt = &[]time.Time{time.Now()}[0]
		state.UpdatedAt = time.Now()
		finalCtx, cancelFinal := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancelFinal()
		if err := i.store.UpdateBackfillState(finalCtx, state); err != nil {
			slog.Warn("failed to finalize backfill state", "error", err)
		}
		slog.Info("backfill worker finished", "status", state.Status, "channels_total", state.ChannelsTotal, "channels_done", state.ChannelsDone)
	}()

	delay := i.config.BackfillDelay

	if seeded, err := i.seedBackfillChannels(ctx); err != nil {
		slog.Warn("failed to seed backfill channels from guild", "error", err)
	} else if seeded > 0 {
		slog.Info("seeded new channels for backfill", "count", seeded)
	}

	channels, err := i.store.GetBackfillChannels(ctx, 100)
	if err != nil {
		slog.Warn("failed to get backfill channels", "error", err)
		return
	}

	state.ChannelsTotal = len(channels)
	state.ChannelsDone = 0
	state.UpdatedAt = time.Now()
	if err := i.store.UpdateBackfillState(ctx, state); err != nil {
		slog.Warn("failed to update channels total", "error", err)
		return
	}

	slog.Info("backfill processing channels", "count", len(channels))

	for _, ch := range channels {
		select {
		case <-ctx.Done():
			state.Status = "failed"
			errMsg := "interrupted"
			state.LastError = &errMsg
			state.UpdatedAt = time.Now()
			interruptCtx, cancelInterrupt := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			if err := i.store.UpdateBackfillState(interruptCtx, state); err != nil {
				slog.Warn("failed to record backfill interruption", "error", err)
			}
			cancelInterrupt()
			return
		default:
		}

		if err := i.backfillChannel(ctx, ch.ChannelID, delay); err != nil {
			ch.LastError = &[]string{err.Error()}[0]
			ch.UpdatedAt = time.Now()
			if saveErr := i.store.SaveBackfillChannel(ctx, &ch); saveErr != nil {
				slog.Warn("failed to save backfill channel error state", "error", saveErr)
			}
			slog.Warn("backfill failed for channel", "channel", ch.ChannelID, "error", err)
			continue
		}

		ch.Done = true
		ch.UpdatedAt = time.Now()
		if err := i.store.SaveBackfillChannel(ctx, &ch); err != nil {
			slog.Warn("failed to save backfill channel completion", "error", err)
		}

		state.ChannelsDone++
		state.MessagesSeen += ch.MessagesSeen
		state.UpdatedAt = time.Now()
		if err := i.store.UpdateBackfillState(ctx, state); err != nil {
			slog.Warn("failed to update backfill progress", "error", err)
		}

		elapsed := time.Since(*state.StartedAt)
		remaining := state.ChannelsTotal - state.ChannelsDone
		eta := (elapsed / time.Duration(state.ChannelsDone)) * time.Duration(remaining)
		slog.Info("backfill channel done", "channel", ch.ChannelID, "messages_seen", ch.MessagesSeen,
			"progress", fmt.Sprintf("%d/%d", state.ChannelsDone, state.ChannelsTotal),
			"total_messages_seen", state.MessagesSeen,
			"elapsed", elapsed.Round(time.Second), "eta", eta.Round(time.Second))
	}
}

// seedBackfillChannels inserts a pending backfill_channel row for every
// guild message channel not already tracked, so enabling backfill picks up
// the whole guild without requiring channels to be seeded by hand.
func (i *Ingest) seedBackfillChannels(ctx context.Context) (int, error) {
	if i.config.GuildID == "" {
		return 0, nil
	}
	guildID, err := snowflake.Parse(i.config.GuildID)
	if err != nil {
		return 0, err
	}

	channels, err := i.rest.GetGuildChannels(guildID, rest.WithCtx(ctx))
	if err != nil {
		return 0, err
	}

	var ids []uint64
	for _, ch := range channels {
		if _, ok := ch.(discord.GuildMessageChannel); ok {
			ids = append(ids, uint64(ch.ID()))
		}
	}

	return i.store.SeedBackfillChannels(ctx, ids)
}

func (i *Ingest) backfillChannel(ctx context.Context, channelID uint64, delay time.Duration) error {
	botID := i.selfID()
	channelStart := time.Now()

	newestMsg, err := i.rest.GetMessages(snowflake.ID(channelID), 0, 0, 0, 1, rest.WithCtx(ctx))
	if err != nil {
		return err
	}

	var newestAtStart uint64
	if len(newestMsg) > 0 {
		newestAtStart = uint64(newestMsg[0].ID)
	}

	channel, err := i.store.GetBackfillChannel(ctx, channelID)
	if errors.Is(err, store.ErrNotFound) {
		channel = &store.BackfillChannel{
			ChannelID:     channelID,
			NewestAtStart: &newestAtStart,
			OldestFetched: nil,
			Done:          false,
		}
	} else if err != nil {
		return fmt.Errorf("get backfill channel %d: %w", channelID, err)
	}

	oldestFetched := uint64(0)
	if channel.OldestFetched != nil {
		oldestFetched = *channel.OldestFetched
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		var before uint64
		if oldestFetched > 0 {
			before = oldestFetched
		}

		msgs, err := i.rest.GetMessages(snowflake.ID(channelID), snowflake.ID(before), 0, 0, i.config.BackfillBatch, rest.WithCtx(ctx))
		if err != nil {
			return err
		}

		if len(msgs) == 0 {
			break
		}

		for _, msg := range msgs {
			if msg.ID < snowflake.ID(oldestFetched) || oldestFetched == 0 {
				oldestFetched = uint64(msg.ID)
			}

			if !i.filter.keepBackfill(msg, snowflake.ID(channelID), botID) {
				continue
			}

			isForgotten, err := i.store.IsAuthorForgotten(ctx, uint64(msg.Author.ID))
			if err != nil {
				slog.Warn("failed to check if author is forgotten", "error", err)
				continue
			}
			if isForgotten {
				continue
			}

			storedMsg := toStoredMessage(msg, snowflake.ID(channelID))

			if err := i.store.CreateMessage(ctx, storedMsg); err != nil {
				slog.Warn("failed to archive message during backfill", "error", err)
				continue
			}

			channel.MessagesSeen++
		}

		channel.OldestFetched = &oldestFetched
		channel.UpdatedAt = time.Now()
		if err := i.store.SaveBackfillChannel(ctx, channel); err != nil {
			slog.Warn("failed to save backfill cursor", "error", err)
		}

		elapsed := time.Since(channelStart)
		rate := float64(channel.MessagesSeen) / elapsed.Seconds()
		slog.Info("backfill channel progress", "channel", channelID, "messages_seen", channel.MessagesSeen,
			"batch_size", len(msgs), "elapsed", elapsed.Round(time.Second),
			"rate_per_sec", fmt.Sprintf("%.1f", rate))

		if len(msgs) < i.config.BackfillBatch {
			break
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}

	return nil
}
