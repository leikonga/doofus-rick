package bot

import (
	"context"
	"log/slog"

	"github.com/disgoorg/disgo"
	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/gateway"
	"github.com/disgoorg/disgo/handler"
	"github.com/disgoorg/snowflake/v2"
	"github.com/leikonga/doofus-rick/internal/config"
	"github.com/leikonga/doofus-rick/internal/store"
)

type Bot struct {
	store  *store.Store
	config *config.Config
	client *bot.Client
}

func New(s *store.Store, c *config.Config) *Bot {
	return &Bot{store: s, config: c}
}

func (b *Bot) Run() error {
	r := handler.New()
	r.SlashCommand("/ping", b.handlePingCommand)
	r.SlashCommand("/quote", b.handleQuote)
	r.SlashCommand("/randomquote", b.handleRandomQuote)
	r.Modal("/quote", b.handleQuoteSubmission)

	client, err := disgo.New(b.config.DiscordToken,
		bot.WithGatewayConfigOpts(
			gateway.WithIntents(gateway.IntentGuilds, gateway.IntentGuildMembers),
		),
		bot.WithEventListeners(r),
		bot.WithEventListenerFunc(onMessageCreate),
	)
	if err != nil {
		return err
	}
	b.client = client

	if b.config.DiscordGuild == "" {
		slog.Warn("no discord guild configured, skipping command registration")
	} else {
		guildID := snowflake.MustParse(b.config.DiscordGuild)
		if err = handler.SyncCommands(client, commands, []snowflake.ID{guildID}); err != nil {
			slog.Error("failed to sync commands", "error", err)
		}
	}

	if err = client.OpenGateway(context.TODO()); err != nil {
		return err
	}

	slog.Info("connected to discord", "appid", client.ApplicationID)
	return nil
}
