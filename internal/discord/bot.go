package discord

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/disgoorg/disgo"
	disgobot "github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/gateway"
	"github.com/disgoorg/disgo/handler"
	"github.com/disgoorg/snowflake/v2"
	"github.com/leikonga/doofus-rick/internal/agent"
	"github.com/leikonga/doofus-rick/internal/ambient"
	"github.com/leikonga/doofus-rick/internal/archive"
	"github.com/leikonga/doofus-rick/internal/config"
	"github.com/leikonga/doofus-rick/internal/llm"
	"github.com/leikonga/doofus-rick/internal/logbuf"
	"github.com/leikonga/doofus-rick/internal/store"
	"github.com/leikonga/doofus-rick/internal/tracer"
)

type Bot struct {
	ctx               context.Context
	store             *store.Store
	config            *config.Config
	client            *disgobot.Client
	agent             *agent.Agent
	logBuf            *logbuf.Buffer
	tracer            *tracer.Tracer
	cache             UserCache
	presences         sync.Map // snowflake.ID -> UserPresence
	voiceChannels     sync.Map // snowflake.ID -> string (channel name, empty if unknown)
	httpClient        *http.Client
	backfillMutex     sync.Mutex
	chunker           *archive.Chunker
	embedder          *archive.Embedder
	chunkGapDuration  time.Duration
	ambientGate       *ambient.Gate
	ambientClassifier *ambient.Classifier
	ambientWindow     time.Duration
	affinityScorer    *archive.AffinityScorer
}

func New(ctx context.Context, s *store.Store, c *config.Config, lb *logbuf.Buffer, tr *tracer.Tracer) *Bot {
	return &Bot{
		ctx:        ctx,
		store:      s,
		config:     c,
		logBuf:     lb,
		tracer:     tr,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (b *Bot) Run() error {
	r := handler.New()
	r.SlashCommand("/ping", b.handlePingCommand)
	r.SlashCommand("/quote", b.handleQuote)
	r.SlashCommand("/randomquote", b.handleRandomQuote)
	r.Modal("/quote", b.handleQuoteSubmission)

	client, err := disgo.New(b.config.DiscordToken,
		disgobot.WithGatewayConfigOpts(
			gateway.WithIntents(gateway.IntentGuilds, gateway.IntentGuildMembers, gateway.IntentGuildMessages, gateway.IntentMessageContent, gateway.IntentGuildPresences, gateway.IntentGuildVoiceStates),
		),
		disgobot.WithEventListeners(r),
		disgobot.WithEventListenerFunc(func(e *events.MessageCreate) { b.agent.HandleMention(b.ctx, e) }),
		disgobot.WithEventListenerFunc(b.onGuildReady),
		disgobot.WithEventListenerFunc(b.onPresenceUpdate),
		disgobot.WithEventListenerFunc(b.onGuildVoiceStateUpdate),
		disgobot.WithEventListenerFunc(b.onMessageCreate),
	)
	if err != nil {
		return err
	}
	b.client = client
	b.agent = agent.New(b.store, b.config, b, b.client, b.logBuf, b.tracer)

	b.chunkGapDuration = parseDurationOr(b.config.ChunkGap, archive.DefaultChunkGap)
	b.chunker = archive.NewChunker(archive.ChunkConfig{
		ChunkGap:      b.chunkGapDuration,
		ChunkMaxMsgs:  b.config.ChunkMaxMsgs,
		ChunkMaxChars: b.config.ChunkMaxChars,
	}, b.store, b)
	llmClient := llm.NewClient(b.config.OpenRouterAPIKey)
	b.embedder = archive.NewEmbedder(archive.EmbeddingConfig{Model: b.config.RickEmbedModel}, b.store, llmClient)

	if b.config.AmbientEnabled {
		b.ambientWindow = parseDurationOr(b.config.AmbientWindow, 90*time.Second)
		b.ambientGate = ambient.NewGate(ambient.GateConfig{
			Enabled:      b.config.AmbientEnabled,
			Window:       b.ambientWindow,
			MinMsgs:      b.config.AmbientMinMsgs,
			MinAuthors:   b.config.AmbientMinAuthors,
			Cooldown:     parseDurationOr(b.config.AmbientCooldown, 60*time.Minute),
			DailyCap:     b.config.AmbientDailyCap,
			EvalDebounce: parseDurationOr(b.config.AmbientEvalDebounce, 60*time.Second),
			MinScore:     b.config.AmbientMinScore,
			Model:        b.config.AmbientModel,
			MaxTokens:    b.config.AmbientMaxTokens,
		}, b.store)
		classifierModel := b.config.AmbientModel
		if classifierModel == "" {
			classifierModel = b.config.RickModel
		}
		b.ambientClassifier = ambient.NewClassifier(ambient.ClassifierConfig{
			Model:     classifierModel,
			MaxTokens: b.config.AmbientMaxTokens,
			MinScore:  b.config.AmbientMinScore,
		}, llmClient, b.store)
	}

	if b.config.AffinityEnabled {
		affinityModel := b.config.AffinityModel
		if affinityModel == "" {
			affinityModel = b.config.RickModel
		}
		aff := archive.NewAffinity(archive.AffinityConfig{
			Baseline:    b.config.AffinityBaseline,
			DecayPerDay: b.config.AffinityDecayPerDay,
			Model:       affinityModel,
		}, b.store)
		b.affinityScorer = archive.NewAffinityScorer(archive.AffinityScorerConfig{Model: affinityModel}, llmClient, aff, b.store)
	}

	if b.config.DiscordGuild == "" {
		slog.Warn("no discord guild configured, skipping command registration")
	} else {
		guildID := snowflake.MustParse(b.config.DiscordGuild)
		if err = handler.SyncCommands(client, commands, []snowflake.ID{guildID}); err != nil {
			slog.Error("failed to sync commands", "error", err)
		}
	}

	if err = client.OpenGateway(b.ctx); err != nil {
		return err
	}

	if b.config.BackfillEnabled {
		go b.runBackfillWorker(b.ctx)
	}

	go b.runReminderLoop(b.ctx)

	if b.config.ArchiveEnabled {
		go b.runChunkingLoop(b.ctx)
		go b.runEmbeddingLoop(b.ctx)
	}

	slog.Info("connected to discord", "appid", client.ApplicationID)
	return nil
}

func parseDurationOr(s string, fallback time.Duration) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		return fallback
	}
	return d
}

func (b *Bot) runReminderLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.fireReminders(ctx)
		}
	}
}

func (b *Bot) fireReminders(ctx context.Context) {
	reminders, err := b.store.GetDueReminders(ctx)
	if err != nil {
		slog.Warn("failed to fetch due reminders", "error", err)
		return
	}
	for _, r := range reminders {
		chID := snowflake.MustParse(r.ChannelID)
		content := fmt.Sprintf("<@%s> %s", r.UserID, r.Message)
		if _, err := b.client.Rest.CreateMessage(chID, discord.NewMessageCreate().WithContent(content)); err != nil {
			slog.Warn("failed to send reminder", "id", r.ID, "error", err)
			continue
		}
		if err := b.store.MarkReminderFired(ctx, r.ID); err != nil {
			slog.Warn("failed to mark reminder fired", "id", r.ID, "error", err)
		}
	}
}

func (b *Bot) onMessageCreate(e *events.MessageCreate) {
	if !b.config.ArchiveEnabled {
		return
	}

	// Other bots are skipped, but Rick's own messages are archived so the
	// ambient gate can see whether he already spoke in a burst.
	isRick := b.client != nil && e.Message.Author.ID == b.client.ID()
	if e.Message.Author.Bot && !isRick {
		return
	}

	if strings.HasPrefix(e.Message.Content, "/") {
		return
	}

	if b.isChannelDenied(e.ChannelID) {
		return
	}

	isForgotten, err := b.store.IsAuthorForgotten(context.Background(), uint64(e.Message.Author.ID))
	if err != nil {
		slog.Warn("failed to check if author is forgotten", "error", err)
		return
	}
	if isForgotten {
		return
	}

	content := e.Message.Content
	if len(content) > 10000 {
		content = content[:10000]
	}

	attachmentsJSON, _ := b.serializeAttachments(e.Message.Attachments)

	msg := store.Message{
		ID:          uint64(e.Message.ID),
		ChannelID:   uint64(e.ChannelID),
		AuthorID:    uint64(e.Message.Author.ID),
		AuthorName:  e.Message.Author.Username,
		Content:     content,
		ReplyToID:   nil,
		IsBot:       e.Message.Author.Bot,
		Attachments: attachmentsJSON,
		CreatedAt:   e.Message.CreatedAt,
		EditedAt:    nil,
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := b.store.CreateMessage(ctx, msg); err != nil {
			slog.Warn("failed to archive message", "error", err)
		}
	}()

	if !isRick {
		b.checkAmbient(e.ChannelID)
	}
}

// checkAmbient evaluates the ambient gate for a channel after a human
// message lands, and fires an unprompted response if it passes. Runs in its
// own goroutine so it never delays message handling.
func (b *Bot) checkAmbient(channelID snowflake.ID) {
	if !b.config.AmbientEnabled || b.ambientGate == nil || b.ambientClassifier == nil {
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		since := time.Now().Add(-b.ambientWindow)
		msgs, err := b.store.GetRecentMessagesSince(ctx, uint64(channelID), since, 200)
		if err != nil {
			slog.Warn("failed to load ambient window", "channel", channelID, "error", err)
			return
		}

		result := b.ambientGate.CheckGate(ctx, channelID, b.client.ID(), msgs)
		if err := b.ambientGate.EvalTouch(ctx, channelID); err != nil {
			slog.Warn("failed to record ambient eval", "channel", channelID, "error", err)
		}
		if !result.Passed {
			return
		}

		llmMsgs := make([]llm.Message, 0, len(msgs))
		for _, m := range msgs {
			name := m.AuthorName
			if m.IsBot {
				name += " (bot)"
			}
			llmMsgs = append(llmMsgs, llm.NewUserMessage(llm.TextPart(fmt.Sprintf("[%s]: %s", name, m.Content))))
		}

		classified, err := b.ambientClassifier.Classify(ctx, uint64(channelID), llmMsgs)
		if err != nil {
			slog.Warn("ambient classification failed", "channel", channelID, "error", err)
			return
		}
		if classified.Hook == "" {
			return
		}

		sentID, err := b.agent.HandleAmbient(ctx, channelID, classified.Hook)
		if err != nil {
			slog.Warn("ambient response failed", "channel", channelID, "error", err)
			return
		}

		if err := b.ambientGate.LogFire(ctx, channelID, classified.Score, classified.Hook); err != nil {
			slog.Warn("failed to log ambient fire", "channel", channelID, "error", err)
		}
		if err := b.ambientGate.UpdateState(ctx, channelID, classified.Score, classified.Hook, uint64(sentID)); err != nil {
			slog.Warn("failed to update ambient state", "channel", channelID, "error", err)
		}
	}()
}

func (b *Bot) runChunkingLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			channelIDs, err := b.store.GetChannelsWithUnchunkedMessages(ctx, 100)
			if err != nil {
				slog.Warn("failed to list channels with unchunked messages", "error", err)
				continue
			}
			for _, channelID := range channelIDs {
				b.chunkChannel(ctx, channelID)
			}
		}
	}
}

// chunkChannel closes any complete chunks for a channel's unchunked
// messages, leaving the trailing chunk unsaved if it's still within
// ChunkGap of now, since more messages could still extend it.
func (b *Bot) chunkChannel(ctx context.Context, channelID uint64) {
	sinceID, err := b.store.GetLastChunkedMessageID(ctx, channelID)
	if err != nil {
		slog.Warn("failed to get last chunked message id", "channel", channelID, "error", err)
		return
	}

	msgs, err := b.store.GetUnchunkedMessages(ctx, channelID, sinceID, 500)
	if err != nil {
		slog.Warn("failed to get unchunked messages", "channel", channelID, "error", err)
		return
	}
	if len(msgs) == 0 {
		return
	}

	chunks := b.chunker.ChunkMessages(msgs)
	if len(chunks) == 0 {
		return
	}

	cutoff := time.Now().Add(-b.chunkGapDuration)
	if chunks[len(chunks)-1].EndedAt.After(cutoff) {
		chunks = chunks[:len(chunks)-1]
	}

	botID := uint64(b.client.ID())
	for _, c := range chunks {
		c.Content = b.chunker.BuildChunkContent(c)
		stored := store.Chunk{
			ChannelID:      c.ChannelID,
			Content:        c.Content,
			StartedAt:      c.StartedAt,
			EndedAt:        c.EndedAt,
			MessageCount:   len(c.Messages),
			FirstMessageID: c.FirstMessageID,
			LastMessageID:  c.LastMessageID,
		}
		if err := b.store.CreateChunk(ctx, stored); err != nil {
			slog.Warn("failed to save chunk", "channel", channelID, "error", err)
			return
		}

		if b.affinityScorer != nil {
			go func(c archive.Chunk) {
				scoreCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				if err := b.affinityScorer.ScoreChunk(scoreCtx, c, botID); err != nil {
					slog.Warn("affinity scoring failed", "channel", channelID, "error", err)
				}
			}(c)
		}
	}
}

func (b *Bot) runEmbeddingLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			chunks, err := b.store.GetChunksWithoutEmbedding(ctx, b.config.RickEmbedModel, 100)
			if err != nil {
				slog.Warn("failed to get chunks pending embedding", "error", err)
				continue
			}
			if len(chunks) == 0 {
				continue
			}
			if err := b.embedder.EmbedChunks(ctx, chunks); err != nil {
				slog.Warn("failed to embed chunks", "error", err)
			}
		}
	}
}

func (b *Bot) isChannelDenied(channelID snowflake.ID) bool {
	if b.config.ArchiveDenyChannels == "" {
		return false
	}
	denied := strings.SplitSeq(b.config.ArchiveDenyChannels, ",")
	for d := range denied {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		if d == channelID.String() {
			return true
		}
	}
	return false
}

func (b *Bot) serializeAttachments(attachments []discord.Attachment) (*string, error) {
	if len(attachments) == 0 {
		return nil, nil
	}
	return &attachments[0].Filename, nil
}

func (b *Bot) runBackfillWorker(ctx context.Context) {
	b.backfillMutex.Lock()
	defer b.backfillMutex.Unlock()

	slog.Info("backfill worker starting")

	state, err := b.store.GetOrCreateBackfillState(ctx)
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
	if err := b.store.UpdateBackfillState(ctx, state); err != nil {
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
		if err := b.store.UpdateBackfillState(context.Background(), state); err != nil {
			slog.Warn("failed to finalize backfill state", "error", err)
		}
		slog.Info("backfill worker finished", "status", state.Status, "channels_total", state.ChannelsTotal, "channels_done", state.ChannelsDone)
	}()

	delay, err := time.ParseDuration(b.config.BackfillDelay)
	if err != nil {
		delay = 1 * time.Second
	}

	if seeded, err := b.seedBackfillChannels(ctx); err != nil {
		slog.Warn("failed to seed backfill channels from guild", "error", err)
	} else if seeded > 0 {
		slog.Info("seeded new channels for backfill", "count", seeded)
	}

	channels, err := b.store.GetBackfillChannels(ctx, 100)
	if err != nil {
		slog.Warn("failed to get backfill channels", "error", err)
		return
	}

	state.ChannelsTotal = len(channels)
	state.ChannelsDone = 0
	state.UpdatedAt = time.Now()
	if err := b.store.UpdateBackfillState(ctx, state); err != nil {
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
			if err := b.store.UpdateBackfillState(context.Background(), state); err != nil {
				slog.Warn("failed to record backfill interruption", "error", err)
			}
			return
		default:
		}

		if err := b.backfillChannel(ctx, ch.ChannelID, delay); err != nil {
			ch.LastError = &[]string{err.Error()}[0]
			ch.UpdatedAt = time.Now()
			if saveErr := b.store.SaveBackfillChannel(ctx, &ch); saveErr != nil {
				slog.Warn("failed to save backfill channel error state", "error", saveErr)
			}
			slog.Warn("backfill failed for channel", "channel", ch.ChannelID, "error", err)
			continue
		}

		ch.Done = true
		ch.UpdatedAt = time.Now()
		if err := b.store.SaveBackfillChannel(ctx, &ch); err != nil {
			slog.Warn("failed to save backfill channel completion", "error", err)
		}

		state.ChannelsDone++
		state.MessagesSeen += ch.MessagesSeen
		state.UpdatedAt = time.Now()
		if err := b.store.UpdateBackfillState(ctx, state); err != nil {
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
func (b *Bot) seedBackfillChannels(ctx context.Context) (int, error) {
	if b.config.DiscordGuild == "" {
		return 0, nil
	}
	guildID, err := snowflake.Parse(b.config.DiscordGuild)
	if err != nil {
		return 0, err
	}

	channels, err := b.client.Rest.GetGuildChannels(guildID)
	if err != nil {
		return 0, err
	}

	var ids []uint64
	for _, ch := range channels {
		if _, ok := ch.(discord.GuildMessageChannel); ok {
			ids = append(ids, uint64(ch.ID()))
		}
	}

	return b.store.SeedBackfillChannels(ctx, ids)
}

func (b *Bot) backfillChannel(ctx context.Context, channelID uint64, delay time.Duration) error {
	botID := b.client.ID()
	channelStart := time.Now()

	newestMsg, err := b.client.Rest.GetMessages(snowflake.ID(channelID), 0, 0, 0, 1)
	if err != nil {
		return err
	}

	var newestAtStart uint64
	if len(newestMsg) > 0 {
		newestAtStart = uint64(newestMsg[0].ID)
	}

	channel, err := b.store.GetBackfillChannel(ctx, channelID)
	if err != nil {
		channel = &store.BackfillChannel{
			ChannelID:     channelID,
			NewestAtStart: &newestAtStart,
			OldestFetched: nil,
			Done:          false,
		}
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

		msgs, err := b.client.Rest.GetMessages(snowflake.ID(channelID), snowflake.ID(before), 0, 0, b.config.BackfillBatch)
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

			if msg.Author.ID == botID {
				continue
			}

			if strings.HasPrefix(msg.Content, "/") {
				continue
			}

			if b.isChannelDenied(snowflake.ID(channelID)) {
				continue
			}

			isForgotten, err := b.store.IsAuthorForgotten(ctx, uint64(msg.Author.ID))
			if err != nil {
				slog.Warn("failed to check if author is forgotten", "error", err)
				continue
			}
			if isForgotten {
				continue
			}

			content := msg.Content
			if len(content) > 10000 {
				content = content[:10000]
			}

			attachmentsJSON, _ := b.serializeAttachments(msg.Attachments)

			storedMsg := store.Message{
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

			if err := b.store.CreateMessage(ctx, storedMsg); err != nil {
				slog.Warn("failed to archive message during backfill", "error", err)
				continue
			}

			channel.MessagesSeen++
		}

		channel.OldestFetched = &oldestFetched
		channel.UpdatedAt = time.Now()
		if err := b.store.SaveBackfillChannel(ctx, channel); err != nil {
			slog.Warn("failed to save backfill cursor", "error", err)
		}

		elapsed := time.Since(channelStart)
		rate := float64(channel.MessagesSeen) / elapsed.Seconds()
		slog.Info("backfill channel progress", "channel", channelID, "messages_seen", channel.MessagesSeen,
			"batch_size", len(msgs), "elapsed", elapsed.Round(time.Second),
			"rate_per_sec", fmt.Sprintf("%.1f", rate))

		if len(msgs) < b.config.BackfillBatch {
			break
		}

		time.Sleep(delay)
	}

	return nil
}
