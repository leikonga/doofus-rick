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
	"github.com/leikonga/doofus-rick/internal/config"
	"github.com/leikonga/doofus-rick/internal/logbuf"
	"github.com/leikonga/doofus-rick/internal/store"
	"github.com/leikonga/doofus-rick/internal/tracer"
)

type Bot struct {
	ctx           context.Context
	store         *store.Store
	config        *config.Config
	client        *disgobot.Client
	agent         *agent.Agent
	logBuf        *logbuf.Buffer
	tracer        *tracer.Tracer
	cache         UserCache
	presences     sync.Map // snowflake.ID -> UserPresence
	voiceChannels sync.Map // snowflake.ID -> string (channel name, empty if unknown)
	httpClient    *http.Client
	backfillMutex sync.Mutex
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

	slog.Info("connected to discord", "appid", client.ApplicationID)
	return nil
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

	if e.Message.Author.Bot {
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
}

func (b *Bot) isChannelDenied(channelID snowflake.ID) bool {
	if b.config.ArchiveDenyChannels == "" {
		return false
	}
	denied := strings.Split(b.config.ArchiveDenyChannels, ",")
	for _, d := range denied {
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

	state, err := b.store.GetBackfillState(ctx)
	if err != nil {
		slog.Warn("failed to get backfill state", "error", err)
		return
	}

	if state.Status == "running" {
		slog.Info("backfill already running, skipping")
		return
	}

	state.Status = "running"
	state.StartedAt = &[]time.Time{time.Now()}[0]
	state.UpdatedAt = time.Now()
	if err := b.store.UpdateBackfillState(ctx, state); err != nil {
		slog.Warn("failed to update backfill state", "error", err)
		return
	}

	defer func() {
		state.Status = "done"
		state.FinishedAt = &[]time.Time{time.Now()}[0]
		state.UpdatedAt = time.Now()
		if err := b.store.UpdateBackfillState(ctx, state); err != nil {
			slog.Warn("failed to finalize backfill state", "error", err)
		}
	}()

	delay, err := time.ParseDuration(b.config.BackfillDelay)
	if err != nil {
		delay = 1 * time.Second
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

	for _, ch := range channels {
		select {
		case <-ctx.Done():
			state.Status = "failed"
			errMsg := "interrupted"
			state.LastError = &errMsg
			state.UpdatedAt = time.Now()
			b.store.UpdateBackfillState(ctx, state)
			return
		default:
		}

		if err := b.backfillChannel(ctx, ch.ChannelID, delay); err != nil {
			ch.LastError = &[]string{err.Error()}[0]
			ch.UpdatedAt = time.Now()
			b.store.SaveBackfillChannel(ctx, &ch)
			slog.Warn("backfill failed for channel", "channel", ch.ChannelID, "error", err)
			continue
		}

		ch.Done = true
		ch.UpdatedAt = time.Now()
		b.store.SaveBackfillChannel(ctx, &ch)

		state.ChannelsDone++
		state.UpdatedAt = time.Now()
		if err := b.store.UpdateBackfillState(ctx, state); err != nil {
			slog.Warn("failed to update backfill progress", "error", err)
		}
	}
}

func (b *Bot) backfillChannel(ctx context.Context, channelID uint64, delay time.Duration) error {
	botID := b.client.ID()

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

			if msg.ID >= snowflake.ID(oldestFetched) && oldestFetched > 0 {
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
			oldestFetched = uint64(msg.ID)
		}

		channel.OldestFetched = &oldestFetched
		channel.UpdatedAt = time.Now()
		if err := b.store.SaveBackfillChannel(ctx, channel); err != nil {
			slog.Warn("failed to save backfill cursor", "error", err)
		}

		if len(msgs) < b.config.BackfillBatch {
			break
		}

		time.Sleep(delay)
	}

	return nil
}
