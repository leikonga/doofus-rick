package discord

import (
	"context"
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
	"github.com/leikonga/doofus-rick/internal/config"
	"github.com/leikonga/doofus-rick/internal/store"
)

type Agent interface {
	HandleMention(ctx context.Context, e *events.MessageCreate)
	ReportDeploy(ctx context.Context)
	ReportInterruptedTasks(ctx context.Context)
	RunTasks(ctx context.Context)
}

type Handlers struct {
	Agent   Agent
	Archive Archive
	Ambient Ambient
}

type Ambient interface {
	Check(channelID snowflake.ID)
}

type Archive interface {
	RecordLive(ctx context.Context, msg discord.Message, channelID snowflake.ID) bool
	Run(ctx context.Context)
}

type Bot struct {
	store            *store.Store
	config           *config.Config
	client           *disgobot.Client
	agent            Agent
	cache            UserCache
	presences        sync.Map // snowflake.ID -> UserPresence
	voiceChannels    sync.Map // snowflake.ID -> string (channel name, empty if unknown)
	deployReportOnce sync.Once
	taskReportOnce   sync.Once
}

func New(c *config.Config, s *store.Store) (*Bot, error) {
	client, err := disgo.New(c.DiscordToken,
		disgobot.WithGatewayConfigOpts(
			gateway.WithIntents(gateway.IntentGuilds, gateway.IntentGuildMembers, gateway.IntentGuildMessages, gateway.IntentMessageContent, gateway.IntentGuildPresences, gateway.IntentGuildVoiceStates),
		),
	)
	if err != nil {
		return nil, err
	}
	return &Bot{store: s, config: c, client: client}, nil
}

func (b *Bot) Client() *disgobot.Client {
	return b.client
}

func (b *Bot) Open(ctx context.Context, h Handlers) error {
	b.agent = h.Agent

	r := handler.New()
	r.SlashCommand("/ping", b.handlePingCommand)
	r.SlashCommand("/quote", b.handleQuote)
	r.SlashCommand("/randomquote", func(d discord.SlashCommandInteractionData, e *handler.CommandEvent) error {
		return b.handleRandomQuote(ctx, d, e)
	})
	r.SlashCommand("/mama", b.handleMama)
	r.Modal("/quote", func(e *handler.ModalEvent) error {
		return b.handleQuoteSubmission(ctx, e)
	})

	b.client.AddEventListeners(
		r,
		disgobot.NewListenerFunc(func(e *events.MessageCreate) { h.Agent.HandleMention(ctx, e) }),
		disgobot.NewListenerFunc(b.onGuildReady),
		disgobot.NewListenerFunc(func(*events.GuildReady) { b.reportDeployOnce(ctx) }),
		disgobot.NewListenerFunc(func(*events.GuildReady) { b.reportInterruptedTasksOnce(ctx) }),
		disgobot.NewListenerFunc(b.onPresenceUpdate),
		disgobot.NewListenerFunc(b.onGuildVoiceStateUpdate),
		disgobot.NewListenerFunc(func(e *events.MessageCreate) {
			if !b.config.ArchiveEnabled {
				return
			}
			if h.Archive.RecordLive(ctx, e.Message, e.ChannelID) {
				h.Ambient.Check(e.ChannelID)
			}
		}),
	)

	if b.config.DiscordGuild == "" {
		slog.Warn("no discord guild configured, skipping command registration")
	} else {
		guildID := snowflake.MustParse(b.config.DiscordGuild)
		if err := handler.SyncCommands(b.client, commands, []snowflake.ID{guildID}); err != nil {
			slog.Error("failed to sync commands", "error", err)
		}
	}

	if err := b.client.OpenGateway(ctx); err != nil {
		return err
	}

	h.Archive.Run(ctx)

	go h.Agent.RunTasks(ctx)

	slog.Info("connected to discord", "appid", b.client.ApplicationID)
	return nil
}

func (b *Bot) reportDeployOnce(ctx context.Context) {
	b.deployReportOnce.Do(func() {
		go func() {
			ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			defer cancel()
			b.agent.ReportDeploy(ctx)
		}()
	})
}

func (b *Bot) reportInterruptedTasksOnce(ctx context.Context) {
	b.taskReportOnce.Do(func() {
		go func() {
			ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			defer cancel()
			b.agent.ReportInterruptedTasks(ctx)
		}()
	})
}
