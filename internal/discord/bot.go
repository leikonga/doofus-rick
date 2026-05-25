package discord

import (
	"context"
	"fmt"
	"log/slog"
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
	"github.com/leikonga/doofus-rick/internal/store"
)

type Bot struct {
	ctx           context.Context
	store         *store.Store
	config        *config.Config
	client        *disgobot.Client
	agent         *agent.Agent
	cache         UserCache
	presences     sync.Map // snowflake.ID -> discord.OnlineStatus
	voiceChannels sync.Map // snowflake.ID -> string (channel name, empty if unknown)
}

func New(ctx context.Context, s *store.Store, c *config.Config) *Bot {
	return &Bot{
		ctx:    ctx,
		store:  s,
		config: c,
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
		disgobot.WithEventListenerFunc(b.onPresenceUpdate),
		disgobot.WithEventListenerFunc(b.onGuildVoiceStateUpdate),
	)
	if err != nil {
		return err
	}
	b.client = client
	b.agent = agent.New(b.store, b.config, b, b.client)

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
